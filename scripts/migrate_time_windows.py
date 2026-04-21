#!/usr/bin/env python3
"""One-shot migration: rename glocker YAML time-window keys to the new
semantically-distinct names. Delete this file once the two conf files have
been ported and committed.

  sudoers.time_allowed                      -> sudoers.allow_windows
  domains[].time_windows                    -> domains[].block_windows
  forbidden_programs.programs[].time_windows-> forbidden_programs.programs[].kill_windows

The rename is indent-aware so the same `time_windows:` key maps to two
different new names depending on where it appears. A .bak is written beside
each input file.

Usage:
  ./scripts/migrate_time_windows.py conf/conf.yaml conf/conf.yaml.sample
"""

from __future__ import annotations

import re
import shutil
import sys
from pathlib import Path

# (pattern, replacement) — applied top-to-bottom.
# Live YAML keys are matched by exact indent; commented template lines are
# matched by any leading whitespace so sample-file copy-paste templates stay
# in sync.
REPLACEMENTS: list[tuple[re.Pattern[str], str]] = [
    # Live keys
    (re.compile(r"^(  )time_allowed:", re.MULTILINE), r"\1allow_windows:"),
    (re.compile(r"^(      )time_windows:", re.MULTILINE), r"\1kill_windows:"),
    (re.compile(r"^(    )time_windows:", re.MULTILINE), r"\1block_windows:"),
    # Commented template lines like `#   time_windows:` in the sample file.
    # Domains-style templates (no `-` before the key).
    (re.compile(r"^(\s*#\s+)time_windows:", re.MULTILINE), r"\1block_windows:"),
    (re.compile(r"^(\s*#\s+)time_allowed:", re.MULTILINE), r"\1allow_windows:"),
]


def migrate(path: Path) -> tuple[int, int, int]:
    text = path.read_text()
    backup = path.with_suffix(path.suffix + ".bak")
    shutil.copy2(path, backup)

    counts = []
    for pattern, repl in REPLACEMENTS:
        text, n = pattern.subn(repl, text)
        counts.append(n)

    path.write_text(text)
    return tuple(counts[:3])  # (allow, kill, block) live-key counts


def main(argv: list[str]) -> int:
    if len(argv) < 2:
        print(__doc__, file=sys.stderr)
        return 2

    for arg in argv[1:]:
        p = Path(arg)
        if not p.exists():
            print(f"skip: {p} does not exist", file=sys.stderr)
            continue
        allow, kill, block = migrate(p)
        print(
            f"{p}: renamed "
            f"{allow} time_allowed→allow_windows, "
            f"{block} time_windows→block_windows (domains), "
            f"{kill} time_windows→kill_windows (programs). "
            f"Backup: {p}.bak"
        )
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
