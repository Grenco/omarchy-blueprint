#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/lib/common.sh"
trap 'ra_finish "$?"' EXIT

mkdir -p "$RA_ARTIFACTS"/{host,base,source,profile,target}

# PR 1 stops after substrate/overlay validation; product phases arrive later.
ra_phase INFRASTRUCTURE
ra_note "workspace: $RA_WORK"
