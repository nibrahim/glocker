package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
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
	uninstallReason := flag.String("uninstall", "", "Uninstall Glocker and revert all changes (provide reason)")
	onceFlag := flag.Bool("once", false, "Run enforcement once and exit")
	enforceFlag := flag.Bool("enforce", false, "Run enforcement loop (runs continuously)")
	statusFlag := flag.Bool("status", false, "Show current status and configuration")
	reloadFlag := flag.Bool("reload", false, "Reload configuration from config file")
	blockHosts := flag.String("block", "", "Comma-separated list of hosts to add to always block list")
	unblockHosts := flag.String("unblock", "", "Comma-separated list of hosts to temporarily unblock (format: 'domain1,domain2:reason')")
	addKeyword := flag.String("add-keyword", "", "Comma-separated list of keywords to add to both URL and content keyword lists")
	panicMinutes := flag.Int("panic", 0, "Enter panic mode for N minutes (suspends system and re-suspends on early wake)")
	lockFlag := flag.Bool("lock", false, "Immediately lock sudoers access (ignores time windows)")
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

	// Handle uninstallation
	if *uninstallReason != "" {
		if !install.RunningAsRoot(true) {
			log.Fatal("Uninstall must be run as root (use sudo)")
		}

		// Check if glocker is actually installed
		if _, err := os.Stat("/usr/local/bin/glocker"); os.IsNotExist(err) {
			log.Fatal("Glocker is not installed. Nothing to uninstall.")
		}

		// Send uninstall request to daemon via socket
		conn, err := net.Dial("unix", ipc.SocketPath)
		if err != nil {
			log.Fatalf("Failed to connect to glocker service: %v", err)
		}
		defer conn.Close()

		message := fmt.Sprintf("uninstall:%s\n", *uninstallReason)
		conn.Write([]byte(message))

		// Read initial response
		reader := bufio.NewReader(conn)
		response, err := reader.ReadString('\n')
		if err != nil {
			log.Fatalf("Failed to read response: %v", err)
		}
		log.Printf("Response: %s", strings.TrimSpace(response))

		// Wait for completion signal
		log.Println("Waiting for uninstall process to complete...")
		completionResponse, err := reader.ReadString('\n')
		if err != nil {
			log.Fatalf("Failed to read completion response: %v", err)
		}
		log.Printf("Completion: %s", strings.TrimSpace(completionResponse))

		return
	}

	// Handle socket-based commands (don't need config)
	if *reloadFlag {
		conn, err := net.Dial("unix", ipc.SocketPath)
		if err != nil {
			log.Fatalf("Failed to connect to glocker service: %v", err)
		}
		defer conn.Close()

		conn.Write([]byte("reload\n"))

		reader := bufio.NewReader(conn)
		response, err := reader.ReadString('\n')
		if err != nil {
			log.Fatalf("Failed to read response: %v", err)
		}

		log.Printf("Response: %s", strings.TrimSpace(response))
		return
	}

	if *blockHosts != "" {
		conn, err := net.Dial("unix", ipc.SocketPath)
		if err != nil {
			log.Fatalf("Failed to connect to glocker service: %v", err)
		}
		defer conn.Close()

		message := fmt.Sprintf("block:%s\n", *blockHosts)
		conn.Write([]byte(message))

		reader := bufio.NewReader(conn)
		response, err := reader.ReadString('\n')
		if err != nil {
			log.Fatalf("Failed to read response: %v", err)
		}

		log.Printf("Response: %s", strings.TrimSpace(response))
		log.Println("Domains will be permanently blocked.")
		return
	}

	if *unblockHosts != "" {
		// Parse format: "domain1,domain2:reason"
		parts := strings.SplitN(*unblockHosts, ":", 2)
		if len(parts) != 2 {
			log.Fatal("ERROR: Reason required. Use format: 'domain1,domain2:reason'")
		}

		domains := strings.TrimSpace(parts[0])
		reason := strings.TrimSpace(parts[1])

		if domains == "" {
			log.Fatal("ERROR: No domains specified")
		}

		if reason == "" {
			log.Fatal("ERROR: Reason cannot be empty")
		}

		conn, err := net.Dial("unix", ipc.SocketPath)
		if err != nil {
			log.Fatalf("Failed to connect to glocker service: %v", err)
		}
		defer conn.Close()

		message := fmt.Sprintf("unblock:%s:%s\n", domains, reason)
		conn.Write([]byte(message))

		reader := bufio.NewReader(conn)
		response, err := reader.ReadString('\n')
		if err != nil {
			log.Fatalf("Failed to read response: %v", err)
		}

		log.Printf("Response: %s", strings.TrimSpace(response))
		return
	}

	if *addKeyword != "" {
		conn, err := net.Dial("unix", ipc.SocketPath)
		if err != nil {
			log.Fatalf("Failed to connect to glocker service: %v", err)
		}
		defer conn.Close()

		message := fmt.Sprintf("add-keyword:%s\n", *addKeyword)
		conn.Write([]byte(message))

		reader := bufio.NewReader(conn)
		response, err := reader.ReadString('\n')
		if err != nil {
			log.Fatalf("Failed to read response: %v", err)
		}

		log.Printf("Response: %s", strings.TrimSpace(response))
		log.Println("Keywords will be added to both URL and content keyword lists.")
		return
	}

	if *panicMinutes > 0 {
		conn, err := net.Dial("unix", ipc.SocketPath)
		if err != nil {
			log.Fatalf("Failed to connect to glocker service: %v", err)
		}
		defer conn.Close()

		message := fmt.Sprintf("panic:%d\n", *panicMinutes)
		conn.Write([]byte(message))

		reader := bufio.NewReader(conn)
		response, err := reader.ReadString('\n')
		if err != nil {
			log.Fatalf("Failed to read response: %v", err)
		}

		log.Printf("%s", strings.TrimSpace(response))
		return
	}

	if *lockFlag {
		conn, err := net.Dial("unix", ipc.SocketPath)
		if err != nil {
			log.Fatalf("Failed to connect to glocker service: %v", err)
		}
		defer conn.Close()

		conn.Write([]byte("lock\n"))

		reader := bufio.NewReader(conn)
		response, err := reader.ReadString('\n')
		if err != nil {
			log.Fatalf("Failed to read response: %v", err)
		}

		log.Printf("Response: %s", strings.TrimSpace(response))
		return
	}

	// Handle default behavior (no flags) - show status or help
	if flag.NFlag() == 0 {
		// Check if socket exists and daemon is running
		if _, err := os.Stat(ipc.SocketPath); err == nil {
			conn, err := net.Dial("unix", ipc.SocketPath)
			if err == nil {
				defer conn.Close()

				log.Println("=== LIVE STATUS ===")
				conn.Write([]byte("status\n"))

				// Read response until END
				scanner := bufio.NewScanner(conn)
				for scanner.Scan() {
					line := scanner.Text()
					if line == "END" {
						break
					}
					fmt.Println(line)
				}
				return
			}
		}

		// Socket not available, show help
		fmt.Println("Glocker - Domain and System Access Control")
		fmt.Println()
		fmt.Println("Usage:")
		flag.PrintDefaults()
		return
	}

	// Load configuration (needed for status, once, and enforce modes)
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Setup logging
	config.SetupLogging(cfg)

	// Handle status command
	if *statusFlag {
		// Try to get live status from socket first
		if _, err := os.Stat(ipc.SocketPath); err == nil {
			conn, err := net.Dial("unix", ipc.SocketPath)
			if err == nil {
				defer conn.Close()

				log.Println("=== LIVE STATUS ===")
				conn.Write([]byte("status\n"))

				scanner := bufio.NewScanner(conn)
				for scanner.Scan() {
					line := scanner.Text()
					if line == "END" {
						break
					}
					fmt.Println(line)
				}
				return
			}
		}

		// Socket not available, fall back to static status
		log.Println("=== STATIC STATUS ===")
		log.Println("(Service not running - showing configuration only)")
		response := cli.GetStatusResponse(cfg)
		fmt.Print(response)

		// Also show dry-run enforcement
		enforcement.RunOnce(cfg, true)
		return
	}

	// Handle once mode
	if *onceFlag {
		enforcement.RunOnce(cfg, false)
		return
	}

	// Start daemon mode (only with -enforce flag or if no flags matched)
	if !*enforceFlag {
		log.Fatal("No matching command. Use -h for help.")
	}

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
