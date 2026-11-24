package onlinememo

import (
	"testing"
	"time"
)

func TestMemo(t *testing.T) {
	now := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	m := New(2, 10*time.Minute)
	m.Put("Apple", "苹果", now)
	if s, ok := m.Get("APPLE", now.Add(time.Minute)); !ok || s != "苹果" {
		t.Fatal("cache miss")
	}
	if _, ok := m.Get("apple", now.Add(10*time.Minute)); ok {
		t.Fatal("expired cache hit")
	}
	m.Put("a", "甲", now)
	m.Put("b", "乙", now.Add(time.Second))
	m.Put("c", "丙", now.Add(2*time.Second))
	if _, ok := m.Get("a", now.Add(3*time.Second)); ok {
		t.Fatal("oldest should be evicted")
	}
	if _, ok := m.Get("c", now.Add(3*time.Second)); !ok {
		t.Fatal("newest lost")
	}
}
func TestInvalidEntries(t *testing.T) {
	m := New(3, time.Minute)
	m.Put("", "x", time.Now())
	if _, ok := m.Get("", time.Now()); ok {
		t.Fatal("empty word stored")
	}
}
