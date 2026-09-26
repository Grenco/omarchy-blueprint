"""The reviewed Capture approval helper answers the real prompt once and never retries."""

import os
from pathlib import Path
import subprocess
import sys
import tempfile
import textwrap
import unittest


HELPER = Path(__file__).resolve().parents[1] / "scenario/approve_capture.py"
PROMPT = "Apply this Capture? [y/N] "

# A stand-in for `omarchy-blueprint capture --review`: it requires a terminal,
# records whether anything was typed before its prompt, and counts invocations.
CHILD = textwrap.dedent("""
    import os, select, sys, time
    work = sys.argv[1]
    with open(os.path.join(work, "runs"), "a") as runs:
        runs.write("run\\n")
    if not os.isatty(0):
        print("capture --review requires an interactive terminal"); sys.exit(1)
    mode = sys.argv[2]
    print("Capture preview for machine source\\n\\nChanges\\n  packages/official:alacritty: Add", flush=True)
    time.sleep(0.3)
    if select.select([0], [], [], 0)[0]:
        print("EARLY INPUT"); sys.exit(3)
    if mode == "no-prompt":
        sys.exit(0)
    if mode == "hang":
        time.sleep(30)
    sys.stdout.write("\\nApply this Capture? [y/N] "); sys.stdout.flush()
    answer = sys.stdin.readline().strip()
    if answer != "yes":
        print("capture cancelled"); sys.exit(1)
    if mode == "changed":
        print("Error: capture review changed; review again: packages official:alacritty"); sys.exit(1)
    print("Captured state"); sys.exit(0)
""")


def approve(work: str, mode: str, *extra: str) -> subprocess.CompletedProcess:
    child = Path(work) / "child.py"
    child.write_text(CHILD)
    return subprocess.run(
        [sys.executable, str(HELPER), "--transcript", f"{work}/transcript.txt", *extra, "--",
         sys.executable, str(child), work, mode],
        capture_output=True, text=True, timeout=30, stdin=subprocess.DEVNULL,
    )


class ApproveCaptureTests(unittest.TestCase):
    def test_answers_exact_prompt_once_through_a_terminal(self):
        with tempfile.TemporaryDirectory() as work:
            result = approve(work, "ok")
            self.assertEqual(result.returncode, 0, result.stderr)
            transcript = Path(work, "transcript.txt").read_text()
            self.assertIn(PROMPT, transcript)
            self.assertIn("Captured state", transcript)
            self.assertNotIn("EARLY INPUT", transcript)
            self.assertEqual(Path(work, "runs").read_text(), "run\n")

    def test_missing_prompt_fails_without_rerunning(self):
        with tempfile.TemporaryDirectory() as work:
            result = approve(work, "no-prompt")
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("prompt", result.stderr)
            self.assertEqual(Path(work, "runs").read_text(), "run\n")

    def test_prompt_timeout_fails_and_stops_the_child(self):
        with tempfile.TemporaryDirectory() as work:
            result = approve(work, "hang", "--prompt-timeout", "2")
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("prompt", result.stderr)

    def test_changed_review_fails_once_and_is_preserved(self):
        with tempfile.TemporaryDirectory() as work:
            result = approve(work, "changed")
            self.assertNotEqual(result.returncode, 0)
            transcript = Path(work, "transcript.txt").read_text()
            self.assertIn("review again", transcript)
            self.assertIn("review again", result.stderr)
            self.assertEqual(Path(work, "runs").read_text(), "run\n")
            self.assertEqual(transcript.count(PROMPT), 1)


if __name__ == "__main__":
    unittest.main()
