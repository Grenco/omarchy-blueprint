"""Fresh Machine B must pass Blueprint check with its package state untouched."""

import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
NOTE = ("package origin classification unavailable: pacman sync databases missing for core, extra, "
        "multilib, omarchy; installed packages are still checked, Capture keeps saved official/AUR state, "
        "and installing packages needs `omarchy update` first")


def envelope(command: str, ok: bool = True, **data) -> str:
    return json.dumps({"api_version": 1, "command": command, "ok": ok, "data": data})


FRESH_CHECK = envelope("check", machine={"name": "target", "source": "explicit"}, notes=[NOTE])
PLAN = envelope("restore", dry_run=True, plan={"operations": []})


def preflight(sync_databases: bool, check_status: int, check_stdout: str,
              plan_status: int = 0, plan_stdout: str = PLAN) -> tuple[subprocess.CompletedProcess, str, Path]:
    work = Path(tempfile.mkdtemp())
    Path(work, "artifacts/target").mkdir(parents=True)
    for name, text in {"check": check_stdout, "plan": plan_stdout}.items():
        Path(work, name).write_text(text)
    script = f"""
        set -euo pipefail
        source '{ROOT}/lib/common.sh'
        trap 'ra_finish "$?"' EXIT
        source '{ROOT}/scenario/stage-guest.sh'
        source '{ROOT}/scenario/capture-source.sh'
        source '{ROOT}/scenario/preflight-target.sh'
        ra_guest_exec() {{
          case $2 in
            *'$repo.db'*) printf '%s' '{'core ' if sync_databases else ''}' ;;
            *-Qqem*) echo 'pacman -Qq exit=0 lines=1043' ;;
          esac
        }}
        ra_blueprint() {{
          case " $* " in
            *" check "*) cat '{work}/check'; return {check_status} ;;
            *" restore "*) cat '{work}/plan'; return {plan_status} ;;
          esac
        }}
        ra_phase TARGET_PREFLIGHT
        ra_target_fresh_check
        ra_pass TARGET_PREFLIGHT
    """
    result = subprocess.run(["bash", "-c", script], capture_output=True, text=True, timeout=30,
                            env={**os.environ, "RA_WORK": str(work)}, stdin=subprocess.DEVNULL)
    return result, Path(work, "artifacts/summary.txt").read_text(), work


class TargetFreshCheckTests(unittest.TestCase):
    def test_fresh_target_check_and_plan_pass_without_exceptions(self):
        result, summary, work = preflight(False, 0, FRESH_CHECK)
        self.assertEqual(result.returncode, 0, summary + result.stderr)
        self.assertIn("TARGET_PREFLIGHT PASS", summary)
        self.assertNotIn("known gap", summary)
        self.assertTrue(Path(work, "artifacts/target/pacman-queries.txt").read_text())

    def test_check_failure_fails_preflight(self):
        result, summary, _ = preflight(False, 1, "")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("TARGET_PREFLIGHT FAIL", summary)
        self.assertIn("check failed", summary)

    def test_check_must_observe_the_fresh_package_state(self):
        blind = envelope("check", machine={"name": "target", "source": "explicit"}, notes=[])
        result, summary, _ = preflight(False, 0, blind)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("did not report", summary)

    def test_restore_planning_must_work(self):
        result, summary, _ = preflight(False, 0, FRESH_CHECK, 1, "")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Restore planning failed", summary)

    def test_refreshed_target_databases_invalidate_freshness(self):
        result, summary, _ = preflight(True, 0, FRESH_CHECK)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("sync databases", summary)

    def test_known_gap_mechanism_is_gone(self):
        sources = "\n".join(p.read_text() for p in [ROOT / "lib/common.sh", ROOT / "lib/capture_contract.py",
                                                     *sorted((ROOT / "scenario").glob("*.sh"))])
        for retired in ("known gap", "known_gap", "fresh-sync-gap", "fresh_sync_database_gap"):
            self.assertNotIn(retired, sources)


if __name__ == "__main__":
    unittest.main()
