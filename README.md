# Introduction

I needed an application that would block sites which distract me. However, none of the existing solutions I found solve this problem. 

This is almost wholly vibe coded and then manually modified for specific features and to avoid specific problems.

# Challenges
Given the control that Linux offers for `root`, it's hard to make something that *really* block everything. However, it is possible to make it very tedious to break out. That's what this application does. 

I've often found that there are liminal moments where I make the wrong decision in a fog of distraction. Having someone, or if not possible, something that makes it hard to make the wrong decision, lets me get back to work. 

That's what glocker tries to do. 

# Strategies and features

Glocker modifies the `/etc/hosts` file to redirect blocked domains to 127.0.0.1 (localhost). When users attempt to access these blocked domains, glocker detects and tracks these attempts as violations.

# Utilities

## glocklock

A standalone screen locker for X11 with two modes. Reads defaults from `/etc/glocker/config.yaml` (use `-conf` to specify a different path).

**Configuration** (in `violation_tracking` section):
```yaml
violation_tracking:
  lock_duration: "5m"  # Duration: "30s", "5m", or plain number (seconds, e.g., 300)
  mindful_text: "I will focus on my work and avoid distractions."
  background: "/path/to/image.png"  # Optional PNG/JPG background (default: dark green)
```

**Time-based mode**: Automatically unlocks after a configurable timeout period.

```bash
# Lock using duration from config (default: 1 minute if no config)
glocklock

# Lock for 5 minutes with custom message
glocklock -duration 5m -message "Break time"

# Use custom config file
glocklock -conf /path/to/config.yaml
```

**Text-based mode**: Requires typing a specific text to unlock. Useful for mindful pauses.

```bash
# Lock until mindful_text from config is typed correctly
glocklock -mindful

# Lock until text from file is typed correctly
glocklock -text /path/to/message.txt
```

The text-based mode displays the target text and shows typed characters in green (correct) or red (incorrect). Press Enter when the text matches to unlock, or Escape to clear and start over.

## glockpeek

A log analysis tool that provides summaries and insights from glocker's violation and unblock logs.

**Summary mode** (default): Shows aggregated statistics with colored bar charts.

```bash
# Show all summaries (violations and unblocks)
glockpeek

# Show only violations or unblocks
glockpeek -violations
glockpeek -unblocks

# Show top 10 items instead of default 5
glockpeek -top 10
```

**Date filtering**: Narrow down to specific time periods.

```bash
# Filter by year, month, or specific date
glockpeek -from 2024
glockpeek -from 2024-06
glockpeek -from 2024-06-15 -to 2024-06-30
```

**Detailed views**: See individual events for a specific day or aggregated daily stats for a month.

```bash
# Detailed timeline for a specific day
glockpeek -day 2024-06-15

# Daily aggregates for a month
glockpeek -month 2024-06
```

The output uses colored bars (red for above average, green for below) and inverse video highlighting for egregious periods.

# Options
A tool that I've found which does this reasonably well is [plucky](https://getplucky.net/). However, the strategies it employs are not particularly transparent and it's tedious to get it to work. It also has a dependency on a browser and doesn't support firefox which is what I use. I opened a support case and was told that my configuration wouldn't work. Hence, I let that go. 

Another one is [Accountable 2 you](https://accountable2you.com/linux/), but I couldn't get it to work so I let that go too. 

This is my attempt and if it solves my problem, I'll try to make it a hosted service. 
