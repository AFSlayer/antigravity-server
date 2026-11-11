package patches

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	v228UserStatusSync = `function lnb(){var a=kl(),b=(0,A.useContext)(kB),c=ll(dl),d=Fj(),f=d.get("models")?.geminiModelsOnly===!0,g=d.get("settings")?.onTelemetrySettingChanged;(0,A.useEffect)(()=>{if(b){var l=!1,m=async()=>{var q=e,r=e;try{if(c?.state==="signedOut")a(ft(Vb(B.UserStatusSchema)));else try{let {userStatus:t}=r(await q(b.getUserStatus()));!l&&t&&(a(ft(Dma(t,f))),t.email&&g?.(!t.disableTelemetry&&!t.userDataCollectionForceDisabled))}catch(t){r(),console.warn("[UserStatusSync] Failed to fetch user status:",t)}}finally{q()}};` + "\n" + `m();var n=setInterval(()=>{m()},6E5);return()=>{l=!0;clearInterval(n)}}},[a,b,c,f,g]);return null}`

	v229UserStatusSync = `async function aCb(a,b,c,d,f){var g=e,h=e;try{try{if(typeof a.getUserStatus!=="function")b(lu($b(D.UserStatusSchema)));else{var {userStatus:k}=h(await g(a.getUserStatus()));!f()&&k&&(b(lu(fna(k,c))),k.email&&d?.(!k.disableTelemetry&&!k.userDataCollectionForceDisabled))}}catch(l){h(),console.warn("[UserStatusSync] Failed to fetch user status:",l),f()||b(lu($b(D.UserStatusSchema)))}}finally{g()}}` + "\n" + `function bCb(){var a=Pl(),b=Ol(),c=(0,A.useContext)(kB),d=Ql(Il),f=sd()?.core.authService,g=fn(),h=hk(),k=h.get("models")?.geminiModelsOnly===!0,l=h.get("settings")?.onTelemetrySettingChanged;(0,A.useEffect)(()=>{if(c)if(d?.state==="signedOut")a(lu($b(D.UserStatusSchema)));else{var p=!1,r=()=>p;aCb(c,a,k,l,r);var u=setInterval(()=>{aCb(c,a,k,l,r)},6E5);return()=>{p=!0;clearInterval(u)}}},[a,c,d,k,l]);return null}`

	v231UserStatusSync = `async function RPb(a,b,c,d,f,g){var h=e,k=e;try{try{if(typeof a.getUserStatus!=="function")b(Xq(Wb(E.UserStatusSchema)));else{var {userStatus:l}=k(await h(a.getUserStatus()));f()||(l&&(b(Xq(Opa(l,c))),l.email&&d?.(!l.disableTelemetry&&!l.userDataCollectionForceDisabled)),l?.email&&!l.cascadeModelConfigData?.errorMessage||g?.())}}catch(m){k(),console.warn("[UserStatusSync] Failed to fetch user status:",m),f()||(b(Xq(Wb(E.UserStatusSchema))),g?.())}}finally{h()}}` + "\n" + `function SPb(){var a=nk(),b=mk(),c=(0,B.useContext)(Pt),d=ok(ik),f=fd()?.core.authService,g=ol(),h=gj(),k=h.get("models")?.geminiModelsOnly===!0,l=h.get("settings")?.onTelemetrySettingChanged;(0,B.useEffect)(()=>{if(c)if(d?.state==="signedOut")a(Xq(Wb(E.UserStatusSchema)));else{var p=!1,q=()=>p,t=()=>{ik(b.getState())?.state==="signedIn"&&f?.signOutIfServerHasNoCredential?.()};RPb(c,a,k,l,q,t);var u=setInterval(()=>{RPb(c,a,k,l,q,t)},6E5);return()=>{p=!0;clearInterval(u)}}},[a,c,d,f,b,k,l]);return null}`
)

func applyUserStatusPatch(t *testing.T, src string) (string, Status) {
	t.Helper()
	out, report := Apply(MainJS, []byte(src), Options{})
	for _, r := range report {
		if r.ID == "user-status-keep-on-error" {
			return string(out), r.Status
		}
	}
	t.Fatal("user-status-keep-on-error is not registered")
	return "", StatusMissing
}

func TestUserStatusKeepOnErrorShapesAndGuards(t *testing.T) {
	t.Run("2.28 leaves userStatus untouched and reports not needed", func(t *testing.T) {
		if !userStatusNoResetOnErrorRe.MatchString(v228UserStatusSync) {
			t.Error("2.28 UserStatusSync should match userStatusNoResetOnErrorRe")
		}
		if userStatusKeepOnErrorRe.MatchString(v228UserStatusSync) {
			t.Error("2.28 UserStatusSync should not match userStatusKeepOnErrorRe")
		}
		out, status := applyUserStatusPatch(t, v228UserStatusSync)
		if status != StatusNotNeeded || out != v228UserStatusSync {
			t.Errorf("got status=%s changed=%v, want not needed and unchanged", status, out != v228UserStatusSync)
		}
	})

	t.Run("2.29 applies without credential callback", func(t *testing.T) {
		if userStatusNoResetOnErrorRe.MatchString(v229UserStatusSync) {
			t.Error("2.29 UserStatusSync should not match userStatusNoResetOnErrorRe")
		}
		out, status := applyUserStatusPatch(t, v229UserStatusSync)
		if status != StatusApplied {
			t.Fatalf("got status %s, want applied", status)
		}
		if strings.Contains(out, `catch(l){h(),console.warn`) || !strings.Contains(out, `catch(l){return h(),console.warn("[UserStatusSync] Failed to fetch user status:",l),!1}`) {
			t.Errorf("unexpected 2.29 catch rewrite:\n%s", out)
		}
	})

	t.Run("2.31 Replace matches ReplaceMatch byte-for-byte", func(t *testing.T) {
		var patch Patch
		for _, p := range All() {
			if p.ID == "user-status-keep-on-error" {
				patch = p
				break
			}
		}
		loc := patch.FindRe.FindSubmatchIndex([]byte(v231UserStatusSync))
		if loc == nil {
			t.Fatal("2.31 fixture did not match")
		}
		expanded := string(patch.FindRe.Expand(nil, []byte(patch.Replace), []byte(v231UserStatusSync), loc))
		fromMatch := patch.ReplaceMatch(submatches([]byte(v231UserStatusSync), loc), Options{})
		if expanded != fromMatch {
			t.Errorf("Replace and ReplaceMatch diverged on 2.31:\nReplace:      %s\nReplaceMatch: %s", expanded, fromMatch)
		}
	})

	t.Run("line breaks between minified tokens still match and parse", func(t *testing.T) {
		wrapped := strings.NewReplacer(
			";", ";\n",
			",", ",\n",
			"||", "||\n",
			"&&", "&&\n",
			"=>", "=>\n",
			"===", "\n===\n",
			"!==", "\n!==\n",
		).Replace(v231UserStatusSync)
		out, status := applyUserStatusPatch(t, wrapped)
		if status != StatusApplied {
			t.Fatalf("wrapped 2.31 source did not match: status=%s", status)
		}
		if node, err := exec.LookPath("node"); err == nil {
			f := filepath.Join(t.TempDir(), "wrapped.js")
			if err := os.WriteFile(f, []byte(out), 0600); err != nil {
				t.Fatal(err)
			}
			if checkOut, err := exec.Command(node, "--check", f).CombinedOutput(); err != nil {
				t.Fatalf("syntax error in wrapped output: %v\n%s", err, checkOut)
			}
		}
	})

	t.Run("reset moved before console.warn is rejected by both guard regexes", func(t *testing.T) {
		oldCatch := `catch(m){k(),console.warn("[UserStatusSync] Failed to fetch user status:",m),f()||(b(Xq(Wb(E.UserStatusSchema))),g?.())}`
		for _, driftedCatch := range []string{
			`catch(m){k(),b(Xq(Wb(E.UserStatusSchema))),console.warn("[UserStatusSync] Failed to fetch user status:",m)}`,
			`catch(m){b(Xq(Wb(E.UserStatusSchema)));console.warn("[UserStatusSync] Failed to fetch user status:",m),f()||g?.()}`,
		} {
			drifted := strings.Replace(v231UserStatusSync, oldCatch, driftedCatch, 1)
			if drifted == v231UserStatusSync {
				t.Fatal("failed to construct drifted catch fixture")
			}
			if userStatusKeepOnErrorRe.MatchString(drifted) {
				t.Errorf("userStatusKeepOnErrorRe unexpectedly matched drifted catch:\n%s", driftedCatch)
			}
			if userStatusNoResetOnErrorRe.MatchString(drifted) {
				t.Errorf("userStatusNoResetOnErrorRe unexpectedly matched drifted catch:\n%s", driftedCatch)
			}
		}
	})
}

func TestUserStatusKeepOnErrorRuntime(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}

	patched231, status := applyUserStatusPatch(t, v231UserStatusSync)
	if status != StatusApplied {
		t.Fatalf("2.31 patch status = %s, want applied", status)
	}
	patched229, status := applyUserStatusPatch(t, v229UserStatusSync)
	if status != StatusApplied {
		t.Fatalf("2.29 patch status = %s, want applied", status)
	}

	dir := t.TempDir()
	upFile := filepath.Join(dir, "upstream231.js")
	p231File := filepath.Join(dir, "patched231.js")
	p229File := filepath.Join(dir, "patched229.js")
	harnessFile := filepath.Join(dir, "harness.cjs")

	for name, content := range map[string]string{
		upFile:   v231UserStatusSync,
		p231File: patched231,
		p229File: patched229,
	} {
		if err := os.WriteFile(name, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}

	harness := `
const assert = require("assert");
const fs = require("fs");

const upstream231Src = fs.readFileSync(process.argv[2], "utf8");
const patched231Src = fs.readFileSync(process.argv[3], "utf8");
const patched229Src = fs.readFileSync(process.argv[4], "utf8");

function createEnv(source, version) {
  let now = 1_000_000;
  let nextTimerId = 1;
  const timers = new Map();
  const docListeners = new Map();
  const winListeners = new Map();
  let visibilityState = "visible";

  const fakeDate = { now: () => now };
  const setTimer = (fn, ms, repeat) => {
    const id = nextTimerId++;
    const delay = Math.max(0, Number(ms) || 0);
    timers.set(id, { id, fn, due: now + delay, delay, repeat });
    return id;
  };
  const clearTimer = id => { timers.delete(id); };

  const fakeDoc = {
    get visibilityState() { return visibilityState; },
    addEventListener(type, fn) {
      if (!docListeners.has(type)) docListeners.set(type, new Set());
      docListeners.get(type).add(fn);
    },
    removeEventListener(type, fn) {
      docListeners.get(type)?.delete(fn);
    },
  };
  const fakeWin = {
    addEventListener(type, fn) {
      if (!winListeners.has(type)) winListeners.set(type, new Set());
      winListeners.get(type).add(fn);
    },
    removeEventListener(type, fn) {
      winListeners.get(type)?.delete(fn);
    },
  };

  const emptyStatus = () => ({ email: "", cascadeModelConfigData: undefined });
  let state = {
    auth: { state: "signedIn" },
    userStatus: emptyStatus(),
    selectedModelValue: 101,
  };
  const dispatches = [];
  const dispatch = action => {
    dispatches.push(action);
    if (action && action.type === "setUserStatus") {
      state.userStatus = action.payload;
    }
  };
  const store = { getState: () => state };

  let rpcImpl = async () => ({
    userStatus: {
      email: "dev@example.com",
      cascadeModelConfigData: { clientModelConfigs: [{ label: "Gemini 2.5 Pro", value: 101 }] },
    },
  });
  let rpcCalls = 0;
  let client = {
    getUserStatus: (...args) => {
      rpcCalls++;
      return rpcImpl(...args);
    },
  };

  let signOutChecks = 0;
  const authService = {
    signOutIfServerHasNoCredential: () => { signOutChecks++; },
  };
  const telemetryCalls = [];
  const settingsMap = new Map([
    ["models", { geminiModelsOnly: false }],
    ["settings", { onTelemetrySettingChanged: v => telemetryCalls.push(v) }],
  ]);

  let effectCleanup = null;
  const React = {
    useContext: () => client,
    useEffect: fn => {
      if (effectCleanup) {
        const prev = effectCleanup;
        effectCleanup = null;
        prev();
      }
      effectCleanup = fn() || null;
    },
  };

  const warnings = [];
  const fakeConsole = { warn: (...args) => warnings.push(args) };
  const e = x => x;
  const Opa = (u, geminiOnly) => u;

  let mount;
  let callDirect5Args;
  if (version === "231") {
    const fn = new Function(
      "Date", "setTimeout", "clearTimeout", "setInterval", "clearInterval",
      "document", "window", "console", "e",
      "B", "Pt", "nk", "mk", "ok", "ik", "fd", "ol", "gj", "Xq", "Wb", "E", "Opa",
      source + "\nreturn { SPb, RPb };"
    );
    const exports = fn(
      fakeDate,
      (cb, ms) => setTimer(cb, ms, false),
      clearTimer,
      (cb, ms) => setTimer(cb, ms, true),
      clearTimer,
      fakeDoc,
      fakeWin,
      fakeConsole,
      e,
      React,
      {},
      () => dispatch,
      () => store,
      sel => sel(state),
      s => s.auth,
      () => ({ core: { authService } }),
      () => null,
      () => settingsMap,
      payload => ({ type: "setUserStatus", payload }),
      schema => schema(),
      { UserStatusSchema: emptyStatus },
      Opa
    );
    mount = () => exports.SPb();
    callDirect5Args = (cancelled = () => false) => exports.RPb(
      client,
      dispatch,
      false,
      settingsMap.get("settings").onTelemetrySettingChanged,
      cancelled
    );
  } else {
    const fn = new Function(
      "Date", "setTimeout", "clearTimeout", "setInterval", "clearInterval",
      "document", "window", "console", "e",
      "A", "kB", "Pl", "Ol", "Ql", "Il", "sd", "fn", "hk", "lu", "$b", "D", "fna",
      source + "\nreturn { bCb, aCb };"
    );
    const exports = fn(
      fakeDate,
      (cb, ms) => setTimer(cb, ms, false),
      clearTimer,
      (cb, ms) => setTimer(cb, ms, true),
      clearTimer,
      fakeDoc,
      fakeWin,
      fakeConsole,
      e,
      React,
      {},
      () => dispatch,
      () => store,
      sel => sel(state),
      s => s.auth,
      () => ({ core: { authService } }),
      () => null,
      () => settingsMap,
      payload => ({ type: "setUserStatus", payload }),
      schema => schema(),
      { UserStatusSchema: emptyStatus },
      Opa
    );
    mount = () => exports.bCb();
    callDirect5Args = (cancelled = () => false) => exports.aCb(
      client,
      dispatch,
      false,
      settingsMap.get("settings").onTelemetrySettingChanged,
      cancelled
    );
  }

  const flushMicrotasks = async (rounds = 8) => {
    for (let i = 0; i < rounds; i++) {
      await Promise.resolve();
    }
  };

  // Mirrors Uq in the bundle: returns undefined when visibleModelConfigs is empty.
  const selectedModel = () => {
    const configs = state.userStatus?.cascadeModelConfigData?.clientModelConfigs ?? [];
    if (configs.length === 0) return undefined;
    return configs.find(m => m.value === state.selectedModelValue) ?? configs[0];
  };

  const advance = async ms => {
    const target = now + ms;
    while (true) {
      let next = null;
      for (const t of timers.values()) {
        if (t.due <= target && (!next || t.due < next.due || (t.due === next.due && t.id < next.id))) {
          next = t;
        }
      }
      if (!next) break;
      now = next.due;
      if (next.repeat) {
        next.due = now + next.delay;
      } else {
        timers.delete(next.id);
      }
      next.fn();
      await flushMicrotasks();
    }
    now = target;
    await flushMicrotasks();
  };

  return {
    mount,
    callDirect5Args,
    unmount: () => {
      if (effectCleanup) {
        const c = effectCleanup;
        effectCleanup = null;
        c();
      }
    },
    flushMicrotasks,
    advance,
    selectedModel,
    getState: () => state,
    setAuthState: s => { state.auth = { state: s }; },
    setRpc: fn => { rpcImpl = fn; },
    setClient: c => { client = c; },
    getRpcCalls: () => rpcCalls,
    getSignOutChecks: () => signOutChecks,
    getTimerDelays: () => [...timers.values()].filter(t => !t.repeat).map(t => t.delay),
    getTimerCount: () => timers.size,
    getListenerCount: () => {
      let n = 0;
      for (const s of docListeners.values()) n += s.size;
      for (const s of winListeners.values()) n += s.size;
      return n;
    },
    setVisibility: async v => {
      visibilityState = v;
      for (const fn of [...(docListeners.get("visibilitychange") || [])]) fn({ type: "visibilitychange" });
      await flushMicrotasks();
    },
    fireWindow: async (type, ev = { type }) => {
      for (const fn of [...(winListeners.get(type) || [])]) fn(ev);
      await flushMicrotasks();
    },
  };
}

(async () => {
  // 1. Upstream 2.31 reproduces the bug: a transient 1006 close at the 10-minute mark wipes models and selection.
  {
    const env = createEnv(upstream231Src, "231");
    env.mount();
    await env.flushMicrotasks();
    assert.strictEqual(env.selectedModel()?.label, "Gemini 2.5 Pro");

    env.setRpc(async () => { throw new Error("WebSocket connection closed unexpectedly (close code 1006)"); });
    await env.advance(600_000);
    assert.strictEqual(env.getState().userStatus.email, "");
    assert.strictEqual(env.selectedModel(), undefined, "upstream wipes selected model on transient error");

    // Even if the socket recovers 500ms later, upstream stays broken for another 10 minutes.
    env.setRpc(async () => ({
      userStatus: {
        email: "dev@example.com",
        cascadeModelConfigData: { clientModelConfigs: [{ label: "Gemini 2.5 Pro", value: 101 }] },
      },
    }));
    await env.advance(30_000);
    assert.strictEqual(env.selectedModel(), undefined);
    env.unmount();
  }

  // 2. Patched 2.31 keeps loaded models and selection across the transient failure, runs the credential check,
  //    and refreshes 1s later via backoff.
  {
    const env = createEnv(patched231Src, "231");
    env.mount();
    await env.flushMicrotasks();
    assert.strictEqual(env.getRpcCalls(), 1);
    assert.strictEqual(env.selectedModel()?.label, "Gemini 2.5 Pro");

    let failOnce = true;
    env.setRpc(async () => {
      if (failOnce) {
        failOnce = false;
        throw new Error("WebSocket connection closed unexpectedly (close code 1006)");
      }
      return {
        userStatus: {
          email: "dev@example.com",
          cascadeModelConfigData: {
            clientModelConfigs: [
              { label: "Gemini 2.5 Pro", value: 101, quota: "fresh" },
              { label: "Gemini 2.5 Flash", value: 102 },
            ],
          },
        },
      };
    });

    await env.advance(600_000);
    assert.strictEqual(env.getRpcCalls(), 2);
    assert.strictEqual(env.getState().userStatus.email, "dev@example.com", "email preserved during error");
    assert.strictEqual(env.selectedModel()?.label, "Gemini 2.5 Pro", "selection never dropped");
    assert.strictEqual(env.getSignOutChecks(), 1, "signOutIfServerHasNoCredential still invoked");
    assert.deepStrictEqual(env.getTimerDelays(), [1000]);

    await env.advance(1000);
    assert.strictEqual(env.getRpcCalls(), 3);
    assert.strictEqual(env.selectedModel()?.quota, "fresh", "backoff retry updated models");
    assert.deepStrictEqual(env.getTimerDelays(), []);
    env.unmount();
  }

  // 3. Patched 2.29/2.30 (5-argument helper without credential callback) preserves status and retries cleanly.
  {
    const env = createEnv(patched229Src, "229");
    env.mount();
    await env.flushMicrotasks();
    assert.strictEqual(env.selectedModel()?.label, "Gemini 2.5 Pro");

    let failed = false;
    env.setRpc(async () => {
      if (!failed) {
        failed = true;
        throw new Error("socket closed");
      }
      return {
        userStatus: {
          email: "dev@example.com",
          cascadeModelConfigData: { clientModelConfigs: [{ label: "Gemini 2.5 Pro", value: 101, v: 2 }] },
        },
      };
    });

    await env.advance(600_000);
    assert.strictEqual(env.selectedModel()?.label, "Gemini 2.5 Pro");
    await env.advance(1000);
    assert.strictEqual(env.selectedModel()?.v, 2);
    env.unmount();
  }

  // 4. Cold-start failures back off 1s, 2s, 4s, 8s, 16s, 32s, 60s, 60s, then recover and reset backoff.
  {
    const env = createEnv(patched231Src, "231");
    env.setRpc(async () => { throw new Error("dial failed"); });
    env.mount();
    await env.flushMicrotasks();
    assert.strictEqual(env.getRpcCalls(), 1);

    const expectedDelays = [1000, 2000, 4000, 8000, 16000, 32000, 60000, 60000];
    for (let i = 0; i < expectedDelays.length; i++) {
      assert.deepStrictEqual(env.getTimerDelays(), [expectedDelays[i]], "step " + i);
      if (i === expectedDelays.length - 1) {
        env.setRpc(async () => ({
          userStatus: {
            email: "dev@example.com",
            cascadeModelConfigData: { clientModelConfigs: [{ label: "Gemini 2.5 Pro", value: 101 }] },
          },
        }));
      }
      await env.advance(expectedDelays[i]);
    }
    assert.strictEqual(env.selectedModel()?.label, "Gemini 2.5 Pro");
    assert.deepStrictEqual(env.getTimerDelays(), []);

    // Next transient error at the 10-minute interval mark (600_000 - 183_000 = 417_000ms away)
    // starts back at 1000ms, not 60000ms.
    env.setRpc(async () => { throw new Error("transient"); });
    await env.advance(600_000 - expectedDelays.reduce((a, b) => a + b, 0));
    assert.deepStrictEqual(env.getTimerDelays(), [1000]);
    env.unmount();
  }

  // 5. Hidden tabs defer background polling and retry Loops, then refresh once on wake;
  //    short tab switches (<60s) and initial non-persisted pageshow do not spam RPCs;
  //    online / persisted pageshow wake immediately out of long backoff; in-flight calls dedupe.
  {
    const env = createEnv(patched231Src, "231");
    env.mount();
    await env.flushMicrotasks();
    assert.strictEqual(env.getRpcCalls(), 1);

    // Non-persisted pageshow right after mount does not re-fetch.
    await env.fireWindow("pageshow", { type: "pageshow", persisted: false });
    assert.strictEqual(env.getRpcCalls(), 1);

    // Quick hide/show within 10s does not re-fetch.
    await env.advance(10_000);
    await env.setVisibility("hidden");
    await env.advance(5_000);
    await env.setVisibility("visible");
    assert.strictEqual(env.getRpcCalls(), 1);

    // While hidden for 15 minutes, the 10-minute interval defers instead of hitting a dead background socket.
    await env.setVisibility("hidden");
    await env.advance(900_000);
    assert.strictEqual(env.getRpcCalls(), 1, "no RPC while hidden");

    // Returning to visible refreshes immediately, and simultaneous persisted pageshow dedupes while in-flight.
    let resolveWake;
    env.setRpc(() => new Promise(r => { resolveWake = r; }));
    const visPromise = env.setVisibility("visible");
    await env.fireWindow("pageshow", { type: "pageshow", persisted: true });
    assert.strictEqual(env.getRpcCalls(), 2, "deduped concurrent wake triggers");
    resolveWake({
      userStatus: {
        email: "dev@example.com",
        cascadeModelConfigData: { clientModelConfigs: [{ label: "Gemini 2.5 Pro", value: 101 }] },
      },
    });
    await visPromise;
    await env.flushMicrotasks();
    assert.strictEqual(env.getRpcCalls(), 2);

    // Online event cuts short a long backoff immediately (915_000ms elapsed so next 600_000ms tick is in 285_000ms).
    env.setRpc(async () => { throw new Error("offline"); });
    await env.advance(285_000); // hits 1,200,000ms interval -> fails -> 1s
    await env.advance(1000);    // fails -> 2s
    await env.advance(2000);    // fails -> 4s
    await env.advance(4000);    // fails -> 8s
    assert.deepStrictEqual(env.getTimerDelays(), [8000]);
    env.setRpc(async () => ({
      userStatus: {
        email: "dev@example.com",
        cascadeModelConfigData: { clientModelConfigs: [{ label: "Gemini 2.5 Pro", value: 101, online: true }] },
      },
    }));
    await env.fireWindow("online");
    assert.strictEqual(env.selectedModel()?.online, true);
    assert.deepStrictEqual(env.getTimerDelays(), []);
    env.unmount();
  }

  // 6. Explicit signedOut state, missing getUserStatus method, and unmount cleanup with an in-flight call.
  {
    const env = createEnv(patched231Src, "231");
    env.mount();
    await env.flushMicrotasks();
    assert.strictEqual(env.getState().userStatus.email, "dev@example.com");

    // Explicit signedOut still clears userStatus immediately.
    env.setAuthState("signedOut");
    env.mount();
    await env.flushMicrotasks();
    assert.strictEqual(env.getState().userStatus.email, "");
    assert.strictEqual(env.selectedModel(), undefined);

    // Client without getUserStatus clears once and never schedules retries.
    env.setAuthState("signedIn");
    env.setClient({});
    env.mount();
    await env.flushMicrotasks();
    assert.deepStrictEqual(env.getTimerDelays(), []);

    // Unmounting while a request is in flight removes all timers/listeners and ignores late resolution/rejection.
    let rejectLate;
    let lateCalls = 0;
    env.setClient({
      getUserStatus: () => {
        lateCalls++;
        return new Promise((_, rej) => { rejectLate = rej; });
      },
    });
    env.mount();
    await env.flushMicrotasks();
    assert.strictEqual(lateCalls, 1);
    const checksBeforeUnmount = env.getSignOutChecks();
    env.unmount();
    assert.strictEqual(env.getTimerCount(), 0, "all timers cleared on unmount");
    assert.strictEqual(env.getListenerCount(), 0, "all listeners removed on unmount");

    rejectLate(new Error("late failure after unmount"));
    await env.flushMicrotasks();
    assert.strictEqual(env.getTimerCount(), 0, "no retry scheduled after unmount");
    assert.strictEqual(env.getSignOutChecks(), checksBeforeUnmount, "no credential check after unmount");
    await env.setVisibility("visible");
    await env.fireWindow("online");
    assert.strictEqual(lateCalls, 1, "no calls after unmount");
  }

  // 7. Direct 5-argument RPb callsite (refreshAuthStatus omits the 6th credential callback) keeps status on error
  //    without throwing on undefined g?.(), returns false on catch, and updates normally on success.
  {
    const env = createEnv(patched231Src, "231");
    env.mount();
    await env.flushMicrotasks();
    assert.strictEqual(env.selectedModel()?.label, "Gemini 2.5 Pro");

    env.setRpc(async () => { throw new Error("refreshAuthStatus transient error"); });
    const errRet = await env.callDirect5Args();
    assert.strictEqual(errRet, false);
    assert.strictEqual(env.getState().userStatus.email, "dev@example.com");
    assert.strictEqual(env.selectedModel()?.label, "Gemini 2.5 Pro");
    assert.strictEqual(env.getSignOutChecks(), 0, "omitted 6th callback must not invoke signOut check");

    env.setRpc(async () => ({
      userStatus: {
        email: "dev@example.com",
        cascadeModelConfigData: { clientModelConfigs: [{ label: "Gemini 2.5 Pro", value: 101, direct: true }] },
      },
    }));
    const okRet = await env.callDirect5Args();
    assert.notStrictEqual(okRet, false);
    assert.strictEqual(env.selectedModel()?.direct, true);
    env.unmount();
  }

  // 8. In-flight request across a hidden transition:
  //    (a) if the tab hides and wakes while an old request is still pending, even a stale success triggers an
  //        immediate follow-up refresh as soon as it settles (and concurrent visibilitychange + pageshow only queue once);
  //    (b) if the in-flight request finishes while hidden (updating _agyLastOk), waking <60s later still refreshes.
  {
    const env = createEnv(patched231Src, "231");
    env.mount();
    await env.flushMicrotasks();
    assert.strictEqual(env.getRpcCalls(), 1);

    let resolveInFlight;
    env.setRpc(() => new Promise(r => { resolveInFlight = r; }));
    await env.advance(600_000);
    assert.strictEqual(env.getRpcCalls(), 2);

    await env.setVisibility("hidden");
    await env.advance(20_000);
    await env.setVisibility("visible");
    await env.fireWindow("pageshow", { type: "pageshow", persisted: true });
    assert.strictEqual(env.getRpcCalls(), 2, "still waiting on in-flight request");

    env.setRpc(async () => ({
      userStatus: {
        email: "dev@example.com",
        cascadeModelConfigData: { clientModelConfigs: [{ label: "Gemini 2.5 Pro", value: 101, v: "fresh-after-wake" }] },
      },
    }));
    resolveInFlight({
      userStatus: {
        email: "dev@example.com",
        cascadeModelConfigData: { clientModelConfigs: [{ label: "Gemini 2.5 Pro", value: 101, v: "stale-pre-freeze" }] },
      },
    });
    await env.flushMicrotasks();
    assert.strictEqual(env.getRpcCalls(), 3, "queued follow-up fired immediately on settlement");
    assert.strictEqual(env.selectedModel()?.v, "fresh-after-wake");
    assert.deepStrictEqual(env.getTimerDelays(), []);
    env.unmount();
  }
  {
    const env = createEnv(patched231Src, "231");
    env.mount();
    await env.flushMicrotasks();

    let resolveBg;
    env.setRpc(() => new Promise(r => { resolveBg = r; }));
    await env.advance(600_000);
    assert.strictEqual(env.getRpcCalls(), 2);

    await env.setVisibility("hidden");
    resolveBg({
      userStatus: {
        email: "dev@example.com",
        cascadeModelConfigData: { clientModelConfigs: [{ label: "Gemini 2.5 Pro", value: 101, v: "bg-settled" }] },
      },
    });
    await env.flushMicrotasks();

    await env.advance(5_000);
    env.setRpc(async () => ({
      userStatus: {
        email: "dev@example.com",
        cascadeModelConfigData: { clientModelConfigs: [{ label: "Gemini 2.5 Pro", value: 101, v: "refreshed-on-show" }] },
      },
    }));
    await env.setVisibility("visible");
    assert.strictEqual(env.getRpcCalls(), 3, "refreshed immediately despite <60s elapsed since background settlement");
    assert.strictEqual(env.selectedModel()?.v, "refreshed-on-show");
    env.unmount();
  }

  // 9. Online event arriving while an offline request is still in flight triggers an immediate retry when that
  //    request rejects, without waiting for the 1s backoff timer.
  {
    const env = createEnv(patched231Src, "231");
    env.mount();
    await env.flushMicrotasks();
    assert.strictEqual(env.getRpcCalls(), 1);

    let rejectOffline;
    env.setRpc(() => new Promise((_, rej) => { rejectOffline = rej; }));
    await env.advance(600_000);
    assert.strictEqual(env.getRpcCalls(), 2);

    env.setRpc(async () => ({
      userStatus: {
        email: "dev@example.com",
        cascadeModelConfigData: { clientModelConfigs: [{ label: "Gemini 2.5 Pro", value: 101, v: "online-immediate" }] },
      },
    }));
    await env.fireWindow("online");
    assert.strictEqual(env.getRpcCalls(), 2, "waits for in-flight offline attempt to settle");

    rejectOffline(new Error("offline socket closed"));
    await env.flushMicrotasks();
    assert.strictEqual(env.getRpcCalls(), 3, "retried at 0ms as soon as offline attempt rejected");
    assert.strictEqual(env.selectedModel()?.v, "online-immediate");
    assert.deepStrictEqual(env.getTimerDelays(), []);
    env.unmount();
  }
})().catch(err => {
  console.error(err);
  process.exit(1);
});
`
	if err := os.WriteFile(harnessFile, []byte(harness), 0600); err != nil {
		t.Fatal(err)
	}

	if out, err := exec.Command(node, harnessFile, upFile, p231File, p229File).CombinedOutput(); err != nil {
		t.Fatalf("runtime harness failed: %v\n%s", err, out)
	}
}
