package lsproc

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// logRetention is how long rotated language server logs are kept. The log grows by about
	// 2 MB a day on a machine in daily use, so this comes to around 60 MB.
	logRetention = 30 * 24 * time.Hour

	logRotationCheck = time.Hour
	rotatedLogLayout = "2006-01-02"
)

// truncateLog is replaced in tests: a truncate failing on a file that opened for writing is hard to arrange.
var truncateLog = func(file *os.File) error { return file.Truncate(0) }

// KeepLogRotated rotates the log at path now and whenever the day changes, until ctx ends.
func KeepLogRotated(ctx context.Context, path string) {
	ticker := time.NewTicker(logRotationCheck)
	defer ticker.Stop()
	for {
		if err := RotateLog(path, time.Now(), logRetention); err != nil {
			log.Printf("[lsproc] rotating %s: %v", path, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RotateLog copies the log to path.YYYY-MM-DD, named for the day before now, empties it, and
// removes copies older than keep. It does nothing further once a copy for that day exists, so
// calling it often is cheap.
//
// The language server holds the file open for as long as it runs and may outlive this process, so
// the file cannot be renamed out from under it. It opens the file in append mode, which makes its
// next write land at the new end after the truncate. A line written between the copy and the
// truncate is lost.
func RotateLog(path string, now time.Time, keep time.Duration) error {
	// A copy that cannot be removed is no reason to let the log itself keep growing.
	return errors.Join(pruneRotatedLogs(path, now.Add(-keep)), rotateLog(path, now))
}

func rotateLog(path string, now time.Time) error {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return nil
	}
	rotated := path + "." + now.AddDate(0, 0, -1).Format(rotatedLogLayout)
	if _, err := os.Stat(rotated); err == nil {
		return nil
	}
	// Opened before copying, so a log that cannot be emptied is not copied every time this runs.
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := copyLog(path, rotated); err != nil {
		return err
	}
	if err := truncateLog(file); err != nil {
		// Left behind, the copy would stop the retries, and the next day's would repeat all of it.
		_ = os.Remove(rotated)
		return err
	}
	return nil
}

// copyLog goes through a temporary file, so a copy that is cut short never reads as finished.
func copyLog(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if err == nil {
		// The original is about to be emptied, so the copy has to be on disk first.
		err = out.Sync()
	}
	if err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func pruneRotatedLogs(path string, before time.Time) error {
	dir, base := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var errs []error
	for _, entry := range entries {
		day, ok := strings.CutPrefix(entry.Name(), base+".")
		if !ok {
			continue
		}
		// A temporary copy here was left by an interrupted rotation, whatever its date.
		day, abandoned := strings.CutSuffix(day, ".tmp")
		// Anything else beside the log, such as a backup someone made, is not this code's to remove.
		date, err := time.ParseInLocation(rotatedLogLayout, day, before.Location())
		if err == nil && (abandoned || date.Before(before)) {
			if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}
