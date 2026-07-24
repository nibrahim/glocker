package heartbeat

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// startPongServer spins up a unix-socket listener that answers "ping" with
// "pong", mimicking the glocker IPC daemon. It returns the socket path.
func startPongServer(t *testing.T) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "glocker.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 16)
				if _, err := c.Read(buf); err != nil {
					return
				}
				c.Write([]byte("pong\n"))
			}(conn)
		}
	}()
	return sock
}

func TestProbe(t *testing.T) {
	sock := startPongServer(t)
	if !Probe(sock, time.Second) {
		t.Error("expected Probe to report alive against a responsive socket")
	}

	// A path with no listener must read as not-alive.
	dead := filepath.Join(t.TempDir(), "nope.sock")
	if Probe(dead, 200*time.Millisecond) {
		t.Error("expected Probe to report not-alive against a missing socket")
	}
}

func TestAppendAndRun(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "hb.jsonl")
	now := time.Now().Truncate(time.Second)

	// Alive path: real listener.
	sock := startPongServer(t)
	alive, err := Run(sock, logPath, time.Second, now)
	if err != nil {
		t.Fatalf("Run (alive): %v", err)
	}
	if !alive {
		t.Error("expected alive=true against responsive socket")
	}

	// Down path: no listener. A second sample is appended.
	down, err := Run(filepath.Join(t.TempDir(), "nope.sock"), logPath, 200*time.Millisecond, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Run (down): %v", err)
	}
	if down {
		t.Error("expected alive=false against missing socket")
	}

	// The log should now hold two samples, in order, matching what we recorded.
	samples := readSamples(t, logPath)
	if len(samples) != 2 {
		t.Fatalf("expected 2 samples, got %d", len(samples))
	}
	if !samples[0].Alive || samples[1].Alive {
		t.Errorf("expected [alive, down], got [%v, %v]", samples[0].Alive, samples[1].Alive)
	}
	if !samples[0].Timestamp.Equal(now) {
		t.Errorf("first sample timestamp = %v, want %v", samples[0].Timestamp, now)
	}
}

func readSamples(t *testing.T, path string) []Sample {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer f.Close()

	var out []Sample
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var s Sample
		if err := json.Unmarshal(sc.Bytes(), &s); err != nil {
			t.Fatalf("unmarshal %q: %v", sc.Text(), err)
		}
		out = append(out, s)
	}
	return out
}
