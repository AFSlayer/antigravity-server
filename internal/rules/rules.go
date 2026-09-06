package rules

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	// ReadAPIPath is the endpoint for reading rule file content.
	ReadAPIPath = "/__agy/api/rules/read"
	// SaveAPIPath is the endpoint for saving rule file content.
	SaveAPIPath = "/__agy/api/rules/save"
)

// Options configures the rules manager.
type Options struct {
	WorkspaceRoot string
}

// Manager handles reading and safely saving rule files.
type Manager struct {
	workspaceRoot string
	homeDir       string
}

// New creates a new Manager.
func New(opts Options) *Manager {
	home, _ := os.UserHomeDir()
	return &Manager{
		workspaceRoot: opts.WorkspaceRoot,
		homeDir:       home,
	}
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

	resolved, err := m.resolveTargetFile(targetPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid path: %v", err), http.StatusForbidden)
		return
	}

	content, err := os.ReadFile(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, readResponse{
				Path:    resolved,
				Content: "",
			})
			return
		}
		http.Error(w, fmt.Sprintf("failed to read file: %v", err), http.StatusInternalServerError)
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

	var req saveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json request", http.StatusBadRequest)
		return
	}

	resolved, err := m.resolveTargetFile(req.Path)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid path: %v", err), http.StatusForbidden)
		return
	}

	dir := filepath.Dir(resolved)
	if err := os.MkdirAll(dir, 0755); err != nil {
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

	if err := tmpFile.Chmod(0644); err != nil {
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

func (m *Manager) validatePath(inputPath string) (string, error) {
	if inputPath == "" {
		return "", errors.New("path cannot be empty")
	}

	if strings.HasPrefix(inputPath, "~/") || inputPath == "~" {
		if m.homeDir == "" {
			return "", errors.New("user home directory not found")
		}
		inputPath = filepath.Join(m.homeDir, strings.TrimPrefix(inputPath, "~"))
	}

	cleaned := filepath.Clean(inputPath)

	var allowed []string
	if m.homeDir != "" {
		allowed = append(allowed, filepath.Join(m.homeDir, ".gemini"))
	}
	if m.workspaceRoot != "" {
		allowed = append(allowed, m.workspaceRoot)
	}

	isAllowed := false
	for _, root := range allowed {
		cleanRoot := filepath.Clean(root)
		if cleaned == cleanRoot || strings.HasPrefix(cleaned, cleanRoot+string(filepath.Separator)) {
			isAllowed = true
			break
		}
	}

	if !isAllowed {
		return "", fmt.Errorf("path %s is outside allowed directories (%s)", inputPath, strings.Join(allowed, ", "))
	}

	return cleaned, nil
}

func (m *Manager) resolveTargetFile(inputPath string) (string, error) {
	resolved, err := m.validatePath(inputPath)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(resolved)
	if err == nil && fi.IsDir() {
		skillFile := filepath.Join(resolved, "SKILL.md")
		if _, err := os.Stat(skillFile); err == nil {
			return skillFile, nil
		}
		ruleFile := filepath.Join(resolved, "GEMINI.md")
		if _, err := os.Stat(ruleFile); err == nil {
			return ruleFile, nil
		}
		return skillFile, nil
	}
	return resolved, nil
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
