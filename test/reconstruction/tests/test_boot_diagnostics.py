"""Hang diagnostics tell a busy guest from a stuck one without the guest's help."""

import os
from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]


def activity(process: str) -> dict:
    with tempfile.TemporaryDirectory() as work:
        script = f"""
            source '{ROOT}/lib/common.sh'
            {process} &
            pid=$!
            ra_qemu_activity $pid 2
            kill $pid
        """
        out = subprocess.run(["bash", "-c", script], capture_output=True, text=True, timeout=30,
                             env={**os.environ, "RA_WORK": work}).stdout
    return dict(field.split("=", 1) for field in out.split())


class QemuActivityTests(unittest.TestCase):
    def test_busy_process_shows_cpu_progress(self):
        sample = activity("bash -c 'while :; do :; done'")
        self.assertTrue(sample["alive"] == "yes")
        self.assertGreater(int(sample["cpu_ticks_delta"]), 0)

    def test_idle_process_shows_none(self):
        sample = activity("sleep 30")
        self.assertEqual(sample["alive"], "yes")
        self.assertEqual(int(sample["cpu_ticks_delta"]), 0)
        self.assertEqual(int(sample["write_bytes_delta"]), 0)

    def test_dead_process_is_reported(self):
        with tempfile.TemporaryDirectory() as work:
            out = subprocess.run(["bash", "-c", f"source '{ROOT}/lib/common.sh'; ra_qemu_activity 999999 1"],
                                 capture_output=True, text=True, timeout=30, env={**os.environ, "RA_WORK": work}).stdout
        self.assertIn("alive=no", out)


if __name__ == "__main__":
    unittest.main()
