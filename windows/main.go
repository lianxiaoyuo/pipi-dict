//go:build windows

package main

import (
	"fmt"
	"hoverdict/internal/bookmarkmeta"
	"hoverdict/internal/requestguard"
	"hoverdict/internal/wordextract"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	WM_MEASUREITEM    = 0x002C
	WVISIBLE          = 0x10000000
	WCHILD            = 0x40000000
	WBORDER           = 0x00800000
	WTOP              = 8
	WTOOL             = 0x80
	WPOP              = 0x80000000
	WCAP              = 0xC00000
	WSYS              = 0x80000
	WSCROLL           = 0x200000
	WMCOMMAND         = 0x111
	WMCLOSE           = 0x10
	WMDESTROY         = 2
	WMCLICK           = 0x201
	WMDRAG            = 0xA1
	WMHOTKEY          = 0x0312
	MODALT            = 0x0001
	MODCTRL           = 0x0002
	HKOPEN            = 777
	HKCLIP            = 778
	HKQUIT            = 779
	HKPARAGRAPH       = 780
	HKOPENLOGS        = 781
	CFUNICODETEXT     = 13
	HTCAPTION         = 2
	ENCHANGE          = 0x300
	BNCLICK           = 0
	LBRESET           = 0x184
	LBADD             = 0x180
	LBGETCURSEL       = 0x188 // LB_GETCURSEL, not LB_GETSEL (0x187)
	LBGETTEXTLEN      = 0x18A
	LBGETTEXT         = 0x189
	LBNDBLCLK         = 2
	WMQUERYENDSESSION = 0x0011
	WMENDSESSION      = 0x0016
)

var (
	u32              = syscall.NewLazyDLL("user32.dll")
	k32              = syscall.NewLazyDLL("kernel32.dll")
	procCreate       = u32.NewProc("CreateWindowExW")
	def              = u32.NewProc("DefWindowProcW")
	send             = u32.NewProc("SendMessageW")
	window           uintptr
	edit             uintptr
	detail           uintptr
	list             uintptr
	saveBtn          uintptr
	listBtn          uintptr
	bubbleBtn        uintptr
	autoEnabled      = true
	ignoreEditChange bool
	autoReady        bool
	expanded         = true
	inList           = false
	saved            = map[string]bool{}
	savefile         string
	saveStoreError   error
	metaStore        *sqliteMetaStore
	metaStoreError   error
	saveRecovered    bool
	requestGuard     *requestguard.Guard
	requestGuardErr  error
	dictionaryError  error
)

type Entry struct {
	Pron    string `json:"pron"`
	Meaning string `json:"meaning"`
}
type WNDCLASS struct {
	Size, Style                        uint32
	Proc                               uintptr
	ExtraA, ExtraB                     int32
	Instance, Icon, Cursor, Background uintptr
	Menu, Name                         *uint16
	Small                              uintptr
}
type MSG struct {
	Wnd           uintptr
	Message       uint32
	W, L          uintptr
	Time          uint32
	X, Y, Private int32
}

func ptr(s string) uintptr                              { return uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(s))) }
func call(p *syscall.LazyProc, args ...uintptr) uintptr { r, _, _ := p.Call(args...); return r }
func ctrl(kind, title string, x, y, w, h int, id uintptr, extra uintptr) uintptr {
	if kind == "BUTTON" {
		extra |= 0x0B
	} // BS_OWNERDRAW: rounded native buttons
	return call(procCreate, 0, ptr(kind), ptr(title), WCHILD|WVISIBLE|extra, uintptr(x), uintptr(y), uintptr(w), uintptr(h), window, id, 0, 0)
}
func set(h uintptr, s string) { call(u32.NewProc("SetWindowTextW"), h, ptr(s)) }
func get(h uintptr) string {
	n := call(u32.NewProc("GetWindowTextLengthW"), h)
	b := make([]uint16, n+2)
	call(u32.NewProc("GetWindowTextW"), h, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)))
	return syscall.UTF16ToString(b)
}
func norm(s string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(s), " \n\r.,!?;:\"'()[]"))
}
func showSaveError(err error) {
	if err == nil {
		return
	}
	call(u32.NewProc("MessageBoxW"), window, ptr("生词没有保存成功："+err.Error()+"\n请检查磁盘空间、目录权限或生词文件。"), ptr("HoverDict 生词本"), 0x10)
}
func validSaveWord(word string) bool {
	return wordextract.IsSingleWord(word)
}
func lookup(s string) string {
	s = norm(s)
	if s == "" {
		return ""
	}
	if e, ok := getEntry(s); ok && strings.TrimSpace(e.Meaning) != "" {
		return s + "\r\n【本地 ECDICT】 " + e.Pron + "\r\n\r\n" + e.Meaning
	}
	return onlineDisplay(s)
}
func draw() {
	if !expanded {
		return
	}
	syncParagraphDetailPaint()
	if inList {
		setLabel(headWord, "")
		setLabel(headPron, "")
		setLabel(headOrigin, "")
		showExpandedControls(true)
		rebuildSavedList()
		setLabel(listBtn, "返回查词")
		setLabel(saveBtn, "删除所选")
		_, selectErr := selectedSavedWord()
		call(u32.NewProc("EnableWindow"), saveBtn, boolUintptr(selectErr == nil))
	} else if lookupDisplayStatus != "" {
		showExpandedControls(true)
		statusHeading := "取词未成功"
		if strings.HasPrefix(lookupDisplayStatus, "正在") {
			statusHeading = "正在识别"
		}
		setLabel(headWord, statusHeading)
		setControlFont(headWord, uiTitleFont)
		setLabel(headPron, "")
		setLabel(headOrigin, "自动取词")
		setLabel(detail, lookupDisplayStatus)
		setLabel(saveBtn, "不可收藏")
		call(u32.NewProc("EnableWindow"), saveBtn, 0)
		setLabel(listBtn, "生词本")
	} else if selectedParagraph != "" {
		showExpandedControls(true)
		setLabel(headWord, "划词翻译")
		setControlFont(headWord, uiTitleFont)
		setLabel(headPron, "")
		setLabel(headOrigin, "选中段落")
		translated := "翻译中…"
		if paragraphMeaning != "" {
			translated = paragraphMeaning
		}
		if paragraphError != "" {
			translated = "联网翻译失败：" + paragraphError
		}
		setLabel(detail, "原文：\r\n"+selectedParagraph+"\r\n\r\n中文：\r\n"+translated)
		setLabel(saveBtn, "不可收藏段落")
		call(u32.NewProc("EnableWindow"), saveBtn, 0)
		setLabel(listBtn, "生词本")
	} else {
		showExpandedControls(true)
		word := norm(get(edit))
		if word == "" {
			setLabel(headWord, "")
			setLabel(headPron, "")
			setLabel(headOrigin, "")
			setLabel(detail, lookup(word))
		} else if e, ok := getEntry(word); ok && strings.TrimSpace(e.Meaning) != "" {
			setLabel(headWord, word)
			setControlFont(headWord, uiWordFont)
			if len([]rune(word)) > 17 {
				setControlFont(headWord, uiWordCompactFont)
			}
			pron := e.Pron
			if pron == "" && metaStore != nil {
				if r, ok := metaStore.Get(word); ok {
					pron = r.Pron
				}
			}
			setLabel(headPron, pron)
			setLabel(headOrigin, "离线")
			setLabel(detail, cleanMeaning(e.Meaning))
		} else {
			setLabel(headWord, word)
			setControlFont(headWord, uiWordFont)
			if len([]rune(word)) > 17 {
				setControlFont(headWord, uiWordCompactFont)
			}
			pron := ""
			if metaStore != nil {
				if r, ok := metaStore.Get(word); ok {
					pron = r.Pron
				}
			}
			setLabel(headPron, pron)
			setLabel(headOrigin, "在线")
			translation := onlineDisplay(word)
			if metaStore != nil {
				if r, ok := metaStore.Get(word); ok && r.Meaning != "" {
					setLabel(headOrigin, "已收藏")
					translation = r.Meaning
				}
			}
			setLabel(detail, translation)
		}
		setLabel(listBtn, "生词本")
		call(u32.NewProc("EnableWindow"), saveBtn, boolUintptr(word != ""))
		if saved[word] {
			setLabel(saveBtn, "取消收藏")
		} else {
			setLabel(saveBtn, "收藏")
		}
	}
	showExpandedControls(true)
	reflowControls()
	call(u32.NewProc("InvalidateRect"), window, 0, 0)
}
func boolUintptr(v bool) uintptr {
	if v {
		return 1
	}
	return 0
}
func cleanMeaning(s string) string {
	s = strings.ReplaceAll(s, `\n`, "\r\n")
	s = strings.ReplaceAll(s, "\n", "\r\n")
	s = strings.ReplaceAll(s, "\r\r\n", "\r\n")
	return strings.TrimSpace(s)
}
func resize(exp bool) {
	if window == 0 {
		return
	}
	// Store the current user placement before switching panel <-> mascot.
	r := currentWindowRect()
	if expanded {
		layoutState.PanelX, layoutState.PanelY = r.Left, r.Top
		if r.Right > r.Left && r.Bottom > r.Top {
			layoutState.PanelW, layoutState.PanelH = r.Right-r.Left, r.Bottom-r.Top
		}
	} else {
		layoutState.BubbleX, layoutState.BubbleY = r.Left, r.Top
	}
	expanded = exp
	width, height := uintptr(layoutState.PanelW), uintptr(layoutState.PanelH)
	if !exp {
		width, height = uintptr(layoutState.BubbleSize), uintptr(layoutState.BubbleSize)
	}
	style := uintptr(WPOP | WVISIBLE)
	if exp {
		style = uintptr(WPOP | WSYS | WVISIBLE | wThickFrame)
	}
	call(u32.NewProc("SetWindowLongPtrW"), window, ^uintptr(15), style)
	// WS_EX_LAYERED + colorkey ensures a transparent, truly circular mascot.
	// Remove the layered flag when opening the normal resizable panel.
	exStyle := uintptr(wExToolWindow)
	if !exp {
		exStyle |= wExLayered
	}
	call(u32.NewProc("SetWindowLongPtrW"), window, ^uintptr(19), exStyle)
	showExpandedControls(exp)
	if exp {
		call(u32.NewProc("SetWindowRgn"), window, 0, 1)
	}
	x, y := positionFor(exp)
	// Relocation only happens when USER opens/collapses their pinned UI.
	// UIA text clicks never change either remembered position.
	call(u32.NewProc("SetWindowPos"), window, ^uintptr(0), uintptr(x), uintptr(y), width, height, swpFrameChanged|swpNoActivate)
	configureWindowComposition(exp)
	if !exp {
		call(u32.NewProc("SetLayeredWindowAttributes"), window, uintptr(rgb(1, 2, 3)), 0, lwaColorKey)
		// Clip the floating bubble to panda ears + face, not a colored disc.
		refreshBubbleShape()
	}
	if exp {
		reflowControls()
		draw()
	}
	call(u32.NewProc("InvalidateRect"), window, 0, 1)
	saveLayout()
}
func selectedSavedWord() (string, error) {
	if !inList || !saved[savedViewSelected] || !validSaveWord(savedViewSelected) {
		return "", fmt.Errorf("请先选择一个生词")
	}
	return savedViewSelected, nil
}
func handler(h uintptr, m uint32, w, l uintptr) uintptr {
	switch m {
	case wmWindowPosChanged:
		// Run DefWindowProc first so WM_MOVE / WM_SIZE are propagated, then
		// publish bounds for the lock-free low-level mouse hook callback.
		result := call(def, h, uintptr(m), w, l)
		publishHookWindowBounds()
		return result
	case wmNCHitTest:
		if expanded {
			if ht := panelHitTest(cursorPoint(), currentWindowRect()); ht != 0 {
				return ht
			}
		}
	case wmSetCursor:
		if !expanded {
			p := cursorPoint()
			call(u32.NewProc("ScreenToClient"), window, uintptr(unsafe.Pointer(&p)))
			cursorID := uintptr(32649) // IDC_HAND for moving the panda
			cur := call(u32.NewProc("LoadCursorW"), 0, cursorID)
			if cur != 0 {
				call(u32.NewProc("SetCursor"), cur)
				return 1
			}
		}
	case wmLeftDown:
		if !expanded {
			beginBubbleDrag(l)
			return 0
		}
	case wmMouseMove:
		if !expanded {
			x, y := int(int16(uint16(l))), int(int16(uint16(l>>16)))
			updateBubbleHover(x, y)
			if dragging {
				moveBubble()
			}
			return 0
		}
	case wmMouseLeave:
		mouseTracking = false
		if hoverGrip {
			hoverGrip = false
			call(u32.NewProc("InvalidateRect"), window, 0, 0)
		}
		return 0
	case wmLeftButtonUp:
		if dragging {
			endBubbleDrag()
			return 0
		}
	case wmCaptureChanged:
		if dragging {
			cancelBubbleDrag()
		}
	case wmGetMinMaxInfo:
		if expanded && l != 0 {
			setMinimumPanelSize(l)
		}
		return 0
	case wmSize:
		if expanded {
			reflowControls()
			call(u32.NewProc("InvalidateRect"), h, 0, 1)
		}
		return 0
	case wmMouseWheel:
		if expanded && inList {
			scrollSavedWheel(w)
			return 0
		}
		if !expanded {
			return 0
		}
	case wmRightButtonUp:
		if !expanded {
			resize(true)
			return 0
		}
	case wmExitSizeMove:
		if expanded {
			r := currentWindowRect()
			layoutState.PanelW, layoutState.PanelH = r.Right-r.Left, r.Bottom-r.Top
			resizeSavedRows()
		}
		saveLayout()
	case wmPaint:
		paintUI(h)
		return 0
	case wmEraseBkgnd:
		return 1
	case wmDrawItem:
		drawModernButton(l)
		return 1
	case wmColorStatic, wmColorEdit, wmColorList:
		return paintControl(m, w, l)
	case wmLookupStatus:
		// Keep failure diagnostics in the local log, not as permanent UI clutter.
		return 0
	case WMQUERYENDSESSION:
		saveLayout()
		return 1
	case WMENDSESSION:
		if w != 0 {
			saveLayout()
		}
		return 0
	case WMCLOSE:
		resize(false)
		return 0
	case WMDESTROY:
		hookBounds.Store(nil)
		clearTheme()
		clearOnline(true)
		if dictionaryDB != nil {
			dictionaryDB.Close()
		}
		if userDB != nil {
			_ = userDB.exec("PRAGMA wal_checkpoint(TRUNCATE)")
			userDB.Close()
		}
		uninstallAutoClick()
		call(u32.NewProc("UnregisterHotKey"), h, HKOPEN)
		call(u32.NewProc("UnregisterHotKey"), h, HKCLIP)
		call(u32.NewProc("UnregisterHotKey"), h, HKQUIT)
		call(u32.NewProc("UnregisterHotKey"), h, HKPARAGRAPH)
		call(u32.NewProc("UnregisterHotKey"), h, HKOPENLOGS)
		call(u32.NewProc("PostQuitMessage"), 0)
		return 0
	case wmShowRunning:
		resize(true)
		call(u32.NewProc("ShowWindow"), h, 5)
		call(u32.NewProc("SetForegroundWindow"), h)
		return 0
	case wmAutoLookupStarted:
		beginLookupPending(uint64(w))
		return 0
	case wmAutoLookup:
		acceptAutoLookup()
		return 0
	case wmOnlineDone:
		acceptOnlineResponse()
	case wmPronDone:
		acceptMissingIPA()
		return 0
	case wmTimer:
		if w == lookupStatusTimerID {
			showLookupPending()
		} else if w == onlineTimerID {
			onOnlineTimer()
		}
		return 0
	case WMHOTKEY:
		if w == HKOPENLOGS {
			openDiagnosticsDirectory()
			return 0
		}
		if w == HKQUIT {
			call(u32.NewProc("DestroyWindow"), h)
			return 0
		}
		if w == HKOPEN {
			resize(true)
			call(u32.NewProc("SetForegroundWindow"), h)
			call(u32.NewProc("SetFocus"), edit)
		}
		if w == HKPARAGRAPH {
			text := clipboardText()
			if text != "" {
				startParagraphTranslation(text)
			}
			return 0
		}
		if w == HKCLIP {
			word := clipboard()
			if word != "" {
				set(edit, word)
				inList = false
			}
			resize(true)
			draw()
			call(u32.NewProc("SetForegroundWindow"), h)
		}
		return 0

	case WMCOMMAND:
		id, evt := w&65535, (w>>16)&65535
		// A deletion is enabled only after a concrete word is selected.
		if inList && id == 106 && evt == 1 { // LBN_SELCHANGE
			_, selectedErr := selectedSavedWord()
			call(u32.NewProc("EnableWindow"), saveBtn, boolUintptr(selectedErr == nil))
			return 0
		}
		// Open a saved word's meaning with a double click. No separate cluttered UI.
		if inList && id == 106 && evt == LBNDBLCLK {
			word, err := selectedSavedWord()
			if err == nil {
				inList = false
				set(edit, word)
				onWordEdited()
				reflowControls()
				draw()
				call(u32.NewProc("InvalidateRect"), window, 0, 1)
			}
			return 0
		}
		if id == 101 && evt == ENCHANGE {
			if ignoreEditChange {
				return 0
			}
			lookupDisplayStatus = ""
			stopLookupPending()
			onWordEdited()
			if !inList {
				draw()
			}
			return 0
		}
		if evt == BNCLICK {
			switch id {
			case 102:
				if saveStoreError != nil {
					showSaveError(saveStoreError)
					return 0
				}
				if userDB == nil {
					showSaveError(fmt.Errorf("SQLite 生词数据库未打开"))
					return 0
				}
				word := ""
				if inList {
					selected, selectionErr := selectedSavedWord()
					if selectionErr != nil {
						showSaveError(selectionErr)
						return 0
					}
					word = selected
				} else {
					if selectedParagraph != "" {
						return 0
					}
					word = norm(get(edit))
					if !validSaveWord(word) {
						showSaveError(fmt.Errorf("请先输入有效的单词"))
						return 0
					}
				}
				existing := saved[word]
				rec := bookmarkmeta.Record{SavedAt: time.Now(), Source: "ECDICT"}
				if e, ok := getEntry(word); ok {
					rec.Pron = e.Pron
					rec.Meaning = cleanMeaning(e.Meaning)
				}
				if rec.Meaning == "" && onlineWord == word {
					rec.Meaning = onlineMeaning
					rec.Source = "MyMemory（用户主动收藏）"
				}
				if err := userDB.toggle(word, rec, inList); err != nil {
					showSaveError(err)
					return 0
				}
				saved, savedOrder, saveStoreError = userDB.savedWords()
				if saveStoreError != nil {
					showSaveError(saveStoreError)
					return 0
				}
				if !inList && !existing && rec.Pron == "" {
					requestMissingIPA(word)
				}
				draw()

			case 103:
				lookupDisplayStatus = ""
				stopLookupPending()
				inList = !inList
				reflowControls()
				call(u32.NewProc("InvalidateRect"), window, 0, 1)
				if !inList {
					onWordEdited()
				}
				draw()
			case 104:
				resize(false)
			case 105:
				resize(true)
			}
			return 0
		}
	}
	return call(def, h, uintptr(m), w, l)
}
func clipboardText() string {
	if call(u32.NewProc("OpenClipboard"), window) == 0 {
		return ""
	}
	defer call(u32.NewProc("CloseClipboard"))
	h := call(u32.NewProc("GetClipboardData"), CFUNICODETEXT)
	if h == 0 {
		return ""
	}
	p := call(k32.NewProc("GlobalLock"), h)
	if p == 0 {
		return ""
	}
	defer call(k32.NewProc("GlobalUnlock"), h)
	n := call(k32.NewProc("GlobalSize"), h) / 2
	if n > 4000 {
		n = 4000
	}
	if n == 0 {
		return ""
	}
	text := syscall.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(p)), int(n)))
	if len([]byte(text)) > 3000 {
		return ""
	}
	return strings.TrimSpace(text)
}
func clipboard() string {
	if call(u32.NewProc("OpenClipboard"), window) == 0 {
		return ""
	}
	defer call(u32.NewProc("CloseClipboard"))
	h := call(u32.NewProc("GetClipboardData"), CFUNICODETEXT)
	if h == 0 {
		return ""
	}
	p := call(k32.NewProc("GlobalLock"), h)
	if p == 0 {
		return ""
	}
	defer call(k32.NewProc("GlobalUnlock"), h)
	n := call(k32.NewProc("GlobalSize"), h) / 2
	if n > 4096 {
		n = 4096
	}
	if n == 0 {
		return ""
	}
	chars := unsafe.Slice((*uint16)(unsafe.Pointer(p)), int(n))
	word := norm(syscall.UTF16ToString(chars))
	if !wordextract.IsSingleWord(word) {
		return ""
	}
	return word
}
func initData() {
	dir, _ := os.UserConfigDir()
	p := filepath.Join(dir, "HoverDict")
	if err := os.MkdirAll(p, 0700); err != nil {
		saveStoreError = err
		return
	}
	savefile = filepath.Join(p, "saved.json") // legacy: never deleted or overwritten
	licensePath := filepath.Join(p, "ECDICT_LICENSE.txt")
	if _, err := os.Stat(licensePath); os.IsNotExist(err) {
		_ = os.WriteFile(licensePath, []byte(dictionaryLicense), 0644)
	}
	requestGuard, requestGuardErr = requestguard.Open(filepath.Join(p, "freeonline_usage.json"))
	userDB, saveStoreError = openUserSQLite(filepath.Join(p, "user.db"))
	loadLayout(p)
	dictionaryDB, dictionaryError = openDictionary(p)
	if saveStoreError == nil {
		saved, savedOrder, saveStoreError = userDB.savedWords()
		if saveStoreError == nil {
			metaStore = &sqliteMetaStore{db: userDB}
		}
	}
}

func enableDPIAwareness() {
	aware := u32.NewProc("SetProcessDpiAwarenessContext")
	if aware.Find() == nil && call(aware, ^uintptr(3)) != 0 { // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
		return
	}
	call(u32.NewProc("SetProcessDPIAware"))
}

func main() {
	// Both the GUI and its UIA helper MUST use the same physical-pixel DPI coordinates.
	// In V3.3 the helper exited before DPI initialization, breaking hit tests on scaled displays.
	enableDPIAwareness()
	if maybeRunUIAChild() {
		return
	}
	runtime.LockOSThread()
	appLock, existing, lockErr := acquireAppInstance()
	if existing {
		return
	}
	if lockErr != nil {
		showStartupError(lockErr)
		return
	}
	defer appLock.Close()
	initDiagnostics()
	initUIScale()
	initData()
	logDiagnostic("dictionary", fmt.Sprintf("sqlite_open=%v", dictionaryDB != nil))
	name := syscall.StringToUTF16Ptr("HoverDictNative1")
	inst := call(k32.NewProc("GetModuleHandleW"), 0)
	wc := WNDCLASS{Size: uint32(unsafe.Sizeof(WNDCLASS{})), Proc: syscall.NewCallback(handler), Instance: inst, Name: name}
	call(u32.NewProc("RegisterClassExW"), uintptr(unsafe.Pointer(&wc)))
	startX, startY := windowStartPosition()
	window = call(procCreate, WTOP|WTOOL, uintptr(unsafe.Pointer(name)), ptr("Pipi Dict V5.6.1"), WPOP|WSYS|WVISIBLE|wThickFrame, uintptr(startX), uintptr(startY), uintptr(layoutState.PanelW), uintptr(layoutState.PanelH), 0, 0, inst, 0)
	if window == 0 {
		return
	}
	buildModernControls()
	initializeStartupDefault()
	resize(false) // Start as a draggable panda; opens at the user-pinned panel position.
	publishHookWindowBounds()
	call(u32.NewProc("RegisterHotKey"), window, HKOPEN, MODALT|MODCTRL, uintptr('D'))
	call(u32.NewProc("RegisterHotKey"), window, HKCLIP, MODALT|MODCTRL, uintptr('W'))
	call(u32.NewProc("RegisterHotKey"), window, HKQUIT, MODALT|MODCTRL, uintptr('Q'))
	call(u32.NewProc("RegisterHotKey"), window, HKPARAGRAPH, MODALT|MODCTRL, uintptr('T'))
	call(u32.NewProc("RegisterHotKey"), window, HKOPENLOGS, MODALT|MODCTRL, uintptr('L'))
	draw()
	autoReady = installAutoClick()
	logDiagnostic("mouse_hook", fmt.Sprintf("ready=%v", autoReady))
	if !autoReady {
		autoEnabled = false
	}

	if dictionaryError != nil {
		call(u32.NewProc("MessageBoxW"), window, ptr("本地词库无法打开："+dictionaryError.Error()), ptr("Pipi Dict"), 0x30)
	}
	if saveStoreError != nil {
		call(u32.NewProc("MessageBoxW"), window, ptr("生词文件存在问题，已禁止继续写入，避免覆盖原有数据。\n"+saveStoreError.Error()), ptr("HoverDict - 请先备份生词本"), 0x30)
	} else if saveRecovered {
		call(u32.NewProc("MessageBoxW"), window, ptr("检测到生词主文件损坏或缺失，已从 .bak 备份恢复到内存；下一次成功收藏时会重新保存。"), ptr("HoverDict - 自动恢复"), 0x40)
	}
	msg := MSG{}
	for {
		r := call(u32.NewProc("GetMessageW"), uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		call(u32.NewProc("TranslateMessage"), uintptr(unsafe.Pointer(&msg)))
		call(u32.NewProc("DispatchMessageW"), uintptr(unsafe.Pointer(&msg)))
	}
}
