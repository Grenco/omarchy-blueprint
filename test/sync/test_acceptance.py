"""Real binary/Git, private Hooks files and controlled Defaults machine effects."""
import json
import fcntl
import os
import pty
import select
from pathlib import Path
import shutil
import subprocess
import struct
import tempfile
import time
import termios
import unittest


class SyncAcceptance(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix="blueprint-sync-hosts-", dir="/tmp")
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.binary = os.environ["BLUEPRINT_SYNC_TEST_BINARY"]
        self.git_binary = shutil.which("git")
        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.global_git = self.root / "gitconfig"
        self.global_git.write_text("[user]\n name = Sync Acceptance\n email = sync@example.invalid\n[core]\n hooksPath = /dev/null\n[init]\n templateDir =\n[gc]\n auto = 0\n[maintenance]\n auto = false\n[protocol \"file\"]\n allow = always\n")
        omarchy = self.bin / "omarchy"
        omarchy.write_text("""#!/bin/sh
case "$*" in
 'version') echo 4.0.0; exit 0;;
 'version channel') echo stable; exit 0;;
esac
case "$HOME" in /tmp/blueprint-sync-hosts-*/A|/tmp/blueprint-sync-hosts-*/B) ;; *) exit 99;; esac
[ "$1" = default ] || exit 99
case "$2" in browser|editor|terminal|agent) ;; *) exit 99;; esac
if [ "$#" = 2 ]; then
 [ ! -f "$HOME/default-$2" ] || cat "$HOME/default-$2"
 exit 0
fi
[ "$#" = 3 ] && [ "$2" != agent ] || exit 99
[ "${FAIL_DEFAULT_RESTORE:-}" != 1 ] || exit 1
case "$3" in firefox|brave|chromium) printf '%s\\n' "$3" > "$HOME/default-$2";; *) exit 99;; esac
""")
        omarchy.chmod(0o700)
        catalogue = self.bin / "omarchy-remove-preinstalls"
        catalogue.write_text("#!/bin/sh\nexit 99\n# read-only catalogue below is never executed\nomarchy-pkg-drop fixture-preinstall\n")
        catalogue.chmod(0o700)
        pacman = self.bin / "pacman"
        pacman.write_text("#!/bin/sh\ncase \"$*\" in\n '-Qq'|'-Qqe'|'-Qqen'|'-Qqem') exit 0;;\n '-Q fixture-preinstall'|'-Q tailscale') exit 1;;\n *) echo forbidden-package-command >&2; exit 99;;\nesac\n")
        pacman.chmod(0o700)
        conf = self.bin / "pacman-conf"
        conf.write_text("#!/bin/sh\ncase \"$*\" in\n '--repo-list') exit 0;;\n 'DBPath') echo \"$HOME/pacmandb\";;\n *) exit 99;;\nesac\n")
        conf.chmod(0o700)
        for name in ["systemctl", "systemd-analyze", "mise", "sudo", "tailscale"]:
            guard = self.bin / name
            guard.write_text("#!/bin/sh\necho forbidden-system-probe >&2\nexit 99\n")
            guard.chmod(0o700)
        self.hosts = {}
        for host in ["A", "B"]:
            home = self.root / host
            home.mkdir()
            env = {"HOME": str(home), "PATH": str(self.bin) + ":/usr/bin:/bin",
                   "XDG_STATE_HOME": str(home / "state"), "XDG_CONFIG_HOME": str(home / ".config"),
                   "XDG_DATA_HOME": str(home / "data"), "XDG_CACHE_HOME": str(home / "cache"),
                   "OMARCHY_PATH": str(home / "runtime"), "GIT_CONFIG_NOSYSTEM": "1",
                   "GIT_CONFIG_GLOBAL": str(self.global_git), "GIT_TERMINAL_PROMPT": "0",
                   "LANG": "C.UTF-8", "OMARCHY_BLUEPRINT_NO_UPDATE_CHECK": "1"}
            self.hosts[host] = (home, home / "profile", env)
            self.hook(host, "p.sh", "initial")
            (home / "default-browser").write_text("firefox\n")
        self.cli("A", "init")
        for machine in ["A", "B"]:
            self.cli("A", "machine", "add", machine, "--no-use")
        self.cli("A", "capture", "hooks")
        self.cli("A", "capture", "packages")
        self.cli("A", "capture", "defaults")
        self.git("A", "init", "-b", "main")
        self.commit("A", "initial")
        self.origin = self.root / "origin.git"
        self.git_raw("init", "--bare", str(self.origin))
        self.git("A", "remote", "add", "origin", str(self.origin))
        self.git("A", "push", "-u", "origin", "main")
        self.git_raw("clone", "-b", "main", str(self.origin), str(self.hosts["B"][1]))
        self.initial = self.head("A")
        for host in ["A", "B"]:
            preview = self.cli(host, "sync", "bootstrap", "inspect", "--no-fetch")["data"]["sync"]
            self.assertFalse(preview.get("blockers"), preview)
            self.assertTrue(preview["convergence_eligible"], preview["items"])
            result = self.cli(host, "sync", "bootstrap", "reconcile-upstream", "--yes", "--no-fetch")
            self.assertTrue(result["data"]["execution"]["cursor_advanced"])
            self.assertEqual(self.cursor(host), self.initial)

    def invoke(self, argv, env, want=0):
        result = subprocess.run(argv, env=env, text=True, capture_output=True, timeout=30)
        self.assertEqual(result.returncode, want, result.stdout + result.stderr)
        return result.stdout

    def cli(self, host, *args, want=0):
        _, profile, env = self.hosts[host]
        machine_args = [] if args[0] == "init" or args[:2] == ("machine", "add") else ["--machine", host]
        output = self.invoke([self.binary, "--profile", str(profile), *machine_args, "--json", *args], env, want)
        return json.loads(output) if output.strip() else None

    def git_raw(self, *args):
        return self.invoke([self.git_binary, *args], self.hosts["A"][2]).strip()

    def git(self, host, *args):
        return self.invoke([self.git_binary, "-C", str(self.hosts[host][1]), *args], self.hosts[host][2]).strip()

    def head(self, host):
        return self.git(host, "rev-parse", "HEAD")

    def commit(self, host, message):
        self.git(host, "add", ".")
        self.git(host, "commit", "-m", message)

    def hook(self, host, target, value):
        home = self.root / host
        path = home / ".config/omarchy/hooks" / target
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("#!/bin/sh\n# fixture: " + value + "\n")
        path.chmod(0o700)
        return path

    def cursor(self, host):
        files = list((self.hosts[host][0] / "state").rglob("sync.toml"))
        self.assertEqual(len(files), 1)
        # Cursor's only persisted revision field; no TOML package dependency.
        for line in files[0].read_text().splitlines():
            if line.startswith("revision ="):
                return line.partition("=")[2].strip().strip("'\"")
        self.fail("cursor lacks revision")

    def inspect(self, host):
        before = self.checkout_snapshot(host), self.cursor(host)
        result = self.cli(host, "sync", "inspect")["data"]["sync"]
        self.assertEqual(before, (self.checkout_snapshot(host), self.cursor(host)))
        return result

    def checkout_snapshot(self, host):
        profile = self.hosts[host][1]
        files = {}
        for path in profile.rglob("*"):
            relative = path.relative_to(profile)
            if ".git" in relative.parts:
                continue
            if path.is_symlink():
                files[str(relative)] = ("link", os.readlink(path))
            elif path.is_file():
                files[str(relative)] = (path.stat().st_mode, path.read_bytes())
        return self.head(host), self.git(host, "ls-files", "--stage", "-z"), files

    def apply(self, host, *choices, want=0):
        return self.cli(host, "sync", "apply", "--yes", "--no-fetch", "--force", *choices, want=want)

    def publish_live(self, host, target, value):
        self.hook(host, target, value)
        plan = self.inspect(host)
        choices = []
        if any(i["key"] == {"category": "hooks", "target": target} and i["review"] == "machine" for i in plan["items"]):
            choices = ["--keep-machine", "hooks:" + target]
        self.apply(host, *choices)
        return self.head(host)

    def test_remote_follow_and_local_capture(self):
        revision = self.publish_live("A", "new-p.sh", "new P")
        plan = self.inspect("B")
        self.assertTrue(any(i["history"] == "remote" for i in plan["items"]))
        before = self.head("B")
        self.assertEqual(before, self.initial)
        self.apply("B")
        self.assertEqual(self.cursor("A"), revision)
        self.assertEqual(self.cursor("B"), revision)
        self.assertIn("new P", (self.hosts["B"][0] / ".config/omarchy/hooks/new-p.sh").read_text())

    def test_tui_pty_smoke_is_lazy_and_inspection_only(self):
        home, profile, env = self.hosts["A"]
        before = self.head("A"), self.git("A", "status", "--porcelain=v1"), self.cursor("A")
        master, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 140, 0, 0))
        process = subprocess.Popen([self.binary, "--profile", str(profile), "--machine", "A"],
                                   env={**env, "TERM": "xterm-256color", "NO_COLOR": "1"},
                                   stdin=slave, stdout=slave, stderr=slave, start_new_session=True)
        os.close(slave)
        output = bytearray()

        def until(needle):
            deadline = time.monotonic() + 8
            while needle not in output and time.monotonic() < deadline:
                if select.select([master], [], [], 0.1)[0]:
                    try:
                        output.extend(os.read(master, 65536))
                    except OSError:
                        break
            self.assertIn(needle, output, bytes(output[-2000:]))

        try:
            until(b"Overview")
            os.write(master, b":sync\r")
            until(b"semantic reconciliation")
            until(b"Upstream")
            self.assertEqual(before, (self.head("A"), self.git("A", "status", "--porcelain=v1"), self.cursor("A")))
            os.write(master, b":quit\r")
            process.wait(timeout=5)
            self.assertEqual(process.returncode, 0)
        finally:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=2)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=2)
            os.close(master)

    def divergent(self, target_a="p.sh", target_b="p.sh"):
        if target_a == target_b == "p.sh":
            (self.hosts["A"][0] / "default-browser").write_text("brave\n")
            self.cli("A", "capture", "defaults")
            self.commit("A", "local browser intent")
            local = self.head("A")
            (self.hosts["B"][0] / "default-browser").write_text("chromium\n")
            self.inspect("B")
            self.apply("B", "--keep-machine", "defaults:browser")
            return local, self.head("B"), self.inspect("A")
        self.hook("A", target_a, "local A")
        self.cli("A", "capture", "hooks")
        self.commit("A", "local A")
        local = self.head("A")
        remote = self.publish_live("B", target_b, "remote B")
        return local, remote, self.inspect("A")

    def test_conflict_keep_local_two_parent_and_peer_follow(self):
        local, remote, plan = self.divergent()
        self.assertTrue(any(i["history"] == "conflict" for i in plan["items"]))
        self.apply("A", "--keep-local", "defaults:browser")
        self.assertEqual(self.git("A", "show", "-s", "--format=%P", "HEAD"), local + " " + remote)
        self.inspect("B")
        self.apply("B")
        self.assertEqual(self.cursor("A"), self.cursor("B"))
        self.assertEqual((self.hosts["B"][0] / "default-browser").read_text().strip(), "brave")

    def test_disjoint_changes_preserve_both_intents(self):
        local, remote, _ = self.divergent("local.sh", "remote.sh")
        self.apply("A")
        self.assertEqual(self.git("A", "show", "-s", "--format=%P", "HEAD"), local + " " + remote)
        self.inspect("B")
        self.apply("B")
        for host in ["A", "B"]:
            self.assertTrue((self.hosts[host][0] / ".config/omarchy/hooks/local.sh").exists())
            self.assertTrue((self.hosts[host][0] / ".config/omarchy/hooks/remote.sh").exists())

    def test_deferred_conflict_keeps_cursor_and_checkout(self):
        local, _, _ = self.divergent()
        self.apply("A", "--defer", "defaults:browser", want=1)
        self.assertEqual(self.cursor("A"), self.initial)
        self.assertEqual(self.head("A"), local)

    def test_dirty_checkout_blocks(self):
        (self.hosts["A"][1] / "private-note").write_text("dirty")
        plan = self.inspect("A")
        self.assertTrue(plan["blockers"])
        self.apply("A", want=1)
        self.assertEqual(self.cursor("A"), self.initial)

    def test_divergent_nonmanaged_history_blocks(self):
        (self.hosts["A"][1] / "notes.txt").write_text("unmanaged committed path")
        self.commit("A", "nonmanaged")
        self.publish_live("B", "p.sh", "remote")
        plan = self.inspect("A")
        self.assertIn("non-managed-history", [b["code"] for b in plan["blockers"]])
        self.apply("A", want=1)
        self.assertEqual(self.cursor("A"), self.initial)

    def test_remote_identity_switch_invalidates_cursor(self):
        replacement = self.root / "replacement.git"
        self.git_raw("clone", "--bare", str(self.origin), str(replacement))
        self.git("A", "remote", "set-url", "origin", str(replacement))
        self.assertTrue(self.inspect("A")["blockers"])
        self.apply("A", want=1)
        self.assertEqual(self.cursor("A"), self.initial)

    def test_upstream_moves_after_review_no_unreviewed_restore(self):
        self.publish_live("B", "remote-one.sh", "reviewed")
        reviewed = self.inspect("A")
        self.publish_live("B", "remote-two.sh", "not reviewed")
        self.assertNotEqual(reviewed["identity"]["remote"], self.head("B"))
        self.apply("A", want=1)
        self.assertEqual(self.cursor("A"), self.initial)
        for target in ["remote-one.sh", "remote-two.sh"]:
            self.assertFalse((self.hosts["A"][0] / ".config/omarchy/hooks" / target).exists())

    def test_restore_failure_after_publication_keeps_cursor_and_drift(self):
        (self.hosts["A"][0] / "default-browser").write_text("brave\n")
        self.inspect("A")
        self.apply("A", "--keep-machine", "defaults:browser")
        remote = self.head("A")
        self.inspect("B")
        self.hosts["B"][2]["FAIL_DEFAULT_RESTORE"] = "1"
        failed = self.apply("B", want=1)
        self.assertTrue(failed["data"]["execution"]["published"])
        self.assertFalse(failed["data"]["execution"]["cursor_advanced"])
        self.assertEqual(self.head("B"), remote)
        self.assertEqual(self.cursor("B"), self.initial)
        self.assertEqual((self.hosts["B"][0] / "default-browser").read_text().strip(), "firefox")
        plan = self.inspect("B")
        browser = next(i for i in plan["items"] if i["key"] == {"category": "defaults", "target": "browser"})
        self.assertNotEqual(browser["desired"]["fingerprint"], browser["machine"]["fingerprint"])
        self.assertFalse(browser["settled"])

    def test_machine_and_upstream_switches_invalidate_cursor(self):
        plan = self.cli("A", "--machine", "B", "sync", "inspect")["data"]["sync"]
        self.assertIn("cursor-identity", [b["code"] for b in plan["blockers"]])
        self.git("A", "push", "origin", "HEAD:other")
        self.git("A", "fetch")
        self.git("A", "branch", "--set-upstream-to=origin/other")
        self.assertIn("cursor-identity", [b["code"] for b in self.inspect("A")["blockers"]])
        self.apply("A", want=1)
        self.assertEqual(self.cursor("A"), self.initial)


if __name__ == "__main__":
    unittest.main()
