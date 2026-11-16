package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rineharc/pr-tool-openai/internal/review"
)

func TestStoreRoundTripAndClone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.json")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	state := PRState{
		ViewedFiles: map[string]bool{"main.go": true},
		Drafts: []review.DraftComment{{
			Path: "main.go", Line: 4, Side: "RIGHT", Body: "Please test this.", HeadSHA: "abc",
		}},
		LastFile: "main.go",
	}
	if err := first.Save("acme/repo#1", state); err != nil {
		t.Fatal(err)
	}
	state.ViewedFiles["main.go"] = false

	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got := second.Load("acme/repo#1")
	if !got.ViewedFiles["main.go"] || got.LastFile != "main.go" || len(got.Drafts) != 1 {
		t.Fatalf("unexpected persisted state: %#v", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestStoreRejectsUnknownVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"version":99,"pullRequests":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("expected version error")
	}
}
