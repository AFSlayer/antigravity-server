package customplugins

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The loader's path and wording helpers, run in node: paths in the server's own form, on Linux and macOS and on
// Windows with drives and shares, and the remove question for each kind of plugin.
func TestLoaderHelpers(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	start := strings.Index(loaderScript, "  function isPluginPath(")
	end := strings.Index(loaderScript, "  // Material icon outlines")
	if start < 0 || end < start {
		t.Fatal("the helpers were not found in loader.js")
	}
	harness := loaderScript[start:end] + `
const assert = require("assert");
const labels = (d) => crumbs(d).map((c) => c.label + "=" + c.path).join(" ");
assert.strictEqual(labels("/"), "/=/");
assert.strictEqual(labels("/home/me"), "/=/ home=/home me=/home/me");
assert.strictEqual(labels("C:\\"), "C:=C:\\");
assert.strictEqual(labels("C:\\Users\\me"), "C:=C:\\ Users=C:\\Users me=C:\\Users\\me");
assert.strictEqual(labels("\\\\host\\share\\dir"), "\\\\host\\share=\\\\host\\share\\ dir=\\\\host\\share\\dir");
assert.strictEqual(join("/", "a.js"), "/a.js");
assert.strictEqual(join("/home/me/", "a.js"), "/home/me/a.js");
assert.strictEqual(join("C:\\", "a.js"), "C:\\a.js");
assert.strictEqual(join("C:\\Users", "a.js"), "C:\\Users\\a.js");
assert.strictEqual(join("\\\\host\\share", "a.js"), "\\\\host\\share\\a.js");
assert.strictEqual(listOf(["/w"]), "/w");
assert.strictEqual(listOf(["/w", "/g"]), "/w or /g");
assert.strictEqual(listOf(["/w", "/g", "/a"]), "/w, /g or /a");
assert.ok(isPluginPath(" /x/Theme.CSS "));
assert.ok(!isPluginPath("/x/notes.txt"));
assert.match(removeQuestion({ name: "a.js", source: "/src/a.js" }), /\/src\/a\.js is left as it is/);
assert.match(removeQuestion({ name: "a.js", copiedFrom: "/work/a.js" }), /copy is deleted, and \/work\/a\.js is left/);
assert.match(removeQuestion({ name: "a.js" }), /only copy, and it is deleted/);
assert.match(removeQuestion({ name: "a.js", source: "/src/a.js", sourceMissing: true }), /only copy, and it is deleted/);
assert.strictEqual(sourceLine({ source: "/src/a.js", sourceMissing: true, sourceProblem: "/src/a.js is not there" }), "/src/a.js is not there, so the saved copy is used");
assert.strictEqual(sourceLine({ source: "/src/a.js", sourceMissing: true }), "cannot read /src/a.js, so the saved copy is used");
assert.strictEqual(sourceLine({ copiedFrom: "/work/a.js" }), "copied from /work/a.js");
assert.strictEqual(sourceLine({}), "");
`
	path := filepath.Join(t.TempDir(), "helpers.js")
	if err := os.WriteFile(path, []byte(harness), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, path).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

// The loader itself, run in node with a page faked around it: it loads the plugins that are on, in their order, and
// none in safe mode, which ?agy-safe=1 starts for the tab and ?agy-safe=0 ends. The styles on last time are put in
// before the list arrives, kept when the list asks for the same, and swapped when it does not.
func TestLoaderLoadsWhatIsOn(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	dir := t.TempDir()
	loader := filepath.Join(dir, "loader.js")
	if err := os.WriteFile(loader, []byte(loaderScript), 0o600); err != nil {
		t.Fatal(err)
	}
	harness := `
const assert = require("assert");
const fs = require("fs");
const vm = require("vm");
const src = fs.readFileSync(process.argv[2], "utf8");
const session = new Map(), local = new Map();
let plugins = [
  { name: "b.js", kind: "js", enabled: true, version: "v1" },
  { name: "off.css", kind: "css", enabled: false, version: "v2" },
  { name: "a theme.css", kind: "css", enabled: true, version: "v3" },
];
function run(search) {
  const appended = [], fetched = [];
  const storage = (m) => ({ getItem: (k) => (m.has(k) ? m.get(k) : null), setItem: (k, v) => m.set(k, String(v)), removeItem: (k) => m.delete(k) });
  const head = {
    children: [],
    appendChild(el) { el.parentNode = head; head.children.push(el); appended.push(el); },
    removeChild(el) { head.children.splice(head.children.indexOf(el), 1); el.parentNode = null; },
  };
  const ctx = {
    location: { search, href: "http://agy.test/" + search },
    sessionStorage: storage(session),
    localStorage: storage(local),
    URLSearchParams, URL, console,
    document: { createElement: (tag) => ({ tag, setAttribute(k, v) { this[k] = v; } }), head },
    fetch: (url) => { fetched.push(url); return Promise.resolve({ ok: true, json: () => Promise.resolve({ plugins }) }); },
  };
  ctx.window = ctx;
  vm.runInNewContext(src, ctx);
  const before = appended.map((e) => e["data-agy-custom-plugin"]);
  return new Promise((resolve) => setTimeout(() => resolve({ appended, fetched, ctx, before, head }), 20));
}
const names = (els) => els.map((e) => e["data-agy-custom-plugin"] + "@" + (e.src || e.href).split("v=")[1]);
(async () => {
  let r = await run("");
  assert.deepStrictEqual(r.appended.map((e) => e["data-agy-custom-plugin"]), ["b.js", "a theme.css"]);
  assert.strictEqual(r.appended[0].tag, "script");
  assert.strictEqual(r.appended[0].async, false);
  assert.strictEqual(r.appended[0].src, "/__agy/custom-plugins/file/b.js?v=v1");
  assert.strictEqual(r.appended[1].tag, "link");
  assert.strictEqual(r.appended[1].rel, "stylesheet");
  assert.strictEqual(r.appended[1].href, "/__agy/custom-plugins/file/a%20theme.css?v=v3");
  assert.strictEqual(typeof r.ctx.__agyCustomPluginsSection, "function");

  r = await run("?agy-safe=1");
  assert.strictEqual(r.appended.length, 0);
  assert.strictEqual(r.fetched.length, 0);
  assert.strictEqual(r.ctx.__agyCustomPluginsSafe, true);
  r = await run("");
  assert.strictEqual(r.appended.length, 0, "safe mode lasts for the tab");
  r = await run("?agy-safe=0");
  assert.strictEqual(r.ctx.__agyCustomPluginsSafe, false);
  assert.deepStrictEqual(names(r.head.children), ["a theme.css@v3", "b.js@v1"], "agy-safe=0 ends safe mode");

  // The style on last time went in before the list came, and was not put in twice.
  assert.deepStrictEqual(r.before, ["a theme.css"]);
  assert.deepStrictEqual(Array.from(r.ctx.__agyCustomPluginsLoaded), ["a theme.css", "b.js"]);

  // A new version of the style: the old one is taken out once the list says so.
  plugins = plugins.map((p) => (p.name === "a theme.css" ? { ...p, version: "v4" } : p));
  r = await run("");
  assert.deepStrictEqual(r.before, ["a theme.css"]);
  assert.deepStrictEqual(names(r.head.children), ["b.js@v1", "a theme.css@v4"]);
  assert.deepStrictEqual(Array.from(r.ctx.__agyCustomPluginsLoaded), ["b.js", "a theme.css"]);

  // Turned off elsewhere: it is taken out, and not put in next time.
  plugins = plugins.map((p) => (p.name === "a theme.css" ? { ...p, enabled: false } : p));
  r = await run("");
  assert.deepStrictEqual(names(r.head.children), ["b.js@v1"]);
  r = await run("");
  assert.deepStrictEqual(r.before, []);
})().catch((e) => { console.error(e); process.exit(1); });
`
	path := filepath.Join(dir, "harness.js")
	if err := os.WriteFile(path, []byte(harness), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, path, loader).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
