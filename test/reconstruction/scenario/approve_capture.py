#!/usr/bin/env python3
"""Drive one real reviewed Capture: wait for its prompt, approve once, keep the transcript.

The command runs on a local pseudo-terminal because `capture --review`
requires one. Nothing is typed until the exact prompt appears. A missing
prompt or any failure after approval (including a changed review) is
reported as-is; the command is never re-run or re-approved.
"""

import argparse
import os
import select
import signal
import subprocess
import sys
import time

PROMPT = b"Apply this Capture? [y/N] "


def fail(message: str, transcript: bytes) -> int:
    tail = transcript[-2000:].decode(errors="replace")
    print(f"approve_capture: {message}\n--- transcript tail ---\n{tail}", file=sys.stderr)
    return 1


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--transcript", required=True)
    parser.add_argument("--prompt-timeout", type=float, default=900)
    parser.add_argument("--exit-timeout", type=float, default=1800)
    parser.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    command = args.command[1:] if args.command[:1] == ["--"] else args.command
    if not command:
        parser.error("missing command")

    master, slave = os.openpty()
    child = subprocess.Popen(command, stdin=slave, stdout=slave, stderr=slave,
                             start_new_session=True, close_fds=True)
    os.close(slave)
    output = bytearray()
    approved = False
    deadline = time.monotonic() + args.prompt_timeout
    with open(args.transcript, "wb") as transcript:
        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                os.killpg(child.pid, signal.SIGTERM)
                child.wait()
                if approved:
                    return fail("Capture did not finish after approval", bytes(output))
                return fail("Capture prompt did not appear before timeout", bytes(output))
            ready, _, _ = select.select([master], [], [], min(remaining, 1))
            if not ready:
                continue
            try:
                chunk = os.read(master, 4096)
            except OSError:  # EIO: the terminal closed because the child exited.
                chunk = b""
            if not chunk:
                break
            transcript.write(chunk)
            transcript.flush()
            output.extend(chunk)
            if not approved and output.endswith(PROMPT):
                os.write(master, b"yes\n")
                approved = True
                deadline = time.monotonic() + args.exit_timeout
    os.close(master)
    status = child.wait()
    if not approved:
        return fail(f"command exited {status} without the Capture prompt", bytes(output))
    if status != 0:
        return fail(f"approved Capture failed with exit {status}; not retrying", bytes(output))
    return 0


if __name__ == "__main__":
    sys.exit(main())
