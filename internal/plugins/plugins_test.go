package plugins

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	for _, bad := range []string{"", "x.txt", "x.html", ".hidden.js", "state.json", "한글 style.css"} {
		if _, _, err := cleanName(bad); err == nil {
			t.Errorf("cleanName(%q) was accepted", bad)
		}
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
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "theme.css")
	write(t, src, "a{color:red}")
	s := New(filepath.Join(dir, "plugins"))

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
	dir := t.TempDir()
	s := New(filepath.Join(dir, "plugins"))
	write(t, filepath.Join(dir, "tools.js"), "console.log(1)")
	if _, err := s.AddPath(filepath.Join(dir, "tools.js")); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnabled("tools.js", false); err != nil {
		t.Fatal(err)
	}
	// Adding a file of the same name again replaces it and keeps it off.
	write(t, filepath.Join(dir, "other", "tools.js"), "console.log(2)")
	if p, err := s.AddPath(filepath.Join(dir, "other", "tools.js")); err != nil || p.Enabled || p.Source != filepath.Join(dir, "other", "tools.js") {
		t.Fatalf("replace: %+v, %v", p, err)
	}
	write(t, filepath.Join(dir, "empty.js"), "")
	write(t, filepath.Join(dir, "notes.txt"), "x")
	write(t, filepath.Join(dir, "big.js"), strings.Repeat("x", MaxPluginBytes+1))
	for _, bad := range []string{"relative.css", dir, filepath.Join(dir, "empty.js"), filepath.Join(dir, "notes.txt"), filepath.Join(dir, "big.js"), filepath.Join(dir, "nope.js")} {
		if _, err := s.AddPath(bad); err == nil {
			t.Errorf("%s was accepted", bad)
		}
	}
	// A file in the plugin folder itself is not linked to itself.
	if p, err := s.AddPath(filepath.Join(dir, "plugins", "tools.js")); err != nil || p.Source != "" {
		t.Fatalf("own copy linked: %+v, %v", p, err)
	}
	if err := s.SetEnabled("missing.js", true); err == nil {
		t.Fatal("a missing plugin was turned on")
	}
	if err := s.Delete("state.json"); err == nil {
		t.Fatal("the state file could be deleted")
	}
}

// The loader is one file for every page, kept by the browser when asked for by its version.
func TestLoaderIsCacheable(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "plugins"))
	mux := http.NewServeMux()
	s.Register(mux)
	for url, want := range map[string]string{LoaderURL: "immutable", LoaderPath: "no-cache"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", url, nil))
		if rec.Code != 200 || !strings.Contains(rec.Header().Get("Cache-Control"), want) || !strings.Contains(rec.Body.String(), "__agyPluginsSection") {
			t.Fatalf("%s: %d %q", url, rec.Code, rec.Header().Get("Cache-Control"))
		}
	}
	if !strings.Contains(LoaderURL, "?v="+loaderVersion) || len(loaderVersion) != 12 {
		t.Fatalf("loader URL %q", LoaderURL)
	}
}

func TestHandlers(t *testing.T) {
	dir := t.TempDir()
	s := New(filepath.Join(dir, "plugins"))
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
	body := `{"path":"` + src + `"}`
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
	file := do("GET", FilePrefix+"a.js?v="+resp.Plugins[0].Version, "", "")
	if file.Code != http.StatusOK || file.Body.String() != "alert(1)" || !strings.HasPrefix(file.Header().Get("Content-Type"), "application/javascript") || !strings.Contains(file.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("file: %d %q %s", file.Code, file.Body, file.Header())
	}
	write(t, src, "alert(2)")
	if got := do("GET", FilePrefix+"a.js", "", "").Body.String(); got != "alert(2)" {
		t.Fatalf("the file is not read from its source: %q", got)
	}
	if rec := do("GET", FilePrefix+"..%2fstate.json", "", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("path escape: %d", rec.Code)
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
	dir := t.TempDir()
	for _, d := range []string{"b-dir", ".hidden"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"z.css", "a.JS", "notes.txt"} {
		write(t, filepath.Join(dir, f), "x")
	}
	if err := os.Symlink(filepath.Join(dir, "b-dir"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	s := New(filepath.Join(t.TempDir(), "plugins")).WithWorkspace("/srv/work")
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
	if want := ".hidden/ b-dir/ link/ a.JS z.css"; strings.Join(got, " ") != want {
		t.Fatalf("entries %q, want %q", strings.Join(got, " "), want)
	}
	if f.Parent != filepath.Dir(dir) || f.Workspace != "/srv/work" || f.Home == "" {
		t.Fatalf("parent %q, workspace %q, home %q", f.Parent, f.Workspace, f.Home)
	}
	if _, err := s.Browse("relative/dir"); err == nil {
		t.Fatal("a relative path was listed")
	}
	if root, err := s.Browse("/"); err != nil || root.Parent != "" {
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
}
