//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var (
	uiAckedClicks    atomic.Uint64
	uiHandledResults atomic.Uint64
)

var diag struct {
	sync.Mutex
	path                               string
	last                               string
	lastOutcome                        string
	lastAt                             time.Time
	start                              time.Time
	successes, empty, timeouts, errors int
	lookupSamples                      []int64
	lastFailureLogAt                   map[string]time.Time
}

func initDiagnostics() {
	diag.start = time.Now()
	root, err := os.UserConfigDir()
	if err != nil {
		return
	}
	p := filepath.Join(root, "HoverDict", "logs")
	if os.MkdirAll(p, 0700) != nil {
		return
	}
	diag.path = filepath.Join(p, "hoverdict.log")
	logDiagnostic("startup", "app launched version=5.6.1")
	// Periodic status continues even if the GUI thread stops processing input.
	// It captures counters only; never the user's clicked text or position.
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			logDiagnostic("heartbeat", fmt.Sprintf("hook_events=%d ui_acks=%d ui_results=%d hook_rearms=%d ocr_attempts=%d ocr_hits=%d", hookEvents.Load(), uiAckedClicks.Load(), uiHandledResults.Load(), hookRearms.Load(), ocrAttempts.Load(), ocrHits.Load()))
		}
	}()
}
func logDiagnostic(event, detail string) {
	diag.Lock()
	defer diag.Unlock()
	if diag.path == "" {
		return
	}
	// Do not store user text, clicked coordinates, clipboard, URL, or API secrets.
	detail = strings.ReplaceAll(strings.ReplaceAll(detail, "\n", " "), "\r", " ")
	if len(detail) > 160 {
		detail = detail[:160]
	}
	stamp := time.Now().Format(time.RFC3339)
	// Keep at most a single 2MB rolling file plus the previous revision.
	if st, err := os.Stat(diag.path); err == nil && st.Size() > 2*1024*1024 {
		os.Remove(diag.path + ".1")
		os.Rename(diag.path, diag.path+".1")
	}
	f, err := os.OpenFile(diag.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err == nil {
		fmt.Fprintf(f, "%s [%s] %s\n", stamp, event, detail)
		f.Close()
	}
	diag.last = event + ": " + detail
	diag.lastAt = time.Now()
}

// recordLookup records only timing and status; never the user's words.
// Every 10 attempts it writes an aggregated p50/p95/p99 time summary.
// Ten samples allow diagnosing the user's observed failure around click 10.
func recordLookup(status string, duration time.Duration) {
	diag.Lock()
	switch status {
	case "ok":
		diag.successes++
	case "no_text", "no_element", "no_selection", "text_pattern_unavailable", "word_range_unavailable", "protected_control":
		diag.empty++
	case "timeout":
		diag.timeouts++
	default:
		diag.errors++
	}
	diag.lastOutcome = status
	// Avoid one goroutine + disk write for every inaccessible App click.
	// The aggregate p50/p95/p99 still measures every executed request.
	shouldLogFailure := false
	if status != "ok" {
		if diag.lastFailureLogAt == nil {
			diag.lastFailureLogAt = make(map[string]time.Time)
		}
		if time.Since(diag.lastFailureLogAt[status]) >= 15*time.Second {
			shouldLogFailure = true
			diag.lastFailureLogAt[status] = time.Now()
		}
	}
	diag.lookupSamples = append(diag.lookupSamples, duration.Milliseconds())
	var measurements []int64
	if len(diag.lookupSamples) >= 10 {
		measurements = append([]int64(nil), diag.lookupSamples...)
		diag.lookupSamples = diag.lookupSamples[:0]
	}
	diag.Unlock()
	if shouldLogFailure {
		// Keep disk IO off the click scheduler's critical path.
		go logDiagnostic("uia_lookup", fmt.Sprintf("%s duration_ms=%d", status, duration.Milliseconds()))
	}
	if len(measurements) > 0 {
		sort.Slice(measurements, func(i, j int) bool { return measurements[i] < measurements[j] })
		at := func(p int) int64 {
			index := (len(measurements)*p+99)/100 - 1
			if index < 0 {
				index = 0
			}
			return measurements[index]
		}
		go logDiagnostic("uia_latency", fmt.Sprintf("samples=%d p50_ms=%d p95_ms=%d p99_ms=%d", len(measurements), at(50), at(95), at(99)))
	}
}

func recordUIAck() {
	n := uiAckedClicks.Add(1)
	if n == 1 || n%10 == 0 {
		go logDiagnostic("click_ack", fmt.Sprintf("count=%d hook_events=%d", n, hookEvents.Load()))
	}
}
func recordUIResult(status string) {
	n := uiHandledResults.Add(1)
	if n == 1 || n%10 == 0 {
		go logDiagnostic("click_result", fmt.Sprintf("count=%d status=%s", n, status))
	}
}

// Slow UI result handling has a different cause from slow UI Automation.
// Separate timing helps distinguish a blocked message pump from a slow provider.
func recordUIRender(d time.Duration) {
	if d >= 100*time.Millisecond {
		go logDiagnostic("ui_render_slow", fmt.Sprintf("duration_ms=%d", d.Milliseconds()))
	}
}

// Ctrl+Alt+L opens the actual diagnostics directory without relying on
// Windows Search indexing hidden AppData folders. Explorer is GUI-only.
func openDiagnosticsDirectory() {
	root, err := os.UserConfigDir()
	if err != nil {
		return
	}
	folder := filepath.Join(root, "HoverDict", "logs")
	if err := os.MkdirAll(folder, 0700); err != nil {
		return
	}
	verb := syscall.StringToUTF16Ptr("open")
	path := syscall.StringToUTF16Ptr(folder)
	call(syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW"), 0,
		uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(path)), 0, 0, 1)
}
