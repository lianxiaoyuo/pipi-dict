// Package requestguard prevents unintended bursts of public free service
// requests. It never stores translation content.
package requestguard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hoverdict/internal/filelock"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	HourlyLimit       = 60
	DailyLimit        = 200
	RepeatWindow      = 10 * time.Second
	DayCharacterLimit = 4500 // leave headroom under MyMemory anonymous 5000 characters/day
)

type entry struct {
	WordHash string    `json:"word_hash"`
	When     time.Time `json:"when"`
	Chars    int       `json:"chars,omitempty"`
}
type Guard struct {
	mu      sync.Mutex
	entries []entry
	path    string
}

func Open(path string) (*Guard, error) {
	g := &Guard{path: path}
	if path != "" {
		entries, err := readEntries(path)
		if err != nil {
			return nil, err
		}
		g.entries = entries
	}
	return g, nil
}
func readEntries(path string) ([]entry, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("无法读取联网用量记录：%w", err)
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if stat.Size() > 2*1024*1024 {
		return nil, errors.New("联网用量记录异常过大")
	}
	var entries []entry
	if err := json.NewDecoder(f).Decode(&entries); err != nil {
		return nil, fmt.Errorf("联网用量记录损坏：%w", err)
	}
	if entries == nil || len(entries) > 5000 {
		return nil, errors.New("联网用量记录无效")
	}
	for _, e := range entries {
		if len(e.WordHash) != 64 || e.When.IsZero() || e.Chars < 0 || e.Chars > 5000 {
			return nil, errors.New("联网用量记录字段异常")
		}
	}
	return entries, nil
}
func hashWord(word string) string {
	sum := sha256.Sum256([]byte(word))
	return hex.EncodeToString(sum[:])
}
func atomicWrite(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".hoverdict-usage-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
func (g *Guard) Allow(word string, now time.Time) error { return g.allow(word, now, false) }

// AllowText reserves budget before an API request for a user-selected segment.
func (g *Guard) AllowText(text string, now time.Time) error {
	return g.allow("selection:"+text, now, true)
}
func (g *Guard) allow(word string, now time.Time, selection bool) error {
	word = strings.ToLower(strings.TrimSpace(word))
	if word == "" {
		return errors.New("单词为空")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	// An in-process mutex alone does NOT protect two EXE instances. Reload
	// the latest ledger only after taking the process-shared file lock.
	if g.path != "" {
		lock, err := filelock.Acquire(g.path + ".lock")
		if err != nil {
			return fmt.Errorf("无法锁定联网额度记录，已阻止本次查询：%w", err)
		}
		defer lock.Close()
		fresh, err := readEntries(g.path)
		if err != nil {
			return err
		} // fail closed on invalid or unreadable ledger
		g.entries = fresh
	}
	valid := make([]entry, 0, len(g.entries))
	hourly := 0
	dayChars := 0
	for _, e := range g.entries {
		age := now.Sub(e.When)
		if age >= 24*time.Hour {
			continue
		}
		valid = append(valid, e)
		dayChars += e.Chars
		if age < time.Hour {
			hourly++
		}
	}
	hash := hashWord(word)
	for _, e := range valid {
		age := now.Sub(e.When)
		if !selection && e.WordHash == hash && age >= 0 && age < RepeatWindow {
			return fmt.Errorf("为减少重复请求，同一个词 %d 秒内不重复联网查询", int(RepeatWindow.Seconds()))
		}
	}
	if hourly >= HourlyLimit {
		return fmt.Errorf("已达到每小时 %d 次联网查询保护上限", HourlyLimit)
	}
	charCount := utf8.RuneCountInString(strings.TrimPrefix(word, "selection:"))
	if dayChars+charCount > DayCharacterLimit {
		return fmt.Errorf("免费翻译字符预算不足（24小时上限 %d 字符）", DayCharacterLimit)
	}
	if len(valid) >= DailyLimit {
		return fmt.Errorf("已达到 24 小时 %d 次联网查询保护上限", DailyLimit)
	}
	next := append(valid, entry{WordHash: hash, When: now.UTC(), Chars: charCount})
	if g.path != "" {
		b, err := json.Marshal(next)
		if err != nil {
			return err
		}
		if err := atomicWrite(g.path, b); err != nil {
			return fmt.Errorf("联网额度记录保存失败，已阻止查询：%w", err)
		}
	}
	g.entries = next
	return nil
}
