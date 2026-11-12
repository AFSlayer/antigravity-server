// Package config resolves runtime options from defaults, config.json, the
// environment and command-line flags, in that order of increasing precedence.
package config

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
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
	// on the mobile geometry tracer, which is meant for one session at a time, and
	// a log line for every closed RPC socket.
	Debug bool `json:"-"`

	// DisableWSRelay comes from AGY_WS_RELAY=off only. It returns the RPC
	// WebSocket to a plain pass-through, as an escape hatch for a network in front
	// of the proxy that mishandles compressed frames.
	DisableWSRelay bool `json:"-"`

	// IDEVersionFromEnv records whether IDEVersion came from AGY_IDE_VERSION so
	// automatic binary detection does not clobber an explicit environment override.
	IDEVersionFromEnv bool `json:"-"`

	runtimeMu sync.RWMutex
	dir       string
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
		if up := os.Getenv("USERPROFILE"); up != "" {
			return up
		}
	} else if home := os.Getenv("HOME"); home != "" {
		return home
	}
	if u, err := user.Current(); err == nil && u.HomeDir != "" {
		return u.HomeDir
	}
	return ""
}

// GeminiDir is where Antigravity keeps its settings and OAuth token.
func GeminiDir() string {
	return filepath.Join(HomeDir(), ".gemini")
}

// EffectiveWorkspaceRoot returns WorkspaceRoot when explicitly configured, or
// falls back to ~/antigravity/workspace while preserving legacy ~/workspace
// setups when ~/workspace already exists and ~/antigravity/workspace does not.
func (c *Config) EffectiveWorkspaceRoot() string {
	if c != nil && c.WorkspaceRoot != "" {
		return c.WorkspaceRoot
	}
	home := HomeDir()
	if home == "" {
		return ""
	}
	preferred := filepath.Join(home, "antigravity", "workspace")
	if info, err := os.Stat(preferred); err == nil && info.IsDir() {
		return preferred
	}
	legacy := filepath.Join(home, "workspace")
	if info, err := os.Stat(legacy); err == nil && info.IsDir() {
		return legacy
	}
	return preferred
}

// PatchWorkspaceRoot returns the workspace directory injected into the web
// folder picker patch: an explicit WorkspaceRoot always wins, otherwise the
// default EffectiveWorkspaceRoot is used when that directory exists on disk.
func (c *Config) PatchWorkspaceRoot() string {
	if c == nil {
		return ""
	}
	if c.WorkspaceRoot != "" {
		return c.WorkspaceRoot
	}
	effective := c.EffectiveWorkspaceRoot()
	if effective != "" {
		if info, err := os.Stat(effective); err == nil && info.IsDir() {
			return effective
		}
	}
	return ""
}

// RuntimeIDEVersion reads IDEVersion safely across background updater goroutines.
func (c *Config) RuntimeIDEVersion() string {
	if c == nil {
		return ""
	}
	c.runtimeMu.RLock()
	defer c.runtimeMu.RUnlock()
	return c.IDEVersion
}

// SetRuntimeIDEVersion updates IDEVersion safely across background updater goroutines.
func (c *Config) SetRuntimeIDEVersion(v string) {
	if c == nil {
		return
	}
	c.runtimeMu.Lock()
	c.IDEVersion = v
	c.runtimeMu.Unlock()
}

// RuntimeLanguageServer reads LanguageServer safely across background updater goroutines.
func (c *Config) RuntimeLanguageServer() string {
	if c == nil {
		return ""
	}
	c.runtimeMu.RLock()
	defer c.runtimeMu.RUnlock()
	return c.LanguageServer
}

// SetRuntimeLanguageServer updates LanguageServer safely across background updater goroutines.
func (c *Config) SetRuntimeLanguageServer(p string) {
	if c == nil {
		return
	}
	c.runtimeMu.Lock()
	c.LanguageServer = p
	c.runtimeMu.Unlock()
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
		c.IDEVersionFromEnv = true
	}
	if v := os.Getenv("AGY_DISABLE_PATCHES"); v != "" {
		c.DisabledPatches = splitList(v)
	}
	if v := os.Getenv("AGY_DEBUG"); v != "" && v != "0" {
		c.Debug = true
	}
	switch strings.ToLower(os.Getenv("AGY_WS_RELAY")) {
	case "off", "0", "false", "no":
		c.DisableWSRelay = true
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

// Save writes config.json atomically with owner-only permissions.
func (c *Config) Save() error {
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return err
	}
	c.runtimeMu.RLock()
	data, err := json.MarshalIndent(c, "", "  ")
	c.runtimeMu.RUnlock()
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(c.dir, ".config-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, c.Path("config.json"))
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
