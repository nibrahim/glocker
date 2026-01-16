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

### Domain Types

- `always_block: true` - Blocked 24/7
- `absolute: true` - Cannot be temporarily unblocked (for high-risk sites)
- `time_windows` - Blocked only during specified times/days
- Time format: 24-hour `HH:MM`, supports midnight-crossing (e.g., `22:00` to `05:00`)

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
    - name: "chromium"
      time_windows:
        - start: "20:00"
          end: "05:00"
          days: ["Mon", "Tue", "Wed", "Thu", "Fri"]
    - name: "steam"  # Always killed (no time windows)
```

## Sudoers Control

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

## Violation Tracking

```yaml
violation_tracking:
  enabled: true
  max_violations: 5
  time_window_minutes: 60
  command: "glocklock"
  lock_duration: "5m"  # For glocklock
  mindful_text: "I will focus on my work."  # For glocklock -mindful
  background: "/path/to/image.png"  # For glocklock
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

## Panic Mode

```yaml
panic_command: "sudo pm-suspend"
```

## Time Window Logic

Time windows use HH:MM format and day-of-week arrays:

```yaml
time_windows:
  - start: "09:00"
    end: "17:00"
    days: ["Mon", "Tue", "Wed", "Thu", "Fri"]
```

Applied to:
- Domain blocking
- Sudoers restrictions
- Forbidden programs

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
