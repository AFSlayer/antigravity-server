package lsproc

import (
	"context"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestListeningPortsAndProcessTreeListeningEndpoints(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	selfPort := ln.Addr().(*net.TCPAddr).Port

	ownPorts, err := ListeningPorts(os.Getpid())
	if err != nil {
		t.Fatalf("ListeningPorts(self): %v", err)
	}
	foundSelf := false
	for _, p := range ownPorts {
		if p == selfPort {
			foundSelf = true
			break
		}
	}
	if !foundSelf {
		t.Fatalf("ListeningPorts(self) = %v, missing %d", ownPorts, selfPort)
	}

	treeEndpoints, err := ProcessTreeListeningEndpoints(context.Background(), os.Getpid())
	if err != nil {
		t.Fatalf("ProcessTreeListeningEndpoints(self): %v", err)
	}
	if _, ok := treeEndpoints[selfPort]; !ok {
		t.Fatalf("ProcessTreeListeningEndpoints(self) = %v, missing %d", treeEndpoints, selfPort)
	}

	// If python3 or node is available, verify descendant child listener discovery too.
	if node, err := exec.LookPath("node"); err == nil {
		cmd := exec.Command(node, "-e", `
const net = require("net");
const srv = net.createServer();
srv.listen(0, "127.0.0.1", () => {
  process.stdout.write(String(srv.address().port) + "\n");
  setInterval(() => {}, 1000);
});
`)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		})

		buf := make([]byte, 32)
		n, err := stdout.Read(buf)
		if err != nil || n == 0 {
			t.Fatalf("failed reading child port: %v", err)
		}
		var childPort int
		for i := 0; i < n; i++ {
			if buf[i] >= '0' && buf[i] <= '9' {
				childPort = childPort*10 + int(buf[i]-'0')
			}
		}
		if childPort <= 0 {
			t.Fatalf("invalid childPort parsed from %q", string(buf[:n]))
		}

		var sawChild bool
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			endpoints, err := ProcessTreeListeningEndpoints(context.Background(), os.Getpid())
			if _, ok := endpoints[childPort]; err == nil && ok {
				sawChild = true
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if !sawChild {
			t.Fatalf("ProcessTreeListeningEndpoints(self) did not include child process port %d", childPort)
		}
	}
}

func TestLoopbackListenMaskAndEndpoints(t *testing.T) {
	cases := []struct {
		mask     LoopbackListenMask
		urlHost  string
		wantHost string
		wantOK   bool
	}{
		{ListenIPv4, "localhost", "127.0.0.1", true},
		{ListenIPv4, "127.0.0.1", "127.0.0.1", true},
		{ListenIPv4, "::1", "", false},
		{ListenIPv6, "localhost", "::1", true},
		{ListenIPv6, "::1", "::1", true},
		{ListenIPv6, "127.0.0.1", "", false},
		{ListenIPv6Wildcard, "localhost", "::1", true},
		{ListenIPv6Wildcard, "::1", "::1", true},
		{ListenIPv6Wildcard, "127.0.0.1", "::1", true},
		{ListenIPv4 | ListenIPv6, "localhost", "127.0.0.1", true},
		{ListenIPv4 | ListenIPv6, "::1", "::1", true},
		{ListenIPv4 | ListenIPv6, "127.0.0.1", "127.0.0.1", true},
		{0, "localhost", "", false},
	}
	for _, tc := range cases {
		gotHost, gotOK := tc.mask.ResolveDialHost(tc.urlHost)
		if gotHost != tc.wantHost || gotOK != tc.wantOK {
			t.Errorf("mask=%v.ResolveDialHost(%q) = (%q, %v), want (%q, %v)",
				tc.mask, tc.urlHost, gotHost, gotOK, tc.wantHost, tc.wantOK)
		}
	}

	// Only 127.0.0.1 is dialed, so a listener on another 127.x address
	// cannot be reached through it.
	if got := parseListenHostMask("127.0.0.5", false); got != 0 {
		t.Errorf("parseListenHostMask(127.0.0.5, false) = %v, want 0", got)
	}
	if got := parseListenHostMask("::ffff:127.0.0.1", true); got != ListenIPv4 {
		t.Errorf("parseListenHostMask(::ffff:127.0.0.1, true) = %v, want ListenIPv4", got)
	}
	if got := parseListenHostMask("[::1]", true); got != ListenIPv6 {
		t.Errorf("parseListenHostMask([::1], true) = %v, want ListenIPv6", got)
	}
	if got := parseListenHostMask("*", false); got != ListenIPv4 {
		t.Errorf("parseListenHostMask(*, false) = %v, want ListenIPv4", got)
	}
	if got := parseListenHostMask("*", true); got != ListenIPv6Wildcard {
		t.Errorf("parseListenHostMask(*, true) = %v, want ListenIPv6Wildcard", got)
	}

	ln4, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln4.Close()
	port4 := ln4.Addr().(*net.TCPAddr).Port

	var port6 int
	if ln6, err := net.Listen("tcp6", "[::1]:0"); err == nil {
		defer ln6.Close()
		port6 = ln6.Addr().(*net.TCPAddr).Port
	}

	endpoints, err := ProcessTreeListeningEndpoints(context.Background(), os.Getpid())
	if err != nil {
		t.Fatalf("ProcessTreeListeningEndpoints(self): %v", err)
	}
	if endpoints[port4]&ListenIPv4 == 0 || endpoints[port4]&ListenIPv6 != 0 {
		t.Fatalf("port4 (%d) mask = %v, want ListenIPv4 only", port4, endpoints[port4])
	}
	if port6 > 0 {
		if endpoints[port6]&ListenIPv6 == 0 || endpoints[port6]&ListenIPv4 != 0 {
			t.Fatalf("port6 (%d) mask = %v, want ListenIPv6 only", port6, endpoints[port6])
		}
	}

	// macOS has only 127.0.0.1 unless an alias is added.
	if alias, err := net.Listen("tcp4", "127.0.0.2:0"); err == nil {
		defer alias.Close()
		aliasPort := alias.Addr().(*net.TCPAddr).Port
		endpoints, err := ProcessTreeListeningEndpoints(context.Background(), os.Getpid())
		if err != nil {
			t.Fatal(err)
		}
		if mask := endpoints[aliasPort]; mask != 0 {
			t.Fatalf("a listener on 127.0.0.2 has mask %v, want none", mask)
		}
	}
}
