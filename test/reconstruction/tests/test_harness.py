"""Host-only lifecycle checks; no VM required."""

import os
from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]


class HarnessLifecycleTests(unittest.TestCase):
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


if __name__ == "__main__":
    unittest.main()
