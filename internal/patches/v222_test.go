package patches

import (
	"strings"
	"testing"
)

// The 2.22.0 bundle renamed the focus utilities on the conversation row's action
// strip from focus-within to has-[:focus-visible]. The patch has to follow the
// tail it finds rather than assume one spelling, or the strip stays hidden on
// touch screens.
func TestConversationRowActionsFollowsRenamedFocusClasses(t *testing.T) {
	const tail = `opacity-0 group-hover:opacity-100 group-has-[:focus-visible]:opacity-100 has-[:focus-visible]:opacity-100`
	body := `className:Xi("absolute top-0 bottom-0 -right-1 pl-6 flex items-center justify-end gap-0.5 z-10",x?"hidden":Wa?"opacity-100":"` + tail + `")`

	out, report := Apply(MainJS, []byte(body), Options{MobileUX: true})
	if missing := report.Missing(); len(missing) > 0 {
		for _, r := range missing {
			if r.ID == "mobile-conversation-row-actions" {
				t.Fatal("mobile-conversation-row-actions did not apply to the 2.22.0 spelling")
			}
		}
	}

	got := string(out)
	for _, want := range []string{
		`className:Xi("absolute top-0 bottom-0 -right-1 pl-6 flex items-center justify-end gap-0.5 z-10",(x||Wa||Boolean(window.innerWidth<=768`,
		`?"opacity-100":"` + tail + `")`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("patched output is missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "focus-within") {
		t.Errorf("the old focus class leaked into the 2.22.0 output:\n%s", got)
	}
}

// 2.22.0 added a second function with the same shape as the one that maps text
// extensions, for office documents. The patch must pick the text one by what
// follows it, and leave the other alone.
func TestFileUploadTextTypesIgnoresOfficeTypeLookalike(t *testing.T) {
	text := `function MLa(a,b){b=b.split(";")[0].trim().toLowerCase();if(ILa.includes(b))return b;a=a.slice(a.lastIndexOf(".")+1).toLowerCase();return LLa[a]}function uA(a){return MLa("",a)!==void 0}`
	office := `function PLa(a,b){b=b.split(";")[0].trim().toLowerCase();if(vA.includes(b))return b;a=a.slice(a.lastIndexOf(".")+1).toLowerCase();return wA[a]}function xA(a,b=""){return PLa(b,a)!==void 0}`

	if n := len(fileUploadCustomTextTypesRe.FindAllString(text+`;var q=1;`+office, -1)); n != 1 {
		t.Fatalf("the pattern matched %d functions, want exactly the text one", n)
	}

	for name, body := range map[string]string{
		"text first":   text + `;var q=1;` + office,
		"office first": office + `;var q=1;` + text,
	} {
		out, _ := Apply(MainJS, []byte(body), Options{})
		got := string(out)
		if !strings.Contains(got, office) {
			t.Errorf("%s: the office type lookup must stay untouched:\n%s", name, got)
		}
		if strings.Contains(got, text) || !strings.Contains(got, `function MLa(a,b){`) || !strings.Contains(got, `"text/plain"`) {
			t.Errorf("%s: the text type lookup was not rewritten:\n%s", name, got)
		}
		if !strings.Contains(got, `function uA(a){return MLa("",a)!==void 0}`) {
			t.Errorf("%s: the function after the match was lost:\n%s", name, got)
		}
	}
}
