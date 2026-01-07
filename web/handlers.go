package web

import (
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"glocker/config"
	"glocker/enforcement"
	"glocker/internal/state"
	"glocker/internal/utils"
	"glocker/monitoring"
	"glocker/notify"
)

// HandleWebTrackingRequest processes incoming web tracking requests and enforces blocking.
// It checks if the requested host is in the blocked domains list and takes appropriate action.
func HandleWebTrackingRequest(cfg *config.Config, w http.ResponseWriter, r *http.Request) {
	// Check for blocked page first, before any other processing
	if r.URL.Path == "/blocked" {
		HandleBlockedPageRequest(w, r)
		return
	}

	host := r.Host
	if host == "" {
		host = r.Header.Get("Host")
	}

	// Remove port if present
	if colonIndex := strings.Index(host, ":"); colonIndex != -1 {
		host = host[:colonIndex]
	}

	slog.Debug("Web tracking request received", "host", host, "url", r.URL.String(), "method", r.Method)

	// Check if this host is in our blocked domains list
	isBlocked := false
	matchedDomain := ""
	now := time.Now()
	blockedDomains := enforcement.GetDomainsToBlock(cfg, now)

	for _, blockedDomain := range blockedDomains {
		// Direct match
		if host == blockedDomain {
			isBlocked = true
			matchedDomain = blockedDomain
			break
		}
		// www subdomain match
		if host == "www."+blockedDomain {
			isBlocked = true
			matchedDomain = blockedDomain
			break
		}
		// Any subdomain match
		if strings.HasSuffix(host, "."+blockedDomain) {
			isBlocked = true
			matchedDomain = blockedDomain
			break
		}
		// Reverse check - if host is the base domain and blocked domain has www
		if strings.HasPrefix(blockedDomain, "www.") && host == blockedDomain[4:] {
			isBlocked = true
			matchedDomain = blockedDomain
			break
		}
	}

	slog.Debug("Host blocking check", "host", host, "is_blocked", isBlocked, "matched_domain", matchedDomain, "blocked_domains_count", len(blockedDomains))

	if isBlocked {
		// Determine the blocking reason
		blockingReason := GetBlockingReason(cfg, matchedDomain, time.Now())
		log.Printf("BLOCKED SITE ACCESS: %s -> matched domain: %s -> reason: %s", host, matchedDomain, blockingReason)

		// Record violation
		if cfg.ViolationTracking.Enabled {
			monitoring.RecordViolation(cfg, "web_access", host, r.URL.String())
		}

		// Execute the configured command
		if cfg.WebTracking.Command != "" {
			go executeWebTrackingCommand(cfg, host, r)
		}

		// Send desktop notification
		notify.SendNotification(cfg, "Glocker Alert",
			fmt.Sprintf("Blocked access to %s", host),
			"normal", "dialog-information")

		// Send accountability email
		if cfg.Accountability.Enabled {
			subject := "GLOCKER ALERT: Blocked Site Access Attempt"
			body := fmt.Sprintf("An attempt to access a blocked site was detected at %s:\n\n", time.Now().Format("2006-01-02 15:04:05"))
			body += fmt.Sprintf("Host: %s\n", host)
			body += fmt.Sprintf("Matched Domain: %s\n", matchedDomain)
			body += fmt.Sprintf("Blocking Reason: %s\n", blockingReason)
			body += fmt.Sprintf("URL: %s\n", r.URL.String())
			body += fmt.Sprintf("Method: %s\n", r.Method)
			body += fmt.Sprintf("User-Agent: %s\n", r.Header.Get("User-Agent"))
			body += fmt.Sprintf("Remote Address: %s\n", r.RemoteAddr)
			body += "\nThis is an automated alert from Glocker."

			if err := notify.SendEmail(cfg, subject, body); err != nil {
				log.Printf("Failed to send web tracking accountability email: %v", err)
			}
		}

		// Redirect to localhost blocked page to avoid double violation
		blockedURL := fmt.Sprintf("http://127.0.0.1/blocked?domain=%s&matched=%s&url=%s", host, matchedDomain, r.URL.String())
		http.Redirect(w, r, blockedURL, http.StatusFound)
	} else {
		// Not a blocked domain, return a simple response
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}
}

// executeWebTrackingCommand executes the configured command when a blocked site is accessed.
func executeWebTrackingCommand(cfg *config.Config, host string, r *http.Request) {
	if cfg.WebTracking.Command == "" {
		return
	}

	slog.Debug("Executing web tracking command", "host", host, "command", cfg.WebTracking.Command)

	// Split command into parts for proper execution
	parts := strings.Fields(cfg.WebTracking.Command)
	if len(parts) == 0 {
		return
	}

	cmd := exec.Command(parts[0], parts[1:]...)

	// Set environment variables with information about the blocked access attempt
	cmd.Env = append(os.Environ(),
		"GLOCKER_BLOCKED_HOST="+host,
		"GLOCKER_BLOCKED_URL="+r.URL.String(),
		"GLOCKER_BLOCKED_METHOD="+r.Method,
		"GLOCKER_BLOCKED_USER_AGENT="+r.Header.Get("User-Agent"),
		"GLOCKER_BLOCKED_REMOTE_ADDR="+r.RemoteAddr,
		"GLOCKER_BLOCKED_TIME="+time.Now().Format("2006-01-02 15:04:05"),
	)

	if err := cmd.Run(); err != nil {
		log.Printf("Failed to execute web tracking command: %v", err)
	} else {
		slog.Debug("Web tracking command executed successfully", "host", host)
	}
}

// HandleKeywordsRequest returns the current keyword configuration for browser extensions.
func HandleKeywordsRequest(cfg *config.Config, w http.ResponseWriter, r *http.Request) {
	// Only accept GET requests
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Set CORS headers to allow browser extension access
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Content-Type", "application/json")

	// Combine content keywords with URL keywords
	combinedContentKeywords := make([]string, 0, len(cfg.ExtensionKeywords.ContentKeywords)+len(cfg.ExtensionKeywords.URLKeywords))
	combinedContentKeywords = append(combinedContentKeywords, cfg.ExtensionKeywords.ContentKeywords...)
	combinedContentKeywords = append(combinedContentKeywords, cfg.ExtensionKeywords.URLKeywords...)

	// Create response with keywords
	response := map[string]interface{}{
		"url_keywords":     cfg.ExtensionKeywords.URLKeywords,
		"content_keywords": combinedContentKeywords,
		"whitelist":        cfg.ExtensionKeywords.Whitelist,
	}

	// Encode and send response
	if err := json.NewEncoder(w).Encode(response); err != nil {
		slog.Debug("Failed to encode keywords response", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	slog.Debug("Keywords request served", "url_keywords_count", len(cfg.ExtensionKeywords.URLKeywords), "content_keywords_count", len(combinedContentKeywords))
}

// HandleReportRequest processes content monitoring reports from browser extensions.
func HandleReportRequest(cfg *config.Config, w http.ResponseWriter, r *http.Request) {
	slog.Info("Got a request here", "method", r.Method, "value", http.MethodPost)
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Check if content monitoring is enabled
	if !cfg.ContentMonitoring.Enabled {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	// Parse JSON body
	var report state.ContentReport
	if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
		slog.Debug("Failed to parse report JSON", "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Record violation
	if cfg.ViolationTracking.Enabled {
		monitoring.RecordViolation(cfg, "content_report", report.Domain, report.URL)
	}

	// Log the report
	if err := LogContentReport(cfg, &report); err != nil {
		slog.Debug("Failed to log content report", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	slog.Debug("Content report logged", "url", report.URL, "trigger", report.Trigger)
	log.Printf("CONTENT REPORT: %s - %s", report.Trigger, report.URL)

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// HandleSSERequest manages server-sent events connections for real-time keyword updates.
func HandleSSERequest(cfg *config.Config, w http.ResponseWriter, r *http.Request) {
	// Only accept GET requests
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Create a channel for this client
	clientChan := make(chan string, 10)

	// Add client to the list
	state.AddSSEClient(clientChan)

	slog.Debug("SSE client connected", "total_clients", state.GetSSEClientCount())

	// Combine content keywords with URL keywords for initial send
	combinedContentKeywords := make([]string, 0, len(cfg.ExtensionKeywords.ContentKeywords)+len(cfg.ExtensionKeywords.URLKeywords))
	combinedContentKeywords = append(combinedContentKeywords, cfg.ExtensionKeywords.ContentKeywords...)
	combinedContentKeywords = append(combinedContentKeywords, cfg.ExtensionKeywords.URLKeywords...)

	// Send initial keywords
	initialKeywords := map[string]interface{}{
		"url_keywords":     cfg.ExtensionKeywords.URLKeywords,
		"content_keywords": combinedContentKeywords,
		"whitelist":        cfg.ExtensionKeywords.Whitelist,
	}
	if keywordsJSON, err := json.Marshal(initialKeywords); err == nil {
		fmt.Fprintf(w, "data: %s\n\n", keywordsJSON)
		w.(http.Flusher).Flush()
	}

	// Handle client disconnect
	defer func() {
		state.RemoveSSEClient(clientChan)
		close(clientChan)
		slog.Debug("SSE client disconnected", "remaining_clients", state.GetSSEClientCount())
	}()

	// Keep connection alive and send updates
	for {
		select {
		case message := <-clientChan:
			fmt.Fprintf(w, "data: %s\n\n", message)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		case <-r.Context().Done():
			return
		case <-time.After(30 * time.Second):
			// Send keepalive ping
			fmt.Fprintf(w, ": keepalive\n\n")
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
	}
}

// GetBlockingReason returns a human-readable reason for why a domain is blocked.
func GetBlockingReason(cfg *config.Config, domain string, now time.Time) string {
	currentDay := now.Weekday().String()[:3]
	currentTime := now.Format("15:04")

	// Find the domain in the config
	for _, configDomain := range cfg.Domains {
		if configDomain.Name == domain {
			if configDomain.AlwaysBlock {
				if configDomain.Absolute {
					return "always blocked (absolute - cannot be temporarily unblocked)"
				}
				return "always blocked"
			}

			// Check which time window is active
			for _, window := range configDomain.TimeWindows {
				if slices.Contains(window.Days, currentDay) && utils.IsInTimeWindow(currentTime, window.Start, window.End) {
					return fmt.Sprintf("time-based block (active %s-%s on %s)", window.Start, window.End, strings.Join(window.Days, ","))
				}
			}
		}
	}

	return "unknown blocking rule"
}
