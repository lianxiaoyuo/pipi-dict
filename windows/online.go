//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"hoverdict/internal/freeonline"
	"hoverdict/internal/onlinememo"
	"hoverdict/internal/textsegments"
	"hoverdict/internal/wordextract"
)

const (
	wmTimer       = 0x0113
	wmOnlineDone  = 0x8002
	onlineTimerID = 931
)

var (
	onlineSequence                                       uint64 // UI thread only
	onlineCancel                                         context.CancelFunc
	onlineWord, onlineMeaning, onlineError, onlineSource string
	responseMu                                           sync.Mutex
	responseReady                                        onlineResponse
	// The public-API quota ledger uses an interprocess file lock and fsync.
	// Serialize reservations in the background; NEVER wait for it on the UI thread.
	onlineQuotaSlot                                     = make(chan struct{}, 1)
	memo                                                = onlinememo.New(512, 90*time.Minute)
	selectedParagraph, paragraphMeaning, paragraphError string
)

type onlineResponse struct {
	Sequence                     uint64
	Word, Meaning, Error, Source string
	Paragraph                    bool
}

func showHide(visible bool, handles ...uintptr) {
	state := uintptr(0)
	if visible {
		state = 5
	}
	for _, h := range handles {
		if h == 0 {
			continue
		}
		if previous, seen := controlVisible[h]; seen && previous == visible {
			continue
		}
		controlVisible[h] = visible
		call(u32.NewProc("ShowWindow"), h, state)
	}
}

func validOnlineWord(w string) bool {
	return len(w) <= 72 && wordextract.IsSingleWord(w)
}

func clearOnline(increment bool) {
	if window != 0 {
		call(u32.NewProc("KillTimer"), window, onlineTimerID)
	}
	if onlineCancel != nil {
		onlineCancel()
		onlineCancel = nil
	}
	if increment {
		onlineSequence++
	}
	onlineWord, onlineMeaning, onlineError, onlineSource = "", "", "", ""
}

func onWordEdited() {
	selectedParagraph, paragraphMeaning, paragraphError = "", "", ""
	word := norm(get(edit))
	if !inList && word != "" && word == onlineWord && (onlineMeaning != "" || onlineError != "") {
		return
	}
	clearOnline(true)
	if inList || !validOnlineWord(word) {
		return
	}
	if translation, found := memo.Get(word, time.Now()); found {
		onlineWord, onlineMeaning, onlineError, onlineSource = word, translation, "", "免费联网（缓存）"
		return
	}
	if e, ok := getEntry(word); ok && strings.TrimSpace(e.Meaning) != "" {
		return
	}
	if metaStore != nil {
		if r, ok := metaStore.Get(word); ok && r.Meaning != "" {
			return
		}
	}
	call(u32.NewProc("SetTimer"), window, onlineTimerID, 300, 0)
}

func onlineDisplay(word string) string {
	if !validOnlineWord(word) {
		return "请输入单个英文单词查询。"
	}
	if onlineWord == word {
		if onlineMeaning != "" {
			return onlineMeaning
		}
		if onlineError != "" {
			return "在线翻译暂不可用：" + onlineError + "\r\n仍可收藏此单词。"
		}
	}
	if cached, ok := memo.Get(word, time.Now()); ok {
		return cached
	}
	if requestGuardErr != nil {
		return "在线查询保护暂不可用：" + requestGuardErr.Error()
	}
	return "本地无释义，正在尝试免费联网翻译…"
}

func onOnlineTimer() {
	call(u32.NewProc("KillTimer"), window, onlineTimerID)
	if inList {
		return
	}
	word := norm(get(edit))
	if !validOnlineWord(word) {
		return
	}
	if e, ok := getEntry(word); ok && strings.TrimSpace(e.Meaning) != "" {
		return
	}
	if metaStore != nil {
		if r, ok := metaStore.Get(word); ok && r.Meaning != "" {
			return
		}
	}
	if cached, ok := memo.Get(word, time.Now()); ok {
		onlineWord, onlineMeaning, onlineError, onlineSource = word, cached, "", "免费联网（缓存）"
		draw()
		return
	}
	if requestGuardErr != nil || requestGuard == nil {
		onlineWord, onlineError = word, fmt.Sprintf("联网额度记录无法使用：%v", requestGuardErr)
		draw()
		return
	}
	if onlineCancel != nil {
		onlineCancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4500*time.Millisecond)
	onlineCancel = cancel
	seq := onlineSequence
	go func() {
		response := onlineResponse{Sequence: seq, Word: word, Source: "免费联网"}
		// requestGuard.Allow may wait for another process's lock and flush
		// the ledger to disk; it must not execute on the Windows message pump.
		// Hold the slot only while writing the quota ledger, never for the
		// subsequent network request. This keeps new lookups independent of
		// an older DNS/HTTP request even when the target provider is slow.
		reserve := func() error {
			select {
			case onlineQuotaSlot <- struct{}{}:
				defer func() { <-onlineQuotaSlot }()
			case <-ctx.Done():
				return ctx.Err()
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return requestGuard.Allow(word, time.Now())
		}
		if err := reserve(); err != nil {
			if ctx.Err() != nil {
				return
			}
			response.Error = err.Error()
		} else if ctx.Err() == nil {
			result, err := (freeonline.Client{}).Translate(ctx, word)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				response.Error = err.Error()
			} else {
				response.Meaning = result
			}
		} else {
			return
		}
		if ctx.Err() == context.Canceled {
			return
		}
		responseMu.Lock()
		if response.Sequence >= responseReady.Sequence {
			responseReady = response
			responseMu.Unlock()
			if window != 0 {
				call(u32.NewProc("PostMessageW"), window, wmOnlineDone, 0, 0)
			}
		} else {
			responseMu.Unlock()
		}
	}()
}

func acceptOnlineResponse() {
	responseMu.Lock()
	resp := responseReady
	responseMu.Unlock()
	if resp.Paragraph {
		if resp.Sequence != onlineSequence || resp.Word != selectedParagraph || inList {
			return
		}
		paragraphMeaning, paragraphError = resp.Meaning, resp.Error
		if onlineCancel != nil {
			onlineCancel()
			onlineCancel = nil
		}
		draw()
		return
	}
	if resp.Sequence != onlineSequence || resp.Word != norm(get(edit)) || inList {
		return
	}
	onlineWord, onlineMeaning, onlineError, onlineSource = resp.Word, resp.Meaning, resp.Error, resp.Source
	if resp.Meaning != "" && resp.Error == "" {
		memo.Put(resp.Word, resp.Meaning, time.Now())
		// If the user explicitly saved this word while translation was pending,
		// complete the saved record without changing the legacy saved.json.
		if saved[resp.Word] && metaStore != nil {
			r, ok := metaStore.Get(resp.Word)
			if ok && r.Meaning == "" {
				r.Meaning = resp.Meaning
				r.Source = "MyMemory（用户主动收藏）"
				if r.SavedAt.IsZero() {
					r.SavedAt = time.Now()
				}
				if metaStore.Set(resp.Word, r) == nil {
					invalidateSavedCards()
				}
			}
		}
	}
	if onlineCancel != nil {
		onlineCancel()
		onlineCancel = nil
	}
	draw()
}

// startParagraphTranslation handles only an explicit mouse-selected region or
// the Ctrl+Alt+T shortcut. Unlike words, it cannot be added to the wordbook.
func startParagraphTranslation(text string) {
	text = textsegments.Clean(text)
	clean, err := textsegments.Split(text)
	if err != nil {
		// Show a meaningful message instead of silently ignoring long selections.
		preview := []rune(text)
		if len(preview) > 240 {
			preview = preview[:240]
		}
		if edit != 0 {
			set(edit, "")
		}
		clearOnline(true)
		selectedParagraph, paragraphMeaning, paragraphError = string(preview), "", err.Error()
		inList = false
		if !expanded {
			resize(true)
		}
		draw()
		return
	}
	if len(clean) == 0 {
		return
	}
	// Clearing the edit control first triggers EN_CHANGE; start selection state after it.
	if edit != 0 {
		set(edit, "")
	}
	clearOnline(true)
	selectedParagraph, paragraphMeaning, paragraphError = text, "", ""
	inList = false
	if !expanded {
		resize(true)
	}
	draw()
	seq := onlineSequence
	if requestGuardErr != nil || requestGuard == nil {
		paragraphError = "免费联网额度管理不可用"
		draw()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 24*time.Second)
	onlineCancel = cancel
	go func() {
		r := onlineResponse{Sequence: seq, Word: text, Source: "MyMemory", Paragraph: true}
		c := freeonline.Client{BeforeChunk: func(chunk string) error { return requestGuard.AllowText(chunk, time.Now()) }}
		res, e := c.TranslateText(ctx, text)
		if e != nil {
			if errors.Is(e, context.Canceled) {
				return
			}
			r.Error = e.Error()
		} else {
			r.Meaning = res
		}
		responseMu.Lock()
		if r.Sequence >= responseReady.Sequence {
			responseReady = r
			responseMu.Unlock()
			if window != 0 {
				call(u32.NewProc("PostMessageW"), window, wmOnlineDone, 0, 0)
			}
		} else {
			responseMu.Unlock()
		}
	}()
}
