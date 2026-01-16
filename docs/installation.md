# Installation and Usage Guide

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

## Browser Extension Installation

The Firefox extension lives in `extensions/firefox/`. To install:

1. Navigate to `about:debugging#/runtime/this-firefox`
2. Click "Load Temporary Add-on"
3. Select `extensions/firefox/manifest.json`
4. Extension will monitor URLs and page content based on keywords from Glocker

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

### Things to Verify During Testing

1. Killing of forbidden programs
2. Violation tracking
3. Making sure that blocked domains are in `/etc/hosts` properly
4. APIs for browser extension integration (confirm with curl)

### Testing Browser Extension

After making changes:
1. Navigate to `about:debugging#/runtime/this-firefox`
2. Load temporary extension from `extensions/firefox/manifest.json`
3. Check browser console for extension logs
4. Monitor `/var/log/glocker-reports.log` for content monitoring events

## System Requirements

- Go 1.21+ for building
- Linux with systemd
- iptables (optional, for firewall blocking)
- Firefox (for browser extension)
- Root access for installation

## File Locations

- **Binary:** `/usr/local/bin/glocker` (setuid root)
- **Config:** `/etc/glocker/config.yaml`
- **Service:** `/etc/systemd/system/glocker.service`
- **Socket:** `/tmp/glocker.sock`
- **Logs:**
  - `/var/log/glocker-reports.log` (content monitoring)
  - `/var/log/glocker-unblocks.log` (unblock requests)
- **systemd logs:** `journalctl -u glocker.service`

## Troubleshooting

### Check Service Status

```bash
systemctl status glocker.service
```

### View Logs

```bash
# Follow service logs
journalctl -u glocker.service -f

# View violation logs
tail -f /var/log/glocker-reports.log

# View unblock logs
tail -f /var/log/glocker-unblocks.log
```

### Socket Communication Issues

```bash
# Check if socket exists
ls -l /tmp/glocker.sock

# Check if daemon is running
ps aux | grep glocker
```

### Hosts File Not Updating

```bash
# Check if hosts file is immutable
lsattr /etc/hosts

# Remove immutability (requires root)
sudo chattr -i /etc/hosts
```

### Permission Issues

Glocker requires setuid root privileges. After installation, verify:

```bash
ls -l /usr/local/bin/glocker
# Should show: -rwsr-xr-x (note the 's' in permissions)
```
