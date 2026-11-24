//go:build windows

package main

import (
	"hoverdict/internal/pixelscroll"
	"slices"
	"sort"
	"strings"
	"syscall"
	"unsafe"
)

// A pixel-scrolled, double-buffered virtual canvas replaces the Windows
// LISTBOX. LISTBOX/LB_SETTOPINDEX jumped by entire variable-height cards,
// producing visible jitter and off-by-one line clipping with trackpads.
const (
	wsVScroll       = 0x00200000
	wmVScroll       = 0x0115
	wmKeyDown       = 0x0100
	wmLButtonDown   = 0x0201
	wmLButtonDblClk = 0x0203
	wmGetDlgCode    = 0x0087
	wmSavedReflow   = 0x800D
	sbLineUp        = 0
	sbLineDown      = 1
	sbPageUp        = 2
	sbPageDown      = 3
	sbThumbPosition = 4
	sbThumbTrack    = 5
	sbTop           = 6
	sbBottom        = 7
	sifRange        = 0x1
	sifPage         = 0x2
	sifPos          = 0x4
	sifTrackPos     = 0x10
)

type savedScrollInfo struct {
	CbSize    uint32
	FMask     uint32
	NMin      int32
	NMax      int32
	NPage     uint32
	NPos      int32
	NTrackPos int32
}
type savedCardText struct{ pron, meaning string }

type savedViewRow struct {
	word, pron, meaning    string
	y, height              int
	wordH, pronH, meaningH int
}

var (
	savedViewRows         []savedViewRow
	savedViewStarts       []int
	savedListShown        []string
	savedViewSelected     string
	savedScrollY          int
	savedScrollRemainder  int
	savedTotalHeight      int
	savedViewWidth        int
	savedViewHeight       int
	savedViewHover        = -1
	savedViewTracking     bool
	savedCanvasCallback   uintptr
	savedViewMeasuring    bool
	savedViewReflowQueued bool
	savedDetailsCache     = map[string]savedCardText{}
)

func createSavedView() uintptr {
	name := syscall.StringToUTF16Ptr("PipiDictSavedCanvasV48")
	instance := call(k32.NewProc("GetModuleHandleW"), 0)
	if savedCanvasCallback == 0 {
		savedCanvasCallback = syscall.NewCallback(savedCanvasProc)
		cls := WNDCLASS{Size: uint32(unsafe.Sizeof(WNDCLASS{})), Style: 0x0008 | 0x0002, Proc: savedCanvasCallback, Instance: instance, Name: name}
		call(u32.NewProc("RegisterClassExW"), uintptr(unsafe.Pointer(&cls)))
	}
	return call(procCreate, 0, uintptr(unsafe.Pointer(name)), 0, WCHILD|WVISIBLE|wsVScroll|0x02000000, 0, 0, 10, 10, window, 106, instance, 0)
}
func savedCanvasSize() (int, int) {
	if list == 0 {
		return dpi(300), dpi(280)
	}
	var r uiRect
	call(u32.NewProc("GetClientRect"), list, uintptr(unsafe.Pointer(&r)))
	return int(r.Right - r.Left), int(r.Bottom - r.Top)
}
func savedCanvasWidth() int {
	w, _ := savedCanvasSize()
	return max(w, dpi(230))
}
func savedColumns(contentWidth int) (int, int) {
	left := contentWidth * 36 / 100
	if left < dpi(103) {
		left = dpi(103)
	}
	if left > dpi(178) {
		left = dpi(178)
	}
	if left > contentWidth-dpi(80) {
		left = max(dpi(70), contentWidth-dpi(80))
	}
	right := max(dpi(42), contentWidth-left-dpi(16))
	return left, right
}

// Clip a long meaning to at most five readable lines in the overview.
// A double click still opens the full, stored definition on the search page.
func savedPreview(dc uintptr, meaning string, width int) (string, int) {
	meaning = strings.TrimSpace(strings.ReplaceAll(meaning, "\r\n", "\n"))
	if meaning == "" {
		meaning = "释义暂无"
	}
	const maxChars = 240
	runes := []rune(meaning)
	if len(runes) > maxChars {
		runes = runes[:maxChars]
	}
	full := string(runes)
	maxH := dpi(112)
	height := int(measureWrapped(dc, uiFont, full, int32(width)))
	if height <= maxH && len(runes) == len([]rune(meaning)) {
		return full, height
	}
	// Binary search fits the text precisely into the same width/font used to draw it.
	lo, hi := 0, len(runes)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if int(measureWrapped(dc, uiFont, string(runes[:mid])+"…", int32(width))) <= maxH {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	if lo <= 0 {
		return "…", dpi(18)
	}
	preview := strings.TrimSpace(string(runes[:lo])) + "…"
	return preview, int(measureWrapped(dc, uiFont, preview, int32(width)))
}
func savedMeasureRow(dc uintptr, word, pron, meaning string, w int) savedViewRow {
	textW := max(100, w-dpi(34))
	left, right := savedColumns(textW)
	wordH := int(measureWrapped(dc, uiRowFont, word, int32(left)))
	pronH := int(measureWrapped(dc, uiPronFont, pron, int32(left)))
	preview, meaningH := savedPreview(dc, meaning, right)
	height := max(wordH+pronH+dpi(6), meaningH) + dpi(30)
	if height < dpi(78) {
		height = dpi(78)
	}
	return savedViewRow{word: word, pron: pron, meaning: preview, height: height, wordH: wordH, pronH: pronH, meaningH: meaningH}
}
func savedUpdateScrollInfo() {
	if list == 0 {
		return
	}
	_, h := savedCanvasSize()
	savedViewHeight = h
	savedScrollY = pixelscroll.Clamp(savedScrollY, savedTotalHeight, h)
	maxPos := max(0, savedTotalHeight-1)
	info := savedScrollInfo{CbSize: uint32(unsafe.Sizeof(savedScrollInfo{})), FMask: sifRange | sifPage | sifPos, NMax: int32(maxPos), NPage: uint32(max(1, h)), NPos: int32(savedScrollY)}
	call(u32.NewProc("SetScrollInfo"), list, 1, uintptr(unsafe.Pointer(&info)), 1)
}
func savedSetOffset(pos int) {
	if list == 0 {
		return
	}
	_, h := savedCanvasSize()
	next := pixelscroll.Clamp(pos, savedTotalHeight, h)
	if next == savedScrollY {
		return
	}
	savedScrollY = next
	savedViewHover = -1
	savedUpdateScrollInfo()
	call(u32.NewProc("InvalidateRect"), list, 0, 0)
}
func savedRowIndexAt(y int) int {
	idx := pixelscroll.At(savedViewStarts, y)
	if idx < 0 || idx >= len(savedViewRows) {
		return -1
	}
	if y >= savedViewRows[idx].y+savedViewRows[idx].height {
		return -1
	}
	return idx
}
func savedViewSelectedIndex() int { return slices.Index(savedListShown, savedViewSelected) }
func savedSelectRow(index int) {
	if index < 0 || index >= len(savedViewRows) {
		return
	}
	if savedViewSelected != savedViewRows[index].word {
		savedViewSelected = savedViewRows[index].word
		call(u32.NewProc("EnableWindow"), saveBtn, 1)
		call(u32.NewProc("InvalidateRect"), list, 0, 0)
	}
}
func savedEnsureVisible(index int) {
	if index < 0 || index >= len(savedViewRows) {
		return
	}
	row := savedViewRows[index]
	_, h := savedCanvasSize()
	if row.y < savedScrollY {
		savedSetOffset(row.y)
	} else if row.y+row.height > savedScrollY+h {
		savedSetOffset(row.y + row.height - h)
	}
}
func savedOpenSelected() {
	word, err := selectedSavedWord()
	if err != nil {
		return
	}
	inList = false
	set(edit, word)
	onWordEdited()
	reflowControls()
	draw()
	call(u32.NewProc("InvalidateRect"), window, 0, 0)
}

// Rebuilds happen when bookmark content or width changes, not on WM_PAINT.
// We anchor the previous top-visible word so edits don't jump to the top.
func savedBuildRows(force bool) {
	if list == 0 || !inList {
		return
	}
	words := savedOrder
	if len(words) != len(saved) {
		words = make([]string, 0, len(saved))
		for word := range saved {
			words = append(words, word)
		}
		sort.Strings(words)
	}
	width, _ := savedCanvasSize()
	if savedViewMeasuring {
		return
	}
	if !force && width == savedViewWidth && slices.Equal(savedListShown, words) {
		return
	}
	savedViewMeasuring = true
	defer func() { savedViewMeasuring = false }()
	if !slices.Equal(savedListShown, words) {
		savedDetailsCache = map[string]savedCardText{}
	}
	anchorWord, anchorWithin := "", 0
	if idx := savedRowIndexAt(savedScrollY); idx >= 0 {
		anchorWord = savedViewRows[idx].word
		anchorWithin = savedScrollY - savedViewRows[idx].y
	}
	dc := call(u32.NewProc("GetDC"), list)
	if dc == 0 {
		return
	}
	rows := make([]savedViewRow, 0, len(words))
	starts := make([]int, 0, len(words))
	offset := 0
	for _, word := range words {
		cached, exists := savedDetailsCache[word]
		if !exists {
			cached.pron, cached.meaning = savedWordDetails(word)
			savedDetailsCache[word] = cached
		}
		row := savedMeasureRow(dc, word, cached.pron, cached.meaning, max(width, dpi(230)))
		row.y = offset
		rows = append(rows, row)
		starts = append(starts, offset)
		offset += row.height + dpi(7)
	}
	call(u32.NewProc("ReleaseDC"), list, dc)
	savedListShown = append([]string(nil), words...)
	savedViewRows = rows
	savedViewStarts = starts
	savedViewWidth = width
	savedTotalHeight = offset
	if anchorWord != "" {
		if idx := slices.Index(savedListShown, anchorWord); idx >= 0 {
			savedScrollY = savedViewRows[idx].y + anchorWithin
		}
	}
	if savedViewSelected != "" && !slices.Contains(savedListShown, savedViewSelected) {
		savedViewSelected = ""
	}
	savedUpdateScrollInfo()
	// Showing/hiding the native scrollbar changes the client width; remeasure
	// once after that layout settles, never recursively from WM_SIZE.
	if actualWidth, _ := savedCanvasSize(); actualWidth != savedViewWidth && !savedViewReflowQueued {
		savedViewReflowQueued = true
		call(u32.NewProc("PostMessageW"), list, wmSavedReflow, 0, 0)
	}
	call(u32.NewProc("InvalidateRect"), list, 0, 0)
}
func rebuildSavedList() { savedBuildRows(false) }
func resizeSavedRows()  { savedBuildRows(true) }
func invalidateSavedCards() {
	savedDetailsCache = map[string]savedCardText{}
	savedBuildRows(true)
}
func scrollSavedWheel(w uintptr) {
	if list == 0 || !inList {
		return
	}
	delta := int(int16(uint16(w >> 16)))
	next, rem := pixelscroll.Wheel(savedScrollY, savedScrollRemainder, delta, dpi(68), savedTotalHeight, savedViewHeight)
	savedScrollRemainder = rem
	savedSetOffset(next)
}
func savedDrawRows(dc uintptr, width, height int) {
	fillSolid(dc, uiRect{0, 0, int32(width), int32(height)}, uiAppBG)
	index := pixelscroll.At(savedViewStarts, savedScrollY)
	if index < 0 {
		index = 0
	}
	for i := index; i < len(savedViewRows); i++ {
		row := savedViewRows[i]
		y := row.y - savedScrollY
		if y > height {
			break
		}
		if y+row.height < 0 {
			continue
		}
		rect := uiRect{int32(dpi(4)), int32(y + dpi(2)), int32(width - dpi(4)), int32(y + row.height - dpi(2))}
		selected := row.word == savedViewSelected
		hovered := i == savedViewHover
		fill, border := uiCardBG, uiBorder
		if hovered {
			fill = rgb(250, 252, 255)
			border = rgb(214, 226, 247)
		}
		if selected {
			fill = rgb(236, 243, 255)
			border = rgb(167, 193, 244)
		}
		paintRounded(dc, rect, fill, border, int32(dpi(12)))
		if selected {
			fillSolid(dc, uiRect{rect.Left + int32(dpi(1)), rect.Top + int32(dpi(13)), rect.Left + int32(dpi(4)), rect.Bottom - int32(dpi(13))}, uiBlue)
		}
		// Two balanced columns; IPA always remains directly under the word.
		x := int(rect.Left) + dpi(14)
		textW := max(100, width-dpi(34))
		left, _ := savedColumns(textW)
		top := int(rect.Top) + dpi(12)
		wordRect := uiRect{int32(x), int32(top), int32(x + left), int32(top + row.wordH)}
		pronRect := uiRect{int32(x), int32(top + row.wordH + dpi(6)), int32(x + left), int32(top + row.wordH + dpi(6) + row.pronH)}
		drawWrappedText(dc, row.word, wordRect, uiRowFont, uiInk)
		drawWrappedText(dc, row.pron, pronRect, uiPronFont, uiMuted)
		splitter := x + left + dpi(6)
		fillSolid(dc, uiRect{int32(splitter), int32(top), int32(splitter + 1), rect.Bottom - int32(dpi(12))}, uiBorder)
		chineseX := splitter + dpi(10)
		chineseRect := uiRect{int32(chineseX), int32(top), rect.Right - int32(dpi(12)), rect.Bottom - int32(dpi(9))}
		chineseInk := uiInk
		if row.meaning == "释义暂无" {
			chineseInk = uiMuted
		}
		drawWrappedText(dc, row.meaning, chineseRect, uiFont, chineseInk)
	}
}
func savedPaintCanvas(hwnd uintptr) {
	var ps uiPaintStruct
	dc := call(u32.NewProc("BeginPaint"), hwnd, uintptr(unsafe.Pointer(&ps)))
	if dc == 0 {
		return
	}
	defer call(u32.NewProc("EndPaint"), hwnd, uintptr(unsafe.Pointer(&ps)))
	w, h := savedCanvasSize()
	if w < 1 || h < 1 {
		return
	}
	mem := call(gdi.NewProc("CreateCompatibleDC"), dc)
	if mem == 0 {
		savedDrawRows(dc, w, h)
		return
	}
	defer call(gdi.NewProc("DeleteDC"), mem)
	bmp := call(gdi.NewProc("CreateCompatibleBitmap"), dc, uintptr(w), uintptr(h))
	if bmp == 0 {
		savedDrawRows(dc, w, h)
		return
	}
	defer call(gdi.NewProc("DeleteObject"), bmp)
	original := call(gdi.NewProc("SelectObject"), mem, bmp)
	savedDrawRows(mem, w, h)
	call(gdi.NewProc("BitBlt"), dc, 0, 0, uintptr(w), uintptr(h), mem, 0, 0, 0x00CC0020)
	call(gdi.NewProc("SelectObject"), mem, original)
}
func savedCanvasProc(hwnd uintptr, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case wmSavedReflow:
		savedViewReflowQueued = false
		savedBuildRows(true)
		return 0
	case wmEraseBkgnd:
		return 1
	case wmPaint:
		savedPaintCanvas(hwnd)
		return 0
	case wmGetDlgCode:
		return 1 // navigation arrows
	case wmSize:
		if list == hwnd && inList {
			savedBuildRows(true)
		}
		return 0
	case wmMouseWheel:
		scrollSavedWheel(w)
		return 0
	case wmVScroll:
		command := int(w & 0xffff)
		scroll := savedScrollY
		_, h := savedCanvasSize()
		switch command {
		case sbLineUp:
			scroll -= dpi(30)
		case sbLineDown:
			scroll += dpi(30)
		case sbPageUp:
			scroll -= max(dpi(36), h-dpi(30))
		case sbPageDown:
			scroll += max(dpi(36), h-dpi(30))
		case sbTop:
			scroll = 0
		case sbBottom:
			scroll = savedTotalHeight
		case sbThumbPosition, sbThumbTrack:
			info := savedScrollInfo{CbSize: uint32(unsafe.Sizeof(savedScrollInfo{})), FMask: sifTrackPos | sifPos}
			if call(u32.NewProc("GetScrollInfo"), hwnd, 1, uintptr(unsafe.Pointer(&info))) != 0 {
				if command == sbThumbTrack {
					scroll = int(info.NTrackPos)
				} else {
					scroll = int(info.NPos)
				}
			}
		}
		savedSetOffset(scroll)
		return 0
	case wmLButtonDown, wmLButtonDblClk:
		y := int(int16(uint16(l >> 16)))
		index := savedRowIndexAt(savedScrollY + y)
		if index >= 0 {
			savedSelectRow(index)
			call(u32.NewProc("SetFocus"), hwnd)
			if msg == wmLButtonDblClk {
				savedOpenSelected()
			}
		}
		return 0
	case wmMouseMove:
		y := int(int16(uint16(l >> 16)))
		hover := savedRowIndexAt(savedScrollY + y)
		if hover != savedViewHover {
			savedViewHover = hover
			call(u32.NewProc("InvalidateRect"), hwnd, 0, 0)
		}
		if !savedViewTracking {
			type trackMouseEvent struct {
				CbSize, DwFlags uint32
				Hwnd            uintptr
				HoverTime       uint32
			}
			event := trackMouseEvent{CbSize: uint32(unsafe.Sizeof(trackMouseEvent{})), DwFlags: 2, Hwnd: hwnd}
			if call(u32.NewProc("TrackMouseEvent"), uintptr(unsafe.Pointer(&event))) != 0 {
				savedViewTracking = true
			}
		}
		return 0
	case wmMouseLeave:
		savedViewTracking = false
		savedViewHover = -1
		call(u32.NewProc("InvalidateRect"), hwnd, 0, 0)
		return 0
	case wmKeyDown:
		if len(savedViewRows) == 0 {
			return 0
		}
		idx := savedViewSelectedIndex()
		if idx < 0 {
			idx = 0
		}
		key := int(w)
		switch key {
		case 0x26:
			idx = max(0, idx-1) // arrow up
		case 0x28:
			idx = min(len(savedViewRows)-1, idx+1) // down
		case 0x24:
			idx = 0 // home
		case 0x23:
			idx = len(savedViewRows) - 1 // end
		case 0x21:
			savedSetOffset(savedScrollY - savedViewHeight)
			return 0
		case 0x22:
			savedSetOffset(savedScrollY + savedViewHeight)
			return 0
		case 0x0D:
			savedOpenSelected()
			return 0
		default:
			return 0
		}
		savedSelectRow(idx)
		savedEnsureVisible(idx)
		return 0
	}
	return call(def, hwnd, uintptr(msg), w, l)
}
