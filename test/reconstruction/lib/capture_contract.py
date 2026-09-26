#!/usr/bin/env python3
"""Assertions for Blueprint's public source-side JSON and the captured profile (no guest orchestration)."""

import argparse
import json
from pathlib import Path
import tomllib


EXPECTED_ENV = Path(__file__).resolve().parents[1] / "fixtures/expected.env"


def expected() -> dict:
    return dict(line.split("=", 1) for line in EXPECTED_ENV.read_text().splitlines() if line)


def load_envelope(path: Path, command: str) -> dict:
    payload = json.loads(Path(path).read_text())
    if not (payload.get("api_version") == 1 and payload.get("command") == command
            and payload.get("ok") is True):
        raise AssertionError(f"expected successful v1 {command!r} JSON envelope")
    return payload["data"]


def assert_machine_current(data: dict, name: str, source: str) -> None:
    machine = data.get("machine") or {}
    if machine.get("name") != name or machine.get("source") != source:
        raise AssertionError(f"expected machine {name} from {source}, got {machine}")


def assert_resource_mapping(data: dict, resource: str, portable: str, effective: str) -> None:
    for item in data["resources"]["items"]:
        if item["id"] == resource:
            if item.get("path") != portable or item.get("effective_path") != effective:
                raise AssertionError(f"{resource} maps {item.get('path')} -> {item.get('effective_path')}")
            return
    raise AssertionError(f"resource {resource} is not tracked")


def assert_restore_skip(data: dict, machine: str, category: str, target: str) -> None:
    if data.get("scope") != f"machine:{machine}":
        raise AssertionError(f"policy scope is {data.get('scope')}, want machine:{machine}")
    if {"category": category, "target": target, "value": "skip"} not in data["rules"]["restore"]:
        raise AssertionError(f"no explicit Restore skip rule for {category} {target}")
    matches = [item for item in data["targets"]
               if item["category"] == category and item.get("target") == target]
    if len(matches) != 1:
        raise AssertionError(f"expected one policy entry for {category} {target}")
    restore = matches[0]["effective"]["restore"]
    source = restore["source"]
    if not (restore["value"] == "skip" and restore["explicit"] is True
            and source.get("kind") == "machine-target" and source.get("machine") == machine
            and source.get("target") == target):
        raise AssertionError(f"Restore for {target} is not an explicit {machine} skip: {restore}")


def canonical_capture_targets(values: dict) -> dict:
    """Customizations whose first Capture must actually write profile state."""
    return {
        ("packages", f"official:{values['RA_PACKAGE']}"),
        ("themes", "active"),
        ("plugins", f"plugin:{values['RA_PLUGIN_ID']}"),
        ("shell", "state"),
        ("config", values["RA_CONFIG_PATH"]),
        ("hooks", values["RA_HOOK_PATH"].removeprefix(".config/omarchy/hooks/")),
        ("defaults", "terminal"),
    }


def assert_capture_preview(data: dict, machine: str, values: dict) -> None:
    if data.get("machine") != machine:
        raise AssertionError(f"Capture preview is for {data.get('machine')}, want {machine}")
    targets = {(t["category"], t["key"]): t for section in data["sections"] for t in section["targets"]}
    for key in sorted(canonical_capture_targets(values)):
        target = targets.get(key)
        if target is None:
            raise AssertionError(f"Capture preview lacks {key[0]} {key[1]}")
        if target["outcome"] not in {"add", "update"}:
            raise AssertionError(f"{key[0]} {key[1]} would not be captured: {target['outcome']}"
                                 f" {target.get('safety_reason', '')}".rstrip())
    # `track` records Resources when they are tracked, so Capture has nothing
    # further to write; they must still be desired and unobstructed.
    for resource in (values["RA_HELPER_RESOURCE"], values["RA_GIT_RESOURCE"], values["RA_SKIP_RESOURCE"]):
        target = targets.get(("resources", f"resource:{resource}"))
        if target is None or target["desired"] != "present" or target["outcome"] in {"blocked", "preserve"}:
            raise AssertionError(f"resource {resource} is not a captured candidate: {target}")


def assert_restore_defaults(data: dict, machine: str, conflicts: str, convergence: str) -> None:
    actual = (data.get("machine"), data.get("conflicts"), data.get("convergence"))
    if actual != (machine, conflicts, convergence):
        raise AssertionError(f"Restore defaults are {actual}, want {(machine, conflicts, convergence)}")


def assert_check(data: dict, machine: str) -> None:
    if (data.get("machine") or {}).get("name") != machine:
        raise AssertionError(f"check ran for {data.get('machine')}, want {machine}")


def assert_fresh_check(data: dict, machine: str) -> None:
    """A fresh Omarchy install must pass check while reporting unknown package origin (ADR 0022)."""
    assert_check(data, machine)
    if not any("package origin classification unavailable" in note for note in data.get("notes") or []):
        raise AssertionError("check did not report the fresh machine's unavailable package origin")


def assert_restore_plan(data: dict) -> None:
    if data.get("dry_run") is not True or not isinstance((data.get("plan") or {}).get("operations"), list):
        raise AssertionError("expected a Restore dry-run plan with an operations list")


def _toml(profile: Path, name: str) -> dict:
    return tomllib.loads((profile / name).read_text())


def assert_profile(profile: Path, values: dict) -> None:
    profile = Path(profile)
    if values["RA_PACKAGE"] not in (profile / "packages/official.txt").read_text().split():
        raise AssertionError(f"packages do not contain {values['RA_PACKAGE']}")
    if _toml(profile, "themes/themes.toml").get("current") != values["RA_THEME"]:
        raise AssertionError(f"theme is not {values['RA_THEME']}")
    if not any(p.get("id") == values["RA_PLUGIN_ID"] for p in _toml(profile, "plugins/plugins.toml").get("plugin", [])):
        raise AssertionError(f"plugin {values['RA_PLUGIN_ID']} was not captured")
    if not _toml(profile, "shell/shell.toml").get("hash"):
        raise AssertionError("Shell state was not captured")
    if not any(f.get("path") == values["RA_CONFIG_PATH"] for f in _toml(profile, "config/config.toml").get("file", [])):
        raise AssertionError(f"Config {values['RA_CONFIG_PATH']} was not captured")
    hook = values["RA_HOOK_PATH"].removeprefix(".config/omarchy/hooks/")
    if not any(h.get("path") == hook for h in _toml(profile, "hooks/hooks.toml").get("hook", [])):
        raise AssertionError(f"Hook {hook} was not captured")
    if _toml(profile, "defaults/defaults.toml").get("terminal") != values["RA_DEFAULT_TERMINAL"]:
        raise AssertionError(f"terminal default is not {values['RA_DEFAULT_TERMINAL']}")
    resources = {r["id"]: r for r in _toml(profile, "resources/resources.toml").get("resource", [])}
    wanted = {values["RA_HELPER_RESOURCE"]: "~/" + values["RA_HELPER_SOURCE"],
              values["RA_GIT_RESOURCE"]: "~/" + values["RA_GIT_PATH"],
              values["RA_SKIP_RESOURCE"]: "~/" + values["RA_SKIP_PATH"]}
    for resource, path in wanted.items():
        if resources.get(resource, {}).get("path") != path:
            raise AssertionError(f"resource {resource} is not tracked at {path}")
    git = resources[values["RA_GIT_RESOURCE"]]
    if (git.get("strategy"), git.get("remote"), git.get("revision")) != (
            "git", values["RA_GIT_REMOTE"], values["RA_GIT_REVISION"]):
        raise AssertionError(f"Git resource does not pin the fixture remote/revision: {git}")
    target = _toml(profile, "machines/target.toml")
    mapping = {"resource": values["RA_HELPER_RESOURCE"], "path": "~/" + values["RA_HELPER_TARGET"]}
    if mapping not in target.get("resource_path", []):
        raise AssertionError("target helper mapping is missing")
    skip = {"category": "resources", "target": f"resource:{values['RA_SKIP_RESOURCE']}", "setting": "disabled"}
    if skip not in target.get("policy", {}).get("restore", []):
        raise AssertionError("target Restore skip rule is missing")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="check", required=True)
    for name, arguments in {
        "machine-current": ("file", "name", "source"),
        "resource-mapping": ("file", "resource", "portable", "effective"),
        "restore-skip": ("file", "machine", "category", "target"),
        "capture-preview": ("file", "machine"),
        "restore-defaults": ("file", "machine", "conflicts", "convergence"),
        "check": ("file", "machine"),
        "fresh-check": ("file", "machine"),
        "restore-plan": ("file",),
        "profile": ("directory",),
    }.items():
        command = commands.add_parser(name)
        for argument in arguments:
            command.add_argument(argument)
    args = parser.parse_args()
    values = expected()
    if args.check == "machine-current":
        assert_machine_current(load_envelope(args.file, "machine current"), args.name, args.source)
    elif args.check == "resource-mapping":
        assert_resource_mapping(load_envelope(args.file, "tracked"), args.resource, args.portable, args.effective)
    elif args.check == "restore-skip":
        assert_restore_skip(load_envelope(args.file, "policy show"), args.machine, args.category, args.target)
    elif args.check == "capture-preview":
        assert_capture_preview(load_envelope(args.file, "capture preview"), args.machine, values)
    elif args.check == "restore-defaults":
        assert_restore_defaults(load_envelope(args.file, "machine restore-defaults"),
                                args.machine, args.conflicts, args.convergence)
    elif args.check == "fresh-check":
        assert_fresh_check(load_envelope(args.file, "check"), args.machine)
    elif args.check == "restore-plan":
        assert_restore_plan(load_envelope(args.file, "restore"))
    elif args.check == "check":
        assert_check(load_envelope(args.file, "check"), args.machine)
    else:
        assert_profile(Path(args.directory), values)


if __name__ == "__main__":
    main()
