package upload

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestUploadStreaming(t *testing.T) {
	tmpDir := t.TempDir()
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

	expectedPath := filepath.Join(tmpDir, "my-project", "uploads", "convo-123", "test.har")
	content, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if string(content) != string(sampleData) {
		t.Errorf("content = %q, want %q", string(content), string(sampleData))
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

	expectedPath := filepath.Join(tmpDir, "uploads", "convo-456", "sample.log")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Fatalf("file not created at expected path: %v", err)
	}
}

func TestCleaner(t *testing.T) {
	tmpDir := t.TempDir()
	u := New(Options{WorkspaceRoot: tmpDir, TTL: 10 * time.Millisecond})

	targetDir := filepath.Join(tmpDir, "proj", "uploads", "convo-789")
	_ = os.MkdirAll(targetDir, 0755)
	oldFile := filepath.Join(targetDir, "old.har")
	_ = os.WriteFile(oldFile, []byte("old content"), 0644)

	// artificially set mtime to the past
	past := time.Now().Add(-1 * time.Hour)
	_ = os.Chtimes(oldFile, past, past)

	u.cleanupOldFiles()

	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Errorf("old file was not cleaned up")
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

	// File must be strictly confined inside tmpDir/uploads/convo-eviletc/evil.sh
	expectedPath := filepath.Join(tmpDir, "uploads", "convo-eviletc", "evil.sh")
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

	first, err := os.ReadFile(filepath.Join(tmpDir, "uploads", "convo-dup", "image.png"))
	if err != nil {
		t.Fatalf("read first upload: %v", err)
	}
	if string(first) != "first image" {
		t.Errorf("first upload was overwritten: got %q", string(first))
	}

	second, err := os.ReadFile(filepath.Join(tmpDir, "uploads", "convo-dup", "image_2.png"))
	if err != nil {
		t.Fatalf("read second upload: %v", err)
	}
	if string(second) != "second image" {
		t.Errorf("second upload content = %q, want %q", string(second), "second image")
	}
}

func TestChunkedUploadReassemblyAndRetry(t *testing.T) {
	tmpDir := t.TempDir()
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
	wantRel := filepath.Join("uploads", "convo-chunk", "216.30.11.225.rom")
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
	stagingPath := filepath.Join(tmpDir, "firmware-proj", "uploads", "convo-chunk", partFilePrefix+"up-rom-1")
	if _, err := os.Stat(stagingPath); !os.IsNotExist(err) {
		t.Errorf("staging file still exists after finalization: %v", err)
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

	stagingPath := filepath.Join(tmpDir, "uploads", "convo-abort", partFilePrefix+"abort-id-99")
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
