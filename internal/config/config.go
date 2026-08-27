// Package config resolves runtime options from defaults, config.json, the
// environment and command-line flags, in that order of increasing precedence.
package config

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultPort       = 8765
	DefaultBindAddr   = "0.0.0.0"
	DefaultSessionTTL = 30 * 24 * time.Hour
)

// Config is the resolved configuration. It is persisted to config.json by the
// config command so a service can start with no arguments.
type Config struct {
	Port            int      `json:"port"`
	BindAddr        string   `json:"bind_addr"`
	PublicURL       string   `json:"public_url,omitempty"`
	WorkspaceRoot   string   `json:"workspace_root,omitempty"`
	SessionDays     int      `json:"session_days"`
	TrustedProxies  []string `json:"trusted_proxies,omitempty"`
	MobileUX        bool     `json:"mobile_ux"`
	DisabledPatches []string `json:"disabled_patches,omitempty"`
	LanguageServer  string   `json:"language_server,omitempty"`
	IDEVersion      string   `json:"ide_version,omitempty"`

	// Debug comes from AGY_DEBUG only, and is deliberately not persisted: it turns
	// on the mobile geometry tracer, which is meant for one session at a time.
	Debug bool `json:"-"`

	dir string
}

// Default returns the configuration used when nothing has been set.
func Default() *Config {
	return &Config{
		Port:        DefaultPort,
		BindAddr:    DefaultBindAddr,
		SessionDays: int(DefaultSessionTTL.Hours() / 24),
		MobileUX:    true,
		dir:         DefaultDir(),
	}
}

// DefaultDir is where credentials, sessions and config.json live.
func DefaultDir() string {
	if d := os.Getenv("AGY_HOME"); d != "" {
		return d
	}
	return filepath.Join(HomeDir(), ".agy-remote")
}

// HomeDir returns the user's home directory, tolerating a missing HOME.
func HomeDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home
	}
	if runtime.GOOS == "windows" {
		return os.Getenv("USERPROFILE")
	}
	return os.Getenv("HOME")
}

// GeminiDir is where Antigravity keeps its settings and OAuth token.
func GeminiDir() string {
	return filepath.Join(HomeDir(), ".gemini")
}

// Dir is the data directory holding credentials and sessions.
func (c *Config) Dir() string { return c.dir }

// SetDir overrides the data directory path (useful for testing).
func (c *Config) SetDir(dir string) { c.dir = dir }

// Path resolves name inside the data directory.
func (c *Config) Path(name string) string { return filepath.Join(c.dir, name) }

// SessionTTL is how long a signed-in device stays signed in.
func (c *Config) SessionTTL() time.Duration {
	if c.SessionDays <= 0 {
		return DefaultSessionTTL
	}
	return time.Duration(c.SessionDays) * 24 * time.Hour
}

// TrustedProxyNets parses TrustedProxies, accepting bare addresses as well as
// CIDRs. An empty result means no forwarded headers are believed.
func (c *Config) TrustedProxyNets() ([]*net.IPNet, error) {
	var nets []*net.IPNet
	for _, raw := range c.TrustedProxies {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if !strings.Contains(raw, "/") {
			if ip := net.ParseIP(raw); ip != nil {
				bits := 32
				if ip.To4() == nil {
					bits = 128
				}
				raw = fmt.Sprintf("%s/%d", raw, bits)
			}
		}
		_, n, err := net.ParseCIDR(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid trusted proxy %q: %w", raw, err)
		}
		nets = append(nets, n)
	}
	return nets, nil
}

// Load reads config.json if present and applies environment overrides.
func Load() (*Config, error) {
	c := Default()

	data, err := os.ReadFile(c.Path("config.json"))
	if err == nil {
		if err := json.Unmarshal(data, c); err != nil {
			return nil, fmt.Errorf("parse %s: %w", c.Path("config.json"), err)
		}
		c.dir = DefaultDir()
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	c.applyEnv()

	if c.Port == 0 {
		c.Port = DefaultPort
	}
	if c.BindAddr == "" {
		c.BindAddr = DefaultBindAddr
	}
	return c, nil
}

func (c *Config) applyEnv() {
	if v := os.Getenv("AGY_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Port = n
		}
	}
	if v := os.Getenv("AGY_BIND"); v != "" {
		c.BindAddr = v
	}
	if v := os.Getenv("AGY_WORKSPACE_ROOT"); v != "" {
		c.WorkspaceRoot = v
	}
	if v := os.Getenv("AGY_LANGUAGE_SERVER"); v != "" {
		c.LanguageServer = v
	}
	if v := os.Getenv("AGY_PUBLIC_URL"); v != "" {
		c.PublicURL = v
	}
	if v := os.Getenv("AGY_IDE_VERSION"); v != "" {
		c.IDEVersion = v
	}
	if v := os.Getenv("AGY_DISABLE_PATCHES"); v != "" {
		c.DisabledPatches = splitList(v)
	}
	if v := os.Getenv("AGY_DEBUG"); v != "" && v != "0" {
		c.Debug = true
	}
	if v := os.Getenv("AGY_TRUSTED_PROXIES"); v != "" {
		c.TrustedProxies = splitList(v)
	}
	if v := os.Getenv("AGY_SESSION_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.SessionDays = n
		}
	}
}

func splitList(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Save writes config.json with owner-only permissions.
func (c *Config) Save() error {
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.Path("config.json"), append(data, '\n'), 0o600)
}

// DisabledPatchSet turns DisabledPatches into a lookup for patches.Options.
func (c *Config) DisabledPatchSet() map[string]bool {
	if len(c.DisabledPatches) == 0 {
		return nil
	}
	out := make(map[string]bool, len(c.DisabledPatches))
	for _, id := range c.DisabledPatches {
		out[strings.TrimSpace(id)] = true
	}
	return out
}

// EnsureDir creates the data directory if needed.
func (c *Config) EnsureDir() error {
	return os.MkdirAll(c.dir, 0o700)
}
