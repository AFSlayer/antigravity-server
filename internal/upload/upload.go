package upload

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultTTL is how long uploaded files are kept before automatic cleanup.
	DefaultTTL = 7 * 24 * time.Hour
	// UploadAPIPath is the endpoint for streaming file uploads.
	UploadAPIPath = "/__agy/api/upload"
)

// Options configures an Uploader.
type Options struct {
	WorkspaceRoot string
	TTL           time.Duration
}

// Uploader handles streaming file uploads to the workspace and periodic cleanup.
type Uploader struct {
	workspaceRoot string
	ttl           time.Duration
}

// New creates a new Uploader.
func New(opts Options) *Uploader {
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Uploader{
		workspaceRoot: opts.WorkspaceRoot,
		ttl:           ttl,
	}
}

// Register mounts upload endpoints on mux.
func (u *Uploader) Register(mux *http.ServeMux) {
	mux.HandleFunc(UploadAPIPath, u.handleUpload)
}

// StartCleaner runs a periodic background task to clean up files older than TTL.
func (u *Uploader) StartCleaner(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Hour
	}

	go func() {
		// Run once on startup
		u.cleanupOldFiles()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				u.cleanupOldFiles()
			}
		}
	}()
}

// Response returned after successful upload.
type Response struct {
	Success      bool   `json:"success"`
	FileName     string `json:"fileName"`
	RelativePath string `json:"relativePath"`
	FullPath     string `json:"fullPath"`
	Size         int64  `json:"size"`
	Error        string `json:"error,omitempty"`
}

func (u *Uploader) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, Response{Error: "Method not allowed"})
		return
	}

	mr, err := r.MultipartReader()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, Response{Error: fmt.Sprintf("invalid multipart request: %v", err)})
		return
	}

	var conversationID string
	var projectPath string
	var savedResponse *Response

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeJSON(w, http.StatusBadRequest, Response{Error: fmt.Sprintf("read part error: %v", err)})
			return
		}

		formName := part.FormName()
		if formName == "conversationId" {
			data, _ := io.ReadAll(io.LimitReader(part, 1024))
			conversationID = sanitizeID(string(data))
			continue
		}
		if formName == "projectPath" {
			data, _ := io.ReadAll(io.LimitReader(part, 4096))
			projectPath = strings.TrimSpace(string(data))
			continue
		}

		if formName == "file" || part.FileName() != "" {
			fileName := sanitizeFileName(part.FileName())
			if fileName == "" {
				fileName = fmt.Sprintf("upload_%d.bin", time.Now().Unix())
			}

			if conversationID == "" {
				conversationID = "shared"
			}

			targetDir, relativePrefix, err := u.resolveTargetDir(projectPath, conversationID)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, Response{Error: err.Error()})
				return
			}

			if err := os.MkdirAll(targetDir, 0755); err != nil {
				writeJSON(w, http.StatusInternalServerError, Response{Error: fmt.Sprintf("cannot create upload directory: %v", err)})
				return
			}

			outFile, finalName, destPath, err := createUniqueFile(targetDir, fileName)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, Response{Error: fmt.Sprintf("cannot create file: %v", err)})
				return
			}

			written, err := io.Copy(outFile, part)
			_ = outFile.Close()
			if err != nil {
				_ = os.Remove(destPath)
				writeJSON(w, http.StatusInternalServerError, Response{Error: fmt.Sprintf("write file error: %v", err)})
				return
			}

			relPath := filepath.Join(relativePrefix, finalName)
			savedResponse = &Response{
				Success:      true,
				FileName:     finalName,
				RelativePath: relPath,
				FullPath:     destPath,
				Size:         written,
			}
		}
	}

	if savedResponse == nil {
		writeJSON(w, http.StatusBadRequest, Response{Error: "no file was provided in the upload request"})
		return
	}

	writeJSON(w, http.StatusOK, *savedResponse)
}

func createUniqueFile(targetDir, fileName string) (*os.File, string, string, error) {
	ext := filepath.Ext(fileName)
	base := strings.TrimSuffix(fileName, ext)
	if base == "" {
		base = fileName
		ext = ""
	}

	for i := 1; i <= 1000; i++ {
		candidate := fileName
		if i > 1 {
			candidate = fmt.Sprintf("%s_%d%s", base, i, ext)
		}
		destPath := filepath.Join(targetDir, candidate)
		f, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err == nil {
			return f, candidate, destPath, nil
		}
		if !os.IsExist(err) {
			return nil, "", "", err
		}
	}

	f, err := os.CreateTemp(targetDir, base+"_*"+ext)
	if err != nil {
		return nil, "", "", err
	}
	destPath := f.Name()
	return f, filepath.Base(destPath), destPath, nil
}

func (u *Uploader) resolveTargetDir(projectPath, conversationID string) (targetDir string, relativePrefix string, err error) {
	wsRoot := u.workspaceRoot
	home, _ := os.UserHomeDir()
	if wsRoot == "" {
		wsRoot = filepath.Join(home, "antigravity", "workspace")
	}
	wsRoot = filepath.Clean(wsRoot)

	if projectPath != "" {
		cleanProj := filepath.Clean(projectPath)
		if !filepath.IsAbs(cleanProj) {
			cleanProj = filepath.Join(wsRoot, cleanProj)
		}
		// Security boundary check: ensure cleanProj does not escape wsRoot
		if rel, err := filepath.Rel(wsRoot, cleanProj); err == nil && !strings.HasPrefix(rel, "..") && rel != ".." {
			targetDir = filepath.Join(cleanProj, "uploads", conversationID)
			relativePrefix = filepath.Join("uploads", conversationID)
			return targetDir, relativePrefix, nil
		}
	}

	// Outside of project: store in workspace/uploads/{conversationID}
	targetDir = filepath.Join(wsRoot, "uploads", conversationID)

	// Calculate relative path from user's home directory so the agent can find it immediately from ~
	if home != "" && strings.HasPrefix(targetDir, home) {
		if rel, err := filepath.Rel(home, targetDir); err == nil {
			relativePrefix = rel
			return targetDir, relativePrefix, nil
		}
	}

	relativePrefix = filepath.Join("antigravity", "workspace", "uploads", conversationID)
	return targetDir, relativePrefix, nil
}

func (u *Uploader) cleanupOldFiles() {
	wsRoot := u.workspaceRoot
	if wsRoot == "" {
		home, _ := os.UserHomeDir()
		wsRoot = filepath.Join(home, "antigravity", "workspace")
	}

	if _, err := os.Stat(wsRoot); os.IsNotExist(err) {
		return
	}

	cutoff := time.Now().Add(-u.ttl)

	// 1. Clean root uploads folder: <workspaceRoot>/uploads
	rootUploads := filepath.Join(wsRoot, "uploads")
	cleanUploadDir(rootUploads, cutoff)

	// 2. Clean project-specific uploads folders: <workspaceRoot>/<project>/uploads
	entries, err := os.ReadDir(wsRoot)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				projUploads := filepath.Join(wsRoot, entry.Name(), "uploads")
				if info, err := os.Stat(projUploads); err == nil && info.IsDir() {
					cleanUploadDir(projUploads, cutoff)
				}
			}
		}
	}
}

func cleanUploadDir(dir string, cutoff time.Time) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return
	}

	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && info.ModTime().Before(cutoff) {
			if err := os.Remove(path); err == nil {
				log.Printf("[upload cleaner] deleted expired file: %s", path)
			}
		}
		return nil
	})

	removeEmptyDirs(dir)
}

func removeEmptyDirs(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			sub := filepath.Join(dir, entry.Name())
			removeEmptyDirs(sub)
			_ = os.Remove(sub)
		}
	}
}

func sanitizeID(raw string) string {
	raw = strings.TrimSpace(raw)
	var b strings.Builder
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	res := b.String()
	if len(res) > 64 {
		res = res[:64]
	}
	return res
}

func sanitizeFileName(name string) string {
	name = filepath.Base(name)
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	if name == "" || name == "." || name == ".." {
		return fmt.Sprintf("upload_%d.bin", time.Now().Unix())
	}
	return name
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
