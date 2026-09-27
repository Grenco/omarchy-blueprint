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

    def test_console_edit_appends_to_every_boot_entry_once(self):
        with tempfile.TemporaryDirectory() as work:
            conf = Path(work, "limine.conf")
            conf.write_text("timeout: 3\n/+Omarchy\n  //linux\n  cmdline: root=x quiet\n  //fallback\n  cmdline: root=x\n")
            for _ in range(2):  # idempotent
                result = bash(f"eval \"$(ra_boot_console_edit 'console=tty0' '{conf}')\"", work)
                self.assertEqual(result.returncode, 0, result.stderr)
            lines = [line for line in conf.read_text().splitlines() if "cmdline:" in line]
            self.assertEqual(lines, ["  cmdline: root=x quiet console=tty0", "  cmdline: root=x console=tty0"])

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
