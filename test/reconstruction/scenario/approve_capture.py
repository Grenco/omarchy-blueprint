#!/usr/bin/env python3
"""Drive one real reviewed Capture: wait for its prompt, approve once, keep the transcript.

`capture --review` requires a terminal; drive_terminal.py provides one and
types nothing until the exact prompt appears. A missing prompt or any
failure after approval (including a changed review) is reported as-is;
the command is never re-run or re-approved.
"""

from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parent))
from drive_terminal import drive  # noqa: E402

PROMPT = "Apply this Capture? [y/N] "

if __name__ == "__main__":
    sys.exit(drive(sys.argv[1:], label="approve_capture", approve=PROMPT))
