#!/usr/bin/python3 -IB
"""Insert the reviewed firmware filter into a fresh copy of update-kernel.

The installed host script is never modified. Both the copied script and the
required filter receipt are checked after image assembly.
"""
import argparse
from contextlib import contextmanager
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import sys

UPSTREAM = {"size": 9942, "sha256": "f52d710314316c789e65080051a2d98fe1aced4e9082be9abffa2c20c9194ec2"}
CONTEXT = b'\tcp -RP "$ROOTFS"/lib/firmware/intel/sof* "$MODLOOP"/modules/firmware/intel/\nfi\n\ncase $ARCH in\n'
HELPER = "scripts/build/filter_alpine_firmware.py"
POLICY = "platform/alpine/firmware-exclusions.json"
ARCHES = {"x86", "x86_64", "aarch64"}


def require(condition):
    if not condition:
        raise ValueError("unreviewed or changed firmware build input")


def record(data):
    return {"size": len(data), "sha256": hashlib.sha256(data).hexdigest()}


def strict_json(data):
    def pairs(items):
        result = {}
        for name, value in items:
            require(name not in result)
            result[name] = value
        return result
    return json.loads(data, object_pairs_hook=pairs, parse_constant=lambda _: require(False))


def safe_path(path):
    text = str(path)
    require(path.is_absolute() and re.fullmatch(r"/[A-Za-z0-9_./-]+", text) and
            all(part not in {".", ".."} for part in text.split("/")[1:]))
    return text


@contextmanager
def directory(path):
    safe_path(path)
    descriptor = os.open("/", os.O_RDONLY | os.O_DIRECTORY)
    try:
        for part in path.parts[1:]:
            child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=descriptor)
            os.close(descriptor)
            descriptor = child
        info = os.fstat(descriptor)
        require(info.st_uid in {0, os.geteuid()} and not info.st_mode & 0o022)
        yield descriptor
    finally:
        os.close(descriptor)


def read_file(path, maximum=1 << 20):
    with directory(path.parent) as parent:
        pinned = os.open(path.name, os.O_PATH | os.O_NOFOLLOW, dir_fd=parent)
        opened = None
        try:
            before = os.fstat(pinned)
            require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and
                    0 < before.st_size <= maximum and before.st_uid in {0, os.geteuid()} and
                    not before.st_mode & 0o022)
            identity = lambda value: (value.st_dev, value.st_ino, value.st_mode, value.st_nlink,
                                      value.st_size, value.st_mtime_ns, value.st_ctime_ns)
            opened = os.open("/proc/self/fd/" + str(pinned), os.O_RDONLY | os.O_NOATIME)
            require(identity(os.fstat(opened)) == identity(before))
            data = bytearray()
            while len(data) < before.st_size:
                chunk = os.read(opened, min(65536, before.st_size - len(data)))
                require(chunk)
                data.extend(chunk)
            require(not os.read(opened, 1) and identity(os.fstat(opened)) == identity(before) ==
                    identity(os.stat(path.name, dir_fd=parent, follow_symlinks=False)))
            return bytes(data)
        finally:
            if opened is not None:
                os.close(opened)
            os.close(pinned)


def render(upstream, source, work_parent, receipt, architecture):
    require(record(upstream) == UPSTREAM and upstream.count(CONTEXT) == 1 and architecture in ARCHES)
    helper = safe_path(source / HELPER)
    work = safe_path(work_parent)
    output = safe_path(receipt)
    hook = ("# HHSoft: filter both staging trees before either image is created.\n"
            "test \"$ARCH\" = '" + architecture + "' || exit 1\n"
            "/usr/bin/python3 -I -B '" + helper + "' --rootfs \"$ROOTFS\" --modloop \"$MODLOOP\" "
            "--architecture '" + architecture + "' --work-parent '" + work + "' --receipt '" + output + "' || exit 1\n\n").encode("ascii")
    replacement = CONTEXT[:-len(b"case $ARCH in\n")] + hook + b"case $ARCH in\n"
    return upstream.replace(CONTEXT, replacement)


def check_receipt(data, architecture, helper, policy):
    value = strict_json(data)
    approved = strict_json(policy)
    entries = approved["entries"]
    require(isinstance(entries, list) and len(entries) == 41 and
            len({row["path"] for row in entries}) == 41)
    require(isinstance(value, dict) and value.get("schema") == 1 and
            value.get("status") == "firmware_exclusions_applied" and
            value.get("architecture") == architecture and value.get("release_approval") is False and
            value.get("helper_sha256") == record(helper)["sha256"] and
            value.get("policy_sha256") == record(policy)["sha256"] and
            value.get("source_archive_sha256") == approved["source_archive"]["sha256"])
    removed, counts = value.get("removed"), value.get("counts")
    require(isinstance(removed, dict) and set(removed) == {"rootfs", "modloop"} and
            isinstance(counts, dict) and set(counts) == set(removed))
    expected = {"rootfs": [], "modloop": []}
    for entry in entries:
        row = {"path": entry["path"] + ".zst", "kind": entry["rootfs_kind"], **entry["stored"]}
        expected["rootfs"].append(row)
        if architecture in entry["modloop_architectures"]:
            expected["modloop"].append(dict(row, kind="file"))
    require(removed == expected and counts == {tree: len(rows) for tree, rows in expected.items()})
    return value


class RedactedParser(argparse.ArgumentParser):
    def error(self, _message):
        raise ValueError("unsupported firmware build arguments")


def main():
    parser = RedactedParser(description=__doc__)
    parser.add_argument("command", choices=("prepare", "verify"))
    parser.add_argument("--tools", type=Path, required=True)
    parser.add_argument("--work-parent", type=Path, required=True)
    parser.add_argument("--receipt", type=Path, required=True)
    parser.add_argument("--architecture", choices=sorted(ARCHES), required=True)
    args = parser.parse_args()
    require(sys.platform.startswith("linux") and hasattr(os, "O_PATH"))
    source = Path(os.path.abspath(__file__)).parents[2]
    require(source.name == "application-source" and args.tools.name == "host-applets" and
            args.tools.parent.name.startswith("tools-" + args.architecture + ".") and
            args.tools.parent.parent == source.parent.parent and
            args.work_parent == args.tools.parent / "tmp" and
            args.receipt == source.parent / "firmware-exclusions.json")
    for path in (source, args.tools, args.work_parent, args.receipt.parent):
        with directory(path) as descriptor:
            require(os.fstat(descriptor).st_uid == os.geteuid())
    helper, policy = read_file(source / HELPER), read_file(source / POLICY)
    upstream = read_file(Path("/usr/sbin/update-kernel"))
    patched = render(upstream, source, args.work_parent, args.receipt, args.architecture)
    destination = args.tools / "update-kernel"
    if args.command == "prepare":
        require(not os.path.lexists(args.receipt))
        with directory(args.tools) as parent:
            descriptor = os.open(destination.name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                                 0o700, dir_fd=parent)
            with os.fdopen(descriptor, "wb") as output:
                output.write(patched)
                output.flush()
                os.fsync(output.fileno())
    require(read_file(destination) == patched)
    if args.command == "verify":
        check_receipt(read_file(args.receipt), args.architecture, helper, policy)
    require(read_file(source / HELPER) == helper and read_file(source / POLICY) == policy and
            read_file(Path("/usr/sbin/update-kernel")) == upstream)
    print("Alpine firmware filter: " + args.command + " passed")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, TypeError, KeyError, UnicodeError):
        print("Alpine firmware filter: invalid or changed build input; image blocked", file=sys.stderr)
        sys.exit(1)
