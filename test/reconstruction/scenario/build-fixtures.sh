#!/usr/bin/env bash
# Sourced by run.sh: deterministic fixture Git remote served to guests over HTTPS.

source "$RA_ROOT/fixtures/expected.env"
RA_GIT_PORT=${RA_GIT_PORT:-9443}
RA_GIT_SERVER_PID=""

ra_build_git_fixture() {
  local work="$RA_WORK/git-build" revision
  rm -rf "$work" "$RA_WORK/git" "$RA_WORK/git-tls"
  mkdir -p "$work" "$RA_WORK/git" "$RA_WORK/git-tls"
  # Fixed identity, dates and no inherited config keep the commit hash pinned.
  (
    export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null
    git init -q -b main "$work/repo"
    git -C "$work/repo" config user.name 'Blueprint RA'
    git -C "$work/repo" config user.email 'ra@example.invalid'
    cp "$RA_ROOT/fixtures/git-resource/README.md" "$work/repo/README.md"
    git -C "$work/repo" add README.md
    GIT_AUTHOR_DATE='2026-09-25T00:00:00Z' GIT_COMMITTER_DATE='2026-09-25T00:00:00Z' \
      git -C "$work/repo" commit -q -m 'fixture: seed reconstruction resource'
  ) || return 1
  revision=$(git -C "$work/repo" rev-parse HEAD)
  if [[ $revision != "$RA_GIT_REVISION" ]]; then
    ra_note "fixture revision $revision does not match pinned $RA_GIT_REVISION"
    return 1
  fi
  git clone -q --bare "$work/repo" "$RA_WORK/git/blueprint-ra.git"
  git -C "$RA_WORK/git/blueprint-ra.git" update-server-info
  (umask 077 && openssl req -x509 -newkey rsa:2048 -nodes -days 2 -subj '/CN=blueprint-ra-git' \
    -addext 'subjectAltName=IP:10.0.2.2,IP:127.0.0.1' \
    -keyout "$RA_WORK/git-tls/key.pem" -out "$RA_WORK/git-tls/cert.pem" 2>/dev/null)
}

ra_start_git_server() {
  local i served=""
  python3 "$RA_ROOT/scenario/serve_git.py" --root "$RA_WORK/git" --port "$RA_GIT_PORT" \
    --cert "$RA_WORK/git-tls/cert.pem" --key "$RA_WORK/git-tls/key.pem" \
    > "$RA_WORK/git-server.log" 2>&1 &
  RA_GIT_SERVER_PID=$!
  ra_cleanup_add ra_kill_if_running "$RA_GIT_SERVER_PID"
  for (( i=0; i<20; i++ )); do
    served=$(git -c http.sslCAInfo="$RA_WORK/git-tls/cert.pem" ls-remote \
      "https://127.0.0.1:$RA_GIT_PORT/blueprint-ra.git" refs/heads/main 2>/dev/null | awk '{print $1}') || true
    [[ -n $served ]] && break
    kill -0 "$RA_GIT_SERVER_PID" 2>/dev/null || break
    sleep 0.5
  done
  if [[ $served != "$RA_GIT_REVISION" ]]; then
    ra_note "fixture Git server did not serve $RA_GIT_REVISION (got '${served}'); see git-server.log"
    return 1
  fi
}

ra_stop_git_server() {
  [[ -n $RA_GIT_SERVER_PID ]] || return 0
  ra_kill_if_running "$RA_GIT_SERVER_PID"
  wait "$RA_GIT_SERVER_PID" 2>/dev/null || true
  RA_GIT_SERVER_PID=""
}
