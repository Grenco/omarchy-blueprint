"""Pure, host-side checks on Blueprint's source-side JSON and captured profile."""

import copy
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


MODULE = Path(__file__).resolve().parents[1] / "lib/capture_contract.py"
spec = importlib.util.spec_from_file_location("capture_contract", MODULE)
contract = importlib.util.module_from_spec(spec)
spec.loader.exec_module(contract)
VALUES = contract.expected()


def target(category, key, outcome, desired="unknown"):
    return {"category": category, "key": key, "outcome": outcome, "desired": desired,
            "current": "present", "policy": {"value": "update", "explicit": False,
                                             "source": {"kind": "default", "category": category}}}


def preview():
    changes = [target(category, key, "add") for category, key in contract.canonical_capture_targets(VALUES)]
    resources = [target("resources", f"resource:{r}", "noop", "present")
                 for r in ("helper-script", "git-fixture", "target-only-skip")]
    return {"machine": "source", "sections": [
        {"group": "Changes", "targets": changes},
        {"group": "Blocked", "targets": [target("config", ".bashrc", "blocked")]},
        {"group": "No action needed", "targets": resources}]}


def skip_policy():
    key = "resource:target-only-skip"
    return {"scope": "machine:target",
            "rules": {"capture": [], "restore": [{"category": "resources", "target": key, "value": "skip"}]},
            "targets": [{"category": "resources", "target": key, "effective": {
                "capture": {"value": "update", "explicit": False,
                            "source": {"kind": "default", "category": "resources", "target": key}},
                "restore": {"value": "skip", "explicit": True,
                            "source": {"kind": "machine-target", "machine": "target",
                                       "category": "resources", "target": key}}}}]}


PROFILE = {
    "packages/official.txt": "alacritty\n",
    "themes/themes.toml": "current = 'catppuccin'\ntheme = []\n",
    "plugins/plugins.toml": "[[plugin]]\nid = 'blueprint.ra.fixture'\nenabled = true\n",
    "shell/shell.toml": "version = 1\nhash = 'abc'\n",
    "config/config.toml": "included = ['.config/blueprint-ra/config.toml']\n[[file]]\npath = '.config/blueprint-ra/config.toml'\nhash = 'x'\n",
    "hooks/hooks.toml": "[[hook]]\npath = 'post-update.d/blueprint-ra'\nhash = 'x'\nmode = '0755'\n",
    "defaults/defaults.toml": "terminal = 'alacritty'\n",
    "resources/resources.toml": """link = []
[[resource]]
id = 'helper-script'
path = '~/.local/bin/blueprint-ra-helper'
[[resource]]
id = 'git-fixture'
path = '~/Projects/blueprint-ra-fixture'
strategy = 'git'
remote = 'https://10.0.2.2:9443/blueprint-ra.git'
revision = 'e161ca52ec7604e6fb335ef15fc212bba03a2994'
[[resource]]
id = 'target-only-skip'
path = '~/.local/share/blueprint-ra/skip.txt'
""",
    "machines/target.toml": """name = 'target'
[[resource_path]]
resource = 'helper-script'
path = '~/bin/blueprint-ra-helper'
[policy]
[[policy.restore]]
category = 'resources'
target = 'resource:target-only-skip'
setting = 'disabled'
""",
}


def write_profile(root: Path, overrides=None) -> Path:
    for name, text in {**PROFILE, **(overrides or {})}.items():
        (root / name).parent.mkdir(parents=True, exist_ok=True)
        (root / name).write_text(text)
    return root


class CapturePreviewTests(unittest.TestCase):
    def test_accepts_canonical_candidates_despite_unrelated_blocks(self):
        contract.assert_capture_preview(preview(), "source", VALUES)

    def test_rejects_wrong_machine(self):
        with self.assertRaises(AssertionError):
            contract.assert_capture_preview(preview(), "target", VALUES)

    def test_rejects_blocked_preserved_or_missing_canonical_target(self):
        for mutate in (lambda t: t.update(outcome="blocked"), lambda t: t.update(outcome="preserve"),
                       lambda t: t.update(outcome="noop"), lambda t: t.update(key="other")):
            data = preview()
            mutate(data["sections"][0]["targets"][0])
            with self.subTest(target=data["sections"][0]["targets"][0]), self.assertRaises(AssertionError):
                contract.assert_capture_preview(data, "source", VALUES)

    def test_rejects_undesired_or_blocked_resource(self):
        for change in ({"desired": "unknown"}, {"outcome": "blocked"}):
            data = preview()
            data["sections"][2]["targets"][1].update(change)
            with self.subTest(change=change), self.assertRaises(AssertionError):
                contract.assert_capture_preview(data, "source", VALUES)


class PolicyAndMachineTests(unittest.TestCase):
    def test_accepts_explicit_target_machine_skip(self):
        contract.assert_restore_skip(skip_policy(), "target", "resources", "resource:target-only-skip")

    def test_rejects_inherited_profile_or_other_machine_skip(self):
        mutations = (
            lambda d: d.update(scope="profile"),
            lambda d: d["rules"].update(restore=[]),
            lambda d: d["targets"][0]["effective"]["restore"].update(explicit=False),
            lambda d: d["targets"][0]["effective"]["restore"]["source"].update(kind="profile-target"),
            lambda d: d["targets"][0]["effective"]["restore"]["source"].update(machine="source"),
            lambda d: d["targets"][0]["effective"]["restore"].update(value="apply"),
        )
        for mutate in mutations:
            data = skip_policy()
            mutate(data)
            with self.assertRaises(AssertionError):
                contract.assert_restore_skip(data, "target", "resources", "resource:target-only-skip")

    def test_resource_mapping_requires_portable_and_effective_paths(self):
        data = {"resources": {"items": [{"id": "helper-script", "path": "~/.local/bin/blueprint-ra-helper",
                                         "effective_path": "/home/spike/bin/blueprint-ra-helper"}]}}
        contract.assert_resource_mapping(data, "helper-script", "~/.local/bin/blueprint-ra-helper",
                                         "/home/spike/bin/blueprint-ra-helper")
        with self.assertRaises(AssertionError):
            contract.assert_resource_mapping(data, "helper-script", "~/.local/bin/blueprint-ra-helper",
                                             "/home/spike/.local/bin/blueprint-ra-helper")
        with self.assertRaises(AssertionError):
            contract.assert_resource_mapping(data, "git-fixture", "x", "y")

    def test_machine_current_and_restore_defaults(self):
        contract.assert_machine_current({"machine": {"name": "target", "source": "binding"}}, "target", "binding")
        with self.assertRaises(AssertionError):
            contract.assert_machine_current({"machine": {"name": "target", "source": "explicit"}}, "target", "binding")
        defaults = {"machine": "target", "conflicts": "safe", "convergence": "additive"}
        contract.assert_restore_defaults(defaults, "target", "safe", "additive")
        with self.assertRaises(AssertionError):
            contract.assert_restore_defaults({**defaults, "convergence": "exact"}, "target", "safe", "additive")

    def test_cli_requires_named_successful_envelope(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "check.json"
            envelope = {"api_version": 1, "command": "check", "ok": True,
                        "data": {"machine": {"name": "source", "source": "explicit"}}}
            path.write_text(json.dumps(envelope))
            run = lambda machine: subprocess.run([sys.executable, str(MODULE), "check", str(path), machine],
                                                 capture_output=True, text=True)
            self.assertEqual(run("source").returncode, 0)
            self.assertNotEqual(run("target").returncode, 0)
            path.write_text(json.dumps({**envelope, "ok": False}))
            self.assertNotEqual(run("source").returncode, 0)


class ProfileTests(unittest.TestCase):
    def test_accepts_canonical_profile(self):
        with tempfile.TemporaryDirectory() as directory:
            contract.assert_profile(write_profile(Path(directory)), VALUES)

    def test_rejects_each_missing_canonical_fact(self):
        broken = {
            "packages/official.txt": "alacritty-git\n",
            "themes/themes.toml": "current = 'tokyo-night'\n",
            "plugins/plugins.toml": "plugin = []\n",
            "shell/shell.toml": "",
            "config/config.toml": "included = ['.config/blueprint-ra/config.toml']\n",
            "hooks/hooks.toml": "hook = []\n",
            "defaults/defaults.toml": "terminal = 'ghostty'\n",
            "resources/resources.toml": PROFILE["resources/resources.toml"].replace("e161ca5", "0000000"),
            "machines/target.toml": PROFILE["machines/target.toml"].replace("setting = 'disabled'", "setting = 'enabled'"),
        }
        for name, text in broken.items():
            with self.subTest(file=name), tempfile.TemporaryDirectory() as directory:
                with self.assertRaises((AssertionError, KeyError)):
                    contract.assert_profile(write_profile(Path(directory), {name: text}), VALUES)

    def test_requires_mapping_to_survive(self):
        text = PROFILE["machines/target.toml"].replace("~/bin/blueprint-ra-helper", "~/.local/bin/blueprint-ra-helper")
        with tempfile.TemporaryDirectory() as directory, self.assertRaises(AssertionError):
            contract.assert_profile(write_profile(Path(directory), {"machines/target.toml": text}), VALUES)


# Observed on the fresh Omarchy 4.0.4 target in run 36224717263.
FRESH_CHECK_STDERR = """Error: check packages: detect explicitly installed native packages: pacman -Qqen: exit status 1: warning: database file for 'core' does not exist (use '-Sy' to download)
warning: database file for 'extra' does not exist (use '-Sy' to download)
warning: database file for 'multilib' does not exist (use '-Sy' to download)
warning: database file for 'omarchy' does not exist (use '-Sy' to download)
"""


class KnownGapTests(unittest.TestCase):
    def test_recognizes_fresh_sync_database_check_failure(self):
        self.assertTrue(contract.is_fresh_sync_database_gap(1, "", FRESH_CHECK_STDERR))

    def test_repository_warning_list_is_incidental(self):
        only_core = FRESH_CHECK_STDERR.splitlines()[0]
        self.assertTrue(contract.is_fresh_sync_database_gap(1, "", only_core))
        renamed = FRESH_CHECK_STDERR.replace("'core'", "'core-testing'")
        self.assertTrue(contract.is_fresh_sync_database_gap(1, "", renamed))

    def test_rejects_other_status_success_envelope_or_other_error(self):
        success = json.dumps({"api_version": 1, "command": "check", "ok": True, "data": {}})
        cases = [
            (0, "", FRESH_CHECK_STDERR),
            (2, "", FRESH_CHECK_STDERR),
            (1, success, FRESH_CHECK_STDERR),
            (1, "", "Error: check packages: detect explicitly installed native packages: pacman -Qqen: exit status 1: error: failed to initialize alpm library"),
            (1, "", "Error: check themes: Omarchy did not report an active theme"),
            (1, "", FRESH_CHECK_STDERR.replace("pacman -Qqen", "pacman -Qqem")),
        ]
        for status, stdout, stderr in cases:
            with self.subTest(status=status, stderr=stderr[:60]):
                self.assertFalse(contract.is_fresh_sync_database_gap(status, stdout, stderr))

    def test_rejects_any_other_stderr_content(self):
        cases = {
            "unrelated second error": FRESH_CHECK_STDERR + "Error: check themes: some unrelated new failure\n",
            "unrelated line between warnings": FRESH_CHECK_STDERR.replace(
                "warning: database file for 'extra'", "warning: something else\nwarning: database file for 'extra'"),
            "prefix before the error": "Warning: Permanently added '[127.0.0.1]:2222' (ED25519) to the list of known hosts.\n"
                                       + FRESH_CHECK_STDERR,
            "trailing text on the error line": FRESH_CHECK_STDERR.replace("(use '-Sy' to download)\n",
                                                                          "(use '-Sy' to download); also broken\n", 1),
            "empty": "",
        }
        for name, stderr in cases.items():
            with self.subTest(name):
                self.assertFalse(contract.is_fresh_sync_database_gap(1, "", stderr))

    def test_blank_lines_are_not_content(self):
        self.assertTrue(contract.is_fresh_sync_database_gap(1, "", "\n" + FRESH_CHECK_STDERR + "\n\n"))

    def test_cli_exit_status_reports_classification(self):
        with tempfile.TemporaryDirectory() as directory:
            out, err = Path(directory, "check.json"), Path(directory, "check.stderr")
            out.write_text("")
            err.write_text(FRESH_CHECK_STDERR)
            run = lambda status: subprocess.run([sys.executable, str(MODULE), "fresh-sync-gap", status, str(out), str(err)],
                                                capture_output=True, text=True).returncode
            self.assertEqual(run("1"), 0)
            self.assertNotEqual(run("0"), 0)


if __name__ == "__main__":
    unittest.main()
