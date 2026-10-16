#!/usr/bin/env python3
"""Create a reviewable Alpine cache lock or verify a build's offline input cache.

Candidate locks belong under build/ until reviewed. This tool never downloads
packages or treats a freshly generated lock as an approved release input.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import sys

ARCHES = {"x86", "x86_64", "aarch64"}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def file_record(path):
    require(path.is_file() and not path.is_symlink(), "cache entry must be a regular file")
    size = path.stat().st_size
    require(size > 0, "empty cache object")
    return {"size": size, "sha256": digest(path)}


def inventory(cache, architecture):
    require(architecture in ARCHES, "unsupported architecture")
    require(cache.is_dir() and not cache.is_symlink(), "cache root must be a regular directory")
    result = {}
    for directory, directories, files in os.walk(cache, followlinks=False):
        for name in directories:
            path = Path(directory) / name
            relative = path.relative_to(cache).parts
            require(not path.is_symlink(), "symlink directory in package cache")
            require((len(relative) == 1 and relative[0] in {"main", "community"}) or
                    (len(relative) == 2 and relative[0] in {"main", "community"} and relative[1] == architecture),
                    "unexpected directory in package cache")
        for name in files:
            path = Path(directory) / name
            relative = path.relative_to(cache).as_posix()
            parts = PurePosixPath(relative).parts
            require(len(parts) == 3 and parts[0] in {"main", "community"} and parts[1] == architecture,
                    "unexpected cache layout")
            require(name == "APKINDEX.tar.gz" or re.fullmatch(r"[A-Za-z0-9_+~.-]+\.apk", name),
                    "unexpected cache object")
            result[relative] = file_record(path)
            require(len(result) <= 10000, "package cache file count exceeds limit")
    for repository in ("main", "community"):
        require(f"{repository}/{architecture}/APKINDEX.tar.gz" in result, "signed repository index missing")
    require(any(path.endswith(".apk") for path in result), "package cache is empty")
    return dict(sorted(result.items()))


def create_lock(cache, architecture, base_lock):
    return {"schema": 1, "architecture": architecture, "base_lock_sha256": digest(base_lock),
            "files": inventory(cache, architecture)}


def read_lock(path):
    require(path.is_file() and not path.is_symlink() and path.stat().st_size <= 8 * 1024 * 1024,
            "invalid cache lock file")
    def unique(pairs):
        result = {}
        for key, value in pairs:
            require(key not in result, "duplicate cache lock field")
            result[key] = value
        return result
    return json.loads(path.read_text(encoding="utf-8"), object_pairs_hook=unique)


def verify(cache, architecture, base_lock, lock_file):
    approved = read_lock(lock_file)
    actual = create_lock(cache, architecture, base_lock)
    require(approved == actual, "cache differs from the reviewed package lock")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("lock", "verify"))
    parser.add_argument("--cache", type=Path, required=True)
    parser.add_argument("--arch", choices=sorted(ARCHES), required=True)
    parser.add_argument("--base-lock", type=Path, required=True)
    parser.add_argument("--lock", type=Path, required=True)
    args = parser.parse_args()
    if args.command == "verify":
        verify(args.cache, args.arch, args.base_lock, args.lock)
    else:
        payload = create_lock(args.cache, args.arch, args.base_lock)
        with args.lock.open("x", encoding="utf-8", newline="\n") as stream:
            json.dump(payload, stream, indent=2, sort_keys=True)
            stream.write("\n")
    print("Alpine package cache: " + args.command + " passed")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, TypeError):
        print("Alpine package cache: invalid or changed input; build blocked", file=sys.stderr)
        sys.exit(1)
