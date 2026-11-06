package patches

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// authCheck is Antigravity 2.29.0's startup sign-in check, as the bundle ships it.
const authCheck = `function j0a(a){if(a instanceof yh&&(a.code===14||a.code===2||a.code===4))return!0;a=a instanceof Error?a.message:String(a);return a.includes("Rpc failed due to xhr error")||a.includes("Failed to fetch")||a.includes("NetworkError")||a.includes("no such host")||a.includes("dial tcp")||a.includes("connection refused")||a.includes("i/o timeout")||a.includes("Precondition check failed")||a.includes("InstanceUnreachableError")||a.includes("Remote instance is unreachable")}
function SD(a,b=!0){a._authActor.send({type:"SIGN_OUT"});b&&a.broadcastChannel?.broadcastSignOut()}
async function k0a(a,b=0){var c=e,d=e;try{if(!a.isLoggingOut){var f=a._currentSessionId,g=a._authState.state==="uninitialized"?{timeoutMs:1E4}:void 0;try{let m=d(await c(a._lsClient.hasAuthToken({},g)));if(!a.isLoggingOut&&a._currentSessionId===f){var h=a._authState.state;if(m.hasToken){var k=d(await c(a._lsClient.getAuthStatus({},g)));if(!a.isLoggingOut&&a._currentSessionId===f){var l=k.authResult;if(l?.hasValidAuth){let n={isGcpTos:l.isGcpTos??!1,project:l.projectId??"",location:l.location??"",
grantedScopes:l.grantedScopes??[],wifProvider:l.wifProvider,authMode:l.authMode},p=a._authState.context,r=p.grantedScopes??[],u=r.length!==n.grantedScopes.length||r.some((v,w)=>v!==n.grantedScopes[w]),t=p.isGcpTos!==n.isGcpTos||p.project!==n.project||p.location!==n.location||p.wifProvider!==n.wifProvider||p.authMode!==n.authMode||u;if(h!=="signedIn"||t)a._authActor.send({type:"AUTH_SUCCESS",tokenInfo:{accessToken:""},scopes:n.grantedScopes,isGcpTos:n.isGcpTos,project:n.project,location:n.location,
wifProvider:n.wifProvider,authMode:n.authMode}),a.broadcastChannel?.broadcastSignIn(n)}else d(await c(l0a(a,l,m,g)))}}else h!=="signedOut"&&h!=="validatingLogin"&&SD(a,!1)}}catch(m){if(d(),a._currentSessionId===f){if(a._authState.state==="uninitialized"){let n=j0a(m);if(b>0&&n){let p=4-b;d(await c(new Promise(r=>{setTimeout(r,1E3*2**p)})));return a.isLoggingOut||a._currentSessionId!==f?void 0:k0a(a,b-1)}a._authActor.send({type:"SIGN_OUT"})}a.recordError(Ng.AUTH_STATUS_VALIDATION_FAILED,m)}}}}finally{c()}}
`

func TestAuthStartupRetryOutlastsASlowLink(t *testing.T) {
	patched := applyOnly(t, "auth-startup-retry", authCheck, Options{})
	if patched == authCheck {
		t.Fatal("anchor not found in the 2.29.0 check")
	}
	if !strings.Contains(patched, "if(n){let p=Math.min(4-b,3);") {
		t.Fatalf("unexpected rewrite: %s", patched)
	}

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	harness := `
const assert = require("assert");
const fs = require("fs");
class yh extends Error { constructor(code) { super("rpc failed"); this.code = code; } }
const e = x => x;
const Ng = {};
const waits = [];
global.setTimeout = (fn, ms) => { waits.push(ms); fn(); };
const l0a = async () => {};
eval(fs.readFileSync(process.argv[1], "utf8") + "; globalThis.check = k0a;");

// Fails the token check failures times with the given code, then succeeds.
async function run(failures, code) {
  let calls = 0;
  const sent = [];
  const app = {
    isLoggingOut: false,
    _currentSessionId: 1,
    _authState: { state: "uninitialized", context: { grantedScopes: [] } },
    _authActor: { send: ev => sent.push(ev.type) },
    recordError() {},
    _lsClient: {
      hasAuthToken: async () => { calls++; if (calls <= failures) throw new yh(code); return { hasToken: true }; },
      getAuthStatus: async () => ({ authResult: { hasValidAuth: true, grantedScopes: [] } }),
    },
  };
  waits.length = 0;
  await check(app, 4);
  return { calls, sent, waits: waits.slice() };
}

(async () => {
  const mode = process.argv[2];
  // Twelve timeouts in a row, about two minutes on a real link.
  const slow = await run(12, 4);
  if (mode === "patched") {
    assert.deepStrictEqual(slow.sent, ["AUTH_SUCCESS"]);
    assert.strictEqual(slow.calls, 13);
    assert.deepStrictEqual(slow.waits, [1000, 2000, 4000, 8000, 8000, 8000, 8000, 8000, 8000, 8000, 8000, 8000]);
  } else {
    assert.deepStrictEqual(slow.sent, ["SIGN_OUT"], "upstream gives up after five tries");
    assert.strictEqual(slow.calls, 5);
  }
  // A failure the bundle does not count as transient still signs out at once.
  const refused = await run(1, 16);
  assert.deepStrictEqual(refused.sent, ["SIGN_OUT"]);
  assert.strictEqual(refused.calls, 1);
  // A quick success is untouched.
  assert.deepStrictEqual((await run(0, 4)).sent, ["AUTH_SUCCESS"]);
})().catch(err => { console.error(err); process.exit(1); });
`
	dir := t.TempDir()
	for mode, body := range map[string]string{"patched": patched, "upstream": authCheck} {
		path := filepath.Join(dir, mode+".js")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(node, "-e", harness, "--", path, mode).CombinedOutput(); err != nil {
			t.Errorf("%s: %v\n%s", mode, err, out)
		}
	}
}
