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

	// 5. Test ~/.agents/skills global skill directory
	agentsSkillDir := filepath.Join(tempDir, ".agents", "skills", "global-skill")
	if err := os.MkdirAll(agentsSkillDir, 0755); err != nil {
		t.Fatal(err)
	}
	saveAgentsSkillBody, _ := json.Marshal(saveRequest{
		Path:    agentsSkillDir,
		Content: "# Global Agent Skill\n",
	})
	reqAgentsSave := httptest.NewRequest(http.MethodPost, SaveAPIPath, bytes.NewReader(saveAgentsSkillBody))
	recAgentsSave := httptest.NewRecorder()
	mux.ServeHTTP(recAgentsSave, reqAgentsSave)
	if recAgentsSave.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on ~/.agents/skills save, got %d: %s", recAgentsSave.Code, recAgentsSave.Body.String())
	}

	// 6. Test Path Traversal Protection
	badPath := filepath.Join(tempDir, "..", "..", "etc", "passwd")
	reqBad := httptest.NewRequest(http.MethodGet, ReadAPIPath+"?path="+badPath, nil)
	recBad := httptest.NewRecorder()
	mux.ServeHTTP(recBad, reqBad)

	if recBad.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden on path traversal, got %d", recBad.Code)
	}

	// 7. Block symlink escape, .git directory, and executable script extensions
	outsideDir := t.TempDir()
	secretMd := filepath.Join(outsideDir, "SECRET.md")
	_ = os.WriteFile(secretMd, []byte("top secret"), 0644)
	symDir := filepath.Join(workspaceRoot, "linked-outside")
	if err := os.Symlink(outsideDir, symDir); err == nil {
		reqSym := httptest.NewRequest(http.MethodGet, ReadAPIPath+"?path="+filepath.Join(symDir, "SECRET.md"), nil)
		recSym := httptest.NewRecorder()
		mux.ServeHTTP(recSym, reqSym)
		if recSym.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden on symlink escape, got %d", recSym.Code)
		}
	}

	gitHookBody, _ := json.Marshal(saveRequest{
		Path:    filepath.Join(workspaceRoot, "proj", ".git", "hooks", "pre-commit.md"),
		Content: "#!/bin/sh\n",
	})
	reqGit := httptest.NewRequest(http.MethodPost, SaveAPIPath, bytes.NewReader(gitHookBody))
	recGit := httptest.NewRecorder()
	mux.ServeHTTP(recGit, reqGit)
	if recGit.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden on .git write, got %d", recGit.Code)
	}

	uploadsBody, _ := json.Marshal(saveRequest{
		Path:    filepath.Join(workspaceRoot, "proj", ".agy-uploads", "shared", "SKILL.md"),
		Content: "# Uploaded\n",
	})
	reqUploads := httptest.NewRequest(http.MethodPost, SaveAPIPath, bytes.NewReader(uploadsBody))
	recUploads := httptest.NewRecorder()
	mux.ServeHTTP(recUploads, reqUploads)
	if recUploads.Code != http.StatusForbidden || !bytes.Contains(recUploads.Body.Bytes(), []byte(".agy-uploads")) {
		t.Errorf("expected 403 Forbidden mentioning .agy-uploads, got %d: %s", recUploads.Code, recUploads.Body.String())
	}

	execBody, _ := json.Marshal(saveRequest{
		Path:    filepath.Join(workspaceRoot, "proj", "evil.sh"),
		Content: "#!/bin/sh\n",
	})
	reqExec := httptest.NewRequest(http.MethodPost, SaveAPIPath, bytes.NewReader(execBody))
	recExec := httptest.NewRecorder()
	mux.ServeHTTP(recExec, reqExec)
	if recExec.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden on non-rule extension .sh, got %d", recExec.Code)
	}

	// 8. Block oversized reads (413), allow exact MaxRuleBytes saves even with JSON escaping overhead,
	// block > MaxRuleBytes decoded saves (413), arbitrary .txt files, and read-only builtin/plugin writes
	largeMd := filepath.Join(workspaceRoot, "huge.md")
	if err := os.WriteFile(largeMd, bytes.Repeat([]byte("a"), MaxRuleBytes+128), 0644); err != nil {
		t.Fatal(err)
	}
	reqHuge := httptest.NewRequest(http.MethodGet, ReadAPIPath+"?path="+largeMd, nil)
	recHuge := httptest.NewRecorder()
	mux.ServeHTTP(recHuge, reqHuge)
	if recHuge.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413 Request Entity Too Large on oversized read, got %d", recHuge.Code)
	}

	exactMaxMd := filepath.Join(workspaceRoot, "exact-max.md")
	exactContent := string(bytes.Repeat([]byte("\"\n"), MaxRuleBytes/2))
	exactSaveBody, _ := json.Marshal(saveRequest{Path: exactMaxMd, Content: exactContent})
	reqExactSave := httptest.NewRequest(http.MethodPost, SaveAPIPath, bytes.NewReader(exactSaveBody))
	recExactSave := httptest.NewRecorder()
	mux.ServeHTTP(recExactSave, reqExactSave)
	if recExactSave.Code != http.StatusOK {
		t.Errorf("expected 200 OK saving MaxRuleBytes markdown with JSON escaping overhead, got %d: %s", recExactSave.Code, recExactSave.Body.String())
	}

	overSaveBody, _ := json.Marshal(saveRequest{Path: exactMaxMd, Content: exactContent + "x"})
	reqOverSave := httptest.NewRequest(http.MethodPost, SaveAPIPath, bytes.NewReader(overSaveBody))
	recOverSave := httptest.NewRecorder()
	mux.ServeHTTP(recOverSave, reqOverSave)
	if recOverSave.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413 when decoded rule content exceeds MaxRuleBytes, got %d", recOverSave.Code)
	}

	txtFile := filepath.Join(workspaceRoot, "notes.txt")
	_ = os.WriteFile(txtFile, []byte("plain data"), 0644)
	reqTxt := httptest.NewRequest(http.MethodGet, ReadAPIPath+"?path="+txtFile, nil)
	recTxt := httptest.NewRecorder()
	mux.ServeHTTP(recTxt, reqTxt)
	if recTxt.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for arbitrary .txt file, got %d", recTxt.Code)
	}

	for _, roPath := range []string{
		filepath.Join(tempDir, ".gemini", "antigravity", "builtin", "skills", "core", "SKILL.md"),
		filepath.Join(tempDir, ".gemini", "config", "plugins", "figma", "skills", "use", "SKILL.md"),
		filepath.Join(tempDir, ".gemini", "agents", "reviewer.md"),
	} {
		roBody, _ := json.Marshal(saveRequest{Path: roPath, Content: "# Overwrite\n"})
		reqRO := httptest.NewRequest(http.MethodPost, SaveAPIPath, bytes.NewReader(roBody))
		recRO := httptest.NewRecorder()
		mux.ServeHTTP(recRO, reqRO)
		if recRO.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden saving read-only customization %s, got %d", roPath, recRO.Code)
		}
	}

	// User-authored files under skills/.../agents/ or project .gemini/rules/agents/ must remain editable
	for _, userWritable := range []string{
		filepath.Join(tempDir, ".gemini", "skills", "my-skill", "agents", "guide.md"),
		filepath.Join(workspaceRoot, "proj", ".gemini", "rules", "agents", "coding.md"),
	} {
		wBody, _ := json.Marshal(saveRequest{Path: userWritable, Content: "# User Guide\n"})
		reqW := httptest.NewRequest(http.MethodPost, SaveAPIPath, bytes.NewReader(wBody))
		recW := httptest.NewRecorder()
		mux.ServeHTTP(recW, reqW)
		if recW.Code != http.StatusOK {
			t.Errorf("expected 200 OK saving user customization %s, got %d: %s", userWritable, recW.Code, recW.Body.String())
		}
	}

	// Symlinked rule inside allowed roots (e.g., ~/.gemini/LINKED.md -> workspace/dotfiles/GEMINI.md)
	// should be readable, while symlink overwriting via SaveAPIPath remains forbidden.
	dotfilesRule := filepath.Join(workspaceRoot, "dotfiles", "GEMINI.md")
	if err := os.MkdirAll(filepath.Dir(dotfilesRule), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dotfilesRule, []byte("# Dotfiles Rule\n"), 0644); err != nil {
		t.Fatal(err)
	}
	linkedRule := filepath.Join(tempDir, ".gemini", "LINKED.md")
	if err := os.Symlink(dotfilesRule, linkedRule); err == nil {
		reqLinkRead := httptest.NewRequest(http.MethodGet, ReadAPIPath+"?path="+linkedRule, nil)
		recLinkRead := httptest.NewRecorder()
		mux.ServeHTTP(recLinkRead, reqLinkRead)
		if recLinkRead.Code != http.StatusOK {
			t.Errorf("expected 200 OK reading symlinked rule within allowed roots, got %d: %s", recLinkRead.Code, recLinkRead.Body.String())
		}
		saveLinkBody, _ := json.Marshal(saveRequest{Path: linkedRule, Content: "# Overwrite Symlink\n"})
		reqLinkSave := httptest.NewRequest(http.MethodPost, SaveAPIPath, bytes.NewReader(saveLinkBody))
		recLinkSave := httptest.NewRecorder()
		mux.ServeHTTP(recLinkSave, reqLinkSave)
		if recLinkSave.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden saving directly over symlink leaf, got %d", recLinkSave.Code)
		}
	}

	// Symlink pointing to a non-rule file inside workspace (e.g., .env) must be blocked even on read
	dotEnv := filepath.Join(workspaceRoot, "proj", ".env")
	_ = os.MkdirAll(filepath.Dir(dotEnv), 0755)
	_ = os.WriteFile(dotEnv, []byte("SECRET=1"), 0600)
	envLink := filepath.Join(tempDir, ".gemini", "SNEAKY.md")
	if err := os.Symlink(dotEnv, envLink); err == nil {
		reqSneaky := httptest.NewRequest(http.MethodGet, ReadAPIPath+"?path="+envLink, nil)
		recSneaky := httptest.NewRecorder()
		mux.ServeHTTP(recSneaky, reqSneaky)
		if recSneaky.Code != http.StatusForbidden {
			t.Errorf("expected 403 Forbidden reading symlink targeting non-rule .env, got %d", recSneaky.Code)
		}
	}

	// 9. Default workspaceRoot fallback when Options.WorkspaceRoot is empty
	defaultMgr := New(Options{})
	if defaultMgr.homeDir != "" && defaultMgr.workspaceRoot == "" {
		t.Errorf("expected non-empty default workspaceRoot when homeDir exists")
	}

	// 10. Handler-level same-origin enforcement rejects cross-origin POSTs even if mounted directly
	csrfBody, _ := json.Marshal(saveRequest{Path: rulePath, Content: "# CSRF\n"})
	reqCSRF := httptest.NewRequest(http.MethodPost, SaveAPIPath, bytes.NewReader(csrfBody))
	reqCSRF.Host = "127.0.0.1:8787"
	reqCSRF.Header.Set("Origin", "https://evil.example")
	recCSRF := httptest.NewRecorder()
	mux.ServeHTTP(recCSRF, reqCSRF)
	if recCSRF.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for cross-origin save request, got %d", recCSRF.Code)
	}

	// 11. file:// URI edge cases: mixed case, 2-slash Windows drive, tilde, UNC, and unencoded # / ?
	uriCases := []struct {
		in   string
		want string
	}{
		{"file:///home/alice/c#proj?v=1/GEMINI.md", filepath.Clean("/home/alice/c#proj?v=1/GEMINI.md")},
		{"File:///home/alice/my%20c%23proj/GEMINI.md", filepath.Clean("/home/alice/my c#proj/GEMINI.md")},
		{"FILE://C:/Users/alice/.gemini/GEMINI.md", filepath.Clean("C:/Users/alice/.gemini/GEMINI.md")},
		{"file:///C:/Users/alice/.gemini/GEMINI.md", filepath.Clean("C:/Users/alice/.gemini/GEMINI.md")},
		{"file://~/.gemini/GEMINI.md", filepath.Join(tempDir, ".gemini", "GEMINI.md")},
		{"file://localhost/home/alice/.gemini/GEMINI.md", filepath.Clean("/home/alice/.gemini/GEMINI.md")},
		{"file://server/share/proj/GEMINI.md", filepath.Clean("//server/share/proj/GEMINI.md")},
	}
	for _, tc := range uriCases {
		got, err := normalizeRulePath(tc.in, tempDir)
		if err != nil {
			t.Fatalf("normalizeRulePath(%q) unexpected error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("normalizeRulePath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	// 12. Saving an existing 0600 rule file must preserve its restrictive file mode instead of widening to 0644
	privateRule := filepath.Join(tempDir, ".gemini", "PRIVATE.md")
	if err := os.WriteFile(privateRule, []byte("initial"), 0o600); err != nil {
		t.Fatal(err)
	}
	privBody, _ := json.Marshal(saveRequest{Path: privateRule, Content: "updated secret rule\n"})
	reqPriv := httptest.NewRequest(http.MethodPost, SaveAPIPath, bytes.NewReader(privBody))
	recPriv := httptest.NewRecorder()
	mux.ServeHTTP(recPriv, reqPriv)
	if recPriv.Code != http.StatusOK {
		t.Fatalf("expected 200 OK saving private rule, got %d: %s", recPriv.Code, recPriv.Body.String())
	}
	if st, err := os.Stat(privateRule); err != nil {
		t.Fatal(err)
	} else if st.Mode().Perm() != 0o600 {
		t.Errorf("expected saved file mode 0600 to be preserved, got %04o", st.Mode().Perm())
	}
}
