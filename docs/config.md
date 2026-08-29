# Configuration Guide

Glocker reads configuration from `/etc/glocker/config.yaml` (sample in [`conf/conf.yaml`](../conf/conf.yaml)).

## Core Settings

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

## Blocked Domains

Domains are permanently blocked by default unless marked as unblockable:

```yaml
domains:
  # Always blocked (permanent - default)
  - {name: "reddit.com"}

  # Always blocked, but can be temporarily unblocked
  - {name: "youtube.com", unblockable: true}

  # Time-based blocking - only blocked during specified windows
  - name: "twitter.com"
    block_windows:
      - start: "09:00"
        end: "17:00"
        days: ["Mon", "Tue", "Wed", "Thu", "Fri"]
      - start: "11:00"
        end: "15:00"
        days: ["Sat", "Sun"]
```

### Domain Blocking Behavior

**Default behavior:** Domains are **permanently blocked** (cannot be temporarily unblocked).

- **No time windows** → Always blocked (permanent by default)
- **Time windows specified** → Only blocked during those time windows
- **`unblockable: true`** → Domain can be temporarily unblocked (use for sites you occasionally need)
- Time format: 24-hour `HH:MM`, supports midnight-crossing (e.g., `22:00` to `05:00`)

**Note:** The `always_block` and `absolute` fields are deprecated. Domains are permanent by default; use `unblockable: true` for sites that can be temporarily unblocked.

## Updating Domain Blocklists

The [`update_domains.py`](../update_domains.py) script automates updating domain lists from curated blocklists. It supports multiple sources with automatic timestamp checking for idempotent updates.

### Available Sources

1. **Bon Appetit Porn Domains** - Comprehensive adult content blocklist (~800K domains)
2. **StevenBlack Unified Hosts** - Ads and malware domains
3. **HaGeZi DoH/VPN/TOR/Proxy Bypass** - Blocks encrypted DNS, VPN, TOR, proxy bypass methods
4. **UnblockStop Proxy Bypass** - Blocks proxy and filter-bypass sites (CroxyProxy, etc.)

### Usage

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

### Features

- **Idempotent updates** - Only updates if source timestamp has changed
- **Automatic deduplication** - Removes duplicate domains and `www.` prefixes
- **Source markers** - Each source is marked in the config file for easy identification
- **Preserves manual domains** - Only modifies managed source sections

After updating domains, reload the configuration:
```bash
glocker -reload
```

## Temporary Unblocking

```yaml
unblocking:
  reasons: ["work", "research", "emergency", "education"]
  log_file: "/var/log/glocker-unblocks.log"
  temp_unblock_time: 20  # Minutes
```

**Reason Validation:**
- The `reasons` list defines valid reasons for temporary unblocking
- When unblocking, you must provide one of these reasons
- Reason validation is case-insensitive (e.g., "Work" matches "work")
- If the reasons list is empty, any reason will be accepted
- Invalid reasons will be rejected with an error

Usage: `glocker -unblock "youtube.com:work research"`

## Web Tracking

```yaml
web_tracking:
  enabled: true
  command: "mpg123 /path/to/alert.mp3"
```

## Content Monitoring

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

## Forbidden Programs

```yaml
forbidden_programs:
  enabled: true
  check_interval_seconds: 5
  programs:
    - name: "chromium"          # killed DURING these windows
      kill_windows:
        - start: "20:00"
          end: "05:00"
          days: ["Mon", "Tue", "Wed", "Thu", "Fri"]
    - name: "steam"             # killed OUTSIDE these windows
      allow_windows:
        - start: "19:00"
          end: "22:00"
          days: ["Fri", "Sat"]
    - name: "firefox"           # extendible: -extend grants one hour, max once per 24h
      extendible: true
      kill_windows:
        - {start: "22:00", end: "05:00", days: ["Mon","Tue","Wed","Thu","Fri","Sat","Sun"]}
    - name: "discord"           # no windows → always killed
```

**Window modes:**
- `kill_windows` → program is killed **during** the listed windows
- `allow_windows` → program is killed **outside** the listed windows (inverse)
- Neither → killed 24/7
- `extendible: true` → `glocker -extend "name:reason"` grants a one-hour reprieve, capped at one grant per rolling 24h

Programs can also be added at runtime with `glocker -block-app "name1,name2"` (no windows, killed on sight); the addition is persisted to the config file.

## Kill on Block (DNS Cache Flush)

Browsers cache DNS internally, so a domain freshly added via `glocker -block` stays reachable in an already-running browser until it restarts. Listed process names are killed right after a block applies, forcing a fresh lookup against the updated `/etc/hosts`. Matching is a case-insensitive substring of the process name; unlike `forbidden_programs`, this ignores time windows and is not counted as a violation.

```yaml
kill_on_block:
  - firefox
  - chromium
  - brave
```

## Sudoers Control

```yaml
sudoers:
  enabled: true
  user: "noufal"
  allowed_sudoers_line: "noufal ALL=(ALL) NOPASSWD:ALL"
  blocked_sudoers_line: "noufal ALL=(ALL) NOPASSWD: /usr/bin/apt"
  # sudo is ALLOWED during these windows, blocked outside them
  allow_windows:
    - start: "10:00"
      end: "16:00"
      days: ["Mon", "Tue", "Wed", "Thu", "Fri"]
```

## Violation Tracking

```yaml
violation_tracking:
  enabled: true
  max_violations: 5
  time_window_minutes: 60
  command: "glocklock"
  lock_duration: "5m"  # For glocklock
  background: "/path/to/image.png"  # Optional lock-screen background (X11)
```

## Tamper Detection

```yaml
enable_self_healing: true
tamper_detection:
  enabled: true
  check_interval_seconds: 30
  alarm_command: "notify-send -u critical 'Glocker' 'Tampering detected!'"
```

## Accountability

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

## Lifecycle Logging

Install and uninstall events are recorded to a log that `glockpeek` reads to mark `UNMANAGED` periods. `glocker -uninstall` must cite one of the configured `reasons` (validation is skipped if the list is empty) and accepts an optional free-form `-note`.

```yaml
lifecycle:
  log_file: "/var/log/glocker-lifecycle.log"
  reasons: ["maintenance", "hardware", "testing"]
```

Usage: `sudo glocker -uninstall "maintenance" -note "kernel upgrade"`

## Panic Mode

```yaml
panic_command: "sudo pm-suspend"
```

## Time Window Logic

Time windows use HH:MM format and day-of-week arrays. Each entry has the same
shape regardless of which key it appears under (`block_windows`, `allow_windows`,
`kill_windows`):

```yaml
block_windows:        # or allow_windows / kill_windows
  - start: "09:00"
    end: "17:00"
    days: ["Mon", "Tue", "Wed", "Thu", "Fri"]
```

The same window shape is used by several keys (the key name carries the semantics):
- Domain blocking — `block_windows` (blocked during)
- Sudoers restrictions — `allow_windows` (sudo allowed during)
- Forbidden programs — `kill_windows` (killed during) or `allow_windows` (killed outside)

Time windows support midnight-crossing (e.g., start: "22:00", end: "05:00").

## Configuration Reload

After modifying the configuration file, reload without restarting:

```bash
glocker -reload
```

Check logs with:

```bash
journalctl -u glocker.service -f
```
