package hyperv

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/damiankw/hvapi/internal/evtlog"
	"github.com/damiankw/hvapi/internal/wmi"
)

// eventLog is one Hyper-V event channel the settings poll reads.
type eventLog struct {
	name    string
	channel string
}

// EventLogs are the channels /events covers, by short name.
var EventLogs = []eventLog{
	{"vmms", "Microsoft-Windows-Hyper-V-VMMS-Admin"},
	{"worker", "Microsoft-Windows-Hyper-V-Worker-Admin"},
	{"hypervisor", "Microsoft-Windows-Hyper-V-Hypervisor-Admin"},
	{"compute", "Microsoft-Windows-Hyper-V-Compute-Admin"},
	{"vmswitch", "Microsoft-Windows-Hyper-V-VmSwitch-Operational"},
	{"high-availability", "Microsoft-Windows-Hyper-V-High-Availability-Admin"},
}

const schedulerChannel = "Microsoft-Windows-Hyper-V-Hypervisor-Operational"

// EventSource is the reader and limits for the event logs.
type EventSource struct {
	Reader    evtlog.Reader
	Hours     int
	MaxPerLog int
}

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
	events    EventSource
}

// SetEvents enables event log collection on the settings poll.
func (p *Poller) SetEvents(src EventSource) { p.events = src }

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
	p.readEvents(ctx, d)
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

// readEvents fills d.events from the Hyper-V logs and the scheduler type
// from the hypervisor's operational log.
func (p *Poller) readEvents(ctx context.Context, d *SettingsData) {
	d.events = map[string][]evtlog.Event{}
	d.eventErrs = map[string]error{}
	d.eventHours = p.events.Hours
	if p.events.Reader == nil {
		return
	}
	since := evtlog.SinceQuery(time.Duration(p.events.Hours) * time.Hour)
	for _, l := range EventLogs {
		qctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		ev, err := p.events.Reader.Query(qctx, l.channel, since, p.events.MaxPerLog)
		cancel()
		for i := range ev {
			ev[i].Log = l.name
			if ev[i].Channel == "" {
				ev[i].Channel = l.channel
			}
		}
		d.events[l.name] = ev
		if err != nil {
			d.eventErrs[l.name] = err
			if p.log != nil {
				p.log.Warn("event log read failed", "log", l.name, "error", err.Error())
			}
		}
	}
	qctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if ev, err := p.events.Reader.Query(qctx, schedulerChannel, "*[System[(EventID=2)]]", 1); err == nil && len(ev) > 0 {
		d.schedulerType = schedulerTypeFrom(ev[0])
	}
}

var schedulerTypes = map[int64]string{1: "Classic (SMT disabled)", 2: "Classic", 3: "Core", 4: "Root"}

// schedulerTypeFrom decodes "Hypervisor scheduler type is 0x3" (event ID 2).
func schedulerTypeFrom(e evtlog.Event) string {
	candidates := append([]string{}, e.Data...)
	for _, f := range strings.Fields(strings.ReplaceAll(e.Message, ".", " ")) {
		candidates = append(candidates, f)
	}
	for _, c := range candidates {
		c = strings.TrimSpace(c)
		var n int64
		var err error
		if strings.HasPrefix(strings.ToLower(c), "0x") {
			n, err = strconv.ParseInt(c[2:], 16, 64)
		} else {
			n, err = strconv.ParseInt(c, 10, 64)
		}
		if err == nil {
			if name, ok := schedulerTypes[n]; ok {
				return name
			}
		}
	}
	return ""
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
