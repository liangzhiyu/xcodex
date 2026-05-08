package codex

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFindSessionByIndexUsesRecentFilesystemOrder(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CODEX_HOME", tmp)

	sessionsDir := filepath.Join(tmp, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	older := createSessionFile(t, filepath.Join(sessionsDir, "older.jsonl"), time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC))
	middle := createSessionFile(t, filepath.Join(sessionsDir, "middle.jsonl"), time.Date(2026, 5, 8, 11, 0, 0, 0, time.UTC))
	newest := createSessionFile(t, filepath.Join(sessionsDir, "newest.jsonl"), time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC))

	path, err := FindSessionByIndex(2, "", 10)
	if err != nil {
		t.Fatalf("FindSessionByIndex returned error: %v", err)
	}
	if path != middle {
		t.Fatalf("expected second newest session %q, got %q (newest=%q older=%q)", middle, path, newest, older)
	}
}

func TestFindSessionByIndexRejectsOutOfRange(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CODEX_HOME", tmp)

	sessionsDir := filepath.Join(tmp, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	createSessionFile(t, filepath.Join(sessionsDir, "only.jsonl"), time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC))

	_, err := FindSessionByIndex(2, "", 10)
	if err == nil {
		t.Fatal("expected out-of-range error")
	}
}

func createSessionFile(t *testing.T, path string, modTime time.Time) string {
	t.Helper()
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write session file: %v", err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	return path
}
