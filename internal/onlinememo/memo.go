// Package onlinememo implements a bounded, in-memory positive translation cache.
// Cached public translations are not persisted, synced, or bundled in the lexicon.
package onlinememo

import (
	"strings"
	"sync"
	"time"
)

type cacheEntry struct {
	meaning string
	savedAt time.Time
}
type Memo struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
	limit   int
	ttl     time.Duration
}

func New(limit int, ttl time.Duration) *Memo {
	if limit < 1 {
		limit = 1
	}
	if ttl <= 0 {
		ttl = time.Minute
	}
	return &Memo{entries: make(map[string]cacheEntry), limit: limit, ttl: ttl}
}
func (m *Memo) Get(word string, now time.Time) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[strings.ToLower(strings.TrimSpace(word))]
	if !ok {
		return "", false
	}
	if now.Before(e.savedAt) || now.Sub(e.savedAt) >= m.ttl {
		delete(m.entries, strings.ToLower(strings.TrimSpace(word)))
		return "", false
	}
	return e.meaning, true
}
func (m *Memo) Put(word, meaning string, now time.Time) {
	word = strings.ToLower(strings.TrimSpace(word))
	meaning = strings.TrimSpace(meaning)
	if word == "" || meaning == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.entries) >= m.limit { // discard expired entries, then the oldest
		for k, e := range m.entries {
			if now.Sub(e.savedAt) >= m.ttl {
				delete(m.entries, k)
			}
		}
		if len(m.entries) >= m.limit {
			oldest := word
			at := now.Add(time.Hour)
			for k, e := range m.entries {
				if e.savedAt.Before(at) {
					oldest = k
					at = e.savedAt
				}
			}
			delete(m.entries, oldest)
		}
	}
	m.entries[word] = cacheEntry{meaning: meaning, savedAt: now}
}
