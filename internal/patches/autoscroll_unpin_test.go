package patches

import (
	"strings"
	"testing"
)

func applyUnpinPatch(t *testing.T, src string) string {
	t.Helper()
	for _, p := range All() {
		if p.ID == "autoscroll-unpin-needs-input" {
			out := p.FindRe.ReplaceAllStringFunc(src, func(m string) string {
				groups := p.FindRe.FindSubmatch([]byte(m))
				return p.ReplaceMatch(groups, Options{})
			})
			if out == src {
				t.Fatalf("anchor did not match %q", src)
			}
			return out
		}
	}
	t.Fatal("patch is not registered")
	return ""
}

func TestAutoscrollUnpinNeedsInputShapes(t *testing.T) {
	cases := []struct {
		name, src string
		want      []string
	}{
		{
			name: "2.29 keeps the follow guard and the smooth-scroll cleanup",
			src:  `var J=()=>{var L=n.current;if(L){var M=L.scrollTop,N=F(),P=Math.max(0,L.scrollHeight-L.clientHeight),O=P<v.current&&M>=P-1,Q=r.current;M<t.current&&!O?(r.current=!1,tT.delete(L)):N&&!C.current&&(r.current=!0);`,
			want: []string{
				`M<t.current&&!O?(r.current&&!C.current&&window.__agyIsSpontaneousScroll?.(L)?L.scrollTo({top:L.scrollHeight,behavior:"instant"}):(r.current=!1,tT.delete(L))):N&&!C.current&&(r.current=!0)`,
			},
		},
		{
			name: "a line break can land at any token",
			src:  "var J=()=>{var L=\nn.current;if(L){var M=L.scrollTop,N=F(),O=!1,Q=r.current;M<\nt.current&&!O?(r.current=\n!1,\ntT.delete(L)):\nN&&!C.current&&\n(r.current=!0);",
			want: []string{
				`(r.current&&!C.current&&window.__agyIsSpontaneousScroll?.(L)?L.scrollTo({top:L.scrollHeight,behavior:"instant"}):(r.current=!1,tT.delete(L))):`,
			},
		},
		{
			name: "2.23 has neither the follow guard nor the cleanup",
			src:  `var H=()=>{var G=m.current;if(G){var K=G.scrollTop,J=C(),L=G.scrollHeight<u.current,M=p.current;K<t.current&&!L?p.current=!1:J&&(p.current=!0);`,
			want: []string{
				`K<t.current&&!L?(p.current&&window.__agyIsSpontaneousScroll?.(G)?G.scrollTo({top:G.scrollHeight,behavior:"instant"}):(p.current=!1)):J&&(p.current=!0)`,
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := applyUnpinPatch(t, c.src)
			for _, w := range c.want {
				if !strings.Contains(out, w) {
					t.Errorf("patched source is missing\n%s\ngot\n%s", w, out)
				}
			}
		})
	}
}
