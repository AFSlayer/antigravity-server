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
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultTTL is how long uploaded files are kept before automatic cleanup.
	DefaultTTL = 7 * 24 * time.Hour
	// StalePartTTL is how long incomplete chunked upload temp files are kept before cleanup.
	StalePartTTL = 2 * time.Hour
	// MaxChunks is the maximum number of chunks allowed per chunked upload.
	MaxChunks = 10000
	// UploadAPIPath is the endpoint for streaming file uploads.
	UploadAPIPath = "/__agy/api/upload"

	partFilePrefix = ".agy_part_"
)

// Options configures an Uploader.
type Options struct {
	WorkspaceRoot string
	TTL           time.Duration
}

type partLock struct {
	mu   sync.Mutex
	refs int
}

type chunkState struct {
	totalChunks int
	fileName    string
	nextChunk   int
	chunkStart  map[int]int64
	updatedAt   time.Time
}

// Uploader handles streaming file uploads to the workspace and periodic cleanup.
type Uploader struct {
	workspaceRoot string
	ttl           time.Duration

	mu          sync.Mutex
	partLocks   map[string]*partLock
	chunkStates map[string]*chunkState
	finalizeMu  sync.Mutex
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
		partLocks:     make(map[string]*partLock),
		chunkStates:   make(map[string]*chunkState),
	}
}

func (u *Uploader) acquirePartLock(partPath string) func() {
	u.mu.Lock()
	if u.partLocks == nil {
		u.partLocks = make(map[string]*partLock)
	}
	pl, ok := u.partLocks[partPath]
	if !ok {
		pl = &partLock{}
		u.partLocks[partPath] = pl
	}
	pl.refs++
	u.mu.Unlock()

	pl.mu.Lock()
	return func() {
		pl.mu.Unlock()
		u.mu.Lock()
		pl.refs--
		if pl.refs <= 0 {
			delete(u.partLocks, partPath)
		}
		u.mu.Unlock()
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
	if r.Method == http.MethodDelete {
		u.handleAbortUpload(w, r)
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, Response{Error: "Method not allowed"})
		return
	}

	mr, err := r.MultipartReader()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, Response{Error: fmt.Sprintf("invalid multipart request: %v", err)})
		return
	}

	q := r.URL.Query()
	conversationID := sanitizeID(q.Get("conversationId"))
	projectPath := strings.TrimSpace(q.Get("projectPath"))
	uploadID := sanitizeID(q.Get("uploadId"))
	rawChunkIndex := strings.TrimSpace(q.Get("chunkIndex"))
	rawTotalChunks := strings.TrimSpace(q.Get("totalChunks"))
	rawChunkOffset := strings.TrimSpace(q.Get("chunkOffset"))
	explicitFileName := strings.TrimSpace(q.Get("fileName"))

	var savedResponse *Response
	var rollback func()

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			if rollback != nil {
				rollback()
			}
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
		if formName == "uploadId" {
			data, _ := io.ReadAll(io.LimitReader(part, 1024))
			uploadID = sanitizeID(string(data))
			continue
		}
		if formName == "chunkIndex" {
			data, _ := io.ReadAll(io.LimitReader(part, 64))
			rawChunkIndex = strings.TrimSpace(string(data))
			continue
		}
		if formName == "totalChunks" {
			data, _ := io.ReadAll(io.LimitReader(part, 64))
			rawTotalChunks = strings.TrimSpace(string(data))
			continue
		}
		if formName == "chunkOffset" {
			data, _ := io.ReadAll(io.LimitReader(part, 64))
			rawChunkOffset = strings.TrimSpace(string(data))
			continue
		}
		if formName == "fileName" {
			data, _ := io.ReadAll(io.LimitReader(part, 1024))
			explicitFileName = strings.TrimSpace(string(data))
			continue
		}

		if formName == "file" || part.FileName() != "" {
			if savedResponse != nil {
				if rollback != nil {
					rollback()
				}
				writeJSON(w, http.StatusBadRequest, Response{Error: "multiple file parts are not supported in a single request"})
				return
			}

			rawName := explicitFileName
			if rawName == "" {
				rawName = part.FileName()
			}
			fileName := sanitizeFileName(rawName)
			if fileName == "" {
				fileName = fmt.Sprintf("upload_%d.bin", time.Now().Unix())
			}

			if conversationID == "" {
				conversationID = "shared"
			}

			chunkIndex := 0
			totalChunks := 1
			isChunked := rawChunkIndex != "" || rawTotalChunks != ""
			if isChunked {
				var parseErr error
				if rawTotalChunks != "" {
					totalChunks, parseErr = strconv.Atoi(rawTotalChunks)
					if parseErr != nil || totalChunks < 1 || totalChunks > MaxChunks {
						writeJSON(w, http.StatusBadRequest, Response{Error: "invalid totalChunks parameter"})
						return
					}
				}
				if totalChunks > 1 && rawChunkIndex == "" {
					writeJSON(w, http.StatusBadRequest, Response{Error: "missing chunkIndex parameter"})
					return
				}
				if rawChunkIndex != "" {
					chunkIndex, parseErr = strconv.Atoi(rawChunkIndex)
					if parseErr != nil || chunkIndex < 0 || chunkIndex >= totalChunks {
						writeJSON(w, http.StatusBadRequest, Response{Error: "invalid chunkIndex parameter"})
						return
					}
				}
			}

			var chunkOffset int64
			hasChunkOffset := rawChunkOffset != ""
			if hasChunkOffset {
				var parseErr error
				chunkOffset, parseErr = strconv.ParseInt(rawChunkOffset, 10, 64)
				if parseErr != nil || chunkOffset < 0 {
					writeJSON(w, http.StatusBadRequest, Response{Error: "invalid chunkOffset parameter"})
					return
				}
				if chunkIndex == 0 && chunkOffset != 0 {
					writeJSON(w, http.StatusBadRequest, Response{Error: "chunk 0 must start at offset 0"})
					return
				}
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

			if totalChunks <= 1 {
				u.finalizeMu.Lock()
				outFile, finalName, destPath, err := createUniqueFile(targetDir, fileName)
				u.finalizeMu.Unlock()
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
				rollback = func() {
					_ = os.Remove(destPath)
				}
				continue
			}

			if uploadID == "" {
				writeJSON(w, http.StatusBadRequest, Response{Error: "missing uploadId for chunked upload"})
				return
			}

			partPath := filepath.Join(targetDir, partFilePrefix+uploadID)
			resp, rb, status, err := u.writeChunk(partPath, targetDir, relativePrefix, fileName, chunkIndex, totalChunks, chunkOffset, hasChunkOffset, part)
			if err != nil {
				writeJSON(w, status, Response{Error: err.Error()})
				return
			}
			savedResponse = resp
			rollback = rb
		}
	}

	if savedResponse == nil {
		writeJSON(w, http.StatusBadRequest, Response{Error: "no file was provided in the upload request"})
		return
	}

	writeJSON(w, http.StatusOK, *savedResponse)
}

func (u *Uploader) handleAbortUpload(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	uploadID := sanitizeID(q.Get("uploadId"))
	if uploadID == "" {
		writeJSON(w, http.StatusBadRequest, Response{Error: "missing uploadId"})
		return
	}
	conversationID := sanitizeID(q.Get("conversationId"))
	if conversationID == "" {
		conversationID = "shared"
	}
	projectPath := strings.TrimSpace(q.Get("projectPath"))

	targetDir, _, err := u.resolveTargetDir(projectPath, conversationID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, Response{Error: err.Error()})
		return
	}

	partPath := filepath.Join(targetDir, partFilePrefix+uploadID)
	unlock := u.acquirePartLock(partPath)
	defer unlock()

	u.mu.Lock()
	delete(u.chunkStates, partPath)
	u.mu.Unlock()

	_ = os.Remove(partPath)
	writeJSON(w, http.StatusOK, Response{Success: true})
}

func (u *Uploader) writeChunk(partPath, targetDir, relativePrefix, fileName string, chunkIndex, totalChunks int, chunkOffset int64, hasChunkOffset bool, part io.Reader) (*Response, func(), int, error) {
	unlock := u.acquirePartLock(partPath)
	defer unlock()

	if chunkIndex == 0 {
		if hasChunkOffset && chunkOffset != 0 {
			return nil, nil, http.StatusBadRequest, fmt.Errorf("chunk 0 must start at offset 0")
		}
		outFile, err := os.OpenFile(partPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			return nil, nil, http.StatusInternalServerError, fmt.Errorf("cannot create staging file: %v", err)
		}
		written, err := io.Copy(outFile, part)
		_ = outFile.Close()
		if err != nil {
			_ = os.Remove(partPath)
			u.mu.Lock()
			delete(u.chunkStates, partPath)
			u.mu.Unlock()
			return nil, nil, http.StatusInternalServerError, fmt.Errorf("write chunk error: %v", err)
		}

		u.mu.Lock()
		if u.chunkStates == nil {
			u.chunkStates = make(map[string]*chunkState)
		}
		u.chunkStates[partPath] = &chunkState{
			totalChunks: totalChunks,
			fileName:    fileName,
			nextChunk:   1,
			chunkStart:  map[int]int64{0: 0, 1: written},
			updatedAt:   time.Now(),
		}
		u.mu.Unlock()

		rb := func() {
			unlockRb := u.acquirePartLock(partPath)
			defer unlockRb()
			_ = os.Remove(partPath)
			u.mu.Lock()
			delete(u.chunkStates, partPath)
			u.mu.Unlock()
		}
		return &Response{
			Success:  true,
			FileName: fileName,
			Size:     written,
		}, rb, http.StatusOK, nil
	}

	info, err := os.Stat(partPath)
	if err != nil {
		return nil, nil, http.StatusBadRequest, fmt.Errorf("upload session not found or missing previous chunks")
	}
	curSize := info.Size()

	u.mu.Lock()
	state := u.chunkStates[partPath]
	u.mu.Unlock()

	startOffset := curSize
	if state != nil {
		if state.totalChunks > 0 && state.totalChunks != totalChunks {
			return nil, nil, http.StatusBadRequest, fmt.Errorf("totalChunks mismatch: got %d, expected %d", totalChunks, state.totalChunks)
		}
		if state.fileName != "" {
			fileName = state.fileName
		}
		if chunkIndex > state.nextChunk {
			return nil, nil, http.StatusBadRequest, fmt.Errorf("out of order chunk: got %d, expected <= %d", chunkIndex, state.nextChunk)
		}
		expectedOffset, ok := state.chunkStart[chunkIndex]
		if !ok {
			return nil, nil, http.StatusBadRequest, fmt.Errorf("missing offset state for chunk %d", chunkIndex)
		}
		if hasChunkOffset && chunkOffset != expectedOffset {
			return nil, nil, http.StatusBadRequest, fmt.Errorf("chunk offset mismatch: got %d, expected %d", chunkOffset, expectedOffset)
		}
		startOffset = expectedOffset
	} else if hasChunkOffset {
		if chunkOffset > curSize {
			return nil, nil, http.StatusBadRequest, fmt.Errorf("chunk offset mismatch: got %d, current size %d", chunkOffset, curSize)
		}
		startOffset = chunkOffset
	}

	outFile, err := os.OpenFile(partPath, os.O_WRONLY, 0644)
	if err != nil {
		return nil, nil, http.StatusInternalServerError, fmt.Errorf("cannot open staging file: %v", err)
	}
	if startOffset < curSize {
		if err := outFile.Truncate(startOffset); err != nil {
			_ = outFile.Close()
			return nil, nil, http.StatusInternalServerError, fmt.Errorf("truncate staging file error: %v", err)
		}
	}
	if _, err := outFile.Seek(startOffset, io.SeekStart); err != nil {
		_ = outFile.Close()
		return nil, nil, http.StatusInternalServerError, fmt.Errorf("seek staging file error: %v", err)
	}

	written, err := io.Copy(outFile, part)
	_ = outFile.Close()
	if err != nil {
		_ = os.Truncate(partPath, startOffset)
		return nil, nil, http.StatusInternalServerError, fmt.Errorf("write chunk error: %v", err)
	}

	totalWritten := startOffset + written

	if chunkIndex < totalChunks-1 {
		u.mu.Lock()
		if state == nil {
			state = &chunkState{
				totalChunks: totalChunks,
				fileName:    fileName,
				chunkStart:  map[int]int64{chunkIndex: startOffset},
			}
			if u.chunkStates == nil {
				u.chunkStates = make(map[string]*chunkState)
			}
			u.chunkStates[partPath] = state
		}
		state.nextChunk = chunkIndex + 1
		state.chunkStart[chunkIndex+1] = totalWritten
		state.updatedAt = time.Now()
		for k := range state.chunkStart {
			if k > chunkIndex+1 {
				delete(state.chunkStart, k)
			}
		}
		u.mu.Unlock()

		rb := func() {
			unlockRb := u.acquirePartLock(partPath)
			defer unlockRb()
			_ = os.Truncate(partPath, startOffset)
			u.mu.Lock()
			if st := u.chunkStates[partPath]; st != nil {
				st.nextChunk = chunkIndex
				delete(st.chunkStart, chunkIndex+1)
			}
			u.mu.Unlock()
		}
		return &Response{
			Success:  true,
			FileName: fileName,
			Size:     totalWritten,
		}, rb, http.StatusOK, nil
	}

	// Final chunk: atomically move staging file into its unique destination name
	u.mu.Lock()
	delete(u.chunkStates, partPath)
	u.mu.Unlock()

	finalName, destPath, err := u.finalizeChunkedFile(targetDir, partPath, fileName)
	if err != nil {
		_ = os.Remove(partPath)
		return nil, nil, http.StatusInternalServerError, fmt.Errorf("cannot finalize file: %v", err)
	}

	relPath := filepath.Join(relativePrefix, finalName)
	rb := func() {
		_ = os.Remove(destPath)
	}
	return &Response{
		Success:      true,
		FileName:     finalName,
		RelativePath: relPath,
		FullPath:     destPath,
		Size:         totalWritten,
	}, rb, http.StatusOK, nil
}

func (u *Uploader) finalizeChunkedFile(targetDir, partPath, fileName string) (string, string, error) {
	u.finalizeMu.Lock()
	defer u.finalizeMu.Unlock()

	f, finalName, destPath, err := createUniqueFile(targetDir, fileName)
	if err != nil {
		return "", "", err
	}
	_ = f.Close()

	if err := os.Rename(partPath, destPath); err != nil {
		_ = os.Remove(destPath)
		return "", "", err
	}
	return finalName, destPath, nil
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
	partCutoff := time.Now().Add(-StalePartTTL)
	if cutoff.After(partCutoff) {
		partCutoff = cutoff
	}

	// 1. Clean root uploads folder: <workspaceRoot>/uploads
	rootUploads := filepath.Join(wsRoot, "uploads")
	u.cleanUploadDir(rootUploads, cutoff, partCutoff)

	// 2. Clean project-specific uploads folders: <workspaceRoot>/<project>/uploads
	entries, err := os.ReadDir(wsRoot)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				projUploads := filepath.Join(wsRoot, entry.Name(), "uploads")
				if info, err := os.Stat(projUploads); err == nil && info.IsDir() {
					u.cleanUploadDir(projUploads, cutoff, partCutoff)
				}
			}
		}
	}

	// 3. Evict any stale in-memory chunk session metadata
	u.mu.Lock()
	for k, st := range u.chunkStates {
		if st.updatedAt.Before(partCutoff) {
			delete(u.chunkStates, k)
		}
	}
	u.mu.Unlock()
}

func (u *Uploader) cleanUploadDir(dir string, cutoff, partCutoff time.Time) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return
	}

	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		isPart := strings.HasPrefix(info.Name(), partFilePrefix)
		targetCutoff := cutoff
		if isPart {
			targetCutoff = partCutoff
		}
		if info.ModTime().Before(targetCutoff) {
			if isPart {
				unlock := u.acquirePartLock(path)
				if latestInfo, statErr := os.Stat(path); statErr == nil && latestInfo.ModTime().Before(targetCutoff) {
					if rmErr := os.Remove(path); rmErr == nil {
						u.mu.Lock()
						delete(u.chunkStates, path)
						u.mu.Unlock()
						log.Printf("[upload cleaner] deleted expired file: %s", path)
					}
				}
				unlock()
				return nil
			}
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
	for strings.HasPrefix(name, partFilePrefix) {
		name = strings.TrimPrefix(name, ".")
	}
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
