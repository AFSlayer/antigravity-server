// Package httpenc decides response compression for handlers that compress
// what they send to the browser themselves.
package httpenc

import (
	"net/http"
	"strconv"
	"strings"
)

// AcceptsGzip reports whether an Accept-Encoding value allows gzip.
func AcceptsGzip(acceptEncoding string) bool {
	for _, part := range strings.Split(acceptEncoding, ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		name = strings.ToLower(strings.TrimSpace(name))
		if name != "gzip" && name != "x-gzip" {
			continue
		}
		for _, p := range strings.Split(params, ";") {
			k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
			if ok && strings.EqualFold(strings.TrimSpace(k), "q") {
				if q, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && q <= 0 {
					return false
				}
			}
		}
		return true
	}
	return false
}

// AddVary adds Accept-Encoding to the Vary header unless it is there.
func AddVary(h http.Header) {
	for _, v := range h.Values("Vary") {
		for _, tok := range strings.Split(v, ",") {
			tok = strings.TrimSpace(tok)
			if tok == "*" || strings.EqualFold(tok, "Accept-Encoding") {
				return
			}
		}
	}
	h.Add("Vary", "Accept-Encoding")
}
