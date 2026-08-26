package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/AFSlayer/antigravity-server/internal/config"
)

var version = "dev"

const usage = `Antigravity Server — self-hosted cloud & mobile server for Google Antigravity.

Usage:
  agy-server [flags]            Share the Antigravity desktop app on your network
  agy-server serve [flags]      Run headless on a server (personal cloud Antigravity)
  agy-server doctor             Check the setup and report what is wrong
  agy-server config [flags]     Save options to config.json without starting
  agy-server passwd [password]  Set the access password
  agy-server sessions [revoke]  List or revoke signed-in devices
  agy-server update [flags]     Update Antigravity language_server to the latest official release
  agy-server version            Print the version

Flags:
  --port N                  Port to listen on (default 8765)
  --bind ADDR               Address to bind (default 0.0.0.0)
  --public-url URL          Public URL shown to clients, e.g. https://agy.example.com
  --workspace-root PATH     Where the folder picker starts
  --language-server PATH    Path to the Antigravity language_server binary
  --trusted-proxies CIDRS   Comma-separated proxy CIDRs, e.g. 127.0.0.1/32
  --session-days N          How long a signed-in device stays signed in (default 30)
  --no-mobile-patches       Serve the UI unmodified
  --disable-patch ID        Turn off one patch, repeatable (see agy-server doctor)

Environment:
  AGY_PASSWORD, AGY_PORT, AGY_BIND, AGY_PUBLIC_URL, AGY_WORKSPACE_ROOT,
  AGY_LANGUAGE_SERVER, AGY_TRUSTED_PROXIES, AGY_SESSION_DAYS, AGY_HOME

Docs: https://github.com/AFSlayer/antigravity-server
`

func main() {
	enableVirtualTerminal()

	args := os.Args[1:]
	command := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command, args = args[0], args[1:]
	}

	switch command {
	case "", "local":
		dispatch(modeLocal, args)
	case "serve":
		dispatch(modeServe, args)
	case "doctor":
		mustRun(doctor(args))
	case "config":
		mustRun(configCommand(args))
	case "passwd", "password":
		mustRun(passwd(args))
	case "sessions":
		mustRun(sessionsCommand(args))
	case "update":
		mustRun(updateCommand(args))
	case "version", "--version", "-v":
		fmt.Println(version)
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", command)
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}

func dispatch(mode runMode, args []string) {
	cfg, err := loadConfig(args, mode)
	if err != nil {
		exitWith(err)
	}
	mustRun(run(mode, cfg))
}

func mustRun(err error) {
	if err != nil {
		exitWith(err)
	}
}

func loadConfig(args []string, mode runMode) (*config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	fs := flag.NewFlagSet("agy-server", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }

	port := fs.Int("port", cfg.Port, "")
	bind := fs.String("bind", cfg.BindAddr, "")
	publicURL := fs.String("public-url", cfg.PublicURL, "")
	workspaceRoot := fs.String("workspace-root", cfg.WorkspaceRoot, "")
	languageServer := fs.String("language-server", cfg.LanguageServer, "")
	trustedProxies := fs.String("trusted-proxies", strings.Join(cfg.TrustedProxies, ","), "")
	sessionDays := fs.Int("session-days", cfg.SessionDays, "")
	noMobile := fs.Bool("no-mobile-patches", !cfg.MobileUX, "")
	disabled := &repeatedFlag{values: cfg.DisabledPatches}
	fs.Var(disabled, "disable-patch", "")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	cfg.Port = *port
	cfg.BindAddr = *bind
	cfg.PublicURL = *publicURL
	cfg.WorkspaceRoot = *workspaceRoot
	cfg.LanguageServer = *languageServer
	cfg.SessionDays = *sessionDays
	cfg.MobileUX = !*noMobile
	cfg.TrustedProxies = splitCSV(*trustedProxies)
	cfg.DisabledPatches = disabled.values

	if mode == modeServe && len(cfg.TrustedProxies) == 0 && cfg.PublicURL != "" {
		cfg.TrustedProxies = []string{"127.0.0.1/32", "::1/128"}
	}

	if _, err := cfg.TrustedProxyNets(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// repeatedFlag collects a flag given more than once. Passing it at all replaces
// whatever config.json held, rather than adding to it, so a run is never stuck
// with a value it cannot clear.
type repeatedFlag struct {
	values []string
	given  bool
}

func (f *repeatedFlag) String() string { return strings.Join(f.values, ",") }

func (f *repeatedFlag) Set(v string) error {
	if !f.given {
		f.values = nil
		f.given = true
	}
	f.values = append(f.values, splitCSV(v)...)
	return nil
}

func splitCSV(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
