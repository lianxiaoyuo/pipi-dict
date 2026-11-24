//go:build windows

package main

import (
	"context"
	"hoverdict/internal/phonetics"
	"sync"
	"time"
)

const wmPronDone = 0x8006

var pronAttempts = map[string]bool{} // UI thread only
var pronResultMu sync.Mutex
var pronResults = map[string]string{}

func requestMissingIPA(word string) {
	if word == "" || pronAttempts[word] || metaStore == nil {
		return
	}
	pronAttempts[word] = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		ipa, err := (phonetics.Client{}).Get(ctx, word)
		if err != nil || ipa == "" {
			return
		}
		pronResultMu.Lock()
		pronResults[word] = ipa
		pronResultMu.Unlock()
		if window != 0 {
			call(u32.NewProc("PostMessageW"), window, wmPronDone, 0, 0)
		}
	}()
}
func acceptMissingIPA() {
	pronResultMu.Lock()
	ready := pronResults
	pronResults = map[string]string{}
	pronResultMu.Unlock()
	if metaStore == nil {
		return
	}
	changed := false
	for word, ipa := range ready {
		if !saved[word] {
			continue
		}
		r, ok := metaStore.Get(word)
		if !ok || r.Pron != "" {
			continue
		}
		r.Pron = ipa
		if metaStore.Set(word, r) == nil {
			changed = true
		}
	}
	if changed {
		invalidateSavedCards()
		draw()
	}
}
