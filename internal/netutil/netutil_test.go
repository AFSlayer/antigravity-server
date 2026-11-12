package netutil

import (
	"errors"
	"fmt"
	"net"
	"syscall"
	"testing"
)

func TestIsConnRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	_, dialErr := net.Dial("tcp", addr)
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{dialErr, true},
		{syscall.Errno(10061), true},
		{fmt.Errorf("dial: %w", syscall.ECONNREFUSED), true},
		{errors.New("No connection could be made because the target machine actively refused it."), true},
		{errors.New("i/o timeout"), false},
		{nil, false},
	} {
		if got := IsConnRefused(tc.err); got != tc.want {
			t.Errorf("IsConnRefused(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}
