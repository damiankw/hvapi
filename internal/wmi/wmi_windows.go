//go:build windows

package wmi

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

// Client talks to the local WMI service through the scripting API
// (WbemScripting.SWbemLocator) over COM. Each call runs on its own
// OS-thread-locked goroutine with its own COM apartment, which keeps COM
// rules simple and lets a caller time out without holding anything.
type Client struct{}

// New returns a Client for the local host.
func New() *Client { return &Client{} }

const (
	wbemFlagReturnImmediately = 0x10
	wbemFlagForwardOnly       = 0x20
	sFalse                    = 0x00000001
	wbemErrInvalidClass       = 0x80041010
	wbemErrInvalidNamespace   = 0x8004100E
	wbemErrInvalidQuery       = 0x80041017
	wbemErrAccessDenied       = 0x80041003
	eAccessDenied             = 0x80070005
)

// Query runs a WQL read in the namespace and returns every instance.
func (c *Client) Query(ctx context.Context, namespace, wql string) ([]Row, error) {
	if err := CheckQuery(wql); err != nil {
		return nil, err
	}
	type result struct {
		rows []Row
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		rows, err := execQuery(namespace, wql)
		ch <- result{rows, err}
	}()
	select {
	case r := <-ch:
		return r.rows, r.err
	case <-ctx.Done():
		// The COM call keeps running on its own thread until WMI returns;
		// its result is dropped.
		return nil, fmt.Errorf("wmi query %q: %w", wql, ctx.Err())
	}
}

// Call invokes an allowlisted read-only method on the object at objectPath.
// in maps parameter names to values (string, []string, int, bool...).
// The out parameters are returned as a Row, including ReturnValue.
func (c *Client) Call(ctx context.Context, namespace, objectPath, method string, in map[string]any) (Row, error) {
	if err := CheckMethod(method); err != nil {
		return nil, err
	}
	type result struct {
		row Row
		err error
	}
	ch := make(chan result, 1)
	go func() {
		row, err := execMethod(namespace, objectPath, method, in)
		ch <- result{row, err}
	}()
	select {
	case r := <-ch:
		return r.row, r.err
	case <-ctx.Done():
		return nil, fmt.Errorf("wmi method %s: %w", method, ctx.Err())
	}
}

// withServices runs f with a connected SWbemServices on a COM-initialised,
// thread-locked goroutine.
func withServices(namespace string, f func(svc *ole.IDispatch) error) (err error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
		var oe *ole.OleError
		if !errors.As(err, &oe) || (oe.Code() != ole.S_OK && oe.Code() != sFalse) {
			return fmt.Errorf("CoInitializeEx: %w", err)
		}
	}
	defer ole.CoUninitialize()

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("wmi: panic: %v", r)
		}
	}()

	unknown, err := oleutil.CreateObject("WbemScripting.SWbemLocator")
	if err != nil {
		return fmt.Errorf("create SWbemLocator: %w", err)
	}
	defer unknown.Release()
	locator, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return fmt.Errorf("SWbemLocator IDispatch: %w", err)
	}
	defer locator.Release()

	svcV, err := oleutil.CallMethod(locator, "ConnectServer", ".", namespace)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", namespace, translate(err))
	}
	defer svcV.Clear()
	return f(svcV.ToIDispatch())
}

func execQuery(namespace, wql string) ([]Row, error) {
	var rows []Row
	err := withServices(namespace, func(svc *ole.IDispatch) error {
		resV, err := oleutil.CallMethod(svc, "ExecQuery", wql, "WQL", wbemFlagReturnImmediately|wbemFlagForwardOnly)
		if err != nil {
			return fmt.Errorf("ExecQuery %q: %w", wql, translate(err))
		}
		defer resV.Clear()
		return oleutil.ForEach(resV.ToIDispatch(), func(v *ole.VARIANT) error {
			defer v.Clear()
			row, err := objectToRow(v.ToIDispatch())
			if err != nil {
				return err
			}
			rows = append(rows, row)
			return nil
		})
	})
	return rows, err
}

func execMethod(namespace, objectPath, method string, in map[string]any) (Row, error) {
	var out Row
	err := withServices(namespace, func(svc *ole.IDispatch) error {
		objV, err := oleutil.CallMethod(svc, "Get", objectPath)
		if err != nil {
			return fmt.Errorf("get %s: %w", objectPath, translate(err))
		}
		defer objV.Clear()
		obj := objV.ToIDispatch()

		methodsV, err := oleutil.GetProperty(obj, "Methods_")
		if err != nil {
			return err
		}
		defer methodsV.Clear()
		mV, err := oleutil.CallMethod(methodsV.ToIDispatch(), "Item", method)
		if err != nil {
			return fmt.Errorf("method %s: %w", method, translate(err))
		}
		defer mV.Clear()

		var inParams *ole.IDispatch
		inClassV, err := oleutil.GetProperty(mV.ToIDispatch(), "InParameters")
		if err != nil {
			return err
		}
		defer inClassV.Clear()
		if inClassV.VT == ole.VT_DISPATCH && inClassV.ToIDispatch() != nil {
			instV, err := oleutil.CallMethod(inClassV.ToIDispatch(), "SpawnInstance_")
			if err != nil {
				return err
			}
			defer instV.Clear()
			inParams = instV.ToIDispatch()
			for name, value := range in {
				if _, err := oleutil.PutProperty(inParams, name, value); err != nil {
					return fmt.Errorf("set parameter %s: %w", name, translate(err))
				}
			}
		}

		var outV *ole.VARIANT
		if inParams != nil {
			outV, err = oleutil.CallMethod(obj, "ExecMethod_", method, inParams)
		} else {
			outV, err = oleutil.CallMethod(obj, "ExecMethod_", method)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", method, translate(err))
		}
		defer outV.Clear()
		out, err = objectToRow(outV.ToIDispatch())
		return err
	})
	return out, err
}

// objectToRow copies every property of an SWbemObject into a Row.
func objectToRow(obj *ole.IDispatch) (Row, error) {
	row := Row{}
	if obj == nil {
		return row, nil
	}
	if pathV, err := oleutil.GetProperty(obj, "Path_"); err == nil {
		if pathV.VT == ole.VT_DISPATCH && pathV.ToIDispatch() != nil {
			if pV, err := oleutil.GetProperty(pathV.ToIDispatch(), "Path"); err == nil {
				row[PathKey] = pV.ToString()
				pV.Clear()
			}
		}
		pathV.Clear()
	}

	propsV, err := oleutil.GetProperty(obj, "Properties_")
	if err != nil {
		return nil, fmt.Errorf("Properties_: %w", err)
	}
	defer propsV.Clear()
	err = oleutil.ForEach(propsV.ToIDispatch(), func(pv *ole.VARIANT) error {
		defer pv.Clear()
		prop := pv.ToIDispatch()
		nameV, err := oleutil.GetProperty(prop, "Name")
		if err != nil {
			return err
		}
		defer nameV.Clear()
		valV, err := oleutil.GetProperty(prop, "Value")
		if err != nil {
			return err
		}
		defer valV.Clear()
		row[nameV.ToString()] = variantToGo(valV)
		return nil
	})
	return row, err
}

// variantToGo converts a VARIANT to the Go representation used in Row.
func variantToGo(v *ole.VARIANT) any {
	switch {
	case v == nil, v.VT == ole.VT_NULL, v.VT == ole.VT_EMPTY:
		return nil
	case v.VT&ole.VT_ARRAY != 0:
		return arrayToGo(v)
	case v.VT == ole.VT_DISPATCH:
		disp := v.ToIDispatch()
		if disp == nil {
			return nil
		}
		row, _ := objectToRow(disp)
		return row
	case v.VT == ole.VT_UNKNOWN:
		unk := v.ToIUnknown()
		if unk == nil {
			return nil
		}
		disp, err := unk.QueryInterface(ole.IID_IDispatch)
		if err != nil {
			return nil
		}
		defer disp.Release()
		row, _ := objectToRow(disp)
		return row
	case v.VT == ole.VT_BSTR:
		return v.ToString()
	default:
		return v.Value()
	}
}

// translate turns well-known WMI HRESULTs into sentinel errors.
func translate(err error) error {
	var oe *ole.OleError
	if errors.As(err, &oe) {
		switch uint32(oe.Code()) {
		case wbemErrInvalidClass:
			return fmt.Errorf("%w (%s)", ErrInvalidClass, err)
		case wbemErrInvalidNamespace:
			return fmt.Errorf("%w (%s)", ErrNamespaceNotFound, err)
		case wbemErrInvalidQuery:
			return fmt.Errorf("invalid query (%s)", err)
		case wbemErrAccessDenied, eAccessDenied:
			return fmt.Errorf("%w (%s)", ErrAccessDenied, err)
		}
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "Invalid class"):
		return fmt.Errorf("%w (%s)", ErrInvalidClass, err)
	case strings.Contains(msg, "Invalid namespace"):
		return fmt.Errorf("%w (%s)", ErrNamespaceNotFound, err)
	case strings.Contains(msg, "Access denied"), strings.Contains(msg, "access is denied"):
		return fmt.Errorf("%w (%s)", ErrAccessDenied, err)
	}
	return err
}
