package customplugins

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCleanName(t *testing.T) {
	for in, want := range map[string]string{
		"theme.css":        "theme.css",
		"My Theme.CSS":     "My-Theme.CSS",
		"../../etc/x.js":   "x.js",
		`C:\Users\me\a.js`: "a.js",
	} {
		got, _, err := cleanName(in)
		if err != nil || got != want {
			t.Errorf("cleanName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "x.txt", "x.html", ".hidden.js", "state.json", "한글 style.css", "my테마.css", "theme (1).css"} {
		if _, _, err := cleanName(bad); err == nil {
			t.Errorf("cleanName(%q) was accepted", bad)
		}
	}
	if _, _, err := cleanName(strings.Repeat("a", 98) + ".css"); err == nil || !strings.Contains(err.Error(), "100 characters") {
		t.Errorf("a long name: %v", err)
	}
}

// tempDir gives a temporary folder by the path its links lead to, as a linked plugin's source is kept: on macOS the
// temporary folder is behind /var, a link to /private/var, and on Windows it may have a short 8.3 name.
func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// newStore gives a store with no folder of the agent's own, so that tests do not depend on the home folder.
func newStore(dir string) *Store {
	s := New(dir)
	s.agentDirs = nil
	return s
}

// jsonBody encodes a request body, so a Windows path's backslashes are escaped.
func jsonBody(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// symlink makes a link, or skips the test where links cannot be made.
func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot make a link: %v", err)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func one(t *testing.T, s *Store, name string) Plugin {
	t.Helper()
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range list {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("%s not listed in %+v", name, list)
	return Plugin{}
}

// A plugin added from a path stays linked to that file: an edit shows in the content and the version, a vanished
// source falls back to the copy kept when it was last read, and removing the plugin leaves the source alone.
func TestAddPathLinksTheSource(t *testing.T) {
	dir := tempDir(t)
	src := filepath.Join(dir, "src", "theme.css")
	write(t, src, "a{color:red}")
	s := newStore(filepath.Join(dir, "plugins"))

	p, err := s.AddPath(src)
	if err != nil || !p.Enabled || p.Kind != "css" || p.Source != src {
		t.Fatalf("add: %+v, %v", p, err)
	}
	first := p.Version

	write(t, src, "a{color:blue}")
	p = one(t, s, "theme.css")
	if p.Version == first || p.SourceMissing {
		t.Fatalf("an edit to the source is not seen: %+v", p)
	}
	b, _, _ := s.content("theme.css", s.loadState())
	if string(b) != "a{color:blue}" {
		t.Fatalf("served %q", b)
	}

	if err := os.Remove(src); err != nil {
		t.Fatal(err)
	}
	p = one(t, s, "theme.css")
	if !p.SourceMissing {
		t.Fatalf("a missing source is not reported: %+v", p)
	}
	if b, _, _ = s.content("theme.css", s.loadState()); string(b) != "a{color:blue}" {
		t.Fatalf("the kept copy is not the last version read: %q", b)
	}

	write(t, src, "a{color:green}")
	if err := s.Delete("theme.css"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal("removing the plugin removed its source")
	}
	if st := s.loadState(); len(st.Sources) != 0 || len(st.Enabled) != 0 {
		t.Fatalf("settings left behind: %+v", st)
	}
}

func TestAddPathRules(t *testing.T) {
	dir := tempDir(t)
	s := newStore(filepath.Join(dir, "plugins"))
	write(t, filepath.Join(dir, "tools.js"), "console.log(1)")
	if _, err := s.AddPath(filepath.Join(dir, "tools.js")); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnabled("tools.js", false); err != nil {
		t.Fatal(err)
	}
	// Adding the same file again refreshes it and keeps it off.
	write(t, filepath.Join(dir, "tools.js"), "console.log(2)")
	if p, err := s.AddPath(filepath.Join(dir, "tools.js")); err != nil || p.Enabled || p.Size != int64(len("console.log(2)")) {
		t.Fatalf("add again: %+v, %v", p, err)
	}
	// A file of the same name from another folder does not replace it.
	write(t, filepath.Join(dir, "other", "tools.js"), "console.log(3)")
	if _, err := s.AddPath(filepath.Join(dir, "other", "tools.js")); err == nil || !strings.Contains(err.Error(), "already added from "+filepath.Join(dir, "tools.js")) {
		t.Fatalf("same name from another folder: %v", err)
	}
	if p := one(t, s, "tools.js"); p.Source != filepath.Join(dir, "tools.js") {
		t.Fatalf("the refused file replaced the plugin: %+v", p)
	}
	write(t, filepath.Join(dir, "empty.js"), "")
	write(t, filepath.Join(dir, "notes.txt"), "x")
	write(t, filepath.Join(dir, "big.js"), strings.Repeat("x", MaxPluginBytes+1))
	for _, bad := range []string{"relative.css", dir, filepath.Join(dir, "empty.js"), filepath.Join(dir, "notes.txt"), filepath.Join(dir, "big.js"), filepath.Join(dir, "nope.js")} {
		if _, err := s.AddPath(bad); err == nil {
			t.Errorf("%s was accepted", bad)
		}
	}
	// A file in the plugin folder itself is not linked to itself, and picking it turns it on.
	write(t, filepath.Join(dir, "plugins", "hand.css"), "b{}")
	if p, err := s.AddPath(filepath.Join(dir, "plugins", "hand.css")); err != nil || p.Source != "" || !p.Enabled {
		t.Fatalf("own file: %+v, %v", p, err)
	}
	// Picking a linked plugin's copy keeps its link and leaves it off.
	if p, err := s.AddPath(filepath.Join(dir, "plugins", "tools.js")); err != nil || p.Source != filepath.Join(dir, "tools.js") || p.Enabled {
		t.Fatalf("a linked plugin's copy: %+v, %v", p, err)
	}
	// A folder named like a plugin says it is not a file, so the chooser can open it.
	if err := os.Mkdir(filepath.Join(dir, "highlight.js"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPath(filepath.Join(dir, "highlight.js")); !errors.Is(err, errNotFile) {
		t.Fatalf("folder: %v", err)
	}
	if err := s.SetEnabled("missing.js", true); err == nil {
		t.Fatal("a missing plugin was turned on")
	}
	if err := s.Delete("state.json"); err == nil {
		t.Fatal("the state file could be deleted")
	}
	write(t, filepath.Join(dir, "plugins", "state.json.broken"), "{")
	write(t, filepath.Join(dir, "plugins", "tools.js.tmp"), "x")
	for _, name := range []string{"state.json.broken", "tools.js.tmp"} {
		if err := s.SetEnabled(name, true); err == nil {
			t.Errorf("%s could be turned on", name)
		}
		if err := s.Delete(name); err == nil {
			t.Errorf("%s could be deleted", name)
		}
	}
}

// The loader is one file for every page, kept by the browser when asked for by its version.
func TestLoaderIsCacheable(t *testing.T) {
	s := newStore(filepath.Join(tempDir(t), "plugins"))
	mux := http.NewServeMux()
	s.Register(mux)
	for url, want := range map[string]string{LoaderURL: "immutable", LoaderPath: "no-cache"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", url, nil))
		if rec.Code != 200 || !strings.Contains(rec.Header().Get("Cache-Control"), want) || !strings.Contains(rec.Body.String(), "__agyCustomPluginsSection") ||
			rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%s: %d %q", url, rec.Code, rec.Header().Get("Cache-Control"))
		}
	}
	if !strings.Contains(LoaderURL, "?v="+loaderVersion) || len(loaderVersion) != 12 {
		t.Fatalf("loader URL %q", LoaderURL)
	}
}

func TestHandlers(t *testing.T) {
	dir := tempDir(t)
	s := newStore(filepath.Join(dir, "plugins"))
	mux := http.NewServeMux()
	s.Register(mux)
	do := func(method, path, body, origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://agy.example.com"+path, strings.NewReader(body))
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	src := filepath.Join(dir, "a.js")
	write(t, src, "alert(1)")
	body := jsonBody(t, map[string]string{"path": src})
	if rec := do("POST", AddPathAPIPath, body, "http://evil.example.com"); rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin add: %d", rec.Code)
	}
	if rec := do("GET", AddPathAPIPath, "", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET add: %d", rec.Code)
	}
	rec := do("POST", AddPathAPIPath, body, "http://agy.example.com")
	if rec.Code != http.StatusOK {
		t.Fatalf("add: %d %s", rec.Code, rec.Body)
	}
	var resp struct {
		Plugins []Plugin `json:"plugins"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || len(resp.Plugins) != 1 || resp.Plugins[0].Source != src {
		t.Fatalf("add answer: %s", rec.Body)
	}
	first := resp.Plugins[0].Version
	file := do("GET", FilePrefix+"a.js?v="+first, "", "")
	if file.Code != http.StatusOK || file.Body.String() != "alert(1)" || !strings.HasPrefix(file.Header().Get("Content-Type"), "application/javascript") || !strings.Contains(file.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("file: %d %q %s", file.Code, file.Body, file.Header())
	}
	write(t, src, "alert(2)")
	if got := do("GET", FilePrefix+"a.js", "", "").Body.String(); got != "alert(2)" {
		t.Fatalf("the file is not read from its source: %q", got)
	}
	// The source changed after the list was read: the new content under the old version is not kept.
	if stale := do("GET", FilePrefix+"a.js?v="+first, "", ""); stale.Body.String() != "alert(2)" || !strings.Contains(stale.Header().Get("Cache-Control"), "no-cache") {
		t.Fatalf("stale version: %q %q", stale.Body, stale.Header().Get("Cache-Control"))
	}
	if err := os.Mkdir(filepath.Join(dir, "lib.js"), 0o700); err != nil {
		t.Fatal(err)
	}
	if rec := do("POST", AddPathAPIPath, jsonBody(t, map[string]string{"path": filepath.Join(dir, "lib.js")}), "http://agy.example.com"); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"notFile":true`) {
		t.Fatalf("adding a folder: %d %s", rec.Code, rec.Body)
	}
	for _, path := range []string{ToggleAPIPath, DeleteAPIPath} {
		if rec := do("POST", path, `{"name":"a.js","enabled":false}`, "http://evil.example.com"); rec.Code != http.StatusForbidden {
			t.Fatalf("cross-origin %s: %d", path, rec.Code)
		}
	}
	if p := one(t, s, "a.js"); !p.Enabled {
		t.Fatal("a cross-origin request changed the plugin")
	}
	if rec := do("GET", FilePrefix+"..%2fstate.json", "", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("path escape: %d", rec.Code)
	}
	if file.Header().Get("Cross-Origin-Resource-Policy") != "same-origin" {
		t.Fatalf("file headers: %s", file.Header())
	}
	if rec := do("GET", FilePrefix+"later.js", "", ""); rec.Code != http.StatusNotFound || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("a missing plugin: %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	if rec := do("HEAD", FilePrefix+"a.js", "", ""); rec.Code != http.StatusOK {
		t.Fatalf("HEAD: %d", rec.Code)
	}
	if rec := do("GET", BrowseAPIPath+"?path="+url.QueryEscape(dir), "", "http://agy.example.com"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"a.js"`) {
		t.Fatalf("browse: %d %s", rec.Code, rec.Body)
	}
	if rec := do("GET", ListAPIPath, "", "http://agy.example.com"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"copiedFolders"`) {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	if rec := do("POST", ToggleAPIPath, `{"name":"a.js","enabled":false}`, "http://agy.example.com"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"enabled":false`) {
		t.Fatalf("toggle: %d %s", rec.Code, rec.Body)
	}
	if rec := do("POST", DeleteAPIPath, `{"name":"a.js"}`, "http://agy.example.com"); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
}

// The chooser gets a server folder's folders first and then its .js and .css files, hidden ones included, with other
// files left out, links followed, the parent, and the home and workspace shortcuts.
func TestBrowse(t *testing.T) {
	dir := tempDir(t)
	for _, d := range []string{"b-dir", ".hidden"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"z.css", "a.JS", "notes.txt", "한글.css"} {
		write(t, filepath.Join(dir, f), "x")
	}
	symlink(t, filepath.Join(dir, "b-dir"), filepath.Join(dir, "link"))
	s := newStore(filepath.Join(tempDir(t), "plugins")).WithWorkspace("/srv/work")
	f, err := s.Browse(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range f.Entries {
		n := e.Name
		if e.Dir {
			n += "/"
		}
		got = append(got, n)
	}
	if want := ".hidden/ b-dir/ link/ a.JS z.css 한글.css"; strings.Join(got, " ") != want {
		t.Fatalf("entries %q, want %q", strings.Join(got, " "), want)
	}
	for _, e := range f.Entries {
		if bad := e.Name == "한글.css"; bad != (e.Unusable != "") || bad == e.Plugin && !e.Dir {
			t.Errorf("%s: plugin %v, unusable %q", e.Name, e.Plugin, e.Unusable)
		}
	}
	if f.Parent != filepath.Dir(dir) || f.Workspace != "/srv/work" || f.Home == "" {
		t.Fatalf("parent %q, workspace %q, home %q", f.Parent, f.Workspace, f.Home)
	}
	if _, err := s.Browse("relative/dir"); err == nil {
		t.Fatal("a relative path was listed")
	}
	if root, err := s.Browse(filepath.VolumeName(dir) + string(filepath.Separator)); err != nil || root.Parent != "" {
		t.Fatalf("root: %+v, %v", root.Parent, err)
	}

	mux := http.NewServeMux()
	s.Register(mux)
	req := httptest.NewRequest("GET", "http://agy.example.com"+BrowseAPIPath+"?path="+dir, nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site listing: %d", rec.Code)
	}
	req = httptest.NewRequest("GET", "http://agy.example.com"+ListAPIPath, nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site plugin list: %d", rec.Code)
	}
}

// A file inside the workspace is copied and not linked, also through a link from outside it, so an agent that may
// change only the workspace cannot change a plugin. A linked source the workspace comes to hold is no longer read.
func TestWorkspaceFilesAreCopied(t *testing.T) {
	dir := tempDir(t)
	ws := filepath.Join(dir, "work")
	inWS := filepath.Join(ws, "theme.css")
	write(t, inWS, "a{color:red}")
	link := filepath.Join(dir, "elsewhere", "linked.css")
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	symlink(t, inWS, link)
	// A link in the workspace to a folder outside it: the path is the workspace's, as the agent can point the link
	// elsewhere.
	outsideDir := filepath.Join(dir, "dotfiles")
	write(t, filepath.Join(outsideDir, "dots.css"), "b{}")
	throughWS := filepath.Join(ws, "dots-link", "dots.css")
	symlink(t, outsideDir, filepath.Join(ws, "dots-link"))
	s := newStore(filepath.Join(dir, "plugins")).WithWorkspace(ws)
	for _, path := range []string{inWS, link, throughWS} {
		p, err := s.AddPath(path)
		if err != nil || p.Source != "" || p.CopiedFrom != path || !p.Enabled {
			t.Fatalf("%s: %+v, %v", path, p, err)
		}
	}
	write(t, inWS, "a{color:blue}")
	if b, _, _ := s.content("theme.css", s.loadState()); string(b) != "a{color:red}" {
		t.Fatalf("a workspace edit reached the plugin: %q", b)
	}
	// Adding it again takes in the edit, and it stays a copy.
	if p, err := s.AddPath(inWS); err != nil || p.CopiedFrom != inWS || p.Source != "" {
		t.Fatalf("add again: %+v, %v", p, err)
	}
	if b, _, _ := s.content("theme.css", s.loadState()); string(b) != "a{color:blue}" {
		t.Fatalf("adding again did not take in the edit: %q", b)
	}

	outside := filepath.Join(dir, "tools", "tools.js")
	write(t, outside, "1")
	if p, err := s.AddPath(outside); err != nil || p.Source != outside {
		t.Fatalf("outside the workspace: %+v, %v", p, err)
	}
	// A plugin added through a link outside the workspace is linked to where the link leads.
	symlink(t, outside, filepath.Join(dir, "elsewhere", "tool-link.js"))
	if p, err := s.AddPath(filepath.Join(dir, "elsewhere", "tool-link.js")); err != nil || p.Source != outside {
		t.Fatalf("through a link: %+v, %v", p, err)
	}
	wider := newStore(filepath.Join(dir, "plugins")).WithWorkspace(dir)
	write(t, outside, "2")
	if p := one(t, wider, "tools.js"); p.Source != "" || p.CopiedFrom != outside || p.Size != 1 {
		t.Fatalf("a source inside the new workspace is still read: %+v", p)
	}
	if err := wider.Delete("tools.js"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("removing a copied plugin removed the file it was copied from")
	}
}

// A damaged state file is kept beside the new one instead of being overwritten on the next change.
func TestDamagedStateIsKept(t *testing.T) {
	dir := filepath.Join(tempDir(t), "plugins")
	write(t, filepath.Join(dir, "a.js"), "1")
	write(t, filepath.Join(dir, stateFile), "{not json")
	s := newStore(dir)
	if p := one(t, s, "a.js"); p.Enabled {
		t.Fatalf("listed from a damaged state: %+v", p)
	}
	if err := s.SetEnabled("a.js", true); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, stateFile+".broken")); err != nil || string(b) != "{not json" {
		t.Fatalf("the damaged state was not kept: %q, %v", b, err)
	}
	if p := one(t, s, "a.js"); !p.Enabled {
		t.Fatal("the new state was not saved")
	}
}

// A plugin that is off is not read from its source, and a file whose size and time have not changed is not read again.
func TestListReadsOnlyWhatChanged(t *testing.T) {
	dir := tempDir(t)
	src := filepath.Join(dir, "a.css")
	write(t, src, "a{}")
	s := newStore(filepath.Join(dir, "plugins"))
	if _, err := s.AddPath(src); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnabled("a.css", false); err != nil {
		t.Fatal(err)
	}
	write(t, src, "a{color:red}")
	if p := one(t, s, "a.css"); p.Size != 3 {
		t.Fatalf("a plugin that is off was read from its source: %+v", p)
	}
	if err := s.SetEnabled("a.css", true); err != nil {
		t.Fatal(err)
	}
	p := one(t, s, "a.css")
	if p.Size != int64(len("a{color:red}")) {
		t.Fatalf("turned on, the source was not read: %+v", p)
	}
	// A file whose stamp has not changed is not read again: the hash kept for it is used.
	s.mu.Lock()
	k := s.known[src]
	k.sum = sha256.Sum256([]byte("kept"))
	s.known[src] = k
	s.mu.Unlock()
	kept := sha256.Sum256([]byte("kept"))
	if again := one(t, s, "a.css"); again.Version != hex.EncodeToString(kept[:6]) {
		t.Fatalf("an unchanged file was read again: %+v", again)
	}
	// An edit that keeps the size and the modification time, as cp -p does, still shows where the system says when
	// the file last changed.
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		info, _ := os.Stat(src)
		time.Sleep(10 * time.Millisecond)
		write(t, src, "a{color:tan}")
		if err := os.Chtimes(src, info.ModTime(), info.ModTime()); err != nil {
			t.Fatal(err)
		}
		if again := one(t, s, "a.css"); again.Version == hex.EncodeToString(kept[:6]) || again.Size != int64(len("a{color:tan}")) {
			t.Fatalf("an edit that kept the size and time was not seen: %+v", again)
		}
	}
	// A source that is gone is reported, with the copy standing in.
	if err := os.Remove(src); err != nil {
		t.Fatal(err)
	}
	if gone := one(t, s, "a.css"); !gone.SourceMissing || gone.Size == 0 || gone.SourceProblem != src+" is not there" {
		t.Fatalf("a missing source: %+v", gone)
	}
	// A source that grew past the limit says so.
	write(t, src, strings.Repeat("x", MaxPluginBytes+1))
	if big := one(t, s, "a.css"); !big.SourceMissing || !strings.Contains(big.SourceProblem, "over 5 MB") {
		t.Fatalf("a source over the limit: %+v", big)
	}
}

// A file in the agent's own folders is copied, as a workspace file is.
func TestAgentFoldersAreCopied(t *testing.T) {
	dir := tempDir(t)
	agent := filepath.Join(dir, "gemini")
	src := filepath.Join(agent, "antigravity", "scratch", "theme.css")
	write(t, src, "a{}")
	s := newStore(filepath.Join(dir, "plugins"))
	s.agentDirs = []string{agent}
	if p, err := s.AddPath(src); err != nil || p.Source != "" || p.CopiedFrom != src {
		t.Fatalf("a file in the agent's folder: %+v, %v", p, err)
	}
	if got := s.guarded(); len(got) != 1 || got[0] != agent {
		t.Fatalf("guarded %q", got)
	}
	if d := New(filepath.Join(dir, "p")); len(d.agentDirs) == 0 {
		t.Fatal("a new store guards none of the agent's folders")
	}
}

// The same folder under another name is still the folder: here a second link to the workspace, which the path is
// written under and leads into, neither under the workspace's own name.
func TestInsideKnowsAFolderByItself(t *testing.T) {
	dir := tempDir(t)
	ws := filepath.Join(dir, "work")
	write(t, filepath.Join(ws, "a.css"), "a{}")
	if !inside(ws, filepath.Join(ws, "a.css")) || inside(ws, filepath.Join(dir, "a.css")) || inside(ws, dir) {
		t.Fatal("plain paths")
	}
	symlink(t, ws, filepath.Join(dir, "other-name"))
	if !inside(filepath.Join(dir, "other-name"), filepath.Join(ws, "a.css")) {
		t.Fatal("the workspace under another name")
	}
}

// A folder with more items than the chooser lists says so.
func TestBrowseSaysWhenCut(t *testing.T) {
	dir := tempDir(t)
	for i := 0; i <= maxEntries; i++ {
		write(t, filepath.Join(dir, fmt.Sprintf("p%04d.js", i)), "x")
	}
	s := newStore(filepath.Join(tempDir(t), "plugins"))
	f, err := s.Browse(dir)
	if err != nil || !f.Truncated || len(f.Entries) != maxEntries {
		t.Fatalf("truncated %v, %d entries, %v", f.Truncated, len(f.Entries), err)
	}
	// The list is sorted before it is cut, so a folder, which comes first, is never the one left out.
	if err := os.Mkdir(filepath.Join(dir, "zzz-folder"), 0o700); err != nil {
		t.Fatal(err)
	}
	if f, _ := s.Browse(dir); !f.Entries[0].Dir || f.Entries[0].Name != "zzz-folder" {
		t.Fatalf("first entry %+v", f.Entries[0])
	}
	if err := os.Remove(filepath.Join(dir, "zzz-folder")); err != nil {
		t.Fatal(err)
	}
	// Items that would not be listed anyway do not make a folder cut.
	if err := os.Remove(filepath.Join(dir, fmt.Sprintf("p%04d.js", maxEntries))); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "zz-notes.txt"), "x")
	if f, err := s.Browse(dir); err != nil || f.Truncated || len(f.Entries) != maxEntries {
		t.Fatalf("truncated %v, %d entries, %v", f.Truncated, len(f.Entries), err)
	}
}
