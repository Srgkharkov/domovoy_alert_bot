package bot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAccessStorePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.json")
	s, err := OpenAccessStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Approve(42, "Мама"); err != nil {
		t.Fatal(err)
	}
	if err := s.Deny(7, "Спамер"); err != nil {
		t.Fatal(err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %v, want 0600", fi.Mode().Perm())
	}

	s2, err := OpenAccessStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if !s2.IsApproved(42) || s2.IsDenied(42) {
		t.Error("42 should be approved after reload")
	}
	if !s2.IsDenied(7) || s2.IsApproved(7) {
		t.Error("7 should be denied after reload")
	}

	// Approving a denied chat clears the denial.
	if err := s2.Approve(7, "Спамер"); err != nil {
		t.Fatal(err)
	}
	if s2.IsDenied(7) || !s2.IsApproved(7) {
		t.Error("7 should move from denied to approved")
	}

	e, ok, err := s2.Revoke(42)
	if err != nil || !ok || e.Title != "Мама" {
		t.Fatalf("Revoke(42) = %+v, %v, %v", e, ok, err)
	}
	if s2.IsApproved(42) {
		t.Error("42 should be revoked")
	}
	if _, ok, _ := s2.Revoke(42); ok {
		t.Error("second revoke should report not found")
	}
}

func TestOpenAccessStoreMissingFile(t *testing.T) {
	s, err := OpenAccessStore(filepath.Join(t.TempDir(), "nope", "access.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Approved()) != 0 {
		t.Error("expected empty store")
	}
}

func TestIsAllowed(t *testing.T) {
	s, err := OpenAccessStore(filepath.Join(t.TempDir(), "access.json"))
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Approve(5, "x")

	b := &Bot{allowed: map[int64]bool{-100: true}}
	if !b.isAllowed(-100) || b.isAllowed(5) || b.isAllowed(1) {
		t.Error("without access store only config chats are allowed")
	}

	b.adminChatID, b.access = 1, s
	for id, want := range map[int64]bool{-100: true, 1: true, 5: true, 9: false} {
		if got := b.isAllowed(id); got != want {
			t.Errorf("isAllowed(%d) = %v, want %v", id, got, want)
		}
	}
}

func TestParseAccessData(t *testing.T) {
	tests := []struct {
		in     string
		action string
		id     int64
		ok     bool
	}{
		{"acc:ok:123", accessApprove, 123, true},
		{"acc:no:-100500", accessDeny, -100500, true},
		{"acc:rm:9", accessRevoke, 9, true},
		{"acc:xx:9", "", 0, false},
		{"acc:ok:abc", "", 0, false},
		{"acc:ok", "", 0, false},
		{"scene:1", "", 0, false},
	}
	for _, tt := range tests {
		action, id, ok := parseAccessData(tt.in)
		if action != tt.action || id != tt.id || ok != tt.ok {
			t.Errorf("parseAccessData(%q) = %q, %d, %v", tt.in, action, id, ok)
		}
	}
}
