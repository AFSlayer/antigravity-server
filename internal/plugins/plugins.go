// Package plugins keeps the JavaScript and CSS files a user adds under Settings > Appearance > UI Plugins, and serves
// the script that loads the enabled ones into the web app. They are files on this server: a plugin added from a path
// stays linked to that file, so editing it takes effect on the next reload, and a copy is kept in case it goes away.
// The plugins are the same for every browser signed in to this agy-server. A plugin runs with the same access as the
// web app itself, which already reaches the agent and its terminal, so adding one needs nothing more than being
// signed in.
package plugins

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/AFSlayer/antigravity-server/internal/auth"
	"github.com/AFSlayer/antigravity-server/internal/config"
)

const (
	ListAPIPath    = "/__agy/api/plugins"
	BrowseAPIPath  = "/__agy/api/plugins/browse"
	AddPathAPIPath = "/__agy/api/plugins/add-path"
	ToggleAPIPath  = "/__agy/api/plugins/toggle"
	DeleteAPIPath  = "/__agy/api/plugins/delete"
	// LoaderPath serves the settings section and the loader. It is the same for every page, so a browser keeps it.
	LoaderPath = "/__agy/plugins/loader.js"
	// FilePrefix serves one plugin file by name.
	FilePrefix = "/__agy/plugins/file/"
	// MaxPluginBytes bounds one plugin file.
	MaxPluginBytes = 5 << 20
	stateFile      = "state.json"
	// maxEntries bounds how many items of one folder the chooser lists.
	maxEntries = 2000
)

//go:embed loader.js
var loaderScript string

// loaderVersion changes with the loader, so a page asks for the new one after an update.
var loaderVersion = func() string {
	sum := sha256.Sum256([]byte(loaderScript))
	return hex.EncodeToString(sum[:6])
}()

// LoaderURL is the address the page loads the loader from.
var LoaderURL = LoaderPath + "?v=" + loaderVersion

// Plugin is one plugin as the settings section lists it.
type Plugin struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Size    int64  `json:"size"`
	Enabled bool   `json:"enabled"`
	Version string `json:"version"`
	// Source is the file on the server the plugin was added from and is read from.
	Source string `json:"source,omitempty"`
	// SourceMissing says the source could not be read, so the copy kept when it last could is used.
	SourceMissing bool `json:"sourceMissing,omitempty"`
}

type state struct {
	Enabled []string          `json:"enabled"`
	Sources map[string]string `json:"sources,omitempty"`
}

// Store is the plugin directory and the record of which plugins are on and where they came from.
type Store struct {
	dir       string
	workspace string
	mu        sync.Mutex
	trusted   []*net.IPNet
	publicURL string
}

// New returns a store kept in dir, which is created when the first plugin is added.
func New(dir string) *Store {
	return &Store{dir: dir}
}

// WithSameOrigin sets what requests are checked against, as the other settings APIs do.
func (s *Store) WithSameOrigin(trusted []*net.IPNet, publicURL string) *Store {
	s.trusted = trusted
	s.publicURL = publicURL
	return s
}

// WithWorkspace sets the workspace folder the file chooser offers as a shortcut.
func (s *Store) WithWorkspace(dir string) *Store {
	s.workspace = dir
	return s
}

// Register mounts the API, the loader and the plugin files. They sit behind the same sign-in as the web app.
func (s *Store) Register(mux *http.ServeMux) {
	mux.HandleFunc(ListAPIPath, s.handleList)
	mux.HandleFunc(BrowseAPIPath, s.handleBrowse)
	mux.HandleFunc(AddPathAPIPath, s.handleAddPath)
	mux.HandleFunc(ToggleAPIPath, s.handleToggle)
	mux.HandleFunc(DeleteAPIPath, s.handleDelete)
	mux.HandleFunc(LoaderPath, s.handleLoader)
	mux.HandleFunc(FilePrefix, s.handleFile)
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

// cleanName turns a file name into the name a plugin is kept under, and says what kind it is.
func cleanName(name string) (string, string, error) {
	name = filepath.Base(strings.TrimSpace(strings.ReplaceAll(name, "\\", "/")))
	name = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			return r
		case r == ' ':
			return '-'
		}
		return -1
	}, name)
	if !nameRe.MatchString(name) {
		return "", "", errors.New("use a file name of letters, digits, dots, dashes and underscores")
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".js":
		return name, "js", nil
	case ".css":
		return name, "css", nil
	}
	return "", "", errors.New("only .js and .css files can be added")
}

// expandPath resolves ~ to the home folder and requires a full path.
func expandPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	switch {
	case path == "~":
		path = config.HomeDir()
	case strings.HasPrefix(path, "~/"):
		path = filepath.Join(config.HomeDir(), path[2:])
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("give a full path on the server, such as /home/me/plugins/theme.css")
	}
	return filepath.Clean(path), nil
}

// readPluginFile reads a regular file of at most MaxPluginBytes.
func readPluginFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a file", path)
	}
	if info.Size() > MaxPluginBytes {
		return nil, fmt.Errorf("the file is over %d MB", MaxPluginBytes>>20)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return nil, errors.New("the file is empty")
	}
	return b, nil
}

func (s *Store) loadState() state {
	var st state
	if b, err := os.ReadFile(filepath.Join(s.dir, stateFile)); err == nil {
		_ = json.Unmarshal(b, &st)
	}
	if st.Sources == nil {
		st.Sources = map[string]string{}
	}
	return st
}

func (s *Store) saveState(st state) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	sort.Strings(st.Enabled)
	if len(st.Sources) == 0 {
		st.Sources = nil
	}
	b, _ := json.MarshalIndent(st, "", "  ")
	tmp := filepath.Join(s.dir, stateFile+".tmp")
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.dir, stateFile))
}

func writeCopy(path string, content []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, content, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// content returns what a plugin serves now: its source when that can be read, refreshing the kept copy when the
// source changed, and else the kept copy. Called with s.mu held.
func (s *Store) content(name string, st state) ([]byte, bool, error) {
	copyPath := filepath.Join(s.dir, name)
	if src := st.Sources[name]; src != "" {
		if b, err := readPluginFile(src); err == nil {
			if old, err := os.ReadFile(copyPath); err != nil || string(old) != string(b) {
				_ = writeCopy(copyPath, b)
			}
			return b, false, nil
		}
		b, err := os.ReadFile(copyPath)
		return b, true, err
	}
	b, err := os.ReadFile(copyPath)
	return b, false, err
}

// List returns every plugin, in name order, with whether it is on and where it is read from.
func (s *Store) List() ([]Plugin, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Plugin{}, nil
	}
	if err != nil {
		return nil, err
	}
	st := s.loadState()
	out := []Plugin{}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		name, kind, err := cleanName(e.Name())
		if err != nil || name != e.Name() {
			continue
		}
		b, missing, err := s.content(name, st)
		if err != nil {
			continue
		}
		sum := sha256.Sum256(b)
		out = append(out, Plugin{Name: name, Kind: kind, Size: int64(len(b)), Enabled: contains(st.Enabled, name),
			Version: hex.EncodeToString(sum[:6]), Source: st.Sources[name], SourceMissing: missing})
	}
	return out, nil
}

// AddPath adds a .js or .css file on this server as a plugin linked to it, under the file's name. A plugin of the
// same name is replaced and keeps whether it was on; a new one starts on, since adding it is how a user asks for it.
func (s *Store) AddPath(path string) (Plugin, error) {
	path, err := expandPath(path)
	if err != nil {
		return Plugin{}, err
	}
	name, kind, err := cleanName(filepath.Base(path))
	if err != nil {
		return Plugin{}, err
	}
	content, err := readPluginFile(path)
	if err != nil {
		return Plugin{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return Plugin{}, err
	}
	copyPath := filepath.Join(s.dir, name)
	_, statErr := os.Stat(copyPath)
	existed := statErr == nil
	if err := writeCopy(copyPath, content); err != nil {
		return Plugin{}, err
	}
	st := s.loadState()
	// A file picked from the plugin folder itself is its own copy, and removing the plugin removes it.
	if path == copyPath {
		delete(st.Sources, name)
	} else {
		st.Sources[name] = path
	}
	if !existed && !contains(st.Enabled, name) {
		st.Enabled = append(st.Enabled, name)
	}
	if err := s.saveState(st); err != nil {
		return Plugin{}, err
	}
	sum := sha256.Sum256(content)
	return Plugin{Name: name, Kind: kind, Size: int64(len(content)), Enabled: contains(st.Enabled, name),
		Version: hex.EncodeToString(sum[:6]), Source: st.Sources[name]}, nil
}

// SetEnabled turns one plugin on or off.
func (s *Store) SetEnabled(name string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !nameRe.MatchString(name) || name == stateFile {
		return fmt.Errorf("no plugin named %q", name)
	}
	if _, err := os.Stat(filepath.Join(s.dir, name)); err != nil {
		return fmt.Errorf("no plugin named %q", name)
	}
	st := s.loadState()
	st.Enabled = without(st.Enabled, name)
	if enabled {
		st.Enabled = append(st.Enabled, name)
	}
	return s.saveState(st)
}

// Delete removes one plugin, its kept copy and its settings. The source file is left alone.
func (s *Store) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !nameRe.MatchString(name) || name == stateFile {
		return fmt.Errorf("no plugin named %q", name)
	}
	if err := os.Remove(filepath.Join(s.dir, name)); err != nil {
		return fmt.Errorf("no plugin named %q", name)
	}
	st := s.loadState()
	st.Enabled = without(st.Enabled, name)
	delete(st.Sources, name)
	return s.saveState(st)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func without(list []string, s string) []string {
	out := []string{}
	for _, v := range list {
		if v != s {
			out = append(out, v)
		}
	}
	return out
}

// Entry is one item of a folder on the server, as the file chooser shows it.
type Entry struct {
	Name   string `json:"name"`
	Dir    bool   `json:"dir"`
	Size   int64  `json:"size,omitempty"`
	Plugin bool   `json:"plugin,omitempty"`
}

// Folder is a folder on the server with the folders and plugin files in it, and the chooser's shortcuts.
type Folder struct {
	Path      string  `json:"path"`
	Parent    string  `json:"parent,omitempty"`
	Entries   []Entry `json:"entries"`
	Home      string  `json:"home,omitempty"`
	Workspace string  `json:"workspace,omitempty"`
}

// Browse lists a folder on the server for the file chooser: its folders, and the .js and .css files that can be added.
// An empty path starts at the home folder. Hidden items are listed too, and the chooser decides whether to show them.
func (s *Store) Browse(dir string) (Folder, error) {
	if strings.TrimSpace(dir) == "" {
		dir = "~"
	}
	dir, err := expandPath(dir)
	if err != nil {
		return Folder{}, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Folder{}, fmt.Errorf("cannot open %s: %w", dir, err)
	}
	out := Folder{Path: dir, Entries: []Entry{}, Home: config.HomeDir(), Workspace: s.workspace}
	if parent := filepath.Dir(dir); parent != dir {
		out.Parent = parent
	}
	for _, e := range entries {
		if len(out.Entries) >= maxEntries {
			break
		}
		// Follow links, so a linked folder opens and a linked file can be added.
		info, err := os.Stat(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		switch {
		case info.IsDir():
			out.Entries = append(out.Entries, Entry{Name: e.Name(), Dir: true})
		case info.Mode().IsRegular():
			if ext := strings.ToLower(filepath.Ext(e.Name())); ext == ".js" || ext == ".css" {
				out.Entries = append(out.Entries, Entry{Name: e.Name(), Size: info.Size(), Plugin: true})
			}
		}
	}
	sort.Slice(out.Entries, func(i, j int) bool {
		a, b := out.Entries[i], out.Entries[j]
		if a.Dir != b.Dir {
			return a.Dir
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return out, nil
}

func (s *Store) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.writeList(w)
}

func (s *Store) writeList(w http.ResponseWriter) {
	list, err := s.List()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"plugins": list})
}

// sameOrigin checks a request came from the web app's own pages.
func (s *Store) sameOrigin(w http.ResponseWriter, r *http.Request) bool {
	if ok, _ := auth.CheckSameOrigin(r, s.trusted, s.publicURL); !ok {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	return true
}

// changing checks a request that changes the plugins: a POST from the web app's own origin.
func (s *Store) changing(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return s.sameOrigin(w, r)
}

func (s *Store) handleBrowse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// A listing of the server's folders is only for the web app itself.
	if !s.sameOrigin(w, r) {
		return
	}
	folder, err := s.Browse(r.URL.Query().Get("path"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, folder)
}

func (s *Store) handleAddPath(w http.ResponseWriter, r *http.Request) {
	if !s.changing(w, r) {
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "send {\"path\": \"...\"}"})
		return
	}
	if _, err := s.AddPath(req.Path); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	s.writeList(w)
}

func (s *Store) handleToggle(w http.ResponseWriter, r *http.Request) {
	if !s.changing(w, r) {
		return
	}
	var req struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "send {\"name\": \"...\", \"enabled\": true}"})
		return
	}
	if err := s.SetEnabled(req.Name, req.Enabled); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	s.writeList(w)
}

func (s *Store) handleDelete(w http.ResponseWriter, r *http.Request) {
	if !s.changing(w, r) {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "send {\"name\": \"...\"}"})
		return
	}
	if err := s.Delete(req.Name); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	s.writeList(w)
}

func (s *Store) handleLoader(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	if r.URL.Query().Get("v") == loaderVersion {
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	_, _ = io.WriteString(w, loaderScript)
}

func (s *Store) handleFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	requested := strings.TrimPrefix(r.URL.Path, FilePrefix)
	name, kind, err := cleanName(requested)
	if err != nil || name != requested {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	b, _, err := s.content(name, s.loadState())
	s.mu.Unlock()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if kind == "css" {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// The loader asks for each file with its content hash in the query, so a changed file is fetched again; a
	// request without it is not kept.
	if r.URL.Query().Get("v") != "" {
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	_, _ = w.Write(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
