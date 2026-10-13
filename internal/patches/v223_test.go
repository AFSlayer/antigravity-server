package patches

import (
	"strings"
	"testing"
)

// applyOnly runs the whole patch set over body and fails when the named patch
// is reported missing, so a bundle change that moves one anchor is caught by
// name instead of by a vague output mismatch.
func applyOnly(t *testing.T, id string, body string, opts Options) string {
	t.Helper()
	out, report := Apply(MainJS, []byte(body), opts)
	for _, r := range report.Missing() {
		if r.ID == id {
			t.Fatalf("%s did not apply to:\n%s", id, body)
		}
	}
	return string(out)
}

// 2.23.0 added a search argument to the onboarding redirect.
func TestSkipOnboardingAcceptsSearchArgument(t *testing.T) {
	body := `c.hasOnboardingScreens&&e!==2&&aQ({to:"/onboarding",search:wR,replace:!0,throw:!0})}}))(J5,"manager-landing-lay`
	got := applyOnly(t, "skip-onboarding", body, Options{})
	if strings.Contains(got, `to:"/onboarding"`) {
		t.Errorf("the onboarding redirect survived:\n%s", got)
	}
}

// 2.23.0 prefixes the hidden state with can-hover:, which the patch has to
// carry through unchanged.
func TestConversationRowActionsKeepsCanHoverPrefix(t *testing.T) {
	const tail = `can-hover:opacity-0 group-hover:opacity-100 group-focus-visible:opacity-100 group-has-[:focus-visible]:opacity-100 has-[:focus-visible]:opacity-100`
	body := `className:Xi("absolute top-0 bottom-0 -right-1 pl-6 flex items-center justify-end gap-0.5 z-10",x?"hidden":Wa?"opacity-100":"` + tail + `")`
	got := applyOnly(t, "mobile-conversation-row-actions", body, Options{MobileUX: true})
	if !strings.Contains(got, `Boolean(window.innerWidth<=768`) || !strings.Contains(got, `?"opacity-100":"`+tail+`")`) {
		t.Errorf("row actions output is wrong:\n%s", got)
	}
}

// 2.23.0 renamed the result variable and put a line break after the call.
func TestTitlebarDeleteModalAllowsLineBreakAndNewVariable(t *testing.T) {
	body := "J=f&&!!a;b=SDb({cascadeId:a,paneId:b,includeRemoveFromSplit:!1});\nreturn H.length>0||b.length>0||G.length>0||g.length>0||e?A.createElement(A.Fragment,null,A.createElement(NL,null)):null"
	got := applyOnly(t, "mobile-titlebar-delete-modal", body, Options{MobileUX: true})
	if got == body {
		t.Errorf("the titlebar patch left the body untouched")
	}
	if !strings.Contains(got, `cascadeId:a,paneId:b,includeRemoveFromSplit:!1});`) {
		t.Errorf("the captured call was lost:\n%s", got)
	}
}

// 2.23.0 moved the accept list into a shared constant.
func TestFileUploadAcceptAllHandlesConstantReference(t *testing.T) {
	body := `A.createElement(n5a,{ref:wa,onChange:za,accept:PYb,multiple:!0}),A.createElement`
	got := applyOnly(t, "file-upload-accept-all", body, Options{})
	if strings.Contains(got, `accept:PYb`) {
		t.Errorf("the constant reference survived:\n%s", got)
	}
	if !strings.Contains(got, `multiple:!0`) {
		t.Errorf("multiple:!0 was lost:\n%s", got)
	}
}

// 2.23.0 breaks the line after the comma in the 1MB size error.
func TestFileUploadStreamingFallbackAllowsLineBreak(t *testing.T) {
	body := `else{var u=vNa(m.name,m.type);if(u)if(m.size>1048576)e?.("Text file size exceeds 1MB limit"),g("validation_check_failed",` + "\n" + `Error("Text file size exceeds 1MB limit"));else try{a([q(await n(SN(m,u)))])}catch(x){q(),g("val`
	got := applyOnly(t, "file-upload-large-file-streaming-fallback", body, Options{})
	if strings.Contains(got, `Text file size exceeds 1MB limit`) {
		t.Errorf("the size error survived:\n%s", got)
	}
}

// The transport selector returns a boolean up to 2.22 and a mode string from
// 2.23. The patch has to pick the replacement that fits the shape it found.
func TestWebsocketTransportDefaultBothShapes(t *testing.T) {
	old := `function wrc(){var a=new URLSearchParams(window.location.search),b=a.get("useWebSocket");return b!==null?b==="true":a.get("wsTransport")==="2"}`
	cur := `function wrc(){var a=new URLSearchParams(window.location.search),b=a.get("useWebSocket");return b!==null?b==="true"?"explicit":"off":a.get("wsTransport")==="3"?"hub":"off"}` + "\nvar xrc=class{}"

	got := applyOnly(t, "websocket-transport-default", old, Options{})
	if want := `return b!==null?b==="true":a.get("wsTransport")!=="1"}`; !strings.Contains(got, want) {
		t.Errorf("2.22 shape: want %q in\n%s", want, got)
	}

	got = applyOnly(t, "websocket-transport-default", cur, Options{})
	want := `return b!==null?b==="true"?"explicit":"off":a.get("wsTransport")==="1"?"off":"explicit"}`
	if !strings.Contains(got, want) {
		t.Errorf("2.23 shape: want %q in\n%s", want, got)
	}
	if !strings.Contains(got, "\nvar xrc=class{}") {
		t.Errorf("the code after the function was lost:\n%s", got)
	}
}
