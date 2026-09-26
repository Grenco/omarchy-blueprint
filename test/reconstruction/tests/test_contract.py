"""Pure, host-side checks on Blueprint's public Restore JSON."""

import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


MODULE = Path(__file__).resolve().parents[1] / "lib/contract.py"
spec = importlib.util.spec_from_file_location("contract", MODULE)
contract = importlib.util.module_from_spec(spec)
spec.loader.exec_module(contract)


class PlanContractTests(unittest.TestCase):
    def test_load_requires_successful_restore_envelope(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "plan.json"
            path.write_text(json.dumps({"api_version": 1, "command": "restore", "ok": True,
                                        "data": {"plan": {"operations": []}}}))
            self.assertEqual(contract.restore_plan(contract.load_envelope(path)), {"operations": []})
            path.write_text(json.dumps({"api_version": 1, "command": "capture", "ok": True,
                                        "data": {"plan": {"operations": []}}}))
            with self.assertRaises(AssertionError):
                contract.load_envelope(path)

    def test_rejects_delete_and_force_replacement(self):
        for op in ({"action": "remove"}, {"action": "write", "delete": {"destination": "/tmp/x"}},
                   {"action": "write", "file": {"replace_existing": True}},
                   {"action": "copy", "copy": {"replace_existing": True}},
                   {"action": "link", "symlink": {"replace_existing": True}}):
            with self.subTest(op=op), self.assertRaisesRegex(AssertionError, "destructive"):
                contract.assert_no_destructive_ops({"operations": [op]})

    def test_only_the_canonical_package_install_may_be_interactive(self):
        install = {"provider": "packages", "resource": "official:alacritty", "interactive": True,
                   "command": ["omarchy", "pkg", "add", "alacritty"],
                   "notice": "May ask for administrator authentication (sudo) in this terminal"}
        contract.assert_expected_interactive_ops({"operations": [install]}, "alacritty")
        for op in ({**install, "notice": ""}, {**install, "command": ["pacman", "-S", "alacritty"]},
                   {"provider": "themes", "resource": "theme", "interactive": True, "command": ["omarchy"]}):
            with self.subTest(op=op), self.assertRaisesRegex(AssertionError, "interactive"):
                contract.assert_expected_interactive_ops({"operations": [op]}, "alacritty")
        with self.assertRaisesRegex(AssertionError, "interactive"):
            contract.assert_expected_interactive_ops({"operations": [{**install, "interactive": False}]}, "alacritty")

    def test_the_canonical_elevated_install_is_required(self):
        install = {"provider": "packages", "resource": "official:alacritty", "interactive": True,
                   "command": ["omarchy", "pkg", "add", "alacritty"],
                   "notice": "May ask for administrator authentication (sudo) in this terminal"}
        other = {"provider": "packages", "resource": "mise:node", "command": ["mise", "install", "node"]}
        cases = {
            "missing": [other],
            "not interactive": [{**install, "interactive": False}],
            "wrong command": [{**install, "command": ["omarchy", "pkg", "add", "kitty"]}],
            "no notice": [{**install, "notice": "installs a package"}],
            "duplicated": [install, dict(install)],
        }
        for name, operations in cases.items():
            with self.subTest(name), self.assertRaises(AssertionError):
                contract.assert_expected_interactive_ops({"operations": operations}, "alacritty")

    def test_requires_provider_skip_and_mapped_copy(self):
        plan = {"operations": [{"provider": "resources", "action": "copy",
                                "resource": "resource:helper-script",
                                "copy": {"destination": "/home/spike/bin/blueprint-ra-helper"}}],
                "skipped": [{"provider": "resources", "resource": "resource:target-only-skip",
                             "reason": 'restore disabled for machine "target"'}]}
        contract.assert_provider_present(plan, "resources")
        contract.assert_skip(plan, "resources", "target-only-skip", "restore disabled")
        contract.assert_copy_destination(plan, "helper-script", "/home/spike/bin/blueprint-ra-helper")
        with self.assertRaises(AssertionError):
            contract.assert_copy_destination(plan, "helper-script", "/home/spike/.local/bin/blueprint-ra-helper")

    def test_converged_plan_with_null_operations_is_empty(self):
        # Blueprint encodes an empty plan's operations as null (run 36237192431).
        plan = contract.restore_plan({"data": {"plan": {"operations": None, "skipped": [
            {"provider": "resources", "resource": "resource:target-only-skip",
             "reason": 'restore disabled for machine "target"'}]}}})
        self.assertEqual(plan["operations"], [])
        contract.assert_final_convergence(plan, "target-only-skip")
        with self.assertRaises(AssertionError):
            contract.restore_plan({"data": {"plan": {"operations": "nope"}}})

    def test_final_convergence_allows_only_policy_skip(self):
        plan = {"operations": [], "skipped": [{"provider": "resources",
                "resource": "resource:target-only-skip", "reason": 'restore disabled for machine "target"'}]}
        contract.assert_final_convergence(plan, "target-only-skip")
        with self.assertRaises(AssertionError):
            contract.assert_final_convergence({**plan, "operations": [{"action": "copy"}]}, "target-only-skip")
        with self.assertRaises(AssertionError):
            contract.assert_final_convergence({**plan, "skipped": [{"resource": "other", "reason": "conflict"}]}, "target-only-skip")


def canonical_plan() -> dict:
    ops = [{"provider": p, "action": "write", "resource": f"{p}:x", "command": ["true"]}
           for p in ("themes", "plugins", "shell", "config", "hooks", "defaults")]
    ops += [{"provider": "packages", "action": "install", "resource": "official:alacritty", "interactive": True,
             "command": ["omarchy", "pkg", "add", "alacritty"],
             "notice": "May ask for administrator authentication (sudo) in this terminal"},
            # Shape observed in run 36236715592: a copied file Resource is a file write.
            {"provider": "resources", "action": "file", "resource": "resource:helper-script",
             "file": {"destination": "/home/spike/bin/blueprint-ra-helper", "expected_missing": True, "mode": 493}},
            {"provider": "resources", "action": "validate", "resource": "resource:git-fixture", "command": ["git"]}]
    return {"operations": ops, "skipped": [{"provider": "resources", "resource": "resource:target-only-skip",
                                            "reason": 'restore disabled for machine "target"'}]}


class CanonicalPlanTests(unittest.TestCase):
    def test_accepts_the_canonical_post_readiness_plan(self):
        contract.assert_canonical_plan(canonical_plan(), "alacritty")

    def test_rejects_each_canonical_violation(self):
        def without(provider):
            plan = canonical_plan()
            plan["operations"] = [op for op in plan["operations"] if op["provider"] != provider]
            return plan
        cases = {f"missing {p}": without(p) for p in
                 ("packages", "themes", "plugins", "shell", "config", "hooks", "defaults", "resources")}
        remove = canonical_plan(); remove["operations"].append({"provider": "themes", "action": "remove"})
        delete = canonical_plan(); delete["operations"].append({"provider": "config", "action": "write", "delete": {}})
        unmapped = canonical_plan(); unmapped["operations"][7]["file"]["destination"] = "/home/spike/.local/bin/blueprint-ra-helper"
        noskip = canonical_plan(); noskip["skipped"] = []
        wrongskip = canonical_plan(); wrongskip["skipped"][0]["reason"] = "conflict"
        unready = canonical_plan(); unready["requirements"] = [{"id": "packages.metadata"}]
        sync = canonical_plan(); sync["operations"].append({"provider": "packages", "action": "install", "command": ["pacman", "-Sy"]})
        cases.update({"remove": remove, "delete": delete, "unmapped helper": unmapped, "no skip": noskip,
                      "wrong skip": wrongskip, "requirement left": unready, "own sync": sync})
        for name, plan in cases.items():
            with self.subTest(name), self.assertRaises(AssertionError):
                contract.assert_canonical_plan(plan, "alacritty")

    def test_cli_checks_canonical_and_final_plans(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "plan.json"
            write = lambda plan: path.write_text(json.dumps({"api_version": 1, "command": "restore", "ok": True,
                                                             "data": {"dry_run": True, "plan": plan}}))
            run = lambda check: subprocess.run([sys.executable, str(MODULE), check, str(path)],
                                               capture_output=True, text=True).returncode
            write(canonical_plan())
            self.assertEqual(run("assert-canonical-plan"), 0)
            self.assertNotEqual(run("assert-final-plan"), 0)
            write({"operations": [], "skipped": canonical_plan()["skipped"]})
            self.assertEqual(run("assert-final-plan"), 0)
            self.assertNotEqual(run("assert-canonical-plan"), 0)


if __name__ == "__main__":
    unittest.main()
