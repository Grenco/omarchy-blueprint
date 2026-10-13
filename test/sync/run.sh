#!/usr/bin/env bash
set -euo pipefail
repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
sync_tmp=$(mktemp -d /tmp/blueprint-sync-acceptance.XXXXXXXX)
trap 'rm -rf -- "$sync_tmp"' EXIT
cd "$repo_root"
go build -o "$sync_tmp/blueprint" ./cmd/omarchy-blueprint
BLUEPRINT_SYNC_TEST_BINARY="$sync_tmp/blueprint" python3 -m unittest discover -s test/sync -p 'test_*.py' -v
