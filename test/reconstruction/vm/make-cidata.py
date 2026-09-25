#!/usr/bin/env python3
"""Generate the official Omarchy unattended installer inputs (ISO assembled by shell)."""

import argparse
import json
from pathlib import Path
import subprocess


GIB = 1024**3
MIB = 1024**2


def size(value: int) -> dict:
    return {"sector_size": {"unit": "B", "value": 512}, "unit": "B", "value": value}


def build_configuration() -> dict:
    partitions = [
        {"btrfs": [], "dev_path": None, "flags": ["boot", "esp"], "fs_type": "fat32",
         "mount_options": [], "mountpoint": "/boot", "obj_id": "ea21d3f2-82bb-49cc-ab5d-6f81ae94e18d",
         "size": size(2*GIB), "start": size(MIB), "status": "create", "type": "primary"},
        {"btrfs": [{"mountpoint": path, "name": name} for path, name in
                   [("/", "@"), ("/home", "@home"), ("/var/log", "@log"),
                    ("/var/cache/pacman/pkg", "@pkg")]],
         "dev_path": None, "flags": [], "fs_type": "btrfs", "mount_options": ["compress=zstd"],
         "mountpoint": None, "obj_id": "8c2c2b92-1070-455d-b76a-56263bab24aa",
         "size": size(38*GIB-2*MIB), "start": size(2*GIB+MIB), "status": "create", "type": "primary"},
    ]
    return {
        "app_config": None, "archinstall-language": "English", "auth_config": {},
        "audio_config": {"audio": "pipewire"}, "bootloader": "Limine", "custom_commands": [],
        "disk_config": {"btrfs_options": {"snapshot_config": {"type": "Snapper"}},
                        "config_type": "default_layout", "device_modifications": [
                            {"device": "/dev/vda", "partitions": partitions, "wipe": True}]},
        "hostname": "blueprint-ra-base", "network_config": {"type": "iso"},
        "ntp": True, "parallel_downloads": 8, "script": None, "services": [], "swap": True,
        "timezone": "UTC", "locale_config": {"kb_layout": "us", "sys_enc": "UTF-8",
                                            "sys_lang": "en_US.UTF-8"},
        "mirror_config": {"custom_repositories": [], "custom_servers": [
            {"url": "https://mirror.omarchy.org/$repo/os/$arch"},
            {"url": "https://mirror.rackspace.com/archlinux/$repo/os/$arch"},
            {"url": "https://geo.mirror.pkgbuild.com/$repo/os/$arch"}],
            "mirror_regions": {}, "optional_repositories": []},
        "packages": ["base-devel", "git", "omarchy-keyring", "snapper"],
        "profile_config": {"gfx_driver": None, "greeter": None, "profile": {}}, "version": "3.0.9",
    }


def write_cidata(output_dir: Path, ssh_public_key: Path) -> None:
    key = ssh_public_key.read_text()
    if not key.startswith("ssh-") or not key.strip():
        raise ValueError("expected an SSH public key")
    output_dir.mkdir(parents=True, exist_ok=True)
    password_hash = subprocess.check_output(["openssl", "passwd", "-6", "spike"], text=True).strip()
    (output_dir / "user_configuration.json").write_text(json.dumps(build_configuration(), indent=2) + "\n")
    (output_dir / "user_credentials.json").write_text(json.dumps({
        "root_enc_password": password_hash,
        "users": [{"enc_password": password_hash, "groups": [], "sudo": True, "username": "spike"}],
    }, indent=2) + "\n")
    (output_dir / "user_encrypt_installation.txt").write_text("false\n")
    (output_dir / "authorized_keys").write_text(key)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-dir", required=True, type=Path)
    parser.add_argument("--ssh-public-key", required=True, type=Path)
    args = parser.parse_args()
    write_cidata(args.output_dir, args.ssh_public_key)
