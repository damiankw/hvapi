// Package evtlog reads Windows event logs through the Windows Event Log API
// (wevtapi), read-only: EvtQuery, EvtNext, EvtRender and EvtFormatMessage.
package evtlog

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Event is one log record.
type Event struct {
	Log       string    `json:"log" doc:"Short log name, for example vmms or worker."`
	Channel   string    `json:"channel" doc:"Full channel name."`
	RecordID  int64     `json:"record_id"`
	ID        int       `json:"id" doc:"Event ID."`
	Level     string    `json:"level" doc:"Critical, Error, Warning, Information or Verbose."`
	LevelCode int       `json:"level_code"`
	Provider  string    `json:"provider"`
	Time      time.Time `json:"time" doc:"UTC."`
	Computer  string    `json:"computer,omitempty"`
	Message   string    `json:"message" doc:"The rendered message; falls back to the event data joined with spaces when the publisher's message table is unavailable."`
	Data      []string  `json:"data,omitempty" doc:"The event's data values, in order."`
	VMID      string    `json:"vm_id,omitempty" doc:"The VM the event mentions, when one can be identified from the data."`
}

// ErrUnavailable is returned where the event log API does not exist.
var ErrUnavailable = errors.New("the Windows event log is only available on Windows")

// Reader reads events; Windows has the real one, tests fake it.
type Reader interface {
	// Query returns up to max events from channel matching the XPath
	// query, newest first. A missing channel is an error that wraps
	// ErrNoChannel.
	Query(ctx context.Context, channel, xpath string, max int) ([]Event, error)
}

// ErrNoChannel means the channel does not exist on this host.
var ErrNoChannel = errors.New("event channel not found")

// Levels maps event level codes to names.
var Levels = map[int]string{0: "Information", 1: "Critical", 2: "Error", 3: "Warning", 4: "Information", 5: "Verbose"}

// SinceQuery is the XPath for events in the last d.
func SinceQuery(d time.Duration) string {
	return fmt.Sprintf("*[System[TimeCreated[timediff(@SystemTime) <= %d]]]", d.Milliseconds())
}

// renderedEvent is the XML EvtRender produces.
type renderedEvent struct {
	System struct {
		Provider struct {
			Name string `xml:"Name,attr"`
		} `xml:"Provider"`
		EventID     string `xml:"EventID"`
		Level       string `xml:"Level"`
		TimeCreated struct {
			SystemTime string `xml:"SystemTime,attr"`
		} `xml:"TimeCreated"`
		EventRecordID string `xml:"EventRecordID"`
		Channel       string `xml:"Channel"`
		Computer      string `xml:"Computer"`
	} `xml:"System"`
	EventData struct {
		Data []struct {
			Name  string `xml:"Name,attr"`
			Value string `xml:",chardata"`
		} `xml:"Data"`
	} `xml:"EventData"`
	UserData struct {
		Inner []byte `xml:",innerxml"`
	} `xml:"UserData"`
}

// ParseXML turns rendered event XML into an Event. message may be empty, in
// which case the data values are joined as the message.
func ParseXML(raw, message string) (Event, error) {
	var r renderedEvent
	if err := xml.Unmarshal([]byte(raw), &r); err != nil {
		return Event{}, err
	}
	e := Event{Channel: r.System.Channel, Provider: r.System.Provider.Name, Computer: r.System.Computer}
	e.ID, _ = strconv.Atoi(strings.TrimSpace(r.System.EventID))
	e.LevelCode, _ = strconv.Atoi(strings.TrimSpace(r.System.Level))
	e.Level = Levels[e.LevelCode]
	if e.Level == "" {
		e.Level = "Information"
	}
	e.RecordID, _ = strconv.ParseInt(strings.TrimSpace(r.System.EventRecordID), 10, 64)
	if t, err := time.Parse(time.RFC3339Nano, r.System.TimeCreated.SystemTime); err == nil {
		e.Time = t.UTC()
	}
	for _, d := range r.EventData.Data {
		e.Data = append(e.Data, strings.TrimSpace(d.Value))
	}
	if len(e.Data) == 0 && len(r.UserData.Inner) > 0 {
		e.Data = userDataValues(string(r.UserData.Inner))
	}
	e.Message = strings.TrimSpace(message)
	if e.Message == "" {
		e.Message = strings.Join(e.Data, " ")
	}
	for _, d := range e.Data {
		if isGUID(d) {
			e.VMID = strings.ToUpper(d)
			break
		}
	}
	if e.VMID == "" {
		if g := findGUID(e.Message); g != "" {
			e.VMID = g
		}
	}
	return e, nil
}

// userDataValues pulls the text of every leaf element out of a UserData
// fragment, whose element names depend on the provider.
func userDataValues(inner string) []string {
	dec := xml.NewDecoder(strings.NewReader("<r>" + inner + "</r>"))
	var out []string
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if cd, ok := tok.(xml.CharData); ok {
			if s := strings.TrimSpace(string(cd)); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func isGUID(s string) bool {
	s = strings.Trim(s, "{}")
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}

// findGUID returns the first GUID in s, upper-cased, or "".
func findGUID(s string) string {
	for i := 0; i+36 <= len(s); i++ {
		if isGUID(s[i : i+36]) {
			return strings.ToUpper(s[i : i+36])
		}
	}
	return ""
}
