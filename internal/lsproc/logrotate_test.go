package lsproc

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

var rotationDay = time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)

const rotationKeep = 30 * 24 * time.Hour

func readLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestRotateLogMovesContentsAsideOnceADay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.log")
	writer, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := writer.WriteString("before\n"); err != nil {
		t.Fatal(err)
	}

	if err := RotateLog(path, rotationDay, rotationKeep); err != nil {
		t.Fatal(err)
	}

	first := path + ".2026-11-04"
	if got := readLog(t, first); got != "before\n" {
		t.Errorf("rotated copy = %q", got)
	}
	// The process holding the file keeps writing to it, from the start and not past a hole.
	if _, err := writer.WriteString("after\n"); err != nil {
		t.Fatal(err)
	}
	if got := readLog(t, path); got != "after\n" {
		t.Errorf("log after rotation = %q", got)
	}

	// Later the same day, nothing moves.
	if err := RotateLog(path, rotationDay.Add(6*time.Hour), rotationKeep); err != nil {
		t.Fatal(err)
	}
	if got := readLog(t, path); got != "after\n" {
		t.Errorf("rotated twice in a day, log = %q", got)
	}

	// The next day it does.
	if err := RotateLog(path, rotationDay.AddDate(0, 0, 1), rotationKeep); err != nil {
		t.Fatal(err)
	}
	if got := readLog(t, path+".2026-11-05"); got != "after\n" {
		t.Errorf("next rotated copy = %q", got)
	}
	if got := readLog(t, first); got != "before\n" {
		t.Errorf("earlier copy changed to %q", got)
	}
}

func TestRotateLogRemovesOnlyOldRotatedCopies(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.log")
	names := map[string]bool{
		"server.log.2026-10-01":  false, // past the limit
		"server.log.2026-10-10":  true,
		"server.log.bak":         true,
		"server.log.2026-01-01x": true,
		"other.log.2020-01-01":   true,
	}
	for name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := RotateLog(path, rotationDay, rotationKeep); err != nil {
		t.Fatal(err)
	}

	for name, kept := range names {
		_, err := os.Stat(filepath.Join(dir, name))
		if exists := err == nil; exists != kept {
			t.Errorf("%s: exists = %v, want %v", name, exists, kept)
		}
	}
}

func TestRotateLogLeavesNothingToDoAlone(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.log")
	if err := RotateLog(missing, rotationDay, rotationKeep); err != nil {
		t.Errorf("missing log: %v", err)
	}
	empty := filepath.Join(dir, "empty.log")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RotateLog(empty, rotationDay, rotationKeep); err != nil {
		t.Errorf("empty log: %v", err)
	}
	if _, err := os.Stat(empty + ".2026-11-04"); err == nil {
		t.Error("an empty log was copied")
	}
}
