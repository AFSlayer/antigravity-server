package updater

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ProgressFn reports download progress.
type ProgressFn func(downloaded, total int64)

// CheckWritable verifies that targetPath and its parent directory can be written to before downloading artifacts.
func CheckWritable(targetPath string) error {
	if targetPath == "" {
		return errors.New("target path cannot be empty")
	}

	targetDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("create target directory %s: %w", targetDir, err)
	}

	// Test write permissions by creating and removing a temporary check file
	tmpFile, err := os.CreateTemp(targetDir, ".permcheck.*.tmp")
	if err != nil {
		return fmt.Errorf("cannot write to directory %s (permission denied): %w", targetDir, err)
	}
	tmpName := tmpFile.Name()
	_ = tmpFile.Close()
	_ = os.Remove(tmpName)

	return nil
}

// DownloadAndInstall downloads the official Antigravity artifact and installs language_server to targetPath.
func DownloadAndInstall(ctx context.Context, url, targetPath string, progress ProgressFn) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if targetPath == "" {
		return errors.New("targetPath cannot be empty")
	}

	// Pre-flight check: ensure target directory is writable before initiating ~170MB download
	if err := CheckWritable(targetPath); err != nil {
		return err
	}

	// Defense-in-depth: validate download URL domain
	if err := ValidateDownloadURL(url); err != nil {
		return err
	}

	if strings.HasSuffix(url, ".dmg") {
		return errors.New("macOS uses DMG desktop bundle; please update via the Antigravity desktop app or use Linux for headless serve mode")
	}

	targetDir := filepath.Dir(targetPath)

	client := &http.Client{Timeout: 30 * time.Minute}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "antigravity-server-updater")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download from %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}

	total := resp.ContentLength
	var src io.Reader = resp.Body
	if progress != nil {
		src = &progressReader{reader: resp.Body, total: total, progress: progress}
	}

	// Create temporary file in the same directory to guarantee same-filesystem atomic rename
	tmpFile, err := os.CreateTemp(targetDir, "language_server.*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", targetDir, err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if strings.HasSuffix(url, ".tar.gz") {
		// Extract language_server from tar.gz
		if err := extractLanguageServerFromTarGz(src, tmpFile); err != nil {
			tmpFile.Close()
			return err
		}
	} else {
		// Direct executable stream
		if _, err := io.Copy(tmpFile, src); err != nil {
			tmpFile.Close()
			return fmt.Errorf("write binary: %w", err)
		}
	}

	if err := tmpFile.Chmod(0755); err != nil {
		tmpFile.Close()
		return fmt.Errorf("chmod 0755: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	// Guaranteed atomic replace
	return atomicReplace(tmpPath, targetPath)
}

func extractLanguageServerFromTarGz(r io.Reader, dst io.Writer) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("open gzip stream: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}

		clean := filepath.ToSlash(hdr.Name)
		if strings.HasSuffix(clean, "/resources/bin/language_server") || clean == "language_server" || strings.HasSuffix(clean, "/language_server") {
			if hdr.Typeflag == tar.TypeReg || hdr.Typeflag == tar.TypeRegA {
				if _, err := io.Copy(dst, tr); err != nil {
					return fmt.Errorf("extract language_server entry: %w", err)
				}
				return nil
			}
		}
	}

	return errors.New("language_server binary not found inside archive")
}

func atomicReplace(src, dst string) error {
	if runtime.GOOS == "windows" {
		// On Windows, moving over a running file fails; rename old first
		old := dst + ".old." + fmt.Sprintf("%d", time.Now().UnixNano())
		_ = os.Rename(dst, old)
		_ = os.Remove(old)
	}

	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("atomic rename failed (original binary preserved): %w", err)
	}
	return nil
}

type progressReader struct {
	reader     io.Reader
	total      int64
	downloaded int64
	progress   ProgressFn
	lastUpdate time.Time
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	if n > 0 {
		pr.downloaded += int64(n)
		now := time.Now()
		if now.Sub(pr.lastUpdate) > 200*time.Millisecond || pr.downloaded == pr.total || err != nil {
			pr.lastUpdate = now
			pr.progress(pr.downloaded, pr.total)
		}
	}
	return n, err
}
