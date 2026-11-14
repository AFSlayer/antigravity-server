package patches

import (
	"fmt"
	"strings"
	"testing"
)

// Antigravity 2.32.0 and 2.32.1 serve the same bundle. Each constant is the
// text one patch matches in it, with the bundle's own line breaks.
const (
	v232RowActions = `className:Gi("agy-row-actions absolute top-0 bottom-0 -right-1 pl-6 flex items-center justify-end gap-0.5 z-10",!h&&"[&>*]:pointer-events-auto",v?"hidden":Ka?
"opacity-100":"can-hover:opacity-0 group-hover:opacity-100 group-focus-visible:opacity-100 group-has-[:focus-visible]:opacity-100 has-[:focus-visible]:opacity-100")`

	// v232PathOfURI is the bundle's helper that turns a URI into a native path.
	v232PathOfURI = `function ppd(a){if(a.authority&&a.path.length>1)return` + "`" + `\\\\${a.authority}${a.path.replace(/\//g,"\\")}` + "`" + `;var b=/^\/([a-zA-Z]):(.*)$/.exec(a.path);return b?` + "`" + `${b[1].toLowerCase()}:${b[2].replace(/\//g,"\\")||"\\"}` + "`" + `:a.path}`
	// v232FolderPicker is cut after the picker's first option and closed there.
	v232FolderPicker = `function rpd(a,b){return{label:"Open Folder (Interactive)",id:"add-folder",isImportant:!0,addWorkspace:async c=>{var d=e,f=e;try{let g=c.showInteractiveFolderPicker,h=c.recordError,k=b?ppd(b):"/",l=f(await d(g({initialPath:k,fetchDirectoryContents:async m=>[]})));return l}finally{d()}}}}`

	v232DropCheck = `function XA(a,b=""){if(a&&Fy("",a))return!1;if(b.startsWith("image/")||b.startsWith("video/")||b.startsWith("audio/")||YZa(b,a)||g_a(b,a)||Gy(b,a))return!0;a=a.slice(a.lastIndexOf(".")+1).toLowerCase();return w4a.has(a)}`

	v232TextTypes = `function d_a(a,b){b=b.split(";")[0].trim().toLowerCase();if(b==="application/xml")return"text/xml";if(TZa.includes(b))return b;b=a.lastIndexOf(".");if(!(b<=0))return WZa[a.slice(b+1).toLowerCase()]}function Fy(a,b=""){return d_a(b,a)!==void 0}function e_a(a,b){a=a.slice(a.lastIndexOf(".")+1).toLowerCase();if(a!=="webm"&&a!=="mp4")return b=b.split(";")[0].trim().toLowerCase(),UZa.includes(b)?b:VZa[a]}`

	v232LivenessProbe = `function Fcc(a,b){var c=a.inboundFrameSeq;a.probeTimer=setTimeout(()=>{if(b.readyState!==WebSocket.OPEN)m0(a,b,` + "`" + `socket not open (readyState ${b.readyState})` + "`" + `);else if(Gcc(a,b))if(a.inboundFrameSeq!==c)Fcc(a,b);else{b.send(JSON.stringify({streamId:Hf(),type:"ping"}));var d=a.inboundFrameSeq;a.probeTimer=setTimeout(()=>{b.readyState!==WebSocket.OPEN?m0(a,b,` + "`" + `socket not open (readyState ${b.readyState})` + "`" + `):a.inboundFrameSeq!==d?Fcc(a,b):(n0(a),b.close(),m0(a,b,"liveness probe timed out"),a.options.recordError?.(xg.WEBSOCKET_LIVENESS_PROBE_FAILED,
Error("no frames for 7000ms and ping unanswered; closing socket")))},5E3)}else n0(a)},2E3)}`

	v232AuthCheck = `function t6a(a){if(a instanceof eh&&(a.code===14||a.code===2||a.code===4))return!0;a=a instanceof Error?a.message:String(a);return a.includes("Rpc failed due to xhr error")||a.includes("Failed to fetch")||a.includes("NetworkError")||a.includes("no such host")||a.includes("dial tcp")||a.includes("connection refused")||a.includes("i/o timeout")||a.includes("Precondition check failed")||a.includes("InstanceUnreachableError")||a.includes("Remote instance is unreachable")}
function DB(a,b,c){BB(a.sessionTracker);EB(a,{type:"SIGN_OUT"},b);c&&a.broadcastChannel?.broadcastSignOut()}function EB(a,b,c){var d=a._authState.state;a._authActor.send(b);b=a.analyticsService;a=a._authState.state;d==="signedIn"&&a!=="signedIn"&&c.trigger!=="user"&&n6a(b,wg.AUTH_BOUNCED,{from_state:d,to_state:a,trigger:c.trigger,failure_kind:c.trigger==="validate_invalid"?c.failureCase??"none":"none"})}
async function u6a(a,b=0){var c=e,d=e;try{if(!a.isLoggingOut){var f=a.sessionTracker.id,g=a._authState.state==="uninitialized";g&&a.startupAuth.attempts++;g=g?{timeoutMs:1E4}:void 0;try{let m=d(await c(a._lsClient.hasAuthToken({},g)));if(!a.isLoggingOut&&a.sessionTracker.id===f){var h=a._authState.state;h==="uninitialized"&&(a.startupAuth.hasToken=m.hasToken?"yes":"no");if(m.hasToken){var k=d(await c(a._lsClient.getAuthStatus({},g)));if(!a.isLoggingOut&&a.sessionTracker.id===f){var l=k.authResult;
if(l?.hasValidAuth){let n={isGcpTos:l.isGcpTos??!1,project:l.projectId??"",location:l.location??"",grantedScopes:l.grantedScopes??[],wifProvider:l.wifProvider,authMode:l.authMode},p=a._authState.context,q=p.grantedScopes??[],t=q.length!==n.grantedScopes.length||q.some((v,w)=>v!==n.grantedScopes[w]),u=p.isGcpTos!==n.isGcpTos||p.project!==n.project||p.location!==n.location||p.wifProvider!==n.wifProvider||p.authMode!==n.authMode||t;if(h!=="signedIn"||u)a._authActor.send({type:"AUTH_SUCCESS",tokenInfo:{accessToken:""},
scopes:n.grantedScopes,isGcpTos:n.isGcpTos,project:n.project,location:n.location,wifProvider:n.wifProvider,authMode:n.authMode}),a.broadcastChannel?.broadcastSignIn(n)}else d(await c(v6a(a,l,m,g)))}}else h!=="signedOut"&&h!=="validatingLogin"&&h!=="loginError"&&DB(a,{trigger:"validate_no_token"},!1)}}catch(m){if(d(),a.sessionTracker.id===f){if(a._authState.state==="uninitialized"){a.startupAuth.lastErrorCode=l6a(m);let n=t6a(m);if(b>0&&n){let p=4-b;d(await c(new Promise(q=>{setTimeout(q,1E3*2**p)})));
return a.isLoggingOut||a.sessionTracker.id!==f?void 0:u6a(a,b-1)}a.startupAuth.fallback=n?"transient_fallback":"error_fallback";a._authActor.send({type:"SIGN_OUT"})}a.recordError(xg.AUTH_STATUS_VALIDATION_FAILED,m)}}}}finally{c()}}`

	// v232UserStatusSync is cut after the poll's effect and closed there.
	v232UserStatusSync = `async function wV(a,b,c,d,f,g,h="clearStatus"){var k=e,l=e;try{try{if(typeof a.getUserStatus!=="function")h==="clearStatus"&&b(jr(Vb(E.UserStatusSchema)));else{var {userStatus:m}=l(await k(a.getUserStatus()));f()||(m&&(b(jr(Ypa(m,c))),m.email&&d?.(!m.disableTelemetry&&!m.userDataCollectionForceDisabled)),m?.email&&!m.cascadeModelConfigData?.errorMessage||g?.())}}catch(n){l(),console.warn("[UserStatusSync] Failed to fetch user status:",n),f()||(h==="clearStatus"&&b(jr(Vb(E.UserStatusSchema))),g?.())}}finally{k()}}
function tSb(){var a=yk(),b=kd()?.core.authService;return(0,B.useCallback)(()=>{tk(a.getState())?.state==="signedIn"&&b?.signOutIfServerHasNoCredential?.()},[a,b])}function uSb(){var a=zk(),b=yk(),c=(0,B.useContext)(jt),d=oj(),f=d.get("models")?.geminiModelsOnly===!0,g=d.get("settings")?.onTelemetrySettingChanged,h=tSb();return(0,B.useCallback)(async()=>{var k=e,l=e;try{if(c){var m=tk(b.getState());l(await k(wV(c,a,f,g,()=>tk(b.getState())!==m,h,"keepStatus")))}}finally{k()}},[a,b,c,f,g,h])}
function vSb(){var a=zk(),b=yk(),c=(0,B.useContext)(jt),d=Ak(tk),f=kd()?.core.authService,g=Al(),h=oj(),k=h.get("models")?.geminiModelsOnly===!0,l=h.get("settings")?.onTelemetrySettingChanged,m=tSb();(0,B.useEffect)(()=>{if(c)if(d?.state==="signedOut")a(jr(Vb(E.UserStatusSchema)));else{var q=!1,t=()=>q;wV(c,a,k,l,t,m);var u=setInterval(()=>{wV(c,a,k,l,t,m)},6E5);return()=>{q=!0;clearInterval(u)}}},[a,c,d,k,l,m]);return null}`
)

// applyWithStatus applies every patch to src and returns the result with the
// status of the patch id.
func applyWithStatus(t *testing.T, id, src string, opts Options) (string, Status) {
	t.Helper()
	out, report := Apply(MainJS, []byte(src), opts)
	for _, r := range report {
		if r.ID == id {
			return string(out), r.Status
		}
	}
	t.Fatalf("%s is not registered", id)
	return "", StatusMissing
}

// 2.32 moved, rewrapped or extended what these patches match.
func TestAntigravity232PatchShapes(t *testing.T) {
	// A line break between the "?" and the class it picks.
	t.Run("mobile-conversation-row-actions", func(t *testing.T) {
		got := applyOnly(t, "mobile-conversation-row-actions", v232RowActions, Options{MobileUX: true})
		want := `"[&>*]:pointer-events-auto",(v||Ka||Boolean(window.innerWidth<=768||(window.matchMedia&&window.matchMedia("(pointer:coarse)").matches)))?"opacity-100":"can-hover:opacity-0 group-hover:opacity-100 `
		if !strings.Contains(got, want) {
			t.Errorf("2.32 shape missing %q in:\n%s", want, got)
		}
	})
	// The path is put in a variable before the call.
	t.Run("folder-picker-initial-path", func(t *testing.T) {
		got := applyOnly(t, "folder-picker-initial-path", v232FolderPicker, Options{WorkspaceRoot: "/srv/work"})
		want := `k=b?ppd(b):"/",l=f(await d(g({initialPath:"/srv/work",fetchDirectoryContents:async m=>[]})))`
		if !strings.Contains(got, want) {
			t.Errorf("2.32 shape missing %q in:\n%s", want, got)
		}
	})
	// A name that is a text file is answered first.
	t.Run("file-drop-accept-all", func(t *testing.T) {
		if got, want := applyOnly(t, "file-drop-accept-all", v232DropCheck, Options{}), `function XA(a,b=""){return!0}`; got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	})
	// application/xml is answered first, and the extension is read only from
	// a name with a dot after its first character.
	t.Run("file-upload-custom-text-types", func(t *testing.T) {
		got := applyOnly(t, "file-upload-custom-text-types", v232TextTypes, Options{})
		for _, want := range []string{
			`function d_a(a,b){b=b.split(";")[0].trim().toLowerCase();if(b==="application/xml")return"text/xml";if(TZa.includes(b))return b;if(b.startsWith("text/"))return"text/plain";`,
			`return WZa[a]||(a==="md"`,
			`}function Fy(a,b=""){return d_a(b,a)!==void 0}function e_a(a,b){a=a.slice(a.lastIndexOf(".")+1).toLowerCase();if(a!=="webm"&&a!=="mp4")`,
		} {
			if !strings.Contains(got, want) {
				t.Errorf("2.32 shape missing %q in:\n%s", want, got)
			}
		}
	})
	// The ping is given 5E3, and the message says 7000ms.
	t.Run("websocket-liveness-probe-relax", func(t *testing.T) {
		got := applyOnly(t, "websocket-liveness-probe-relax", v232LivenessProbe, Options{})
		if want := `closing socket")))},Math.max(3E4,5E3))}else n0(a)},2E3)}`; !strings.Contains(got, want) {
			t.Errorf("2.32 shape missing %q in:\n%s", want, got)
		}
	})
	// The error code is recorded before the retry decision, and a line break
	// comes before the return.
	t.Run("auth-startup-retry", func(t *testing.T) {
		got := applyOnly(t, "auth-startup-retry", v232AuthCheck, Options{})
		for _, want := range []string{
			`{a.startupAuth.lastErrorCode=l6a(m);let n=t6a(m);if(b>0&&n){`,
			`u6a(a,b>1?b-1:Date.now()<(a.__agyAuthRetryUntil??=Date.now()+3E5)?1:0)}a.startupAuth.fallback=n?"transient_fallback":"error_fallback";`,
		} {
			if !strings.Contains(got, want) {
				t.Errorf("2.32 shape missing %q in:\n%s", want, got)
			}
		}
	})
	// The status is cleared only in "clearStatus" mode, which is the default
	// and what the poll runs in.
	t.Run("user-status-keep-on-error", func(t *testing.T) {
		got := applyOnly(t, "user-status-keep-on-error", v232UserStatusSync, Options{})
		for _, want := range []string{
			`catch(n){return l(),console.warn("[UserStatusSync] Failed to fetch user status:",n),f()||g?.(),!1}}finally{k()}}`,
			`_ok=(await wV(c,a,k,l,t,m))!==!1`,
			`_agyRun(!0);var u=setInterval(()=>{_agyRun(!1)},6E5);`,
			`return()=>{q=!0;clearInterval(u);clearTimeout(_agyRetry);`,
		} {
			if !strings.Contains(got, want) {
				t.Errorf("2.32 shape missing %q in:\n%s", want, got)
			}
		}
	})
}

// lineBreakPositions lists the offsets in minified src where the minifier may
// start a new line: after a comma, a semicolon, the closing brace of a block,
// or a binary, ternary or assignment operator, outside string and template
// literals. These are the places the line breaks of the 2.32 bundle fall.
func lineBreakPositions(src string) []int {
	var out []int
	var quote byte
	for i := 0; i < len(src); i++ {
		c := src[i]
		if quote != 0 {
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		var prev, next byte
		if i > 0 {
			prev = src[i-1]
		}
		if i+1 < len(src) {
			next = src[i+1]
		}
		ok := false
		switch c {
		case '"', '\'', '`':
			quote = c
		case ',', ';', ':':
			ok = true
		case '}':
			ok = isJSIdentByte(next) || next == ';'
		case '=':
			ok = next != '=' && next != '>'
		case '<', '>':
			ok = next != '=' && next != c
		case '?':
			ok = next != '.' && next != '?' && next != '='
		case '&', '|':
			ok = prev == c && next != '='
		case '*':
			ok = next != '*' && next != '='
		case '+', '-':
			// Binary only: after an operand, and not half of ++ or --.
			ok = next != c && next != '=' && (isJSIdentByte(prev) || prev == ')' || prev == ']')
		}
		if ok && next != 0 {
			out = append(out, i+1)
		}
	}
	return out
}

// A release breaks the bundle's long lines in different places, which is what
// stopped most of these patches matching 2.32. Each must apply the same way
// with a line break at any place the minifier could put one, in the 2.32
// form and in the form of earlier releases.
func TestAntigravity232PatchesTolerateLineBreaks(t *testing.T) {
	type breakCase struct {
		id, src string
		opts    Options
	}
	cases := []breakCase{
		{"mobile-conversation-row-actions", v232RowActions, Options{MobileUX: true}},
		{"folder-picker-initial-path", v232FolderPicker, Options{WorkspaceRoot: "/srv/work"}},
		{"file-drop-accept-all", v232DropCheck, Options{}},
		{"file-upload-custom-text-types", v232TextTypes, Options{}},
		{"websocket-liveness-probe-relax", v232LivenessProbe, Options{}},
		{"auth-startup-retry", v232AuthCheck, Options{}},
		{"user-status-keep-on-error", v232UserStatusSync, Options{}},
	}
	for _, c := range cases[:len(cases):len(cases)] {
		cases = append(cases, breakCase{c.id, regexpFixtures[c.id], c.opts})
	}
	compact := func(s string) string { return strings.Join(strings.Fields(s), "") }
	for _, c := range cases {
		base, status := applyWithStatus(t, c.id, c.src, c.opts)
		if status != StatusApplied {
			t.Errorf("%s: status %v without a line break", c.id, status)
			continue
		}
		positions := lineBreakPositions(c.src)
		if len(positions) < 3 {
			t.Errorf("%s: only %d break positions in %q", c.id, len(positions), c.src)
		}
		for _, pos := range positions {
			out, status := applyWithStatus(t, c.id, c.src[:pos]+"\n"+c.src[pos:], c.opts)
			if status != StatusApplied || compact(out) != compact(base) {
				t.Errorf("%s: status %v with a line break at offset %d: %q", c.id, status, pos, c.src[max(0, pos-30):min(len(c.src), pos+30)])
				break
			}
		}
	}
}

// The folder picker opens at the configured root in both forms of the call,
// and the root is written as it is.
func TestFolderPickerOpensAtWorkspaceRoot(t *testing.T) {
	const root = `/srv/$1 work/"quoted"`
	opts := Options{WorkspaceRoot: root}
	old := applyOnly(t, "folder-picker-initial-path", `var o={initialPath:b?b.fsPath:g?"C:/":"/",fetchDirectoryContents:f};`, opts)
	if want := `var o={initialPath:"/srv/$1 work/\"quoted\"",fetchDirectoryContents:f};`; old != want {
		t.Errorf("earlier form: got %s, want %s", old, want)
	}
	patched := applyOnly(t, "folder-picker-initial-path", v232FolderPicker, opts)
	runNode(t, fmt.Sprintf(`"use strict";
const assert = require("assert");
const load = (src) => new Function("e", src + "; return rpd;")((x) => x);
const openedBy = async (rpd) => {
  let opened;
  const picker = async (o) => { opened = o.initialPath; };
  await rpd({}, { authority: "", path: "/home/me" }).addWorkspace({ showInteractiveFolderPicker: picker, recordError() {} });
  return opened;
};
(async () => {
  assert.strictEqual(await openedBy(load(%s)), "/home/me");
  assert.strictEqual(await openedBy(load(%s)), %s);
})().catch((err) => { console.error(err); process.exit(1); });
`, jsString(v232PathOfURI+v232FolderPicker), jsString(v232PathOfURI+patched), jsString(root)))
}

// The text type function answers what the bundle's own did and what the
// patch adds, in the 2.32 form and the earlier one.
func TestCustomTextTypesInBothForms(t *testing.T) {
	old := applyOnly(t, "file-upload-custom-text-types", regexpFixtures["file-upload-custom-text-types"], Options{})
	v232 := applyOnly(t, "file-upload-custom-text-types", v232TextTypes, Options{})
	runNode(t, fmt.Sprintf(`"use strict";
const assert = require("assert");
const mimes = ["text/plain", "text/markdown"], table = { md: "text/markdown", py: "text/x-python" };
const old = new Function("UEa", "VEa", %s + "; return WEa;")(mimes, table);
const v232 = new Function("TZa", "WZa", "UZa", "VZa", %s + "; return { type: d_a, isText: Fy };")(mimes, table, [], {});
for (const [form, type] of [["earlier", old], ["2.32", v232.type]]) {
  // What the bundle's own function answers.
  assert.strictEqual(type("notes.md", ""), "text/markdown", form);
  assert.strictEqual(type("tool.py", "application/octet-stream"), "text/x-python", form);
  assert.strictEqual(type("x", "text/markdown; charset=utf-8"), "text/markdown", form);
  assert.strictEqual(type("blob.bin", "application/octet-stream"), undefined, form);
  assert.strictEqual(type("README", ""), undefined, form);
  // What the patch adds.
  assert.strictEqual(type("trace.log", "text/x-log"), "text/plain", form);
  assert.strictEqual(type("session.har", ""), "application/json", form);
  assert.strictEqual(type("rows.jsonl", "application/octet-stream"), "application/json", form);
  assert.strictEqual(type("Dockerfile", ""), "text/plain", form);
  assert.strictEqual(type("C:\\src\\Makefile", ""), "text/plain", form);
  assert.strictEqual(type(".env", ""), "text/plain", form);
  assert.strictEqual(type("", ""), undefined, form);
}
// 2.32 answers text/xml for application/xml itself, and that answer is kept.
assert.strictEqual(v232.type("feed.xml", "application/xml"), "text/xml");
assert.strictEqual(old("feed.xml", "application/xml"), "text/plain");
// 2.32 asks the same function whether a dropped file is text.
assert.strictEqual(v232.isText("", "session.har"), true);
assert.strictEqual(v232.isText("application/octet-stream", "blob.bin"), false);
`, jsString(old), jsString(v232)))
}

// A bundle that retries the startup sign-in check in a form the patch does
// not match is reported, and one that does not retry is left alone.
func TestAuthStartupRetryReportsAnUnknownForm(t *testing.T) {
	drifted := strings.Replace(v232AuthCheck, "let p=4-b;", "let p=triesLeft(b);", 1)
	if drifted == v232AuthCheck {
		t.Fatal("the 2.32 check no longer has the countdown this test rewrites")
	}
	if out, status := applyWithStatus(t, "auth-startup-retry", drifted, Options{}); status != StatusMissing || out != drifted {
		t.Errorf("a retry in an unknown form: status %v, changed %v", status, out != drifted)
	}
	const noRetry = `catch(m){if(a._authState.state==="uninitialized"){a._authActor.send({type:"SIGN_OUT"})}a.recordError(m)}`
	if out, status := applyWithStatus(t, "auth-startup-retry", noRetry, Options{}); status != StatusNotNeeded || out != noRetry {
		t.Errorf("no retry: status %v, changed %v", status, out != noRetry)
	}
}
