package main

import (
	"errors"
	"testing"
)

func TestResolveCompressSessionFileUsesExplicitFile(t *testing.T) {
	path, err := resolveCompressSessionFile("/tmp/session.jsonl", "", "", 15)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if path != "/tmp/session.jsonl" {
		t.Fatalf("expected explicit file path, got %q", path)
	}
}

func TestResolveCompressSessionFileUsesIndexSelector(t *testing.T) {
	origIndex := findSessionByIndex
	defer func() { findSessionByIndex = origIndex }()

	called := false
	findSessionByIndex = func(index int, cwd string, limit int) (string, error) {
		called = true
		if index != 3 {
			t.Fatalf("expected index 3, got %d", index)
		}
		if cwd != "/work" {
			t.Fatalf("expected cwd /work, got %q", cwd)
		}
		if limit != 30 {
			t.Fatalf("expected limit 30, got %d", limit)
		}
		return "/tmp/by-index.jsonl", nil
	}

	path, err := resolveCompressSessionFile("", "3", "/work", 30)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !called {
		t.Fatal("expected findSessionByIndex to be called")
	}
	if path != "/tmp/by-index.jsonl" {
		t.Fatalf("unexpected path: %q", path)
	}
}

func TestResolveCompressSessionFileUsesIDSelector(t *testing.T) {
	origID := findSessionByID
	defer func() { findSessionByID = origID }()

	called := false
	findSessionByID = func(id string) (string, error) {
		called = true
		if id != "abc123" {
			t.Fatalf("expected id abc123, got %q", id)
		}
		return "/tmp/by-id.jsonl", nil
	}

	path, err := resolveCompressSessionFile("", "abc123", "", 15)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !called {
		t.Fatal("expected findSessionByID to be called")
	}
	if path != "/tmp/by-id.jsonl" {
		t.Fatalf("unexpected path: %q", path)
	}
}

func TestResolveCompressSessionFileFallsBackToLatest(t *testing.T) {
	origLatest := findLatestSession
	defer func() { findLatestSession = origLatest }()

	called := false
	findLatestSession = func() (string, error) {
		called = true
		return "/tmp/latest.jsonl", nil
	}

	path, err := resolveCompressSessionFile("", "", "", 15)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !called {
		t.Fatal("expected findLatestSession to be called")
	}
	if path != "/tmp/latest.jsonl" {
		t.Fatalf("unexpected path: %q", path)
	}
}

func TestResolveCompressSessionFileRejectsZeroIndex(t *testing.T) {
	_, err := resolveCompressSessionFile("", "0", "", 15)
	if err == nil {
		t.Fatal("expected error for zero index")
	}
}

func TestResolveCompressSessionFileReturnsLookupErrors(t *testing.T) {
	origID := findSessionByID
	defer func() { findSessionByID = origID }()

	want := errors.New("boom")
	findSessionByID = func(id string) (string, error) {
		return "", want
	}

	_, err := resolveCompressSessionFile("", "session-x", "", 15)
	if !errors.Is(err, want) {
		t.Fatalf("expected wrapped error %v, got %v", want, err)
	}
}

func TestResolveDiffSessionFileUsesIndexSelector(t *testing.T) {
	origIndex := findSessionByIndex
	defer func() { findSessionByIndex = origIndex }()

	called := false
	findSessionByIndex = func(index int, cwd string, limit int) (string, error) {
		called = true
		if index != 2 {
			t.Fatalf("expected index 2, got %d", index)
		}
		if cwd != "/repo" {
			t.Fatalf("expected cwd /repo, got %q", cwd)
		}
		if limit != 25 {
			t.Fatalf("expected limit 25, got %d", limit)
		}
		return "/tmp/diff-by-index.jsonl", nil
	}

	path, err := resolveDiffSessionFile("", "2", "/repo", 25)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !called {
		t.Fatal("expected findSessionByIndex to be called")
	}
	if path != "/tmp/diff-by-index.jsonl" {
		t.Fatalf("unexpected path: %q", path)
	}
}

func TestResolveDiffSessionFileUsesIDSelector(t *testing.T) {
	origID := findSessionByID
	defer func() { findSessionByID = origID }()

	called := false
	findSessionByID = func(id string) (string, error) {
		called = true
		if id != "session-42" {
			t.Fatalf("expected id session-42, got %q", id)
		}
		return "/tmp/diff-by-id.jsonl", nil
	}

	path, err := resolveDiffSessionFile("", "session-42", "", 15)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !called {
		t.Fatal("expected findSessionByID to be called")
	}
	if path != "/tmp/diff-by-id.jsonl" {
		t.Fatalf("unexpected path: %q", path)
	}
}
