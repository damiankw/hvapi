package hyperv

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/damiankw/hvapi/internal/wmi"
)

func TestPreflight(t *testing.T) {
	ok := fixtures()
	if err := Preflight(context.Background(), ok); err != nil {
		t.Fatalf("preflight against fixtures: %v", err)
	}
	if !strings.Contains(ok.called[len(ok.called)-1], "Hosting Computer System") {
		t.Error("preflight should ask for the host entry")
	}

	denied := fixtures()
	denied.fail["Msvm_ComputerSystem"] = wmi.ErrAccessDenied
	err := Preflight(context.Background(), denied)
	if !errors.Is(err, wmi.ErrAccessDenied) {
		t.Fatalf("got %v", err)
	}
	if msg := ExplainPreflight(err, false); !strings.Contains(msg, "Run as administrator") {
		t.Errorf("unelevated advice = %q", msg)
	}
	if msg := ExplainPreflight(err, true); !strings.Contains(msg, "Hyper-V Administrators") || strings.Contains(msg, "Run as administrator") {
		t.Errorf("elevated advice = %q", msg)
	}
	if msg := ExplainPreflight(wmi.ErrNamespaceNotFound, true); !strings.Contains(msg, "Hyper-V role") {
		t.Errorf("namespace advice = %q", msg)
	}
	if msg := ExplainPreflight(wmi.ErrUnavailable, true); !strings.Contains(msg, "not available") {
		t.Errorf("unavailable advice = %q", msg)
	}
	if ExplainPreflight(nil, false) != "" {
		t.Error("nil should explain nothing")
	}
	if err := Preflight(context.Background(), wmi.New()); !errors.Is(err, wmi.ErrUnavailable) {
		t.Errorf("stub client: %v", err)
	}
}
