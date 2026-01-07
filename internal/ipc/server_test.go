package ipc

import (
	"testing"
)

func TestSocketPath(t *testing.T) {
	expected := "/tmp/glocker.sock"
	if SocketPath != expected {
		t.Errorf("SocketPath = %s, expected %s", SocketPath, expected)
	}
}

// Note: Full socket testing requires running server, which is tested during integration testing.
