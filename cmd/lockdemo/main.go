// Command lockdemo demonstrates the screen locker.
//
// Usage:
//
//	# Time-based lock (default)
//	go run ./cmd/lockdemo -duration 10s
//	go run ./cmd/lockdemo -duration 1m -message "Taking a break"
//
//	# Text-based lock (requires typing text to unlock)
//	go run ./cmd/lockdemo -text /path/to/file.txt
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"glocker/internal/lock"
)

func main() {
	duration := flag.Duration("duration", 10*time.Second, "Lock duration (for time-based lock)")
	message := flag.String("message", "Screen locked", "Message to display")
	textFile := flag.String("text", "", "Path to text file (enables text-based lock)")
	flag.Parse()

	// Text-based lock mode
	if *textFile != "" {
		fmt.Printf("Locking screen until text from %s is typed...\n", *textFile)
		fmt.Println("Type the displayed text exactly and press Enter to unlock.")

		locker, err := lock.NewTextLockerFromFile(*textFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating text locker: %v\n", err)
			os.Exit(1)
		}

		if err := locker.Lock(); err != nil {
			fmt.Fprintf(os.Stderr, "Error locking screen: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("Screen unlocked.")
		return
	}

	// Time-based lock mode (default)
	fmt.Printf("Locking screen for %v...\n", *duration)
	fmt.Println("The screen will automatically unlock when the timer expires.")

	locker, err := lock.New(lock.Config{
		Duration: *duration,
		Message:  *message,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating locker: %v\n", err)
		os.Exit(1)
	}

	if err := locker.Lock(); err != nil {
		fmt.Fprintf(os.Stderr, "Error locking screen: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Screen unlocked.")
}
