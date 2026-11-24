package bookmarks

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSaveLoadAndBackup(t *testing.T) {
	p := filepath.Join(t.TempDir(), "saved.json")
	if err := Save(p, map[string]bool{"apple": true, "old": false}); err != nil {
		t.Fatal(err)
	}
	if err := Save(p, map[string]bool{"apple": true, "banana": true}); err != nil {
		t.Fatal(err)
	}
	got, restored, err := Load(p)
	if err != nil || restored || !reflect.DeepEqual(got, map[string]bool{"apple": true, "banana": true}) {
		t.Fatalf("%v %v %v", got, restored, err)
	}
	if err := os.WriteFile(p, []byte("invalid JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	got, restored, err = Load(p)
	if err != nil || !restored || !reflect.DeepEqual(got, map[string]bool{"apple": true}) {
		t.Fatalf("bad backup %v %v %v", got, restored, err)
	}
	if err := Save(p, map[string]bool{"apple": true, "car": true}); err != nil {
		t.Fatal(err)
	}
	got, _, err = Load(p)
	if err != nil || !got["car"] {
		t.Fatal(got, err)
	}
	backup, err := os.ReadFile(p + ".bak")
	if err != nil || string(backup) == "invalid JSON" {
		t.Fatalf("backup destroyed %v %v", string(backup), err)
	}
}
func TestDeleteExactAndSaveFailureRollback(t *testing.T) {
	p := filepath.Join(t.TempDir(), "saved.json")
	words := map[string]bool{"apple": true, "banana": true, "computer": true}
	if err := Save(p, words); err != nil {
		t.Fatal(err)
	}
	newWords, err := Delete(p, words, "computer")
	if err != nil || !newWords["apple"] || !newWords["banana"] || newWords["computer"] {
		t.Fatalf("wrong delete %v %v", newWords, err)
	}
	bad := filepath.Join(t.TempDir(), "notdir")
	if err := os.WriteFile(bad, []byte("no"), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := Toggle(filepath.Join(bad, "saved.json"), newWords, "apple")
	if err == nil || !reflect.DeepEqual(after, newWords) {
		t.Fatalf("rollback expected %v %v", after, err)
	}
}
func TestUnreadableOrCorruptNeverOverwritten(t *testing.T) {
	p := filepath.Join(t.TempDir(), "saved.json")
	if err := os.WriteFile(p, []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(p); err == nil {
		t.Fatal("expected error")
	}
	data, _ := os.ReadFile(p)
	if string(data) != "not json" {
		t.Fatal("corrupt primary changed")
	}
}
