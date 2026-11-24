//go:build windows

package main

import (
	"time"
)

const lookupStatusTimerID = 932

var (
	lookupPendingSeq    uint64
	lookupDisplayStatus string
)

// Accept event immediately on the UI thread, but only redraw if the helper is
// slow: fast clicks should never pay an extra full-panel paint per attempt.
func beginLookupPending(seq uint64) {
	if seq != clickCounter.Load() {
		return
	}
	lookupPendingSeq = seq
	recordUIAck()
	lookupDisplayStatus = ""
	if window != 0 {
		call(u32.NewProc("KillTimer"), window, lookupStatusTimerID)
		call(u32.NewProc("SetTimer"), window, lookupStatusTimerID, uintptr((180*time.Millisecond)/time.Millisecond), 0)
	}
}
func stopLookupPending() {
	lookupPendingSeq = 0
	if window != 0 {
		call(u32.NewProc("KillTimer"), window, lookupStatusTimerID)
	}
}
func showLookupPending() {
	if lookupPendingSeq == 0 || lookupPendingSeq != clickCounter.Load() || !expanded || inList {
		return
	}
	call(u32.NewProc("KillTimer"), window, lookupStatusTimerID)
	lookupDisplayStatus = "正在读取点击位置的英文…"
	draw()
}
func lookupErrorText(status string) string {
	switch status {
	case "ocr_unavailable", "ocr_language_missing":
		return "本机英文 OCR 不可用。可添加 Windows 英语 OCR 语言包，或复制单词后按 Ctrl+Alt+W。"
	case "ocr_busy":
		return "正在识别上一次点击，请直接再点一下英文。"
	case "ocr_warming":
		return "本地 OCR 正在初始化，请再点击一次。"
	case "ocr_no_word", "ocr_no_bounds", "ocr_occluded", "ocr_stale":
		return "点击处未识别到英文单词，请直接点击英文文字。"
	case "ocr_timeout_or_error", "ocr_capture_failed", "ocr_invalid_output", "ocr_error":
		return "本地 OCR 暂未成功，请继续点击，或复制后按 Ctrl+Alt+W。"
	case "timeout":
		return "目标应用文字接口响应超时。你可以继续点击其他单词；或复制单词后按 Ctrl+Alt+W。"
	case "text_pattern_unavailable", "word_range_unavailable", "no_element", "no_text", "no_selection":
		return "当前点击位置未能获取英文单词。可尝试选中并复制后按 Ctrl+Alt+W。"
	case "invalid_selection":
		return "没有识别到有效的英文选区，请重新选择。"
	case "helper_error", "invalid_output", "automation_unavailable", "com_init_failed":
		return "取词辅助接口异常，后续点击可继续重试；也可复制单词后按 Ctrl+Alt+W。"
	default:
		return "未识别到英文单词，请点击文字或使用 Ctrl+Alt+W。"
	}
}
