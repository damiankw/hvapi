package hyperv

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/damiankw/hvapi/internal/wmi"
)

// Poller keeps the cache fresh with two loops: a fast one for state and a
// slow one for configuration. Handlers only ever read the cache.
type Poller struct {
	q          wmi.Querier
	log        *slog.Logger
	stateEvery time.Duration
	heavyEvery time.Duration

	state    atomic.Pointer[StateData]
	settings atomic.Pointer[SettingsData]

	mu        sync.Mutex
	stateRuns int64
	heavyRuns int64
	stateStat []SourceStatus
	heavyStat []SourceStatus
	stateLast time.Time
	heavyLast time.Time
	stateTook time.Duration
	heavyTook time.Duration
	warnings  []string
}

// AddWarning records a start-up problem (for example a failed Preflight) so
// /status shows it alongside the per-source errors.
func (p *Poller) AddWarning(msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.warnings = append(p.warnings, msg)
}

// NewPoller returns a Poller that is not running yet.
func NewPoller(q wmi.Querier, log *slog.Logger, stateEvery, heavyEvery time.Duration) *Poller {
	return &Poller{q: q, log: log, stateEvery: stateEvery, heavyEvery: heavyEvery}
}

// Run polls until ctx is cancelled. Both loops run at once and each starts
// with an immediate poll, so the cache fills as soon as possible.
func (p *Poller) Run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); p.loop(ctx, p.stateEvery, p.pollState) }()
	go func() { defer wg.Done(); p.loop(ctx, p.heavyEvery, p.pollSettings) }()
	wg.Wait()
}

func (p *Poller) loop(ctx context.Context, every time.Duration, poll func(context.Context)) {
	poll(ctx)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			poll(ctx)
		}
	}
}

// PollOnce runs both polls once, synchronously. Used at start-up and by tests.
func (p *Poller) PollOnce(ctx context.Context) {
	p.pollState(ctx)
	p.pollSettings(ctx)
}

func (p *Poller) pollState(ctx context.Context) {
	rs := collect(ctx, p.q, stateSources, 30*time.Second)
	d := buildState(rs)
	p.state.Store(d)
	p.mu.Lock()
	p.stateRuns++
	p.stateStat, p.stateLast, p.stateTook = rs.status, rs.at, rs.duration
	p.mu.Unlock()
	p.report("state", rs, len(d.vms))
}

func (p *Poller) pollSettings(ctx context.Context) {
	rs := collect(ctx, p.q, settingsSources, 120*time.Second)
	d := buildSettings(rs)
	p.settings.Store(d)
	p.mu.Lock()
	p.heavyRuns++
	p.heavyStat, p.heavyLast, p.heavyTook = rs.status, rs.at, rs.duration
	p.mu.Unlock()
	p.report("settings", rs, len(d.vms))
}

func (p *Poller) report(kind string, rs *rowSet, vms int) {
	if p.log == nil {
		return
	}
	for name, err := range rs.errs {
		p.log.Warn("poll source failed", "poll", kind, "source", name, "error", err.Error())
	}
	p.log.Debug("poll done", "poll", kind, "vms", vms, "duration_ms", rs.duration.Milliseconds(), "errors", len(rs.errs))
}

// Data returns the current cache. Either part may be nil before its first poll.
func (p *Poller) Data() *Data {
	return &Data{State: p.state.Load(), Settings: p.settings.Load()}
}

// Status describes the poll loops for /status.
func (p *Poller) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	st := Status{
		WMIAvailable: true,
		Warnings:     append([]string(nil), p.warnings...),
		State:        PollStatus{IntervalSeconds: int(p.stateEvery.Seconds()), Runs: p.stateRuns, LastDurationMs: p.stateTook.Milliseconds(), Sources: append([]SourceStatus(nil), p.stateStat...)},
		Settings:     PollStatus{IntervalSeconds: int(p.heavyEvery.Seconds()), Runs: p.heavyRuns, LastDurationMs: p.heavyTook.Milliseconds(), Sources: append([]SourceStatus(nil), p.heavyStat...)},
	}
	if !p.stateLast.IsZero() {
		t := p.stateLast.UTC()
		st.State.LastRun = &t
	}
	if !p.heavyLast.IsZero() {
		t := p.heavyLast.UTC()
		st.Settings.LastRun = &t
	}
	if s := p.state.Load(); s != nil {
		st.VMCount = len(s.vms)
		for _, src := range st.State.Sources {
			if src.Name == "computer_system" && src.Error != "" && src.Error == wmi.ErrUnavailable.Error() {
				st.WMIAvailable = false
			}
		}
	}
	return st
}
