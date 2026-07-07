package usage

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockSource returns a fixed sequence of samples, cycling on the last one,
// and counts how many times Capture was called.
type mockSource struct {
	mu      sync.Mutex
	samples []Sample
	calls   int
	err     error
}

func (m *mockSource) Capture() (Sample, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if m.err != nil {
		return Sample{}, m.err
	}
	i := m.calls - 1
	if i >= len(m.samples) {
		i = len(m.samples) - 1
	}
	return m.samples[i], nil
}

func (m *mockSource) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *mockSource) Close() error { return nil }

func sampleActiveFirefox() Sample {
	return Sample{
		Timestamp: time.Date(2026, 7, 7, 14, 3, 0, 0, time.UTC),
		IdleMS:    1200,
		Windows: []Window{
			{Active: true, Class: "firefox", Instance: "Navigator", Title: "arbtt - GitHub"},
			{Active: false, Class: "Alacritty", Title: "vim usage_test.go"},
		},
	}
}

func TestSampleActive(t *testing.T) {
	s := sampleActiveFirefox()
	got := s.Active()
	if got == nil {
		t.Fatal("Active() returned nil, want the firefox window")
	}
	if got.Class != "firefox" || got.Title != "arbtt - GitHub" {
		t.Errorf("Active() = %+v, want firefox/arbtt", got)
	}

	none := Sample{Windows: []Window{{Active: false, Class: "x"}}}
	if none.Active() != nil {
		t.Error("Active() = non-nil for a sample with no focused window")
	}
}

func TestJSONLSinkWritesOneLinePerSample(t *testing.T) {
	var buf bytes.Buffer
	sink := NewJSONLSink(&buf)

	if err := sink.Write(sampleActiveFirefox()); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := sink.Write(sampleActiveFirefox()); err != nil {
		t.Fatalf("Write: %v", err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2:\n%s", len(lines), buf.String())
	}

	var round Sample
	if err := json.Unmarshal([]byte(lines[0]), &round); err != nil {
		t.Fatalf("unmarshal line: %v", err)
	}
	if round.IdleMS != 1200 {
		t.Errorf("IdleMS round-trip = %d, want 1200", round.IdleMS)
	}
	if a := round.Active(); a == nil || a.Class != "firefox" {
		t.Errorf("active window did not round-trip: %+v", a)
	}
	// Instance omitempty: the Alacritty window has no instance, so the second
	// window's JSON must not contain an "instance" key.
	if strings.Count(lines[0], "\"instance\"") != 1 {
		t.Errorf("expected exactly one instance field, got line: %s", lines[0])
	}
}

func TestTrackerSamplesImmediatelyThenTicks(t *testing.T) {
	src := &mockSource{samples: []Sample{sampleActiveFirefox()}}
	var buf bytes.Buffer
	sink := NewJSONLSink(&buf)

	tracker := NewTracker(src, sink, Config{Interval: 5 * time.Millisecond})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = tracker.Run(ctx)
		close(done)
	}()

	// Give it time for the immediate sample plus a few ticks.
	time.Sleep(40 * time.Millisecond)
	cancel()
	<-done

	if got := src.callCount(); got < 2 {
		t.Errorf("Capture called %d times, want >= 2 (immediate + ticks)", got)
	}
	if buf.Len() == 0 {
		t.Error("sink received no samples")
	}
}

func TestTrackerReportsErrorsAndKeepsRunning(t *testing.T) {
	src := &mockSource{err: errTest}
	var mu sync.Mutex
	var errs int
	cfg := Config{
		Interval: 5 * time.Millisecond,
		OnError: func(error) {
			mu.Lock()
			errs++
			mu.Unlock()
		},
	}
	tracker := NewTracker(src, NewJSONLSink(&bytes.Buffer{}), cfg)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = tracker.Run(ctx); close(done) }()
	time.Sleep(30 * time.Millisecond)
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	if errs < 1 {
		t.Error("OnError was never called despite a failing source")
	}
}

func TestNewTrackerDefaultsInterval(t *testing.T) {
	tr := NewTracker(&mockSource{samples: []Sample{{}}}, NewJSONLSink(&bytes.Buffer{}), Config{})
	if tr.cfg.Interval != DefaultInterval {
		t.Errorf("Interval = %s, want default %s", tr.cfg.Interval, DefaultInterval)
	}
}

type testError struct{}

func (testError) Error() string { return "boom" }

var errTest = testError{}
