package rules

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/AFSlayer/antigravity-server/internal/auth"
	"github.com/AFSlayer/antigravity-server/internal/config"
)

const (
	// ReadAPIPath is the endpoint for reading rule file content.
	ReadAPIPath = "/__agy/api/rules/read"
	// SaveAPIPath is the endpoint for saving rule file content.
	SaveAPIPath = "/__agy/api/rules/save"
	// MaxRuleBytes caps the decoded rule file content accepted by ReadAPIPath and SaveAPIPath.
	MaxRuleBytes = 2 << 20
	// maxSaveRequestBytes accommodates JSON string escaping overhead for a MaxRuleBytes document.
	maxSaveRequestBytes = MaxRuleBytes*2 + (64 << 10)
)

// Options configures the rules manager.
type Options struct {
	WorkspaceRoot string
}

// Manager handles reading and safely saving rule files.
type Manager struct {
	workspaceRoot        string
	homeDir              string
	defaultWorkspaceRoot bool
	trusted              []*net.IPNet
	publicURL            string
}

// New creates a new Manager.
func New(opts Options) *Manager {
	home := config.HomeDir()
	wsRoot := opts.WorkspaceRoot
	isDefault := wsRoot == ""
	if isDefault && home != "" {
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
	return &Manager{
		workspaceRoot:        wsRoot,
		homeDir:              home,
		defaultWorkspaceRoot: isDefault,
	}
}

// WithSameOrigin configures reverse-proxy origin verification to match Authenticator.
func (m *Manager) WithSameOrigin(trusted []*net.IPNet, publicURL string) *Manager {
	m.trusted = trusted
	m.publicURL = publicURL
	return m
}

// Register mounts the rules endpoints on mux.
func (m *Manager) Register(mux *http.ServeMux) {
	mux.HandleFunc(ReadAPIPath, m.handleRead)
	mux.HandleFunc(SaveAPIPath, m.handleSave)
}

type readResponse struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type saveRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type saveResponse struct {
	OK    bool   `json:"ok"`
	Path  string `json:"path"`
	Bytes int    `json:"bytes"`
}

func (m *Manager) handleRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	targetPath := r.URL.Query().Get("path")
	if targetPath == "" {
		http.Error(w, "missing path query parameter", http.StatusBadRequest)
		return
	}

	resolved, err := m.resolveTargetFile(targetPath, true)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid path: %v", err), http.StatusForbidden)
		return
	}

	f, err := os.Open(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, readResponse{
				Path:    resolved,
				Content: "",
			})
			return
		}
		http.Error(w, fmt.Sprintf("failed to open file: %v", err), http.StatusInternalServerError)
		return
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to stat file: %v", err), http.StatusInternalServerError)
		return
	}
	if fi.IsDir() {
		http.Error(w, "target is a directory", http.StatusForbidden)
		return
	}
	if fi.Size() > MaxRuleBytes {
		http.Error(w, "rule file exceeds maximum allowed size", http.StatusRequestEntityTooLarge)
		return
	}

	content, err := io.ReadAll(io.LimitReader(f, MaxRuleBytes+1))
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to read file: %v", err), http.StatusInternalServerError)
		return
	}
	if int64(len(content)) > MaxRuleBytes {
		http.Error(w, "rule file exceeds maximum allowed size", http.StatusRequestEntityTooLarge)
		return
	}

	writeJSON(w, http.StatusOK, readResponse{
		Path:    resolved,
		Content: string(content),
	})
}

func (m *Manager) handleSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if ok, _ := auth.CheckSameOrigin(r, m.trusted, m.publicURL); !ok {
		http.Error(w, "cross-origin request forbidden", http.StatusForbidden)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxSaveRequestBytes)
	var req saveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "rule file exceeds maximum allowed size", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid json request", http.StatusBadRequest)
		return
	}
	if len(req.Content) > MaxRuleBytes {
		http.Error(w, "rule file exceeds maximum allowed size", http.StatusRequestEntityTooLarge)
		return
	}

	resolved, err := m.resolveTargetFile(req.Path, false)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid path: %v", err), http.StatusForbidden)
		return
	}
	if err := checkWritableCustomizationPath(resolved); err != nil {
		http.Error(w, fmt.Sprintf("invalid path: %v", err), http.StatusForbidden)
		return
	}

	filePerm := os.FileMode(0o600)
	if existingInfo, statErr := os.Lstat(resolved); statErr == nil && existingInfo.Mode().IsRegular() {
		filePerm = existingInfo.Mode().Perm()
	}

	dir := filepath.Dir(resolved)
	dirPerm := os.FileMode(0o755)
	if m.homeDir != "" {
		cleanHome := filepath.Clean(m.homeDir)
		for _, secretRoot := range []string{filepath.Join(cleanHome, ".gemini"), filepath.Join(cleanHome, ".agents")} {
			if dir == secretRoot || strings.HasPrefix(dir, secretRoot+string(filepath.Separator)) {
				dirPerm = 0o700
				break
			}
		}
	}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		http.Error(w, fmt.Sprintf("failed to create directory: %v", err), http.StatusInternalServerError)
		return
	}

	// Create secure, unpredictable temporary file in same directory (O_EXCL | O_CREATE | 0600)
	tmpFile, err := os.CreateTemp(dir, "rule-*.tmp")
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to create temp file: %v", err), http.StatusInternalServerError)
		return
	}
	tmpName := tmpFile.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()

	if _, err := tmpFile.WriteString(req.Content); err != nil {
		_ = tmpFile.Close()
		http.Error(w, fmt.Sprintf("failed to write rule file: %v", err), http.StatusInternalServerError)
		return
	}

	if err := tmpFile.Chmod(filePerm); err != nil {
		_ = tmpFile.Close()
		http.Error(w, fmt.Sprintf("failed to set rule file permissions: %v", err), http.StatusInternalServerError)
		return
	}

	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		http.Error(w, fmt.Sprintf("failed to sync rule file: %v", err), http.StatusInternalServerError)
		return
	}

	if err := tmpFile.Close(); err != nil {
		http.Error(w, fmt.Sprintf("failed to close temp file: %v", err), http.StatusInternalServerError)
		return
	}

	if err := os.Rename(tmpName, resolved); err != nil {
		http.Error(w, fmt.Sprintf("failed to atomically update rule file: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, saveResponse{
		OK:    true,
		Path:  resolved,
		Bytes: len(req.Content),
	})
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
		// file://~/.gemini/...
	case rest != "":
		rest = "//" + rest
	}
	if len(rest) >= 3 && rest[0] == '/' && ((rest[1] >= 'a' && rest[1] <= 'z') || (rest[1] >= 'A' && rest[1] <= 'Z')) && (rest[2] == ':' || rest[2] == '|') {
		rest = string(rest[1]) + ":" + rest[3:]
	}
	return rest
}

func normalizeRulePath(inputPath, homeDir string) (string, error) {
	p := stripFileURI(inputPath)
	if p == "" {
		return "", errors.New("path cannot be empty")
	}
	if len(p) >= 3 && p[0] == '/' && ((p[1] >= 'a' && p[1] <= 'z') || (p[1] >= 'A' && p[1] <= 'Z')) && p[2] == ':' {
		p = p[1:]
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) || p == "~" {
		if homeDir == "" {
			return "", errors.New("user home directory not found")
		}
		p = filepath.Join(homeDir, p[1:])
	}
	return filepath.Clean(p), nil
}

func containsForbiddenDirSegment(p string) bool {
	parts := strings.FieldsFunc(p, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	for _, seg := range parts {
		if strings.EqualFold(seg, ".git") || strings.EqualFold(seg, ".agy-uploads") {
			return true
		}
	}
	return false
}

func isReadOnlyCustomizationCategory(seg string) bool {
	switch strings.ToLower(seg) {
	case "builtin", "plugins", "agents":
		return true
	default:
		return false
	}
}

func isReadOnlyCustomizationPath(p string) bool {
	parts := strings.FieldsFunc(p, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	if len(parts) < 3 {
		return false
	}
	dirParts := parts[:len(parts)-1]
	for i := 0; i < len(dirParts); i++ {
		lower := strings.ToLower(dirParts[i])
		if lower != ".gemini" && lower != ".agents" && lower != ".agent" {
			continue
		}
		if i+1 < len(dirParts) {
			next := strings.ToLower(dirParts[i+1])
			if isReadOnlyCustomizationCategory(next) {
				return true
			}
			if (next == "config" || next == "antigravity") && i+2 < len(dirParts) && isReadOnlyCustomizationCategory(dirParts[i+2]) {
				return true
			}
		}
	}
	return false
}

func checkWritableCustomizationPath(resolved string) error {
	if isReadOnlyCustomizationPath(resolved) {
		return errors.New("built-in, plugin, and managed agent customizations are read-only")
	}
	if realTarget, err := evalPathWithMissingLeaf(resolved); err == nil && isReadOnlyCustomizationPath(realTarget) {
		return errors.New("built-in, plugin, and managed agent customizations are read-only")
	}
	return nil
}

func isAllowedRuleFileName(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	switch base {
	case ".cursorrules", ".windsurfrules", ".clinerules", "gemini.md", "skill.md", "claude.md", "agents.md":
		return true
	}
	ext := strings.ToLower(filepath.Ext(base))
	switch ext {
	case ".md", ".mdc", ".markdown", ".rules":
		return true
	}
	return false
}

func evalPathWithMissingLeaf(p string) (string, error) {
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

func isWithinOrEqual(target, root string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (m *Manager) allowedRoots() []string {
	var allowed []string
	if m.homeDir != "" {
		allowed = append(allowed,
			filepath.Join(m.homeDir, ".gemini"),
			filepath.Join(m.homeDir, ".agents"),
			filepath.Join(m.homeDir, ".agent"),
		)
	}
	if m.workspaceRoot != "" {
		allowed = append(allowed, m.workspaceRoot)
	}
	if m.defaultWorkspaceRoot && m.homeDir != "" {
		for _, candidate := range []string{
			filepath.Join(m.homeDir, "antigravity", "workspace"),
			filepath.Join(m.homeDir, "workspace"),
		} {
			if candidate != m.workspaceRoot {
				if info, err := os.Stat(candidate); err == nil && info.IsDir() {
					allowed = append(allowed, candidate)
				}
			}
		}
	}
	return allowed
}

func (m *Manager) validatePath(inputPath string) (string, error) {
	cleaned, err := normalizeRulePath(inputPath, m.homeDir)
	if err != nil {
		return "", err
	}
	if containsForbiddenDirSegment(cleaned) {
		return "", errors.New("access to .git or .agy-uploads directory is forbidden")
	}

	allowed := m.allowedRoots()
	realTarget, err := evalPathWithMissingLeaf(cleaned)
	if err != nil {
		return "", fmt.Errorf("invalid path: %v", err)
	}
	if containsForbiddenDirSegment(realTarget) {
		return "", errors.New("access to .git or .agy-uploads directory is forbidden")
	}

	cleanedAllowed := false
	realAllowed := false
	for _, root := range allowed {
		cleanRoot := filepath.Clean(root)
		if isWithinOrEqual(cleaned, cleanRoot) {
			cleanedAllowed = true
		}
		realRoot, err := evalPathWithMissingLeaf(cleanRoot)
		if err != nil {
			continue
		}
		if isWithinOrEqual(realTarget, realRoot) {
			realAllowed = true
		}
	}

	if !cleanedAllowed || !realAllowed {
		return "", fmt.Errorf("path %s is outside allowed directories (%s)", inputPath, strings.Join(allowed, ", "))
	}

	return cleaned, nil
}

func (m *Manager) resolveTargetFile(inputPath string, allowSymlinkLeaf bool) (string, error) {
	resolved, err := m.validatePath(inputPath)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(resolved)
	if err == nil && fi.IsDir() {
		skillFile := filepath.Join(resolved, "SKILL.md")
		if _, err := os.Stat(skillFile); err == nil {
			resolved = skillFile
		} else {
			ruleFile := filepath.Join(resolved, "GEMINI.md")
			if _, err := os.Stat(ruleFile); err == nil {
				resolved = ruleFile
			} else {
				resolved = skillFile
			}
		}
		if _, err := m.validatePath(resolved); err != nil {
			return "", err
		}
	}
	if lfi, lerr := os.Lstat(resolved); lerr == nil && lfi.Mode()&os.ModeSymlink != 0 {
		if !allowSymlinkLeaf {
			return "", errors.New("rule target file cannot be a symlink")
		}
		realTarget, evalErr := evalPathWithMissingLeaf(resolved)
		if evalErr != nil || !isAllowedRuleFileName(realTarget) {
			return "", errors.New("symlink target is not a valid rule file")
		}
	}
	if !isAllowedRuleFileName(resolved) {
		return "", fmt.Errorf("unsupported rule file type: %s", filepath.Base(resolved))
	}
	return resolved, nil
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
