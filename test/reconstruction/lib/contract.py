"""Assertions for Blueprint's public JSON Restore plan (no guest orchestration)."""

import json
from pathlib import Path


def load_envelope(path: Path) -> dict:
    payload = json.loads(path.read_text())
    if not (payload.get("api_version") == 1 and payload.get("command") == "restore"
            and payload.get("ok") is True):
        raise AssertionError("expected successful v1 restore JSON envelope")
    return payload


def restore_plan(envelope: dict) -> dict:
    plan = envelope["data"]["plan"]
    if not isinstance(plan.get("operations"), list):
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
    """Only the canonical official package install elevates, at the terminal (ADR 0022)."""
    install = None
    for operation in plan["operations"]:
        is_install = (operation.get("provider") == "packages" and operation.get("resource") == f"official:{package}"
                      and (operation.get("command") or [])[:3] == ["omarchy", "pkg", "add"])
        if is_install:
            install = operation
        if operation.get("interactive") is True and not (
                is_install and "administrator" in (operation.get("notice") or "")):
            raise AssertionError(f"unexpected interactive operation: {operation}")
    if install is not None and install.get("interactive") is not True:
        raise AssertionError(f"package install must be interactive: {install}")


def assert_provider_present(plan: dict, provider: str) -> None:
    if not any(op.get("provider") == provider for op in plan["operations"]):
        raise AssertionError(f"missing operation from {provider}")


def assert_skip(plan: dict, provider: str, resource_substring: str, reason_substring: str) -> None:
    if not any(skip.get("provider") == provider
               and resource_substring in skip.get("resource", "")
               and reason_substring in skip.get("reason", "") for skip in plan.get("skipped", [])):
        raise AssertionError(f"missing {provider} skip for {resource_substring}: {reason_substring}")


def assert_copy_destination(plan: dict, resource_substring: str, destination: str) -> None:
    if not any(resource_substring in op.get("resource", "")
               and isinstance(op.get("copy"), dict)
               and op["copy"].get("destination") == destination for op in plan["operations"]):
        raise AssertionError(f"missing mapped copy for {resource_substring} at {destination}")


CANONICAL_PROVIDERS = ("packages", "themes", "plugins", "shell", "config", "hooks", "defaults", "resources")


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


def assert_final_convergence(plan: dict, allowed_skip_substring: str) -> None:
    if plan["operations"]:
        raise AssertionError("actionable operations remain after Restore")
    for skip in plan.get("skipped", []):
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
