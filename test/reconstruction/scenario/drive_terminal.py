#!/usr/bin/env python3
"""Drive one real terminal session: approve once at an exact prompt, answer only sudo's prompt.

The command runs on a local pseudo-terminal (use `ssh -tt` for a guest), so
programs that require a terminal behave as they do for a person. Nothing is
typed except:

- `yes`, once, when output ends with the exact `--approve` prompt;
- the password from `--sudo-password-file`, when output ends with sudo's
  own password prompt (at most `--max-sudo` times). sudo disables echo, so
  the password never reaches the transcript, and it is never in argv.

A missing approval prompt, an unanswered prompt, or any failure is reported
with the transcript tail. The command is never re-run or re-approved.
"""

import argparse
import os
import re
import select
import signal
import subprocess
import sys
import time

# sudo ("[sudo] password for spike: ") and sudo-rs ("[sudo: authenticate] Password: ").
SUDO_PROMPT = re.compile(rb"\[sudo[^\]\n]*\][^\n]*[Pp]assword[^\n]*: ?$")


def fail(label: str, message: str, transcript: bytes) -> int:
    tail = transcript[-2000:].decode(errors="replace")
    print(f"{label}: {message}\n--- transcript tail ---\n{tail}", file=sys.stderr)
    return 1


def drive(argv: list[str], label: str = "drive_terminal", approve: str | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--transcript", required=True)
    parser.add_argument("--approve", default=approve, help="exact prompt to answer with 'yes' once")
    parser.add_argument("--sudo-password-file")
    parser.add_argument("--max-sudo", type=int, default=3)
    parser.add_argument("--prompt-timeout", type=float, default=900, help="fail after this long without output")
    parser.add_argument("--exit-timeout", type=float, default=5400, help="fail if the command runs longer")
    parser.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args(argv)
    command = args.command[1:] if args.command[:1] == ["--"] else args.command
    if not command:
        parser.error("missing command")
    password = None
    if args.sudo_password_file:
        with open(args.sudo_password_file, "rb") as secret:
            password = secret.read().strip()
    approve_bytes = args.approve.encode() if args.approve else None

    master, slave = os.openpty()
    child = subprocess.Popen(command, stdin=slave, stdout=slave, stderr=slave,
                             start_new_session=True, close_fds=True)
    os.close(slave)
    output = bytearray()
    answered = 0  # output consumed by earlier answers; prompts are matched after it
    approved, sudo_answers = False, 0
    started = last_output = time.monotonic()

    def stop(message: str) -> int:
        os.killpg(child.pid, signal.SIGTERM)
        child.wait()
        os.close(master)
        return fail(label, message, bytes(output))

    with open(args.transcript, "wb") as transcript:
        while True:
            now = time.monotonic()
            if now - started > args.exit_timeout:
                return stop(f"command still running after {args.exit_timeout:.0f}s")
            if now - last_output > args.prompt_timeout:
                waiting = "the approval prompt" if approve_bytes and not approved else "the command"
                return stop(f"no output for {args.prompt_timeout:.0f}s while waiting for {waiting}")
            ready, _, _ = select.select([master], [], [], 1)
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
            last_output = time.monotonic()
            pending = bytes(output[answered:])
            if approve_bytes and not approved and pending.endswith(approve_bytes):
                os.write(master, b"yes\n")
                approved, answered = True, len(output)
            elif SUDO_PROMPT.search(pending):
                if password is None:
                    continue  # never answered; the idle timeout reports it
                if sudo_answers >= args.max_sudo:
                    return stop(f"sudo asked more than {args.max_sudo} times")
                os.write(master, password + b"\n")
                sudo_answers, answered = sudo_answers + 1, len(output)
    os.close(master)
    status = child.wait()
    if approve_bytes and not approved:
        return fail(label, f"command exited {status} without the approval prompt", bytes(output))
    if status != 0:
        return fail(label, f"command failed with exit {status}; not retrying", bytes(output))
    return 0


if __name__ == "__main__":
    sys.exit(drive(sys.argv[1:]))
