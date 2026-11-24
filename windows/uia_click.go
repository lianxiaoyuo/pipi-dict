//go:build windows

package main

// System-wide click lookup, without code injection or clipboard interception.
// UIA/TextPattern/RangeFromPoint only works when the target application exposes
// accessible text. Local OCR is a bounded fallback for verified text controls.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"hoverdict/internal/helperproc"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"hoverdict/internal/clickgesture"
	"hoverdict/internal/clicktext"
	"hoverdict/internal/fastclick"
	"hoverdict/internal/ocrclick"
	"hoverdict/internal/textsegments"
	"hoverdict/internal/wordextract"
)

const (
	whMouseLL           = 14
	wmLeftUp            = 0x0202
	wmLeftDown          = 0x0201
	wmWindowPosChanged  = 0x0047
	wmAutoLookup        = 0x8001 // WM_APP + 1
	wmAutoLookupStarted = 0x8005 // user input acknowledged asynchronously
	gaRoot              = 2
	uiaTextPattern      = 10014
	uiaControlEdit      = 50004
	uiaControlButton    = 50000
	uiaControlText      = 50020
	uiaControlHyperlink = 50005
	uiaControlMenuItem  = 50011
	textUnitWord        = 2
	coInitMultiThreaded = 0
	clsctxInprocServer  = 1
)

type clickRequest struct {
	at           point
	seq          uint64
	selectedOnly bool
}
type point struct{ X, Y int32 }
type msllHook struct {
	Point     point
	MouseData uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}
type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var (
	ole32              = syscall.NewLazyDLL("ole32.dll")
	oleaut             = syscall.NewLazyDLL("oleaut32.dll")
	clickHook          uintptr // owned exclusively by the dedicated hook thread
	clickCb            = syscall.NewCallback(mouseClickHook)
	procCallNextHookEx = u32.NewProc("CallNextHookEx") // cached: hook callback must not allocate a LazyProc on every event
	procPostMessage    = u32.NewProc("PostMessageW")
	hookThreadID       atomic.Uint32
	hookStop           context.CancelFunc
	hookDone           chan struct{}
	workerStop         context.CancelFunc
	clicks             = make(chan clickRequest, 1)
	clickCounter       atomic.Uint64
	hookBounds         atomic.Pointer[winRect]
	hookEvents         atomic.Uint64
	hookRearms         atomic.Uint64
	lastHookLog        uint64
	// Periodically recycle COM apartments instead of keeping potentially stale
	// UIA provider references alive for hundreds of clicks.
	uiaClients         = [2]*helperproc.Manager{helperproc.New(newUIAHelperCommand, 64), helperproc.New(newUIAHelperCommand, 64)}
	mouseDown          point
	mouseDownAt        uint32
	downTracked        bool
	startedOnOurWindow bool
	resultMu           sync.Mutex
	resultWord         string
	resultParagraph    string
	resultStatus       string
	resultAt           point
	resultSeq          uint64
)
var (
	clsidCUIAutomation = guid{0xff48dba4, 0x60ef, 0x4201, [8]byte{0xaa, 0x87, 0x54, 0x10, 0x3e, 0xef, 0x59, 0x4e}}
	iidIUIAutomation   = guid{0x30cbe57d, 0xd9d0, 0x452a, [8]byte{0xab, 0x13, 0x7a, 0xc5, 0xac, 0x48, 0x25, 0xee}}
	// IID_IUIAutomationTextPattern: resolve the actual interface explicitly.
	// GetCurrentPattern returns IUnknown, not necessarily a typed TextPattern pointer.
	iidTextPattern = guid{0x32eba289, 0x3583, 0x42c9, [8]byte{0x9c, 0x59, 0x3b, 0x6d, 0x9a, 0x1e, 0x9b, 0x6a}}
)

func comcall(obj uintptr, index uintptr, args ...uintptr) bool {
	if obj == 0 {
		return false
	}
	vt := *(*uintptr)(unsafe.Pointer(obj))
	fn := *(*uintptr)(unsafe.Pointer(vt + index*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{obj}, args...)...)
	return int32(r) >= 0
}
func release(obj uintptr) {
	if obj != 0 {
		comcall(obj, 2)
	}
}
func packPoint(p point) uintptr { return uintptr(uint64(uint32(p.X)) | uint64(uint32(p.Y))<<32) }

func mouseClickHook(code int32, msg, info uintptr) uintptr {
	if code >= 0 && info != 0 && window != 0 && autoEnabled {
		m := (*msllHook)(unsafe.Pointer(info))
		if m.Flags&1 == 0 { // ignore injected mouse clicks
			if msg == wmLeftDown {
				mouseDown = m.Point
				mouseDownAt = m.Time
				downTracked = true
				// Never query other applications from a WH_MOUSE_LL callback.
				// That can block the installing thread and cause Windows to silently
				// remove the global hook after LowLevelHooksTimeout.
				// The low-level hook MUST NOT call GetWindowRect or any other
				// synchronous function that can wait on the GUI thread.
				// UI publishes immutable bounds when Windows moves/resizes it.
				bounds := hookBounds.Load()
				startedOnOurWindow = bounds != nil && m.Point.X >= bounds.Left && m.Point.X < bounds.Right && m.Point.Y >= bounds.Top && m.Point.Y < bounds.Bottom
			}
			if msg == wmLeftUp {
				hookEvents.Add(1)
				if downTracked {
					dx, dy := abs32(m.Point.X-mouseDown.X), abs32(m.Point.Y-mouseDown.Y)
					durationMS := m.Time - mouseDownAt // uint32 subtraction survives tick wraparound
					kind := clickgesture.Classify(dx, dy, durationMS)
					if !startedOnOurWindow && kind != clickgesture.Ignore {
						seq := clickCounter.Add(1)
						location := m.Point
						selectDrag := kind == clickgesture.Selection
						if selectDrag {
							location = point{(m.Point.X + mouseDown.X) / 2, (m.Point.Y + mouseDown.Y) / 2}
						}
						req := clickRequest{location, seq, selectDrag}
						// Latest-click wins; never block the low-level hook thread.
						select {
						case clicks <- req:
						default:
							select {
							case <-clicks:
							default:
							}
							select {
							case clicks <- req:
							default:
							}
						}
						// Async UI acknowledgement. No SendMessage or COM in hook.
						call(procPostMessage, window, wmAutoLookupStarted, uintptr(seq), 0)
					} else if startedOnOurWindow {
						// User manipulated the panel; a stale external result must not
						// overwrite their work. Ignored pointer jitter does not do this.
						clickCounter.Add(1)
					}
				}
				downTracked = false
				startedOnOurWindow = false
			}
		}
	}
	return call(procCallNextHookEx, clickHook, uintptr(code), msg, info)
}

// Must be called from the UI thread (never from the mouse hook callback).
// Publishing one immutable pointer keeps the hook callback bounded and lock-free.
func publishHookWindowBounds() {
	if window == 0 {
		hookBounds.Store(nil)
		return
	}
	var rect winRect
	if call(u32.NewProc("GetWindowRect"), window, uintptr(unsafe.Pointer(&rect))) != 0 {
		hookBounds.Store(&rect)
	}
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// WH_MOUSE_LL callbacks are delivered on the installing thread. The main
// window thread can be busy painting, processing SQLite, or receiving COM
// notifications; putting the hook there causes Windows to remove it silently.
// This worker owns its own message pump and only records click coordinates.
func hookMessageLoop(ctx context.Context, ready chan<- bool, done chan<- struct{}) {
	defer close(done)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var msg MSG
	call(u32.NewProc("PeekMessageW"), uintptr(unsafe.Pointer(&msg)), 0, 0, 0, 0)
	tid := uint32(call(k32.NewProc("GetCurrentThreadId")))
	hookThreadID.Store(tid)
	clickHook = call(u32.NewProc("SetWindowsHookExW"), whMouseLL, clickCb, 0, 0)
	ready <- clickHook != 0
	if clickHook == 0 {
		hookThreadID.Store(0)
		return
	}
	// Windows does not notify an app when it silently drops a slow hook.
	// Re-arm periodically to recover without requiring a restart.
	const watchdogID = 0x5049
	call(u32.NewProc("SetTimer"), 0, watchdogID, 12000, 0)
	defer func() {
		call(u32.NewProc("KillTimer"), 0, watchdogID)
		if clickHook != 0 {
			call(u32.NewProc("UnhookWindowsHookEx"), clickHook)
			clickHook = 0
		}
		hookThreadID.Store(0)
	}()
	for ctx.Err() == nil {
		r := int32(call(u32.NewProc("GetMessageW"), uintptr(unsafe.Pointer(&msg)), 0, 0, 0))
		if r <= 0 {
			return
		}
		if msg.Message == wmTimer && msg.W == watchdogID {
			// An active, responsive hook must not be torn down repeatedly.
			// Re-arm only after an entire watchdog interval with no delivered
			// events (we cannot distinguish an idle mouse from a removed hook).
			// This prevents re-registration gaps while the user is reading.
			events := hookEvents.Load()
			if events != lastHookLog {
				lastHookLog = events
				continue
			}
			if clickHook != 0 {
				call(u32.NewProc("UnhookWindowsHookEx"), clickHook)
			}
			clickHook = call(u32.NewProc("SetWindowsHookExW"), whMouseLL, clickCb, 0, 0)
			hookRearms.Add(1)
			if clickHook == 0 {
				go logDiagnostic("mouse_hook", "idle_rearm_failed")
			}
		}
	}
}

// Unexpected message-pump exit is supervised too: recovery does not depend
// solely on the re-arm timer inside the same potentially terminated thread.
func superviseHookLoop(ctx context.Context, ready chan<- bool, done chan<- struct{}) {
	defer close(done)
	first := true
	for ctx.Err() == nil {
		attemptReady := make(chan bool, 1)
		attemptDone := make(chan struct{})
		go hookMessageLoop(ctx, attemptReady, attemptDone)
		ok := <-attemptReady
		if first {
			ready <- ok
			first = false
		}
		<-attemptDone
		if ctx.Err() != nil || !ok {
			return
		}
		logDiagnostic("mouse_hook", "message_pump_ended_restarting")
		select {
		case <-ctx.Done():
			return
		case <-time.After(180 * time.Millisecond):
		}
	}
}

func installAutoClick() bool {
	ctx, cancel := context.WithCancel(context.Background())
	hookStop = cancel
	hookDone = make(chan struct{})
	ready := make(chan bool, 1)
	go superviseHookLoop(ctx, ready, hookDone)
	select {
	case ok := <-ready:
		if !ok {
			cancel()
			return false
		}
		workerCtx, stop := context.WithCancel(context.Background())
		workerStop = stop
		go uiaClickWorker(workerCtx)
		go warmOCR(workerCtx)
		return true
	case <-time.After(3 * time.Second):
		cancel()
		return false
	}
}
func uninstallAutoClick() {
	if workerStop != nil {
		workerStop()
		workerStop = nil
	}
	for _, client := range uiaClients {
		client.Close()
	}
	ocrManager.Close()
	if hookStop != nil {
		hookStop()
		hookStop = nil
	}
	if tid := hookThreadID.Load(); tid != 0 {
		call(u32.NewProc("PostThreadMessageW"), uintptr(tid), 0x0012, 0, 0) // WM_QUIT
	}
	if hookDone != nil {
		select {
		case <-hookDone:
		case <-time.After(500 * time.Millisecond):
		}
		hookDone = nil
	}
}

// Run UI Automation in a short-lived child process of the SAME EXE.
// Some app accessibility providers block indefinitely inside COM. The parent
// enforces a deadline by terminating ONLY the child, never freezing the GUI or
// permanently poisoning the next click. This avoids an extra installed helper.
type uiaRPCRequest struct {
	Mode string `json:"mode"`
	X    int32  `json:"x"`
	Y    int32  `json:"y"`
}
type uiaRPCResponse struct {
	Text      string   `json:"text"`
	Status    string   `json:"status"`
	OCRBounds *winRect `json:"ocr_bounds,omitempty"`
}

func newUIAHelperCommand() *exec.Cmd {
	exe, err := os.Executable()
	if err != nil {
		exe = os.Args[0]
	}
	cmd := exec.Command(exe, "--uia-server")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.WaitDelay = 300 * time.Millisecond
	return cmd
}

// One supervised helper is reused for consecutive clicks. This avoids
// repeatedly launching and antivirus-scanning a ~27 MB GUI EXE. A hung COM
// provider is killed at the parent deadline and replaced for the next click.
func runUIAServer() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 2048), 8192)
	encoder := json.NewEncoder(os.Stdout)
	hr, _, _ := ole32.NewProc("CoInitializeEx").Call(0, coInitMultiThreaded)
	if int32(hr) < 0 {
		for scanner.Scan() {
			_ = encoder.Encode(uiaRPCResponse{Status: "com_init_failed"})
		}
		return
	}
	defer ole32.NewProc("CoUninitialize").Call()
	var automation uintptr
	hr, _, _ = ole32.NewProc("CoCreateInstance").Call(
		uintptr(unsafe.Pointer(&clsidCUIAutomation)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIUIAutomation)), uintptr(unsafe.Pointer(&automation)))
	if int32(hr) < 0 || automation == 0 {
		for scanner.Scan() {
			_ = encoder.Encode(uiaRPCResponse{Status: "automation_unavailable"})
		}
		return
	}
	defer release(automation)
	for scanner.Scan() {
		req := uiaRPCRequest{}
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			_ = encoder.Encode(uiaRPCResponse{Status: "invalid_request"})
			continue
		}
		p := point{req.X, req.Y}
		var text, status string
		var bounds *winRect
		switch req.Mode {
		case "query":
			text, status = uiaWordFromPointWithBounds(automation, p, false, &bounds)
		case "selected":
			text, status = uiaWordFromPoint(automation, p, true)
		case "paragraph":
			text, status = uiaSelectedParagraphFromPoint(automation, p)
		default:
			status = "invalid_request"
		}
		if err := encoder.Encode(uiaRPCResponse{Text: text, Status: status, OCRBounds: bounds}); err != nil {
			return
		}
	}
}

func maybeRunUIAChild() bool {
	if len(os.Args) == 2 && os.Args[1] == "--uia-server" {
		runUIAServer()
		return true
	}
	if len(os.Args) != 4 || (os.Args[1] != "--uia-query" && os.Args[1] != "--uia-selected" && os.Args[1] != "--uia-paragraph") {
		return false
	}
	selectionOnly := os.Args[1] == "--uia-selected"
	x, ex := strconv.ParseInt(os.Args[2], 10, 32)
	y, ey := strconv.ParseInt(os.Args[3], 10, 32)
	if ex != nil || ey != nil {
		return true
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hr, _, _ := ole32.NewProc("CoInitializeEx").Call(0, coInitMultiThreaded)
	if int32(hr) < 0 {
		fmt.Fprint(os.Stdout, "#com_init_failed")
		return true
	}
	defer ole32.NewProc("CoUninitialize").Call()
	var automation uintptr
	hr, _, _ = ole32.NewProc("CoCreateInstance").Call(
		uintptr(unsafe.Pointer(&clsidCUIAutomation)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIUIAutomation)), uintptr(unsafe.Pointer(&automation)))
	if int32(hr) < 0 || automation == 0 {
		fmt.Fprint(os.Stdout, "#automation_unavailable")
		return true
	}
	defer release(automation)
	word, status := "", ""
	if os.Args[1] == "--uia-paragraph" {
		word, status = uiaSelectedParagraphFromPoint(automation, point{int32(x), int32(y)})
	} else {
		word, status = uiaWordFromPoint(automation, point{int32(x), int32(y)}, selectionOnly)
	}
	if word != "" {
		fmt.Fprint(os.Stdout, word)
	} else {
		fmt.Fprint(os.Stdout, "#"+status)
	}
	return true
}
func readWordInChild(ctx context.Context, lane int, p point, selectionOnly bool) (string, string) {
	mode := "--uia-query"
	if selectionOnly {
		mode = "--uia-selected"
	}
	return readInChild(ctx, lane, p, mode, 0)
}
func readParagraphInChild(ctx context.Context, lane int, p point) (string, string) {
	return readInChild(ctx, lane, p, "--uia-paragraph", 0)
}
func readInChild(parent context.Context, lane int, p point, mode string, seq uint64) (string, string) {
	wireMode := "query"
	switch mode {
	case "--uia-selected":
		wireMode = "selected"
	case "--uia-paragraph":
		wireMode = "paragraph"
	case "--uia-query":
	default:
		return "", "invalid_mode"
	}
	root := uintptr(0)
	if seq != 0 && wireMode == "query" {
		root = nativeRootAt(p)
	}
	ctx, cancel := context.WithTimeout(parent, 950*time.Millisecond)
	defer cancel()
	req, _ := json.Marshal(uiaRPCRequest{Mode: wireMode, X: p.X, Y: p.Y})
	client := uiaClients[lane]
	data, err := client.Request(ctx, string(req))
	if ctx.Err() == context.DeadlineExceeded {
		return "", "timeout"
	}
	if parent.Err() != nil {
		return "", "superseded"
	}
	if err != nil {
		logDiagnostic("uia_helper", fmt.Sprintf("request_failed: %v", err))
		return "", "helper_error"
	}
	var r uiaRPCResponse
	if json.Unmarshal([]byte(data), &r) != nil {
		return "", "invalid_output"
	}
	if r.Status != "ok" {
		if seq != 0 && wireMode == "query" && r.OCRBounds == nil && (r.Status == "text_pattern_unavailable" || r.Status == "word_range_unavailable") {
			go logDiagnostic("ocr_skipped", "no_verified_text_bounds")
		}
		if seq != 0 && ocrclick.Fallback(r.Status, wireMode != "query", r.OCRBounds != nil) {
			// Preserve the UIA diagnostic independently of the OCR outcome.
			go logDiagnostic("uia_fallback", r.Status)
			word, status := localOCR(parent, p, *r.OCRBounds, root, seq)
			if word != "" {
				return word, "ok"
			}
			if status != "ocr_stale" {
				go logDiagnostic("ocr_result", status)
			}
			return "", status
		}
		if r.Status == "com_init_failed" || r.Status == "automation_unavailable" || r.Status == "invalid_request" {
			resetCtx, stop := context.WithTimeout(context.Background(), 500*time.Millisecond)
			_ = client.Reset(resetCtx)
			stop()
		}
		return "", r.Status
	}
	if wireMode == "paragraph" {
		value := textsegments.Clean(r.Text)
		if value == "" {
			return "", "no_text"
		}
		return value, "ok"
	}
	value := wordextract.FromTextUnit(r.Text)
	if value == "" {
		return "", "no_text"
	}
	return value, "ok"
}

type clickOutcome struct {
	word, status string
	elapsed      time.Duration
}

func uiaClickWorker(ctx context.Context) {
	fastclick.RunDual(ctx, clicks,
		func(ctx context.Context, lane int, req clickRequest) clickOutcome {
			if req.seq != clickCounter.Load() {
				return clickOutcome{status: "superseded"}
			}
			started := time.Now()
			var word, status string
			if req.selectedOnly {
				word, status = readParagraphInChild(ctx, lane, req.at)
			} else {
				word, status = readInChild(ctx, lane, req.at, "--uia-query", req.seq)
			}
			elapsed := time.Since(started)
			recordLookup(status, elapsed) // record every attempt, including superseded clicks
			return clickOutcome{word: word, status: status, elapsed: elapsed}
		},
		func(req clickRequest, out clickOutcome) {
			if req.seq != clickCounter.Load() || out.status == "superseded" {
				return
			}
			word, paragraph := clicktext.Classify(req.selectedOnly, out.word)
			status := out.status
			if status == "ok" && word == "" && paragraph == "" {
				status = "invalid_selection"
			}
			resultMu.Lock()
			resultWord, resultParagraph = word, paragraph
			resultStatus = status
			resultAt, resultSeq = req.at, req.seq
			resultMu.Unlock()
			if window != 0 {
				call(procPostMessage, window, wmAutoLookup, 0, 0)
			}
		})
}

// isRestrictedUIAControl is checked on the hit element and its nearest
// ancestors BEFORE any text access. Never collect text from password/edit,
// button, or menu controls just to make a lookup seem more responsive.
func isRestrictedUIAControl(e uintptr) bool {
	var secret int32
	if comcall(e, 35, uintptr(unsafe.Pointer(&secret))) && secret != 0 {
		return true
	}
	var typ int32
	return comcall(e, 21, uintptr(unsafe.Pointer(&typ))) &&
		(typ == uiaControlEdit || typ == uiaControlButton || typ == uiaControlMenuItem)
}

func textAtUIAElement(e uintptr, p point, selectedOnly bool) (string, bool) {
	var pattern uintptr
	// Request the typed interface; treating raw IUnknown as TextPattern can
	// call an unrelated COM slot and crash the accessibility helper.
	if !comcall(e, 14, uiaTextPattern, uintptr(unsafe.Pointer(&iidTextPattern)), uintptr(unsafe.Pointer(&pattern))) || pattern == 0 {
		return "", false
	}
	defer release(pattern)
	if selectedOnly {
		return uiaSelectedWord(pattern, p), true
	}
	return uiaTextAtPoint(pattern, p), true
}

// One click must not fan out into a dozen slow cross-process COM requests.
// Check hit control and the two nearest ancestors for protected inputs, then
// attempt the direct text node before trying only nearby parents. Normal clicks
// deliberately avoid GetSelection: that path belongs to explicit drags.
func uiaWordFromPoint(auto uintptr, p point, selectedOnly bool) (string, string) {
	return uiaWordFromPointWithBounds(auto, p, selectedOnly, nil)
}
func uiaWordFromPointWithBounds(auto uintptr, p point, selectedOnly bool, bounds **winRect) (string, string) {
	started := time.Now()
	var elem uintptr
	if !comcall(auto, 7, packPoint(p), uintptr(unsafe.Pointer(&elem))) || elem == 0 {
		return "", "no_element"
	}
	parents := []uintptr{elem}
	defer func() {
		for _, e := range parents {
			release(e)
		}
	}()
	if isRestrictedUIAControl(elem) {
		return "", "protected_control"
	}
	var walker uintptr
	comcall(auto, 16, uintptr(unsafe.Pointer(&walker)))
	if walker != 0 {
		defer release(walker)
	}
	// Guard against nested text nodes inside a password/edit field.
	for len(parents) < 3 && walker != 0 {
		var ancestor uintptr
		if !comcall(walker, 3, parents[len(parents)-1], uintptr(unsafe.Pointer(&ancestor))) || ancestor == 0 {
			break
		}
		parents = append(parents, ancestor)
		if isRestrictedUIAControl(ancestor) {
			return "", "protected_control"
		}
	}
	foundPattern := false
	for _, e := range parents {
		word, hasPattern := textAtUIAElement(e, p, selectedOnly)
		foundPattern = foundPattern || hasPattern
		if word != "" {
			return word, "ok"
		}
	}
	// Limit slow fallback breadth. A single external COM call can still be
	// slow; the parent process enforces an independent 950ms hard deadline.
	for len(parents) < 8 && walker != 0 && time.Since(started) < 140*time.Millisecond {
		var ancestor uintptr
		if !comcall(walker, 3, parents[len(parents)-1], uintptr(unsafe.Pointer(&ancestor))) || ancestor == 0 {
			break
		}
		parents = append(parents, ancestor)
		if isRestrictedUIAControl(ancestor) {
			return "", "protected_control"
		}
		word, hasPattern := textAtUIAElement(ancestor, p, selectedOnly)
		foundPattern = foundPattern || hasPattern
		if word != "" {
			return word, "ok"
		}
	}
	if selectedOnly {
		return "", "no_selection"
	}
	// AccessibleName is only acceptable if the COMPLETE name is a word.
	for i, e := range parents {
		if i >= 2 || time.Since(started) > 180*time.Millisecond {
			break
		}
		if word := uiaSingleNameWord(e); word != "" {
			return word, "ok"
		}
	}
	if bounds != nil {
		*bounds = verifiedOCRBounds(parents, p)
	}
	if !foundPattern {
		return "", "text_pattern_unavailable"
	}
	return "", "word_range_unavailable"
}

// OCR is allowed only after a known read-only text hit and verified safe ancestors.
// If any provider property cannot be read, fail closed rather than capturing it.
func verifiedOCRBounds(parents []uintptr, p point) *winRect {
	if len(parents) == 0 {
		return nil
	}
	var leaf int32
	document := false
	for i, e := range parents {
		var secret, typ int32
		if !comcall(e, 35, uintptr(unsafe.Pointer(&secret))) || secret != 0 || !comcall(e, 21, uintptr(unsafe.Pointer(&typ))) || typ == uiaControlEdit || typ == uiaControlButton || typ == uiaControlMenuItem {
			return nil
		}
		if i == 0 {
			leaf = typ
		}
		if typ == 50030 {
			document = true
		}
	}
	// Web renderers sometimes expose a generic group/pane rather than a Text
	// node. Accept those only inside a verified Document, never arbitrary UI.
	if leaf != uiaControlText && leaf != uiaControlHyperlink && leaf != 50030 && !(document && (leaf == 50025 || leaf == 50026 || leaf == 50033)) {
		return nil
	}
	var r winRect
	if !comcall(parents[0], 43, uintptr(unsafe.Pointer(&r))) || p.X < r.Left || p.X >= r.Right || p.Y < r.Top || p.Y >= r.Bottom {
		return nil
	}
	return &r
}

// A paragraph is only read from a deliberate mouse selection. Single clicks
// continue through the original word-only path.
func uiaSelectedParagraphFromPoint(auto uintptr, p point) (string, string) {
	var elem uintptr
	if !comcall(auto, 7, packPoint(p), uintptr(unsafe.Pointer(&elem))) || elem == 0 {
		return "", "no_element"
	}
	parents := []uintptr{elem}
	defer func() {
		for _, e := range parents {
			release(e)
		}
	}()
	var walker uintptr
	comcall(auto, 16, uintptr(unsafe.Pointer(&walker)))
	if walker != 0 {
		defer release(walker)
		for depth := 0; depth < 5; depth++ {
			var parent uintptr
			if !comcall(walker, 3, parents[len(parents)-1], uintptr(unsafe.Pointer(&parent))) || parent == 0 {
				break
			}
			parents = append(parents, parent)
		}
	}
	for _, e := range parents {
		var secret int32
		if comcall(e, 35, uintptr(unsafe.Pointer(&secret))) && secret != 0 {
			return "", "protected_control"
		}
		var typ int32
		if comcall(e, 21, uintptr(unsafe.Pointer(&typ))) && (typ == uiaControlEdit || typ == uiaControlButton || typ == uiaControlMenuItem) {
			return "", "protected_control"
		}
	}
	for _, e := range parents {
		var pattern uintptr
		if !comcall(e, 14, uiaTextPattern, uintptr(unsafe.Pointer(&iidTextPattern)), uintptr(unsafe.Pointer(&pattern))) || pattern == 0 {
			continue
		}
		text := uiaTextSelection(pattern, p)
		release(pattern)
		if text != "" {
			return text, "ok"
		}
	}
	return "", "no_selection"
}
func uiaTextSelection(pattern uintptr, p point) string {
	var arr uintptr
	if !comcall(pattern, 5, uintptr(unsafe.Pointer(&arr))) || arr == 0 {
		return ""
	}
	defer release(arr)
	var count int32
	if !comcall(arr, 3, uintptr(unsafe.Pointer(&count))) || count < 1 || count > 16 {
		return ""
	}
	for i := int32(0); i < count; i++ {
		var tr uintptr
		if !comcall(arr, 4, uintptr(i), uintptr(unsafe.Pointer(&tr))) || tr == 0 {
			continue
		}
		if !uiaClickInsideRange(tr, p) {
			release(tr)
			continue
		}
		var bstr uintptr
		ok := comcall(tr, 12, 3002, uintptr(unsafe.Pointer(&bstr)))
		release(tr)
		if !ok || bstr == 0 {
			continue
		}
		n := call(oleaut.NewProc("SysStringLen"), bstr)
		if n > 0 && n < 3002 {
			text := textsegments.Clean(syscall.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(bstr)), int(n))))
			oleaut.NewProc("SysFreeString").Call(bstr)
			if text != "" && len([]byte(text)) <= textsegments.MaxTextBytes {
				return text
			}
			continue
		}
		oleaut.NewProc("SysFreeString").Call(bstr)
	}
	return ""
}

func uiaSingleNameWord(elem uintptr) string {
	var control int32
	if !comcall(elem, 21, uintptr(unsafe.Pointer(&control))) || (control != uiaControlText && control != uiaControlHyperlink) {
		return ""
	}
	var bstr uintptr
	if !comcall(elem, 23, uintptr(unsafe.Pointer(&bstr))) || bstr == 0 {
		return ""
	} // CurrentName
	defer oleaut.NewProc("SysFreeString").Call(bstr)
	ln := call(oleaut.NewProc("SysStringLen"), bstr)
	if ln == 0 || ln > 80 {
		return ""
	}
	name := syscall.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(bstr)), int(ln)))
	if !wordextract.IsSingleWord(name) {
		return ""
	}
	return wordextract.FromTextUnit(name)
}

func uiaTextAtPoint(pattern uintptr, p point) string {
	// RangeFromPoint is already the text provider's answer for this pointer.
	// The old version additionally called GetBoundingRectangles and rejected
	// legitimate words when accessibility providers returned missing / stale
	// screen rectangles. That extra cross-process COM call also grew latency.
	// We still reject protected parent controls before accepting the text.
	var tr uintptr
	if !comcall(pattern, 3, packPoint(p), uintptr(unsafe.Pointer(&tr))) || tr == 0 {
		return ""
	}
	defer release(tr)
	if !comcall(tr, 6, textUnitWord) {
		return ""
	}
	return uiaTextRangeWord(tr)
}
func uiaSelectedWord(pattern uintptr, p point) string {
	var arr uintptr
	if !comcall(pattern, 5, uintptr(unsafe.Pointer(&arr))) || arr == 0 { // TextPattern.GetSelection
		return ""
	}
	defer release(arr)
	var count int32
	if !comcall(arr, 3, uintptr(unsafe.Pointer(&count))) || count < 1 || count > 16 {
		return ""
	}
	for i := int32(0); i < count; i++ {
		var tr uintptr
		if !comcall(arr, 4, uintptr(i), uintptr(unsafe.Pointer(&tr))) || tr == 0 {
			continue
		}
		// Prevent returning a stale selection elsewhere in the same document.
		if !uiaClickInsideRange(tr, p) {
			release(tr)
			continue
		}
		word := uiaTextRangeWord(tr)
		release(tr)
		if word != "" {
			return word
		}
	}
	return ""
}

func uiaTextRangeWord(tr uintptr) string {
	var str uintptr
	if !comcall(tr, 12, 81, uintptr(unsafe.Pointer(&str))) || str == 0 {
		return ""
	}
	defer oleaut.NewProc("SysFreeString").Call(str)
	ln := call(oleaut.NewProc("SysStringLen"), str)
	if ln == 0 || ln > 80 {
		return ""
	}
	return wordextract.FromTextUnit(syscall.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(str)), int(ln))))
}

func uiaClickInsideRange(tr uintptr, p point) bool {
	var arr uintptr
	if !comcall(tr, 10, uintptr(unsafe.Pointer(&arr))) || arr == 0 {
		return false
	}
	defer oleaut.NewProc("SafeArrayDestroy").Call(arr)
	var low, high int32
	lo, _, _ := oleaut.NewProc("SafeArrayGetLBound").Call(arr, 1, uintptr(unsafe.Pointer(&low)))
	hi, _, _ := oleaut.NewProc("SafeArrayGetUBound").Call(arr, 1, uintptr(unsafe.Pointer(&high)))
	if int32(lo) < 0 || int32(hi) < 0 {
		return false
	}
	n := int(high - low + 1)
	if n < 4 || n > 400 || n%4 != 0 {
		return false
	}
	var ptr uintptr
	r, _, _ := oleaut.NewProc("SafeArrayAccessData").Call(arr, uintptr(unsafe.Pointer(&ptr)))
	if int32(r) < 0 || ptr == 0 {
		return false
	}
	defer oleaut.NewProc("SafeArrayUnaccessData").Call(arr)
	vals := unsafe.Slice((*float64)(unsafe.Pointer(ptr)), n)
	for i := 0; i+3 < n; i += 4 {
		left, top, w, h := vals[i], vals[i+1], vals[i+2], vals[i+3]
		if w > 0 && h > 0 && float64(p.X) >= left-4 && float64(p.X) <= left+w+4 && float64(p.Y) >= top-4 && float64(p.Y) <= top+h+4 {
			return true
		}
	}
	return false
}

// Called ONLY from the Win32 UI thread via PostMessageW.
// Clicking text updates the dictionary, but never moves the user-positioned UI.
func acceptAutoLookup() {
	uiStart := time.Now()
	defer func() { recordUIRender(time.Since(uiStart)) }()
	resultMu.Lock()
	word, paragraph, status, seq := resultWord, resultParagraph, resultStatus, resultSeq
	resultWord, resultParagraph, resultStatus = "", "", ""
	resultMu.Unlock()
	if !autoEnabled || seq != clickCounter.Load() {
		return
	}
	stopLookupPending()
	recordUIResult(status)
	if word == "" && paragraph == "" {
		// Never leave an old result or ".0" displayed indefinitely after a
		// failed click. Do not open a collapsed bubble on an empty result.
		if expanded && !inList && status != "protected_control" {
			lookupDisplayStatus = lookupErrorText(status)
			draw()
		}
		return
	}
	lookupDisplayStatus = ""
	inList = false
	if paragraph != "" {
		startParagraphTranslation(paragraph)
	} else {
		selectedParagraph = ""
		ignoreEditChange = true
		set(edit, word)
		ignoreEditChange = false
		onWordEdited()
		if !expanded {
			resize(true)
		} else {
			draw()
		}
	}
	// Deliberately no SetWindowPos: users choose where the dictionary lives.
}
