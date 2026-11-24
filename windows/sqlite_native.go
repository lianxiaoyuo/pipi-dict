//go:build windows

package main

// Uses Microsoft's Windows SQLite runtime (winsqlite3.dll) rather than an
// undocumented third-party endpoint or a CGO dependency. Windows 10/11 ship it.
// Native calls are isolated here so application code never constructs SQL.

import (
	"errors"
	"fmt"
	"hoverdict/internal/bookmarkmeta"
	"hoverdict/internal/bookmarks"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var winsqlite = syscall.NewLazyDLL("winsqlite3.dll")
var (
	sqOpen        = winsqlite.NewProc("sqlite3_open_v2")
	sqClose       = winsqlite.NewProc("sqlite3_close")
	sqPrepare     = winsqlite.NewProc("sqlite3_prepare_v2")
	sqStep        = winsqlite.NewProc("sqlite3_step")
	sqFinalize    = winsqlite.NewProc("sqlite3_finalize")
	sqBindText    = winsqlite.NewProc("sqlite3_bind_text")
	sqColumnText  = winsqlite.NewProc("sqlite3_column_text")
	sqColumnBytes = winsqlite.NewProc("sqlite3_column_bytes")
	sqErrMsg      = winsqlite.NewProc("sqlite3_errmsg")
	sqBusy        = winsqlite.NewProc("sqlite3_busy_timeout")
)

const (
	sqliteOK        = 0
	sqliteRow       = 100
	sqliteDone      = 101
	sqliteReadOnly  = 1
	sqliteReadWrite = 2
	sqliteCreate    = 4
)

type sqliteConn struct{ ptr uintptr }
type sqliteStmt struct {
	conn *sqliteConn
	ptr  uintptr
}

func sqliteBytes(s string) ([]byte, error) {
	if strings.IndexByte(s, 0) >= 0 {
		return nil, errors.New("SQLite text contains NUL")
	}
	return append([]byte(s), 0), nil
}
func sqlOpenFile(path string, readOnly bool) (*sqliteConn, error) {
	if err := winsqlite.Load(); err != nil {
		return nil, fmt.Errorf("Windows SQLite 不可用: %w", err)
	}
	name, err := sqliteBytes(path)
	if err != nil {
		return nil, err
	}
	flags := uintptr(sqliteReadWrite | sqliteCreate)
	if readOnly {
		flags = sqliteReadOnly
	}
	conn := &sqliteConn{}
	rc, _, _ := sqOpen.Call(uintptr(unsafe.Pointer(&name[0])), uintptr(unsafe.Pointer(&conn.ptr)), flags, 0)
	runtime.KeepAlive(name)
	if rc != sqliteOK {
		msg := conn.err("打开数据库")
		conn.Close()
		return nil, msg
	}
	sqBusy.Call(conn.ptr, 5000)
	if readOnly {
		_ = conn.exec("PRAGMA query_only=ON")
	} else {
		if err = conn.exec("PRAGMA journal_mode=WAL"); err != nil {
			conn.Close()
			return nil, err
		}
		if err = conn.exec("PRAGMA synchronous=FULL"); err != nil {
			conn.Close()
			return nil, err
		}
		_ = conn.exec("PRAGMA busy_timeout=5000")
	}
	return conn, nil
}
func (d *sqliteConn) Close() {
	if d != nil && d.ptr != 0 {
		sqClose.Call(d.ptr)
		d.ptr = 0
	}
}
func (d *sqliteConn) err(action string) error {
	if d == nil || d.ptr == 0 {
		return errors.New(action + ": no SQLite connection")
	}
	p, _, _ := sqErrMsg.Call(d.ptr)
	if p == 0 {
		return fmt.Errorf("%s: SQLite error", action)
	}
	buf := make([]byte, 0, 256)
	for i := uintptr(0); i < 1024; i++ {
		b := *(*byte)(unsafe.Pointer(p + i))
		if b == 0 {
			break
		}
		buf = append(buf, b)
	}
	return fmt.Errorf("%s: %s", action, string(buf))
}
func (d *sqliteConn) prepare(sql string) (*sqliteStmt, error) {
	if d == nil || d.ptr == 0 {
		return nil, errors.New("SQLite not open")
	}
	b, err := sqliteBytes(sql)
	if err != nil {
		return nil, err
	}
	st := &sqliteStmt{conn: d}
	rc, _, _ := sqPrepare.Call(d.ptr, uintptr(unsafe.Pointer(&b[0])), ^uintptr(0), uintptr(unsafe.Pointer(&st.ptr)), 0)
	runtime.KeepAlive(b)
	if rc != sqliteOK {
		return nil, d.err("prepare SQLite")
	}
	return st, nil
}
func (s *sqliteStmt) Close() {
	if s != nil && s.ptr != 0 {
		sqFinalize.Call(s.ptr)
		s.ptr = 0
	}
}
func (s *sqliteStmt) bind(i int, value string) error {
	b, err := sqliteBytes(value)
	if err != nil {
		return err
	}
	// SQLITE_TRANSIENT (-1): SQLite copies UTF-8 before Go releases the buffer.
	rc, _, _ := sqBindText.Call(s.ptr, uintptr(i), uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)-1), ^uintptr(0))
	runtime.KeepAlive(b)
	if rc != sqliteOK {
		return s.conn.err("bind SQLite")
	}
	return nil
}
func (s *sqliteStmt) step() (uintptr, error) {
	rc, _, _ := sqStep.Call(s.ptr)
	if rc != sqliteRow && rc != sqliteDone {
		return rc, s.conn.err("step SQLite")
	}
	return rc, nil
}
func (s *sqliteStmt) text(i int) string {
	p, _, _ := sqColumnText.Call(s.ptr, uintptr(i))
	if p == 0 {
		return ""
	}
	n, _, _ := sqColumnBytes.Call(s.ptr, uintptr(i))
	if n == 0 || n > 4<<20 {
		return ""
	}
	return string(unsafe.Slice((*byte)(unsafe.Pointer(p)), int(n)))
}
func (d *sqliteConn) exec(query string) error {
	st, err := d.prepare(query)
	if err != nil {
		return err
	}
	defer st.Close()
	for {
		rc, err := st.step()
		if err != nil {
			return err
		}
		if rc == sqliteDone {
			return nil
		}
	}
}
func (d *sqliteConn) countOrVersion(sql string) (string, error) {
	st, err := d.prepare(sql)
	if err != nil {
		return "", err
	}
	defer st.Close()
	rc, err := st.step()
	if err != nil {
		return "", err
	}
	if rc != sqliteRow {
		return "", errors.New("SQLite row missing")
	}
	return st.text(0), nil
}
func (d *sqliteConn) queryEntry(word string) (Entry, bool, error) {
	st, err := d.prepare("SELECT phonetic, translation FROM dictionary WHERE word = ? COLLATE NOCASE LIMIT 1")
	if err != nil {
		return Entry{}, false, err
	}
	defer st.Close()
	if err = st.bind(1, word); err != nil {
		return Entry{}, false, err
	}
	rc, err := st.step()
	if err != nil {
		return Entry{}, false, err
	}
	if rc != sqliteRow {
		return Entry{}, false, nil
	}
	return Entry{Pron: st.text(0), Meaning: st.text(1)}, true, nil
}
func (d *sqliteConn) bookmark(word string) (bookmarkmeta.Record, bool, error) {
	st, err := d.prepare("SELECT pron, meaning, source, saved_at FROM bookmarks WHERE word=? LIMIT 1")
	if err != nil {
		return bookmarkmeta.Record{}, false, err
	}
	defer st.Close()
	if err = st.bind(1, word); err != nil {
		return bookmarkmeta.Record{}, false, err
	}
	rc, err := st.step()
	if err != nil {
		return bookmarkmeta.Record{}, false, err
	}
	if rc != sqliteRow {
		return bookmarkmeta.Record{}, false, nil
	}
	at, _ := time.Parse(time.RFC3339Nano, st.text(3))
	return bookmarkmeta.Record{Pron: st.text(0), Meaning: st.text(1), Source: st.text(2), SavedAt: at}, true, nil
}
func (d *sqliteConn) savedWords() (map[string]bool, []string, error) {
	st, err := d.prepare("SELECT word FROM bookmarks ORDER BY saved_at DESC, word ASC")
	if err != nil {
		return nil, nil, err
	}
	defer st.Close()
	all := map[string]bool{}
	order := make([]string, 0, 256)
	for {
		rc, err := st.step()
		if err != nil {
			return nil, nil, err
		}
		if rc == sqliteDone {
			break
		}
		w := st.text(0)
		if w != "" {
			all[w] = true
			order = append(order, w)
		}
	}
	return all, order, nil
}
func (d *sqliteConn) updateBookmark(word string, r bookmarkmeta.Record) error {
	st, err := d.prepare("UPDATE bookmarks SET pron=?,meaning=?,source=? WHERE word=?")
	if err != nil {
		return err
	}
	defer st.Close()
	for i, v := range []string{r.Pron, r.Meaning, r.Source, word} {
		if err = st.bind(i+1, v); err != nil {
			return err
		}
	}
	_, err = st.step()
	return err
}
func (d *sqliteConn) putBookmark(word string, r bookmarkmeta.Record) error {
	st, err := d.prepare("INSERT OR REPLACE INTO bookmarks(word,pron,meaning,source,saved_at) VALUES(?,?,?,?,?)")
	if err != nil {
		return err
	}
	defer st.Close()
	at := r.SavedAt
	if at.IsZero() {
		at = time.Now()
	}
	for i, v := range []string{word, r.Pron, r.Meaning, r.Source, at.UTC().Format("2006-01-02T15:04:05.000000000Z07:00")} {
		if err = st.bind(i+1, v); err != nil {
			return err
		}
	}
	_, err = st.step()
	return err
}
func (d *sqliteConn) delBookmark(word string) error {
	st, err := d.prepare("DELETE FROM bookmarks WHERE word=?")
	if err != nil {
		return err
	}
	defer st.Close()
	if err = st.bind(1, word); err != nil {
		return err
	}
	_, err = st.step()
	return err
}
func (d *sqliteConn) toggle(word string, record bookmarkmeta.Record, remove bool) error {
	if err := d.exec("BEGIN IMMEDIATE"); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = d.exec("ROLLBACK")
		}
	}()
	_, exists, err := d.bookmark(word)
	if err != nil {
		return err
	}
	if remove || exists {
		err = d.delBookmark(word)
	} else {
		err = d.putBookmark(word, record)
	}
	if err != nil {
		return err
	}
	if err = d.exec("COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}
func (d *sqliteConn) migrateLegacy(dir string) error {
	version, err := d.countOrVersion("PRAGMA user_version")
	if err != nil {
		return err
	}
	if version != "0" {
		return nil
	}
	legacy := filepath.Join(dir, "saved.json")
	words, _, err := bookmarks.Load(legacy)
	if err != nil {
		return fmt.Errorf("旧生词本无法迁移（原文件仍保留）: %w", err)
	}
	details, e := bookmarkmeta.Open(filepath.Join(dir, "saved_details.json"))
	if e != nil {
		return fmt.Errorf("旧释义文件无法迁移（原文件仍保留）: %w", e)
	}
	if err = d.exec("BEGIN IMMEDIATE"); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = d.exec("ROLLBACK")
		}
	}()
	keys := make([]string, 0, len(words))
	for w, ok := range words {
		if ok {
			keys = append(keys, strings.ToLower(strings.TrimSpace(w)))
		}
	}
	sort.Strings(keys)
	for _, word := range keys {
		if word == "" {
			continue
		}
		r, _ := details.Get(word)
		if r.SavedAt.IsZero() {
			r.SavedAt = time.Now()
		}
		if err = d.putBookmark(word, r); err != nil {
			return err
		}
	}
	if err = d.exec("PRAGMA user_version=1"); err != nil {
		return err
	}
	if err = d.exec("COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}
func (d *sqliteConn) getSetting(key string) (string, error) {
	st, err := d.prepare("SELECT value FROM settings WHERE key=? LIMIT 1")
	if err != nil {
		return "", err
	}
	defer st.Close()
	if err = st.bind(1, key); err != nil {
		return "", err
	}
	rc, err := st.step()
	if err != nil {
		return "", err
	}
	if rc != sqliteRow {
		return "", nil
	}
	return st.text(0), nil
}
func (d *sqliteConn) setSetting(key, value string) error {
	st, err := d.prepare("INSERT OR REPLACE INTO settings(key,value) VALUES(?,?)")
	if err != nil {
		return err
	}
	defer st.Close()
	if err = st.bind(1, key); err != nil {
		return err
	}
	if err = st.bind(2, value); err != nil {
		return err
	}
	_, err = st.step()
	return err
}
func openUserSQLite(path string) (*sqliteConn, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sqlOpenFile(path, false)
	if err != nil {
		return nil, err
	}
	if err = db.exec("CREATE TABLE IF NOT EXISTS bookmarks(word TEXT PRIMARY KEY COLLATE NOCASE,pron TEXT NOT NULL DEFAULT '',meaning TEXT NOT NULL DEFAULT '',source TEXT NOT NULL DEFAULT '',saved_at TEXT NOT NULL)"); err != nil {
		db.Close()
		return nil, err
	}
	if err = db.exec("CREATE TABLE IF NOT EXISTS settings(key TEXT PRIMARY KEY, value TEXT NOT NULL)"); err != nil {
		db.Close()
		return nil, err
	}
	if err = db.migrateLegacy(filepath.Dir(path)); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// SqliteMetaStore intentionally mimics the old Get/Set methods, while writes
// now use the *same* durable SQLite row as the favorite itself.
type sqliteMetaStore struct{ db *sqliteConn }

func (s *sqliteMetaStore) Get(word string) (bookmarkmeta.Record, bool) {
	if s == nil || s.db == nil {
		return bookmarkmeta.Record{}, false
	}
	r, ok, _ := s.db.bookmark(word)
	return r, ok
}
func (s *sqliteMetaStore) Set(word string, r bookmarkmeta.Record) error {
	if s == nil || s.db == nil {
		return errors.New("SQLite unavailable")
	}
	return s.db.updateBookmark(word, r)
}
func (s *sqliteMetaStore) Remove(word string) error {
	if s == nil || s.db == nil {
		return errors.New("SQLite unavailable")
	}
	return s.db.updateBookmark(word, bookmarkmeta.Record{})
}
