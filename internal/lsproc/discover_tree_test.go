package lsproc

import (
	"net"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestListeningPortsAndProcessTreeListeningPorts(t *testing.T) {
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

	treePorts, err := ProcessTreeListeningPorts(os.Getpid())
	if err != nil {
		t.Fatalf("ProcessTreeListeningPorts(self): %v", err)
	}
	foundInTree := false
	for _, p := range treePorts {
		if p == selfPort {
			foundInTree = true
			break
		}
	}
	if !foundInTree {
		t.Fatalf("ProcessTreeListeningPorts(self) = %v, missing %d", treePorts, selfPort)
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
			ports, err := ProcessTreeListeningPorts(os.Getpid())
			if err == nil {
				for _, p := range ports {
					if p == childPort {
						sawChild = true
						break
					}
				}
			}
			if sawChild {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if !sawChild {
			t.Fatalf("ProcessTreeListeningPorts(self) did not include child process port %d", childPort)
		}
	}
}
