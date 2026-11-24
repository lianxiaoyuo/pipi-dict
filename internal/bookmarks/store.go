// Package bookmarks persists a small word collection atomically, retaining a
// validated previous revision. It accepts the legacy HoverDict saved.json format.
package bookmarks

import (
	"encoding/json"
	"errors"
	"fmt"
	"hoverdict/internal/filelock"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Load attempts the primary file first, then its last known-good backup.
// restored is true when the backup was needed. Invalid files are never overwritten
// merely as a side effect of loading them.
func Load(path string) (words map[string]bool, restored bool, err error) {
	data, primaryErr := os.ReadFile(path)
	if primaryErr == nil {
		words, err = decode(data)
		if err == nil {
			return words, false, nil
		}
		primaryErr = fmt.Errorf("生词本损坏：%w", err)
	}
	if !errors.Is(primaryErr, os.ErrNotExist) {
		// Try last known-good backup even after a parse or disk failure.
		backup, backupErr := os.ReadFile(path + ".bak")
		if backupErr == nil {
			words, err = decode(backup)
			if err == nil {
				return words, true, nil
			}
			return nil, false, fmt.Errorf("主文件错误：%v；备份也损坏：%w", primaryErr, err)
		}
		return nil, false, fmt.Errorf("无法读取生词本：%w（备份：%v）", primaryErr, backupErr)
	}
	// Missing primary: recover an existing backup if available.
	backup, backupErr := os.ReadFile(path + ".bak")
	if backupErr == nil {
		words, err = decode(backup)
		if err != nil {
			return nil, false, fmt.Errorf("备份损坏：%w", err)
		}
		return words, true, nil
	}
	if !errors.Is(backupErr, os.ErrNotExist) {
		return nil, false, fmt.Errorf("读取生词备份失败：%w", backupErr)
	}
	return make(map[string]bool), false, nil
}
func decode(b []byte) (map[string]bool, error) {
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil, errors.New("空文件")
	}
	var words map[string]bool
	if err := json.Unmarshal(b, &words); err != nil {
		return nil, err
	}
	if words == nil {
		return nil, errors.New("无效的生词列表")
	}
	// Older data may contain false flags: treat them as deleted.
	for word, saved := range words {
		if !saved {
			delete(words, word)
		}
	}
	return words, nil
}

// Save commits a full snapshot to saved.json. The previous valid snapshot is
// retained in saved.json.bak; neither partial writes nor corrupt old files can
// silently destroy that backup.
func Save(path string, words map[string]bool) error {
	if path == "" {
		return errors.New("生词本路径为空")
	}
	out := make(map[string]bool, len(words))
	for w, ok := range words {
		if ok {
			out[w] = true
		}
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	old, err := os.ReadFile(path)
	if err == nil {
		if _, decodeErr := decode(old); decodeErr == nil {
			if err := atomicWrite(path+".bak", old); err != nil {
				return fmt.Errorf("备份原生词本失败：%w", err)
			}
		}
		// If old primary is corrupt, DO NOT overwrite a good backup.
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("读取现有生词本失败：%w", err)
	}
	if err := atomicWrite(path, b); err != nil {
		return fmt.Errorf("保存生词失败：%w", err)
	}
	return nil
}

// Update is a single transactional read-modify-write. Never trust a caller's
// snapshot: another process might have changed the file since it was read.
// The lock is process-shared and automatically released after a crash.
func Update(path, word string, remove bool) (map[string]bool, error) {
	word = strings.TrimSpace(word)
	if word == "" {
		return nil, errors.New("不能收藏或删除空单词")
	}
	lock, err := filelock.Acquire(path + ".lock")
	if err != nil {
		return nil, fmt.Errorf("生词本正在被其他进程使用：%w", err)
	}
	defer lock.Close()
	latest, _, err := Load(path)
	if err != nil {
		return nil, err
	}
	if remove {
		delete(latest, word)
	} else if latest[word] {
		delete(latest, word)
	} else {
		latest[word] = true
	}
	if err := Save(path, latest); err != nil {
		return nil, err
	}
	return latest, nil
}

// Toggle/Delete retain the pre-V3.3 API; the original map is only the UI
// rollback snapshot. The true source of data is reloaded under the file lock.
func Toggle(path string, original map[string]bool, word string) (map[string]bool, error) {
	next, err := Update(path, word, false)
	if err != nil {
		return original, err
	}
	return next, nil
}
func Delete(path string, original map[string]bool, word string) (map[string]bool, error) {
	next, err := Update(path, word, true)
	if err != nil {
		return original, err
	}
	return next, nil
}
func Sorted(words map[string]bool) []string {
	result := make([]string, 0, len(words))
	for w, ok := range words {
		if ok {
			result = append(result, w)
		}
	}
	sort.Strings(result)
	return result
}
func atomicWrite(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".hoverdict-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	defer func() {
		if f != nil {
			f.Close()
		}
	}()
	if err := f.Chmod(0600); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	f = nil
	return os.Rename(tmp, path)
}
