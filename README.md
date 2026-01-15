# Glocker

A Linux-based distraction blocking system that uses multiple enforcement mechanisms to help you stay focused.

## Why Glocker?

I needed an application that would block distracting sites and apps, but existing solutions didn't work for me:
- **Plucky** wasn't transparent about its strategies and didn't support Firefox
- **Accountable2You** couldn't get it working on Linux

Given the control that Linux offers for `root`, it's hard to make something that *really* blocks everything. However, it is possible to make it very tedious to break out. That's what this application does.

I've often found that there are liminal moments where I make the wrong decision in a fog of distraction. Having someone, or if not possible, something that makes it hard to make the wrong decision, lets me get back to work.

That's what Glocker tries to do.

## Quick Start

```bash
# Build all binaries
make build-all

# Install as systemd service (requires sudo)
sudo ./glocker -install

# Check status
glocker -status

# View configuration info
glocker -info

# Uninstall
sudo glocker -uninstall "reason for uninstalling"
```

## Command Line Usage

### Status and Information

```bash
# Show runtime status (violations, temp unblocks, panic mode)
glocker -status

# Show configuration info (blocked domains, time windows, forbidden programs)
glocker -info

# Show version information
glocker -version
```

### Domain Management

```bash
# Temporarily unblock domains (20 minutes by default)
glocker -unblock "youtube.com,reddit.com:work research"

# Permanently block additional domains
glocker -block "facebook.com,instagram.com"

# Add keywords to monitoring lists (URL and content)
glocker -add-keyword "gambling,casino,poker"
```

### Control Commands

```bash
# Reload configuration from disk
glocker -reload

# Immediately lock sudo access (ignores time windows)
glocker -lock

# Enter panic mode - suspend system for N minutes
# System re-suspends if woken early (requires accountability partner to disable)
glocker -panic 30
```

### Installation

```bash
# Install as systemd service with setuid privileges
sudo ./glocker -install

# Uninstall and revert all system changes
sudo glocker -uninstall "testing new features"
```

All commands communicate with the running daemon via Unix socket (`/tmp/glocker.sock`). The `-daemon` flag is used internally by systemd and shouldn't be invoked manually.

---

## Utility Tools

### glockpeek - Log Analysis

A command-line tool for analyzing Glocker's violation and unblock logs with visual summaries.

**Default Summary View**

```bash
# Show all summaries (violations and unblocks)
glockpeek

# Show only violations or unblocks
glockpeek -violations
glockpeek -unblocks

# Show top 10 items instead of default 5
glockpeek -top 10
```

**Date Filtering**

```bash
# Filter by year, month, or date range
glockpeek -from 2024
glockpeek -from 2024-06
glockpeek -from 2024-06-15 -to 2024-06-30
```

**Detailed Views**

```bash
# Timeline for a specific day (hour-by-hour breakdown)
glockpeek -day 2024-06-15

# Daily aggregates for a month (calendar view)
glockpeek -month 2024-06
```

The output includes:
- Colored bar charts (red for above average, green for below)
- Inverse video highlighting for egregious periods
- Top offenders by frequency
- Time-of-day patterns

### glocklock - Screen Locker

A standalone X11 screen locker with two modes, designed for mindful breaks.
It will read the `violation_tracking` section from the config file and work
accordingly. However, the settings can be overridden by command line flags 
listed below.

**Time-based Mode** - Automatically unlocks after a timeout:

```bash
# Lock using duration from config (default: 1 minute)
glocklock

# Lock for 5 minutes with custom message
glocklock -duration 5m -message "Break time"

# Use custom config file
glocklock -conf /path/to/config.yaml
```

**Text-based Mode** - Requires typing specific text to unlock:

```bash
# Lock until mindful_text from config is typed correctly
glocklock -mindful

# Lock until text from file is typed correctly
glocklock -text /path/to/message.txt
```

The text-based mode displays the target text and shows typed characters in green (correct) or red (incorrect). Press Enter when text matches to unlock, or Escape to clear and start over.

**Configuration** (in `/etc/glocker/config.yaml`):

```yaml
violation_tracking:
  lock_duration: "5m"  # Duration: "30s", "5m", or plain number (seconds)
  mindful_text: "I will focus on my work and avoid distractions."
  background: "/path/to/image.png"  # Optional PNG/JPG background
```

---

## Monitoring Systems

Glocker uses multiple independent monitors that work together to enforce blocking:

### 1. Hosts File Blocking

**What it does:** Modifies `/etc/hosts` to redirect blocked domains to `127.0.0.1`

**How it works:**
- Writes blocked domains below `### GLOCKER START ###`.
- Sets file immutable using `chattr +i` (when enabled)
- Updates every 60 seconds (configurable via `enforce_interval_seconds`)
- Handles 800,000+ domains efficiently (memory optimization clears list after initial write)

**Time windows:** Can block domains only during specific times/days

```yaml
domains:
  - name: "twitter.com"
    always_block: false
    time_windows:
      - start: "09:00"
        end: "17:00"
        days: ["Mon", "Tue", "Wed", "Thu", "Fri"]
```

### 2. Firewall Blocking (Optional)

**What it does:** Uses `iptables` to block domains at network level

**How it works:**
- Resolves domains to IPs and adds DROP rules
- More aggressive than hosts file (can't be bypassed by direct IP access)
- Disabled by default due to complexity
- Enable with `enable_firewall: true`

### 3. Web Tracking & Violation Recording

**What it does:** Catches attempts to access blocked sites via HTTP/HTTPS

**How it works:**
- Runs HTTP server on port 80 and HTTPS on port 443
- When browser tries to access blocked domain (redirected by hosts file), server intercepts
- Records violation to database
- Executes configured command (e.g., play alert sound)
- Sends accountability email if enabled
- Shows blocking reason (always blocked, time-based, etc.)

**Configuration:**

```yaml
web_tracking:
  enabled: true
  command: "mpg123 /path/to/alert.mp3"  # Optional: command to run on block
```

### 4. Browser Extension Integration

**What it does:** Content monitoring for keywords and URL patterns in browsers

**How it works:**
- Firefox extension (in `extensions/firefox/`) monitors page URLs and content
- Fetches keyword lists from `http://127.0.0.1/keywords` API
- Scans page content for forbidden keywords
- Reports violations to `http://127.0.0.1/report` API
- Works with glocker's violation tracking system

**Configuration:**

```yaml
extension_keywords:
  url_keywords: ["gambling", "casino", "poker"]
  content_keywords: ["bet", "jackpot"]
  whitelist:
    - "stackoverflow.com"
    - "github.com"
```

### 5. Forbidden Programs Monitor

**What it does:** Kills specified programs during configured time windows

**How it works:**
- Checks every 5 seconds (configurable) for forbidden process names
- Kills processes using `killall` when found during active time window
- Useful for blocking browsers, chat apps, games during work hours

**Configuration:**

```yaml
forbidden_programs:
  enabled: true
  check_interval_seconds: 5
  programs:
    - name: "chromium"
      time_windows:
        - start: "20:00"
          end: "05:00"
          days: ["Mon", "Tue", "Wed", "Thu", "Fri"]
    - name: "mullvadbrowser"  # Always blocked (no time windows)
```

### 6. Violation Tracking & Threshold Actions

**What it does:** Counts violations and triggers action when threshold exceeded

**How it works:**
- Counts violations (web access attempts, content keyword matches) in time window
- Executes command when threshold reached (e.g., lock screen)
- Resets counter after time window expires
- Can be used to escalate enforcement

**Configuration:**

```yaml
violation_tracking:
  enabled: true
  max_violations: 5
  time_window_minutes: 60
  command: "sudo -u noufal DISPLAY=:1 glocklock -duration 5m"
  lock_duration: "5m"  # For glocklock
```

### 7. Sudoers Control

**What it does:** Restricts `sudo` access during blocking periods

**How it works:**
- Swaps between "allowed" and "blocked" sudoers lines based on time windows
- Prevents user from running `sudo` to bypass protections
- Can whitelist specific commands (e.g., suspend, package management)

**Configuration:**

```yaml
sudoers:
  enabled: true
  user: "noufal"
  allowed_sudoers_line: "noufal ALL=(ALL) NOPASSWD:ALL"
  blocked_sudoers_line: "noufal ALL=(ALL) NOPASSWD: /usr/bin/apt, /sbin/modprobe"
  time_allowed:
    - start: "10:00"
      end: "16:00"
      days: ["Mon", "Tue", "Wed", "Thu", "Fri"]
```

### 8. Tamper Detection & Self-Healing

**What it does:** Monitors critical files for modifications and restores them

**How it works:**
- Calculates checksums of `/etc/hosts`, glocker binary, systemd service
- Checks every 30 seconds (configurable)
- Re-applies protections if tampering detected
- Executes alarm command (e.g., play sound, send notification)

**Configuration:**

```yaml
enable_self_healing: true
tamper_detection:
  enabled: true
  check_interval_seconds: 30
  alarm_command: "mpg123 /path/to/alarm.mp3"
```

### 9. Panic Mode

**What it does:** Emergency enforcement via system suspension

**How it works:**
- Suspends system using `pm-suspend` or `rtcwake`
- Monitors for early wake (via logind D-Bus signals)
- Re-suspends system if woken before timer expires
- Requires accountability partner to disable panic mode
- Nuclear option for moments of extreme distraction

**Usage:**

```bash
# Suspend for 30 minutes (re-suspends on early wake)
glocker -panic 30
```

**Configuration:**

```yaml
panic_command: "sudo pm-suspend"
```

### How Monitors Work Together

```
                     User attempts to access blocked site
                                    |
                                    v
     ┌──────────────────────────────────────────────────────┐
     |              DNS/Hosts Resolution                    |
     |  /etc/hosts redirects blocked.com → 127.0.0.1        |
     └──────────────────────────────────────────────────────┘
                                    |
                                    v
     ┌──────────────────────────────────────────────────────┐
     |         HTTP Request to 127.0.0.1:80/443             |
     |  Web tracking server intercepts request              |
     └──────────────────────────────────────────────────────┘
                                    |
                                    v
     ┌──────────────────────────────────────────────────────┐
     |              Violation Recording                     |
     |  • Increment violation counter                       |
     |  • Log to database with timestamp                    |
     |  • Execute web_tracking.command (alert sound)        |
     |  • Send to accountability partner (if enabled)       |
     └──────────────────────────────────────────────────────┘
                                    |
                                    v
     ┌──────────────────────────────────────────────────────┐
     |           Check Violation Threshold                  |
     |  If count >= max_violations in time_window:          |
     |    Execute violation_tracking.command (lock screen)  |
     └──────────────────────────────────────────────────────┘
                                    |
                                    v
     ┌──────────────────────────────────────────────────────┐
     |              Show Blocking Page                      |
     |  Redirect to: http://127.0.0.1/blocked?domain=...    |
     |  Display blocking reason and time remaining          |
     └──────────────────────────────────────────────────────┘

     Meanwhile, in parallel:

     • Browser Extension monitors page content for keywords
     • Forbidden Programs monitor kills blocked apps
     • Sudoers control restricts sudo during work hours
     • Tamper detection ensures protections stay active
```

---

## Configuration File

Glocker reads configuration from `/etc/glocker/config.yaml` (sample in `conf/conf.yaml`).

### Core Settings

```yaml
# Development mode - bypasses delays for testing
dev: false

# Log level: debug, info, warn, error
log_level: "info"

# Enable/disable each enforcement mechanism
enable_hosts: true
enable_firewall: false
enable_forbidden_programs: true
enable_self_healing: false

# Enforcement loop interval (seconds)
enforce_interval_seconds: 60

# Paths (leave empty for defaults)
hosts_path: "/etc/hosts"
```

### Blocked Domains

Domains can be always blocked or time-based:

```yaml
domains:
  # Always blocked (cannot be temporarily unblocked)
  - {name: "facebook.com", always_block: true, absolute: true}

  # Always blocked (can be temporarily unblocked)
  - {name: "reddit.com", always_block: true}

  # Time-based blocking
  - name: "twitter.com"
    always_block: false
    time_windows:
      - start: "09:00"
        end: "17:00"
        days: ["Mon", "Tue", "Wed", "Thu", "Fri"]
      - start: "11:00"
        end: "15:00"
        days: ["Sat", "Sun"]
```

**Domain types:**
- `always_block: true` - Blocked 24/7
- `absolute: true` - Cannot be temporarily unblocked (for high-risk sites)
- `time_windows` - Blocked only during specified times/days
- Time format: 24-hour `HH:MM`, supports midnight-crossing (e.g., `22:00` to `05:00`)

### Updating Domain Blocklists

The `update_domains.py` script automates updating domain lists from curated blocklists. It supports multiple sources with automatic timestamp checking for idempotent updates.

**Available sources:**
1. **Bon Appetit Porn Domains** - Comprehensive adult content blocklist (~800K domains)
2. **StevenBlack Unified Hosts** - Ads and malware domains
3. **HaGeZi DoH/VPN/TOR/Proxy Bypass** - Blocks encrypted DNS, VPN, TOR, proxy bypass methods
4. **UnblockStop Proxy Bypass** - Blocks proxy and filter-bypass sites (CroxyProxy, etc.)

**Usage:**

```bash
# List all available sources and their status
./update_domains.py

# Update from a specific source
./update_domains.py 1

# Update from all sources
./update_domains.py all

# Remove all managed domain lists (keeps manual domains)
./update_domains.py strip
```

**Features:**
- **Idempotent updates** - Only updates if source timestamp has changed
- **Automatic deduplication** - Removes duplicate domains and `www.` prefixes
- **Source markers** - Each source is marked in the config file for easy identification
- **Preserves manual domains** - Only modifies managed source sections

After updating domains, reload the configuration:
```bash
glocker -reload
```

### Temporary Unblocking

```yaml
unblocking:
  reasons: ["work", "research", "emergency", "education"]
  log_file: "/var/log/glocker-unblocks.log"
  temp_unblock_time: 20  # Minutes
```

Usage: `glocker -unblock "youtube.com:work research"`

### Web Tracking

```yaml
web_tracking:
  enabled: true
  command: "mpg123 /path/to/alert.mp3"
```

### Content Monitoring

```yaml
content_monitoring:
  enabled: true
  log_file: "/var/log/glocker-reports.log"

extension_keywords:
  url_keywords: ["gambling", "casino"]
  content_keywords: ["bet", "jackpot"]
  whitelist:
    - "stackoverflow.com"
    - "github.com"
```

### Forbidden Programs

```yaml
forbidden_programs:
  enabled: true
  check_interval_seconds: 5
  programs:
    - name: "chromium"
      time_windows:
        - start: "20:00"
          end: "05:00"
          days: ["Mon", "Tue", "Wed", "Thu", "Fri"]
    - name: "steam"  # Always killed (no time windows)
```

### Sudoers Control

```yaml
sudoers:
  enabled: true
  user: "noufal"
  allowed_sudoers_line: "noufal ALL=(ALL) NOPASSWD:ALL"
  blocked_sudoers_line: "noufal ALL=(ALL) NOPASSWD: /usr/bin/apt"
  time_allowed:
    - start: "10:00"
      end: "16:00"
      days: ["Mon", "Tue", "Wed", "Thu", "Fri"]
```

### Violation Tracking

```yaml
violation_tracking:
  enabled: true
  max_violations: 5
  time_window_minutes: 60
  command: "glocklock -duration 5m"
  lock_duration: "5m"  # For glocklock
  mindful_text: "I will focus on my work."  # For glocklock -mindful
  background: "/path/to/image.png"  # For glocklock
```

### Tamper Detection

```yaml
enable_self_healing: true
tamper_detection:
  enabled: true
  check_interval_seconds: 30
  alarm_command: "notify-send -u critical 'Glocker' 'Tampering detected!'"
```

### Accountability

```yaml
accountability:
  enabled: true
  partner_email: "friend@example.com"
  from_email: "me@example.com"
  api_key: "your-mailgun-api-key"
```

Sends notifications to accountability partner when:
- Blocked sites are accessed
- Domains are temporarily unblocked
- Violations exceed threshold
- Panic mode is activated/deactivated
- Glocker is uninstalled

### Panic Mode

```yaml
panic_command: "sudo pm-suspend"
```

### Mindful Delay

```yaml
mindful_delay: 60  # Seconds to wait before uninstall completes
```

Prevents impulsive uninstallation by requiring a 60-second wait.

---

## Architecture

### System Design

Glocker is a **Go application** organized into modular packages under `internal/`:

```
glocker/
├── main.go                     # Entry point, CLI flags, daemon startup
├── cmd/                        # Additional binaries
│   ├── glocker/                # Main daemon
│   ├── glocklock/              # Screen locker utility
│   └── glockpeek/              # Log analysis tool
├── internal/                   # Application packages
│   ├── cli/                    # Command processors
│   ├── config/                 # Configuration loading (YAML)
│   ├── enforcement/            # Core blocking logic
│   ├── install/                # Installation/uninstallation
│   ├── ipc/                    # Unix socket server
│   ├── monitoring/             # Background monitoring tasks
│   ├── notify/                 # Email notifications
│   ├── state/                  # Thread-safe global state
│   ├── utils/                  # Shared utilities
│   └── web/                    # HTTP server for extension
├── extensions/firefox/         # Browser extension
├── conf/conf.yaml              # Sample configuration (~60MB)
└── extras/glocker.service      # Systemd service definition
```

### Execution Model

**Daemon Mode** (normal operation):

```
systemd launches glocker
         |
         v
  ┌──────────────────┐
  │  main.go         │
  │  -daemon flag    │
  └──────────────────┘
         |
         v
  ┌──────────────────────────────────────┐
  │  Load config from YAML               │
  │  (/etc/glocker/config.yaml)          │
  └──────────────────────────────────────┘
         |
         v
  ┌──────────────────────────────────────┐
  │  Start Unix socket server            │
  │  (/tmp/glocker.sock)                 │
  └──────────────────────────────────────┘
         |
         v
  ┌──────────────────────────────────────┐
  │  Launch background goroutines:       │
  │  • Tamper detection monitor          │
  │  • Forbidden programs monitor        │
  │  • Violation tracking monitor        │
  │  • Panic mode monitor                │
  │  • Web tracking HTTP/HTTPS servers   │
  └──────────────────────────────────────┘
         |
         v
  ┌──────────────────────────────────────┐
  │  Main enforcement loop (60s cycle):  │
  │  • Update /etc/hosts file            │
  │  • Update firewall rules             │
  │  • Update sudoers restrictions       │
  │  • Evaluate time windows             │
  │  • Apply file immutability           │
  └──────────────────────────────────────┘
```

**Client Mode** (CLI commands):

```
User runs: glocker -status
         |
         v
  ┌──────────────────────────────────────┐
  │  Connect to /tmp/glocker.sock        │
  └──────────────────────────────────────┘
         |
         v
  ┌──────────────────────────────────────┐
  │  Send command: "status"              │
  └──────────────────────────────────────┘
         |
         v
  ┌──────────────────────────────────────┐
  │  Daemon processes request            │
  │  (internal/ipc/server.go)            │
  └──────────────────────────────────────┘
         |
         v
  ┌──────────────────────────────────────┐
  │  Generate response                   │
  │  (internal/cli/commands.go)          │
  └──────────────────────────────────────┘
         |
         v
  ┌──────────────────────────────────────┐
  │  Send response back to client        │
  └──────────────────────────────────────┘
         |
         v
  ┌──────────────────────────────────────┐
  │  Client displays formatted output    │
  └──────────────────────────────────────┘
```

### IPC Protocol

Communication between CLI and daemon uses Unix socket with simple text protocol:

**Format:** `"action:payload\n"`

**Examples:**
- `status\n` - Request runtime status
- `reload\n` - Reload configuration
- `unblock:youtube.com,reddit.com:work\n` - Temporarily unblock domains
- `block:facebook.com\n` - Permanently block domain
- `panic:30\n` - Enter panic mode for 30 minutes

**Responses:** Multi-line text ending with `"END\n"`

### Key Technical Details

**1. Setuid Binary**

Glocker is installed as a setuid root binary:
- Allows non-root users to run commands via socket
- Daemon runs with elevated privileges to modify `/etc/hosts`, iptables, sudoers
- Binary made immutable with `chattr +i` to prevent tampering

**2. Memory Optimization**

To handle 800,000+ domains:
- Full domain list loaded during initial enforcement
- List written to `/etc/hosts`
- **Domain list cleared from memory** after initial write to save RAM
- Only time-window domains (typically <10) kept cached
- On config reload, domains are loaded from disk temporarily

**3. Lazy-Loaded Cache for Web Tracking**

Web tracking needs domain info but domains are cleared from memory:
- **First violation:** Loads domain from config file (~3-4s)
- **Subsequent violations:** Uses cached domain info (~0.2s, 15-20x faster)
- Cache cleared on config reload
- Only accessed domains are cached (memory efficient)

**4. Time Window Evaluation**

```go
// Ported from internal/utils/time.go
func IsInTimeWindow(current, start, end string) bool {
    if start <= end {
        // Normal window: 09:00 to 17:00
        return current >= start && current <= end
    } else {
        // Midnight-crossing: 22:00 to 05:00
        return current >= start || current <= end
    }
}
```

**5. Immutable File Protection**

```bash
# Make /etc/hosts immutable (requires root)
chattr +i /etc/hosts

# Remove immutability to update
chattr -i /etc/hosts

# Check immutable status
lsattr /etc/hosts
```

**6. Hosts File Format**

```
# Standard entries
127.0.0.1 localhost
::1 localhost

### GLOCKER START ###
127.0.0.1 blocked-domain1.com
127.0.0.1 www.blocked-domain1.com
::1 blocked-domain1.com
::1 www.blocked-domain1.com
127.0.0.1 blocked-domain2.com
...

# User's custom entries below (preserved)
```

**7. Browser Extension Communication**

```
Firefox Extension (background.js)
         |
         v
  GET http://127.0.0.1/keywords
         |
         v
  {"url_keywords": [...], "content_keywords": [...]}
         |
         v
  Extension monitors page URLs and content
         |
         v
  POST http://127.0.0.1/report
  {"trigger": "url_keyword_match", "url": "...", "domain": "..."}
         |
         v
  Glocker records violation
```

**8. Violation Tracking Flow**

```go
// internal/monitoring/violations.go
func RecordViolation(cfg *config.Config, violationType, domain, url string) {
    // 1. Add to in-memory list
    violations = append(violations, Violation{
        Type: violationType,
        Domain: domain,
        URL: url,
        Timestamp: time.Now(),
    })

    // 2. Log to file
    logViolation(cfg, violation)

    // 3. Check threshold
    recentCount := countRecentViolations(cfg.ViolationTracking.TimeWindowMinutes)
    if recentCount >= cfg.ViolationTracking.MaxViolations {
        // 4. Execute command (e.g., lock screen)
        executeViolationCommand(cfg)
    }
}
```

### Dependencies

**Go Modules:**
- `github.com/mailgun/mailgun-go/v4` - Email notifications via Mailgun
- `gopkg.in/yaml.v3` - YAML configuration parsing

**System Requirements:**
- `iptables` / `ip6tables` - Firewall enforcement (optional)
- `systemd` - Service management
- `chattr` / `lsattr` - File immutability
- Firefox with extension support (for content monitoring)

**Runtime Paths:**
- `/etc/glocker/config.yaml` - Main configuration
- `/tmp/glocker.sock` - Unix socket for IPC
- `/var/log/glocker-reports.log` - Content monitoring logs
- `/var/log/glocker-unblocks.log` - Unblock request logs
- `/usr/local/bin/glocker` - Installed binary (setuid root)
- `/etc/systemd/system/glocker.service` - Systemd service file

---

## Development

### Building

```bash
# Build all binaries (glocker, glocklock, glockpeek)
make build-all

# Build single binary
go build -o glocker ./cmd/glocker
```

### Testing

```bash
# Run all tests
go test ./...

# Run specific package tests
go test ./internal/web -v
go test ./internal/enforcement -v
```

### Installing for Development

```bash
# Uninstall current version
sudo glocker -uninstall "testing"

# Build new version
make build-all

# Install new version
sudo ./glocker -install

# Check status
glocker -status
```

### Test Enforcement Cycle

```bash
# Run a single enforcement cycle without installing
sudo glocker -once
```

---

## Android Port

See [ANDROID.md](ANDROID.md) for detailed architecture documentation for an Android version using AccessibilityService, VpnService, and Device Admin APIs.

---

## License

See LICENSE file for details.

## Contributing

This is a personal tool that solves my specific problem. If it helps you too, great! Feel free to fork and adapt to your needs.
