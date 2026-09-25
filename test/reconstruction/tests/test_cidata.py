"""Omarchy unattended config must leave hardware/kernel policy to Omarchy."""

import importlib.util
from pathlib import Path
import tempfile
import unittest


PATH = Path(__file__).resolve().parents[1] / "vm/make-cidata.py"
spec = importlib.util.spec_from_file_location("make_cidata", PATH)
make_cidata = importlib.util.module_from_spec(spec)
spec.loader.exec_module(make_cidata)


class CidataTests(unittest.TestCase):
    def test_supported_default_and_btrfs_layout(self):
        config = make_cidata.build_configuration()
        self.assertNotIn("kernels", config)
        self.assertEqual(config["hostname"], "blueprint-ra-base")
        self.assertEqual(config["bootloader"], "Limine")
        self.assertEqual(config["timezone"], "UTC")
        self.assertEqual(config["version"], "3.0.9")
        self.assertEqual(config["disk_config"]["device_modifications"][0]["device"], "/dev/vda")
        partitions = config["disk_config"]["device_modifications"][0]["partitions"]
        self.assertEqual(partitions[0]["size"]["value"], 2 * 1024**3)
        self.assertEqual(partitions[1]["size"]["value"], 38 * 1024**3 - 2 * 1024**2)
        self.assertEqual([(item["mountpoint"], item["name"]) for item in partitions[1]["btrfs"]],
                         [("/", "@"), ("/home", "@home"), ("/var/log", "@log"),
                          ("/var/cache/pacman/pkg", "@pkg")])
        self.assertEqual(config["packages"], ["base-devel", "git", "omarchy-keyring", "snapper"])
        self.assertEqual([item["url"] for item in config["mirror_config"]["custom_servers"]], [
            "https://mirror.omarchy.org/$repo/os/$arch",
            "https://mirror.rackspace.com/archlinux/$repo/os/$arch",
            "https://geo.mirror.pkgbuild.com/$repo/os/$arch",
        ])

    def test_cidata_files_contain_key_without_kernel_override(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            key = root / "key.pub"
            key.write_text("ssh-ed25519 fixture-key\n")
            make_cidata.write_cidata(root / "out", key)
            import json
            self.assertNotIn("kernels", json.loads((root / "out/user_configuration.json").read_text()))
            self.assertEqual((root / "out/authorized_keys").read_text(), key.read_text())
            self.assertEqual((root / "out/user_encrypt_installation.txt").read_text(), "false\n")


if __name__ == "__main__":
    unittest.main()
