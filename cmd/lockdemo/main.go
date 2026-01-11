// Command lockdemo demonstrates the timeout-based screen locker.
//
// Usage:
//
//	go run ./cmd/lockdemo -duration 10s
//	go run ./cmd/lockdemo -duration 1m -message "Taking a break"
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"glocker/internal/lock"
)

func main() {
	duration := flag.Duration("duration", 10*time.Second, "Lock duration")
	message := flag.String("message", "Screen locked", "Message to display")
	flag.Parse()

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
