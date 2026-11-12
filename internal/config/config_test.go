package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveAndLoadTrustedProxiesExplicitRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGY_HOME", dir)

	// 1. Implicit empty TrustedProxies omits the key in config.json.
	implicitCfg := Default()
	implicitCfg.PublicURL = "https://agy.example.com"
	if err := implicitCfg.Save(); err != nil {
		t.Fatalf("Save implicit: %v", err)
	}
	rawImplicit, err := os.ReadFile(implicitCfg.Path("config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rawImplicit), `"trusted_proxies"`) {
		t.Fatalf("implicit empty TrustedProxies should omit trusted_proxies key, got:\n%s", string(rawImplicit))
	}
	loadedImplicit, err := Load()
	if err != nil {
		t.Fatalf("Load implicit: %v", err)
	}
	if loadedImplicit.TrustedProxiesExplicit || len(loadedImplicit.TrustedProxies) != 0 {
		t.Fatalf("expected implicit empty on Load, got explicit=%v proxies=%v", loadedImplicit.TrustedProxiesExplicit, loadedImplicit.TrustedProxies)
	}

	// 2. Explicit empty TrustedProxies writes "trusted_proxies": [] and reloads as TrustedProxiesExplicit=true.
	explicitEmpty := Default()
	explicitEmpty.PublicURL = "https://agy.example.com"
	explicitEmpty.TrustedProxiesExplicit = true
	if err := explicitEmpty.Save(); err != nil {
		t.Fatalf("Save explicit empty: %v", err)
	}
	rawEmpty, err := os.ReadFile(explicitEmpty.Path("config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rawEmpty), `"trusted_proxies": []`) {
		t.Fatalf("explicit empty TrustedProxies must persist \"trusted_proxies\": [], got:\n%s", string(rawEmpty))
	}
	loadedEmpty, err := Load()
	if err != nil {
		t.Fatalf("Load explicit empty: %v", err)
	}
	if !loadedEmpty.TrustedProxiesExplicit || len(loadedEmpty.TrustedProxies) != 0 {
		t.Fatalf("expected TrustedProxiesExplicit=true and empty slice after Load, got explicit=%v proxies=%v", loadedEmpty.TrustedProxiesExplicit, loadedEmpty.TrustedProxies)
	}

	// 3. Explicit CIDR list persists and can be cleared by AGY_TRUSTED_PROXIES="".
	customCfg := Default()
	customCfg.TrustedProxies = []string{"10.42.0.0/16"}
	if err := customCfg.Save(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGY_TRUSTED_PROXIES", "")
	envCleared, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !envCleared.TrustedProxiesExplicit || len(envCleared.TrustedProxies) != 0 {
		t.Fatalf("expected AGY_TRUSTED_PROXIES='' to clear disk proxies with explicit=true, got explicit=%v proxies=%v", envCleared.TrustedProxiesExplicit, envCleared.TrustedProxies)
	}
}

func TestNormalizeWorkspacePath(t *testing.T) {
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	t.Setenv("USERPROFILE", fakeHome)

	if got := NormalizeWorkspacePath("   "); got != "" {
		t.Fatalf("NormalizeWorkspacePath blank = %q, want empty", got)
	}
	if got := NormalizeWorkspacePath("~"); got != filepath.Clean(fakeHome) {
		t.Fatalf("NormalizeWorkspacePath(~) = %q, want %q", got, filepath.Clean(fakeHome))
	}
	wantSub := filepath.Join(fakeHome, "antigravity", "workspace")
	if got := NormalizeWorkspacePath("~/antigravity/workspace"); got != wantSub {
		t.Fatalf("NormalizeWorkspacePath(~/...) = %q, want %q", got, wantSub)
	}
	rel := NormalizeWorkspacePath("./rel-dir")
	if !filepath.IsAbs(rel) || filepath.Base(rel) != "rel-dir" {
		t.Fatalf("NormalizeWorkspacePath(./rel-dir) = %q, want absolute path ending in rel-dir", rel)
	}
}
