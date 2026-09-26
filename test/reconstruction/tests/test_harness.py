"""Host-only lifecycle checks; no VM required."""

import os
from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
LOOPBACK_FORWARD = 'hostfwd=tcp:127.0.0.1:${RA_SSH_PORT}-:22'


class HarnessLifecycleTests(unittest.TestCase):
    def test_both_qemu_guests_bind_ssh_to_loopback_only(self):
        for script in ("vm/install-omarchy.sh", "vm/guest.sh"):
            with self.subTest(script=script):
                source = (ROOT / script).read_text()
                self.assertEqual(source.count('hostfwd='), 1)
                self.assertIn(LOOPBACK_FORWARD, source)
                self.assertNotIn('hostfwd=tcp::', source)

    def test_overlay_nvram_is_writable_but_base_is_read_only(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "base").mkdir()
            (root / "base/omarchy-base.qcow2").write_bytes(b"base")
            vars_base = root / "base/OVMF_VARS.base.fd"
            vars_base.write_bytes(b"NVRAM")
            vars_base.chmod(0o444)
            stub = root / "qemu-img"
            stub.write_text("#!/bin/sh\nexit 0\n")
            stub.chmod(0o755)
            result = subprocess.run(
                ["bash", "-c", f"source '{ROOT}/lib/common.sh'; "
                 f"source '{ROOT}/vm/guest.sh'; ra_guest_create source"],
                env={**os.environ, "RA_WORK": directory, "PATH": f"{directory}:{os.environ['PATH']}"},
                capture_output=True, text=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(vars_base.stat().st_mode & 0o777, 0o444)
            self.assertEqual((root / "guests/source.vars.fd").stat().st_mode & 0o777, 0o644)

    def test_guest_sudo_uses_disposable_account_password(self):
        result = subprocess.run(
            ["bash", "-c", f"source '{ROOT}/lib/common.sh'; "
             "ra_ssh() { read -r password; test \"$password\" = spike; "
             "[[ $1 == *'sudo -S'* && $1 == *systemctl* && $1 == *poweroff* ]]; }; "
             "ra_ssh_sudo 'systemctl poweroff'"],
            capture_output=True, text=True,
        )
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_guest_start_requires_created_overlay(self):
        with tempfile.TemporaryDirectory() as directory:
            result = subprocess.run(
                ["bash", "-c", f"source '{ROOT}/lib/common.sh'; "
                 f"source '{ROOT}/vm/guest.sh'; ra_guest_start source blueprint-ra-source"],
                env={**os.environ, "RA_WORK": directory},
                capture_output=True, text=True,
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("overlay missing", result.stderr)

    def test_guest_start_refuses_second_active_guest(self):
        with tempfile.TemporaryDirectory() as directory:
            result = subprocess.run(
                ["bash", "-c", f"source '{ROOT}/lib/common.sh'; "
                 f"source '{ROOT}/vm/guest.sh'; RA_ACTIVE_GUEST=source; "
                 "ra_guest_start target blueprint-ra-target"],
                env={**os.environ, "RA_WORK": directory},
                capture_output=True, text=True,
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("guest source is still active", result.stderr)

    def test_insufficient_host_disk_stops_before_iso_download(self):
        with tempfile.TemporaryDirectory() as directory:
            result = subprocess.run(
                ["bash", str(ROOT / "run.sh")],
                env={**os.environ, "RA_WORK": directory,
                     "RA_MIN_FREE_BYTES": "999999999999999"},
                capture_output=True, text=True,
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("INFRASTRUCTURE FAIL", (Path(directory) / "artifacts/summary.txt").read_text())
            self.assertFalse((Path(directory) / "omarchy.iso").exists())

    def test_first_failure_survives_cleanup(self):
        with tempfile.TemporaryDirectory() as directory:
            result = subprocess.run(
                ["bash", "-c", f"source '{ROOT}/lib/common.sh'; "
                 "ra_phase CAPTURE; ra_fail CAPTURE 'capture failed first' || true; "
                 "ra_phase TARGET_PREFLIGHT; ra_cleanup_add false; ra_finish"],
                env={**os.environ, "RA_WORK": directory},
                capture_output=True, text=True,
            )
            self.assertEqual(result.returncode, 1, result.stderr)
            summary = (Path(directory) / "artifacts/summary.txt").read_text()
            self.assertIn("phase: CAPTURE", summary)
            self.assertIn("capture failed first", summary)
            self.assertIn("CAPTURE FAIL", summary)
            self.assertIn("TARGET_PREFLIGHT NOT RUN", summary)


class ReadinessDeadlineTests(unittest.TestCase):
    def test_readiness_deadline_is_wall_clock_even_when_each_probe_is_slow(self):
        # Old iteration-counted loops stretched "6s" to ~16s when every SSH probe stalled.
        import time
        with tempfile.TemporaryDirectory() as directory:
            started = time.monotonic()
            result = subprocess.run(
                ["bash", "-c", f"source '{ROOT}/lib/common.sh'; sleep 60 >/dev/null 2>&1 & pid=$!; status=0; "
                 "ra_measure_host() { :; }; ra_guest_health() { sleep 4; return 1; }; "
                 f"ra_wait_ready $pid '{directory}/log' 6 || status=$?; kill $pid; exit $status"],
                env={**os.environ, "RA_WORK": directory}, capture_output=True, text=True, timeout=60)
            elapsed = time.monotonic() - started
            self.assertNotEqual(result.returncode, 0)
            self.assertLess(elapsed, 12, f"deadline overran: {elapsed:.1f}s")

    def test_ssh_allows_a_loaded_guest_time_to_send_its_banner(self):
        options = (ROOT / "lib/common.sh").read_text().split("RA_SSH_OPTS=(")[1].split(")")[0]
        self.assertIn("ConnectTimeout=10", options)


if __name__ == "__main__":
    unittest.main()
