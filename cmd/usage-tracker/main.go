// Command usage-tracker is a standalone, arbtt-style desktop usage logger.
//
// It samples the open windows on an X11 desktop at a fixed interval and
// appends each sample as a JSON line to a log file. Later tooling can read the
// log to compute time-usage patterns (which apps/sites, and for how long,
// excluding idle time).
//
// Usage:
//
//	# Log to the default location ($XDG_DATA_HOME/glocker/usage.jsonl)
//	usage-tracker
//
//	# Custom log path and sampling interval, echoing the active window
//	usage-tracker -out ~/usage.jsonl -interval 10s -v
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"glocker/internal/usage"
)

func main() {
	out := flag.String("out", defaultLogPath(), "Path to the JSONL usage log")
	interval := flag.Duration("interval", usage.DefaultInterval, "Sampling interval")
	verbose := flag.Bool("v", false, "Echo each sample's active window to stderr")
	flag.Parse()

	source, err := usage.NewX11Source()
	if err != nil {
		log.Fatalf("usage-tracker: %v", err)
	}
	defer source.Close()

	sink, err := usage.NewJSONLFileSink(*out)
	if err != nil {
		log.Fatalf("usage-tracker: %v", err)
	}
	defer sink.Close()

	var finalSink usage.Sink = sink
	if *verbose {
		finalSink = &verboseSink{inner: sink}
	}

	cfg := usage.Config{
		Interval: *interval,
		OnError:  func(err error) { log.Printf("usage-tracker: %v", err) },
	}
	tracker := usage.NewTracker(source, finalSink, cfg)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("usage-tracker: logging to %s every %s (Ctrl-C to stop)", *out, *interval)
	if err := tracker.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("usage-tracker: %v", err)
	}
	log.Printf("usage-tracker: stopped")
}

// verboseSink prints the active window of each sample to stderr, then
// delegates persistence to the wrapped Sink.
type verboseSink struct {
	inner usage.Sink
}

func (v *verboseSink) Write(s usage.Sample) error {
	if a := s.Active(); a != nil {
		fmt.Fprintf(os.Stderr, "%s  [%s] %s (idle %dms)\n",
			s.Timestamp.Format("15:04:05"), a.Class, a.Title, s.IdleMS)
	} else {
		fmt.Fprintf(os.Stderr, "%s  (no active window)\n", s.Timestamp.Format("15:04:05"))
	}
	return v.inner.Write(s)
}

func (v *verboseSink) Close() error { return v.inner.Close() }

// defaultLogPath returns $XDG_DATA_HOME/glocker/usage.jsonl, falling back to
// ~/.local/share/glocker/usage.jsonl.
func defaultLogPath() string {
	dir := os.Getenv("XDG_DATA_HOME")
	if dir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, ".local", "share")
		}
	}
	return filepath.Join(dir, "glocker", "usage.jsonl")
}
