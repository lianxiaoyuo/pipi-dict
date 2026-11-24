//go:build windows

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hoverdict/internal/helperproc"
	"hoverdict/internal/ocrclick"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

//go:embed ocr_helper.ps1
var ocrScript string

//go:embed ocr_selftest.png
var ocrSelfTest []byte
var ocrManager = helperproc.NewWithRequestLimit(newOCRCommand, 100000, 512*1024)
var ocrSlot = make(chan struct{}, 1)
var ocrReady atomic.Bool
var ocrDisabled atomic.Bool
var ocrNextWarm atomic.Int64
var ocrHits, ocrAttempts atomic.Uint64
var ocrCache struct {
	sync.Mutex
	hash  [32]byte
	words []ocrclick.Word
	at    time.Time
}

func newOCRCommand() *exec.Cmd {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	path := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	u := utf16.Encode([]rune(ocrScript))
	b := make([]byte, len(u)*2)
	for i, v := range u {
		b[2*i] = byte(v)
		b[2*i+1] = byte(v >> 8)
	}
	cmd := exec.Command(path, "-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(b))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.WaitDelay = 300 * time.Millisecond
	return cmd
}

type ocrResponse struct {
	Status string          `json:"status"`
	Words  []ocrclick.Word `json:"words"`
}

func warmOCR(ctx context.Context) {
	if ocrDisabled.Load() {
		return
	}
	n := time.Now().UnixNano()
	prev := ocrNextWarm.Load()
	if n < prev || !ocrNextWarm.CompareAndSwap(prev, n+int64(5*time.Second)) {
		return
	}
	// Warm the complete decode + recognize pipeline on a synthetic local image,
	// not on the user's screen. This detects missing language/API at startup.
	select {
	case ocrSlot <- struct{}{}:
		defer func() { <-ocrSlot }()
	case <-ctx.Done():
		return
	}
	c, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, _ := json.Marshal(map[string]string{"mode": "recognize", "png": base64.StdEncoding.EncodeToString(ocrSelfTest)})
	raw, err := ocrManager.Request(c, string(req))
	var resp ocrResponse
	if err == nil && json.Unmarshal([]byte(raw), &resp) == nil {
		pass := false
		for _, w := range resp.Words {
			if w.Text == "working" || w.Text == "Working" {
				pass = true
				break
			}
		}
		ocrReady.Store(resp.Status == "ok" && pass)
		if resp.Status == "ok" && !pass {
			resp.Status = "ocr_selftest_failed"
			ocrDisabled.Store(true)
		}
		if resp.Status == "ocr_language_missing" || resp.Status == "ocr_unavailable" || resp.Status == "ocr_error" {
			ocrDisabled.Store(true)
		}
		logDiagnostic("ocr_init", resp.Status)
	} else {
		ocrReady.Store(false)
		logDiagnostic("ocr_init", "helper_error")
	}
}

// Capture only a bounded rectangle inside the UIA-approved text element.
// No screenshots are written to disk, included in logs, or sent to the web.
func captureOCR(r ocrclick.Rect) ([]byte, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	w, h := int(r.Right-r.Left), int(r.Bottom-r.Top)
	if w < 8 || h < 8 || w > 384 || h > 108 {
		return nil, fmt.Errorf("invalid crop")
	}
	dc := call(u32.NewProc("GetDC"), 0)
	if dc == 0 {
		return nil, fmt.Errorf("screen dc")
	}
	defer call(u32.NewProc("ReleaseDC"), 0, dc)
	mem := call(gdi.NewProc("CreateCompatibleDC"), dc)
	if mem == 0 {
		return nil, fmt.Errorf("memory dc")
	}
	defer call(gdi.NewProc("DeleteDC"), mem)
	// BITMAPINFOHEADER, top-down 32bpp BGRX.
	hdr := struct {
		Size                   uint32
		Width, Height          int32
		Planes, Bits           uint16
		Compression, SizeImage uint32
		Xppm, Yppm             int32
		Used, Important        uint32
	}{40, int32(w), -int32(h), 1, 32, 0, 0, 0, 0, 0, 0}
	var pixels uintptr
	bm := call(gdi.NewProc("CreateDIBSection"), dc, uintptr(unsafe.Pointer(&hdr)), 0, uintptr(unsafe.Pointer(&pixels)), 0, 0)
	if bm == 0 || pixels == 0 {
		if bm != 0 {
			call(gdi.NewProc("DeleteObject"), bm)
		}
		return nil, fmt.Errorf("dib")
	}
	defer call(gdi.NewProc("DeleteObject"), bm)
	old := call(gdi.NewProc("SelectObject"), mem, bm)
	if old == 0 || old == ^uintptr(0) {
		return nil, fmt.Errorf("select bitmap")
	}
	defer call(gdi.NewProc("SelectObject"), mem, old)
	if call(gdi.NewProc("BitBlt"), mem, 0, 0, uintptr(w), uintptr(h), dc, uintptr(r.Left), uintptr(r.Top), 0x00CC0020) == 0 {
		return nil, fmt.Errorf("capture")
	}
	call(gdi.NewProc("GdiFlush"))
	src := unsafe.Slice((*byte)(unsafe.Pointer(pixels)), w*h*4)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(src); i += 4 {
		img.Pix[i] = src[i+2]
		img.Pix[i+1] = src[i+1]
		img.Pix[i+2] = src[i]
		img.Pix[i+3] = 255
	}
	var out bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
func nativeRootAt(p point) uintptr {
	h := call(u32.NewProc("WindowFromPoint"), packPoint(p))
	if h == 0 {
		return 0
	}
	return call(u32.NewProc("GetAncestor"), h, gaRoot)
}
func localOCR(ctx context.Context, p point, bounds winRect, root uintptr, seq uint64) (string, string) {
	if root == 0 || seq != clickCounter.Load() || nativeRootAt(p) != root {
		return "", "ocr_stale"
	}
	if ocrDisabled.Load() {
		return "", "ocr_unavailable"
	}
	if !ocrReady.Load() {
		go warmOCR(ctx)
		return "", "ocr_warming"
	}
	// Only one OCR request can occupy the helper. A newer click may briefly
	// wait for the preceding request, but never queues unbounded work.
	wait := time.NewTimer(180 * time.Millisecond)
	defer wait.Stop()
	select {
	case ocrSlot <- struct{}{}:
		defer func() { <-ocrSlot }()
	case <-wait.C:
		return "", "ocr_busy"
	case <-ctx.Done():
		return "", "ocr_stale"
	}
	if !ocrReady.Load() || seq != clickCounter.Load() {
		return "", "ocr_stale"
	}
	screen := virtualDesktop()
	var wr winRect
	if call(u32.NewProc("GetWindowRect"), root, uintptr(unsafe.Pointer(&wr))) == 0 {
		return "", "ocr_no_bounds"
	}
	bounds2 := ocrclick.Intersect(ocrclick.Rect{Left: bounds.Left, Top: bounds.Top, Right: bounds.Right, Bottom: bounds.Bottom}, ocrclick.Rect{Left: wr.Left, Top: wr.Top, Right: wr.Right, Bottom: wr.Bottom})
	r, ok := ocrclick.Crop(p.X, p.Y, bounds2, ocrclick.Rect{Left: screen.X, Top: screen.Y, Right: screen.X + screen.W, Bottom: screen.Y + screen.H})
	if !ok {
		return "", "ocr_no_bounds"
	}
	// Reject crops crossing the dictionary itself or another root window.
	for _, q := range []point{{r.Left, r.Top}, {r.Right - 1, r.Top}, {r.Left, r.Bottom - 1}, {r.Right - 1, r.Bottom - 1}} {
		if nativeRootAt(q) != root {
			return "", "ocr_occluded"
		}
	}
	started := time.Now()
	ocrAttempts.Add(1)
	data, err := captureOCR(r)
	if err != nil {
		return "", "ocr_capture_failed"
	}
	if seq != clickCounter.Load() || nativeRootAt(p) != root {
		return "", "ocr_stale"
	}
	hash := sha256.Sum256(data)
	x, y := float64(p.X-r.Left), float64(p.Y-r.Top)
	ocrCache.Lock()
	if hash == ocrCache.hash && time.Since(ocrCache.at) < 2*time.Second {
		v := ocrclick.Hit(ocrCache.words, x, y)
		ocrCache.Unlock()
		if v != "" {
			ocrHits.Add(1)
			return v, "ocr_cache_hit"
		}
		return "", "ocr_no_word"
	}
	ocrCache.Unlock()
	req, _ := json.Marshal(struct {
		Mode string `json:"mode"`
		PNG  string `json:"png"`
	}{"recognize", base64.StdEncoding.EncodeToString(data)})
	c, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
	defer cancel()
	raw, err := ocrManager.Request(c, string(req))
	if err != nil {
		ocrReady.Store(false)
		ocrNextWarm.Store(0)
		go warmOCR(ctx)
		return "", "ocr_timeout_or_error"
	}
	var result ocrResponse
	if json.Unmarshal([]byte(raw), &result) != nil {
		return "", "ocr_invalid_output"
	}
	if result.Status != "ok" {
		if result.Status == "ocr_language_missing" || result.Status == "ocr_unavailable" {
			ocrDisabled.Store(true)
		}
		return "", result.Status
	}
	if seq != clickCounter.Load() {
		return "", "ocr_stale"
	}
	ocrCache.Lock()
	ocrCache.hash = hash
	ocrCache.words = result.Words
	ocrCache.at = time.Now()
	ocrCache.Unlock()
	word := ocrclick.Hit(result.Words, x, y)
	if word == "" {
		return "", "ocr_no_word"
	}
	ocrHits.Add(1)
	go logDiagnostic("ocr_latency", fmt.Sprintf("duration_ms=%d", time.Since(started).Milliseconds()))
	go logDiagnostic("ocr_result", "ocr_ok")
	return word, "ocr_ok"
}
