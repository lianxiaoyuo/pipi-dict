//go:build windows

package main

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const dictionarySize = 47104000 // ECDICT database, 764507 indexed entries.

var dictionaryDB *sqliteConn
var userDB *sqliteConn
var savedOrder []string

// Small bounded cache avoids re-preparing identical SELECT statements when
// EN_CHANGE, offline/online checks and painting request the same word.
// The immutable lexicon makes both hits and misses safe to cache.
const maxEntryCache = 512

type cachedEntry struct {
	entry Entry
	found bool
}

var entryCache = struct {
	sync.Mutex
	byWord map[string]cachedEntry
	order  []string
}{byWord: make(map[string]cachedEntry, maxEntryCache)}

func getEntry(word string) (Entry, bool) {
	word = strings.ToLower(strings.TrimSpace(word))
	if word == "" {
		return Entry{}, false
	}
	entryCache.Lock()
	if e, ok := entryCache.byWord[word]; ok {
		entryCache.Unlock()
		return e.entry, e.found
	}
	entryCache.Unlock()
	if dictionaryDB == nil {
		return Entry{}, false
	}
	e, found, err := dictionaryDB.queryEntry(word)
	if err != nil {
		logDiagnostic("sqlite_lookup", err.Error())
		return Entry{}, false
	}
	entryCache.Lock()
	if _, ok := entryCache.byWord[word]; !ok {
		if len(entryCache.order) >= maxEntryCache {
			delete(entryCache.byWord, entryCache.order[0])
			entryCache.order = entryCache.order[1:]
		}
		entryCache.byWord[word] = cachedEntry{e, found}
		entryCache.order = append(entryCache.order, word)
	}
	entryCache.Unlock()
	return e, found
}

func ensureDictionary(path string) error {
	if stat, err := os.Stat(path); err == nil && stat.Size() == dictionarySize {
		return nil
	}
	if len(embeddedDictionary) == 0 {
		return errors.New("词库资源不存在")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	gz, err := gzip.NewReader(bytes.NewReader(embeddedDictionary))
	if err != nil {
		return err
	}
	defer gz.Close()
	f, err := os.CreateTemp(filepath.Dir(path), ".pipidict-lexicon-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	n, err := io.Copy(f, gz)
	if err != nil {
		f.Close()
		return err
	}
	if n != dictionarySize {
		f.Close()
		return fmt.Errorf("词库解压大小不正确：%d", n)
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	// Windows cannot replace an open database. Preserve any old file rather
	// than deleting an unexpected version before the replacement is valid.
	if _, err = os.Stat(path); err == nil {
		_ = os.Remove(path + ".previous")
		if err = os.Rename(path, path+".previous"); err != nil {
			return err
		}
	}
	return os.Rename(f.Name(), path)
}
func openDictionary(dir string) (*sqliteConn, error) {
	path := filepath.Join(dir, "dictionary.db")
	stat, statErr := os.Stat(path)
	fresh := statErr != nil || stat.Size() != dictionarySize
	if err := ensureDictionary(path); err != nil {
		return nil, err
	}
	conn, err := sqlOpenFile(path, true)
	if err != nil {
		return nil, err
	}
	// Cheap indexed database row count on every start (~milliseconds),
	// full integrity scan only after initial extraction (~one second).
	count, err := conn.countOrVersion("SELECT count(*) FROM dictionary")
	if err != nil || count != "764507" {
		conn.Close()
		return nil, fmt.Errorf("词库词条数校验失败: %v, count=%s", err, count)
	}
	if fresh {
		result, err := conn.countOrVersion("PRAGMA quick_check")
		if err != nil || result != "ok" {
			conn.Close()
			return nil, fmt.Errorf("词库首次完整性校验失败: %v %s", err, result)
		}
	}
	return conn, nil
}
