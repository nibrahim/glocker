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

# Uninstall
sudo glocker -uninstall "reason for uninstalling"
```

## What It Does

Glocker uses **9 independent monitoring systems** that work together:

1. **Hosts File Blocking** - Redirects blocked domains to `127.0.0.1`
2. **Firewall Blocking** - Network-level blocking via iptables (optional)
3. **Web Tracking** - Intercepts HTTP/HTTPS requests, records violations
4. **Browser Extension** - Monitors page content for forbidden keywords
5. **Forbidden Programs** - Kills specified programs during time windows
6. **Violation Tracking** - Triggers actions when threshold exceeded (e.g., screen lock)
7. **Sudoers Control** - Restricts `sudo` access during blocking periods
8. **Tamper Detection** - Self-healing when critical files are modified
9. **Panic Mode** - Emergency system suspension with re-suspend on early wake

Each system can be independently enabled/disabled and configured with time windows for fine-grained control.

## Documentation

- **[Installation & Usage Guide](docs/installation.md)** - Commands, utilities, development setup
- **[Configuration Guide](docs/config.md)** - All YAML configuration options
- **[Architecture](docs/architecture.md)** - System design, monitoring systems, technical details

## Key Features

- **Time-Based Blocking** - Block sites only during work hours
- **Temporary Unblocking** - Unblock domains for short periods with logged reasons
- **Accountability** - Email notifications to partner on violations
- **Content Monitoring** - Firefox extension watches for keywords on any page
- **Screen Locker** - Time-based or text-based mindful unlocking
- **Log Analysis** - Visual summaries of violations and patterns with `glockpeek`
- **Panic Mode** - Nuclear option: suspend system and re-suspend on early wake

## Utilities

- **[glocker](cmd/glocker/)** - Main daemon and CLI
- **[glocklock](cmd/glocklock/)** - X11 screen locker with time/text-based modes
- **[glockpeek](cmd/glockpeek/)** - Log analysis tool with visual summaries

## Example Configuration

```yaml
domains:
  # Always blocked (no time windows = always block by default)
  - {name: "reddit.com"}

  # Always blocked, cannot be temporarily unblocked
  - {name: "facebook.com", absolute: true}

  # Time-based blocking - only blocked during specified windows
  - name: "twitter.com"
    time_windows:
      - start: "09:00"
        end: "17:00"
        days: ["Mon", "Tue", "Wed", "Thu", "Fri"]

# Kill programs during time windows
forbidden_programs:
  programs:
    - name: "chromium"
      time_windows:
        - start: "20:00"
          end: "05:00"
          days: ["Mon", "Tue", "Wed", "Thu", "Fri"]

# Lock screen after 5 violations in 60 minutes
violation_tracking:
  enabled: true
  max_violations: 5
  time_window_minutes: 60
  command: "glocklock -duration 5m"
```

**Domain Blocking Behavior:**
- **No time windows specified** → Domain is always blocked (default behavior)
- **Time windows specified** → Domain is only blocked during those time windows
- **`absolute: true`** → Domain cannot be temporarily unblocked (permanent block)

See [sample config](conf/conf.yaml) and [configuration guide](docs/config.md) for all options.

## Command Examples

```bash
# Domain management
glocker -unblock "youtube.com,reddit.com:work research"
glocker -block "facebook.com,instagram.com"
glocker -add-keyword "gambling,casino,poker"

# Control
glocker -reload          # Reload config
glocker -lock            # Lock sudo immediately
glocker -panic 30        # Suspend for 30 minutes

# Analysis
glockpeek                # Show violation/unblock summaries
glockpeek -day 2024-06-15   # Hour-by-hour timeline
glockpeek -month 2024-06    # Calendar view
```

## Architecture

Glocker is a **Go application** that runs as a systemd service with setuid root privileges:

- **Daemon:** Runs enforcement loop every 60s, manages protections
- **CLI:** Communicates with daemon via Unix socket (`/tmp/glocker.sock`)
- **Browser Extension:** Firefox extension in [`extensions/firefox/`](extensions/firefox/)
- **Config:** YAML configuration in `/etc/glocker/config.yaml` ([sample](conf/conf.yaml))

See [architecture documentation](docs/architecture.md) for detailed design.

## Project Structure

```
glocker/
├── cmd/                        # Binaries
│   ├── glocker/                # Main daemon
│   ├── glocklock/              # Screen locker
│   └── glockpeek/              # Log analyzer
├── internal/                   # Application packages
│   ├── cli/                    # Command processors
│   ├── config/                 # Configuration loading
│   ├── enforcement/            # Core blocking logic
│   ├── monitoring/             # Background monitors
│   ├── web/                    # HTTP server for extension
│   └── ...
├── extensions/firefox/         # Browser extension
├── conf/conf.yaml              # Sample config (~60MB)
├── extras/glocker.service      # Systemd service
└── docs/                       # Documentation
```

See [CLAUDE.md](CLAUDE.md) for complete file map and developer guide.

## Additional Resources

- **[Domain Blocklist Updater](update_domains.py)** - Automated domain list updates from curated sources
- **[Android Port Architecture](ANDROID.md)** - Design docs for Android version
- **[Gap Analysis](GAP_ANALYSIS.md)** - Detailed refactoring analysis
- **[Implementation Status](IMPLEMENTATION_STATUS.md)** - Feature completion report

## System Requirements

- Linux with systemd
- Go 1.21+ (for building)
- iptables (optional, for firewall blocking)
- Firefox (for browser extension)
- Root access (for installation)

## License

See [LICENSE](LICENSE) file for details.

## Contributing

This is a personal tool that solves my specific problem. If it helps you too, great! Feel free to fork and adapt to your needs.
