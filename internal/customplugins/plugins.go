// Package customplugins keeps the JavaScript and CSS files a user adds under Settings > Appearance > Custom Plugins,
// and serves the script that loads the enabled ones into the web app. They are files on this server: a plugin added
// from a path stays linked to the file it leads to, so editing it takes effect on the next reload, and a copy is kept in
// case it goes away. A file the agent can change, inside the workspace folder agy-server is set up with or the agent's
// own folders, is copied and not linked, since an agent held to those folders could otherwise run code in the web app
// through it. The plugins are the same for every browser signed in to this agy-server, and they run with the web app's
// access, which reaches the agent and its terminal.
package customplugins

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/AFSlayer/antigravity-server/internal/auth"
	"github.com/AFSlayer/antigravity-server/internal/config"
)

const (
	ListAPIPath    = "/__agy/api/custom-plugins"
	BrowseAPIPath  = "/__agy/api/custom-plugins/browse"
	AddPathAPIPath = "/__agy/api/custom-plugins/add-path"
	ToggleAPIPath  = "/__agy/api/custom-plugins/toggle"
	DeleteAPIPath  = "/__agy/api/custom-plugins/delete"
	// LoaderPath serves the settings section and the loader. It is the same for every page, so a browser keeps it.
	LoaderPath = "/__agy/custom-plugins/loader.js"
	// FilePrefix serves one plugin file by name.
	FilePrefix = "/__agy/custom-plugins/file/"
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
	// SourceMissing says the source could not be used, so the copy kept when it last could is, and SourceProblem why.
	SourceMissing bool   `json:"sourceMissing,omitempty"`
	SourceProblem string `json:"sourceProblem,omitempty"`
	// SourceGone says the source is not there at all, so the copy is the only one left.
	SourceGone bool `json:"sourceGone,omitempty"`
	// CopiedFrom is the file a plugin was copied from without a link: a file the agent can change.
	CopiedFrom string `json:"copiedFrom,omitempty"`
}

type state struct {
	Enabled []string          `json:"enabled"`
	Sources map[string]string `json:"sources,omitempty"`
	Copied  map[string]string `json:"copied,omitempty"`
}

// Store is the plugin directory and the record of which plugins are on and where they came from.
type Store struct {
	dir       string
	workspace string
	// agentDirs are the agent's own folders, where it keeps its notes, scratch files, rules and skills.
	agentDirs []string
	mu        sync.Mutex
	trusted   []*net.IPNet
	publicURL string
	// known holds the hash of each file read, with its stamp then, so a file that has not changed is not read again.
	// A source is held under the plugin it is read for, as two plugins may be linked to one file and each has a copy.
	known map[string]known
	// damaged is where a state file that could not be read was put aside, until the next change is saved.
	damaged string
}

// sourceKey is what a plugin's source is held under in known.
func sourceKey(name, src string) string { return name + "\x00" + src }

// stamp is what tells a file apart from the last time it was read, without reading it. Where the system has them, the
// file's number and the time its inode last changed are in it too, so a copy that keeps the size and the modification
// time, such as cp -p or rsync -t, still shows.
type stamp struct {
	size   int64
	mod    time.Time
	ino    uint64
	change int64
}

func (a stamp) same(b stamp) bool {
	return a.size == b.size && a.mod.Equal(b.mod) && a.ino == b.ino && a.change == b.change
}

type known struct {
	stamp stamp
	sum   [32]byte
}

// stampOf gives a regular file's stamp.
func stampOf(path string) (stamp, error) {
	info, err := os.Stat(path)
	if err != nil {
		return stamp{}, err
	}
	if !info.Mode().IsRegular() {
		return stamp{}, errNotFile
	}
	ino, change := fileIdentity(info)
	return stamp{size: info.Size(), mod: info.ModTime(), ino: ino, change: change}, nil
}

// New returns a store kept in dir, which is created when the first plugin is added.
func New(dir string) *Store {
	s := &Store{dir: dir, known: map[string]known{}}
	if home := config.HomeDir(); home != "" {
		s.agentDirs = []string{config.GeminiDir(), filepath.Join(home, ".agents"), filepath.Join(home, ".agent")}
	}
	return s
}

// guarded gives the folders whose files the agent can change: the workspace and the agent's own folders.
func (s *Store) guarded() []string {
	out := []string{}
	if s.workspace != "" {
		out = append(out, s.workspace)
	}
	return append(out, s.agentDirs...)
}

// agentCanChange says whether a path is inside a folder the agent can change.
func (s *Store) agentCanChange(paths ...string) bool {
	for _, dir := range s.guarded() {
		for _, p := range paths {
			if inside(dir, p) {
				return true
			}
		}
	}
	return false
}

// WithSameOrigin sets what requests are checked against, as the other settings APIs do.
func (s *Store) WithSameOrigin(trusted []*net.IPNet, publicURL string) *Store {
	s.trusted = trusted
	s.publicURL = publicURL
	return s
}

// WithWorkspace sets the workspace folder: the file chooser offers it as a shortcut, and a file in it is copied.
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

var (
	nameRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
	charsRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

// cleanName turns a file name into the name a plugin is kept under, and says what kind it is. A space becomes a dash;
// any other character a plugin's name cannot have refuses the file, rather than being dropped and the plugin named
// something the user did not pick.
func cleanName(name string) (string, string, error) {
	name = filepath.Base(strings.TrimSpace(strings.ReplaceAll(name, "\\", "/")))
	name = strings.ReplaceAll(name, " ", "-")
	switch {
	case !charsRe.MatchString(name):
		return "", "", errors.New("use a file name of letters, digits, dots, dashes and underscores")
	case !nameRe.MatchString(name):
		return "", "", errors.New("use a file name of at most 100 characters")
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
	case strings.HasPrefix(path, "~/"), filepath.Separator == '\\' && strings.HasPrefix(path, `~\`):
		path = filepath.Join(config.HomeDir(), path[2:])
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("give a full path on the server, such as /home/me/plugins/theme.css")
	}
	return filepath.Clean(path), nil
}

// errNotFile is what reading a folder as a plugin gives, so the chooser can open it instead.
var errNotFile = errors.New("is not a file")

// errGone is a source that is not there any more, as against one that is there and cannot be used.
var errGone = errors.New("is not there")

// unreadable says why a file cannot be read, naming it.
func unreadable(path string, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%s %w", path, errGone)
	}
	if errors.Is(err, errNotFile) {
		return fmt.Errorf("%s %w", path, errNotFile)
	}
	return fmt.Errorf("cannot read %s: %w", path, err)
}

// readPluginFile reads a regular file of at most MaxPluginBytes, however much the file grows while it is read. Every
// error names the file.
func readPluginFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, unreadable(path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, unreadable(path, errNotFile)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, unreadable(path, err)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxPluginBytes+1))
	if err != nil {
		return nil, unreadable(path, err)
	}
	if len(b) > MaxPluginBytes {
		return nil, fmt.Errorf("%s is over %d MB", path, MaxPluginBytes>>20)
	}
	if len(b) == 0 {
		return nil, fmt.Errorf("%s is empty", path)
	}
	return b, nil
}

// inside says whether path is dir or below it, as written or after following links in both. A path written under dir
// counts even when a link there leads out of it, since that link is dir's to change.
func inside(dir, path string) bool {
	if dir == "" {
		return false
	}
	under := func(d, p string) bool {
		rel, err := filepath.Rel(d, p)
		return err == nil && !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	resolve := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return filepath.Clean(p)
	}
	if under(filepath.Clean(dir), filepath.Clean(path)) || under(resolve(dir), resolve(path)) {
		return true
	}
	// The same folder under another name: other letter case on macOS or Windows, or a bind mount. Each folder the path
	// is in, as written and once links are followed, is compared with dir itself.
	dirInfo, err := os.Stat(dir)
	if err != nil {
		return false
	}
	for _, p := range []string{filepath.Clean(path), resolve(path)} {
		for {
			if info, err := os.Stat(p); err == nil && os.SameFile(info, dirInfo) {
				return true
			}
			parent := filepath.Dir(p)
			if parent == p {
				break
			}
			p = parent
		}
	}
	return false
}

func (s *Store) loadState() state {
	var st state
	path := filepath.Join(s.dir, stateFile)
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &st); err != nil {
			// Keep a damaged file beside the new one rather than overwrite it on the next change, under a name of its
			// own so a second one does not overwrite the first, and say so in the section.
			aside := path + ".broken-" + time.Now().UTC().Format("20060102T150405.000000000Z")
			if os.Rename(path, aside) == nil {
				s.damaged = aside
			}
			st = state{}
		}
	}
	if st.Sources == nil {
		st.Sources = map[string]string{}
	}
	if st.Copied == nil {
		st.Copied = map[string]string{}
	}
	// A source the agent has come to be able to change, after the workspace changed, is no longer read; its copy is used.
	for name, src := range st.Sources {
		if s.agentCanChange(src) {
			delete(st.Sources, name)
			st.Copied[name] = src
		}
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
	if len(st.Copied) == 0 {
		st.Copied = nil
	}
	b, _ := json.MarshalIndent(st, "", "  ")
	tmp := filepath.Join(s.dir, stateFile+".tmp")
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(s.dir, stateFile)); err != nil {
		return err
	}
	s.damaged = ""
	return nil
}

func writeCopy(path string, content []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, content, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// content returns what a plugin serves now: its source when that can be used, refreshing the kept copy when the
// source changed, and else the kept copy, with why the source could not be used. Called with s.mu held.
func (s *Store) content(name string, st state) ([]byte, error, error) {
	copyPath := filepath.Join(s.dir, name)
	if src := st.Sources[name]; src != "" {
		key := sourceKey(name, src)
		// The stamp before the read: a change during the read then shows as a new stamp next time.
		before, _ := stampOf(src)
		b, err := readPluginFile(src)
		if err == nil {
			sum := sha256.Sum256(b)
			// The copy is known to hold this content when it was last written or seen with it. Otherwise it is looked
			// at and written, and the source is remembered only once the copy holds what it says, so a copy that could
			// not be written is tried again next time.
			fresh := false
			if k, ok := s.known[key]; ok && k.sum == sum {
				fresh = true
			} else if old, err := os.ReadFile(copyPath); err == nil && bytes.Equal(old, b) {
				fresh = true
			} else {
				fresh = writeCopy(copyPath, b) == nil
			}
			if fresh {
				s.known[key] = known{stamp: before, sum: sum}
			} else {
				delete(s.known, key)
			}
			return b, nil, nil
		}
		kept, copyErr := os.ReadFile(copyPath)
		return kept, err, copyErr
	}
	b, err := os.ReadFile(copyPath)
	return b, nil, err
}

// summary gives a plugin's size and content hash for the list, reading only what changed since it was last read. A
// plugin that is off is not read from its source: its copy stands for it until it is turned on. Called with s.mu held.
func (s *Store) summary(name string, st state, enabled bool) (int64, [32]byte, error, error) {
	var problem error
	if src := st.Sources[name]; src != "" {
		if now, err := stampOf(src); err != nil {
			problem = unreadable(src, err)
		} else if k, hit := s.known[sourceKey(name, src)]; hit && k.stamp.same(now) {
			return now.size, k.sum, nil, nil
		} else if enabled {
			b, problem, err := s.content(name, st)
			return int64(len(b)), sha256.Sum256(b), problem, err
		}
	}
	copyPath := filepath.Join(s.dir, name)
	now, err := stampOf(copyPath)
	if err != nil {
		return 0, [32]byte{}, problem, fmt.Errorf("no plugin named %q", name)
	}
	if k, hit := s.known[copyPath]; hit && k.stamp.same(now) {
		return now.size, k.sum, problem, nil
	}
	b, err := os.ReadFile(copyPath)
	if err != nil {
		return 0, [32]byte{}, problem, err
	}
	sum := sha256.Sum256(b)
	s.known[copyPath] = known{stamp: now, sum: sum}
	return int64(len(b)), sum, problem, nil
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
		enabled := contains(st.Enabled, name)
		size, sum, problem, err := s.summary(name, st, enabled)
		if err != nil {
			continue
		}
		p := Plugin{Name: name, Kind: kind, Size: size, Enabled: enabled, Version: hex.EncodeToString(sum[:6]),
			Source: st.Sources[name], CopiedFrom: st.Copied[name]}
		if problem != nil {
			p.SourceMissing, p.SourceProblem, p.SourceGone = true, problem.Error(), errors.Is(problem, errGone)
		}
		out = append(out, p)
	}
	return out, nil
}

// AddPath adds a .js or .css file on this server as a plugin under the file's name. It is linked to the file a path
// leads to, and read from there from now on, so no link on the way is followed again; a path in a folder the agent can
// change, as written or once its links are followed, is copied instead. Adding the same file again refreshes it and keeps
// whether it was on; a file of the same name from somewhere else is refused rather than replace another plugin. A new
// plugin starts on, since adding it is how a user asks for it, and so does a file put in the plugin folder by hand.
func (s *Store) AddPath(path string) (Plugin, error) {
	path, err := expandPath(path)
	if err != nil {
		return Plugin{}, err
	}
	name, kind, err := cleanName(filepath.Base(path))
	if err != nil {
		return Plugin{}, err
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return Plugin{}, unreadable(path, err)
	}
	// The stamp before the read: a change during the read then shows as a new stamp next time.
	before, _ := stampOf(target)
	content, err := readPluginFile(target)
	if err != nil {
		return Plugin{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return Plugin{}, err
	}
	copyPath := filepath.Join(s.dir, name)
	copyInfo, statErr := os.Stat(copyPath)
	existed := statErr == nil
	st := s.loadState()
	// The plugin folder's own file, also when reached through a link or in other letter case.
	own := path == copyPath || target == copyPath
	if !own && existed {
		if info, err := os.Stat(target); err == nil && os.SameFile(info, copyInfo) {
			own = true
		}
	}
	if own {
		// Where a plugin comes from stays as it is when its copy is picked. One put in the folder by hand, which comes
		// from nowhere else, is turned on.
		if st.Sources[name] == "" && st.Copied[name] == "" && !contains(st.Enabled, name) {
			st.Enabled = append(st.Enabled, name)
			if err := s.saveState(st); err != nil {
				return Plugin{}, err
			}
		}
		return s.plugin(name, kind, content, st), nil
	}
	copied := s.agentCanChange(path, target)
	if existed && st.Sources[name] != target && st.Copied[name] != path && st.Copied[name] != target {
		if from := st.Sources[name] + st.Copied[name]; from != "" {
			return Plugin{}, fmt.Errorf("a plugin named %s is already added from %s; remove it first, or rename the file", name, from)
		}
		return Plugin{}, fmt.Errorf("a plugin named %s is already in %s; remove it first, or rename the file", name, s.dir)
	}
	if err := writeCopy(copyPath, content); err != nil {
		return Plugin{}, err
	}
	delete(st.Sources, name)
	delete(st.Copied, name)
	if copied {
		st.Copied[name] = path
	} else {
		st.Sources[name] = target
		s.known[sourceKey(name, target)] = known{stamp: before, sum: sha256.Sum256(content)}
	}
	if !existed && !contains(st.Enabled, name) {
		st.Enabled = append(st.Enabled, name)
	}
	if err := s.saveState(st); err != nil {
		return Plugin{}, err
	}
	return s.plugin(name, kind, content, st), nil
}

func (s *Store) plugin(name, kind string, content []byte, st state) Plugin {
	sum := sha256.Sum256(content)
	return Plugin{Name: name, Kind: kind, Size: int64(len(content)), Enabled: contains(st.Enabled, name),
		Version: hex.EncodeToString(sum[:6]), Source: st.Sources[name], CopiedFrom: st.Copied[name]}
}

// SetEnabled turns one plugin on or off.
func (s *Store) SetEnabled(name string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !isPluginName(name) {
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
	if !isPluginName(name) {
		return fmt.Errorf("no plugin named %q", name)
	}
	if err := os.Remove(filepath.Join(s.dir, name)); err != nil {
		return fmt.Errorf("no plugin named %q", name)
	}
	st := s.loadState()
	st.Enabled = without(st.Enabled, name)
	delete(s.known, sourceKey(name, st.Sources[name]))
	delete(s.known, filepath.Join(s.dir, name))
	delete(st.Sources, name)
	delete(st.Copied, name)
	return s.saveState(st)
}

// isPluginName says whether a name is one a plugin is kept under, and not the state file, its backup or a file being
// written.
func isPluginName(name string) bool {
	clean, _, err := cleanName(name)
	return err == nil && clean == name
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
	// Unusable says why a .js or .css file cannot be added as it is named.
	Unusable string `json:"unusable,omitempty"`
}

// Folder is a folder on the server with the folders and plugin files in it, and the chooser's shortcuts.
type Folder struct {
	Path      string  `json:"path"`
	Parent    string  `json:"parent,omitempty"`
	Entries   []Entry `json:"entries"`
	Home      string  `json:"home,omitempty"`
	Workspace string  `json:"workspace,omitempty"`
	// Truncated says the folder has more items than the chooser lists.
	Truncated bool `json:"truncated,omitempty"`
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
		ext := strings.ToLower(filepath.Ext(e.Name()))
		script := ext == ".js" || ext == ".css"
		// A plain file that is not a script or a style is never listed, and needs no look at it.
		if e.Type().IsRegular() && !script {
			continue
		}
		// Follow links, so a linked folder opens and a linked file can be added.
		info, err := os.Stat(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var entry Entry
		switch {
		case info.IsDir():
			entry = Entry{Name: e.Name(), Dir: true}
		case info.Mode().IsRegular() && script:
			entry = Entry{Name: e.Name(), Size: info.Size(), Plugin: true}
			if _, _, err := cleanName(e.Name()); err != nil {
				entry.Plugin, entry.Unusable = false, err.Error()
			}
		default:
			continue
		}
		out.Entries = append(out.Entries, entry)
	}
	// Sorted before it is cut, so what is left out is the end of the list as the chooser shows it.
	sort.Slice(out.Entries, func(i, j int) bool {
		a, b := out.Entries[i], out.Entries[j]
		if a.Dir != b.Dir {
			return a.Dir
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	if len(out.Entries) > maxEntries {
		out.Entries, out.Truncated = out.Entries[:maxEntries], true
	}
	return out, nil
}

func (s *Store) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// The list names files on the server, so like a folder listing it is only for the web app itself.
	if !s.sameOrigin(w, r) {
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
	// The folders whose files are copied rather than linked are named, those that are there.
	copied := []string{}
	for _, dir := range s.guarded() {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			copied = append(copied, dir)
		}
	}
	out := map[string]any{"plugins": list, "copiedFolders": copied}
	s.mu.Lock()
	if s.damaged != "" {
		out["stateProblem"] = "The plugin settings could not be read, so every plugin was turned off and its link to its file " +
			"was dropped. The settings were kept as " + s.damaged + "."
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, out)
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
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "notFile": errors.Is(err, errNotFile)})
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
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
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
	// A plugin that is not there now may be added later under the same name.
	w.Header().Set("Cache-Control", "no-store")
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
	sum := sha256.Sum256(b)
	if kind == "css" {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Only the web app's own pages may use a plugin, not a page on another subdomain of the same site.
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	// The loader asks for each file with its content hash in the query, so a changed file is fetched again. Only an
	// answer whose content has that hash is kept: a source that changed in between must not be kept under the old one.
	if r.URL.Query().Get("v") == hex.EncodeToString(sum[:6]) {
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
