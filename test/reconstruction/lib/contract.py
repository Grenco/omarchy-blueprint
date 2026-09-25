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


def assert_no_interactive_ops(plan: dict) -> None:
    for operation in plan["operations"]:
        if operation.get("interactive") is True:
            raise AssertionError(f"interactive operation: {operation}")


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


def assert_final_convergence(plan: dict, allowed_skip_substring: str) -> None:
    if plan["operations"]:
        raise AssertionError("actionable operations remain after Restore")
    for skip in plan.get("skipped", []):
        if (allowed_skip_substring not in skip.get("resource", "")
                or "restore disabled" not in skip.get("reason", "")):
            raise AssertionError(f"unexpected final skip: {skip}")
