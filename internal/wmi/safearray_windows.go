//go:build windows

package wmi

import (
	"unsafe"

	"github.com/go-ole/go-ole"
	"golang.org/x/sys/windows"
)

var (
	modoleaut32             = windows.NewLazySystemDLL("oleaut32.dll")
	procSafeArrayGetElement = modoleaut32.NewProc("SafeArrayGetElement")
)

// arrayToGo converts a SAFEARRAY variant. go-ole handles arrays of scalars
// and strings; arrays of embedded objects (VT_UNKNOWN, VT_DISPATCH) and of
// VARIANTs are read element by element here so the objects stay alive while
// they are copied into Rows.
func arrayToGo(v *ole.VARIANT) any {
	sa := v.ToArray()
	if sa == nil || sa.Array == nil {
		return nil
	}
	vt, err := sa.GetType()
	if err != nil {
		return nil
	}
	n, err := sa.TotalElements(0)
	if err != nil || n < 0 {
		return nil
	}
	out := make([]any, 0, n)
	switch ole.VT(vt) {
	case ole.VT_UNKNOWN, ole.VT_DISPATCH:
		for i := int32(0); i < n; i++ {
			var unk *ole.IUnknown // AddRef'd by SafeArrayGetElement
			if hr, _, _ := procSafeArrayGetElement.Call(uintptr(unsafe.Pointer(sa.Array)), uintptr(unsafe.Pointer(&i)), uintptr(unsafe.Pointer(&unk))); hr != 0 || unk == nil {
				out = append(out, nil)
				continue
			}
			disp, err := unk.QueryInterface(ole.IID_IDispatch)
			unk.Release()
			if err != nil {
				out = append(out, nil)
				continue
			}
			row, _ := objectToRow(disp)
			disp.Release()
			out = append(out, row)
		}
	case ole.VT_VARIANT:
		for i := int32(0); i < n; i++ {
			var elem ole.VARIANT
			ole.VariantInit(&elem)
			if hr, _, _ := procSafeArrayGetElement.Call(uintptr(unsafe.Pointer(sa.Array)), uintptr(unsafe.Pointer(&i)), uintptr(unsafe.Pointer(&elem))); hr != 0 {
				out = append(out, nil)
				continue
			}
			out = append(out, variantToGo(&elem))
			elem.Clear()
		}
	default:
		for _, e := range sa.ToValueArray() {
			out = append(out, e)
		}
	}
	return out
}
