// Package bookmarkmeta preserves details only when the user explicitly saves
// a word. Old saved.json stays intact for backward compatibility.
package bookmarkmeta

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Record struct {
	Pron    string    `json:"pron,omitempty"`
	Meaning string    `json:"meaning,omitempty"`
	Source  string    `json:"source,omitempty"`
	SavedAt time.Time `json:"saved_at"`
}
type Store struct {
	Path  string
	Items map[string]Record
}

func Open(path string) (*Store, error) {
	s := &Store{Path: path, Items: map[string]Record{}}
	b, e := os.ReadFile(path)
	if os.IsNotExist(e) {
		return s, nil
	}
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(b, &s.Items); e != nil {
		return nil, fmt.Errorf("收藏释义文件损坏：%w", e)
	}
	if s.Items == nil {
		return nil, fmt.Errorf("收藏释义文件无效")
	}
	return s, nil
}
func (s *Store) Set(word string, r Record) error {
	word = strings.ToLower(strings.TrimSpace(word))
	if word == "" {
		return fmt.Errorf("单词为空")
	}
	old, had := s.Items[word]
	s.Items[word] = r
	if e := s.commit(); e != nil {
		if had {
			s.Items[word] = old
		} else {
			delete(s.Items, word)
		}
		return e
	}
	return nil
}
func (s *Store) Remove(word string) error {
	word = strings.ToLower(strings.TrimSpace(word))
	old, had := s.Items[word]
	delete(s.Items, word)
	if e := s.commit(); e != nil {
		if had {
			s.Items[word] = old
		}
		return e
	}
	return nil
}
func (s *Store) Get(word string) (Record, bool) {
	r, ok := s.Items[strings.ToLower(strings.TrimSpace(word))]
	return r, ok
}
func (s *Store) commit() error {
	b, e := json.MarshalIndent(s.Items, "", "  ")
	if e != nil {
		return e
	}
	dir := filepath.Dir(s.Path)
	if e = os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(dir, ".pipi-meta-*.tmp")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(0600); e != nil {
		f.Close()
		return e
	}
	if _, e = f.Write(append(b, '\n')); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(f.Name(), s.Path)
}
