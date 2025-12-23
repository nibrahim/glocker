# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

Glocker is a Linux-based distraction blocking system written in Go (single file: `glocker.go`, ~4400 lines). It blocks distracting websites and applications using multiple enforcement mechanisms to make bypassing difficult. The tool runs as a privileged setuid systemd service and uses Unix sockets for IPC.

## Build and Test Commands

```bash
# Build the binary
go build .

# Run a single enforcement cycle (requires sudo)
glocker -once

# Check current status (blocking state, active domains, violations)
glocker -status

# Install as systemd service with setuid privileges
glocker -install

# Uninstall and cleanup all protections
glocker -uninstall
```

## Common Development Workflows

### Running Commands
```bash
# Reload configuration from /etc/glocker/config.yaml
sudo glocker -reload

# Temporarily unblock domains with reason
sudo glocker -unblock "reddit.com,youtube.com:work"

# Add domains to permanent block list
sudo glocker -block "example.com,another.com"

# Add keywords to monitoring lists
sudo glocker -add-keyword "keyword1,keyword2"
```

### Testing Browser Extension
The Firefox extension lives in `extensions/firefox/`. After making changes:
1. Navigate to `about:debugging#/runtime/this-firefox`
2. Load temporary extension from `extensions/firefox/manifest.json`
3. Check browser console for extension logs
4. Monitor `/var/log/glocker-reports.log` for content monitoring events

## Architecture

### Core Components

**Main Service (`glocker.go`)**
- Single Go binary that handles all blocking logic
- Runs as systemd service with setuid root permissions
- Config loaded from `/etc/glocker/config.yaml` (sample in `conf/conf.yaml`)
- Uses Unix socket `/tmp/glocker.sock` for runtime commands

**Enforcement Mechanisms** (configured independently via YAML)
1. **Hosts file blocking** - Modifies `/etc/hosts` with immutable flag
2. **Forbidden programs** - Kills processes during configured time windows
3. **Sudoers modification** - Restricts sudo access during blocking periods
4. **Browser extension** - Content/keyword monitoring via Firefox extension

**Firefox Extension** (`extensions/firefox/`)
- `background.js` - Monitors URL navigation, fetches keywords from glocker service
- `content.js` - Scans page content for problematic keywords
- Reports violations to glocker via HTTP POST to `http://127.0.0.1/report`

### Key Data Structures

All configuration is in YAML with these main structs (glocker.go:39-139):

```go
type Config struct {
    EnableHosts             bool
    EnableFirewall          bool
    EnableForbiddenPrograms bool
    Domains                 []Domain
    Sudoers                 SudoersConfig
    TamperDetection         TamperConfig
    Accountability          AccountabilityConfig
    ContentMonitoring       ContentMonitoringConfig
    ForbiddenPrograms       ForbiddenProgramsConfig
    ExtensionKeywords       ExtensionKeywordsConfig
    ViolationTracking       ViolationTrackingConfig
    Unblocking              UnblockingConfig
    // ... more fields
}

type Domain struct {
    Name        string
    AlwaysBlock bool
    TimeWindows []TimeWindow
    Absolute    bool  // Cannot be temporarily unblocked
}
```

### Control Flow

**Installation** (`glocker -install`):
1. Copies binary to `/usr/local/bin/glocker` with setuid permissions
2. Copies `conf/conf.yaml` to `/etc/glocker/config.yaml`
3. Installs systemd service from `extras/glocker.service`

**Enforcement Loop** (main.go:191-334):
1. Loads config from `/etc/glocker/config.yaml`
2. Starts Unix socket server for IPC (handles reload/uninstall/block/unblock)
3. Launches monitoring goroutines:
   - Tamper detection (monitors file checksums every 30s)
   - Forbidden programs (kills processes every 5s)
   - Violation tracking (counts blocked access attempts)
   - Web tracking HTTP server (port 80 and 443)
4. Main loop applies enforcement every 60s:
   - Updates hosts file with blocked domains
   - Updates firewall rules
   - Updates sudoers restrictions
   - Applies time window logic

**Socket Commands** (glocker.go:398-436):
- Client sends JSON command via Unix socket
- Server processes command and returns JSON response
- Commands: `reload`, `uninstall`, `block`, `unblock`, `status`, `add-keyword`

### Important Files and Paths

Production paths:
- `/etc/glocker/config.yaml` - Main configuration
- `/tmp/glocker.sock` - Unix socket for IPC
- `/var/log/glocker-reports.log` - Content monitoring logs
- `/var/log/glocker-unblocks.log` - Unblock request logs
- `/etc/hosts` - Modified with GLOCKER markers and immutable flag
- `/etc/sudoers` - Modified during blocking periods

Development files:
- `glocker.go` - Single source file with all logic
- `conf/conf.yaml` - Sample configuration (embedded during install)
- `extras/glocker.service` - Systemd service definition


### Security Features

**Self-Healing** (when enabled):
- Monitors critical file checksums (binary, /etc/hosts, systemd service)
- Re-applies protections if tampering detected
- Triggers alarm command on tampering

**Tamper Resistance**:
- Setuid root binary with immutable flag (`chattr +i`)
- Immutable /etc/hosts during blocking
- Sudoers restrictions prevent privilege escalation
- Mindful delay (configurable, default 60s) before uninstall completes

**Accountability**:
- Sends email notifications via Mailgun when blocks are bypassed
- Logs all unblock attempts with reasons
- Daily violation reports (configurable)

### Time Window Logic

Time windows use HH:MM format and day-of-week arrays:
```yaml
time_windows:
  - start: "09:00"
    end: "17:00"
    days: ["Mon", "Tue", "Wed", "Thu", "Fri"]
```

Applied to:
- Domain blocking (glocker.go:Domain.TimeWindows)
- Sudoers restrictions (glocker.go:SudoersConfig.TimeAllowed)
- Forbidden programs (glocker.go:ForbiddenProgram.TimeWindows)

### HTTP(s) Servers (port 80, 443)

Endpoints for browser extension communication:
- `POST /report` - Content monitoring reports from extension
- `GET /keywords` - Returns current URL/content keyword lists
- `GET /sse` - Server-sent events for real-time updates
- `GET /blocked` - Blocked page display (shown when firewall blocks request)

### Extension Communication Flow

1. Extension loads and fetches keywords: `GET http://127.0.0.1/keywords`
2. Extension monitors URLs/content using cached keyword regexes
3. On violation: `POST http://127.0.0.1/report` with JSON payload
4. Glocker logs to file and increments violation counter
5. If violations exceed threshold: executes lock command

## Configuration Notes

The `conf/conf.yaml` file contains extensive blocking lists and is ~60MB due to comprehensive domain lists. When making changes:
- Use `glocker -reload` to reload config without restart
- Check logs with `journalctl -u glocker.service -f`

Violation tracking can trigger automatic actions (e.g., screen lock):
```yaml
violation_tracking:
  enabled: true
  max_violations: 5
  time_window_minutes: 60
  command: "sudo -u noufal DISPLAY=:1 i3lock"
```

## Code Organization

Despite being a single file, `glocker.go` is organized into functional sections:
- Lines 1-190: Imports, constants, config structs
- Lines 191-400: Main function and command parsing
- Lines 400-710: Socket handlers and IPC commands
- Lines 710-1020: Core enforcement logic (hosts, firewall, sudoers)
- Lines 1020-2100: Installation, uninstallation, signal handling
- Lines 2100-2400: Tamper detection and monitoring
- Lines 2400-3000: Status display and reload logic
- Lines 3000-3500: HTTP server for web tracking and extension
- Lines 3500-4400: Forbidden programs, violation tracking, utilities

When modifying enforcement logic, search for the specific mechanism by name (e.g., `grep -n "enforceHosts"` or `grep -n "updateFirewall"`).

## Dependencies

Go modules (go.mod):
- `github.com/mailgun/mailgun-go/v4` - Email notifications
- `gopkg.in/yaml.v3` - Configuration parsing

System dependencies:
- `iptables` / `ip6tables` - Firewall rules
- `systemd` - Service management
- `chattr` / `lsattr` - File immutability
- Firefox with extension support
