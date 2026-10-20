//go:build !windows

package evtlog

import "context"

// API is the event log reader; outside Windows it only reports ErrUnavailable.
type API struct{}

// New returns a reader.
func New() *API { return &API{} }

func (a *API) Query(ctx context.Context, channel, xpath string, max int) ([]Event, error) {
	return nil, ErrUnavailable
}
