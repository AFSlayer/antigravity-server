package rules

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestRulesReadAndSave(t *testing.T) {
	tempDir := t.TempDir()
	workspaceRoot := filepath.Join(tempDir, "workspace")
	if err := os.MkdirAll(workspaceRoot, 0755); err != nil {
		t.Fatal(err)
	}

	mgr := &Manager{
		workspaceRoot: workspaceRoot,
		homeDir:       tempDir,
	}

	mux := http.NewServeMux()
	mgr.Register(mux)

	// 1. Save global rule
	rulePath := filepath.Join(tempDir, ".gemini", "GEMINI.md")
	ruleContent := "# Global Rules\n\n- Rule 1\n- Rule 2\n"

	saveBody, _ := json.Marshal(saveRequest{
		Path:    rulePath,
		Content: ruleContent,
	})
	req := httptest.NewRequest(http.MethodPost, SaveAPIPath, bytes.NewReader(saveBody))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on save, got %d: %s", rec.Code, rec.Body.String())
	}

	// 2. Read global rule back
	reqRead := httptest.NewRequest(http.MethodGet, ReadAPIPath+"?path="+rulePath, nil)
	recRead := httptest.NewRecorder()
	mux.ServeHTTP(recRead, reqRead)

	if recRead.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on read, got %d: %s", recRead.Code, recRead.Body.String())
	}

	var readRes readResponse
	if err := json.NewDecoder(recRead.Body).Decode(&readRes); err != nil {
		t.Fatal(err)
	}

	if readRes.Content != ruleContent {
		t.Fatalf("content mismatch: got %q, want %q", readRes.Content, ruleContent)
	}

	// 3. Test Directory Resolution (Skills directory resolving to SKILL.md)
	skillDir := filepath.Join(tempDir, ".gemini", "config", "skills", "test-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	skillContent := "# Test Skill\n\nInstructions here.\n"

	saveSkillBody, _ := json.Marshal(saveRequest{
		Path:    skillDir, // passing directory path
		Content: skillContent,
	})
	reqSkillSave := httptest.NewRequest(http.MethodPost, SaveAPIPath, bytes.NewReader(saveSkillBody))
	recSkillSave := httptest.NewRecorder()
	mux.ServeHTTP(recSkillSave, reqSkillSave)

	if recSkillSave.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on skill save via dir, got %d: %s", recSkillSave.Code, recSkillSave.Body.String())
	}

	// Verify file actually created at skillDir/SKILL.md
	expectedFile := filepath.Join(skillDir, "SKILL.md")
	readBytes, err := os.ReadFile(expectedFile)
	if err != nil || string(readBytes) != skillContent {
		t.Fatalf("expected skill file content at %s, err: %v", expectedFile, err)
	}

	// Read back via dir path
	reqSkillRead := httptest.NewRequest(http.MethodGet, ReadAPIPath+"?path="+skillDir, nil)
	recSkillRead := httptest.NewRecorder()
	mux.ServeHTTP(recSkillRead, reqSkillRead)
	if recSkillRead.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on skill read via dir, got %d: %s", recSkillRead.Code, recSkillRead.Body.String())
	}

	// 4. Test Directory Resolution fallback (directory with only GEMINI.md)
	geminiDir := filepath.Join(tempDir, ".gemini", "config", "skills", "legacy-skill")
	if err := os.MkdirAll(geminiDir, 0755); err != nil {
		t.Fatal(err)
	}
	legacyContent := "# Legacy Skill\n\nFallback to GEMINI.md\n"
	if err := os.WriteFile(filepath.Join(geminiDir, "GEMINI.md"), []byte(legacyContent), 0644); err != nil {
		t.Fatal(err)
	}

	reqLegacyRead := httptest.NewRequest(http.MethodGet, ReadAPIPath+"?path="+geminiDir, nil)
	recLegacyRead := httptest.NewRecorder()
	mux.ServeHTTP(recLegacyRead, reqLegacyRead)
	if recLegacyRead.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on fallback read, got %d", recLegacyRead.Code)
	}
	var legacyRes readResponse
	if err := json.NewDecoder(recLegacyRead.Body).Decode(&legacyRes); err != nil {
		t.Fatal(err)
	}
	if legacyRes.Content != legacyContent {
		t.Fatalf("fallback content mismatch: got %q, want %q", legacyRes.Content, legacyContent)
	}

	// 5. Test Path Traversal Protection
	badPath := filepath.Join(tempDir, "..", "..", "etc", "passwd")
	reqBad := httptest.NewRequest(http.MethodGet, ReadAPIPath+"?path="+badPath, nil)
	recBad := httptest.NewRecorder()
	mux.ServeHTTP(recBad, reqBad)

	if recBad.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden on path traversal, got %d", recBad.Code)
	}
}
