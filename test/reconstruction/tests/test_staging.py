"""Guest staging gives both machines harness inputs only; package state stays fresh on the target."""

import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]


def stage(role: str) -> list[str]:
    """Run ra_guest_stage with guest I/O stubbed, returning the privileged commands issued."""
    with tempfile.TemporaryDirectory() as work:
        Path(work, "git-tls").mkdir()
        Path(work, "artifacts", role).mkdir(parents=True)
        Path(work, "git-tls/cert.pem").write_text("cert")
        Path(work, "blueprint").write_text("binary")
        log = Path(work, "sudo.log")
        script = f"""
            source '{ROOT}/lib/common.sh'
            source '{ROOT}/scenario/stage-guest.sh'
            ra_guest_wait_session() {{ :; }}
            ra_guest_exec() {{ cat >/dev/null; }}
            ra_guest_copy_to() {{ :; }}
            ra_ssh_sudo() {{ printf '%s\\n' "$1" >> '{log}'; }}
            ra_guest_stage {role}
        """
        result = subprocess.run(["bash", "-c", script], capture_output=True, text=True, timeout=30,
                                env={**os.environ, "RA_WORK": work, "RA_BLUEPRINT_BIN": f"{work}/blueprint"},
                                stdin=subprocess.DEVNULL)
        if result.returncode != 0:
            raise AssertionError(result.stderr)
        return log.read_text().splitlines() if log.exists() else []


class StagingTests(unittest.TestCase):
    def test_common_staging_never_refreshes_package_databases(self):
        for role in ("source", "target"):
            with self.subTest(role=role):
                commands = "\n".join(stage(role))
                self.assertIn("omarchy-blueprint", commands)
                self.assertNotRegex(commands, r"pacman\s+-S")

    def test_only_machine_a_refreshes_package_databases(self):
        calls = re.findall(r"ra_guest_refresh_package_databases\s+(\S+)",
                           "\n".join(p.read_text() for p in
                                     [ROOT / "run.sh", *sorted((ROOT / "scenario").glob("*.sh"))]))
        self.assertEqual(calls, ["source"])
        self.assertNotIn("ra_guest_refresh_package_databases", (ROOT / "scenario/preflight-target.sh").read_text())


if __name__ == "__main__":
    unittest.main()
