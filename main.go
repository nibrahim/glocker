package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"glocker/cli"
	"glocker/config"
	"glocker/enforcement"
	"glocker/install"
	"glocker/ipc"
	"glocker/monitoring"
	"glocker/web"
)

func main() {
	// Parse command-line flags
	installFlag := flag.Bool("install", false, "Install glocker as a system service")
	onceFlag := flag.Bool("once", false, "Run enforcement once and exit")
	statusFlag := flag.Bool("status", false, "Show current status")
	versionFlag := flag.Bool("version", false, "Show version information")

	flag.Parse()

	// Handle version flag
	if *versionFlag {
		log.Println("Glocker v1.0.0")
		return
	}

	// Handle installation
	if *installFlag {
		if !install.RunningAsRoot(true) {
			log.Fatal("Installation must be run as root (use sudo)")
		}
		if err := install.InstallGlocker(); err != nil {
			log.Fatalf("Installation failed: %v", err)
		}
		return
	}

	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Setup logging
	config.SetupLogging(cfg)

	// Handle status command
	if *statusFlag {
		response := cli.GetStatusResponse(cfg)
		log.Print(response)
		return
	}

	// Handle once mode
	if *onceFlag {
		enforcement.RunOnce(cfg, false)
		return
	}

	// Start daemon mode
	log.Println("Starting glocker daemon...")

	// Setup IPC socket
	if err := ipc.SetupCommunication(cfg); err != nil {
		log.Fatalf("Failed to setup IPC: %v", err)
	}

	// Start monitoring goroutines
	if cfg.TamperDetection.Enabled {
		// Tamper detection monitoring (simplified for now)
		go func() {
			log.Println("Tamper detection enabled")
			// Full implementation would call monitoring.MonitorTampering
		}()
	}

	if cfg.ForbiddenPrograms.Enabled {
		go monitoring.MonitorForbiddenPrograms(cfg)
	}

	if cfg.ViolationTracking.Enabled {
		go monitoring.MonitorViolations(cfg)
	}

	if cfg.PanicCommand != "" {
		go monitoring.MonitorPanicMode(cfg)
	}

	// Start web tracking server
	if cfg.WebTracking.Enabled || cfg.ContentMonitoring.Enabled {
		go web.StartWebTrackingServer(cfg)
	}

	// Main enforcement loop
	ticker := time.NewTicker(time.Duration(cfg.EnforceInterval) * time.Second)
	defer ticker.Stop()

	// Run once immediately
	enforcement.RunOnce(cfg, false)

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	log.Println("Glocker daemon started successfully")

	for {
		select {
		case <-ticker.C:
			enforcement.RunOnce(cfg, false)
		case sig := <-sigChan:
			log.Printf("Received signal %v, shutting down...", sig)
			return
		}
	}
}
