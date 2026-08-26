package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// isTerminal reports whether stdout is attached to a terminal. It is false when
// the binary was double-clicked from a file manager or launched by a service
// manager, which is when console output would be invisible to the user.
func isTerminal() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

var attached = isTerminal()

var useColor = os.Getenv("NO_COLOR") == "" && attached

func paint(code, s string) string {
	if !useColor {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func dim(s string) string    { return paint("90", s) }
func green(s string) string  { return paint("32", s) }
func yellow(s string) string { return paint("33", s) }
func red(s string) string    { return paint("31", s) }
func bold(s string) string   { return paint("1", s) }
func cyan(s string) string   { return paint("36", s) }

func info(format string, args ...any) { fmt.Printf("  "+format+"\n", args...) }
func step(format string, args ...any) {
	fmt.Printf("  %s "+format+"\n", append([]any{green("✓")}, args...)...)
}
func warn(format string, args ...any) {
	fmt.Printf("  %s "+format+"\n", append([]any{yellow("!")}, args...)...)
}
func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "  %s "+format+"\n", append([]any{red("✕")}, args...)...)
}

type userError struct {
	msg   string
	hints []string
}

func (e *userError) Error() string { return e.msg }

func errWithHints(msg string, hints ...string) error {
	return &userError{msg: msg, hints: hints}
}

func exitWith(err error) {
	fmt.Fprintln(os.Stderr)

	var ue *userError
	if e, ok := err.(*userError); ok {
		ue = e
	} else {
		ue = &userError{msg: err.Error()}
	}

	fail("%s", ue.msg)

	if len(ue.hints) > 0 {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "  "+bold("What to do next"))
		for _, hint := range ue.hints {
			fmt.Fprintln(os.Stderr, "    "+dim("→")+" "+hint)
		}
	}

	fmt.Fprintln(os.Stderr)

	if !attached {
		showDialog(ue)
	}

	waitForEnterOnWindows()
	os.Exit(1)
}

// showDialog surfaces a fatal error through the desktop environment. Without it
// a double-clicked binary would fail with no visible explanation at all.
func showDialog(e *userError) {
	title := "Antigravity Server"
	body := e.msg
	if len(e.hints) > 0 {
		body += "\n\n" + strings.Join(e.hints, "\n\n")
	}

	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf(
			"display alert %s message %s as critical",
			appleScriptString(title), appleScriptString(body))
		_ = exec.Command("osascript", "-e", script).Run()

	case "windows":
		script := fmt.Sprintf(
			"Add-Type -AssemblyName PresentationFramework; [System.Windows.MessageBox]::Show(%s, %s)",
			powerShellString(body), powerShellString(title))
		_ = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).Run()

	default:
		if path, err := exec.LookPath("zenity"); err == nil {
			_ = exec.Command(path, "--error", "--title", title, "--text", body).Run()
		}
	}
}

func appleScriptString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func powerShellString(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `''`) + `'`
}

func waitForEnterOnWindows() {
	if runtime.GOOS != "windows" || !attached {
		return
	}
	fmt.Fprint(os.Stderr, "  Press Enter to close this window…")
	var discard string
	_, _ = fmt.Scanln(&discard)
}

func banner(version string) {
	fmt.Println()
	fmt.Println("  " + bold("Antigravity Server") + " " + dim("v"+version))
}

func rule() {
	fmt.Println("  " + dim(strings.Repeat("─", 52)))
}
