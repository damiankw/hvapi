package hyperv

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/damiankw/hvapi/internal/wmi"
)

// Preflight checks that Hyper-V data can be read at all, before the poller
// starts, so a permissions problem is reported once and clearly instead of
// as an empty API. The query asks for the host's own entry, which every
// Hyper-V host has.
func Preflight(ctx context.Context, q wmi.Querier) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err := q.Query(ctx, wmi.NSVirtualization, "SELECT Name FROM Msvm_ComputerSystem WHERE Caption = 'Hosting Computer System'")
	return err
}

// ExplainPreflight turns a Preflight error into advice for the operator.
// It returns "" for nil.
func ExplainPreflight(err error, elevated bool) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, wmi.ErrUnavailable):
		return "WMI is not available on this operating system; the API will serve empty data."
	case errors.Is(err, wmi.ErrAccessDenied) && !elevated:
		return "Hyper-V WMI refused access and this process is not elevated. Start it from an elevated prompt (right-click, Run as administrator), or run it as a member of the Hyper-V Administrators group."
	case errors.Is(err, wmi.ErrAccessDenied):
		return "Hyper-V WMI refused access. The account needs to be a member of Hyper-V Administrators (or Administrators). For the service, check which account it runs as."
	case errors.Is(err, wmi.ErrNamespaceNotFound):
		return "The root\\virtualization\\v2 namespace does not exist. Is the Hyper-V role installed on this machine?"
	case errors.Is(err, context.DeadlineExceeded):
		return "WMI did not answer within 30 seconds. Check that the Windows Management Instrumentation and Hyper-V Virtual Machine Management services are running."
	default:
		return fmt.Sprintf("Hyper-V WMI could not be read: %v", err)
	}
}
