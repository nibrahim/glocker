// Command mindful is a standalone harness for the metronome-paced mindful
// typing gate (internal/mindful). It lets us tune the feel of the challenge in
// isolation before wiring it into the uninstall friction path.
//
//	go run ./cmd/mindful                 # base tier, defaults
//	go run ./cmd/mindful -lines 2        # two chained sentences
//	go run ./cmd/mindful -interval 800ms -deadline 1500ms
//
// Exit code 0 = passed, 1 = aborted (Esc/Ctrl-C).
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"glocker/internal/mindful"
)

func main() {
	interval := flag.Duration("interval", time.Second, "per-character reveal cadence")
	deadline := flag.Duration("deadline", 2*time.Second, "how long after reveal a character may be typed before reset")
	grace := flag.Duration("grace", 1200*time.Millisecond, "pause before the first character is revealed")
	lines := flag.Int("lines", 1, "number of sentences to chain (friction tier)")
	flag.Parse()

	passed, err := mindful.Run(mindful.Options{
		Lines:    *lines,
		Interval: *interval,
		Deadline: *deadline,
		Grace:    *grace,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "mindful: "+err.Error())
		os.Exit(2)
	}

	if passed {
		fmt.Println("✓ passed")
		os.Exit(0)
	}
	fmt.Println("✗ aborted")
	os.Exit(1)
}
