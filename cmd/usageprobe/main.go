// Command usageprobe is a throwaway tool for exercising the usage Source
// selector: it picks the backend for the current session, captures one (or
// repeated) Sample(s), and prints them as JSON. Use it to check usage tracking
// on a new machine or session — e.g. a nested Wayland compositor
// (`dbus-run-session -- gnome-shell --nested --wayland`, or `sway`) — without
// installing the daemon:
//
//	go build -o usageprobe ./cmd/usageprobe && ./usageprobe
//	./usageprobe -interval 2s          # watch the active window as you switch apps
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"glocker/internal/usage"
)

func main() {
	display := flag.String("display", "", "X11 DISPLAY override (empty = $DISPLAY)")
	interval := flag.Duration("interval", 0, "capture repeatedly at this interval (0 = once)")
	flag.Parse()

	src, backend, err := usage.NewSource(usage.Options{
		Display:    *display,
		XAuthority: os.Getenv("XAUTHORITY"),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "no usage source: %v\n", err)
		os.Exit(1)
	}
	defer src.Close()
	fmt.Fprintf(os.Stderr, "backend: %s\n", backend)

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	for {
		sample, err := src.Capture()
		if err != nil {
			fmt.Fprintf(os.Stderr, "capture: %v\n", err)
			os.Exit(1)
		}
		if err := enc.Encode(sample); err != nil {
			fmt.Fprintf(os.Stderr, "encode: %v\n", err)
			os.Exit(1)
		}
		if *interval <= 0 {
			return
		}
		time.Sleep(*interval)
	}
}
