package patches

import (
	"os/exec"
	"strings"
	"testing"
)

// The keyboard is predicted, and the chat shrunk ahead of it, only for a field
// the user just touched. Closing the model picker's bottom sheet hands focus
// back to the composer about 540 ms after the tap on its backdrop; predicting
// a keyboard for that made the chat jump up and settle back.
func TestKeyboardPredictionNeedsATouchOnTheField(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	start := strings.Index(keyboardDetect, "  var lastTouchTarget = null;")
	end := strings.Index(keyboardDetect, "  // When an objective option (radio) in ask_question is selected on mobile devices:")
	if start < 0 || end < start {
		t.Fatal("the touch tracking was not found")
	}
	harness := `
const assert = require("assert");
let clock = 100000;
const performance = { now: () => clock };
const listeners = {};
const document = { addEventListener: (type, fn) => { listeners[type] = fn; } };
let lastComposerTouchTime = 0;

// A tiny tree: the composer box holds the editor, which holds a paragraph.
const node = (parent, testid) => {
  const n = { parent, testid };
  n.contains = other => { for (let x = other; x; x = x.parent) if (x === n) return true; return false; };
  n.closest = sel => {
    const want = (sel.match(/data-testid="([^"]+)"/) || [])[1];
    for (let x = n; x; x = x.parent) if (x.testid === want) return x;
    return null;
  };
  return n;
};
const page = node(null, "page");
const box = node(page, "agent-input-box");
const padding = node(box, "padding");
const editor = node(box, "editor");
const paragraph = node(editor, "paragraph");
const backdrop = node(page, "bottom-sheet-backdrop");
const search = node(page, "search");
const touch = target => { listeners.touchstart({ target }); listeners.touchend({ target }); };
` + keyboardDetect[start:end] + `
// Nothing touched yet.
assert.strictEqual(justTouched(editor), false);

// A tap on the text, or on the composer around it, focuses the editor.
touch(paragraph);
clock += 120;
assert.strictEqual(justTouched(editor), true);
touch(padding);
clock += 120;
assert.strictEqual(justTouched(editor), true);
assert.strictEqual(lastComposerTouchTime, clock - 120);

// The model picker is closed by a tap on its backdrop, and focus comes back
// to the editor when the sheet has finished closing.
touch(backdrop);
clock += 540;
assert.strictEqual(justTouched(editor), false);

// A field outside the composer counts only when it was the one touched.
touch(search);
clock += 100;
assert.strictEqual(justTouched(search), true);
assert.strictEqual(justTouched(editor), false);

// A touch on the editor long ago does not count for a focus now.
touch(paragraph);
clock += 1500;
assert.strictEqual(justTouched(editor), false);

// A long press focuses the editor when it is lifted, and that counts.
listeners.touchstart({ target: paragraph });
clock += 1500;
listeners.touchend({ target: paragraph });
clock += 50;
assert.strictEqual(justTouched(editor), true);
process.exit(0);
`
	if out, err := exec.Command(node, "-e", harness).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
