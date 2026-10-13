package upload

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
