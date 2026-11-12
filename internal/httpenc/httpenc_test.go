package httpenc

import (
	"net/http"
	"testing"
)

func TestAcceptsGzip(t *testing.T) {
	for in, want := range map[string]bool{
		"gzip, deflate, br": true,
		"br, GZIP;q=0.5":    true,
		"gzip;q=0":          false,
		"gzip; q=0.0":       false,
		"br, deflate":       false,
		"":                  false,
		"x-gzip":            true,
	} {
		if got := AcceptsGzip(in); got != want {
			t.Errorf("AcceptsGzip(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestAddVary(t *testing.T) {
	h := http.Header{"Vary": {"Origin"}}
	AddVary(h)
	AddVary(h)
	if got := h.Values("Vary"); len(got) != 2 || got[1] != "Accept-Encoding" {
		t.Fatalf("Vary = %v, want Origin and one Accept-Encoding", got)
	}
}
