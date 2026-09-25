"""Pure, host-side checks on Blueprint's public Restore JSON."""

import importlib.util
import json
from pathlib import Path
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

    def test_rejects_interactive_operation(self):
        with self.assertRaisesRegex(AssertionError, "interactive"):
            contract.assert_no_interactive_ops({"operations": [{"interactive": True}]})

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

    def test_final_convergence_allows_only_policy_skip(self):
        plan = {"operations": [], "skipped": [{"provider": "resources",
                "resource": "resource:target-only-skip", "reason": 'restore disabled for machine "target"'}]}
        contract.assert_final_convergence(plan, "target-only-skip")
        with self.assertRaises(AssertionError):
            contract.assert_final_convergence({**plan, "operations": [{"action": "copy"}]}, "target-only-skip")
        with self.assertRaises(AssertionError):
            contract.assert_final_convergence({**plan, "skipped": [{"resource": "other", "reason": "conflict"}]}, "target-only-skip")


if __name__ == "__main__":
    unittest.main()
