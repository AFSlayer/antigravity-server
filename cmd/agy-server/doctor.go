package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AFSlayer/antigravity-server/internal/auth"
	"github.com/AFSlayer/antigravity-server/internal/config"
	"github.com/AFSlayer/antigravity-server/internal/lsproc"
	"github.com/AFSlayer/antigravity-server/internal/netinfo"
	"github.com/AFSlayer/antigravity-server/internal/patches"
)

type checklist struct {
	problems int
}

func (c *checklist) ok(format string, args ...any) {
	fmt.Printf("  %s %s\n", green("✓"), fmt.Sprintf(format, args...))
}

func (c *checklist) skip(format string, args ...any) {
	fmt.Printf("  %s %s\n", dim("–"), dim(fmt.Sprintf(format, args...)))
}

func (c *checklist) bad(format string, args ...any) {
	c.problems++
	fmt.Printf("  %s %s\n", red("✕"), fmt.Sprintf(format, args...))
}

func (c *checklist) note(format string, args ...any) {
	fmt.Printf("    %s %s\n", dim("→"), dim(fmt.Sprintf(format, args...)))
}

func doctor(args []string) error {
	cfg, err := loadConfig(args, modeLocal)
	if err != nil {
		return err
	}

	banner(version)
	fmt.Println()

	c := &checklist{}

	c.checkDataDir(cfg)
	c.checkGeminiConfig()
	c.checkBinaries(cfg)
	instance := c.checkRunning()
	c.checkPatches(cfg, instance)
	c.checkNetwork(cfg)

	fmt.Println()
	rule()
	if c.problems == 0 {
		step("Everything looks good.")
	} else {
		warn("%d problem(s) found.", c.problems)
	}
	fmt.Println()
	return nil
}

func (c *checklist) checkDataDir(cfg *config.Config) {
	if err := cfg.EnsureDir(); err != nil {
		c.bad("Data directory %s is not writable", cfg.Dir())
		c.note("Set AGY_HOME to a writable location")
		return
	}
	c.ok("Data directory %s", dim(cfg.Dir()))

	credentialsPath := cfg.Path("credentials.json")
	if _, err := os.Stat(credentialsPath); err != nil {
		c.skip("No password set yet (one will be generated on first run)")
	} else {
		c.ok("Access password is set")
	}

	if store, err := auth.NewSessionStore(cfg.Path("sessions.json"), cfg.SessionTTL()); err == nil {
		c.ok("%d device(s) signed in", store.Count())
	}
}

func (c *checklist) checkGeminiConfig() {
	path := filepath.Join(config.GeminiDir(), "config", "config.json")

	data, err := os.ReadFile(path)
	if err != nil {
		c.skip("Antigravity settings not found at %s", path)
		c.note("Start Antigravity once so it can create its settings file")
		return
	}

	var parsed struct {
		UserSettings struct {
			RemoteControlEnabled bool `json:"remoteControlEnabled"`
		} `json:"userSettings"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		c.bad("Could not parse %s", path)
		return
	}

	if parsed.UserSettings.RemoteControlEnabled {
		c.ok("Remote control is enabled in Antigravity settings")
	} else {
		c.skip("Remote control is not enabled yet (agy-server will enable it)")
	}
}

func (c *checklist) checkBinaries(cfg *config.Config) {
	if binary := lsproc.FindLanguageServer(cfg.LanguageServer); binary != "" {
		c.ok("language_server found at %s", dim(binary))
		return
	}
	c.skip("No language_server binary found on disk")
	c.note("Local mode does not need one if the desktop app is installed")
	c.note("Server mode does: run scripts/install.sh or pass --language-server")
}

func (c *checklist) checkRunning() *lsproc.Instance {
	instance, err := lsproc.Find()
	if err != nil {
		c.bad("No standalone Antigravity language server is running")
		c.note("Start the Antigravity desktop app, or run: agy-server serve")
		return nil
	}

	c.ok("Language server running (pid %d, port %d)", instance.PID, instance.Port)
	if instance.CSRFToken == "" {
		c.bad("Language server has no CSRF token, requests will be rejected")
		c.note("Restart it through agy-server so a token is passed")
	} else {
		c.ok("CSRF token is present")
	}
	return instance
}

func (c *checklist) checkPatches(cfg *config.Config, instance *lsproc.Instance) {
	if instance == nil {
		c.skip("Skipped patch check (no running language server)")
		return
	}

	opts := patches.Options{
		MobileUX:      cfg.MobileUX,
		WorkspaceRoot: cfg.WorkspaceRoot,
		Disabled:      cfg.DisabledPatchSet(),
		Debug:         cfg.Debug,
	}
	opts.CacheKey = patches.CacheKey(version, opts)

	for _, target := range []struct {
		path   string
		target patches.Target
	}{
		{"/main.js", patches.MainJS},
		{"/", patches.HTML},
	} {
		body, _, err := instance.Fetch(target.path)
		if err != nil {
			c.bad("Could not fetch %s: %v", target.path, err)
			continue
		}

		_, report := patches.Apply(target.target, body, opts)
		for _, res := range report {
			switch res.Status {
			case patches.StatusApplied:
				c.ok("patch %s", dim(res.ID))
			case patches.StatusNotNeeded:
				c.ok("patch %s %s", dim(res.ID), dim("(cleaned up upstream)"))
			case patches.StatusDisabled:
				c.skip("patch %s (disabled)", res.ID)
			default:
				if res.Required {
					c.bad("patch %s did not match — remote access will not work", res.ID)
				} else {
					c.bad("patch %s did not match — %s", res.ID, res.Desc)
				}
				c.note("Report it: https://github.com/AFSlayer/antigravity-server/issues")
			}
		}
	}
}

func alreadyServing(port int) bool {
	client := &http.Client{Timeout: 2 * time.Second}

	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d%s", port, auth.LoginPath))
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return false
	}
	// The marker keeps its pre-rename spelling on purpose: doctor often probes a
	// port held by an older build, and both sides have to agree on the string.
	return strings.Contains(string(body), `name="agy-remote"`)
}

func (c *checklist) checkNetwork(cfg *config.Config) {
	local := netinfo.Local()

	if local.LAN != "" {
		c.ok("Reachable on your network at %s", cyan(fmt.Sprintf("http://%s:%d", local.LAN, cfg.Port)))
	} else {
		c.bad("No network address found; only this machine can connect")
	}
	if local.Tailscale != "" {
		c.ok("Tailscale address %s", dim(local.Tailscale))
	}

	listener, err := net.Listen("tcp", net.JoinHostPort(cfg.BindAddr, fmt.Sprint(cfg.Port)))
	if err != nil {
		if alreadyServing(cfg.Port) {
			c.ok("Port %d is serving an agy-server instance", cfg.Port)
		} else {
			c.bad("Port %d is already in use by something else", cfg.Port)
			c.note("Pick another port: agy-server --port 8888")
		}
		return
	}
	_ = listener.Close()
	c.ok("Port %d is free", cfg.Port)

	if cfg.PublicURL != "" && len(cfg.TrustedProxies) == 0 {
		c.bad("public-url is set but no trusted proxies are configured")
		c.note("Rate limiting will see your proxy instead of real clients")
		c.note("Fix it with: --trusted-proxies 127.0.0.1/32")
	}
}
