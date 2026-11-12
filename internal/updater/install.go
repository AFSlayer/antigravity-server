package updater

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const maxLanguageServerBytes int64 = 1 << 30

func urlPathLower(rawURL string) string {
	if parsed, err := url.Parse(strings.TrimSpace(rawURL)); err == nil && parsed.Path != "" {
		return strings.ToLower(parsed.Path)
	}
	cut := rawURL
	if idx := strings.IndexAny(cut, "?#"); idx >= 0 {
		cut = cut[:idx]
	}
	return strings.ToLower(strings.TrimSpace(cut))
}

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
	return DownloadAndInstallWithBackupPolicy(ctx, url, targetPath, false, progress)
}

// DownloadAndInstallWithBackupPolicy installs language_server to targetPath. When
// preserveExistingBackup is true and targetPath+".bak" already exists, the verified
// backup is kept intact rather than overwritten with an unverified staged binary.
func DownloadAndInstallWithBackupPolicy(ctx context.Context, url, targetPath string, preserveExistingBackup bool, progress ProgressFn) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if targetPath == "" {
		return errors.New("targetPath cannot be empty")
	}

	// Defense-in-depth: validate download URL domain and archive format before touching disk
	if err := ValidateDownloadURL(url); err != nil {
		return err
	}

	lowerPath := urlPathLower(url)
	if strings.HasSuffix(lowerPath, ".dmg") {
		return errors.New("macOS uses DMG desktop bundle; please update via the Antigravity desktop app or use Linux for headless serve mode")
	}
	if strings.HasSuffix(lowerPath, ".exe") {
		return errors.New("Windows uses a desktop installer executable; please update via the Antigravity desktop app")
	}

	// Pre-flight check: ensure target directory is writable before initiating ~170MB download
	if err := CheckWritable(targetPath); err != nil {
		return err
	}

	targetDir := filepath.Dir(targetPath)

	client := &http.Client{
		Timeout: 30 * time.Minute,
		CheckRedirect: func(redirectReq *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			return ValidateDownloadURL(redirectReq.URL.String())
		},
	}
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

	if strings.HasSuffix(lowerPath, ".tar.gz") || strings.HasSuffix(lowerPath, ".tgz") {
		// Extract language_server from tar.gz
		if err := extractLanguageServerFromTarGz(src, tmpFile); err != nil {
			tmpFile.Close()
			return err
		}
	} else {
		// Direct executable stream
		written, err := io.Copy(tmpFile, io.LimitReader(src, maxLanguageServerBytes+1))
		if err != nil {
			tmpFile.Close()
			return fmt.Errorf("write binary: %w", err)
		}
		if written > maxLanguageServerBytes {
			tmpFile.Close()
			return fmt.Errorf("downloaded language_server exceeds maximum allowed size (%d bytes)", maxLanguageServerBytes)
		}
	}

	if err := tmpFile.Chmod(0755); err != nil {
		tmpFile.Close()
		return fmt.Errorf("chmod 0755: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	// Guaranteed atomic replace
	return atomicReplace(tmpPath, targetPath, preserveExistingBackup)
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
				if hdr.Size > maxLanguageServerBytes {
					return fmt.Errorf("language_server archive entry exceeds maximum allowed size (%d bytes)", maxLanguageServerBytes)
				}
				written, err := io.Copy(dst, io.LimitReader(tr, maxLanguageServerBytes+1))
				if err != nil {
					return fmt.Errorf("extract language_server entry: %w", err)
				}
				if written > maxLanguageServerBytes {
					return fmt.Errorf("language_server archive entry exceeds maximum allowed size (%d bytes)", maxLanguageServerBytes)
				}
				return nil
			}
		}
	}

	return errors.New("language_server binary not found inside archive")
}

var binaryEmbeddedVersionRe = regexp.MustCompile(`(?:"ideVersion"\s*:\s*"|antigravity-hub/)([0-9]+\.[0-9]+(?:\.[0-9]+)+)`)

// DetectBinaryVersion scans an existing language_server binary for an embedded release version.
func DetectBinaryVersion(binPath string) string {
	f, err := os.Open(binPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	const chunkSize = 4 << 20
	const overlap = 128
	buf := make([]byte, chunkSize+overlap)
	carry := 0
	for {
		n, readErr := f.Read(buf[carry : carry+chunkSize])
		if n > 0 {
			window := buf[:carry+n]
			if m := binaryEmbeddedVersionRe.FindSubmatch(window); len(m) > 1 {
				return string(m[1])
			}
			if len(window) > overlap {
				copy(buf[:overlap], window[len(window)-overlap:])
				carry = overlap
			} else {
				carry = len(window)
			}
		}
		if readErr != nil {
			break
		}
	}
	return ""
}

func copyBackupFile(src, dst string, allowHardLink bool) error {
	dir := filepath.Dir(dst)
	if allowHardLink && runtime.GOOS != "windows" {
		linkTmp := filepath.Join(dir, fmt.Sprintf(".language_server.bak.link-%d", time.Now().UnixNano()))
		if err := os.Link(src, linkTmp); err == nil {
			if renErr := os.Rename(linkTmp, dst); renErr == nil {
				return nil
			}
			_ = os.Remove(linkTmp)
		}
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmpFile, err := os.CreateTemp(dir, ".language_server.bak.tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := io.Copy(tmpFile, in); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Chmod(0o755); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		_ = os.Remove(dst)
	}
	return os.Rename(tmpPath, dst)
}

// RestoreBackup atomically restores targetPath from targetPath+".bak" into a fresh
// inode so in-place writes to targetPath never mutate the backup copy.
func RestoreBackup(targetPath string) error {
	if targetPath == "" {
		return errors.New("target path cannot be empty")
	}
	bak := targetPath + ".bak"
	info, err := os.Stat(bak)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("backup path %s is a directory", bak)
	}
	return copyBackupFile(bak, targetPath, false)
}

func atomicReplace(src, dst string, preserveExistingBackup bool) error {
	bak := dst + ".bak"
	if info, err := os.Stat(dst); err == nil && !info.IsDir() && src != dst {
		keepExisting := false
		if preserveExistingBackup {
			if bakInfo, bakErr := os.Stat(bak); bakErr == nil && !bakInfo.IsDir() {
				keepExisting = true
			}
		}
		if !keepExisting {
			if berr := copyBackupFile(dst, bak, true); berr != nil {
				log.Printf("[updater] warning: could not create backup %s: %v", bak, berr)
			}
		}
	}

	var windowsOld string
	if runtime.GOOS == "windows" {
		// On Windows, moving over a running file fails; rename old aside first and only delete after replacement succeeds.
		if _, statErr := os.Stat(dst); statErr == nil {
			old := fmt.Sprintf("%s.old.%d", dst, time.Now().UnixNano())
			if renErr := os.Rename(dst, old); renErr == nil {
				windowsOld = old
			}
		}
	}

	if err := os.Rename(src, dst); err != nil {
		if windowsOld != "" {
			if restoreErr := os.Rename(windowsOld, dst); restoreErr != nil {
				return fmt.Errorf("atomic rename failed (%w) and rollback of %s failed (%v)", err, windowsOld, restoreErr)
			}
		}
		return fmt.Errorf("atomic rename failed (original binary preserved): %w", err)
	}
	if windowsOld != "" {
		_ = os.Remove(windowsOld)
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
