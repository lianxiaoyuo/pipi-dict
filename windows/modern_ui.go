//go:build windows

package main

// Single-window native Win32 interface, with responsive control placement,
// Windows 11 rounded corners, a vector panda bubble, and no paid-provider UI.

import (
	"fmt"
	"hoverdict/internal/panelgeom"
	"math"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	wmPaint        = 0x000F
	wmEraseBkgnd   = 0x0014
	wmDrawItem     = 0x002B
	wmColorEdit    = 0x0133
	wmColorList    = 0x0134
	wmColorStatic  = 0x0138
	wmSetFont      = 0x0030
	wmLookupStatus = 0x8004
)

var (
	gdi                                                                                            = syscall.NewLazyDLL("gdi32.dll")
	dwm                                                                                            = syscall.NewLazyDLL("dwmapi.dll")
	uiScale                                                                                float64 = 1
	uiFont, uiTitleFont, uiWordFont, uiSmallFont, uiRowFont, uiPronFont, uiWordCompactFont uintptr
	uiBackground, uiWhite                                                                  uintptr
	headWord, headPron, headOrigin                                                         uintptr
	collapseBtn                                                                            uintptr
)

// Cache stable child-window geometry and visibility. Moving or hiding the
// same child on every lookup caused cascades of synchronous WM_SIZE/WM_PAINT.
var controlGeometry = make(map[uintptr][4]int)
var controlVisible = make(map[uintptr]bool)
var controlLabelText = make(map[uintptr]string)
var controlFontCache = make(map[uintptr]uintptr)
var detailMeasurement struct {
	text   string
	width  int32
	font   uintptr
	height int32
	valid  bool
}

func measureDetailText(hdc, font uintptr, text string, width int32) int32 {
	if detailMeasurement.valid && detailMeasurement.text == text && detailMeasurement.width == width && detailMeasurement.font == font {
		return detailMeasurement.height
	}
	h := measureWrapped(hdc, font, text, width)
	detailMeasurement.text, detailMeasurement.width, detailMeasurement.font, detailMeasurement.height, detailMeasurement.valid = text, width, font, h, true
	return h
}

func setLabel(h uintptr, text string) {
	if h == 0 {
		return
	}
	if prev, ok := controlLabelText[h]; ok && prev == text {
		return
	}
	controlLabelText[h] = text
	set(h, text)
}

type uiRect struct{ Left, Top, Right, Bottom int32 }
type uiPaintStruct struct {
	HDC       uintptr
	Erase     int32
	Rect      uiRect
	Restore   int32
	IncUpdate int32
	Reserved  [32]byte
}
type uiMeasureItem struct {
	CtlType, CtlID, ItemID, Width, Height uint32
	ItemData                              uintptr
}
type uiDrawItem struct {
	CtlType, CtlID, ItemID, ItemAction, ItemState uint32
	HwndItem, HDC                                 uintptr
	Rect                                          uiRect
	ItemData                                      uintptr
}

func initUIScale() {
	getDPI := u32.NewProc("GetDpiForSystem")
	if getDPI.Find() == nil {
		if p := call(getDPI); p >= 96 && p <= 384 {
			uiScale = float64(p) / 96.0
		}
	}
}
func dpi(x int) int              { return int(math.Round(float64(x) * uiScale)) }
func rgb(r, g, b uint32) uintptr { return uintptr(r | (g << 8) | (b << 16)) }

var (
	uiInk       = rgb(28, 42, 65)
	uiMuted     = rgb(119, 133, 157)
	uiBlue      = rgb(65, 106, 232)
	uiBlueHover = rgb(43, 87, 208)
	uiCardBG    = rgb(255, 255, 255)
	uiAppBG     = rgb(247, 249, 252)
	uiBorder    = rgb(231, 236, 244)
)

func makeFont(px, weight int32) uintptr {
	family := syscall.StringToUTF16Ptr("Microsoft YaHei UI")
	return call(gdi.NewProc("CreateFontW"), uintptr(int32(-px)), 0, 0, 0, uintptr(weight), 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(family)))
}
func setControlFont(hwnd, font uintptr) {
	if hwnd == 0 || font == 0 {
		return
	}
	if controlFontCache[hwnd] == font {
		return
	}
	controlFontCache[hwnd] = font
	call(send, hwnd, wmSetFont, font, 1)
}
func setupModernUI() {
	uiFont = makeFont(int32(dpi(14)), 400)
	uiTitleFont = makeFont(int32(dpi(17)), 700)
	uiWordFont = makeFont(int32(dpi(21)), 700)
	uiSmallFont = makeFont(int32(dpi(11)), 400)
	uiRowFont = makeFont(int32(dpi(15)), 650)
	uiPronFont = makeFont(int32(dpi(11)), 400)
	uiWordCompactFont = makeFont(int32(dpi(16)), 700)
	uiBackground = call(gdi.NewProc("CreateSolidBrush"), uiAppBG)
	uiWhite = call(gdi.NewProc("CreateSolidBrush"), uiCardBG)
	p := uint32(2) // DWM rounded window corner preference
	if proc := dwm.NewProc("DwmSetWindowAttribute"); proc.Find() == nil {
		proc.Call(window, 33, uintptr(unsafe.Pointer(&p)), 4)
		caption := uint32(uiAppBG)
		proc.Call(window, 35, uintptr(unsafe.Pointer(&caption)), 4)
		foreground := uint32(uiInk)
		proc.Call(window, 36, uintptr(unsafe.Pointer(&foreground)), 4)
	}
}
func buildModernControls() {
	setupModernUI()
	// No subtitle, tips or permanent diagnostic/status text in the personal UI.
	// The header is painted onto the parent so it doubles as a drag surface.
	edit = ctrl("EDIT", "", 0, 0, 10, 10, 101, 0x80)
	headWord = ctrl("STATIC", "", 0, 0, 10, 10, 0, 0)
	headPron = ctrl("STATIC", "", 0, 0, 10, 10, 0, 0)
	headOrigin = ctrl("STATIC", "", 0, 0, 10, 10, 0, 0)
	detail = ctrl("EDIT", "", 0, 0, 10, 10, 0, 0x0004|0x0800|0x0040)
	list = createSavedView()
	saveBtn = ctrl("BUTTON", "☆ 收藏", 0, 0, 10, 10, 102, 0)
	listBtn = ctrl("BUTTON", "生词本", 0, 0, 10, 10, 103, 0)
	collapseBtn = ctrl("BUTTON", "–", 0, 0, 10, 10, 104, 0)
	for _, hwnd := range []uintptr{edit, headPron, headOrigin, detail, list, saveBtn, listBtn, collapseBtn} {
		setControlFont(hwnd, uiFont)
	}
	setControlFont(headWord, uiWordFont)
	setControlFont(list, uiRowFont)
	setControlFont(headPron, uiSmallFont)
	setControlFont(headOrigin, uiSmallFont)
	setControlFont(detail, uiFont)
	// Native search placeholder. It appears only while the input is empty.
	call(send, edit, 0x1501, 1, ptr("输入或点击英文单词")) // EM_SETCUEBANNER
	showHide(false, list)
	reflowControls()
	call(u32.NewProc("InvalidateRect"), window, 0, 1)
}
func rectClient() uiRect {
	var r uiRect
	if window != 0 {
		call(u32.NewProc("GetClientRect"), window, uintptr(unsafe.Pointer(&r)))
	}
	return r
}
func moveControl(h uintptr, x, y, w, ht int) {
	if h == 0 {
		return
	}
	if w < 5 {
		w = 5
	}
	if ht < 5 {
		ht = 5
	}
	wanted := [4]int{x, y, w, ht}
	if old, ok := controlGeometry[h]; ok && old == wanted {
		return
	}
	controlGeometry[h] = wanted
	call(u32.NewProc("MoveWindow"), h, uintptr(x), uintptr(y), uintptr(w), uintptr(ht), 1)
}

// Small, consistent vertical rhythm. The title bar remains draggable, and the
// upper-left/borders still use native Windows hit testing to resize.
func panelLayout() panelgeom.Layout {
	r := rectClient()
	return panelgeom.Compute(int(r.Right-r.Left), int(r.Bottom-r.Top), uiScale)
}
func moveArea(h uintptr, r panelgeom.Rect) { moveControl(h, r.X, r.Y, r.W, r.H) }

// Search and saved words share the same geometry, typography and bottom actions.
// A short translation uses only the required height; it does not create a
// full-page edit box. Long definitions can scroll without enlarging the panel.
func reflowControls() {
	if window == 0 || edit == 0 || !expanded {
		return
	}
	a := panelLayout()
	moveArea(collapseBtn, a.Collapse)
	moveArea(edit, a.SearchInput)
	if inList {
		moveArea(listBtn, a.Primary)
		moveArea(saveBtn, a.Secondary)
		moveArea(list, a.SavedList)
	} else {
		moveArea(saveBtn, a.Primary)
		moveArea(listBtn, a.Secondary)
		moveArea(headWord, a.Word)
		moveArea(headPron, a.Pron)
		moveArea(headOrigin, a.Origin)
		if detail != 0 {
			text := get(detail)
			required := dpi(23)
			if text != "" {
				dc := call(u32.NewProc("GetDC"), detail)
				if dc != 0 {
					required = int(measureDetailText(dc, uiFont, text, int32(a.Detail.W-dpi(10)))) + dpi(9)
					call(u32.NewProc("ReleaseDC"), detail, dc)
				}
			}
			visible := required
			if visible > a.Detail.H {
				visible = a.Detail.H
			}
			if visible < dpi(22) {
				visible = dpi(22)
			}
			moveControl(detail, a.Detail.X, a.Detail.Y, a.Detail.W, visible)
			call(u32.NewProc("ShowScrollBar"), detail, 1, boolToPtr(required > a.Detail.H))
		}
	}
}
func boolToPtr(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}
func showExpandedControls(exp bool) {
	wordReady := exp && !inList && (norm(get(edit)) != "" || selectedParagraph != "" || lookupDisplayStatus != "")
	showHide(exp, collapseBtn, saveBtn, listBtn)
	showHide(exp && !inList && wordReady, headWord)
	showHide(exp && !inList && wordReady && get(headPron) != "", headPron)
	showHide(wordReady, headOrigin, detail)
	showHide(exp && !inList, edit)
	showHide(exp && inList && len(saved) > 0, list)
}
func paintRounded(hdc uintptr, r uiRect, fill, border uintptr, radius int32) {
	brush := call(gdi.NewProc("CreateSolidBrush"), fill)
	pen := call(gdi.NewProc("CreatePen"), 0, uintptr(dpi(1)), border)
	oldBrush := call(gdi.NewProc("SelectObject"), hdc, brush)
	oldPen := call(gdi.NewProc("SelectObject"), hdc, pen)
	call(gdi.NewProc("RoundRect"), hdc, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right), uintptr(r.Bottom), uintptr(radius), uintptr(radius))
	call(gdi.NewProc("SelectObject"), hdc, oldBrush)
	call(gdi.NewProc("SelectObject"), hdc, oldPen)
	call(gdi.NewProc("DeleteObject"), brush)
	call(gdi.NewProc("DeleteObject"), pen)
}
func uiDrawText(hdc uintptr, s string, r uiRect, font, color uintptr) {
	call(gdi.NewProc("SetBkMode"), hdc, 1)
	call(gdi.NewProc("SetTextColor"), hdc, color)
	old := call(gdi.NewProc("SelectObject"), hdc, font)
	text := syscall.StringToUTF16Ptr(s)
	call(u32.NewProc("DrawTextW"), hdc, uintptr(unsafe.Pointer(text)), ^uintptr(0), uintptr(unsafe.Pointer(&r)), 0x0|0x4|0x20|0x8000)
	call(gdi.NewProc("SelectObject"), hdc, old)
}
func fillSolid(hdc uintptr, r uiRect, color uintptr) {
	brush := call(gdi.NewProc("CreateSolidBrush"), color)
	call(u32.NewProc("FillRect"), hdc, uintptr(unsafe.Pointer(&r)), brush)
	call(gdi.NewProc("DeleteObject"), brush)
}
func paintUI(hwnd uintptr) {
	var ps uiPaintStruct
	dc := call(u32.NewProc("BeginPaint"), hwnd, uintptr(unsafe.Pointer(&ps)))
	if dc == 0 {
		return
	}
	defer call(u32.NewProc("EndPaint"), hwnd, uintptr(unsafe.Pointer(&ps)))
	// In paragraph mode, the native EDIT owns its pixels, including its scrollbar.
	clipParagraphDetailFromParent(dc, hwnd)
	r := rectClient()
	w, h := r.Right-r.Left, r.Bottom-r.Top
	if w <= 0 || h <= 0 {
		return
	}
	// Double-buffer parent-owned geometry to prevent flicker while resizing.
	dest := dc
	mem := call(gdi.NewProc("CreateCompatibleDC"), dc)
	var image, original uintptr
	if mem != 0 {
		image = call(gdi.NewProc("CreateCompatibleBitmap"), dc, uintptr(w), uintptr(h))
		if image != 0 {
			original = call(gdi.NewProc("SelectObject"), mem, image)
			dest = mem
		}
	}
	if !expanded {
		// The collapsed tool window uses LWA_COLORKEY: the corners are
		// genuinely transparent, not a rounded-square background.
		fillSolid(dest, r, rgb(1, 2, 3))
	} else {
		fillSolid(dest, r, uiAppBG)
	}
	if !expanded {
		paintPanda(dest, int32(layoutState.BubbleSize))
	} else {
		paintPanel(dest, w, h)
	}
	if dest == mem {
		call(gdi.NewProc("BitBlt"), dc, 0, 0, uintptr(w), uintptr(h), mem, 0, 0, 0x00CC0020)
		call(gdi.NewProc("SelectObject"), mem, original)
	}
	if image != 0 {
		call(gdi.NewProc("DeleteObject"), image)
	}
	if mem != 0 {
		call(gdi.NewProc("DeleteDC"), mem)
	}
}
func uiDrawPanelPanda(hdc uintptr, r panelgeom.Rect) {
	x, y := int32(r.X), int32(r.Y)
	s := int32(r.W)
	// Compact mascot mark in the header; no oversized text or persistent tips.
	paintRounded(hdc, uiRect{x, y, x + s, y + s}, uiBlue, uiBlue, int32(dpi(9)))
	dark := rgb(37, 64, 106)
	pandaOval(hdc, uiRect{x + int32(dpi(4)), y + int32(dpi(3)), x + int32(dpi(12)), y + int32(dpi(12))}, dark)
	pandaOval(hdc, uiRect{x + s - int32(dpi(12)), y + int32(dpi(3)), x + s - int32(dpi(4)), y + int32(dpi(12))}, dark)
	pandaOval(hdc, uiRect{x + int32(dpi(5)), y + int32(dpi(7)), x + s - int32(dpi(5)), y + s - int32(dpi(3))}, rgb(255, 255, 255))
	pandaOval(hdc, uiRect{x + int32(dpi(9)), y + int32(dpi(13)), x + int32(dpi(13)), y + int32(dpi(18))}, dark)
	pandaOval(hdc, uiRect{x + s - int32(dpi(13)), y + int32(dpi(13)), x + s - int32(dpi(9)), y + int32(dpi(18))}, dark)
	pandaOval(hdc, uiRect{x + s/2 - int32(dpi(2)), y + int32(dpi(19)), x + s/2 + int32(dpi(2)), y + int32(dpi(22))}, dark)
}
func paintPanel(hdc uintptr, w, h int32) {
	a := panelLayout()
	uiDrawPanelPanda(hdc, a.Logo)
	uiDrawText(hdc, "Pipi Dict", uiRect{int32(a.Title.X), int32(a.Title.Y), int32(a.Title.Right()), int32(a.Title.Bottom())}, uiTitleFont, uiInk)
	if inList {
		countLabel := fmt.Sprintf("%d 个收藏", len(saved))
		uiDrawTextCenter(hdc, countLabel, uiRect{w - int32(dpi(162)), int32(dpi(17)), w - int32(dpi(55)), int32(dpi(43))}, uiFont, uiMuted)
		if len(saved) == 0 {
			uiDrawTextCenter(hdc, "还没有收藏的单词", uiRect{int32(a.SavedList.X), int32(a.SavedList.Y + 50), int32(a.SavedList.Right()), int32(a.SavedList.Y + 100)}, uiFont, uiMuted)
		}
		return
	}
	search := a.SearchCard
	paintRounded(hdc, uiRect{int32(search.X), int32(search.Y), int32(search.Right()), int32(search.Bottom())}, uiCardBG, uiBorder, int32(dpi(13)))
	// Draw a clean vector search icon in the left slot.
	cx, cy := int32(search.X+dpi(25)), int32(search.Y+dpi(21))
	pen := call(gdi.NewProc("CreatePen"), 0, uintptr(dpi(2)), uiBlue)
	oldPen := call(gdi.NewProc("SelectObject"), hdc, pen)
	oldBrush := call(gdi.NewProc("SelectObject"), hdc, call(gdi.NewProc("GetStockObject"), 5))
	call(gdi.NewProc("Ellipse"), hdc, uintptr(cx-int32(dpi(6))), uintptr(cy-int32(dpi(6))), uintptr(cx+int32(dpi(6))), uintptr(cy+int32(dpi(6))))
	call(gdi.NewProc("MoveToEx"), hdc, uintptr(cx+int32(dpi(4))), uintptr(cy+int32(dpi(4))), 0)
	call(gdi.NewProc("LineTo"), hdc, uintptr(cx+int32(dpi(10))), uintptr(cy+int32(dpi(11))))
	call(gdi.NewProc("SelectObject"), hdc, oldBrush)
	call(gdi.NewProc("SelectObject"), hdc, oldPen)
	call(gdi.NewProc("DeleteObject"), pen)
	if norm(get(edit)) == "" && selectedParagraph == "" && lookupDisplayStatus == "" {
		return
	}
	card := a.ResultCard
	maxBottom := card.Bottom()
	// GDI measurement uses the same font/width as the live translation text.
	meaningHeight := int32(dpi(30))
	if detail != 0 {
		if text := get(detail); text != "" {
			meaningHeight = measureDetailText(hdc, uiFont, text, int32(a.Detail.W-dpi(10)))
		}
	}
	desiredBottom := a.Detail.Y + int(meaningHeight) + dpi(23)
	if desiredBottom < a.Detail.Y+dpi(34) {
		desiredBottom = a.Detail.Y + dpi(34)
	}
	if desiredBottom > maxBottom {
		desiredBottom = maxBottom
	}
	paintRounded(hdc, uiRect{int32(card.X), int32(card.Y), int32(card.Right()), int32(desiredBottom)}, uiCardBG, uiBorder, int32(dpi(15)))
	r := a.Divider
	fillSolid(hdc, uiRect{int32(r.X), int32(r.Y), int32(r.Right()), int32(r.Bottom())}, uiBorder)
}
func uiDrawTextCenter(hdc uintptr, s string, r uiRect, font, color uintptr) {
	call(gdi.NewProc("SetBkMode"), hdc, 1)
	call(gdi.NewProc("SetTextColor"), hdc, color)
	old := call(gdi.NewProc("SelectObject"), hdc, font)
	text := syscall.StringToUTF16Ptr(s)
	call(u32.NewProc("DrawTextW"), hdc, uintptr(unsafe.Pointer(text)), ^uintptr(0), uintptr(unsafe.Pointer(&r)), 0x1|0x4|0x20|0x800|0x8000)
	call(gdi.NewProc("SelectObject"), hdc, old)
}
func pandaOval(hdc uintptr, rect uiRect, fill uintptr) {
	b := call(gdi.NewProc("CreateSolidBrush"), fill)
	pen := call(gdi.NewProc("CreatePen"), 0, 1, fill)
	oldB := call(gdi.NewProc("SelectObject"), hdc, b)
	oldP := call(gdi.NewProc("SelectObject"), hdc, pen)
	call(gdi.NewProc("Ellipse"), hdc, uintptr(rect.Left), uintptr(rect.Top), uintptr(rect.Right), uintptr(rect.Bottom))
	call(gdi.NewProc("SelectObject"), hdc, oldB)
	call(gdi.NewProc("SelectObject"), hdc, oldP)
	call(gdi.NewProc("DeleteObject"), b)
	call(gdi.NewProc("DeleteObject"), pen)
}

// Draw a round panda avatar, without the previous blue/square background.
// The floating Win32 window itself is clipped to a circle in layout.go.
func paintPanda(hdc uintptr, size int32) {
	if size < 1 {
		return
	}
	sc := func(x int32) int32 { return x * size / 100 }
	box := func(a, b, c, d int32) uiRect { return uiRect{sc(a), sc(b), sc(c), sc(d)} }
	dark := rgb(32, 57, 92)
	white := rgb(255, 255, 255)
	pink := rgb(251, 162, 187)
	// The outer circle IS the panda face, not a decorative colored badge.
	pandaOval(hdc, box(0, 0, 100, 100), white)
	pandaOval(hdc, box(16, 12, 43, 42), dark)
	pandaOval(hdc, box(57, 12, 84, 42), dark)
	pandaOval(hdc, box(10, 25, 90, 93), white)
	pandaOval(hdc, box(28, 43, 45, 64), dark)
	pandaOval(hdc, box(55, 43, 72, 64), dark)
	pandaOval(hdc, box(33, 48, 39, 54), white)
	pandaOval(hdc, box(61, 48, 67, 54), white)
	pandaOval(hdc, box(45, 63, 55, 71), dark)
	pandaOval(hdc, box(22, 65, 34, 75), pink)
	pandaOval(hdc, box(66, 65, 78, 75), pink)
	// The resize handle remains available only while hovered.
	if hoverGrip {
		paintRounded(hdc, box(17, 17, 43, 43), rgb(231, 240, 255), rgb(231, 240, 255), sc(8))
		pen := call(gdi.NewProc("CreatePen"), 0, uintptr(sc(4)+1), uiBlue)
		old := call(gdi.NewProc("SelectObject"), hdc, pen)
		call(gdi.NewProc("MoveToEx"), hdc, uintptr(sc(22)), uintptr(sc(36)), 0)
		call(gdi.NewProc("LineTo"), hdc, uintptr(sc(35)), uintptr(sc(23)))
		call(gdi.NewProc("MoveToEx"), hdc, uintptr(sc(22)), uintptr(sc(23)), 0)
		call(gdi.NewProc("LineTo"), hdc, uintptr(sc(35)), uintptr(sc(23)))
		call(gdi.NewProc("MoveToEx"), hdc, uintptr(sc(22)), uintptr(sc(23)), 0)
		call(gdi.NewProc("LineTo"), hdc, uintptr(sc(22)), uintptr(sc(36)))
		call(gdi.NewProc("SelectObject"), hdc, old)
		call(gdi.NewProc("DeleteObject"), pen)
	}
}

// Word metadata is resolved once when the saved view is rebuilt, never from
// WM_PAINT. All preview heights use the same font and width as painting.
func savedWordDetails(word string) (pron, meaning string) {
	if e, ok := getEntry(strings.ToLower(word)); ok {
		pron = strings.TrimSpace(e.Pron)
		meaning = strings.TrimSpace(cleanMeaning(e.Meaning))
	}
	if metaStore != nil {
		if r, ok := metaStore.Get(word); ok {
			if pron == "" {
				pron = strings.TrimSpace(r.Pron)
			}
			if meaning == "" {
				meaning = strings.TrimSpace(r.Meaning)
			}
		}
	}
	if meaning == "" {
		if cached, found := memo.Get(strings.ToLower(word), time.Now()); found {
			meaning = cached
		}
	}
	if pron == "" {
		pron = "音标暂无"
	}
	if meaning == "" {
		meaning = "释义暂无"
	}
	meaning = strings.ReplaceAll(meaning, `\n`, "\n")
	meaning = strings.ReplaceAll(meaning, "\r\n", "\n")
	return pron, meaning
}

func measureWrapped(hdc, font uintptr, s string, width int32) int32 {
	if width < 16 {
		return int32(dpi(18))
	}
	r := uiRect{0, 0, width, 0}
	old := call(gdi.NewProc("SelectObject"), hdc, font)
	call(u32.NewProc("DrawTextW"), hdc, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(s))), ^uintptr(0), uintptr(unsafe.Pointer(&r)), 0x10|0x0800|0x0400)
	call(gdi.NewProc("SelectObject"), hdc, old)
	measured := r.Bottom - r.Top
	if measured < int32(dpi(16)) {
		measured = int32(dpi(16))
	}
	return measured
}
func drawWrappedText(hdc uintptr, s string, r uiRect, font, color uintptr) {
	call(gdi.NewProc("SetBkMode"), hdc, 1)
	call(gdi.NewProc("SetTextColor"), hdc, color)
	old := call(gdi.NewProc("SelectObject"), hdc, font)
	call(u32.NewProc("DrawTextW"), hdc, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(s))), ^uintptr(0), uintptr(unsafe.Pointer(&r)), 0x10|0x0800)
	call(gdi.NewProc("SelectObject"), hdc, old)
}

func drawModernButton(wptr uintptr) {
	if wptr == 0 {
		return
	}
	item := (*uiDrawItem)(unsafe.Pointer(wptr))
	if item.HDC == 0 || item.HwndItem == 0 {
		return
	}
	fill, ink, outline := uiCardBG, uiInk, uiBorder
	primary := (item.CtlID == 102 && !inList) || (item.CtlID == 103 && inList)
	danger := item.CtlID == 102 && inList
	if primary {
		fill, ink, outline = uiBlue, rgb(255, 255, 255), uiBlue
	}
	if danger {
		ink = rgb(194, 65, 70)
	}
	if item.ItemState&0x0004 != 0 {
		fill, ink, outline = rgb(238, 242, 248), uiMuted, uiBorder
	} else if item.ItemState&0x1 != 0 {
		if primary {
			fill = uiBlueHover
		} else {
			fill = rgb(231, 238, 249)
		}
	}
	paintRounded(item.HDC, item.Rect, fill, outline, int32(dpi(14)))
	call(gdi.NewProc("SetBkMode"), item.HDC, 1)
	call(gdi.NewProc("SetTextColor"), item.HDC, ink)
	old := call(gdi.NewProc("SelectObject"), item.HDC, uiFont)
	var buf [128]uint16
	call(u32.NewProc("GetWindowTextW"), item.HwndItem, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	call(u32.NewProc("DrawTextW"), item.HDC, uintptr(unsafe.Pointer(&buf[0])), ^uintptr(0), uintptr(unsafe.Pointer(&item.Rect)), 0x1|0x4|0x20|0x8000)
	call(gdi.NewProc("SelectObject"), item.HDC, old)
}
func paintControl(m uint32, hdc, handle uintptr) uintptr {
	if handle == detail && paragraphDetailActive() {
		// Read-only multiline EDIT uses WM_CTLCOLORSTATIC. Transparent text
		// can leave old glyphs behind when the control scrolls its pixels.
		call(gdi.NewProc("SetBkMode"), hdc, 2) // OPAQUE
	} else {
		call(gdi.NewProc("SetBkMode"), hdc, 1) // TRANSPARENT: unchanged for word mode
	}
	foreground := uiInk
	brush := uiBackground
	if handle == headWord || handle == headPron || handle == headOrigin || handle == detail || handle == edit {
		brush = uiWhite
	}
	if handle == list || (inList && (handle == headWord || handle == headPron)) {
		brush = uiBackground
	}
	if handle == headPron || handle == headOrigin {
		foreground = uiMuted
	}
	if handle == headOrigin {
		foreground = uiBlue
	}
	call(gdi.NewProc("SetTextColor"), hdc, foreground)
	if brush == uiWhite {
		call(gdi.NewProc("SetBkColor"), hdc, uiCardBG)
	} else {
		call(gdi.NewProc("SetBkColor"), hdc, uiAppBG)
	}
	return brush
}
func clearTheme() {
	for _, h := range []uintptr{uiFont, uiTitleFont, uiWordFont, uiSmallFont, uiRowFont, uiPronFont, uiWordCompactFont, uiBackground, uiWhite} {
		if h != 0 {
			call(gdi.NewProc("DeleteObject"), h)
		}
	}
}
