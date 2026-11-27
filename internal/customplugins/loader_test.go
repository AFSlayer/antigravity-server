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
assert.ok(isPluginPath(" /x/Theme.CSS "));
assert.ok(!isPluginPath("/x/notes.txt"));
assert.match(removeQuestion({ name: "a.js", source: "/src/a.js" }), /\/src\/a\.js is left as it is/);
assert.match(removeQuestion({ name: "a.js", copiedFrom: "/work/a.js" }), /copy is deleted, and \/work\/a\.js is left/);
assert.match(removeQuestion({ name: "a.js" }), /only copy, and it is deleted/);
assert.strictEqual(sourceLine({ source: "/src/a.js", sourceMissing: true }), "cannot read /src/a.js, using the saved copy");
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
