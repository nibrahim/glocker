#!/usr/bin/env python3
"""
Multi-source domain list updater for glocker config file.

This script supports updating domain blocklists from multiple sources,
each with custom fetching and parsing logic. The script is idempotent
and checks timestamps to avoid redundant updates.

Usage:
    ./update_domains.py          - List all available sources
    ./update_domains.py <id>     - Update from specific source
    ./update_domains.py all      - Update from all sources sequentially
    ./update_domains.py strip    - Remove all managed sources (keeps manual domains)
"""

import json
import re
import sys
import urllib.request
from typing import Optional, Tuple, List, Dict, Callable

# Constants
CONFIG_FILE = "conf/conf.yaml"


## Utility Functions

def fetch_url(url: str) -> bytes:
    """Fetch content from a URL."""
    try:
        with urllib.request.urlopen(url, timeout=30) as response:
            return response.read()
    except Exception as e:
        print(f"Error fetching {url}: {e}", file=sys.stderr)
        sys.exit(1)


def extract_timestamp_near_marker(config_content: str, marker: str) -> Optional[str]:
    """
    Extract timestamp near a specific marker in the config.
    Looks for '# Last updated: TIMESTAMP' after the marker.
    """
    lines = config_content.split('\n')
    marker_idx = None

    # Find marker line
    for i, line in enumerate(lines):
        if marker in line:
            marker_idx = i
            break

    if marker_idx is None:
        return None

    # Look for timestamp in next few lines after marker
    pattern = r'# Last updated: ([0-9T:\-Z]+)'
    for i in range(marker_idx, min(marker_idx + 5, len(lines))):
        match = re.search(pattern, lines[i])
        if match:
            return match.group(1)

    return None


def find_marker_position(config_content: str, marker: str) -> Optional[int]:
    """Find the position of a marker comment. Returns line index or None."""
    lines = config_content.split('\n')
    for i, line in enumerate(lines):
        if marker in line:
            return i
    return None


def find_domains_section_end(lines: list, start_idx: int) -> int:
    """
    Find the end of the domains list starting from start_idx.
    Returns the line index where domains list ends (before next top-level section).
    """
    i = start_idx
    while i < len(lines):
        line = lines[i]

        # Check for two consecutive blank lines (end of domain block)
        if i + 1 < len(lines):
            if line.strip() == '' and lines[i + 1].strip() == '':
                # Check if followed by expanded YAML format domain
                if i + 2 < len(lines) and lines[i + 2].strip().startswith('- name:'):
                    return i  # End before the blank lines

        # Check for top-level YAML key (non-indented, not a list item)
        if line and not line[0].isspace() and not line.strip().startswith('#'):
            return i

        i += 1

    return len(lines)

## Source-Specific Functions: Bon Appetit Porn Domains

def fetch_bon_appetit() -> Tuple[List[str], str, Dict]:
    """
    Fetch Bon Appetit porn domains list.
    Returns: (domains_list, timestamp, metadata_dict)
    """
    META_URL = "https://raw.githubusercontent.com/Bon-Appetit/porn-domains/refs/heads/main/meta.json"
    BASE_URL = "https://raw.githubusercontent.com/Bon-Appetit/porn-domains/refs/heads/main/"

    # Step 1: Fetch metadata
    print("  Fetching metadata...")
    content = fetch_url(META_URL)

    try:
        meta = json.loads(content.decode('utf-8'))
    except json.JSONDecodeError as e:
        print(f"  Error parsing meta.json: {e}", file=sys.stderr)
        sys.exit(1)

    # Extract blocklist info
    if 'blocklist' not in meta:
        print("  Error: 'blocklist' not found in meta.json", file=sys.stderr)
        sys.exit(1)

    blocklist = meta['blocklist']
    filename = blocklist.get('name')
    timestamp = blocklist.get('updated')
    lines_count = blocklist.get('lines', 0)

    if not filename or not timestamp:
        print("  Error: Missing required fields in blocklist metadata", file=sys.stderr)
        sys.exit(1)

    print(f"  Current blocklist: {filename}")
    print(f"  Last updated: {timestamp} ({lines_count} domains)")

    # Step 2: Download blocklist
    url = BASE_URL + filename
    print(f"  Downloading blocklist...")

    content = fetch_url(url)
    domains = []

    for line in content.decode('utf-8').splitlines():
        domain = line.strip()
        if domain:  # Skip empty lines
            domains.append(domain)

    if not domains:
        print("  Error: Downloaded blocklist is empty", file=sys.stderr)
        sys.exit(1)

    metadata = {
        'filename': filename,
        'count': len(domains),
        'lines_count': lines_count
    }

    return domains, timestamp, metadata


def parse_bon_appetit(domain: str) -> str:
    """Format domain in compact JSON YAML format."""
    return f'  - {{"name": "{domain}", "absolute": true}}'


## Source-Specific Functions: StevenBlack Hosts

def fetch_stevenblack() -> Tuple[List[str], str, Dict]:
    """
    Fetch StevenBlack unified hosts list.
    Returns: (domains_list, timestamp, metadata_dict)
    """
    HOSTS_URL = "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts"

    # Download hosts file
    print("  Fetching hosts file...")
    content = fetch_url(HOSTS_URL)

    # Parse header for metadata
    lines = content.decode('utf-8').splitlines()
    timestamp = None
    total_entries = None
    domains = []

    for line in lines:
        line = line.strip()

        # Extract date from header
        if line.startswith('# Date:'):
            # Format: # Date: 19 December 2025 21:22:56 (UTC)
            date_str = line.replace('# Date:', '').strip()
            # Convert to ISO8601 format
            try:
                from datetime import datetime
                # Parse the date string
                date_str_clean = date_str.replace('(UTC)', '').strip()
                dt = datetime.strptime(date_str_clean, '%d %B %Y %H:%M:%S')
                timestamp = dt.strftime('%Y-%m-%dT%H:%M:%SZ')
            except Exception as e:
                print(f"  Warning: Could not parse date: {e}", file=sys.stderr)
                timestamp = date_str

        # Extract total entries count
        elif line.startswith('# Number of unique domains:'):
            match = re.search(r'# Number of unique domains:\s*([\d,]+)', line)
            if match:
                total_entries = int(match.group(1).replace(',', ''))

        # Parse domain entries (format: 0.0.0.0 domain.name)
        elif line and not line.startswith('#') and line.startswith('0.0.0.0 '):
            parts = line.split()
            if len(parts) >= 2:
                domain = parts[1].strip()
                # Skip localhost entries
                if domain not in ['localhost', 'localhost.localdomain', 'local',
                                'broadcasthost', '0.0.0.0']:
                    domains.append(domain)

    if not timestamp:
        print("  Error: Could not extract timestamp from hosts file", file=sys.stderr)
        sys.exit(1)

    if not domains:
        print("  Error: No domains found in hosts file", file=sys.stderr)
        sys.exit(1)

    print(f"  Last updated: {timestamp} ({len(domains)} domains)")

    metadata = {
        'count': len(domains),
        'total_entries': total_entries or len(domains)
    }

    return domains, timestamp, metadata


def parse_stevenblack(domain: str) -> str:
    """Format domain in compact JSON YAML format."""
    return f'  - {{\"name\": \"{domain}\", \"absolute\": true}}'


## Source-Specific Functions: HaGeZi DoH/VPN/TOR/Proxy Bypass

def fetch_hagezi() -> Tuple[List[str], str, Dict]:
    """
    Fetch HaGeZi DoH/VPN/TOR/Proxy Bypass blocklist.
    Returns: (domains_list, timestamp, metadata_dict)
    """
    BLOCKLIST_URL = "https://raw.githubusercontent.com/hagezi/dns-blocklists/main/wildcard/doh-vpn-proxy-bypass-onlydomains.txt"

    # Download blocklist
    print("  Fetching blocklist...")
    content = fetch_url(BLOCKLIST_URL)

    # Parse file
    lines = content.decode('utf-8').splitlines()
    timestamp = None
    version = None
    entry_count = None
    domains = []

    for line in lines:
        line = line.strip()

        # Extract metadata from header
        if line.startswith('# Last modified:'):
            # Format: # Last modified: 23 Dec 2025 10:32 UTC
            date_str = line.replace('# Last modified:', '').strip()
            try:
                from datetime import datetime
                # Parse the date string
                dt = datetime.strptime(date_str, '%d %b %Y %H:%M %Z')
                timestamp = dt.strftime('%Y-%m-%dT%H:%M:%SZ')
            except Exception as e:
                print(f"  Warning: Could not parse date: {e}", file=sys.stderr)
                timestamp = date_str

        elif line.startswith('# Version:'):
            version = line.replace('# Version:', '').strip()

        elif line.startswith('# Number of entries:'):
            match = re.search(r'# Number of entries:\s*(\d+)', line)
            if match:
                entry_count = int(match.group(1))

        # Parse domain entries (non-comment, non-empty lines)
        elif line and not line.startswith('#'):
            # Filter out Mullvad VPN domains (legitimate VPN service)
            if 'mullvad' not in line.lower():
                domains.append(line)

    if not timestamp:
        print("  Error: Could not extract timestamp from blocklist", file=sys.stderr)
        sys.exit(1)

    if not domains:
        print("  Error: No domains found in blocklist", file=sys.stderr)
        sys.exit(1)

    print(f"  Last updated: {timestamp} ({len(domains)} domains)")
    if entry_count:
        filtered_count = entry_count - len(domains)
        if filtered_count > 0:
            print(f"  Filtered out {filtered_count} Mullvad VPN domains")

    metadata = {
        'count': len(domains),
        'version': version,
        'original_count': entry_count or len(domains)
    }

    return domains, timestamp, metadata


def parse_hagezi(domain: str) -> str:
    """Format domain in compact JSON YAML format."""
    return f'  - {{\"name\": \"{domain}\", \"absolute\": true}}'


## Source-Specific Functions: UnblockStop Proxy Bypass

def fetch_unblockstop() -> Tuple[List[str], str, Dict]:
    """
    Fetch UnblockStop proxy and filter-bypass sites blocklist.
    Returns: (domains_list, timestamp, metadata_dict)
    """
    BLOCKLIST_URL = "https://raw.githubusercontent.com/tachnoraki/unblockstop/main/unblockstop.txt"

    # Download blocklist
    print("  Fetching blocklist...")
    content = fetch_url(BLOCKLIST_URL)

    # Parse file (Adblock Plus format)
    lines = content.decode('utf-8').splitlines()
    timestamp = None
    version = None
    domains = []

    for line in lines:
        line = line.strip()

        # Extract metadata from header
        if line.startswith('! Version:'):
            version = line.replace('! Version:', '').strip()
            # Version format: 202506091445 (YYYYMMDDHHmm)
            try:
                from datetime import datetime
                dt = datetime.strptime(version, '%Y%m%d%H%M')
                timestamp = dt.strftime('%Y-%m-%dT%H:%M:%SZ')
            except Exception:
                # If parsing fails, use version as-is
                timestamp = version

        # Parse domain entries (Adblock format: ||domain.com^)
        elif line.startswith('||') and line.endswith('^'):
            # Extract domain from ||domain.com^
            domain = line[2:-1]  # Remove || prefix and ^ suffix
            if domain:  # Skip empty entries
                domains.append(domain)

    if not timestamp:
        print("  Error: Could not extract timestamp/version from blocklist", file=sys.stderr)
        sys.exit(1)

    if not domains:
        print("  Error: No domains found in blocklist", file=sys.stderr)
        sys.exit(1)

    print(f"  Last updated: {timestamp} ({len(domains)} domains)")

    metadata = {
        'count': len(domains),
        'version': version
    }

    return domains, timestamp, metadata


def parse_unblockstop(domain: str) -> str:
    """Format domain in compact JSON YAML format."""
    return f'  - {{\"name\": \"{domain}\", \"absolute\": true}}'


## Source Registry
SOURCES = [
    {
        'id': 1,
        'name': 'Bon Appetit Porn Domains',
        'description': 'Comprehensive adult content blocklist',
        'marker': '# Domains from https://github.com/Bon-Appetit/porn-domains',
        'fetch_func': fetch_bon_appetit,
        'parse_func': parse_bon_appetit,
    },
    {
        'id': 2,
        'name': 'StevenBlack Unified Hosts',
        'description': 'Unified hosts file with ads and malware domains',
        'marker': '# Domains from https://github.com/StevenBlack/hosts',
        'fetch_func': fetch_stevenblack,
        'parse_func': parse_stevenblack,
    },
    {
        'id': 3,
        'name': 'HaGeZi DoH/VPN/TOR/Proxy Bypass',
        'description': 'Blocks encrypted DNS, VPN, TOR, and proxy bypass methods (excludes Mullvad)',
        'marker': '# Domains from https://github.com/hagezi/dns-blocklists (DoH/VPN/TOR/Proxy Bypass)',
        'fetch_func': fetch_hagezi,
        'parse_func': parse_hagezi,
    },
    {
        'id': 4,
        'name': 'UnblockStop Proxy Bypass',
        'description': 'Blocks proxy and filter-bypass sites like CroxyProxy',
        'marker': '# Domains from https://github.com/tachnoraki/unblockstop',
        'fetch_func': fetch_unblockstop,
        'parse_func': parse_unblockstop,
    },
    # Add more sources here following the same pattern
]


## Generic Source Update Logic

def get_source_by_id(source_id: int) -> Optional[Dict]:
    """Get source definition by ID."""
    for source in SOURCES:
        if source['id'] == source_id:
            return source
    return None


def update_config_section(config_content: str, marker: str, marker_line_idx: Optional[int],
                         formatted_domains: str, timestamp: str, domain_count: int) -> str:
    """
    Update or insert a source's section in the config file.
    Works for both existing markers and first-run scenarios.
    """
    lines = config_content.split('\n')

    # Build the new block
    new_block = [
        f"{marker}. Blocked 24/7",
        f"# Last updated: {timestamp} ({domain_count} domains)",
        "",
        formatted_domains
    ]

    if marker_line_idx is not None:
        # Marker exists - replace the existing block
        # Find the end of this source's block (two consecutive blank lines)
        end_idx = marker_line_idx + 1
        while end_idx < len(lines) - 1:
            if lines[end_idx].strip() == '' and lines[end_idx + 1].strip() == '':
                # Found two blank lines
                break
            end_idx += 1

        # Replace the old block with new one
        new_lines = lines[:marker_line_idx] + new_block + lines[end_idx:]
    else:
        # Marker doesn't exist - append to end of domains section
        # Find "domains:" section
        domains_idx = None
        for i, line in enumerate(lines):
            if line.strip() == 'domains:':
                domains_idx = i
                break

        if domains_idx is None:
            print("  Error: 'domains:' section not found in config file", file=sys.stderr)
            sys.exit(1)

        # Find where to insert (end of domains list)
        insert_idx = find_domains_section_end(lines, domains_idx + 1)

        # Insert with blank line before
        new_block_with_separator = [""] + new_block
        new_lines = lines[:insert_idx] + new_block_with_separator + lines[insert_idx:]

    return '\n'.join(new_lines)


def update_source(source_id: int):
    """Update domains from a specific source."""
    # Step 1: Find source
    source = get_source_by_id(source_id)
    if not source:
        print(f"Error: Source ID {source_id} not found", file=sys.stderr)
        print("\nAvailable sources:")
        list_sources()
        sys.exit(1)

    print(f"\nUpdating source [{source['id']}]: {source['name']}")
    print(f"Description: {source['description']}\n")

    # Step 2: Read config file
    try:
        with open(CONFIG_FILE, 'r') as f:
            config_content = f.read()
    except FileNotFoundError:
        print(f"  Error: Config file '{CONFIG_FILE}' not found", file=sys.stderr)
        sys.exit(1)
    except Exception as e:
        print(f"  Error reading config file: {e}", file=sys.stderr)
        sys.exit(1)

    # Step 3: Check if marker exists
    marker = source['marker']
    marker_line_idx = find_marker_position(config_content, marker)

    # Step 4: Extract existing timestamp if marker exists
    existing_timestamp = None
    if marker_line_idx is not None:
        existing_timestamp = extract_timestamp_near_marker(config_content, marker)

    # Step 5: Fetch latest data from source
    fetch_func = source['fetch_func']
    domains, timestamp, metadata = fetch_func()

    # Step 6: Check idempotency
    if existing_timestamp and existing_timestamp == timestamp:
        print(f"\n  Already up to date (last update: {timestamp})")
        return

    if marker_line_idx is not None:
        print("  Update available, proceeding...")
    else:
        print("  First run - marker not found, will append to domains list")
        # Verify domains section exists
        if 'domains:' not in config_content:
            print("  Error: 'domains:' section not found in config file", file=sys.stderr)
            sys.exit(1)

    # Step 7: Strip www prefix and remove duplicates
    print(f"  Processing {len(domains)} domains...")
    original_count = len(domains)

    # Strip www. prefix from all domains
    stripped_domains = []
    for domain in domains:
        if domain.startswith('www.'):
            stripped_domains.append(domain[4:])  # Remove 'www.' prefix
        else:
            stripped_domains.append(domain)

    # Remove duplicates while preserving order
    seen = set()
    unique_domains = []
    for domain in stripped_domains:
        if domain not in seen:
            seen.add(domain)
            unique_domains.append(domain)

    removed_count = original_count - len(unique_domains)
    if removed_count > 0:
        print(f"  Removed {removed_count} duplicate/www-prefixed domains")

    # Format domains using source's parse function
    parse_func = source['parse_func']
    formatted_lines = [parse_func(domain) for domain in unique_domains]
    formatted_domains = '\n'.join(formatted_lines)

    # Step 8: Update config
    print(f"  Updating {CONFIG_FILE}...")
    new_config = update_config_section(
        config_content, marker, marker_line_idx,
        formatted_domains, timestamp, len(unique_domains)
    )

    # Step 9: Write updated config
    try:
        with open(CONFIG_FILE, 'w') as f:
            f.write(new_config)
        print(f"  Successfully updated config file\n")
    except Exception as e:
        print(f"  Error writing config file: {e}", file=sys.stderr)
        sys.exit(1)

## Strip Managed Sources

def strip_managed_sources():
    """Remove all managed domain lists from the config file."""
    print("\nStripping all managed domain sources from config file...")
    print("This will remove all auto-managed domain lists, keeping only manually added domains.\n")

    # Step 1: Read config file
    try:
        with open(CONFIG_FILE, 'r') as f:
            config_content = f.read()
    except FileNotFoundError:
        print(f"  Error: Config file '{CONFIG_FILE}' not found", file=sys.stderr)
        sys.exit(1)
    except Exception as e:
        print(f"  Error reading config file: {e}", file=sys.stderr)
        sys.exit(1)

    # Step 2: Track which sources were found and removed
    original_content = config_content
    removed_sources = []

    # Step 3: Remove each managed source block
    for source in SOURCES:
        marker = source['marker']
        marker_line_idx = find_marker_position(config_content, marker)

        if marker_line_idx is not None:
            # Found this source's marker
            lines = config_content.split('\n')

            # Find the end of this source's block
            # Look for two consecutive blank lines or next top-level section
            end_idx = marker_line_idx + 1
            while end_idx < len(lines) - 1:
                if lines[end_idx].strip() == '' and lines[end_idx + 1].strip() == '':
                    # Found two blank lines - end of block
                    end_idx += 1  # Include the second blank line
                    break
                end_idx += 1

            # Remove the block (including marker and one trailing blank line)
            new_lines = lines[:marker_line_idx] + lines[end_idx:]
            config_content = '\n'.join(new_lines)

            removed_sources.append(source['name'])
            print(f"  ✓ Removed: {source['name']}")

    # Step 4: Check if anything was removed
    if not removed_sources:
        print("  No managed sources found in config file.")
        print("  Config file unchanged.\n")
        return

    # Step 5: Write the cleaned config back
    try:
        with open(CONFIG_FILE, 'w') as f:
            f.write(config_content)
        print(f"\n  Successfully stripped {len(removed_sources)} managed source(s) from config file")
        print(f"  Removed sources: {', '.join(removed_sources)}")
        print("\n  Manual domains have been preserved.")
        print(f"  Config file: {CONFIG_FILE}\n")
    except Exception as e:
        print(f"  Error writing config file: {e}", file=sys.stderr)
        sys.exit(1)

## Source Listing
def list_sources():
    """Display all available sources with their current status."""
    # Read config file once
    try:
        with open(CONFIG_FILE, 'r') as f:
            config_content = f.read()
    except FileNotFoundError:
        print(f"Warning: Config file '{CONFIG_FILE}' not found", file=sys.stderr)
        config_content = ""
    except Exception as e:
        print(f"Warning: Error reading config file: {e}", file=sys.stderr)
        config_content = ""

    print("\nAvailable domain sources:\n")

    for source in SOURCES:
        print(f"  [{source['id']}] {source['name']}")
        print(f"      {source['description']}")

        # Check if marker exists
        marker = source['marker']
        marker_idx = find_marker_position(config_content, marker)

        if marker_idx is not None:
            # Extract timestamp
            timestamp = extract_timestamp_near_marker(config_content, marker)
            if timestamp:
                # Try to extract domain count from timestamp line
                lines = config_content.split('\n')
                for i in range(marker_idx, min(marker_idx + 5, len(lines))):
                    match = re.search(r'\((\d+) domains\)', lines[i])
                    if match:
                        count = match.group(1)
                        print(f"      Last updated: {timestamp} ({count} domains)")
                        break
                else:
                    print(f"      Last updated: {timestamp}")
            else:
                print(f"      Status: Marker found but no timestamp")
        else:
            print(f"      Status: Not yet configured (first run)")

        print(f"      Marker: {marker}")
        print()

    print("Usage:")
    print("  ./update_domains.py <number>  - Update a specific source")
    print("  ./update_domains.py all       - Update all sources")
    print("  ./update_domains.py strip     - Remove all managed sources\n")

## Update All Sources

def update_all_sources():
    """Update domains from all sources sequentially."""
    print("\nUpdating all domain sources...\n")
    print("=" * 60)

    success_count = 0
    error_count = 0
    skipped_count = 0

    for source in SOURCES:
        print(f"\n[{source['id']}/{len(SOURCES)}] Processing: {source['name']}")
        print("-" * 60)

        try:
            update_source(source['id'])
            success_count += 1
        except SystemExit as e:
            # update_source() calls sys.exit() on errors
            # Catch it and continue with next source
            if e.code == 0:
                # Exit code 0 means success or "already up to date"
                skipped_count += 1
            else:
                error_count += 1
                print(f"  ⚠️  Error updating source {source['id']}, continuing with next source...")

    # Summary
    print("\n" + "=" * 60)
    print("Update Summary:")
    print(f"  ✓ Successfully updated: {success_count}")
    print(f"  ⊘ Already up to date: {skipped_count}")
    if error_count > 0:
        print(f"  ✗ Errors: {error_count}")
    print("=" * 60 + "\n")

## Main Entry Point

def main():
    """Main execution function with CLI argument parsing."""
    if len(sys.argv) == 1:
        # No arguments - list sources
        list_sources()
        return

    if len(sys.argv) != 2:
        progname = sys.argv[0]
        help_message = f"""Usage: {progname} [source_id|all|strip]
       {progname}          - List all sources
       {progname} all      - Update all sources
       {progname} strip    - Remove all managed domain sources"""
        print(help_message, file=sys.stderr)
        sys.exit(1)

    # Check for all command
    if sys.argv[1].lower() in ['all', '-all', '--all']:
        update_all_sources()
        return

    # Check for strip command
    if sys.argv[1].lower() == 'strip':
        strip_managed_sources()
        return

    # Parse source ID
    try:
        source_id = int(sys.argv[1])
    except ValueError:
        print(f"Error: Invalid source ID '{sys.argv[1]}'. Must be a number, 'all', or 'strip'.", file=sys.stderr)
        print("\nAvailable sources:")
        list_sources()
        sys.exit(1)

    # Update the specified source
    update_source(source_id)


if __name__ == "__main__":
    main()
