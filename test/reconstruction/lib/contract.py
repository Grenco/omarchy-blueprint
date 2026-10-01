"""Assertions for Blueprint's public JSON Restore plan (no guest orchestration)."""

import json
import hashlib
from pathlib import Path


def load_envelope(path: Path) -> dict:
    payload = json.loads(path.read_text())
    if not (payload.get("api_version") == 1 and payload.get("command") == "restore"
            and payload.get("ok") is True):
        raise AssertionError("expected successful v1 restore JSON envelope")
    return payload


def restore_plan(envelope: dict) -> dict:
    plan = envelope["data"]["plan"]
    if plan.get("operations") is None:  # Blueprint encodes an empty plan as null.
        plan["operations"] = []
    if not isinstance(plan["operations"], list):
        raise AssertionError("restore plan has no operations list")
    return plan


def assert_no_destructive_ops(plan: dict) -> None:
    for operation in plan["operations"]:
        if (operation.get("action") in {"remove", "delete"} or operation.get("delete") is not None
                or any(isinstance(operation.get(key), dict)
                       and operation[key].get("replace_existing") is True
                       for key in ("file", "copy", "symlink"))):
            raise AssertionError(f"destructive operation: {operation}")


def assert_expected_interactive_ops(plan: dict, package: str) -> None:
    """Exactly one elevated step: the canonical package install, at the terminal (ADR 0022).

    Requiring it keeps the gate exercising real sudo authentication; any
    other interactive operation is unexpected.
    """
    installs = []
    for operation in plan["operations"]:
        is_install = (operation.get("provider") == "packages" and operation.get("resource") == f"official:{package}"
                      and (operation.get("command") or [])[:3] == ["omarchy", "pkg", "add"])
        if is_install:
            installs.append(operation)
        elif operation.get("interactive") is True:
            raise AssertionError(f"unexpected interactive operation: {operation}")
    if len(installs) != 1:
        raise AssertionError(f"expected exactly one interactive omarchy pkg add for {package}, found {len(installs)}")
    install = installs[0]
    if package not in install["command"][3:]:
        raise AssertionError(f"canonical install does not add {package}: {install}")
    if install.get("interactive") is not True:
        raise AssertionError(f"package install must be interactive: {install}")
    if "administrator authentication" not in (install.get("notice") or ""):
        raise AssertionError(f"package install must explain administrator authentication: {install}")


def assert_provider_present(plan: dict, provider: str) -> None:
    if not any(op.get("provider") == provider for op in plan["operations"]):
        raise AssertionError(f"missing operation from {provider}")


def assert_skip(plan: dict, provider: str, resource_substring: str, reason_substring: str) -> None:
    if not any(skip.get("provider") == provider
               and resource_substring in skip.get("resource", "")
               and reason_substring in skip.get("reason", "") for skip in plan.get("skipped", [])):
        raise AssertionError(f"missing {provider} skip for {resource_substring}: {reason_substring}")


def assert_copy_destination(plan: dict, resource_substring: str, destination: str) -> None:
    """A Resource is restored to destination, as a directory copy or a file write."""
    if not any(resource_substring in op.get("resource", "")
               and any(isinstance(op.get(kind), dict) and op[kind].get("destination") == destination
                       for kind in ("copy", "file")) for op in plan["operations"]):
        raise AssertionError(f"missing mapped copy for {resource_substring} at {destination}")


CANONICAL_PROVIDERS = ("packages", "themes", "plugins", "shell", "config", "hooks", "defaults", "resources", "services")


def assert_services_plan(plan: dict) -> None:
    root = "/home/spike/.config/systemd/user/"
    fixture = Path(__file__).resolve().parents[1] / "fixtures/services"
    expected_files = {
        "blueprint-ra-marker.service": fixture / "blueprint-ra-marker.service",
        "blueprint-ra-marker.timer": fixture / "blueprint-ra-marker.timer",
        "blueprint-ra-marker.service.d/10-ra.conf": fixture / "10-ra.conf",
    }
    operations = [op for op in plan["operations"] if op.get("provider") == "services"]
    writes = [op["file"] for op in operations if op.get("file") is not None]
    if len(writes) != 3 or {f.get("destination") for f in writes} != {root + name for name in expected_files}:
        raise AssertionError("Services plan does not create exactly the reviewed fixture artifacts")
    for write in writes:
        name = write["destination"].removeprefix(root)
        if not write.get("expected_missing") or not write.get("reject_symlink_parents"):
            raise AssertionError("Services write lacks fresh-target filesystem guards")
        if write.get("source_hash") != hashlib.sha256(expected_files[name].read_bytes()).hexdigest():
            raise AssertionError("Services write source differs from the canonical fixture")
        if name.endswith(".conf") and write.get("mode") != 0o640:
            raise AssertionError("Services drop-in mode provenance was lost")
    expected_enable = ["systemctl", "--user", "enable", "--no-reload", "--", "blueprint-ra-marker.timer"]
    commands = [op.get("command") for op in operations if op.get("command")]
    if commands.count(expected_enable) != 1:
        raise AssertionError("persistent timer enablement is absent or duplicated")
    if any(command not in (expected_enable, ["systemctl", "--user", "daemon-reload"]) for command in commands):
        raise AssertionError("canonical Services Restore gained unexpected state or activation authority")
    categories = [c for c in plan.get("compatibility", {}).get("categories", []) if c.get("category") == "services"]
    if len(categories) != 1 or (categories[0].get("state"), categories[0].get("authority"), categories[0].get("applies")) != ("supported", "unchanged", True):
        raise AssertionError("Services reconstruction lacks Supported, applicable compatibility evidence")


def assert_canonical_plan(plan: dict, package: str) -> None:
    """The post-readiness Safe + Additive plan that the user approves on Machine B."""
    if plan.get("requirements"):
        raise AssertionError(f"readiness requirements remain after omarchy update: {plan['requirements']}")
    for provider in CANONICAL_PROVIDERS:
        assert_provider_present(plan, provider)
    assert_no_destructive_ops(plan)
    assert_expected_interactive_ops(plan, package)
    for operation in plan["operations"]:
        command = operation.get("command") or []
        if any(arg.startswith("-Sy") for arg in command) or command[:2] == ["omarchy", "update"]:
            raise AssertionError(f"Blueprint planned its own package metadata sync: {operation}")
    assert_copy_destination(plan, "helper-script", "/home/spike/bin/blueprint-ra-helper")
    assert_skip(plan, "resources", "target-only-skip", "restore disabled")
    assert_services_plan(plan)


def assert_final_convergence(plan: dict, allowed_skip_substring: str) -> None:
    if plan["operations"]:
        raise AssertionError("actionable operations remain after Restore")
    for skip in plan.get("skipped", []):
        if (skip.get("provider") == "services" and skip.get("resource") == "blueprint-ra-marker.timer"
                and skip.get("reason") == "Will not start during Restore: target preference is Persistent state only."):
            continue
        if (allowed_skip_substring not in skip.get("resource", "")
                or "restore disabled" not in skip.get("reason", "")):
            raise AssertionError(f"unexpected final skip: {skip}")


def main() -> None:
    import sys
    if len(sys.argv) != 3 or sys.argv[1] not in ("assert-canonical-plan", "assert-final-plan"):
        raise SystemExit("usage: contract.py assert-canonical-plan|assert-final-plan <restore.json>")
    plan = restore_plan(load_envelope(Path(sys.argv[2])))
    if sys.argv[1] == "assert-canonical-plan":
        expected = dict(line.split("=", 1) for line in
                        (Path(__file__).resolve().parents[1] / "fixtures/expected.env").read_text().splitlines() if line)
        assert_canonical_plan(plan, expected["RA_PACKAGE"])
    else:
        assert_final_convergence(plan, "target-only-skip")


if __name__ == "__main__":
    main()
