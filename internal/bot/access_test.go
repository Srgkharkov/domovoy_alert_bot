package bot

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestAccessStorePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.json")
	s, err := OpenAccessStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Approve(42, "Мама", []string{"aqara"}); err != nil {
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
	if err := s2.Approve(7, "Спамер", []string{"tuya-1"}); err != nil {
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

func TestApproveMergesHomes(t *testing.T) {
	s, err := OpenAccessStore(filepath.Join(t.TempDir(), "access.json"))
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Approve(1, "x", []string{"aqara"})
	_ = s.Approve(1, "x", []string{"tuya-1", "aqara"})
	got, ok := s.Homes(1)
	if !ok || !slices.Equal(got, []string{"aqara", "tuya-1"}) {
		t.Fatalf("Homes(1) = %v, %v", got, ok)
	}
}

func TestParseAccessData(t *testing.T) {
	tests := []struct {
		in     string
		action string
		id     int64
		home   string
		ok     bool
	}{
		{"acc:ok:123", accessApprove, 123, "", true},
		{"acc:ok:123:tuya-123456", accessApprove, 123, "tuya-123456", true},
		{"acc:ok:123:*", accessApprove, 123, allHomes, true},
		{"acc:no:-100500", accessDeny, -100500, "", true},
		{"acc:rm:9", accessRevoke, 9, "", true},
		{"acc:xx:9", "", 0, "", false},
		{"acc:ok:abc", "", 0, "", false},
		{"acc:ok", "", 0, "", false},
		{"scene:1", "", 0, "", false},
	}
	for _, tt := range tests {
		action, id, home, ok := parseAccessData(tt.in)
		if action != tt.action || id != tt.id || home != tt.home || ok != tt.ok {
			t.Errorf("parseAccessData(%q) = %q, %d, %q, %v", tt.in, action, id, home, ok)
		}
	}
}
