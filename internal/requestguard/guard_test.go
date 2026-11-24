package requestguard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRepeat(t *testing.T) {
	var g Guard
	n := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	if err := g.Allow("apple", n); err != nil {
		t.Fatal(err)
	}
	if err := g.Allow("APPLE", n.Add(2*time.Second)); err == nil {
		t.Fatal("duplicate allowed")
	}
	if err := g.Allow("apple", n.Add(11*time.Second)); err != nil {
		t.Fatal(err)
	}
}
func TestHourAndDayLimits(t *testing.T) {
	var g Guard
	n := time.Now()
	for i := 0; i < HourlyLimit; i++ {
		if err := g.Allow(fmt.Sprint("w", i), n); err != nil {
			t.Fatal(err)
		}
	}
	if err := g.Allow("last", n); err == nil || !strings.Contains(err.Error(), "每小时") {
		t.Fatal("hour limit failed", err)
	}
	for i := HourlyLimit; i < DailyLimit; i++ {
		dt := n.Add(time.Duration(i/HourlyLimit) * time.Hour)
		if err := g.Allow(fmt.Sprint("w", i), dt); err != nil {
			t.Fatal(err)
		}
	}
	if err := g.Allow("over", n.Add(3*time.Hour)); err == nil || !strings.Contains(err.Error(), "24 小时") {
		t.Fatal("day limit failed", err)
	}
	if err := g.Allow("new", n.Add(25*time.Hour)); err != nil {
		t.Fatal(err)
	}
}

func TestPersistentUsageSurvivesRestart(t *testing.T) {
	p := filepath.Join(t.TempDir(), "usage.json")
	now := time.Now()
	first, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Allow("gentrification", now); err != nil {
		t.Fatal(err)
	}
	second, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Allow("gentrification", now.Add(time.Second)); err == nil {
		t.Fatal("restart reset repeat guard")
	}
	raw, err := os.ReadFile(p)
	if err != nil || strings.Contains(string(raw), "gentrification") {
		t.Fatalf("plaintext query recorded: %v", err)
	}
	if err := os.WriteFile(p, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(p); err == nil {
		t.Fatal("corrupt usage file should fail closed")
	}
}
func TestPersistFailureDoesNotConsumeQuota(t *testing.T) {
	dir := t.TempDir()
	badParent := filepath.Join(dir, "regular-file")
	if err := os.WriteFile(badParent, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	guard := &Guard{path: filepath.Join(badParent, "usage.json")}
	if err := guard.Allow("one", time.Now()); err == nil {
		t.Fatal("expected storage failure")
	}
	if len(guard.entries) != 0 {
		t.Fatal("failed request counted")
	}
}
