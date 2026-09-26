"""The terminal driver approves once at the exact prompt and answers only sudo's own prompt."""

from pathlib import Path
import subprocess
import sys
import tempfile
import textwrap
import unittest


HELPER = Path(__file__).resolve().parents[1] / "scenario/drive_terminal.py"
RESTORE_PROMPT = "Apply this restore? [y/N] "

# Stand-in for a remote Restore/update: optionally asks for approval, then
# raises sudo-style password prompts with echo disabled, as sudo does.
CHILD = textwrap.dedent("""
    import os, sys, termios, time
    work, mode = sys.argv[1], sys.argv[2]
    with open(os.path.join(work, "runs"), "a") as runs:
        runs.write("run\\n")
    def ask(prompt):
        sys.stdout.write(prompt); sys.stdout.flush()
        return sys.stdin.readline().strip()
    def sudo():
        attrs = termios.tcgetattr(0)
        quiet = attrs[:]; quiet[3] &= ~termios.ECHO
        termios.tcsetattr(0, termios.TCSANOW, quiet)
        answer = ask("[sudo] password for spike: ")
        termios.tcsetattr(0, termios.TCSANOW, attrs)
        print()
        return answer
    if mode.startswith("approve"):
        print("Restore plan", flush=True); time.sleep(0.2)
        if ask("\\nApply this restore? [y/N] ") != "yes":
            print("restore cancelled"); sys.exit(1)
    if mode in ("approve-sudo", "sudo"):
        if sudo() != "fixture-secret":
            print("Sorry, try again."); sys.exit(1)
        print("installed alacritty")
    if mode == "sudo-twice-wrong":
        sudo(); sudo(); sudo(); sudo()
    if mode == "changed":
        pass
    if mode == "approve-changed":
        print("Error: restore plan changed after approval; inspect the new plan and approve again"); sys.exit(1)
    print("done"); sys.exit(0)
""")


def drive(work: str, mode: str, *options: str) -> subprocess.CompletedProcess:
    child = Path(work, "child.py")
    child.write_text(CHILD)
    Path(work, "password").write_text("fixture-secret\n")
    return subprocess.run(
        [sys.executable, str(HELPER), "--transcript", f"{work}/transcript.txt", *options, "--",
         sys.executable, str(child), work, mode],
        capture_output=True, text=True, timeout=30, stdin=subprocess.DEVNULL,
    )


class DriveTerminalTests(unittest.TestCase):
    def test_approves_once_and_answers_sudo_without_recording_the_password(self):
        with tempfile.TemporaryDirectory() as work:
            result = drive(work, "approve-sudo", "--approve", RESTORE_PROMPT, "--sudo-password-file", f"{work}/password")
            self.assertEqual(result.returncode, 0, result.stderr)
            transcript = Path(work, "transcript.txt").read_text()
            self.assertIn("installed alacritty", transcript)
            self.assertEqual(transcript.count(RESTORE_PROMPT), 1)
            self.assertNotIn("fixture-secret", transcript + result.stdout + result.stderr)
            self.assertEqual(Path(work, "runs").read_text(), "run\n")

    def test_sudo_only_session_needs_no_approval(self):
        with tempfile.TemporaryDirectory() as work:
            result = drive(work, "sudo", "--sudo-password-file", f"{work}/password")
            self.assertEqual(result.returncode, 0, result.stderr)

    def test_required_approval_prompt_must_appear(self):
        with tempfile.TemporaryDirectory() as work:
            result = drive(work, "sudo", "--approve", RESTORE_PROMPT, "--sudo-password-file", f"{work}/password")
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("without the approval prompt", result.stderr)

    def test_sudo_prompt_without_a_password_file_is_not_answered(self):
        with tempfile.TemporaryDirectory() as work:
            result = drive(work, "sudo", "--prompt-timeout", "2")
            self.assertNotEqual(result.returncode, 0)
            self.assertNotIn("fixture-secret", Path(work, "transcript.txt").read_text())

    def test_repeated_sudo_prompts_are_capped(self):
        with tempfile.TemporaryDirectory() as work:
            result = drive(work, "sudo-twice-wrong", "--sudo-password-file", f"{work}/password", "--max-sudo", "2")
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("sudo asked more than 2 times", result.stderr)

    def test_failure_after_approval_is_reported_once_and_not_retried(self):
        with tempfile.TemporaryDirectory() as work:
            result = drive(work, "approve-changed", "--approve", RESTORE_PROMPT)
            self.assertEqual(result.returncode, 1)
            self.assertIn("changed after approval", result.stderr)
            self.assertEqual(Path(work, "runs").read_text(), "run\n")


if __name__ == "__main__":
    unittest.main()
