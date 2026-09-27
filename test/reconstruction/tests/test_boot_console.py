"""Canonical guests boot with the normalized CI console: explicit tty0, no serial kernel console."""

import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
WORKFLOWS = ROOT.parents[1] / ".github/workflows"

# Booted command lines measured by Reconstruction Boot Soak (PR #47).
INSTALLER = ("root=PARTUUID=x zswap.enabled=0 rootflags=subvol=@ rw rootfstype=btrfs  resume=/dev/vda2 "
             "resume_offset=1 initramfs_async=0 quiet splash loglevel=0 systemd.show_status=false "
             "rd.udev.log_level=0 vt.global_cursor_default=0 console=uart,io,0x3f8 console=tty0")
TTY0 = INSTALLER.replace("console=uart,io,0x3f8 console=tty0", "console=tty0")
TTYS0 = INSTALLER.replace("console=uart,io,0x3f8 console=tty0", "console=ttyS0,115200 console=tty0")


def bash(script: str, work: str = "", **env) -> subprocess.CompletedProcess:
    work = work or tempfile.mkdtemp()
    return subprocess.run(["bash", "-c", f"source '{ROOT}/lib/common.sh'; {script}"], capture_output=True,
                          text=True, timeout=30, env={**os.environ, "RA_WORK": work, **env})


class CanonicalBootImageTests(unittest.TestCase):
    def test_production_resolves_to_the_canonical_image_with_explicit_tty0(self):
        result = bash('image=$(ra_resolve_boot_image) && echo "$image" && ra_boot_console_args "$image"')
        image, consoles = result.stdout.split("\n")[:2]
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(image, "canonical")
        self.assertEqual(consoles, "console=tty0")

    def test_diagnostic_images_are_distinct_and_the_installer_control_is_untouched(self):
        for image, consoles in (("installer", ""), ("ttys0-console", "console=ttyS0,115200 console=tty0")):
            with self.subTest(image=image):
                result = bash(f"ra_resolve_boot_image {image} >/dev/null && ra_boot_console_args {image}")
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(result.stdout.strip(), consoles)
        self.assertNotEqual(bash("ra_resolve_boot_image tty0-typo").returncode, 0)

    def test_durable_normalization_uses_the_limine_entry_tool_and_verifies_every_entry(self):
        with tempfile.TemporaryDirectory() as work:
            conf, dropin, bin_dir = Path(work, "limine.conf"), Path(work, "entry-tool.d/zz-ra.conf"), Path(work, "bin")
            bin_dir.mkdir()
            # Stands in for limine-update: regenerates every entry from the drop-ins.
            Path(bin_dir, "limine-update").write_text(f"""#!/bin/bash
declare -A KERNEL_CMDLINE=([default]="root=x quiet")
for f in {dropin.parent}/*.conf; do source "$f"; done
printf 'timeout: 3\\n/+Omarchy\\n  //linux\\n  cmdline: %s\\n  //fallback\\n  cmdline: %s\\n' \\
  "${{KERNEL_CMDLINE[default]}}" "${{KERNEL_CMDLINE[default]}}" > {conf}
""")
            Path(bin_dir, "limine-update").chmod(0o755)
            command = bash(f"ra_boot_console_normalize_command 'console=tty0' '{conf}' '{dropin}'", work).stdout
            self.assertNotIn("sed", command)
            run = subprocess.run(["bash", "-c", command], capture_output=True, text=True,
                                 env={**os.environ, "PATH": f"{bin_dir}:{os.environ['PATH']}"})
            self.assertEqual(run.returncode, 0, run.stdout + run.stderr)
            self.assertEqual(dropin.read_text(), 'KERNEL_CMDLINE[default]+=" console=tty0"\n')
            lines = [line.strip() for line in conf.read_text().splitlines() if "cmdline:" in line]
            self.assertEqual(lines, ["cmdline: root=x quiet console=tty0"] * 2)

    def test_durable_normalization_fails_when_regeneration_drops_the_console(self):
        with tempfile.TemporaryDirectory() as work:
            conf, bin_dir = Path(work, "limine.conf"), Path(work, "bin")
            bin_dir.mkdir()
            Path(bin_dir, "limine-update").write_text(f"#!/bin/bash\nprintf '  cmdline: root=x quiet\\n' > {conf}\n")
            Path(bin_dir, "limine-update").chmod(0o755)
            command = bash(f"ra_boot_console_normalize_command 'console=tty0' '{conf}' '{work}/d/zz.conf'", work).stdout
            run = subprocess.run(["bash", "-c", command], capture_output=True, text=True,
                                 env={**os.environ, "PATH": f"{bin_dir}:{os.environ['PATH']}"})
            self.assertNotEqual(run.returncode, 0)

    def test_canonical_normalization_targets_omarchys_entry_tool_directory(self):
        command = bash("ra_boot_console_normalize_command console=tty0").stdout
        self.assertIn("/etc/limine-entry-tool.d/", command)
        self.assertIn("KERNEL_CMDLINE", command)
        self.assertIn("limine-update", command)

    def test_cmdline_contract_requires_tty0_and_rejects_serial_kernel_consoles(self):
        self.assertEqual(bash(f"ra_check_canonical_cmdline '{TTY0}'").returncode, 0)
        for cmdline, reason in ((INSTALLER, "console=uart"), (TTYS0, "console=ttyS"),
                                (TTY0.replace(" console=tty0", ""), "console=tty0 is missing")):
            with self.subTest(reason=reason):
                result = bash(f"ra_check_canonical_cmdline '{cmdline}'")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(reason, result.stdout)

    def test_guest_outside_the_contract_fails_infrastructure_with_its_cmdline_kept(self):
        for cmdline, ok in ((TTY0, True), (INSTALLER, False)):
            with self.subTest(ok=ok), tempfile.TemporaryDirectory() as work:
                Path(work, "artifacts/source").mkdir(parents=True)
                result = bash(f"""trap 'ra_finish "$?"' EXIT
                    source '{ROOT}/vm/guest.sh'
                    ra_guest_exec() {{ echo '{cmdline}'; }}
                    ra_phase OMARCHY_INSTALL
                    ra_guest_require_canonical_boot source
                    ra_pass OMARCHY_INSTALL""", work)
                summary = Path(work, "artifacts/summary.txt").read_text()
                self.assertEqual(Path(work, "artifacts/source/proc-cmdline.txt").read_text().strip(), cmdline)
                if ok:
                    self.assertEqual(result.returncode, 0, summary)
                else:
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn("phase: INFRASTRUCTURE", summary)
                    self.assertIn("console=uart", summary)


def base_first_boot(work: str, image: str, cmdline: str) -> tuple[subprocess.CompletedProcess, str]:
    Path(work, "artifacts/base").mkdir(parents=True, exist_ok=True)
    result = bash(f"""trap 'ra_finish "$?"' EXIT
        source '{ROOT}/vm/install-omarchy.sh'
        ra_ssh() {{ echo '{cmdline}'; }}
        ra_phase OMARCHY_INSTALL
        ra_base_first_disk_boot {image}
        ra_pass OMARCHY_INSTALL""", work)
    return result, Path(work, "artifacts/summary.txt").read_text()


NO_CONSOLE = TTY0.replace(" console=tty0", "")


class BaseBuilderTopologyTests(unittest.TestCase):
    def test_canonical_base_builder_has_no_serial_device(self):
        result = bash("ra_base_serial_args canonical /x/serial.log")
        self.assertEqual(result.stdout.split(), ["-serial", "none"])
        install = (ROOT / "vm/install-omarchy.sh").read_text()
        self.assertEqual(re.findall(r"\s-serial\s", install), [])  # only through ra_base_serial_args
        self.assertIn('"${serial[@]}"', install)

    def test_diagnostic_base_builders_keep_the_original_serial_device(self):
        for image in ("installer", "ttys0-console", "rebuilt", "debug"):
            with self.subTest(image=image):
                self.assertEqual(bash(f"ra_base_serial_args {image} /x/serial.log").stdout.split(),
                                 ["-serial", "file:/x/serial.log"])
        self.assertNotEqual(bash("ra_base_serial_args typo /x").returncode, 0)

    def test_overlay_guests_restore_the_serial_device(self):
        start = (ROOT / "vm/guest.sh").read_text().split("ra_guest_start() {")[1].split("\n}\n")[0]
        self.assertIn("-serial chardev:serial0", start)
        self.assertIn("serial.log", start)

    def test_first_disk_boot_accepts_no_console_yet_and_rejects_serial_consoles(self):
        for cmdline, ok in ((NO_CONSOLE, True), (TTY0, True), (INSTALLER, False), (TTYS0, False)):
            with self.subTest(cmdline=cmdline[-40:]), tempfile.TemporaryDirectory() as work:
                result, summary = base_first_boot(work, "canonical", cmdline)
                self.assertEqual(Path(work, "artifacts/base/first-disk-boot-cmdline.txt").read_text().strip(), cmdline)
                self.assertIn("-serial none", summary)
                if ok:
                    self.assertEqual(result.returncode, 0, summary)
                else:
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn("phase: INFRASTRUCTURE", summary)

    def test_installer_control_first_disk_boot_is_recorded_not_enforced(self):
        with tempfile.TemporaryDirectory() as work:
            result, summary = base_first_boot(work, "installer", INSTALLER)
            self.assertEqual(result.returncode, 0, summary)
            self.assertIn("console=uart", Path(work, "artifacts/base/first-disk-boot-cmdline.txt").read_text())

    def test_base_timeout_diagnostics_expect_no_serial_log_for_the_canonical_builder(self):
        for image, expected in (("canonical", "no serial device"), ("installer", "firmware line")):
            with self.subTest(image=image), tempfile.TemporaryDirectory() as work:
                Path(work, "artifacts/base").mkdir(parents=True)
                if image == "installer":
                    Path(work, "artifacts/base/omarchy-serial.log").write_text("firmware line\n")
                result = bash(f"""source '{ROOT}/vm/install-omarchy.sh'
                    ra_screendump() {{ :; }}; ra_monitor() {{ :; }}; ra_qemu_activity() {{ :; }}
                    set -e; ra_base_timeout_diagnostics {image} 1""", work)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn(expected, Path(work, "artifacts/base/boot-timeout-serial-tail.txt").read_text())

    def test_overlay_boot_records_the_serial_log_and_requires_tty0(self):
        with tempfile.TemporaryDirectory() as work:
            Path(work, "artifacts/target").mkdir(parents=True)
            Path(work, "artifacts/target/serial.log").write_text("BdsDxe: loading Boot0004\nBdsDxe: starting\n")
            result = bash(f"""trap 'ra_finish "$?"' EXIT
                source '{ROOT}/vm/guest.sh'
                ra_guest_exec() {{ echo '{TTY0}'; }}
                ra_phase TARGET_PREFLIGHT
                ra_guest_require_canonical_boot target post-update
                ra_pass TARGET_PREFLIGHT""", work)
            summary = Path(work, "artifacts/summary.txt").read_text()
            self.assertEqual(result.returncode, 0, summary)
            self.assertIn("2 firmware boot manager lines", summary)
            self.assertTrue(Path(work, "artifacts/target/proc-cmdline-post-update.txt").exists())


class WorkflowModeTests(unittest.TestCase):
    def test_boot_soak_keeps_the_installer_control_beside_the_canonical_image(self):
        workflow = (WORKFLOWS / "reconstruction-boot-soak.yml").read_text()
        options = re.search(r"boot_image:.*?options: \[([^\]]*)\]", workflow, re.S).group(1)
        images = [option.strip() for option in options.split(",")]
        self.assertIn("installer", images)
        self.assertIn("canonical", images)
        for image in images:
            with self.subTest(image=image):
                self.assertEqual(bash(f"ra_resolve_boot_image {image}").returncode, 0)

    def test_substrate_record_survives_missing_metadata(self):
        with tempfile.TemporaryDirectory() as work:
            Path(work, "artifacts/host").mkdir(parents=True)
            result = bash("ra_record_substrate; cat \"$RA_ARTIFACTS/host/substrate.txt\"", work,
                          ImageOS="ubuntu24", ImageVersion="20260920.1", RA_IMDS_URL="http://127.0.0.1:9/none")
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn("runner_image=ubuntu24 20260920.1", result.stdout)
            self.assertIn("azure=unavailable", result.stdout)


if __name__ == "__main__":
    unittest.main()
