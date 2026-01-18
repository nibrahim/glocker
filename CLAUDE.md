# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

Glocker is a Linux-based distraction blocking system written in Go. It blocks distracting websites and applications using multiple enforcement mechanisms to make bypassing difficult. The tool runs as a privileged setuid systemd service and uses Unix sockets for IPC.

**Architecture:** Modular Go application with packages in `internal/` directory (see File Map below)

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

## File Map - Where to Find What

### Entry Point
- **`main.go`** (377 lines) - CLI entry point
  - All 12 command-line flag definitions (lines 26-37)
  - Flag handlers with socket communication (lines 48-234)
  - Default behavior: show status or help (lines 236-266)
  - Daemon startup logic (lines 322-376)

### Configuration (`internal/config/`)
- **`config.go`** - Configuration structs and YAML loading
  - `Config` struct with all settings
  - `Domain`, `TimeWindow`, `SudoersConfig` structs
  - `LoadConfig()` - Loads from `/etc/glocker/config.yaml`
  - `SetupLogging()` - Configures log output

### CLI Commands (`internal/cli/`)
- **`commands.go`** - Command processors for socket requests
  - `GetStatusResponse()` - Generates formatted status output (lines 15-145)
  - `ProcessReloadRequest()` - Handles config reload (lines 148-173)
  - `ProcessUnblockRequest()` - Temporary domain unblocking (lines 76-100)
  - `ProcessBlockRequest()` - Permanent domain blocking (lines 103-124)
  - `ProcessPanicRequest()` - Panic mode activation (lines 127-140)
  - `formatTimeWindows()` - Helper for time window display
- **`commands_test.go`** - Unit tests for CLI commands

### Enforcement (`internal/enforcement/`)
- **`enforcement.go`** - Core blocking logic
  - `RunOnce()` - Main enforcement cycle
  - `UpdateHosts()` - Modifies `/etc/hosts` file
  - `UpdateFirewall()` - Manages iptables rules
  - `UpdateSudoers()` - Controls sudo access
  - Time window evaluation logic
  - Immutable file protection (chattr)

### IPC / Socket Communication (`internal/ipc/`)
- **`server.go`** - Unix socket server for daemon communication
  - `SetupCommunication()` - Creates socket at `/tmp/glocker.sock`
  - `HandleConnection()` - Processes socket commands (lines 33-146)
  - Socket command handlers:
    - `status` - Live status query (lines 75-77)
    - `reload` - Config reload (lines 78-80)
    - `unblock` - Temporary unblock (lines 81-99)
    - `block` - Permanent block (lines 100-107)
    - `panic` - Panic mode (lines 111-123)
    - `lock` - Force sudoers lock (lines 124-126)
    - `add-keyword` - Add monitoring keywords (lines 127-134)
    - `uninstall` - Uninstall glocker (lines 135-146)
  - `processLockRequest()` - Lock processor (lines 189-200)
  - `processAddKeywordRequest()` - Keyword processor (lines 202-221)
  - `processUninstallRequest()` - Uninstall processor (lines 223-244)
- **`server_test.go`** - Socket server tests

### Installation (`internal/install/`)
- **`install.go`** - Installation and uninstallation logic
  - `InstallGlocker()` - Copies binary, config, systemd service
  - `RestoreSystemChanges()` - Reverts all system modifications
  - `RunningAsRoot()` - Privilege checks
  - Setuid permission setup
  - File immutability management

### Monitoring (`internal/monitoring/`)
- **`forbidden.go`** - Forbidden program monitoring
  - `MonitorForbiddenPrograms()` - Background goroutine
  - Process killing during time windows
  - Program detection via process name
- **`violations.go`** - Violation tracking
  - `MonitorViolations()` - Counts access attempts
  - Threshold-based action triggering
- **`tampering.go`** - Tamper detection
  - File checksum monitoring
  - Self-healing mechanisms
  - Alarm command execution
- **`panic.go`** - Panic mode monitoring
  - System suspend/resume detection
  - Re-suspension on early wake

### Web Server (`internal/web/`)
- **`server.go`** - HTTP/HTTPS server for browser extension
  - `StartWebTrackingServer()` - Starts on ports 80, 443
- **`handlers.go`** - HTTP endpoint handlers
  - `GET /keywords` - Returns monitoring keywords
  - `POST /report` - Content violation reports
  - `GET /sse` - Server-sent events for real-time updates
  - `GET /blocked` - Blocked page display

### Notifications (`internal/notify/`)
- **`notify.go`** - Email notifications
  - Mailgun integration
  - Accountability notifications
  - Violation reports

### State Management (`internal/state/`)
- **`state.go`** - Thread-safe global state
  - Panic mode state (`panicUntil`)
  - Temporary unblocks tracking
  - `sync.RWMutex` for concurrency safety

### Utilities (`internal/utils/`)
- **`utils.go`** - Shared utility functions
  - Time window evaluation
  - String processing helpers
  - File operations

### Browser Extension (`extensions/firefox/`)
- **`manifest.json`** - Firefox extension manifest
- **`background.js`** - URL monitoring, keyword fetching
- **`content.js`** - Page content scanning
- **`blocked.html`** - Blocked page template

### Configuration & Resources
- **`conf/conf.yaml`** - Sample configuration (~60MB with domain lists)
- **`extras/glocker.service`** - Systemd service definition

### Documentation
- **`CLAUDE.md`** - This file - developer guidance
- **`GAP_ANALYSIS.md`** - Detailed gap analysis from refactoring
- **`IMPLEMENTATION_STATUS.md`** - Implementation completion report

## Quick Reference: Common Tasks

| Task | Primary File | Key Function |
|------|-------------|--------------|
| Add new CLI flag | `main.go` | Add flag definition + handler |
| Modify status output | `internal/cli/commands.go` | `GetStatusResponse()` |
| Change blocking logic | `internal/enforcement/enforcement.go` | `UpdateHosts()`, `UpdateFirewall()` |
| Add socket command | `internal/ipc/server.go` | Add case in `HandleConnection()` |
| Modify installation | `internal/install/install.go` | `InstallGlocker()` |
| Add web endpoint | `internal/web/handlers.go` | Add handler function |
| Change time windows | `internal/config/config.go` | `TimeWindow` struct |
| Add monitoring feature | `internal/monitoring/` | Create new file or extend existing |

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
### To test the program, 
#### Install new version
1. `sudo glocker -uninstall "testing"`
2. `go build .`
3. `sudo ./glocker -install`
4. Run `glocker` to get status.

#### Things to verify
1. Killing of forbidden programs
2. Violation trackin
3. Making sure that blocked domains are in `/etc/hosts` properly
4. APIs for browser extension integration (confirm with curl)


#### Testing Browser Extension
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

All configuration is in YAML with these main structs (internal/config/config.go):

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
    AlwaysBlock bool         // Deprecated: use TimeWindows instead
    TimeWindows []TimeWindow // If empty, domain is always blocked (default)
    Absolute    bool         // Cannot be temporarily unblocked
}

// Domain blocking logic (internal/enforcement/domains.go):
// - No time windows → Always blocked (default behavior)
// - Time windows specified → Blocked only during those windows
// - Absolute flag → Cannot be temporarily unblocked
```

### Control Flow

**Installation** (`glocker -install`):
1. Copies binary to `/usr/local/bin/glocker` with setuid permissions (internal/install/install.go)
2. Copies `conf/conf.yaml` to `/etc/glocker/config.yaml`
3. Installs systemd service from `extras/glocker.service`

**Enforcement Loop** (main.go:322-376):
1. Loads config from `/etc/glocker/config.yaml` (internal/config/config.go)
2. Starts Unix socket server for IPC (internal/ipc/server.go)
3. Launches monitoring goroutines:
   - Tamper detection (internal/monitoring/tampering.go)
   - Forbidden programs (internal/monitoring/forbidden.go)
   - Violation tracking (internal/monitoring/violations.go)
   - Panic mode monitoring (internal/monitoring/panic.go)
   - Web tracking HTTP server on ports 80 and 443 (internal/web/server.go)
4. Main loop applies enforcement every 60s:
   - Updates hosts file with blocked domains (internal/enforcement/enforcement.go)
   - Updates firewall rules
   - Updates sudoers restrictions
   - Applies time window logic

**Socket Commands** (internal/ipc/server.go:33-146):
- Client sends command via Unix socket at `/tmp/glocker.sock`
- Format: `"action:payload\n"` (e.g., `"block:example.com\n"`)
- Server processes command and returns response
- Commands: `status`, `reload`, `unblock`, `block`, `panic`, `lock`, `add-keyword`, `uninstall`
- Multi-line responses end with `"END"`

### Important Files and Paths

**Production paths:**
- `/etc/glocker/config.yaml` - Main configuration
- `/tmp/glocker.sock` - Unix socket for IPC
- `/var/log/glocker-reports.log` - Content monitoring logs
- `/var/log/glocker-unblocks.log` - Unblock request logs
- `/etc/hosts` - Modified with GLOCKER markers and immutable flag
- `/etc/sudoers` - Modified during blocking periods
- `/usr/local/bin/glocker` - Installed binary (setuid root)
- `/etc/systemd/system/glocker.service` - Systemd service file

**Development files:**
- `main.go` - Entry point
- `internal/` - Application packages (see File Map above)
- `conf/conf.yaml` - Sample configuration (~60MB with domain lists)
- `extras/glocker.service` - Systemd service template
- `extensions/firefox/` - Browser extension


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
- Domain blocking (internal/config/config.go: `Domain.TimeWindows`)
- Sudoers restrictions (internal/config/config.go: `SudoersConfig.TimeAllowed`)
- Forbidden programs (internal/config/config.go: `ForbiddenProgram.TimeWindows`)
- Evaluation logic in internal/enforcement/enforcement.go

### HTTP(s) Servers (port 80, 443)

Endpoints for browser extension communication (internal/web/handlers.go):
- `POST /report` - Content monitoring reports from extension
- `GET /keywords` - Returns current URL/content keyword lists
- `GET /sse` - Server-sent events for real-time updates
- `GET /blocked` - Blocked page display (shown when firewall blocks request)

Server started by internal/web/server.go:StartWebTrackingServer()

### Extension Communication Flow

1. Extension loads and fetches keywords: `GET http://127.0.0.1/keywords`
2. Extension monitors URLs/content using cached keyword regexes (extensions/firefox/background.js, content.js)
3. On violation: `POST http://127.0.0.1/report` with JSON payload
4. Glocker logs to file and increments violation counter (internal/web/handlers.go)
5. If violations exceed threshold: executes lock command (internal/monitoring/violations.go)

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

The codebase is organized into modular packages under `internal/`:

```
glocker/
├── main.go                      # Entry point, CLI flags, daemon startup
├── internal/                    # Application packages
│   ├── cli/                    # Command processors
│   ├── config/                 # Configuration structs and loading
│   ├── enforcement/            # Core blocking logic
│   ├── install/                # Installation and uninstallation
│   ├── ipc/                    # Unix socket server
│   ├── monitoring/             # Background monitoring tasks
│   ├── notify/                 # Email notifications
│   ├── state/                  # Thread-safe global state
│   ├── utils/                  # Shared utilities
│   └── web/                    # HTTP server for extension
├── extensions/firefox/          # Browser extension
├── conf/conf.yaml              # Sample configuration
└── extras/glocker.service      # Systemd service
```

**Package Dependencies:**
- `main` imports all packages for orchestration
- `cli` imports `config`, `enforcement`, `state`
- `ipc` imports `cli`, `config`, `enforcement`, `install`
- `enforcement` imports `config`, `state`, `utils`
- `monitoring` imports `config`, `notify`, `state`
- `web` imports `config`, `monitoring`, `state`

When searching for functionality:
- CLI flags: `main.go`
- Enforcement logic: `internal/enforcement/enforcement.go`
- Socket commands: `internal/ipc/server.go`
- Status output: `internal/cli/commands.go`

## Dependencies

Go modules (go.mod):
- `github.com/mailgun/mailgun-go/v4` - Email notifications
- `gopkg.in/yaml.v3` - Configuration parsing

System dependencies:
- `iptables` / `ip6tables` - Firewall rules
- `systemd` - Service management
- `chattr` / `lsattr` - File immutability
- Firefox with extension support
