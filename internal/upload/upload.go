package upload

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AFSlayer/antigravity-server/internal/auth"
	"github.com/AFSlayer/antigravity-server/internal/config"
)

const (
	// DefaultTTL is how long uploaded files are kept before automatic cleanup.
	DefaultTTL = 7 * 24 * time.Hour
	// StalePartTTL is how long incomplete chunked upload temp files are kept before cleanup.
	StalePartTTL = 2 * time.Hour
	// MaxChunks is the maximum number of chunks allowed per chunked upload.
	MaxChunks = 10000
	// MaxSingleUploadBytes caps a single non-chunked upload stream.
	MaxSingleUploadBytes int64 = 20 << 30
	// MaxChunkBytes caps an individual chunk upload body.
	MaxChunkBytes int64 = 256 << 20
	// UploadDirName is the dedicated hidden directory managed by the uploader.
	UploadDirName = ".agy-uploads"
	// UploadAPIPath is the endpoint for streaming file uploads.
	UploadAPIPath = "/__agy/api/upload"

	partFilePrefix                         = ".agy_part_"
	stageFilePrefix                        = ".agy_stage_"
	uploadedFilePerm           os.FileMode = 0o600
	multipartOverheadAllowance             = 4 << 20
	maxWorkspaceScanDepth                  = 5
	maxWorkspaceScanDirs                   = 4096
	maxKnownUploadRoots                    = 2048
	completedSessionTTL                    = 5 * time.Minute
	maxChunkStates                         = 2048
)

var (
	errUploadStreamTooLarge = errors.New("upload stream exceeds maximum allowed size")
	errUploadTotalTooLarge  = errors.New("total upload size exceeds maximum allowed size")
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
	totalChunks   int
	fileName      string
	nextChunk     int
	chunkStart    map[int]int64
	completedResp *Response
	updatedAt     time.Time
}

// Uploader handles streaming file uploads to the workspace and periodic cleanup.
type Uploader struct {
	workspaceRoot       string
	ttl                 time.Duration
	maxChunkBytes       int64
	maxTotalBytes       int64
	maxChunkStatesLimit int
	trusted             []*net.IPNet
	publicURL           string

	mu               sync.Mutex
	partLocks        map[string]*partLock
	chunkStates      map[string]*chunkState
	knownUploadRoots map[string]struct{}
	finalizeMu       sync.Mutex
}

// New creates a new Uploader.
func New(opts Options) *Uploader {
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Uploader{
		workspaceRoot:       opts.WorkspaceRoot,
		ttl:                 ttl,
		maxChunkBytes:       MaxChunkBytes,
		maxTotalBytes:       MaxSingleUploadBytes,
		maxChunkStatesLimit: maxChunkStates,
		partLocks:           make(map[string]*partLock),
		chunkStates:         make(map[string]*chunkState),
		knownUploadRoots:    make(map[string]struct{}),
	}
}

func (u *Uploader) effectiveMaxChunkBytes() int64 {
	if u.maxChunkBytes > 0 {
		return u.maxChunkBytes
	}
	return MaxChunkBytes
}

func (u *Uploader) effectiveMaxTotalBytes() int64 {
	if u.maxTotalBytes > 0 {
		return u.maxTotalBytes
	}
	return MaxSingleUploadBytes
}

func (u *Uploader) effectiveMaxChunkStates() int {
	if u.maxChunkStatesLimit > 0 {
		return u.maxChunkStatesLimit
	}
	return maxChunkStates
}

func (u *Uploader) rememberUploadRoot(dir string) {
	if dir == "" {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.knownUploadRoots == nil {
		u.knownUploadRoots = make(map[string]struct{})
	}
	if _, exists := u.knownUploadRoots[dir]; exists {
		return
	}
	if len(u.knownUploadRoots) >= maxKnownUploadRoots {
		return
	}
	u.knownUploadRoots[dir] = struct{}{}
}

type maxBytesStreamReader struct {
	r         io.Reader
	remaining int64
	exceedErr error
}

func (m *maxBytesStreamReader) Read(p []byte) (int, error) {
	if m.remaining <= 0 {
		var probe [1]byte
		n, err := m.r.Read(probe[:])
		if n > 0 {
			if m.exceedErr != nil {
				return 0, m.exceedErr
			}
			return 0, errUploadStreamTooLarge
		}
		return 0, err
	}
	if int64(len(p)) > m.remaining {
		p = p[:m.remaining]
	}
	n, err := m.r.Read(p)
	m.remaining -= int64(n)
	return n, err
}

func parseChunkParams(rawChunkIndex, rawTotalChunks, rawChunkOffset string) (chunkIndex int, totalChunks int, chunkOffset int64, hasChunkOffset bool, isChunked bool, err error) {
	totalChunks = 1
	isChunked = rawChunkIndex != "" || rawTotalChunks != ""
	if isChunked {
		if rawTotalChunks != "" {
			totalChunks, err = strconv.Atoi(rawTotalChunks)
			if err != nil || totalChunks < 1 || totalChunks > MaxChunks {
				return 0, 0, 0, false, false, fmt.Errorf("invalid totalChunks parameter")
			}
		}
		if totalChunks > 1 && rawChunkIndex == "" {
			return 0, 0, 0, false, false, fmt.Errorf("missing chunkIndex parameter")
		}
		if rawChunkIndex != "" {
			chunkIndex, err = strconv.Atoi(rawChunkIndex)
			if err != nil || chunkIndex < 0 || chunkIndex >= totalChunks {
				return 0, 0, 0, false, false, fmt.Errorf("invalid chunkIndex parameter")
			}
		}
	}
	if rawChunkOffset != "" {
		hasChunkOffset = true
		chunkOffset, err = strconv.ParseInt(rawChunkOffset, 10, 64)
		if err != nil || chunkOffset < 0 {
			return 0, 0, 0, false, false, fmt.Errorf("invalid chunkOffset parameter")
		}
		if chunkIndex == 0 && chunkOffset != 0 {
			return 0, 0, 0, false, false, fmt.Errorf("chunk 0 must start at offset 0")
		}
	}
	return chunkIndex, totalChunks, chunkOffset, hasChunkOffset, isChunked, nil
}

func canonicalPartKey(partPath string) string {
	if partPath == "" {
		return ""
	}
	cleaned := filepath.Clean(partPath)
	dir := filepath.Dir(cleaned)
	base := filepath.Base(cleaned)
	if resolvedDir, err := evalExistingPath(dir); err == nil && resolvedDir != "" {
		return filepath.Join(resolvedDir, base)
	}
	return cleaned
}

func rejectNonRegularPartFile(path string) error {
	linfo, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !linfo.Mode().IsRegular() {
		return fmt.Errorf("staging part path is not a regular file")
	}
	return nil
}

func removeRegularPartFile(path string) {
	if path == "" {
		return
	}
	if linfo, err := os.Lstat(path); err == nil && linfo.Mode().IsRegular() {
		_ = os.Remove(path)
	}
}

func truncateRegularPartFile(path string, size int64) {
	if path == "" {
		return
	}
	if linfo, err := os.Lstat(path); err == nil && linfo.Mode().IsRegular() {
		_ = os.Truncate(path, size)
	}
}

func (u *Uploader) acquirePartLock(partPath string) func() {
	partPath = canonicalPartKey(partPath)
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

// WithSameOrigin configures reverse-proxy origin verification to match Authenticator.
func (u *Uploader) WithSameOrigin(trusted []*net.IPNet, publicURL string) *Uploader {
	u.trusted = trusted
	u.publicURL = publicURL
	return u
}

// Register mounts upload endpoints on mux.
func (u *Uploader) Register(mux *http.ServeMux) {
	mux.HandleFunc(UploadAPIPath, u.handleUpload)
}

// StartCleaner runs a periodic background task to clean up files older than TTL.
func (u *Uploader) StartCleaner(ctx context.Context, interval time.Duration) {
	go u.RunCleaner(ctx, interval)
}

// RunCleaner cleans up files older than TTL immediately and on every interval until ctx ends.
func (u *Uploader) RunCleaner(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Hour
	}

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
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		writeJSON(w, http.StatusMethodNotAllowed, Response{Error: "Method not allowed"})
		return
	}
	if ok, _ := auth.CheckSameOrigin(r, u.trusted, u.publicURL); !ok {
		writeJSON(w, http.StatusForbidden, Response{Error: "cross-origin request forbidden"})
		return
	}
	if r.Method == http.MethodDelete {
		u.handleAbortUpload(w, r)
		return
	}

	maxChunk := u.effectiveMaxChunkBytes()
	maxTotal := u.effectiveMaxTotalBytes()
	if r.ContentLength > maxTotal+multipartOverheadAllowance {
		writeJSON(w, http.StatusRequestEntityTooLarge, Response{Error: "uploaded file exceeds maximum allowed size"})
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

	if rawTotalChunks != "" {
		if tc, err := strconv.Atoi(rawTotalChunks); err == nil && tc > 1 && r.ContentLength > maxChunk+multipartOverheadAllowance {
			writeJSON(w, http.StatusRequestEntityTooLarge, Response{Error: "chunk exceeds maximum allowed chunk size"})
			return
		}
	}

	mr, err := r.MultipartReader()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, Response{Error: fmt.Sprintf("invalid multipart request: %v", err)})
		return
	}

	var stagedPath string
	var stagedSize int64
	var partFileName string
	var directResp *Response
	var directFinalize func() (*Response, int, error)
	var directRollback func()
	defer func() {
		if stagedPath != "" {
			_ = os.Remove(stagedPath)
		}
	}()

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			if directRollback != nil {
				directRollback()
			}
			writeJSON(w, http.StatusBadRequest, Response{Error: fmt.Sprintf("read part error: %v", err)})
			return
		}

		formName := part.FormName()
		if formName == "conversationId" || formName == "projectPath" || formName == "uploadId" ||
			formName == "chunkIndex" || formName == "totalChunks" || formName == "chunkOffset" || formName == "fileName" {
			isChunkField := formName == "uploadId" || formName == "chunkIndex" || formName == "totalChunks" || formName == "chunkOffset"
			if directResp != nil || directFinalize != nil || (stagedPath != "" && isChunkField) {
				if directRollback != nil {
					directRollback()
				}
				writeJSON(w, http.StatusBadRequest, Response{Error: "chunk upload metadata fields must precede the file part"})
				return
			}
		}
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
			if stagedPath != "" || directResp != nil || directFinalize != nil {
				if directRollback != nil {
					directRollback()
				}
				writeJSON(w, http.StatusBadRequest, Response{Error: "multiple file parts are not supported in a single request"})
				return
			}
			partFileName = part.FileName()

			// When chunk metadata precedes the file part (standard client order), stream
			// directly into the target part file with a strict MaxChunkBytes cap so we
			// never double-write chunks through a temporary staging file or accept >256 MiB.
			if rawChunkIndex != "" || rawTotalChunks != "" {
				chunkIndex, totalChunks, chunkOffset, hasChunkOffset, _, parseErr := parseChunkParams(rawChunkIndex, rawTotalChunks, rawChunkOffset)
				if parseErr != nil {
					writeJSON(w, http.StatusBadRequest, Response{Error: parseErr.Error()})
					return
				}
				if totalChunks > 1 {
					if uploadID == "" {
						writeJSON(w, http.StatusBadRequest, Response{Error: "missing uploadId for chunked upload"})
						return
					}
					rawName := explicitFileName
					if rawName == "" {
						rawName = partFileName
					}
					fileName := sanitizeFileName(rawName)
					if fileName == "" {
						fileName = fmt.Sprintf("upload_%d.bin", time.Now().Unix())
					}
					convo := conversationID
					if convo == "" {
						convo = "shared"
					}
					targetDir, relativePrefix, dirErr := u.resolveTargetDir(projectPath, convo)
					if dirErr != nil {
						writeJSON(w, http.StatusBadRequest, Response{Error: dirErr.Error()})
						return
					}
					partPath := filepath.Join(targetDir, partFilePrefix+uploadID)
					cappedPart := &maxBytesStreamReader{r: part, remaining: maxChunk}
					resp, finFn, rb, status, writeErr := u.writeChunk(partPath, targetDir, relativePrefix, fileName, chunkIndex, totalChunks, chunkOffset, hasChunkOffset, cappedPart)
					if writeErr != nil {
						writeJSON(w, status, Response{Error: writeErr.Error()})
						return
					}
					directResp = resp
					directFinalize = finFn
					directRollback = rb
					continue
				}
			}

			stageLimit := maxTotal
			if rawTotalChunks != "" {
				if tc, convErr := strconv.Atoi(rawTotalChunks); convErr == nil && tc > 1 {
					stageLimit = maxChunk
				}
			}

			stageDir, err := u.ensureRootUploadDir()
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, Response{Error: fmt.Sprintf("cannot prepare staging directory: %v", err)})
				return
			}
			tmpFile, err := os.CreateTemp(stageDir, stageFilePrefix+"*")
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, Response{Error: fmt.Sprintf("cannot create staging file: %v", err)})
				return
			}
			stagedPath = tmpFile.Name()

			written, copyErr := io.Copy(tmpFile, &maxBytesStreamReader{r: part, remaining: stageLimit})
			closeErr := tmpFile.Close()
			if errors.Is(copyErr, errUploadStreamTooLarge) {
				msg := "uploaded file exceeds maximum allowed size"
				if stageLimit == maxChunk {
					msg = "chunk exceeds maximum allowed chunk size"
				}
				writeJSON(w, http.StatusRequestEntityTooLarge, Response{Error: msg})
				return
			}
			if copyErr != nil {
				if errors.Is(copyErr, io.ErrUnexpectedEOF) {
					writeJSON(w, http.StatusBadRequest, Response{Error: "incomplete upload stream"})
					return
				}
				writeJSON(w, http.StatusInternalServerError, Response{Error: fmt.Sprintf("write staging file error: %v", copyErr)})
				return
			}
			if closeErr != nil {
				writeJSON(w, http.StatusInternalServerError, Response{Error: fmt.Sprintf("close staging file error: %v", closeErr)})
				return
			}
			stagedSize = written
		}
	}

	if directFinalize != nil {
		finalResp, finStatus, finErr := directFinalize()
		if finErr != nil {
			writeJSON(w, finStatus, Response{Error: finErr.Error()})
			return
		}
		writeJSON(w, http.StatusOK, *finalResp)
		return
	}

	if directResp != nil {
		writeJSON(w, http.StatusOK, *directResp)
		return
	}

	if stagedPath == "" {
		writeJSON(w, http.StatusBadRequest, Response{Error: "no file was provided in the upload request"})
		return
	}

	rawName := explicitFileName
	if rawName == "" {
		rawName = partFileName
	}
	fileName := sanitizeFileName(rawName)
	if fileName == "" {
		fileName = fmt.Sprintf("upload_%d.bin", time.Now().Unix())
	}

	if conversationID == "" {
		conversationID = "shared"
	}

	_, totalChunks, _, _, _, err := parseChunkParams(rawChunkIndex, rawTotalChunks, rawChunkOffset)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, Response{Error: err.Error()})
		return
	}

	targetDir, relativePrefix, err := u.resolveTargetDir(projectPath, conversationID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, Response{Error: err.Error()})
		return
	}

	if totalChunks <= 1 {
		u.finalizeMu.Lock()
		outFile, finalName, destPath, err := createUniqueFile(targetDir, fileName)
		if err == nil {
			_ = outFile.Close()
			err = moveOrCopyFile(stagedPath, destPath)
			if err != nil {
				_ = os.Remove(destPath)
			}
		}
		u.finalizeMu.Unlock()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, Response{Error: fmt.Sprintf("cannot finalize file: %v", err)})
			return
		}
		stagedPath = ""

		relPath := filepath.ToSlash(filepath.Join(relativePrefix, finalName))
		writeJSON(w, http.StatusOK, Response{
			Success:      true,
			FileName:     finalName,
			RelativePath: relPath,
			FullPath:     destPath,
			Size:         stagedSize,
		})
		return
	}

	writeJSON(w, http.StatusBadRequest, Response{Error: "chunk upload metadata fields must precede the file part"})
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

	rawPartPath := filepath.Join(targetDir, partFilePrefix+uploadID)
	partKey := canonicalPartKey(rawPartPath)
	unlock := u.acquirePartLock(partKey)
	defer unlock()

	if err := rejectNonRegularPartFile(rawPartPath); err != nil {
		writeJSON(w, http.StatusBadRequest, Response{Error: err.Error()})
		return
	}

	u.mu.Lock()
	delete(u.chunkStates, partKey)
	u.mu.Unlock()

	removeRegularPartFile(rawPartPath)
	writeJSON(w, http.StatusOK, Response{Success: true})
}

func (u *Uploader) pruneChunkStatesLocked(now time.Time, excludeKey string) []string {
	if len(u.chunkStates) == 0 {
		return nil
	}
	var evictedIncomplete []string
	for k, st := range u.chunkStates {
		if k == excludeKey {
			continue
		}
		if st == nil {
			delete(u.chunkStates, k)
			continue
		}
		if st.completedResp != nil && now.Sub(st.updatedAt) > completedSessionTTL {
			delete(u.chunkStates, k)
			continue
		}
		if now.Sub(st.updatedAt) > StalePartTTL {
			if pl := u.partLocks[k]; pl != nil && pl.refs > 0 {
				continue
			}
			if st.completedResp == nil {
				evictedIncomplete = append(evictedIncomplete, k)
			}
			delete(u.chunkStates, k)
		}
	}
	limit := u.effectiveMaxChunkStates()
	for len(u.chunkStates) >= limit {
		var victimKey string
		var victimTime time.Time
		var victimCompleted bool
		for k, st := range u.chunkStates {
			if k == excludeKey {
				continue
			}
			isDone := st.completedResp != nil
			if !isDone {
				if pl := u.partLocks[k]; pl != nil && pl.refs > 0 {
					continue
				}
			}
			if victimKey == "" ||
				(isDone && !victimCompleted) ||
				(isDone == victimCompleted && st.updatedAt.Before(victimTime)) {
				victimKey = k
				victimTime = st.updatedAt
				victimCompleted = isDone
			}
		}
		if victimKey == "" {
			break
		}
		if !victimCompleted {
			evictedIncomplete = append(evictedIncomplete, victimKey)
		}
		delete(u.chunkStates, victimKey)
	}
	return evictedIncomplete
}

func (u *Uploader) removeEvictedPartFiles(paths []string, excludePath string) {
	for _, p := range paths {
		if p == "" || p == excludePath {
			continue
		}
		unlock := u.acquirePartLock(p)
		u.mu.Lock()
		_, activeAgain := u.chunkStates[p]
		u.mu.Unlock()
		if !activeAgain {
			removeRegularPartFile(p)
		}
		unlock()
	}
}

func (u *Uploader) writeChunk(partPath, targetDir, relativePrefix, fileName string, chunkIndex, totalChunks int, chunkOffset int64, hasChunkOffset bool, part io.Reader) (*Response, func() (*Response, int, error), func(), int, error) {
	partKey := canonicalPartKey(partPath)
	partPath = partKey
	var evictedParts []string
	defer func() {
		if len(evictedParts) > 0 {
			u.removeEvictedPartFiles(evictedParts, partKey)
		}
	}()

	unlock := u.acquirePartLock(partKey)
	defer unlock()

	maxTotal := u.effectiveMaxTotalBytes()

	u.mu.Lock()
	state := u.chunkStates[partKey]
	u.mu.Unlock()

	if chunkIndex == 0 {
		if hasChunkOffset && chunkOffset != 0 {
			return nil, nil, nil, http.StatusBadRequest, fmt.Errorf("chunk 0 must start at offset 0")
		}
		u.finalizeMu.Lock()
		dirErr := ensureTargetUploadDir(targetDir)
		var outFile *os.File
		var openErr error
		var linkErr error
		if dirErr == nil {
			linkErr = rejectNonRegularPartFile(partPath)
			if linkErr == nil {
				removeRegularPartFile(partPath)
				outFile, openErr = os.OpenFile(partPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, uploadedFilePerm)
			}
		}
		u.finalizeMu.Unlock()
		if dirErr != nil {
			return nil, nil, nil, http.StatusInternalServerError, fmt.Errorf("cannot create upload directory: %v", dirErr)
		}
		if linkErr != nil {
			return nil, nil, nil, http.StatusBadRequest, linkErr
		}
		if openErr != nil {
			return nil, nil, nil, http.StatusInternalServerError, fmt.Errorf("cannot create staging file: %v", openErr)
		}
		totalCapReader := &maxBytesStreamReader{r: part, remaining: maxTotal, exceedErr: errUploadTotalTooLarge}
		written, err := io.Copy(outFile, totalCapReader)
		_ = outFile.Close()
		if err != nil {
			removeRegularPartFile(partPath)
			u.mu.Lock()
			delete(u.chunkStates, partKey)
			u.mu.Unlock()
			if errors.Is(err, errUploadTotalTooLarge) {
				return nil, nil, nil, http.StatusRequestEntityTooLarge, fmt.Errorf("uploaded file exceeds maximum allowed size")
			}
			if errors.Is(err, errUploadStreamTooLarge) {
				return nil, nil, nil, http.StatusRequestEntityTooLarge, fmt.Errorf("chunk exceeds maximum allowed chunk size")
			}
			if errors.Is(err, io.ErrUnexpectedEOF) {
				return nil, nil, nil, http.StatusBadRequest, fmt.Errorf("incomplete chunk upload stream")
			}
			return nil, nil, nil, http.StatusInternalServerError, fmt.Errorf("write chunk error: %v", err)
		}

		now := time.Now()
		u.mu.Lock()
		if u.chunkStates == nil {
			u.chunkStates = make(map[string]*chunkState)
		}
		evictedParts = u.pruneChunkStatesLocked(now, partKey)
		u.chunkStates[partKey] = &chunkState{
			totalChunks: totalChunks,
			fileName:    fileName,
			nextChunk:   1,
			chunkStart:  map[int]int64{0: 0, 1: written},
			updatedAt:   now,
		}
		u.mu.Unlock()

		rb := func() {
			unlockRb := u.acquirePartLock(partKey)
			defer unlockRb()
			removeRegularPartFile(partPath)
			u.mu.Lock()
			delete(u.chunkStates, partKey)
			u.mu.Unlock()
		}
		return &Response{
			Success:  true,
			FileName: fileName,
			Size:     written,
		}, nil, rb, http.StatusOK, nil
	}

	// Idempotent retry for final chunk if finalization already completed but response was lost in transit
	if state != nil && state.completedResp != nil && chunkIndex == totalChunks-1 && (state.totalChunks == 0 || state.totalChunks == totalChunks) {
		if _, err := io.Copy(io.Discard, part); errors.Is(err, errUploadStreamTooLarge) {
			return nil, nil, nil, http.StatusRequestEntityTooLarge, fmt.Errorf("chunk exceeds maximum allowed chunk size")
		}
		respCopy := *state.completedResp
		return &respCopy, nil, nil, http.StatusOK, nil
	}

	if err := rejectNonRegularPartFile(partPath); err != nil {
		return nil, nil, nil, http.StatusBadRequest, err
	}

	if state == nil {
		removeRegularPartFile(partPath)
		return nil, nil, nil, http.StatusBadRequest, fmt.Errorf("upload session expired or not found; please restart upload from chunk 0")
	}

	info, err := os.Lstat(partPath)
	if err != nil {
		u.mu.Lock()
		delete(u.chunkStates, partKey)
		u.mu.Unlock()
		return nil, nil, nil, http.StatusBadRequest, fmt.Errorf("upload session not found or missing previous chunks")
	}
	if !info.Mode().IsRegular() {
		return nil, nil, nil, http.StatusBadRequest, fmt.Errorf("staging part path is not a regular file")
	}
	curSize := info.Size()

	if state.totalChunks > 0 && state.totalChunks != totalChunks {
		return nil, nil, nil, http.StatusBadRequest, fmt.Errorf("totalChunks mismatch: got %d, expected %d", totalChunks, state.totalChunks)
	}
	if state.fileName != "" {
		fileName = state.fileName
	}
	if chunkIndex > state.nextChunk {
		return nil, nil, nil, http.StatusBadRequest, fmt.Errorf("out of order chunk: got %d, expected <= %d", chunkIndex, state.nextChunk)
	}
	expectedOffset, ok := state.chunkStart[chunkIndex]
	if !ok {
		return nil, nil, nil, http.StatusBadRequest, fmt.Errorf("missing offset state for chunk %d", chunkIndex)
	}
	if hasChunkOffset && chunkOffset != expectedOffset {
		return nil, nil, nil, http.StatusBadRequest, fmt.Errorf("chunk offset mismatch: got %d, expected %d", chunkOffset, expectedOffset)
	}
	if expectedOffset > curSize {
		u.mu.Lock()
		delete(u.chunkStates, partKey)
		u.mu.Unlock()
		removeRegularPartFile(partPath)
		return nil, nil, nil, http.StatusBadRequest, fmt.Errorf("upload staging file corrupted; please restart upload from chunk 0")
	}
	startOffset := expectedOffset
	if startOffset > maxTotal {
		return nil, nil, nil, http.StatusRequestEntityTooLarge, fmt.Errorf("uploaded file exceeds maximum allowed size")
	}

	rollbackStateToChunk := func() {
		u.mu.Lock()
		if state != nil {
			state.nextChunk = chunkIndex
			for k := range state.chunkStart {
				if k > chunkIndex {
					delete(state.chunkStart, k)
				}
			}
			state.updatedAt = time.Now()
		}
		u.mu.Unlock()
	}

	outFile, err := os.OpenFile(partPath, os.O_WRONLY, uploadedFilePerm)
	if err != nil {
		return nil, nil, nil, http.StatusInternalServerError, fmt.Errorf("cannot open staging file: %v", err)
	}
	if finfo, fstatErr := outFile.Stat(); fstatErr != nil || !finfo.Mode().IsRegular() || !os.SameFile(info, finfo) {
		_ = outFile.Close()
		return nil, nil, nil, http.StatusBadRequest, fmt.Errorf("staging part path is not a regular file")
	}
	if startOffset < curSize {
		if err := outFile.Truncate(startOffset); err != nil {
			_ = outFile.Close()
			return nil, nil, nil, http.StatusInternalServerError, fmt.Errorf("truncate staging file error: %v", err)
		}
	}
	if _, err := outFile.Seek(startOffset, io.SeekStart); err != nil {
		_ = outFile.Close()
		rollbackStateToChunk()
		return nil, nil, nil, http.StatusInternalServerError, fmt.Errorf("seek staging file error: %v", err)
	}

	totalCapReader := &maxBytesStreamReader{
		r:         part,
		remaining: maxTotal - startOffset,
		exceedErr: errUploadTotalTooLarge,
	}
	written, err := io.Copy(outFile, totalCapReader)
	_ = outFile.Close()
	if err != nil {
		truncateRegularPartFile(partPath, startOffset)
		rollbackStateToChunk()
		if errors.Is(err, errUploadTotalTooLarge) {
			return nil, nil, nil, http.StatusRequestEntityTooLarge, fmt.Errorf("uploaded file exceeds maximum allowed size")
		}
		if errors.Is(err, errUploadStreamTooLarge) {
			return nil, nil, nil, http.StatusRequestEntityTooLarge, fmt.Errorf("chunk exceeds maximum allowed chunk size")
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, nil, nil, http.StatusBadRequest, fmt.Errorf("incomplete chunk upload stream")
		}
		return nil, nil, nil, http.StatusInternalServerError, fmt.Errorf("write chunk error: %v", err)
	}

	totalWritten := startOffset + written
	now := time.Now()

	u.mu.Lock()
	if u.chunkStates == nil {
		u.chunkStates = make(map[string]*chunkState)
	}
	evictedParts = u.pruneChunkStatesLocked(now, partKey)
	u.chunkStates[partKey] = state
	state.nextChunk = chunkIndex + 1
	state.chunkStart[chunkIndex+1] = totalWritten
	state.updatedAt = now
	for k := range state.chunkStart {
		if k > chunkIndex+1 {
			delete(state.chunkStart, k)
		}
	}
	u.mu.Unlock()

	rb := func() {
		unlockRb := u.acquirePartLock(partKey)
		defer unlockRb()
		truncateRegularPartFile(partPath, startOffset)
		u.mu.Lock()
		if st := u.chunkStates[partKey]; st != nil {
			st.nextChunk = chunkIndex
			delete(st.chunkStart, chunkIndex+1)
		}
		u.mu.Unlock()
	}

	if chunkIndex < totalChunks-1 {
		return &Response{
			Success:  true,
			FileName: fileName,
			Size:     totalWritten,
		}, nil, rb, http.StatusOK, nil
	}

	// Final chunk: defer moving partPath to destPath until multipart stream EOF so
	// trailing multipart boundary/field errors roll back only this chunk without
	// destroying previously uploaded chunks.
	finalizeFn := func() (*Response, int, error) {
		var finEvicted []string
		defer func() {
			if len(finEvicted) > 0 {
				u.removeEvictedPartFiles(finEvicted, partKey)
			}
		}()

		unlockFin := u.acquirePartLock(partKey)
		defer unlockFin()

		u.mu.Lock()
		if st := u.chunkStates[partKey]; st != nil && st.completedResp != nil {
			respCopy := *st.completedResp
			u.mu.Unlock()
			return &respCopy, http.StatusOK, nil
		}
		u.mu.Unlock()

		finalName, destPath, err := u.finalizeChunkedFile(targetDir, partPath, fileName)
		if err != nil {
			truncateRegularPartFile(partPath, startOffset)
			rollbackStateToChunk()
			return nil, http.StatusInternalServerError, fmt.Errorf("cannot finalize file: %v", err)
		}

		relPath := filepath.ToSlash(filepath.Join(relativePrefix, finalName))
		finalResp := &Response{
			Success:      true,
			FileName:     finalName,
			RelativePath: relPath,
			FullPath:     destPath,
			Size:         totalWritten,
		}

		finNow := time.Now()
		u.mu.Lock()
		if u.chunkStates == nil {
			u.chunkStates = make(map[string]*chunkState)
		}
		finEvicted = u.pruneChunkStatesLocked(finNow, partKey)
		state.completedResp = finalResp
		state.updatedAt = finNow
		u.chunkStates[partKey] = state
		u.mu.Unlock()

		respCopy := *finalResp
		return &respCopy, http.StatusOK, nil
	}

	return nil, finalizeFn, rb, http.StatusOK, nil
}

func (u *Uploader) finalizeChunkedFile(targetDir, partPath, fileName string) (string, string, error) {
	u.finalizeMu.Lock()
	defer u.finalizeMu.Unlock()

	if err := rejectNonRegularPartFile(partPath); err != nil {
		return "", "", err
	}

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

func ensureUploadRootGitignore(uploadRoot string) {
	if uploadRoot == "" {
		return
	}
	if linfo, err := os.Lstat(uploadRoot); err != nil || linfo.Mode()&os.ModeSymlink != 0 || !linfo.IsDir() {
		return
	}
	gitignorePath := filepath.Join(uploadRoot, ".gitignore")
	if _, err := os.Lstat(gitignorePath); err == nil {
		return
	}
	f, err := os.OpenFile(gitignorePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return
	}
	_, _ = f.WriteString("*\n")
	_ = f.Close()
}

func ensureTargetUploadDir(targetDir string) error {
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return err
	}
	if parent := filepath.Dir(targetDir); filepath.Base(parent) == UploadDirName {
		ensureUploadRootGitignore(parent)
	}
	return nil
}

func createUniqueFile(targetDir, fileName string) (*os.File, string, string, error) {
	if err := ensureTargetUploadDir(targetDir); err != nil {
		return nil, "", "", err
	}
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
		f, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, uploadedFilePerm)
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
	_ = os.Chmod(destPath, uploadedFilePerm)
	return f, filepath.Base(destPath), destPath, nil
}

func moveOrCopyFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		_ = os.Chmod(dst, uploadedFilePerm)
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, uploadedFilePerm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	_ = os.Chmod(dst, uploadedFilePerm)
	_ = os.Remove(src)
	return nil
}

func (u *Uploader) effectiveWorkspaceRoot() (wsRoot string, home string) {
	home = config.HomeDir()
	wsRoot = u.workspaceRoot
	if wsRoot == "" && home != "" {
		preferred := filepath.Join(home, "antigravity", "workspace")
		legacy := filepath.Join(home, "workspace")
		if info, err := os.Stat(preferred); err == nil && info.IsDir() {
			wsRoot = preferred
		} else if info, err := os.Stat(legacy); err == nil && info.IsDir() {
			wsRoot = legacy
		} else {
			wsRoot = preferred
		}
	}
	return filepath.Clean(wsRoot), home
}

func (u *Uploader) ensureRootUploadDir() (string, error) {
	wsRoot, _ := u.effectiveWorkspaceRoot()
	rootUploadDir := filepath.Join(wsRoot, UploadDirName)
	if linfo, err := os.Lstat(rootUploadDir); err == nil && linfo.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("upload directory cannot be a symlink")
	}
	if err := os.MkdirAll(rootUploadDir, 0755); err != nil {
		return "", err
	}
	ensureUploadRootGitignore(rootUploadDir)
	return rootUploadDir, nil
}

func isSafeRelativeSubpath(rel string) bool {
	return rel != "" && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func isWithinOrEqualPath(target, root string) (string, bool) {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", false
	}
	if rel == "." {
		return ".", true
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

func evalExistingPath(p string) (string, error) {
	cleaned := filepath.Clean(p)
	cur := cleaned
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(cur)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return cleaned, nil
		}
		suffix = append(suffix, filepath.Base(cur))
		cur = parent
	}
}

func isUUIDString(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
			if !isHex {
				return false
			}
		}
	}
	return true
}

func unhexByte(b byte) (byte, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, true
	default:
		return 0, false
	}
}

func unescapeValidPercent(s string) string {
	if strings.IndexByte(s, '%') < 0 {
		return s
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if hi, ok1 := unhexByte(s[i+1]); ok1 {
				if lo, ok2 := unhexByte(s[i+2]); ok2 {
					out = append(out, (hi<<4)|lo)
					i += 2
					continue
				}
			}
		}
		out = append(out, s[i])
	}
	return string(out)
}

func stripFileURI(raw string) string {
	s := strings.TrimSpace(raw)
	if len(s) < 7 || !strings.EqualFold(s[:7], "file://") {
		return s
	}
	rest := unescapeValidPercent(s[7:])
	switch {
	case strings.HasPrefix(rest, "/"):
		// file:///path or file:///C:/path
	case len(rest) >= 10 && strings.EqualFold(rest[:10], "localhost/"):
		rest = rest[9:]
	case strings.EqualFold(rest, "localhost"):
		rest = "/"
	case len(rest) >= 2 && ((rest[0] >= 'a' && rest[0] <= 'z') || (rest[0] >= 'A' && rest[0] <= 'Z')) && (rest[1] == ':' || rest[1] == '|'):
		if rest[1] == '|' {
			rest = string(rest[0]) + ":" + rest[2:]
		}
	case rest == "~" || strings.HasPrefix(rest, "~/") || strings.HasPrefix(rest, `~\`):
		// file://~
	case rest != "":
		rest = "//" + rest
	}
	if len(rest) >= 3 && rest[0] == '/' && ((rest[1] >= 'a' && rest[1] <= 'z') || (rest[1] >= 'A' && rest[1] <= 'Z')) && (rest[2] == ':' || rest[2] == '|') {
		rest = string(rest[1]) + ":" + rest[3:]
	}
	return rest
}

func normalizeProjectPath(raw string) string {
	s := stripFileURI(raw)
	if s == "" {
		return ""
	}
	if len(s) >= 3 && s[0] == '/' && ((s[1] >= 'a' && s[1] <= 'z') || (s[1] >= 'A' && s[1] <= 'Z')) && s[2] == ':' {
		s = s[1:]
	}
	if isUUIDString(s) || isUUIDString(filepath.Base(s)) {
		return ""
	}
	return s
}

func (u *Uploader) resolveTargetDir(projectPath, conversationID string) (targetDir string, relativePrefix string, err error) {
	wsRoot, home := u.effectiveWorkspaceRoot()
	realWsRoot, err := evalExistingPath(wsRoot)
	if err != nil {
		return "", "", fmt.Errorf("invalid workspace root: %v", err)
	}

	normProj := normalizeProjectPath(projectPath)
	if normProj != "" {
		cleanProj := filepath.Clean(normProj)
		if !filepath.IsAbs(cleanProj) {
			cleanProj = filepath.Join(wsRoot, cleanProj)
		}
		if rel, err := filepath.Rel(wsRoot, cleanProj); err == nil && isSafeRelativeSubpath(rel) {
			if info, statErr := os.Stat(cleanProj); statErr == nil && info.IsDir() {
				realProj, evalErr := filepath.EvalSymlinks(cleanProj)
				if evalErr != nil {
					return "", "", fmt.Errorf("invalid project path: %v", evalErr)
				}
				if _, ok := isWithinOrEqualPath(realProj, realWsRoot); !ok {
					return "", "", fmt.Errorf("project path escapes workspace root")
				}
				projUploadRoot := filepath.Join(cleanProj, UploadDirName)
				if linfo, lerr := os.Lstat(projUploadRoot); lerr == nil && linfo.Mode()&os.ModeSymlink != 0 {
					return "", "", fmt.Errorf("project upload directory cannot be a symlink")
				}
				targetDir = filepath.Join(projUploadRoot, conversationID)
				if linfo, lerr := os.Lstat(targetDir); lerr == nil && linfo.Mode()&os.ModeSymlink != 0 {
					return "", "", fmt.Errorf("conversation upload directory cannot be a symlink")
				}
				u.rememberUploadRoot(filepath.Join(realProj, UploadDirName))
				relativePrefix = filepath.Join(UploadDirName, conversationID)
				return targetDir, relativePrefix, nil
			}
		}
	}

	rootUploadDir := filepath.Join(wsRoot, UploadDirName)
	if linfo, lerr := os.Lstat(rootUploadDir); lerr == nil && linfo.Mode()&os.ModeSymlink != 0 {
		return "", "", fmt.Errorf("upload directory cannot be a symlink")
	}
	targetDir = filepath.Join(rootUploadDir, conversationID)
	if linfo, lerr := os.Lstat(targetDir); lerr == nil && linfo.Mode()&os.ModeSymlink != 0 {
		return "", "", fmt.Errorf("conversation upload directory cannot be a symlink")
	}

	if home != "" {
		cleanHome := filepath.Clean(home)
		if rel, ok := isWithinOrEqualPath(targetDir, cleanHome); ok && rel != "." {
			return targetDir, rel, nil
		}
	}

	return targetDir, targetDir, nil
}

func isRealDirectory(path string) bool {
	linfo, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return linfo.Mode()&os.ModeSymlink == 0 && linfo.IsDir()
}

func shouldSkipWorkspaceSubdir(name string) bool {
	switch name {
	case "", ".", "..", ".git", "node_modules", "vendor", ".venv", "venv", "target", "dist", "build", ".next", ".cache", UploadDirName:
		return true
	default:
		return false
	}
}

func (u *Uploader) cleanupOldFiles() {
	wsRoot, _ := u.effectiveWorkspaceRoot()
	realWsRoot, err := evalExistingPath(wsRoot)
	if err != nil {
		return
	}
	wsInfo, err := os.Stat(realWsRoot)
	if err != nil || !wsInfo.IsDir() {
		return
	}

	cutoff := time.Now().Add(-u.ttl)
	partCutoff := time.Now().Add(-StalePartTTL)
	if cutoff.After(partCutoff) {
		partCutoff = cutoff
	}

	cleaned := make(map[string]struct{})
	cleanOnce := func(uploadDir string) {
		cleanPath := filepath.Clean(uploadDir)
		if _, seen := cleaned[cleanPath]; seen {
			return
		}
		cleaned[cleanPath] = struct{}{}
		u.cleanUploadDir(cleanPath, cutoff, partCutoff)
	}

	// 1. Clean dedicated root upload directory: <workspaceRoot>/.agy-uploads
	cleanOnce(filepath.Join(realWsRoot, UploadDirName))

	// 2. Clean project-specific dedicated upload directories up to maxWorkspaceScanDepth levels deep
	scannedDirs := 0
	var scanWorkspace func(dir string, depth int)
	scanWorkspace = func(dir string, depth int) {
		if depth > maxWorkspaceScanDepth || scannedDirs >= maxWorkspaceScanDirs {
			return
		}
		scannedDirs++
		entries, readErr := os.ReadDir(dir)
		if readErr != nil {
			return
		}
		for _, entry := range entries {
			if scannedDirs >= maxWorkspaceScanDirs {
				return
			}
			if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() || shouldSkipWorkspaceSubdir(entry.Name()) {
				continue
			}
			subDir := filepath.Join(dir, entry.Name())
			cleanOnce(filepath.Join(subDir, UploadDirName))
			if depth < maxWorkspaceScanDepth {
				scanWorkspace(subDir, depth+1)
			}
		}
	}
	scanWorkspace(realWsRoot, 1)

	// 3. Also clean any deeper project upload roots previously resolved during this server run
	u.mu.Lock()
	known := make([]string, 0, len(u.knownUploadRoots))
	for root := range u.knownUploadRoots {
		known = append(known, root)
	}
	u.mu.Unlock()
	for _, root := range known {
		if _, ok := isWithinOrEqualPath(root, realWsRoot); ok {
			cleanOnce(root)
		}
	}

	// 4. Evict any stale or completed in-memory chunk session metadata under partLock
	completedCutoff := time.Now().Add(-completedSessionTTL)
	var staleKeys []string
	u.mu.Lock()
	for k, st := range u.chunkStates {
		if st == nil || st.updatedAt.Before(partCutoff) || (st.completedResp != nil && st.updatedAt.Before(completedCutoff)) {
			staleKeys = append(staleKeys, k)
		}
	}
	u.mu.Unlock()

	for _, k := range staleKeys {
		unlock := u.acquirePartLock(k)
		removePart := false
		u.mu.Lock()
		if st, ok := u.chunkStates[k]; ok {
			if st == nil || st.updatedAt.Before(partCutoff) || (st.completedResp != nil && st.updatedAt.Before(completedCutoff)) {
				if st != nil && st.completedResp == nil {
					removePart = true
				}
				delete(u.chunkStates, k)
			}
		}
		u.mu.Unlock()
		if removePart {
			removeRegularPartFile(k)
		}
		unlock()
	}
}

func (u *Uploader) cleanUploadDir(dir string, cutoff, partCutoff time.Time) {
	if !isRealDirectory(dir) {
		return
	}

	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		name := d.Name()
		if name == ".gitignore" && filepath.Clean(filepath.Dir(path)) == filepath.Clean(dir) {
			return nil
		}
		isPart := strings.HasPrefix(name, partFilePrefix)
		isStage := strings.HasPrefix(name, stageFilePrefix)
		targetCutoff := cutoff
		if isPart || isStage {
			targetCutoff = partCutoff
		}
		if info.ModTime().Before(targetCutoff) {
			if isPart {
				partKey := canonicalPartKey(path)
				unlock := u.acquirePartLock(partKey)
				if latestInfo, statErr := os.Stat(path); statErr == nil && latestInfo.ModTime().Before(targetCutoff) {
					if rmErr := os.Remove(path); rmErr == nil {
						u.mu.Lock()
						delete(u.chunkStates, partKey)
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

	u.finalizeMu.Lock()
	removeEmptyDirs(dir)
	u.finalizeMu.Unlock()
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

func isWindowsReservedDeviceName(stem string) bool {
	switch strings.ToUpper(strings.TrimSpace(stem)) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	default:
		return false
	}
}

func sanitizeFileName(name string) string {
	normalized := strings.ReplaceAll(strings.TrimSpace(name), "\\", "/")
	normalized = strings.TrimRight(normalized, "/")
	if idx := strings.LastIndexByte(normalized, '/'); idx >= 0 {
		normalized = normalized[idx+1:]
	}
	if len(normalized) >= 2 && ((normalized[0] >= 'a' && normalized[0] <= 'z') || (normalized[0] >= 'A' && normalized[0] <= 'Z')) && normalized[1] == ':' {
		normalized = normalized[2:]
	}

	var b strings.Builder
	b.Grow(len(normalized))
	for _, r := range normalized {
		if r < 0x20 || r == 0x7f {
			b.WriteByte('_')
			continue
		}
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}

	cleaned := strings.TrimSpace(b.String())
	cleaned = strings.TrimRight(cleaned, " .")
	for strings.HasPrefix(cleaned, partFilePrefix) || strings.HasPrefix(cleaned, stageFilePrefix) {
		cleaned = strings.TrimPrefix(cleaned, ".")
	}
	if cleaned == "" || cleaned == "." || cleaned == ".." {
		return fmt.Sprintf("upload_%d.bin", time.Now().Unix())
	}

	stem := cleaned
	if dot := strings.IndexByte(stem, '.'); dot >= 0 {
		stem = stem[:dot]
	}
	if isWindowsReservedDeviceName(stem) {
		cleaned = "_" + cleaned
	}
	return cleaned
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
