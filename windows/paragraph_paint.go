//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// V5.6.1: strictly local to paragraph display. Keep the existing native EDIT,
// its selection, scrollbar, keyboard interaction and scroll offset. Neither
// the UIA/OCR engines nor the normal word display use this subclass.
var (
	paragraphComctl            = syscall.NewLazyDLL("comctl32.dll")
	paragraphSetSubclass       = paragraphComctl.NewProc("SetWindowSubclass")
	paragraphRemoveSubclass    = paragraphComctl.NewProc("RemoveWindowSubclass")
	paragraphDefSubclass       = paragraphComctl.NewProc("DefSubclassProc")
	paragraphInvalidate        = u32.NewProc("InvalidateRect")
	paragraphFillRect          = u32.NewProc("FillRect")
	paragraphGetClientRect     = u32.NewProc("GetClientRect")
	paragraphCallback          uintptr
	paragraphSubclassInstalled bool
)

const paragraphSubclassID = 1

func paragraphDetailActive() bool {
	return expanded && !inList && selectedParagraph != "" && lookupDisplayStatus == ""
}

// Only install the scroll repaint hook while the paragraph view is in use.
// Returning to single-word lookup restores the original native EDIT procedure.
func syncParagraphDetailPaint() {
	if detail == 0 || paragraphDetailActive() == paragraphSubclassInstalled {
		return
	}
	if paragraphDetailActive() {
		if paragraphCallback == 0 {
			if paragraphSetSubclass.Find() != nil || paragraphRemoveSubclass.Find() != nil || paragraphDefSubclass.Find() != nil {
				return // opaque painting still applies if the optional hook is unavailable
			}
			paragraphCallback = syscall.NewCallback(paragraphDetailProc)
		}
		paragraphSubclassInstalled = call(paragraphSetSubclass, detail, paragraphCallback, paragraphSubclassID, 0) != 0
	} else {
		if call(paragraphRemoveSubclass, detail, paragraphCallback, paragraphSubclassID) != 0 {
			paragraphSubclassInstalled = false
		}
	}
	// Clear any pixels from the preceding mode without forcing synchronous paint.
	call(paragraphInvalidate, detail, 0, 1)
}

func paragraphDetailProc(hwnd uintptr, msg uint32, w, l, id, ref uintptr) uintptr {
	if msg == 0x0082 { // WM_NCDESTROY
		call(paragraphRemoveSubclass, hwnd, paragraphCallback, id)
		paragraphSubclassInstalled = false
		return call(paragraphDefSubclass, hwnd, uintptr(msg), w, l)
	}
	if !paragraphDetailActive() {
		return call(paragraphDefSubclass, hwnd, uintptr(msg), w, l)
	}
	if msg == wmEraseBkgnd && uiWhite != 0 {
		var r uiRect
		if call(paragraphGetClientRect, hwnd, uintptr(unsafe.Pointer(&r))) != 0 && call(paragraphFillRect, w, uintptr(unsafe.Pointer(&r)), uiWhite) != 0 {
			return 1
		}
	}
	result := call(paragraphDefSubclass, hwnd, uintptr(msg), w, l)
	// Let Windows update the native scroll position first. Then invalidate the
	// entire text client with erase enabled, instead of leaving copied pixels.
	// Invalidations coalesce in the message queue during fast thumb dragging.
	switch msg {
	case wmVScroll, wmMouseWheel, wmKeyDown, 0x00B5, 0x00B6, 0x00B7: // EM_SCROLL, EM_LINESCROLL, EM_SCROLLCARET
		call(paragraphInvalidate, hwnd, 0, 1)
	case wmMouseMove:
		if w&1 != 0 { // selection dragging may autoscroll
			call(paragraphInvalidate, hwnd, 0, 1)
		}
	}
	return result
}

// The parent is double-buffered but does not use WS_CLIPCHILDREN. Exclude the
// native paragraph EDIT from the parent's final blit (and direct-paint fallback),
// including the non-client scrollbar. Leave the word and saved views untouched.
func clipParagraphDetailFromParent(dc, parent uintptr) {
	if !paragraphDetailActive() || detail == 0 || !controlVisible[detail] {
		return
	}
	var r uiRect
	if call(u32.NewProc("GetWindowRect"), detail, uintptr(unsafe.Pointer(&r))) == 0 {
		return
	}
	call(u32.NewProc("MapWindowPoints"), 0, parent, uintptr(unsafe.Pointer(&r)), 2)
	call(gdi.NewProc("ExcludeClipRect"), dc, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right), uintptr(r.Bottom))
}
