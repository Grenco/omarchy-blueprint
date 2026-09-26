"""The production check can only pass by running the canonical reconstruction lifecycle."""

from pathlib import Path
import re
import unittest


ROOT = Path(__file__).resolve().parents[1]
WORKFLOWS = ROOT.parents[1] / ".github/workflows"


def jobs(workflow: str) -> list[str]:
    return re.findall(r"^    name: (.+)$", (WORKFLOWS / workflow).read_text(), re.M)


class GateIdentityTests(unittest.TestCase):
    def test_canonical_runner_has_no_diagnostic_or_early_exit_path(self):
        runner = (ROOT / "run.sh").read_text()
        for diagnostic in ("SOAK", "soak", "BOOT_DEBUG", "boot_debug", "exit 0"):
            self.assertNotIn(diagnostic, runner)
        self.assertNotIn("RA_BOOT_DEBUG", (ROOT / "vm/install-omarchy.sh").read_text())

    def test_production_workflow_runs_only_the_canonical_lifecycle(self):
        workflow = (WORKFLOWS / "reconstruction-assurance.yml").read_text()
        self.assertEqual(jobs("reconstruction-assurance.yml"), ["Reconstruction Assurance"])
        self.assertIn("bash test/reconstruction/run.sh", workflow)
        for diagnostic in ("inputs:", "boot_soak", "boot_debug", "soak.sh", "RA_BOOT"):
            self.assertNotIn(diagnostic, workflow)

    def test_boot_soak_is_a_separate_dispatch_only_diagnostic(self):
        workflow = (WORKFLOWS / "reconstruction-boot-soak.yml").read_text()
        self.assertEqual(jobs("reconstruction-boot-soak.yml"), ["Reconstruction Boot Soak"])
        self.assertNotIn("Reconstruction Assurance", workflow)
        self.assertNotIn("pull_request", workflow)
        self.assertIn("workflow_dispatch", workflow)
        self.assertIn("bash test/reconstruction/soak.sh", workflow)
        self.assertNotIn("run.sh", workflow)


class ScriptSyntaxTests(unittest.TestCase):
    def test_diagnostic_entrypoint_parses(self):
        import subprocess
        result = subprocess.run(["bash", "-n", str(ROOT / "soak.sh")], capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)


if __name__ == "__main__":
    unittest.main()
