#!/usr/bin/env python3
"""Review and verify bounded, offline Buildroot source archive inputs.

Candidate output is restricted to ignored build storage. Only the fixed,
separately reviewed source-cache-lock.json is accepted by verify and stage.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import stat
import sys


APPROVED = "platform/buildroot/source-cache-lock.json"
POLICY_FILES = (".go-version", "scripts/build/build-go.sh",
                "scripts/build/build-buildroot-image.sh", "scripts/build/buildroot_source_cache.py")
MAX_FILES = 4096
MAX_DIRECTORIES = 512
MAX_OBJECT_BYTES = 4 * 1024**3
MAX_TOTAL_BYTES = 16 * 1024**3
MAX_POLICY_BYTES = 16 * 1024**2
MAX_LOCK_BYTES = 4 * 1024**2
NAME = re.compile(r"[A-Za-z0-9][A-Za-z0-9._+~-]{0,199}\Z")
SHA256 = re.compile(r"[0-9a-f]{64}\Z")
SOURCE_SUFFIXES = (".tar.gz", ".tar.bz2", ".tar.xz", ".tar.zst", ".tar.lz",
                   ".tar.lzma", ".tgz", ".tbz2", ".txz", ".zip", ".patch",
                   ".patch.gz", ".patch.xz", ".diff", ".diff.gz", ".diff.xz")


def require(condition, message):
    if not condition:
        raise ValueError(message)


def absolute_path(path):
    path = Path(path)
    require(".." not in path.parts, "parent traversal is not permitted")
    require(path.anchor != "//", "ambiguous double-slash root is not permitted")
    return path if path.is_absolute() else Path.cwd() / path


def directory_flags():
    require(sys.platform.startswith("linux") and hasattr(os, "O_NOFOLLOW") and hasattr(os, "O_PATH"),
            "source cache operations require native Linux")
    return os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC


def open_directory(path):
    """Open each component without following links, including parent links."""
    path = absolute_path(path)
    descriptor = os.open(path.anchor, directory_flags())
    try:
        for part in path.parts[1:]:
            child = os.open(part, directory_flags(), dir_fd=descriptor)
            os.close(descriptor)
            descriptor = child
        return descriptor
    except BaseException:
        os.close(descriptor)
        raise


def regular_bytes(parent, name, maximum, destination=None, keep=False):
    # O_PATH pins the entry without opening a device for I/O if its pathname
    # changes. Reopen only the verified regular inode through the kernel fd.
    pinned = os.open(name, os.O_PATH | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=parent)
    try:
        before = os.fstat(pinned)
        require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1,
                "source input must be a regular file without hardlinks")
        require(0 <= before.st_size <= maximum, "source input exceeds its size bound")
        descriptor = os.open("/proc/self/fd/" + str(pinned), os.O_RDONLY | os.O_NONBLOCK | os.O_CLOEXEC)
    finally:
        os.close(pinned)
    try:
        opened = os.fstat(descriptor)
        fields = ("st_dev", "st_ino", "st_mode", "st_nlink", "st_size", "st_mtime_ns", "st_ctime_ns")
        require(all(getattr(before, field) == getattr(opened, field) for field in fields),
                "source input changed while opening")
        checksum = hashlib.sha256()
        chunks = []
        total = 0
        while True:
            chunk = os.read(descriptor, min(1024**2, before.st_size - total + 1))
            if not chunk:
                break
            total += len(chunk)
            require(total <= before.st_size, "source input grew beyond its captured size")
            checksum.update(chunk)
            if keep:
                chunks.append(chunk)
            if destination is not None:
                view = memoryview(chunk)
                while view:
                    written = os.write(destination, view)
                    require(written > 0, "could not stage complete source input")
                    view = view[written:]
        after = os.fstat(descriptor)
        current = os.stat(name, dir_fd=parent, follow_symlinks=False)
        require(total == opened.st_size and
                all(getattr(opened, field) == getattr(after, field) == getattr(current, field) for field in fields),
                "source input changed while reading")
        return {"size": total, "sha256": checksum.hexdigest()}, b"".join(chunks)
    finally:
        os.close(descriptor)


def relative_file(root, relative, maximum, keep=False):
    parts = PurePosixPath(relative).parts
    parent = os.dup(root)
    try:
        for part in parts[:-1]:
            child = os.open(part, directory_flags(), dir_fd=parent)
            os.close(parent)
            parent = child
        return regular_bytes(parent, parts[-1], maximum, keep=keep)
    finally:
        os.close(parent)


def entries(descriptor, maximum):
    result = []
    with os.scandir(descriptor) as iterator:
        for entry in iterator:
            require(len(result) < maximum, "directory entry count exceeds its bound")
            result.append(entry.name)
    return sorted(result)


def payload_path(path):
    require(isinstance(path, str) and len(path) <= 401, "invalid source object path")
    parts = path.split("/")
    require(len(parts) == 2 and all(NAME.fullmatch(part) for part in parts),
            "source objects must use package/archive paths")
    require(parts[1].endswith(SOURCE_SUFFIXES), "unsupported source object type")


def inventory(cache):
    root = open_directory(cache)
    result = {}
    total = 0
    try:
        for package in entries(root, MAX_DIRECTORIES):
            require(NAME.fullmatch(package), "invalid source package directory")
            package_fd = os.open(package, directory_flags(), dir_fd=root)
            try:
                count = 0
                for name in entries(package_fd, MAX_FILES + 1):
                    if name == ".lock":
                        regular_bytes(package_fd, name, 0)
                        continue
                    relative = package + "/" + name
                    payload_path(relative)
                    require(len(result) < MAX_FILES, "source object count exceeds its bound")
                    record, _ = regular_bytes(package_fd, name, min(MAX_OBJECT_BYTES, MAX_TOTAL_BYTES - total))
                    require(record["size"] > 0, "empty source object")
                    total += record["size"]
                    require(total <= MAX_TOTAL_BYTES, "source cache exceeds its total size bound")
                    result[relative] = record
                    count += 1
                require(count > 0, "empty source package directory")
            finally:
                os.close(package_fd)
        require(result, "empty source cache")
        return dict(sorted(result.items()))
    finally:
        os.close(root)


def policy(repository):
    root = open_directory(repository)
    result = {}
    total = 0
    directories = 0

    def add(relative):
        nonlocal total
        require(len(relative) <= 1024, "build policy path exceeds its length bound")
        require(len(result) < MAX_FILES, "build policy file count exceeds its bound")
        record, _ = relative_file(root, relative, MAX_POLICY_BYTES)
        total += record["size"]
        require(total <= MAX_POLICY_BYTES, "build policy exceeds its total size bound")
        result[relative] = record

    def walk(descriptor, prefix, depth=0):
        nonlocal directories
        directories += 1
        require(directories <= MAX_DIRECTORIES, "build policy directory count exceeds its bound")
        require(depth <= 12, "build policy directory depth exceeds its bound")
        for name in entries(descriptor, MAX_FILES):
            require(NAME.fullmatch(name), "invalid build policy path")
            relative = prefix + "/" + name
            if relative == APPROVED:
                continue
            info = os.stat(name, dir_fd=descriptor, follow_symlinks=False)
            if stat.S_ISDIR(info.st_mode):
                child = os.open(name, directory_flags(), dir_fd=descriptor)
                try:
                    walk(child, relative, depth + 1)
                finally:
                    os.close(child)
            else:
                add(relative)

    try:
        for relative in POLICY_FILES:
            add(relative)
        platform = os.open("platform", directory_flags(), dir_fd=root)
        try:
            buildroot = os.open("buildroot", directory_flags(), dir_fd=platform)
        finally:
            os.close(platform)
        try:
            walk(buildroot, "platform/buildroot")
        finally:
            os.close(buildroot)
        require("platform/buildroot/base.lock" in result and
                "platform/buildroot/configs/hhpt_legacy_x86_defconfig" in result,
                "required Buildroot policy is missing")
        _, content = relative_file(root, "platform/buildroot/base.lock", MAX_POLICY_BYTES, keep=True)
        lines = content.decode("ascii").splitlines()
        values = {}
        for key in ("LINUX_SOURCE", "LINUX_SHA256"):
            matches = [line[len(key) + 1:] for line in lines if line.startswith(key + "=")]
            require(len(matches) == 1, "kernel source policy is ambiguous")
            values[key] = matches[0]
        payload_path("linux/" + values["LINUX_SOURCE"])
        require(SHA256.fullmatch(values["LINUX_SHA256"]), "invalid kernel source digest")
        return dict(sorted(result.items())), values
    finally:
        os.close(root)


def create_lock(cache, repository):
    policy_files, kernel = policy(repository)
    files = inventory(cache)
    require(files.get("linux/" + kernel["LINUX_SOURCE"], {}).get("sha256") == kernel["LINUX_SHA256"],
            "source cache must contain the pinned kernel archive")
    return validate_lock({"schema": 1, "profile": "legacy-x86", "policy": policy_files, "files": files})


def validate_lock(value):
    require(isinstance(value, dict) and set(value) == {"schema", "profile", "policy", "files"},
            "invalid source lock fields")
    require(type(value["schema"]) is int and value["schema"] == 1 and value["profile"] == "legacy-x86",
            "unsupported source lock schema or profile")
    for group in ("policy", "files"):
        records = value[group]
        require(isinstance(records, dict) and 0 < len(records) <= MAX_FILES, "invalid source lock records")
        total = 0
        for path, record in records.items():
            if group == "files":
                payload_path(path)
            else:
                require(isinstance(path, str) and len(path) <= 1024 and
                        (path in POLICY_FILES or path.startswith("platform/buildroot/")) and
                        path != APPROVED and (path == ".go-version" or
                        all(NAME.fullmatch(part) for part in path.split("/"))), "invalid policy lock path")
            require(isinstance(record, dict) and set(record) == {"size", "sha256"}, "invalid source record")
            require(type(record["size"]) is int and 0 <= record["size"] <=
                    (MAX_POLICY_BYTES if group == "policy" else MAX_OBJECT_BYTES), "invalid source record size")
            require(group == "policy" or record["size"] > 0, "empty source record")
            require(isinstance(record["sha256"], str) and SHA256.fullmatch(record["sha256"]),
                    "invalid source record digest")
            total += record["size"]
        require(total <= (MAX_POLICY_BYTES if group == "policy" else MAX_TOTAL_BYTES),
                "source lock exceeds its total size bound")
    return value


def read_approved(repository):
    root = open_directory(repository)
    try:
        _, content = relative_file(root, APPROVED, MAX_LOCK_BYTES, keep=True)
    finally:
        os.close(root)

    def unique(pairs):
        value = {}
        for key, item in pairs:
            require(key not in value, "duplicate source lock field")
            value[key] = item
        return value

    return validate_lock(json.loads(content.decode("utf-8"), object_pairs_hook=unique))


def verify(cache, repository):
    approved = read_approved(repository)
    require(create_lock(cache, repository) == approved, "source inputs differ from the reviewed lock")
    return approved


def candidate(cache, repository, output):
    repository = absolute_path(repository)
    cache = absolute_path(cache)
    output = absolute_path(output)
    require(output.is_relative_to(repository / "build") and output.name.endswith(".json") and
            NAME.fullmatch(output.name), "candidate output must be a new JSON file under build")
    require(not output.is_relative_to(cache), "candidate output must be separate from input cache")
    payload = create_lock(cache, repository)
    encoded = json.dumps(payload, indent=2, sort_keys=True) + "\n"
    require(len(encoded.encode("utf-8")) <= MAX_LOCK_BYTES, "candidate exceeds the lock size bound")
    parent = open_directory(output.parent)
    try:
        descriptor = os.open(output.name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                             0o600, dir_fd=parent)
        with os.fdopen(descriptor, "w", encoding="utf-8", newline="\n") as stream:
            stream.write(encoded)
    finally:
        os.close(parent)


def stage(cache, repository, destination):
    approved = verify(cache, repository)
    cache = absolute_path(cache)
    destination = absolute_path(destination)
    require(not destination.is_relative_to(cache), "staged cache must be separate from input cache")
    parent = open_directory(destination.parent)
    source = open_directory(cache)
    target = None
    try:
        os.mkdir(destination.name, 0o700, dir_fd=parent)
        target = os.open(destination.name, directory_flags(), dir_fd=parent)
        for relative, expected in approved["files"].items():
            package, name = relative.split("/")
            try:
                os.mkdir(package, 0o700, dir_fd=target)
            except FileExistsError:
                pass
            source_package = os.open(package, directory_flags(), dir_fd=source)
            target_package = os.open(package, directory_flags(), dir_fd=target)
            try:
                output = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                                 0o600, dir_fd=target_package)
                try:
                    actual, _ = regular_bytes(source_package, name, MAX_OBJECT_BYTES, destination=output)
                    require(actual == expected, "source input changed during staging")
                finally:
                    os.close(output)
            finally:
                os.close(source_package)
                os.close(target_package)
    finally:
        if target is not None:
            os.close(target)
        os.close(source)
        os.close(parent)
    verify(cache, repository)
    verify(destination, repository)


class RedactedParser(argparse.ArgumentParser):
    def error(self, message):
        raise ValueError("invalid source cache arguments")


def main():
    parser = RedactedParser(description=__doc__)
    parser.add_argument("command", choices=("candidate", "verify", "stage"))
    parser.add_argument("--cache", type=Path, required=True)
    parser.add_argument("--repository", type=Path, required=True)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--destination", type=Path)
    args = parser.parse_args()
    if args.command == "candidate":
        require(args.output is not None and args.destination is None, "candidate requires only an output path")
        candidate(args.cache, args.repository, args.output)
        print("Candidate created for review; it is not release approval.")
    elif args.command == "stage":
        require(args.destination is not None and args.output is None, "stage requires only a destination path")
        stage(args.cache, args.repository, args.destination)
    else:
        require(args.output is None and args.destination is None, "verify does not accept output paths")
        verify(args.cache, args.repository)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, TypeError, UnicodeError, RecursionError):
        print("Buildroot source cache check failed; inputs or reviewed policy differ.", file=sys.stderr)
        sys.exit(1)
