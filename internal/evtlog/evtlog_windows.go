//go:build windows

package evtlog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modwevtapi                   = windows.NewLazySystemDLL("wevtapi.dll")
	procEvtQuery                 = modwevtapi.NewProc("EvtQuery")
	procEvtNext                  = modwevtapi.NewProc("EvtNext")
	procEvtRender                = modwevtapi.NewProc("EvtRender")
	procEvtClose                 = modwevtapi.NewProc("EvtClose")
	procEvtOpenPublisherMetadata = modwevtapi.NewProc("EvtOpenPublisherMetadata")
	procEvtFormatMessage         = modwevtapi.NewProc("EvtFormatMessage")
)

const (
	evtQueryChannelPath      = 0x1
	evtQueryReverseDirection = 0x200
	evtRenderEventXml        = 1
	evtFormatMessageEvent    = 1

	errorNoMoreItems          syscall.Errno = 259
	errorInsufficientBuffer   syscall.Errno = 122
	errorEvtChannelNotFound   syscall.Errno = 15007
	errorEvtMessageNotFound   syscall.Errno = 15027
	errorEvtMessageIDNotFound syscall.Errno = 15028
	errorEvtUnresolvedValue   syscall.Errno = 15029
	errorEvtMaxInserts        syscall.Errno = 15033
	errorResourceTypeNotFound syscall.Errno = 1813
)

// API reads the local event log. Publisher metadata handles are cached, as
// opening them is the expensive part of rendering messages.
type API struct {
	mu         sync.Mutex
	publishers map[string]uintptr
}

// New returns a reader for the local machine.
func New() *API { return &API{publishers: map[string]uintptr{}} }

// Query runs an XPath query against a channel and renders up to max events.
func (a *API) Query(ctx context.Context, channel, xpath string, max int) ([]Event, error) {
	type result struct {
		events []Event
		err    error
	}
	ch := make(chan result, 1)
	go func() {
		ev, err := a.query(channel, xpath, max)
		ch <- result{ev, err}
	}()
	select {
	case r := <-ch:
		return r.events, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (a *API) query(channel, xpath string, max int) ([]Event, error) {
	chanPtr, err := windows.UTF16PtrFromString(channel)
	if err != nil {
		return nil, err
	}
	queryPtr, err := windows.UTF16PtrFromString(xpath)
	if err != nil {
		return nil, err
	}
	h, _, callErr := procEvtQuery.Call(0, uintptr(unsafe.Pointer(chanPtr)), uintptr(unsafe.Pointer(queryPtr)), evtQueryChannelPath|evtQueryReverseDirection)
	if h == 0 {
		if errno, ok := callErr.(syscall.Errno); ok && errno == errorEvtChannelNotFound {
			return nil, fmt.Errorf("%w: %s", ErrNoChannel, channel)
		}
		return nil, fmt.Errorf("EvtQuery %s: %w", channel, callErr)
	}
	defer procEvtClose.Call(h)

	short := shortName(channel)
	var events []Event
	handles := make([]uintptr, 16)
	for len(events) < max {
		var returned uint32
		ok, _, callErr := procEvtNext.Call(h, uintptr(len(handles)), uintptr(unsafe.Pointer(&handles[0])), 5000, 0, uintptr(unsafe.Pointer(&returned)))
		if ok == 0 {
			if errno, isErrno := callErr.(syscall.Errno); isErrno && errno == errorNoMoreItems {
				break
			}
			return events, fmt.Errorf("EvtNext %s: %w", channel, callErr)
		}
		for i := 0; i < int(returned); i++ {
			eh := handles[i]
			if len(events) < max {
				if e, err := a.render(eh); err == nil {
					e.Log, e.Channel = short, channel
					events = append(events, e)
				}
			}
			procEvtClose.Call(eh)
		}
		if returned == 0 {
			break
		}
	}
	return events, nil
}

// render turns an event handle into an Event: XML for the fields, the
// publisher's message table for the text.
func (a *API) render(eh uintptr) (Event, error) {
	var used, props uint32
	procEvtRender.Call(0, eh, evtRenderEventXml, 0, 0, uintptr(unsafe.Pointer(&used)), uintptr(unsafe.Pointer(&props)))
	if used == 0 {
		return Event{}, errors.New("EvtRender: no size")
	}
	buf := make([]uint16, used/2+1)
	ok, _, callErr := procEvtRender.Call(0, eh, evtRenderEventXml, uintptr(used), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&used)), uintptr(unsafe.Pointer(&props)))
	if ok == 0 {
		return Event{}, fmt.Errorf("EvtRender: %w", callErr)
	}
	raw := windows.UTF16ToString(buf)
	e, err := ParseXML(raw, "")
	if err != nil {
		return Event{}, err
	}
	if msg := a.message(e.Provider, eh); msg != "" {
		e.Message = msg
	}
	return e, nil
}

func (a *API) publisher(name string) uintptr {
	a.mu.Lock()
	defer a.mu.Unlock()
	if h, ok := a.publishers[name]; ok {
		return h
	}
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0
	}
	h, _, _ := procEvtOpenPublisherMetadata.Call(0, uintptr(unsafe.Pointer(p)), 0, 0, 0)
	a.publishers[name] = h // 0 is cached too: no point retrying a missing publisher
	return h
}

func (a *API) message(provider string, eh uintptr) string {
	pub := a.publisher(provider)
	if pub == 0 {
		return ""
	}
	var used uint32
	procEvtFormatMessage.Call(pub, eh, 0, 0, 0, evtFormatMessageEvent, 0, 0, uintptr(unsafe.Pointer(&used)))
	if used == 0 {
		return ""
	}
	buf := make([]uint16, used+1)
	ok, _, callErr := procEvtFormatMessage.Call(pub, eh, 0, 0, 0, evtFormatMessageEvent, uintptr(used), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&used)))
	if ok == 0 {
		// Some inserts cannot be resolved; the partial text is still useful.
		if errno, isErrno := callErr.(syscall.Errno); !isErrno || (errno != errorEvtUnresolvedValue && errno != errorEvtMaxInserts) {
			return ""
		}
	}
	return strings.TrimSpace(windows.UTF16ToString(buf))
}

// shortName turns "Microsoft-Windows-Hyper-V-VMMS-Admin" into "vmms".
func shortName(channel string) string {
	s := strings.TrimPrefix(channel, "Microsoft-Windows-Hyper-V-")
	s = strings.TrimSuffix(strings.TrimSuffix(s, "-Admin"), "-Operational")
	return strings.ToLower(s)
}
