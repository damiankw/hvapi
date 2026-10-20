package hyperv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"sync"
	"time"

	"github.com/damiankw/hvapi/internal/wmi"
)

// OnDemand serves the two things that are not polled: disk state (which
// opens the VHD) and console thumbnails. Both are allowlisted read-only WMI
// methods with short timeouts, and disk results are cached briefly so a
// chatty client cannot keep the host busy.
type OnDemand struct {
	caller wmi.Caller
	data   func() *Data
	now    func() time.Time

	mu    sync.Mutex
	disks map[string]cachedDisk
}

type cachedDisk struct {
	at    time.Time
	state *DiskState
}

// ErrNotRunning is returned for a thumbnail of a VM that is not running.
var ErrNotRunning = errors.New("the VM is not running")

// ErrOnDemandUnavailable is returned when no WMI caller is wired in.
var ErrOnDemandUnavailable = errors.New("on-demand WMI calls are not available")

const (
	diskCacheTTL  = 30 * time.Second
	methodTimeout = 20 * time.Second
	maxChain      = 64
)

// NewOnDemand wires a caller and the cache reader.
func NewOnDemand(c wmi.Caller, data func() *Data) *OnDemand {
	return &OnDemand{caller: c, data: data, now: time.Now, disks: map[string]cachedDisk{}}
}

var diskTypes = map[string]string{"2": "Fixed", "3": "Dynamic", "4": "Differencing"}
var diskFormats = map[string]string{"2": "VHD", "3": "VHDX", "4": "VHDSet"}

// DiskState implements /vms/{id}/disks/{diskId}/state.
func (o *OnDemand) DiskState(ctx context.Context, vmKey, diskID string) (*DiskState, error) {
	if o == nil || o.caller == nil {
		return nil, ErrOnDemandUnavailable
	}
	d := o.data()
	id, err := d.Resolve(vmKey)
	if err != nil {
		return nil, err
	}
	disks, err := d.Disks(id)
	if err != nil {
		return nil, err
	}
	var drive *Drive
	for _, c := range disks.Controllers {
		for i := range c.Drives {
			if strings.EqualFold(c.Drives[i].DiskID, diskID) {
				drive = &c.Drives[i]
			}
		}
	}
	if drive == nil {
		return nil, fmt.Errorf("%w: no drive %q on this VM (see /vms/{id}/disks for disk_id values)", ErrNotFound, diskID)
	}
	if drive.Media != "VHD" || drive.Path == "" {
		return nil, fmt.Errorf("%w: drive %q has no virtual hard disk attached (%s)", ErrNotFound, diskID, drive.Media)
	}
	svc, err := d.servicePath("image_service")
	if err != nil {
		return nil, err
	}

	o.mu.Lock()
	if c, ok := o.disks[strings.ToLower(drive.Path)]; ok && o.now().Sub(c.at) < diskCacheTTL {
		o.mu.Unlock()
		out := *c.state
		out.VMID, out.VMName, out.DiskID = id, disks.VMName, drive.DiskID
		return &out, nil
	}
	o.mu.Unlock()

	out := &DiskState{Path: drive.Path, CheckedAt: o.now().UTC(), Chain: []DiskLink{}}
	ctx, cancel := context.WithTimeout(ctx, methodTimeout)
	defer cancel()

	path := drive.Path
	seen := map[string]bool{}
	for i := 0; i < maxChain && path != "" && !seen[strings.ToLower(path)]; i++ {
		seen[strings.ToLower(path)] = true
		link, err := o.diskSettings(ctx, svc, path)
		if err != nil {
			if i == 0 {
				return nil, fmt.Errorf("GetVirtualHardDiskSettingData for %s: %w", path, err)
			}
			out.Chain[i-1].ParentMissing = true
			out.ChainBroken = true
			out.Chain = append(out.Chain, DiskLink{Path: path, Type: "Unknown", Format: "Unknown", Error: err.Error()})
			break
		}
		out.Chain = append(out.Chain, link)
		path = link.ParentPath
	}
	if state, err := o.diskState(ctx, svc, drive.Path); err != nil {
		out.Notes = append(out.Notes, "GetVirtualHardDiskState failed: "+err.Error())
	} else {
		out.FileSizeMB, out.MinSizeMB, out.InUse, out.Fragmentation, out.Alignment = state.FileSizeMB, state.MinSizeMB, state.InUse, state.Fragmentation, state.Alignment
	}

	o.mu.Lock()
	o.disks[strings.ToLower(drive.Path)] = cachedDisk{at: o.now(), state: out}
	o.mu.Unlock()
	res := *out
	res.VMID, res.VMName, res.DiskID = id, disks.VMName, drive.DiskID
	return &res, nil
}

func (o *OnDemand) diskSettings(ctx context.Context, svc, path string) (DiskLink, error) {
	out, err := o.caller.Call(ctx, wmi.NSVirtualization, svc, "GetVirtualHardDiskSettingData", map[string]any{"Path": path})
	if err != nil {
		return DiskLink{}, err
	}
	if rv := out.Int("ReturnValue"); rv != 0 {
		return DiskLink{}, fmt.Errorf("return value %d", rv)
	}
	props := ParseCIMXML(out.String("SettingData"))
	if props == nil {
		return DiskLink{}, errors.New("no SettingData in the reply")
	}
	l := DiskLink{Path: props["Path"], ParentPath: props["ParentPath"]}
	if l.Path == "" {
		l.Path = path
	}
	l.Type = lookupStr(diskTypes, props["Type"], "Unknown")
	l.Format = lookupStr(diskFormats, props["Format"], "Unknown")
	l.MaxSizeMB = MB(wmi.Row{"v": props["MaxInternalSize"]}.Int("v"))
	l.BlockSizeKB = wmi.Row{"v": props["BlockSize"]}.Int("v") / 1024
	l.LogicalSector = wmi.Row{"v": props["LogicalSectorSize"]}.Int("v")
	l.PhysicalSector = wmi.Row{"v": props["PhysicalSectorSize"]}.Int("v")
	return l, nil
}

func (o *OnDemand) diskState(ctx context.Context, svc, path string) (*DiskState, error) {
	out, err := o.caller.Call(ctx, wmi.NSVirtualization, svc, "GetVirtualHardDiskState", map[string]any{"Path": path})
	if err != nil {
		return nil, err
	}
	if rv := out.Int("ReturnValue"); rv != 0 {
		return nil, fmt.Errorf("return value %d", rv)
	}
	props := ParseCIMXML(out.String("State"))
	if props == nil {
		return nil, errors.New("no State in the reply")
	}
	r := wmi.Row{}
	for k, v := range props {
		r[k] = v
	}
	st := &DiskState{}
	if r.Has("FileSize") {
		v := MB(r.Int("FileSize"))
		st.FileSizeMB = &v
	}
	if r.Has("MinInternalSize") {
		v := MB(r.Int("MinInternalSize"))
		st.MinSizeMB = &v
	}
	st.InUse = boolPtr(r, "InUse")
	st.Fragmentation = intPtr(r, "FragmentationPercentage")
	st.Alignment = intPtr(r, "Alignment")
	return st, nil
}

func lookupStr(m map[string]string, key, def string) string {
	if v, ok := m[key]; ok {
		return v
	}
	return def
}

// Thumbnail implements /vms/{id}/thumbnail: a PNG of the VM's console.
func (o *OnDemand) Thumbnail(ctx context.Context, vmKey string, width, height int) ([]byte, error) {
	if o == nil || o.caller == nil {
		return nil, ErrOnDemandUnavailable
	}
	d := o.data()
	id, err := d.Resolve(vmKey)
	if err != nil {
		return nil, err
	}
	if d.State.vms[id].cs.Int("EnabledState") != 2 {
		return nil, ErrNotRunning
	}
	set := d.settingsFor(id)
	if set == nil {
		return nil, ErrNotReady
	}
	svc, err := d.servicePath("management_service")
	if err != nil {
		return nil, err
	}
	if width <= 0 || width > 1920 {
		width = 320
	}
	if height <= 0 || height > 1200 {
		height = 240
	}
	ctx, cancel := context.WithTimeout(ctx, methodTimeout)
	defer cancel()
	out, err := o.caller.Call(ctx, wmi.NSVirtualization, svc, "GetVirtualSystemThumbnailImage", map[string]any{
		"TargetSystem": set.vssd.String(wmi.PathKey), "WidthPixels": width, "HeightPixels": height,
	})
	if err != nil {
		return nil, err
	}
	if rv := out.Int("ReturnValue"); rv != 0 {
		return nil, fmt.Errorf("GetVirtualSystemThumbnailImage returned %d", rv)
	}
	raw := bytesOf(out["ImageData"])
	if len(raw) < width*height*2 {
		return nil, fmt.Errorf("thumbnail data is %d bytes, expected %d", len(raw), width*height*2)
	}
	return rgb565ToPNG(raw, width, height)
}

// bytesOf accepts the shapes a byte array can arrive in from COM.
func bytesOf(v any) []byte {
	switch b := v.(type) {
	case []byte:
		return b
	case []any:
		out := make([]byte, 0, len(b))
		for _, e := range b {
			switch n := e.(type) {
			case uint8:
				out = append(out, n)
			case int8:
				out = append(out, byte(n))
			default:
				x, _ := toInt(n)
				out = append(out, byte(x))
			}
		}
		return out
	}
	return nil
}

func toInt(v any) (int64, bool) { return wmi.Row{"v": v}.Int("v"), true }

// rgb565ToPNG converts the 16-bit RGB565 little-endian pixels the method
// returns into a PNG.
func rgb565ToPNG(raw []byte, width, height int) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			i := (y*width + x) * 2
			v := uint16(raw[i]) | uint16(raw[i+1])<<8
			r := uint8((v >> 11) & 0x1f)
			g := uint8((v >> 5) & 0x3f)
			b := uint8(v & 0x1f)
			img.SetRGBA(x, y, color.RGBA{R: r<<3 | r>>2, G: g<<2 | g>>4, B: b<<3 | b>>2, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
