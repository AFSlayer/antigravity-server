package upload

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestUploadStreaming(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "my-project"), 0755); err != nil {
		t.Fatal(err)
	}
	u := New(Options{WorkspaceRoot: tmpDir, TTL: 24 * time.Hour})

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("conversationId", "convo-123")
	_ = writer.WriteField("projectPath", "my-project")

	part, err := writer.CreateFormFile("file", "test.har")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	sampleData := []byte("HAR FILE CONTENT SAMPLE 12345")
	if _, err := part.Write(sampleData); err != nil {
		t.Fatalf("write sample data: %v", err)
	}
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, UploadAPIPath, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rec := httptest.NewRecorder()
	u.handleUpload(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	expectedPath := filepath.Join(tmpDir, "my-project", UploadDirName, "convo-123", "test.har")
	content, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if string(content) != string(sampleData) {
		t.Errorf("content = %q, want %q", string(content), string(sampleData))
	}

	gitignoreBytes, err := os.ReadFile(filepath.Join(tmpDir, "my-project", UploadDirName, ".gitignore"))
	if err != nil {
		t.Fatalf("expected .gitignore inside .agy-uploads: %v", err)
	}
	if string(gitignoreBytes) != "*\n" {
		t.Errorf(".gitignore content = %q, want %q", string(gitignoreBytes), "*\n")
	}
}

func TestUploadOutsideProject(t *testing.T) {
	tmpDir := t.TempDir()
	u := New(Options{WorkspaceRoot: tmpDir})

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("conversationId", "convo-456")

	part, _ := writer.CreateFormFile("file", "sample.log")
	_, _ = part.Write([]byte("log line 1"))
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, UploadAPIPath, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rec := httptest.NewRecorder()
	u.handleUpload(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	expectedPath := filepath.Join(tmpDir, UploadDirName, "convo-456", "sample.log")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Fatalf("file not created at expected path: %v", err)
	}
	var resp Response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.RelativePath != expectedPath {
		t.Errorf("non-home workspace RelativePath = %q, want %q", resp.RelativePath, expectedPath)
	}
}

func TestCleanerPreservesUserUploadsFolder(t *testing.T) {
	tmpDir := t.TempDir()
	u := New(Options{WorkspaceRoot: tmpDir, TTL: 10 * time.Millisecond})

	targetDir := filepath.Join(tmpDir, "proj", UploadDirName, "convo-789")
	_ = os.MkdirAll(targetDir, 0755)
	oldFile := filepath.Join(targetDir, "old.har")
	_ = os.WriteFile(oldFile, []byte("old content"), 0644)

	nestedDir := filepath.Join(tmpDir, "org", "repo", UploadDirName, "convo-nested")
	_ = os.MkdirAll(nestedDir, 0755)
	nestedOld := filepath.Join(nestedDir, "nested.har")
	_ = os.WriteFile(nestedOld, []byte("nested old"), 0644)

	// User application's own uploads directory MUST NOT be touched by cleaner
	userAppUploads := filepath.Join(tmpDir, "proj", "uploads")
	_ = os.MkdirAll(userAppUploads, 0755)
	userAsset := filepath.Join(userAppUploads, "avatar.png")
	_ = os.WriteFile(userAsset, []byte("precious user asset"), 0644)

	rootUserUploads := filepath.Join(tmpDir, "uploads")
	_ = os.MkdirAll(rootUserUploads, 0755)
	rootAsset := filepath.Join(rootUserUploads, "logo.svg")
	_ = os.WriteFile(rootAsset, []byte("precious root asset"), 0644)

	past := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(oldFile, past, past)
	_ = os.Chtimes(nestedOld, past, past)
	_ = os.Chtimes(userAsset, past, past)
	_ = os.Chtimes(rootAsset, past, past)

	u.cleanupOldFiles()

	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Errorf("old file in .agy-uploads was not cleaned up")
	}
	if _, err := os.Stat(nestedOld); !os.IsNotExist(err) {
		t.Errorf("nested old file in .agy-uploads was not cleaned up")
	}
	if _, err := os.Stat(userAsset); err != nil {
		t.Errorf("user project uploads/avatar.png was wrongly deleted: %v", err)
	}
	if _, err := os.Stat(rootAsset); err != nil {
		t.Errorf("workspace uploads/logo.svg was wrongly deleted: %v", err)
	}
}

func TestPathTraversalAttemptBlocked(t *testing.T) {
	tmpDir := t.TempDir()
	u := New(Options{WorkspaceRoot: tmpDir})

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("conversationId", "convo-evil/../../../etc")
	_ = writer.WriteField("projectPath", "../../../../../etc")

	part, _ := writer.CreateFormFile("file", "../../../../../evil.sh")
	_, _ = part.Write([]byte("malicious content"))
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, UploadAPIPath, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rec := httptest.NewRecorder()
	u.handleUpload(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// File must be strictly confined inside tmpDir/.agy-uploads/convo-eviletc/evil.sh
	expectedPath := filepath.Join(tmpDir, UploadDirName, "convo-eviletc", "evil.sh")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Fatalf("file not safely sandboxed inside workspace root: %v", err)
	}
}

func TestUploadDuplicateFileNameDoesNotOverwrite(t *testing.T) {
	tmpDir := t.TempDir()
	u := New(Options{WorkspaceRoot: tmpDir})

	uploadOnce := func(content string) {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		_ = writer.WriteField("conversationId", "convo-dup")
		part, err := writer.CreateFormFile("file", "image.png")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write([]byte(content))
		_ = writer.Close()

		req := httptest.NewRequest(http.MethodPost, UploadAPIPath, &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		rec := httptest.NewRecorder()
		u.handleUpload(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
		}
	}

	uploadOnce("first image")
	uploadOnce("second image")

	first, err := os.ReadFile(filepath.Join(tmpDir, UploadDirName, "convo-dup", "image.png"))
	if err != nil {
		t.Fatalf("read first upload: %v", err)
	}
	if string(first) != "first image" {
		t.Errorf("first upload was overwritten: got %q", string(first))
	}

	second, err := os.ReadFile(filepath.Join(tmpDir, UploadDirName, "convo-dup", "image_2.png"))
	if err != nil {
		t.Fatalf("read second upload: %v", err)
	}
	if string(second) != "second image" {
		t.Errorf("second upload content = %q, want %q", string(second), "second image")
	}
}

func TestChunkedUploadReassemblyAndRetry(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "firmware-proj"), 0755); err != nil {
		t.Fatal(err)
	}
	u := New(Options{WorkspaceRoot: tmpDir})

	sendChunk := func(uploadID string, chunkIndex, totalChunks int, chunkOffset int64, fileName, content string) (*httptest.ResponseRecorder, Response) {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		_ = writer.WriteField("conversationId", "convo-chunk")
		_ = writer.WriteField("projectPath", "firmware-proj")
		_ = writer.WriteField("uploadId", uploadID)
		_ = writer.WriteField("chunkIndex", strconv.Itoa(chunkIndex))
		_ = writer.WriteField("totalChunks", strconv.Itoa(totalChunks))
		_ = writer.WriteField("chunkOffset", strconv.FormatInt(chunkOffset, 10))
		_ = writer.WriteField("fileName", fileName)

		part, err := writer.CreateFormFile("file", fileName)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write([]byte(content))
		_ = writer.Close()

		req := httptest.NewRequest(http.MethodPost, UploadAPIPath, &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		rec := httptest.NewRecorder()
		u.handleUpload(rec, req)

		var resp Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		return rec, resp
	}

	// Chunk 0
	rec, resp := sendChunk("up-rom-1", 0, 3, 0, "216.30.11.225.rom", "AAAA-")
	if rec.Code != http.StatusOK || !resp.Success {
		t.Fatalf("chunk 0 failed: code=%d body=%s", rec.Code, rec.Body.String())
	}
	if resp.RelativePath != "" {
		t.Errorf("expected empty RelativePath on intermediate chunk 0, got %q", resp.RelativePath)
	}

	// Chunk 1 (first attempt)
	rec, resp = sendChunk("up-rom-1", 1, 3, 5, "216.30.11.225.rom", "BBBB-partial-retry-will-overwrite")
	if rec.Code != http.StatusOK || !resp.Success {
		t.Fatalf("chunk 1 attempt 1 failed: code=%d body=%s", rec.Code, rec.Body.String())
	}

	// Chunk 1 retry at offset 5 (must truncate and overwrite idempotently)
	rec, resp = sendChunk("up-rom-1", 1, 3, 5, "216.30.11.225.rom", "BBBB-")
	if rec.Code != http.StatusOK || !resp.Success {
		t.Fatalf("chunk 1 retry failed: code=%d body=%s", rec.Code, rec.Body.String())
	}

	// Out-of-order or wrong-offset chunk 2 must fail
	badRec, _ := sendChunk("up-rom-1", 2, 3, 999, "216.30.11.225.rom", "CCCC")
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on mismatched chunkOffset, got %d", badRec.Code)
	}

	// Chunk 2 (final chunk at offset 10)
	rec, resp = sendChunk("up-rom-1", 2, 3, 10, "216.30.11.225.rom", "CCCC")
	if rec.Code != http.StatusOK || !resp.Success {
		t.Fatalf("chunk 2 failed: code=%d body=%s", rec.Code, rec.Body.String())
	}
	wantRel := filepath.Join(UploadDirName, "convo-chunk", "216.30.11.225.rom")
	if resp.RelativePath != wantRel {
		t.Errorf("RelativePath = %q, want %q", resp.RelativePath, wantRel)
	}
	if resp.Size != 14 {
		t.Errorf("Size = %d, want 14", resp.Size)
	}

	saved, err := os.ReadFile(filepath.Join(tmpDir, "firmware-proj", wantRel))
	if err != nil {
		t.Fatalf("read finalized file: %v", err)
	}
	if string(saved) != "AAAA-BBBB-CCCC" {
		t.Errorf("saved content = %q, want %q", string(saved), "AAAA-BBBB-CCCC")
	}

	// Staging file must no longer exist
	stagingPath := filepath.Join(tmpDir, "firmware-proj", UploadDirName, "convo-chunk", partFilePrefix+"up-rom-1")
	if _, err := os.Stat(stagingPath); !os.IsNotExist(err) {
		t.Errorf("staging file still exists after finalization: %v", err)
	}

	// Retrying the final chunk (e.g. if response was lost in transit) must return the same finalized Response idempotently
	retryRec, retryResp := sendChunk("up-rom-1", 2, 3, 10, "216.30.11.225.rom", "CCCC")
	if retryRec.Code != http.StatusOK || !retryResp.Success {
		t.Fatalf("final chunk retry failed: code=%d body=%s", retryRec.Code, retryRec.Body.String())
	}
	if retryResp.RelativePath != wantRel {
		t.Errorf("retry RelativePath = %q, want %q", retryResp.RelativePath, wantRel)
	}
}

func TestChunkedUploadAbortDeletesStagingFile(t *testing.T) {
	tmpDir := t.TempDir()
	u := New(Options{WorkspaceRoot: tmpDir})

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("conversationId", "convo-abort")
	_ = writer.WriteField("uploadId", "abort-id-99")
	_ = writer.WriteField("chunkIndex", "0")
	_ = writer.WriteField("totalChunks", "4")
	part, _ := writer.CreateFormFile("file", "abort.rom")
	_, _ = part.Write([]byte("chunk0-bytes"))
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, UploadAPIPath, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	u.handleUpload(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("chunk 0 status = %d, body = %s", rec.Code, rec.Body.String())
	}

	stagingPath := filepath.Join(tmpDir, UploadDirName, "convo-abort", partFilePrefix+"abort-id-99")
	if _, err := os.Stat(stagingPath); err != nil {
		t.Fatalf("staging file not created: %v", err)
	}

	delReq := httptest.NewRequest(http.MethodDelete, UploadAPIPath+"?uploadId=abort-id-99&conversationId=convo-abort", nil)
	delRec := httptest.NewRecorder()
	u.handleUpload(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d, body = %s", delRec.Code, delRec.Body.String())
	}

	if _, err := os.Stat(stagingPath); !os.IsNotExist(err) {
		t.Errorf("staging file still exists after DELETE abort")
	}
}

func TestUploadFilePartBeforeMetadataAndGhostUUIDPrevention(t *testing.T) {
	tmpDir := t.TempDir()
	realProj := filepath.Join(tmpDir, "real-proj")
	if err := os.MkdirAll(realProj, 0755); err != nil {
		t.Fatal(err)
	}
	u := New(Options{WorkspaceRoot: tmpDir})

	// 1. File part placed BEFORE metadata fields with file:// URI projectPath
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "first.txt")
	_, _ = part.Write([]byte("file-first-payload"))
	_ = writer.WriteField("conversationId", "convo-order")
	_ = writer.WriteField("projectPath", "file://"+realProj)
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, UploadAPIPath, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	u.handleUpload(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("file-first upload failed: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(realProj, UploadDirName, "convo-order", "first.txt")); err != nil {
		t.Fatalf("expected file in realProj/.agy-uploads: %v", err)
	}

	// 1b. Chunk upload metadata placed AFTER file part must be rejected with 400
	var badOrderBody bytes.Buffer
	badOrderWriter := multipart.NewWriter(&badOrderBody)
	badPart, _ := badOrderWriter.CreateFormFile("file", "chunked.bin")
	_, _ = badPart.Write([]byte("0123456789"))
	_ = badOrderWriter.WriteField("uploadId", "late-meta")
	_ = badOrderWriter.WriteField("chunkIndex", "0")
	_ = badOrderWriter.WriteField("totalChunks", "2")
	_ = badOrderWriter.Close()

	badOrderReq := httptest.NewRequest(http.MethodPost, UploadAPIPath, &badOrderBody)
	badOrderReq.Header.Set("Content-Type", badOrderWriter.FormDataContentType())
	badOrderRec := httptest.NewRecorder()
	u.handleUpload(badOrderRec, badOrderReq)
	if badOrderRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when chunk metadata follows file part, got %d: %s", badOrderRec.Code, badOrderRec.Body.String())
	}

	// 2. Non-existent or UUID projectPath must NOT create a ghost directory
	ghostUUID := "123e4567-e89b-12d3-a456-426614174000"
	var body2 bytes.Buffer
	w2 := multipart.NewWriter(&body2)
	_ = w2.WriteField("conversationId", "convo-uuid")
	_ = w2.WriteField("projectPath", ghostUUID)
	p2, _ := w2.CreateFormFile("file", "note.txt")
	_, _ = p2.Write([]byte("hello"))
	_ = w2.Close()

	req2 := httptest.NewRequest(http.MethodPost, UploadAPIPath, &body2)
	req2.Header.Set("Content-Type", w2.FormDataContentType())
	rec2 := httptest.NewRecorder()
	u.handleUpload(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("uuid fallback upload failed: %d %s", rec2.Code, rec2.Body.String())
	}
	if _, err := os.Stat(filepath.Join(tmpDir, ghostUUID)); !os.IsNotExist(err) {
		t.Errorf("ghost UUID directory should not have been created")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, UploadDirName, "convo-uuid", "note.txt")); err != nil {
		t.Errorf("expected fallback to workspace .agy-uploads: %v", err)
	}

	// 3. Symlink project escaping workspace must be rejected
	outside := t.TempDir()
	escapeLink := filepath.Join(tmpDir, "escape-link")
	if err := os.Symlink(outside, escapeLink); err == nil {
		var body3 bytes.Buffer
		w3 := multipart.NewWriter(&body3)
		_ = w3.WriteField("conversationId", "convo-sym")
		_ = w3.WriteField("projectPath", "escape-link")
		p3, _ := w3.CreateFormFile("file", "escape.txt")
		_, _ = p3.Write([]byte("blocked"))
		_ = w3.Close()

		req3 := httptest.NewRequest(http.MethodPost, UploadAPIPath, &body3)
		req3.Header.Set("Content-Type", w3.FormDataContentType())
		rec3 := httptest.NewRecorder()
		u.handleUpload(rec3, req3)
		if rec3.Code != http.StatusBadRequest {
			t.Errorf("expected 400 when project symlink escapes workspace, got %d", rec3.Code)
		}
	}
}

func TestChunkedUploadDirectStreamCapAndSanitizeStagePrefix(t *testing.T) {
	tmpDir := t.TempDir()
	u := New(Options{WorkspaceRoot: tmpDir, TTL: 24 * time.Hour})
	u.maxChunkBytes = 16

	sendChunk := func(chunkIndex, totalChunks int, offset int64, fileName, payload string) *httptest.ResponseRecorder {
		t.Helper()
		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		_ = w.WriteField("conversationId", "convo-cap")
		_ = w.WriteField("uploadId", "cap-session-1")
		_ = w.WriteField("chunkIndex", strconv.Itoa(chunkIndex))
		_ = w.WriteField("totalChunks", strconv.Itoa(totalChunks))
		_ = w.WriteField("chunkOffset", strconv.FormatInt(offset, 10))
		_ = w.WriteField("fileName", fileName)
		part, err := w.CreateFormFile("file", fileName)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write([]byte(payload))
		_ = w.Close()

		req := httptest.NewRequest(http.MethodPost, UploadAPIPath, &body)
		req.Header.Set("Content-Type", w.FormDataContentType())
		rec := httptest.NewRecorder()
		u.handleUpload(rec, req)
		return rec
	}

	// Oversized chunk 0 (> 16 bytes) must fail with 413 without leaving part or stage files
	overRec := sendChunk(0, 2, 0, ".agy_stage_report.txt", "0123456789ABCDEFG")
	if overRec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 on oversized chunk 0, got %d: %s", overRec.Code, overRec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(tmpDir, UploadDirName, "convo-cap", partFilePrefix+"cap-session-1")); !os.IsNotExist(err) {
		t.Errorf("expected failed chunk 0 part file to be removed, err=%v", err)
	}

	// Valid chunk 0 (16 bytes), followed by oversized chunk 1 (> 16 bytes) -> must 413 and preserve chunk 0 state
	okRec0 := sendChunk(0, 2, 0, ".agy_stage_report.txt", "0123456789ABCDEF")
	if okRec0.Code != http.StatusOK {
		t.Fatalf("chunk 0 within limit failed: %d %s", okRec0.Code, okRec0.Body.String())
	}
	// Root staging dir should not have been created for direct-streamed chunks
	if _, err := os.Stat(filepath.Join(tmpDir, UploadDirName)); err == nil {
		entries, _ := os.ReadDir(filepath.Join(tmpDir, UploadDirName))
		for _, ent := range entries {
			if len(ent.Name()) >= len(stageFilePrefix) && ent.Name()[:len(stageFilePrefix)] == stageFilePrefix {
				t.Errorf("unexpected root stage file created during direct chunk stream: %s", ent.Name())
			}
		}
	}

	overRec1 := sendChunk(1, 2, 16, ".agy_stage_report.txt", "0123456789ABCDEFG")
	if overRec1.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 on oversized chunk 1, got %d: %s", overRec1.Code, overRec1.Body.String())
	}

	// Retry chunk 1 with valid 4-byte payload at offset 16 -> finalizes cleanly with stripped leading dot
	okRec1 := sendChunk(1, 2, 16, ".agy_stage_report.txt", "TAIL")
	if okRec1.Code != http.StatusOK {
		t.Fatalf("chunk 1 retry within limit failed: %d %s", okRec1.Code, okRec1.Body.String())
	}

	var finalResp Response
	if err := json.Unmarshal(okRec1.Body.Bytes(), &finalResp); err != nil {
		t.Fatal(err)
	}
	if finalResp.FileName != "agy_stage_report.txt" {
		t.Errorf("FileName = %q, want %q (must strip internal stage prefix)", finalResp.FileName, "agy_stage_report.txt")
	}
	data, err := os.ReadFile(finalResp.FullPath)
	if err != nil {
		t.Fatalf("read finalized file: %v", err)
	}
	if string(data) != "0123456789ABCDEFTAIL" {
		t.Errorf("finalized content = %q, want %q", string(data), "0123456789ABCDEFTAIL")
	}

	// File aged 3h (> StalePartTTL=2h, < TTL=24h) must NOT be deleted early as a stage file
	threeHoursAgo := time.Now().Add(-3 * time.Hour)
	_ = os.Chtimes(finalResp.FullPath, threeHoursAgo, threeHoursAgo)
	u.cleanupOldFiles()
	if _, err := os.Stat(finalResp.FullPath); err != nil {
		t.Errorf("sanitized user file was prematurely deleted before TTL: %v", err)
	}
}

func TestCleanerHandlesSymlinkWorkspaceRootAndDeepProjects(t *testing.T) {
	backingDir := t.TempDir()
	linkHolder := t.TempDir()
	symlinkWs := filepath.Join(linkHolder, "workspace-link")
	if err := os.Symlink(backingDir, symlinkWs); err != nil {
		t.Skipf("symlink not supported in this environment: %v", err)
	}

	u := New(Options{WorkspaceRoot: symlinkWs, TTL: 10 * time.Millisecond})

	deepUploadDir := filepath.Join(backingDir, "team", "mono", "apps", "web", UploadDirName, "convo-deep")
	if err := os.MkdirAll(deepUploadDir, 0755); err != nil {
		t.Fatal(err)
	}
	deepOld := filepath.Join(deepUploadDir, "expired.bin")
	if err := os.WriteFile(deepOld, []byte("expired"), 0644); err != nil {
		t.Fatal(err)
	}

	rootUploadDir := filepath.Join(backingDir, UploadDirName, "convo-root")
	if err := os.MkdirAll(rootUploadDir, 0755); err != nil {
		t.Fatal(err)
	}
	rootOld := filepath.Join(rootUploadDir, "root-expired.bin")
	if err := os.WriteFile(rootOld, []byte("expired"), 0644); err != nil {
		t.Fatal(err)
	}

	past := time.Now().Add(-3 * time.Hour)
	_ = os.Chtimes(deepOld, past, past)
	_ = os.Chtimes(rootOld, past, past)

	u.cleanupOldFiles()

	if _, err := os.Stat(deepOld); !os.IsNotExist(err) {
		t.Errorf("deep project expired upload was not cleaned under symlinked workspace root")
	}
	if _, err := os.Stat(rootOld); !os.IsNotExist(err) {
		t.Errorf("root expired upload was not cleaned under symlinked workspace root")
	}
}

func TestFinalChunkTrailingErrorPreservesPreviousChunks(t *testing.T) {
	tmpDir := t.TempDir()
	u := New(Options{WorkspaceRoot: tmpDir})

	sendNormalChunk := func(idx, total int, offset int64, content string) (*httptest.ResponseRecorder, Response) {
		t.Helper()
		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		_ = w.WriteField("conversationId", "convo-final-rb")
		_ = w.WriteField("uploadId", "session-final-rb")
		_ = w.WriteField("chunkIndex", strconv.Itoa(idx))
		_ = w.WriteField("totalChunks", strconv.Itoa(total))
		_ = w.WriteField("chunkOffset", strconv.FormatInt(offset, 10))
		_ = w.WriteField("fileName", "bundle.bin")
		p, err := w.CreateFormFile("file", "bundle.bin")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = p.Write([]byte(content))
		_ = w.Close()

		req := httptest.NewRequest(http.MethodPost, UploadAPIPath, &body)
		req.Header.Set("Content-Type", w.FormDataContentType())
		rec := httptest.NewRecorder()
		u.handleUpload(rec, req)
		var resp Response
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		return rec, resp
	}

	if rec, _ := sendNormalChunk(0, 3, 0, "PART0-"); rec.Code != http.StatusOK {
		t.Fatalf("chunk 0 failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec, _ := sendNormalChunk(1, 3, 6, "PART1-"); rec.Code != http.StatusOK {
		t.Fatalf("chunk 1 failed: %d %s", rec.Code, rec.Body.String())
	}

	// 1. Final chunk with a trailing metadata field after the file part -> 400 without losing chunks 0..1
	var badBody bytes.Buffer
	badW := multipart.NewWriter(&badBody)
	_ = badW.WriteField("conversationId", "convo-final-rb")
	_ = badW.WriteField("uploadId", "session-final-rb")
	_ = badW.WriteField("chunkIndex", "2")
	_ = badW.WriteField("totalChunks", "3")
	_ = badW.WriteField("chunkOffset", "12")
	_ = badW.WriteField("fileName", "bundle.bin")
	badPart, _ := badW.CreateFormFile("file", "bundle.bin")
	_, _ = badPart.Write([]byte("BAD-TAIL"))
	_ = badW.WriteField("chunkOffset", "12")
	_ = badW.Close()

	badReq := httptest.NewRequest(http.MethodPost, UploadAPIPath, &badBody)
	badReq.Header.Set("Content-Type", badW.FormDataContentType())
	badRec := httptest.NewRecorder()
	u.handleUpload(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on trailing chunk metadata, got %d", badRec.Code)
	}

	// 2. Final chunk truncated mid-stream and truncated in a trailing multipart boundary -> 400 without losing chunks 0..1
	var truncBody bytes.Buffer
	truncW := multipart.NewWriter(&truncBody)
	_ = truncW.WriteField("conversationId", "convo-final-rb")
	_ = truncW.WriteField("uploadId", "session-final-rb")
	_ = truncW.WriteField("chunkIndex", "2")
	_ = truncW.WriteField("totalChunks", "3")
	_ = truncW.WriteField("chunkOffset", "12")
	_ = truncW.WriteField("fileName", "bundle.bin")
	truncPart, _ := truncW.CreateFormFile("file", "bundle.bin")
	_, _ = truncPart.Write([]byte("TRUNC"))
	truncReq := httptest.NewRequest(http.MethodPost, UploadAPIPath, &truncBody)
	truncReq.Header.Set("Content-Type", truncW.FormDataContentType())
	truncRec := httptest.NewRecorder()
	u.handleUpload(truncRec, truncReq)
	if truncRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on truncated file part, got %d", truncRec.Code)
	}

	var trailerTruncBody bytes.Buffer
	trailerW := multipart.NewWriter(&trailerTruncBody)
	_ = trailerW.WriteField("conversationId", "convo-final-rb")
	_ = trailerW.WriteField("uploadId", "session-final-rb")
	_ = trailerW.WriteField("chunkIndex", "2")
	_ = trailerW.WriteField("totalChunks", "3")
	_ = trailerW.WriteField("chunkOffset", "12")
	_ = trailerW.WriteField("fileName", "bundle.bin")
	trailerPart, _ := trailerW.CreateFormFile("file", "bundle.bin")
	_, _ = trailerPart.Write([]byte("TRAIL"))
	// Close the file part boundary cleanly, then emit a malformed trailing part header so mr.NextPart() fails after writeChunk!
	_, _ = trailerTruncBody.WriteString("\r\n--" + trailerW.Boundary() + "\r\nMalformed-Header-Without-Colon\r\n\r\n")
	trailerReq := httptest.NewRequest(http.MethodPost, UploadAPIPath, &trailerTruncBody)
	trailerReq.Header.Set("Content-Type", trailerW.FormDataContentType())
	trailerRec := httptest.NewRecorder()
	u.handleUpload(trailerRec, trailerReq)
	if trailerRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on malformed multipart trailer, got %d", trailerRec.Code)
	}

	// 3. Clean retry of ONLY final chunk 2 must succeed and assemble PART0-PART1-PART2 intact!
	okRec, okResp := sendNormalChunk(2, 3, 12, "PART2")
	if okRec.Code != http.StatusOK || !okResp.Success {
		t.Fatalf("final chunk retry failed after trailing error: %d %s", okRec.Code, okRec.Body.String())
	}
	gotBytes, err := os.ReadFile(okResp.FullPath)
	if err != nil {
		t.Fatalf("read assembled file: %v", err)
	}
	if string(gotBytes) != "PART0-PART1-PART2" {
		t.Fatalf("assembled content = %q, want %q", string(gotBytes), "PART0-PART1-PART2")
	}
}

func TestChunkedUploadTotalSizeCapSessionIntegrityAndEvictionCleanup(t *testing.T) {
	tmpDir := t.TempDir()
	u := New(Options{WorkspaceRoot: tmpDir})
	u.maxChunkBytes = 16
	u.maxTotalBytes = 24
	u.maxChunkStatesLimit = 2

	send := func(uploadID string, idx, total int, offset string, payload string) *httptest.ResponseRecorder {
		t.Helper()
		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		_ = w.WriteField("conversationId", "convo-sec")
		_ = w.WriteField("uploadId", uploadID)
		_ = w.WriteField("chunkIndex", strconv.Itoa(idx))
		_ = w.WriteField("totalChunks", strconv.Itoa(total))
		if offset != "" {
			_ = w.WriteField("chunkOffset", offset)
		}
		_ = w.WriteField("fileName", uploadID+".bin")
		p, err := w.CreateFormFile("file", uploadID+".bin")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = p.Write([]byte(payload))
		_ = w.Close()

		req := httptest.NewRequest(http.MethodPost, UploadAPIPath, &body)
		req.Header.Set("Content-Type", w.FormDataContentType())
		rec := httptest.NewRecorder()
		u.handleUpload(rec, req)
		return rec
	}

	// 1. Cumulative total size limit: 2 chunks of 16 bytes = 32 bytes > maxTotalBytes (24) -> 413
	if r0 := send("totcap", 0, 2, "0", "0123456789ABCDEF"); r0.Code != http.StatusOK {
		t.Fatalf("chunk 0 failed: %d %s", r0.Code, r0.Body.String())
	}
	if r1 := send("totcap", 1, 2, "16", "012345678"); r1.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 when cumulative chunked size exceeds maxTotalBytes, got %d: %s", r1.Code, r1.Body.String())
	}
	// Retrying chunk 1 within remaining budget (8 bytes -> total 24 bytes) must succeed
	if r1ok := send("totcap", 1, 2, "16", "01234567"); r1ok.Code != http.StatusOK {
		t.Fatalf("chunk 1 within cumulative budget failed: %d %s", r1ok.Code, r1ok.Body.String())
	}

	// 2. Evicting incomplete sessions must also delete their on-disk .agy_part_* file AND block skipped-chunk finalization
	if rA := send("evict-a", 0, 3, "0", "AAAA"); rA.Code != http.StatusOK {
		t.Fatalf("evict-a chunk 0 failed: %d", rA.Code)
	}
	partAPath := filepath.Join(tmpDir, UploadDirName, "convo-sec", partFilePrefix+"evict-a")
	if _, err := os.Stat(partAPath); err != nil {
		t.Fatalf("expected partA on disk: %v", err)
	}
	// Start two newer sessions with maxChunkStatesLimit=2 -> evict-a is evicted
	if rB := send("evict-b", 0, 3, "0", "BBBB"); rB.Code != http.StatusOK {
		t.Fatalf("evict-b chunk 0 failed: %d", rB.Code)
	}
	if rC := send("evict-c", 0, 3, "0", "CCCC"); rC.Code != http.StatusOK {
		t.Fatalf("evict-c chunk 0 failed: %d", rC.Code)
	}
	if _, err := os.Stat(partAPath); !os.IsNotExist(err) {
		t.Fatalf("expected evicted incomplete session part file %s to be deleted from disk, err=%v", partAPath, err)
	}
	// Attempting to finalize evict-a at chunk 2 (skipping chunk 1 after state eviction, even if a stray part file existed) must fail 400
	_ = os.WriteFile(partAPath, []byte("AAAA"), 0o644)
	if rSkip := send("evict-a", 2, 3, "", "ZZZZ"); rSkip.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when resuming chunk > 0 without active session state, got %d: %s", rSkip.Code, rSkip.Body.String())
	}

	// 3. Truncated/shrunk part file below expectedOffset must be rejected rather than creating a sparse hole
	partCPath := filepath.Join(tmpDir, UploadDirName, "convo-sec", partFilePrefix+"evict-c")
	if err := os.Truncate(partCPath, 1); err != nil {
		t.Fatal(err)
	}
	if rShrink := send("evict-c", 1, 3, "4", "DDDD"); rShrink.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when part file shrunk below expectedOffset, got %d: %s", rShrink.Code, rShrink.Body.String())
	}
}

func TestSanitizeFileNameCrossPlatformPermissionsAndSameOrigin(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`C:\Users\alice\Documents\report.pdf`, "report.pdf"},
		{`/home/alice/notes/spec.md`, "spec.md"},
		{`bad:name*with?chars"<>|.txt`, "bad_name_with_chars____.txt"},
		{"line\nbreak\x00ctrl.bin", "line_break_ctrl.bin"},
		{"CON.txt", "_CON.txt"},
		{"lpt1", "_lpt1"},
		{"trailing. . ", "trailing"},
	}
	for _, tc := range cases {
		if got := sanitizeFileName(tc.in); got != tc.want {
			t.Errorf("sanitizeFileName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	tmpDir := t.TempDir()
	projDir := filepath.Join(tmpDir, "proj")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	up := New(Options{WorkspaceRoot: tmpDir}).WithSameOrigin(nil, "https://agy.example.com")
	mux := http.NewServeMux()
	up.Register(mux)

	// Cross-origin POST and HTTPS->HTTP public domain downgrade must be blocked, while plaintext loopback/LAN access still works
	crossReq := httptest.NewRequest(http.MethodPost, UploadAPIPath, strings.NewReader(""))
	crossReq.Host = "agy.example.com"
	crossReq.Header.Set("Origin", "https://attacker.example")
	crossRec := httptest.NewRecorder()
	mux.ServeHTTP(crossRec, crossReq)
	if crossRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-origin upload, got %d", crossRec.Code)
	}

	downgradeReq := httptest.NewRequest(http.MethodPost, UploadAPIPath, strings.NewReader(""))
	downgradeReq.Host = "agy.example.com"
	downgradeReq.Header.Set("Origin", "http://agy.example.com")
	downgradeRec := httptest.NewRecorder()
	mux.ServeHTTP(downgradeRec, downgradeReq)
	if downgradeRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for plaintext downgrade on HTTPS PublicURL host, got %d", downgradeRec.Code)
	}

	// Single upload and chunked upload must produce uniform file permissions and slash-normalized RelativePath
	buildBody := func(fields map[string]string, fname, data string) (*bytes.Buffer, string) {
		var b bytes.Buffer
		mw := multipart.NewWriter(&b)
		for k, v := range fields {
			_ = mw.WriteField(k, v)
		}
		fw, _ := mw.CreateFormFile("file", fname)
		_, _ = fw.Write([]byte(data))
		_ = mw.Close()
		return &b, mw.FormDataContentType()
	}

	bSingle, ctSingle := buildBody(map[string]string{"conversationId": "c1", "projectPath": projDir}, `C:\Users\alice\single:1.txt`, "single")
	reqSingle := httptest.NewRequest(http.MethodPost, UploadAPIPath, bSingle)
	reqSingle.Host = "127.0.0.1:8765"
	reqSingle.Header.Set("Origin", "http://127.0.0.1:8765")
	reqSingle.Header.Set("Sec-Fetch-Site", "same-origin")
	reqSingle.Header.Set("Content-Type", ctSingle)
	recSingle := httptest.NewRecorder()
	mux.ServeHTTP(recSingle, reqSingle)
	if recSingle.Code != http.StatusOK {
		t.Fatalf("single upload failed: %d %s", recSingle.Code, recSingle.Body.String())
	}
	var respSingle Response
	_ = json.Unmarshal(recSingle.Body.Bytes(), &respSingle)
	if respSingle.FileName != "single_1.txt" || respSingle.RelativePath != ".agy-uploads/c1/single_1.txt" {
		t.Fatalf("unexpected single upload paths: %+v", respSingle)
	}

	for idx, chunk := range []string{"ch0-", "ch1"} {
		bChunk, ctChunk := buildBody(map[string]string{
			"conversationId": "c1",
			"projectPath":    projDir,
			"uploadId":       "permcheck",
			"chunkIndex":     strconv.Itoa(idx),
			"totalChunks":    "2",
			"fileName":       `C:\Users\alice\chunked*2.txt`,
		}, "blob", chunk)
		reqChunk := httptest.NewRequest(http.MethodPost, UploadAPIPath, bChunk)
		reqChunk.Header.Set("Content-Type", ctChunk)
		recChunk := httptest.NewRecorder()
		mux.ServeHTTP(recChunk, reqChunk)
		if recChunk.Code != http.StatusOK {
			t.Fatalf("chunk %d failed: %d %s", idx, recChunk.Code, recChunk.Body.String())
		}
		if idx == 1 {
			var respChunk Response
			_ = json.Unmarshal(recChunk.Body.Bytes(), &respChunk)
			if respChunk.FileName != "chunked_2.txt" || respChunk.RelativePath != ".agy-uploads/c1/chunked_2.txt" {
				t.Fatalf("unexpected chunk upload paths: %+v", respChunk)
			}
			if runtime.GOOS != "windows" {
				stSingle, err1 := os.Stat(respSingle.FullPath)
				stChunk, err2 := os.Stat(respChunk.FullPath)
				if err1 != nil || err2 != nil {
					t.Fatalf("stat failed: %v / %v", err1, err2)
				}
				if stSingle.Mode().Perm() != stChunk.Mode().Perm() {
					t.Fatalf("mismatched file permissions: single=%o chunk=%o", stSingle.Mode().Perm(), stChunk.Mode().Perm())
				}
			}
		}
	}

	// file:// URI project path normalization must preserve drives, ~, UNC, and unencoded # / ?
	if got := normalizeProjectPath("File://C:/Users/alice/c#proj?x=1"); got != "C:/Users/alice/c#proj?x=1" {
		t.Fatalf("normalizeProjectPath 2-slash Windows URI = %q", got)
	}
	if got := normalizeProjectPath("file:///home/alice/my%20c#proj"); got != "/home/alice/my c#proj" {
		t.Fatalf("normalizeProjectPath fragment/percent URI = %q", got)
	}
	if got := normalizeProjectPath("file://server/share/proj"); got != "//server/share/proj" {
		t.Fatalf("normalizeProjectPath UNC URI = %q", got)
	}

	// Symlinked WorkspaceRoot must canonicalize partLocks and chunkStates keys across writeChunk and cleaner
	if runtime.GOOS != "windows" {
		realWS := t.TempDir()
		linkParent := t.TempDir()
		symlinkWS := filepath.Join(linkParent, "ws-link")
		if err := os.Symlink(realWS, symlinkWS); err == nil {
			symUp := New(Options{WorkspaceRoot: symlinkWS})
			symMux := http.NewServeMux()
			symUp.Register(symMux)

			b0, ct0 := buildBody(map[string]string{
				"conversationId": "symc",
				"uploadId":       "symu",
				"chunkIndex":     "0",
				"totalChunks":    "2",
				"fileName":       "sym.bin",
			}, "blob", "part0")
			r0 := httptest.NewRequest(http.MethodPost, UploadAPIPath, b0)
			r0.Header.Set("Content-Type", ct0)
			rec0 := httptest.NewRecorder()
			symMux.ServeHTTP(rec0, r0)
			if rec0.Code != http.StatusOK {
				t.Fatalf("symlinked workspace chunk 0 failed: %d %s", rec0.Code, rec0.Body.String())
			}

			realPart := filepath.Join(realWS, UploadDirName, "symc", partFilePrefix+"symu")
			expired := time.Now().Add(-3 * time.Hour)
			_ = os.Chtimes(realPart, expired, expired)
			symUp.cleanupOldFiles()

			symUp.mu.Lock()
			remStates := len(symUp.chunkStates)
			symUp.mu.Unlock()
			if remStates != 0 {
				t.Fatalf("expected cleaner to evict chunkStates entry via canonical key on symlinked WorkspaceRoot, remaining=%d", remStates)
			}
		}

		// Pre-planted leaf symlink (.agy_part_<id> -> external file) must NEVER overwrite or delete the external target
		outsideDir := t.TempDir()
		outsideSecret := filepath.Join(outsideDir, "credentials.json")
		const secretContent = `{"secret":"do-not-touch"}`
		if err := os.WriteFile(outsideSecret, []byte(secretContent), 0o600); err != nil {
			t.Fatal(err)
		}
		leafDir := filepath.Join(projDir, UploadDirName, "c1")
		if err := os.MkdirAll(leafDir, 0o755); err != nil {
			t.Fatal(err)
		}
		leafPartSymlink := filepath.Join(leafDir, partFilePrefix+"pwn")
		if err := os.Symlink(outsideSecret, leafPartSymlink); err == nil {
			for _, ci := range []string{"0", "1"} {
				bp, ctp := buildBody(map[string]string{
					"conversationId": "c1",
					"projectPath":    projDir,
					"uploadId":       "pwn",
					"chunkIndex":     ci,
					"totalChunks":    "2",
					"fileName":       "evil.bin",
				}, "blob", "attacker-bytes")
				rp := httptest.NewRequest(http.MethodPost, UploadAPIPath, bp)
				rp.Header.Set("Content-Type", ctp)
				recP := httptest.NewRecorder()
				mux.ServeHTTP(recP, rp)
				if recP.Code != http.StatusBadRequest {
					t.Fatalf("expected 400 rejecting symlinked part file on chunk %s, got %d: %s", ci, recP.Code, recP.Body.String())
				}
			}

			delReq := httptest.NewRequest(http.MethodDelete, UploadAPIPath+"?conversationId=c1&projectPath="+projDir+"&uploadId=pwn", nil)
			delRec := httptest.NewRecorder()
			mux.ServeHTTP(delRec, delReq)
			if delRec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 rejecting symlinked part file on DELETE abort, got %d: %s", delRec.Code, delRec.Body.String())
			}

			afterSecret, readErr := os.ReadFile(outsideSecret)
			if readErr != nil {
				t.Fatalf("external symlink target was deleted! err=%v", readErr)
			}
			if string(afterSecret) != secretContent {
				t.Fatalf("external symlink target was modified: got %q, want %q", string(afterSecret), secretContent)
			}
		}
	}
}
