package lsproc

import (
	"context"
	"errors"
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

func TestKeepLogRotatedRotatesAtOnceAndStopsWithTheContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.log")
	if err := os.WriteFile(path, []byte("line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		KeepLogRotated(ctx, path)
		close(done)
	}()

	rotated := path + "." + time.Now().AddDate(0, 0, -1).Format(rotatedLogLayout)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if data, err := os.ReadFile(path); err == nil && len(data) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the log was not rotated")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := readLog(t, rotated); got != "line\n" {
		t.Errorf("rotated copy = %q", got)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("it did not stop with the context")
	}
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

func TestRotateLogRemovesAbandonedTemporaryCopies(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.log")
	abandoned := path + ".2026-11-01.tmp"
	unrelated := path + ".backup.tmp"
	for _, name := range []string{abandoned, unrelated} {
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := RotateLog(path, rotationDay, rotationKeep); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(abandoned); err == nil {
		t.Error("abandoned temporary copy is still there")
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Errorf("unrelated file: %v", err)
	}
}

func TestRotateLogStillRotatesWhenAnOldCopyCannotBeRemoved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.log")
	if err := os.WriteFile(path, []byte("line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A directory with something in it stands in for a copy that cannot be removed.
	stuck := path + ".2020-01-01"
	if err := os.Mkdir(stuck, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stuck, "keep"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := RotateLog(path, rotationDay, rotationKeep); err == nil {
		t.Error("the failed removal was not reported")
	}

	if got := readLog(t, path+".2026-11-04"); got != "line\n" {
		t.Errorf("rotated copy = %q", got)
	}
	if got := readLog(t, path); got != "" {
		t.Errorf("log after rotation = %q", got)
	}
}

func TestRotateLogDoesNotCopyALogItCannotEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.log")
	if err := os.WriteFile(path, []byte("line\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	if f, err := os.OpenFile(path, os.O_WRONLY, 0); err == nil {
		_ = f.Close()
		t.Skip("permissions are not enforced for this user")
	}

	for day := 0; day < 3; day++ {
		if err := RotateLog(path, rotationDay.AddDate(0, 0, day), rotationKeep); err == nil {
			t.Error("the failure was not reported")
		}
	}

	matches, err := filepath.Glob(path + ".*")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Errorf("copies were left behind: %v", matches)
	}
	if got := readLog(t, path); got != "line\n" {
		t.Errorf("log changed to %q", got)
	}
}

func TestRotateLogDropsTheCopyWhenTheTruncateFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.log")
	if err := os.WriteFile(path, []byte("line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	working := truncateLog
	t.Cleanup(func() { truncateLog = working })
	truncateLog = func(*os.File) error { return errors.New("refused") }

	if err := RotateLog(path, rotationDay, rotationKeep); err == nil {
		t.Error("the failure was not reported")
	}
	matches, err := filepath.Glob(path + ".*")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Errorf("copies were left behind: %v", matches)
	}
	if got := readLog(t, path); got != "line\n" {
		t.Errorf("log changed to %q", got)
	}

	// Having left no copy, a later check the same day tries again.
	truncateLog = working
	if err := RotateLog(path, rotationDay.Add(time.Hour), rotationKeep); err != nil {
		t.Fatal(err)
	}
	if got := readLog(t, path+".2026-11-04"); got != "line\n" {
		t.Errorf("rotated copy = %q", got)
	}
	if got := readLog(t, path); got != "" {
		t.Errorf("log after rotation = %q", got)
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
