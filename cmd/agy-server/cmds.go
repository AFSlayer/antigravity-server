package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/AFSlayer/antigravity-server/internal/auth"
	"github.com/AFSlayer/antigravity-server/internal/config"
)

func configCommand(args []string) error {
	cfg, err := loadConfig(args, modeServe)
	if err != nil {
		return err
	}
	if err := cfg.EnsureDir(); err != nil {
		return err
	}
	if err := cfg.Save(); err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr)
	step("Saved %s", cfg.Path("config.json"))

	data, err := os.ReadFile(cfg.Path("config.json"))
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr)
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		fmt.Fprintln(os.Stderr, "  "+dim(line))
	}
	fmt.Fprintln(os.Stderr)
	return nil
}

func passwd(args []string) error {
	fromStdin := false
	var positional []string
	afterDoubleDash := false
	for _, a := range args {
		if !afterDoubleDash {
			switch a {
			case "--":
				afterDoubleDash = true
				continue
			case "--stdin":
				fromStdin = true
				continue
			case "--help", "-h":
				fmt.Fprint(os.Stderr, usage)
				return nil
			}
		}
		positional = append(positional, a)
	}
	if fromStdin && len(positional) > 0 {
		return fmt.Errorf("cannot combine --stdin with a positional password argument")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := cfg.EnsureDir(); err != nil {
		return err
	}

	password := ""
	switch {
	case fromStdin:
		raw, err := io.ReadAll(io.LimitReader(os.Stdin, 1024))
		if err != nil {
			return err
		}
		password = strings.TrimRight(string(raw), "\r\n")
	case len(positional) > 0:
		password = positional[0]
	default:
		password, err = promptPassword()
		if err != nil {
			return err
		}
	}

	generated := password == ""
	if generated {
		password = auth.GeneratePassword()
	}
	if len(password) < auth.MinPasswordLen {
		return errWithHints(
			fmt.Sprintf("password must be at least %d characters", auth.MinPasswordLen),
			fmt.Sprintf("Use at least %d characters.", auth.MinPasswordLen),
		)
	}

	credsPath := cfg.Path("credentials.json")
	creds, err := auth.LoadCredentials(credsPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		creds, _, err = auth.LoadOrCreateCredentials(credsPath, password)
		if err != nil {
			return errWithHints(err.Error(), fmt.Sprintf("Use at least %d characters.", auth.MinPasswordLen))
		}
	} else if err := creds.Set(password); err != nil {
		return errWithHints(err.Error(), fmt.Sprintf("Use at least %d characters.", auth.MinPasswordLen))
	}

	fmt.Fprintln(os.Stderr)
	if generated {
		fmt.Fprintf(os.Stderr, "  %s Generated a new password:\n", green("✓"))
		fmt.Println(password)
	} else {
		fmt.Fprintf(os.Stderr, "  %s Password updated\n", green("✓"))
	}
	fmt.Fprintf(os.Stderr, "  %s\n", dim("Signed-in devices stay signed in. Run 'agy-server sessions revoke' to sign them out."))
	return nil
}

func promptPassword() (string, error) {
	fmt.Fprint(os.Stderr, "  New password (blank to generate one): ")

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func sessionsCommand(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	store, err := auth.NewSessionStore(cfg.Path("sessions.json"), cfg.SessionTTL())
	if err != nil {
		return err
	}

	if len(args) > 0 && (args[0] == "revoke" || args[0] == "revoke-all") {
		n, err := store.RevokeAll()
		if err != nil {
			return err
		}
		fmt.Println()
		step("Signed out %d device(s)", n)
		return nil
	}

	list := store.List()
	fmt.Println()

	if len(list) == 0 {
		info("%s", dim("No devices are signed in."))
		fmt.Println()
		return nil
	}

	info("%-20s %-16s %s", bold("SIGNED IN"), bold("LAST SEEN"), bold("DEVICE"))
	for _, s := range list {
		info("%-20s %-16s %s",
			s.CreatedAt.Local().Format("2006-01-02 15:04"),
			humanSince(s.LastSeen),
			shortAgent(s.UserAgent))
	}

	fmt.Println()
	info("%s", dim("Sign them all out with: agy-server sessions revoke"))
	fmt.Println()
	return nil
}

func humanSince(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func shortAgent(ua string) string {
	switch {
	case ua == "":
		return "unknown"
	case strings.Contains(ua, "iPhone"):
		return "iPhone"
	case strings.Contains(ua, "iPad"):
		return "iPad"
	case strings.Contains(ua, "Android"):
		return "Android"
	case strings.Contains(ua, "Macintosh"):
		return "Mac"
	case strings.Contains(ua, "Windows"):
		return "Windows"
	case strings.Contains(ua, "Linux"):
		return "Linux"
	default:
		return "browser"
	}
}
