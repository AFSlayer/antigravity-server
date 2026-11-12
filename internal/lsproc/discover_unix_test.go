//go:build !windows

package lsproc

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"testing"
)

func TestPortsViaProcWithLocalListener(t *testing.T) {
	// If /proc is not available (e.g. macOS), skip
	if _, err := os.Stat("/proc"); os.IsNotExist(err) {
		t.Skip("/proc is not available on this platform")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create test listener: %v", err)
	}
	defer ln.Close()

	expectedPort := ln.Addr().(*net.TCPAddr).Port

	ports, err := portsViaProc(os.Getpid())
	if err != nil {
		t.Fatalf("portsViaProc failed: %v", err)
	}

	found := false
	for _, p := range ports {
		if p == expectedPort {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("portsViaProc did not find expected port %d in %v", expectedPort, ports)
	}
}

func TestListeningPortsFallbackChain(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create test listener: %v", err)
	}
	defer ln.Close()

	expectedPort := ln.Addr().(*net.TCPAddr).Port

	ports, err := listeningPorts(os.Getpid())
	if err != nil {
		t.Fatalf("listeningPorts failed: %v", err)
	}

	found := false
	for _, p := range ports {
		if p == expectedPort {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("listeningPorts did not find expected port %d in %v", expectedPort, ports)
	}
}

// /proc/net/tcp writes each 32-bit word of an address in host byte order.
func TestParseProcHexAddrMask(t *testing.T) {
	v4 := func(a, b, c, d byte) string {
		return fmt.Sprintf("%08X", binary.NativeEndian.Uint32([]byte{a, b, c, d}))
	}
	v6 := func(ip net.IP) string {
		out := ""
		for i := 0; i < 16; i += 4 {
			out += fmt.Sprintf("%08X", binary.NativeEndian.Uint32(ip[i:i+4]))
		}
		return out
	}
	for _, tc := range []struct {
		hex  string
		ipv6 bool
		want LoopbackListenMask
	}{
		{v4(127, 0, 0, 1), false, ListenIPv4},
		{v4(0, 0, 0, 0), false, ListenIPv4},
		{v4(127, 0, 0, 2), false, 0},
		{v4(10, 0, 0, 1), false, 0},
		{v6(net.IPv6loopback), true, ListenIPv6},
		{v6(net.IPv6unspecified), true, ListenIPv6Wildcard},
		{v6(net.ParseIP("::ffff:127.0.0.1")), true, ListenIPv4},
		{v6(net.ParseIP("::ffff:127.0.0.2")), true, 0},
	} {
		if got := parseProcHexAddrMask(tc.hex, tc.ipv6); got != tc.want {
			t.Errorf("parseProcHexAddrMask(%s, %v) = %v, want %v", tc.hex, tc.ipv6, got, tc.want)
		}
	}
}
