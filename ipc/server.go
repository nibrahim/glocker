package ipc

import (
	"bufio"
	"fmt"
	"log"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"

	"glocker/cli"
	"glocker/config"
)

const SocketPath = "/tmp/glocker.sock"

// SetupCommunication creates and starts listening on the Unix domain socket.
func SetupCommunication(cfg *config.Config) error {
	// Remove existing socket
	os.Remove(SocketPath)

	// Create Unix domain socket
	listener, err := net.Listen("unix", SocketPath)
	if err != nil {
		return fmt.Errorf("failed to create socket: %w", err)
	}

	// Set permissions
	if err := os.Chmod(SocketPath, 0600); err != nil {
		log.Printf("Warning: couldn't set socket permissions: %v", err)
	}

	go handleConnections(cfg, listener)
	return nil
}

// handleConnections accepts incoming socket connections.
func handleConnections(cfg *config.Config, listener net.Listener) {
	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Socket accept error: %v", err)
			continue
		}

		go HandleConnection(cfg, conn)
	}
}

// HandleConnection processes commands from a single socket connection.
func HandleConnection(cfg *config.Config, conn net.Conn) {
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) < 1 {
			conn.Write([]byte("ERROR: Invalid format\n"))
			continue
		}

		action := strings.TrimSpace(parts[0])
		slog.Debug("Socket command received", "action", action)

		switch action {
		case "status":
			response := cli.GetStatusResponse(cfg)
			conn.Write([]byte(response))
		case "reload":
			conn.Write([]byte("OK: Reload request received\n"))
			go cli.ProcessReloadRequest(cfg)
		case "unblock":
			if len(parts) != 2 {
				conn.Write([]byte("ERROR: Invalid format. Use 'unblock:domains:reason'\n"))
				continue
			}
			payload := strings.TrimSpace(parts[1])
			payloadParts := strings.SplitN(payload, ":", 2)
			if len(payloadParts) != 2 {
				conn.Write([]byte("ERROR: Reason required. Use 'unblock:domains:reason'\n"))
				continue
			}
			domains := strings.TrimSpace(payloadParts[0])
			reason := strings.TrimSpace(payloadParts[1])
			if reason == "" {
				conn.Write([]byte("ERROR: Reason cannot be empty\n"))
				continue
			}
			conn.Write([]byte("OK: Unblock request received\n"))
			go cli.ProcessUnblockRequest(cfg, domains, reason)
		case "block":
			if len(parts) != 2 {
				conn.Write([]byte("ERROR: Invalid format. Use 'block:domains'\n"))
				continue
			}
			domains := strings.TrimSpace(parts[1])
			conn.Write([]byte("OK: Block request received\n"))
			go cli.ProcessBlockRequest(cfg, domains)
		case "panic":
			if len(parts) != 2 {
				conn.Write([]byte("ERROR: Invalid format. Use 'panic:minutes'\n"))
				continue
			}
			minutesStr := strings.TrimSpace(parts[1])
			minutes, err := strconv.Atoi(minutesStr)
			if err != nil || minutes <= 0 {
				conn.Write([]byte("ERROR: Invalid minutes value. Must be a positive integer\n"))
				continue
			}
			conn.Write([]byte(fmt.Sprintf("OK: Entering panic mode for %d minutes\n", minutes)))
			go cli.ProcessPanicRequest(cfg, minutes)
		default:
			conn.Write([]byte("ERROR: Unknown action\n"))
		}
	}
}

// SendSocketMessage sends a message to the glocker socket and returns the response.
func SendSocketMessage(action, payload string) (string, error) {
	conn, err := net.Dial("unix", SocketPath)
	if err != nil {
		return "", fmt.Errorf("failed to connect to socket: %w", err)
	}
	defer conn.Close()

	message := action
	if payload != "" {
		message += ":" + payload
	}
	message += "\n"

	if _, err := conn.Write([]byte(message)); err != nil {
		return "", fmt.Errorf("failed to send message: %w", err)
	}

	scanner := bufio.NewScanner(conn)
	var response strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if line == "END" {
			break
		}
		response.WriteString(line)
		response.WriteString("\n")
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	return response.String(), nil
}
