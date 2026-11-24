//go:build windows

package main

import (
	"encoding/json"
	"hoverdict/internal/bubblegeom"
	"hoverdict/internal/screenlayout"
	"hoverdict/internal/windowgeom"
	"os"
	"path/filepath"
	"unsafe"
)

const (
	wmExitSizeMove   = 0x0232
	wmSize           = 0x0005
	wmSetCursor      = 0x0020
	wmGetMinMaxInfo  = 0x0024
	wmCaptureChanged = 0x0215
	wmNCHitTest      = 0x0084
	wmMouseLeave     = 0x02A3
	wmMouseMove      = 0x0200
	wmLeftButtonUp   = 0x0202
	wmRightButtonUp  = 0x0205
	wmMouseWheel     = 0x020A
	wThickFrame      = 0x00040000 // WS_THICKFRAME makes the panel resize using a mouse
	swpNoSize        = 0x0001
	swpNoZOrder      = 0x0004
	swpNoActivate    = 0x0010
	swpFrameChanged  = 0x0020
	wExLayered       = 0x00080000
	wExToolWindow    = 0x00000080
	lwaColorKey      = 0x00000001
)

type winPoint struct{ X, Y int32 }
type winRect struct{ Left, Top, Right, Bottom int32 }
type winMinMax struct{ Reserved, MaxSize, MaxPosition, MinTrack, MaxTrack winPoint }
type layoutPreferences struct {
	PanelX             int32 `json:"panel_x"`
	PanelY             int32 `json:"panel_y"`
	PanelW             int32 `json:"panel_w"`
	PanelH             int32 `json:"panel_h"`
	BubbleX            int32 `json:"bubble_x"`
	BubbleY            int32 `json:"bubble_y"`
	BubbleSize         int   `json:"bubble_size"`
	UIRevision         int   `json:"ui_revision"`
	Initialized        bool  `json:"initialized"`
	StartupInitialized bool  `json:"startup_initialized"`
}

const (
	dragNone = iota
	dragMove
	dragResize
)

var (
	layoutFile   string
	layoutState  = layoutPreferences{}
	dragging     bool
	didDrag      bool
	dragKind     int
	hoverGrip    bool
	dragOrigin   winPoint
	windowOrigin winPoint
	initialSize  int
)

func fixedBubbleSize() int { return dpi(44) }

func cursorPoint() winPoint {
	var p winPoint
	call(u32.NewProc("GetCursorPos"), uintptr(unsafe.Pointer(&p)))
	return p
}
func currentWindowRect() winRect {
	var r winRect
	if window != 0 {
		call(u32.NewProc("GetWindowRect"), window, uintptr(unsafe.Pointer(&r)))
	}
	return r
}
func screenBounds() (int32, int32) {
	w := int32(call(u32.NewProc("GetSystemMetrics"), 0))
	h := int32(call(u32.NewProc("GetSystemMetrics"), 1))
	if w < 640 {
		w = 1280
	}
	if h < 480 {
		h = 800
	}
	return w, h
}

// Virtual-screen metrics include monitors placed left of or above the primary.
func virtualDesktop() screenlayout.Rect {
	x := int32(call(u32.NewProc("GetSystemMetrics"), 76)) // SM_XVIRTUALSCREEN
	y := int32(call(u32.NewProc("GetSystemMetrics"), 77)) // SM_YVIRTUALSCREEN
	w := int32(call(u32.NewProc("GetSystemMetrics"), 78)) // SM_CXVIRTUALSCREEN
	h := int32(call(u32.NewProc("GetSystemMetrics"), 79)) // SM_CYVIRTUALSCREEN
	if w <= 0 || h <= 0 {
		pw, ph := screenBounds()
		return screenlayout.Rect{X: 0, Y: 0, W: pw, H: ph}
	}
	return screenlayout.Rect{X: x, Y: y, W: w, H: h}
}
func clamp32(v, lo, hi int32) int32 {
	if hi < lo {
		hi = lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
func loadLayout(dir string) {
	layoutFile = filepath.Join(dir, "layout.json")
	if raw, err := os.ReadFile(layoutFile); err == nil {
		_ = json.Unmarshal(raw, &layoutState)
	}
	// SQLite is authoritative after first write; JSON is retained as an
	// independent, readable recovery copy for older installations.
	if userDB != nil {
		if raw, err := userDB.getSetting("layout_json"); err == nil && raw != "" {
			var stored layoutPreferences
			if json.Unmarshal([]byte(raw), &stored) == nil {
				layoutState = stored
			}
		}
	}
	minW, minH := int32(dpi(336)), int32(dpi(346))
	if layoutState.PanelW < minW || layoutState.PanelW > int32(dpi(1250)) {
		layoutState.PanelW = int32(dpi(368))
	}
	if layoutState.PanelH < minH || layoutState.PanelH > int32(dpi(1200)) {
		layoutState.PanelH = int32(dpi(365))
	}
	priorRevision := layoutState.UIRevision
	// V4.2: reduce the oversized default panel seen on high-DPI displays.
	// Never resize a user-customized panel; only migrate the historic defaults.
	if priorRevision < 42 {
		oldDefaultW, oldDefaultH := int32(dpi(416)), int32(dpi(488))
		if abs32(layoutState.PanelW-oldDefaultW) <= int32(dpi(4)) && abs32(layoutState.PanelH-oldDefaultH) <= int32(dpi(4)) {
			layoutState.PanelW, layoutState.PanelH = int32(dpi(384)), int32(dpi(366))
		}
		layoutState.UIRevision = 42
	}
	// One-time V4.3 visual cleanup: older high-DPI panel dimensions can be
	// enormous. Reset only old oversized layouts, preserving the pinned X/Y
	// and always retaining saved words/preferences. Later user resizes persist.
	if priorRevision < 43 {
		if layoutState.PanelW > int32(dpi(380)) || layoutState.PanelH > int32(dpi(405)) {
			layoutState.PanelW = int32(dpi(368))
			layoutState.PanelH = int32(dpi(365))
		}
		layoutState.UIRevision = 43
	}
	// V5.3: keep the mascot bubble fixed, small, and non-resizable.
	layoutState.BubbleSize = fixedBubbleSize()
	sw, sh := screenBounds()
	if !layoutState.Initialized {
		layoutState.PanelX = sw - layoutState.PanelW - int32(dpi(36))
		layoutState.PanelY = int32(dpi(120))
		layoutState.BubbleX = sw - int32(layoutState.BubbleSize) - int32(dpi(24))
		layoutState.BubbleY = sh / 3
	}
	// Preserve intentionally negative coordinates on multi-monitor desktops;
	// only reset windows that have become effectively unreachable.
	virtual := virtualDesktop()
	minVisible := int32(dpi(20))
	if !screenlayout.VisibleAtLeast(screenlayout.Rect{X: layoutState.PanelX, Y: layoutState.PanelY, W: layoutState.PanelW, H: layoutState.PanelH}, virtual, minVisible) {
		layoutState.PanelX = sw - layoutState.PanelW - int32(dpi(24))
		layoutState.PanelY = int32(dpi(80))
	}
	if !screenlayout.VisibleAtLeast(screenlayout.Rect{X: layoutState.BubbleX, Y: layoutState.BubbleY, W: int32(layoutState.BubbleSize), H: int32(layoutState.BubbleSize)}, virtual, minVisible) {
		layoutState.BubbleX = sw - int32(layoutState.BubbleSize) - int32(dpi(20))
		layoutState.BubbleY = sh / 3
	}
}
func persistLayout() error {
	if window != 0 {
		r := currentWindowRect()
		if expanded {
			layoutState.PanelX, layoutState.PanelY = r.Left, r.Top
			if r.Right > r.Left && r.Bottom > r.Top {
				layoutState.PanelW = r.Right - r.Left
				layoutState.PanelH = r.Bottom - r.Top
			}
		} else {
			layoutState.BubbleX, layoutState.BubbleY = r.Left, r.Top
		}
		layoutState.Initialized = true
	}
	b, err := json.MarshalIndent(layoutState, "", "  ")
	if err != nil {
		return err
	}
	// Commit primary SQLite setting before updating the backward-compatible
	// JSON backup. After a crash, SQLite will never be older than the backup.
	if userDB != nil {
		if err := userDB.setSetting("layout_json", string(b)); err != nil {
			return err
		}
	}
	temp := layoutFile + ".tmp"
	if err = os.WriteFile(temp, b, 0600); err != nil {
		return err
	}
	return os.Rename(temp, layoutFile)
}
func saveLayout() {
	if layoutFile != "" && window != 0 {
		_ = persistLayout()
	}
}
func windowStartPosition() (int32, int32) { return layoutState.PanelX, layoutState.PanelY }
func positionFor(exp bool) (int32, int32) {
	if exp {
		return layoutState.PanelX, layoutState.PanelY
	}
	return layoutState.BubbleX, layoutState.BubbleY
}

// The layered colour key removes every pixel outside the painted round face;
// the elliptic region also ensures corners cannot intercept mouse clicks.
// The previous region-only approach left DWM's native square shadow/border on
// some Windows versions, which looked like a rounded-square badge.
func refreshBubbleShape() {
	if window == 0 || expanded {
		return
	}
	var client uiRect
	if call(u32.NewProc("GetClientRect"), window, uintptr(unsafe.Pointer(&client))) == 0 {
		return
	}
	w, h := client.Right-client.Left, client.Bottom-client.Top
	if w <= 0 || h <= 0 {
		return
	}
	region := call(gdi.NewProc("CreateEllipticRgn"), 0, 0, uintptr(w), uintptr(h))
	if region != 0 {
		if ok := call(u32.NewProc("SetWindowRgn"), window, region, 1); ok == 0 {
			call(gdi.NewProc("DeleteObject"), region)
			logDiagnostic("bubble_region", "SetWindowRgn failed")
		}
	} else {
		logDiagnostic("bubble_region", "CreateEllipticRgn failed")
	}
	call(u32.NewProc("InvalidateRect"), window, 0, 1)
}

// Turn off nonclient rendering on the mascot, removing the DWM border/shadow.
// The expanded panel retains Windows 11's normal decorated rounded frame.
func configureWindowComposition(exp bool) {
	proc := dwm.NewProc("DwmSetWindowAttribute")
	if proc.Find() != nil {
		return
	}
	ncPolicy, corner, border := uint32(1), uint32(1), uint32(0xFFFFFFFE)
	if exp {
		ncPolicy, corner, border = 0, 2, 0xFFFFFFFF
	}
	proc.Call(window, 2, uintptr(unsafe.Pointer(&ncPolicy)), 4) // DWMWA_NCRENDERING_POLICY
	proc.Call(window, 33, uintptr(unsafe.Pointer(&corner)), 4)  // DWMWA_WINDOW_CORNER_PREFERENCE
	proc.Call(window, 34, uintptr(unsafe.Pointer(&border)), 4)  // DWMWA_BORDER_COLOR
}

func changeBubbleSize(step int) {
	// Bubble resizing is intentionally disabled in V5.3.
	return
}
func bubbleHandleResize(x, y, size int) bool {
	// Bubble resizing is disabled; only the main panel can be resized.
	return false
}
func beginBubbleDrag(lp uintptr) {
	if expanded {
		return
	}
	dragging, didDrag = true, false
	dragOrigin = cursorPoint()
	r := currentWindowRect()
	windowOrigin = winPoint{r.Left, r.Top}
	initialSize = layoutState.BubbleSize
	x, y := int(int16(uint16(lp))), int(int16(uint16(lp>>16)))
	dragKind = dragMove
	_ = x
	_ = y
	call(u32.NewProc("SetCapture"), window)
}
func moveBubble() {
	if !dragging {
		return
	}
	p := cursorPoint()
	dx, dy := int(p.X-dragOrigin.X), int(p.Y-dragOrigin.Y)
	if dx > 4 || dx < -4 || dy > 4 || dy < -4 {
		didDrag = true
	}
	if !didDrag {
		return
	}
	if dragKind == dragResize {
		next := bubblegeom.FromTopLeftDrag(initialSize, dx, dy, dpi(32), dpi(88))
		layoutState.BubbleSize = next
		right, bottom := windowOrigin.X+int32(initialSize), windowOrigin.Y+int32(initialSize)
		call(u32.NewProc("SetWindowPos"), window, 0, uintptr(right-int32(next)), uintptr(bottom-int32(next)), uintptr(next), uintptr(next), swpNoZOrder|swpNoActivate)
		refreshBubbleShape()
	} else {
		call(u32.NewProc("SetWindowPos"), window, 0, uintptr(int32(int(windowOrigin.X)+dx)), uintptr(int32(int(windowOrigin.Y)+dy)), 0, 0, swpNoSize|swpNoZOrder|swpNoActivate)
	}
}
func cancelBubbleDrag() {
	dragging = false
	dragKind = dragNone
}
func endBubbleDrag() {
	if !dragging {
		return
	}
	dragged := didDrag
	dragging = false
	dragKind = dragNone
	call(u32.NewProc("ReleaseCapture"))
	if dragged {
		saveLayout()
	} else {
		resize(true)
	}
}
func setMinimumPanelSize(lp uintptr) {
	info := (*winMinMax)(unsafe.Pointer(lp))
	info.MinTrack = winPoint{int32(dpi(336)), int32(dpi(346))}
}

// Hit-test the full *window* rectangle so the user can hover over the
// top-left corner and use the native ↖ resize cursor. X/Y edges are independent:
// dragging the left side changes width, top changes height, corner changes both.
func panelHitTest(p winPoint, r winRect) uintptr {
	// Use the same hit testing geometry on all monitors; inputs are physical pixels.
	// uiScale is pixels/logical-point, not necessarily an integer on 125% displays.
	return windowgeom.HitTestScaled(int(p.X), int(p.Y), int(r.Left), int(r.Top), int(r.Right), int(r.Bottom), uiScale)
}

type trackMouseEvent struct {
	Size      uint32
	Flags     uint32
	Hwnd      uintptr
	HoverTime uint32
}

var mouseTracking bool

func updateBubbleHover(x, y int) {
	if window == 0 || expanded {
		return
	}
	if !mouseTracking {
		track := trackMouseEvent{Size: uint32(unsafe.Sizeof(trackMouseEvent{})), Flags: 2, Hwnd: window}
		call(u32.NewProc("TrackMouseEvent"), uintptr(unsafe.Pointer(&track)))
		mouseTracking = true
	}
	_ = x
	_ = y
	if hoverGrip {
		hoverGrip = false
		call(u32.NewProc("InvalidateRect"), window, 0, 0)
	}
}
