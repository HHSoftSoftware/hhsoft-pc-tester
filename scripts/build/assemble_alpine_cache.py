#!/usr/bin/python3 -IB
"""Assemble original APK v2 inputs without downloading or extracting packages.

This checks index membership and byte consistency, not signature trust or
dependency closure. Independently verify upstream signatures before approving
the resulting cache with alpine_package_cache.py. Metadata follows apk-tools
v3.0.8 doc/apk-v2.5.scd and src/extract_v2.c; the Q1 index identity hashes the
compressed control member, not the complete APK. Payload members are opaque.
"""

import argparse
import base64
from contextlib import contextmanager
import hashlib
import io
import itertools
import json
import os
from pathlib import Path
import re
import stat
import sys
import tarfile
import zlib


ARCHES = {"x86", "x86_64", "aarch64"}
REPOSITORIES = ("main", "community")
MAX_APK = 512 << 20
MAX_INPUT_BYTES = 16 << 30
MAX_EXPANDED_BYTES = 32 << 30
MAX_INDEX = 16 << 20
MAX_INDEX_TEXT = 64 << 20
MAX_INDEX_LINE = 1 << 20
MAX_CONTROL = 4 << 20
MAX_PAYLOAD = 2 << 30
MAX_FILES = 10000
MAX_RECORDS = 50000
CHUNK = 64 << 10
LINUX_PINNING = sys.platform.startswith("linux") and hasattr(os, "O_PATH")
TOKEN = re.compile(r"[A-Za-z0-9][A-Za-z0-9_+~.-]{0,127}\Z")
SIGNATURE = re.compile(r"\.SIGN\.(?:RSA|RSA256|RSA512|DSA)\.[A-Za-z0-9_@+.-]{1,180}\Z")


def require(condition, message):
    if not condition:
        raise ValueError(message)


def identity(info):
    result = info.st_dev, info.st_ino, info.st_mode, info.st_size, info.st_mtime_ns
    # Windows fixture lstat/fstat disagree on legacy ctime semantics. Production
    # Linux comparisons include metadata-change time as well as content time.
    return (*result, info.st_ctime_ns) if LINUX_PINNING else result


class BoundedReader:
    """Read only the captured regular-file extent, with a one-byte EOF probe."""

    def __init__(self, stream, size):
        self.stream, self.size = stream, size

    def read(self, count):
        require(count >= 0, "unbounded input read")
        remaining = self.size - self.stream.tell()
        require(remaining >= 0, "input position exceeds captured size")
        if count and not remaining:
            require(not self.stream.read(1), "input grew during read")
            return b""
        return self.stream.read(min(count, remaining))

    def seek(self, offset, whence=os.SEEK_SET):
        position = offset if whence == os.SEEK_SET else self.stream.tell() + offset
        require(whence in (os.SEEK_SET, os.SEEK_CUR) and 0 <= position <= self.size,
                "input seek exceeds captured size")
        return self.stream.seek(position)


class ExpansionBudget:
    def __init__(self):
        self.remaining = MAX_EXPANDED_BYTES

    def consume(self, count):
        require(count <= self.remaining, "total archive expansion exceeds limit")
        self.remaining -= count


class Files:
    """Pin Linux inputs before data opens; the portable mode is fixtures only."""

    def __init__(self, fixture=False):
        require(LINUX_PINNING or fixture, "assembly requires Linux descriptor pinning")

    @contextmanager
    def pin(self, path):
        path = Path(path)
        require(".." not in path.parts, "ambiguous input path")
        path = path.absolute()
        descriptor = None
        try:
            if LINUX_PINNING:
                descriptor = os.open(path.anchor, os.O_PATH | os.O_DIRECTORY | os.O_NOFOLLOW)
                for part in path.parts[1:]:
                    child = os.open(part, os.O_PATH | os.O_NOFOLLOW, dir_fd=descriptor)
                    os.close(descriptor)
                    descriptor = child
                    require(not stat.S_ISLNK(os.fstat(descriptor).st_mode), "symlink input path")
                yield path, descriptor, os.fstat(descriptor)
            else:
                # Never used by the production CLI. Windows fixture runs still
                # reject known links/reparse points before opening regular data.
                for prefix in reversed((path, *path.parents)):
                    info = prefix.lstat()
                    require(not stat.S_ISLNK(info.st_mode) and
                            not getattr(info, "st_file_attributes", 0) & 0x400,
                            "symlink input path")
                yield path, None, path.lstat()
        finally:
            if descriptor is not None:
                os.close(descriptor)

    @contextmanager
    def regular(self, path, limit):
        with self.pin(path) as (path, descriptor, before):
            require(stat.S_ISREG(before.st_mode) and 0 < before.st_size <= limit,
                    "input must be a bounded regular file")
            if descriptor is None:
                stream = path.open("rb")
            else:
                stream = os.fdopen(os.open("/proc/self/fd/" + str(descriptor), os.O_RDONLY), "rb")
            with stream:
                require(identity(os.fstat(stream.fileno())) == identity(before), "input changed before read")
                yield BoundedReader(stream, before.st_size), before.st_size
                require(identity(os.fstat(stream.fileno())) == identity(before), "input changed during read")

    def package_paths(self, directory):
        with self.pin(directory) as (path, descriptor, info):
            require(stat.S_ISDIR(info.st_mode), "package input must be a directory")
            names = []
            if descriptor is None:
                readable = None
                source = path
            else:
                readable = os.open("/proc/self/fd/" + str(descriptor), os.O_RDONLY | os.O_DIRECTORY)
                source = readable
            try:
                if readable is not None:
                    require(identity(os.fstat(readable)) == identity(info), "input directory changed")
                with os.scandir(source) as entries:
                    for entry in entries:
                        require(len(names) < MAX_FILES, "too many package directory entries")
                        names.append(entry.name)
            finally:
                if readable is not None:
                    os.close(readable)
            packages = []
            for name in sorted(names):
                child = path / name
                with self.pin(child) as (_, _, entry):
                    require(stat.S_ISREG(entry.st_mode), "package directory contains unsupported nodes")
                if name.endswith(".apk"):
                    packages.append(child)
            return packages


def gzip_member(stream, limit, capture=True, budget=None):
    decoder = zlib.decompressobj(31)
    sha1, sha256 = hashlib.sha1(), hashlib.sha256()
    output, pending = bytearray(), b""
    expanded, compressed = 0, 0
    while not decoder.eof:
        block = pending or stream.read(CHUNK)
        require(block, "truncated gzip member")
        remaining = min(limit - expanded, budget.remaining if budget is not None else limit)
        value = decoder.decompress(block, min(CHUNK, remaining + 1))
        pending = decoder.unused_data if decoder.eof else decoder.unconsumed_tail
        used = len(block) - len(pending)
        sha1.update(block[:used])
        sha256.update(block[:used])
        compressed += used
        expanded += len(value)
        require(expanded <= limit, "expanded archive exceeds limit")
        if budget is not None:
            budget.consume(len(value))
        if capture:
            output.extend(value)
    if pending:
        stream.seek(-len(pending), os.SEEK_CUR)
    return bytes(output), {"sha1": sha1.digest(), "sha256": sha256.hexdigest(),
                           "compressed": compressed, "expanded": expanded}


def metadata_tar(data, allowed, maximum_members=32):
    require(data and len(data) % 512 == 0, "invalid metadata tar boundary")
    result, end, pending_pax = {}, 0, False
    while end < len(data):
        header = data[end:end + 512]
        if not any(header):
            require(not any(data[end:]) and not pending_pax, "data after metadata tar terminator")
            break
        member = tarfile.TarInfo.frombuf(header, "utf-8", "strict")
        start = end + 512
        end = start + ((member.size + 511) // 512) * 512
        require(0 < member.size <= len(data) and end <= len(data), "truncated metadata member")
        content = data[start:start + member.size]
        require(not any(data[start + member.size:end]), "invalid metadata tar padding")
        if member.type == tarfile.XHDTYPE:
            # Official indexes carry local PAX timestamps. Accept only those
            # non-path attributes, without letting tarfile apply path, size,
            # sparse, or link overrides to a later metadata member.
            require(not pending_pax and member.size <= 4096 and
                    not member.name.startswith("/") and "\\" not in member.name and
                    ".." not in member.name.split("/"), "unsupported PAX header")
            position, seen = 0, set()
            while position < len(content):
                separator = content.find(b" ", position)
                require(separator > position and re.fullmatch(rb"[1-9][0-9]{0,3}", content[position:separator]),
                        "invalid PAX record length")
                stop = position + int(content[position:separator])
                require(separator + 1 < stop <= len(content) and content[stop - 1:stop] == b"\n", "truncated PAX record")
                key, delimiter, value = content[separator + 1:stop - 1].partition(b"=")
                require(delimiter and key in {b"mtime", b"ctime", b"atime"} and key not in seen and
                        re.fullmatch(rb"[0-9]{1,20}(?:\.[0-9]{1,20})?", value), "unsupported PAX attribute")
                seen.add(key)
                position = stop
            require(seen, "empty PAX header")
            pending_pax = True
            continue
        require(len(result) < maximum_members and member.name not in result,
                "duplicate or excessive metadata members")
        require(member.type in (tarfile.REGTYPE, tarfile.AREGTYPE) and allowed(member.name),
                "unsupported metadata member")
        result[member.name] = content
        pending_pax = False
    require(not pending_pax, "PAX header lacks a metadata member")
    return result


def signatures(stream, budget=None):
    data, _ = gzip_member(stream, 1 << 20, budget=budget)
    values = metadata_tar(data, lambda name: SIGNATURE.fullmatch(name) is not None, 16)
    require(values and all(len(value) <= 65536 for value in values.values()), "signature members missing or excessive")


def text_lines(data):
    text = data.decode("utf-8")
    require(all(character == "\n" or ord(character) >= 32 and ord(character) != 127 for character in text),
            "control characters in metadata")
    return (line.removesuffix("\n") for line in io.StringIO(text))


def token(value):
    require(isinstance(value, str) and TOKEN.fullmatch(value), "unsupported package identity")
    return value


def checksum(value):
    require(value.startswith("Q1"), "unsupported index checksum format")
    decoded = base64.b64decode(value[2:], validate=True)
    require(len(decoded) == 20 and base64.b64encode(decoded).decode("ascii") == value[2:],
            "invalid index checksum")
    return decoded


def parse_index(data, architecture):
    records, fields = {}, {}
    for line in itertools.chain(text_lines(data), ("",)):
        if not line:
            if not fields:
                continue
            require({"P", "V", "A", "C", "S"} <= fields.keys(), "incomplete index identity")
            key = token(fields["P"]), token(fields["V"])
            require(fields["A"] in {architecture, "noarch"}, "index architecture differs")
            checksum(fields["C"])
            # Unselected repository members may exceed the local archive
            # processing limit. Apply that limit when opening provided APKs.
            require(re.fullmatch(r"[1-9][0-9]{0,11}", fields["S"]), "invalid indexed package size")
            require(key not in records and len(records) < MAX_RECORDS, "duplicate or excessive index identities")
            records[key] = fields
            fields = {}
            continue
        require(len(line) <= MAX_INDEX_LINE and re.match(r"[A-Za-z]:", line), "invalid index field")
        name, value = line.split(":", 1)
        require(name not in fields and len(fields) < 64, "duplicate or excessive index fields")
        fields[name] = value
    require(records, "empty repository index")
    return records


def sha256(stream, size):
    stream.seek(0)
    digest, remaining = hashlib.sha256(), size
    while remaining:
        block = stream.read(min(CHUNK, remaining))
        require(block, "input truncated during hashing")
        digest.update(block)
        remaining -= len(block)
    require(not stream.read(1), "input grew during hashing")
    stream.seek(0)
    return digest.hexdigest()


def read_index(files, path, architecture, budget=None):
    with files.regular(path, MAX_INDEX) as (stream, size):
        digest = sha256(stream, size)
        signatures(stream, budget=budget)
        data, _ = gzip_member(stream, MAX_INDEX_TEXT + (1 << 20), budget=budget)
        require(not stream.read(1), "extra index gzip members or trailing data")
        members = metadata_tar(data, lambda name: name in {"APKINDEX", "DESCRIPTION"}, 2)
        require(set(members) == {"APKINDEX", "DESCRIPTION"} and len(members["DESCRIPTION"]) <= 160 and
                len(members["APKINDEX"]) <= MAX_INDEX_TEXT, "invalid index members")
        return parse_index(members["APKINDEX"], architecture), {"size": size, "sha256": digest}


def parse_pkginfo(data):
    values = {}
    repeated = {"depend", "provides", "install_if", "replaces", "triggers"}
    for line in text_lines(data):
        if not line or line.startswith("#"):
            continue
        name, delimiter, value = line.partition(" = ")
        require(delimiter and re.fullmatch(r"[a-z][a-z0-9_]{0,63}", name) and len(line) <= 65536,
                "invalid package metadata field")
        require(name not in values or name in repeated, "duplicate package metadata field")
        values.setdefault(name, []).append(value)
    require({"pkgname", "pkgver", "arch", "datahash"} <= values.keys(), "incomplete package identity")
    result = {name: items[0] for name, items in values.items() if name not in repeated}
    token(result["pkgname"])
    token(result["pkgver"])
    require(re.fullmatch(r"[0-9a-f]{64}", result["datahash"]), "invalid package payload hash")
    return result


def read_package(files, path, architecture, compressed_remaining=MAX_INPUT_BYTES, budget=None):
    with files.regular(path, min(MAX_APK, compressed_remaining)) as (stream, size):
        digest = sha256(stream, size)
        signatures(stream, budget=budget)
        control, control_hash = gzip_member(stream, MAX_CONTROL, budget=budget)
        members = metadata_tar(control, lambda name: re.fullmatch(r"\.[A-Za-z][A-Za-z0-9_.+-]{0,127}", name) is not None)
        require(".PKGINFO" in members and not any(name.startswith(".SIGN.") for name in members),
                "package control metadata missing or misplaced")
        metadata = parse_pkginfo(members[".PKGINFO"])
        require(metadata["arch"] in {architecture, "noarch"}, "package architecture differs")
        _, payload = gzip_member(stream, MAX_PAYLOAD, capture=False, budget=budget)
        require(payload["expanded"] >= 1024 and payload["expanded"] % 512 == 0 and not stream.read(1),
                "unsupported package payload boundary")
        require(metadata["datahash"] == payload["sha256"], "package payload hash differs")
        return metadata, {"name": metadata["pkgname"], "version": metadata["pkgver"],
                          "architecture": metadata["arch"], "size": size, "sha256": digest,
                          "control_checksum": "Q1" + base64.b64encode(control_hash["sha1"]).decode("ascii")}


def required_identity(value):
    name, separator, version = value.partition("=")
    require(separator and "=" not in version, "required package must specify an exact version")
    return token(name), token(version)


def membership(metadata, package, indexed):
    require(package["control_checksum"] == indexed["C"] and package["size"] == int(indexed["S"]),
            "package bytes differ from indexed identity")
    require(metadata["arch"] == "noarch" or metadata["arch"] == indexed["A"], "package and index architecture differ")
    for index_field, field in (("I", "size"), ("T", "pkgdesc"), ("U", "url"), ("L", "license"),
                               ("o", "origin"), ("m", "maintainer"), ("t", "builddate"), ("c", "commit"),
                               ("k", "provider_priority")):
        if index_field in indexed:
            require(metadata.get(field) == indexed[index_field], "package metadata differs from index")


def plan(files, architecture, indexes, directories, required):
    require(architecture in ARCHES and set(indexes) == set(REPOSITORIES), "unsupported cache layout")
    require(0 < len(directories) <= 32 and len(required) <= MAX_FILES, "invalid input selection size")
    index_records, index_files, budget = {}, {}, ExpansionBudget()
    for repository in REPOSITORIES:
        index_records[repository], record = read_index(files, indexes[repository], architecture, budget=budget)
        index_files[repository + "/" + architecture + "/APKINDEX.tar.gz"] = (Path(indexes[repository]), record)
    selected, seen, unmatched, inputs = {}, {}, [], 0
    total = 0
    for directory in directories:
        for path in files.package_paths(directory):
            inputs += 1
            require(inputs <= MAX_FILES, "too many input packages")
            metadata, package = read_package(files, path, architecture, MAX_INPUT_BYTES - total, budget=budget)
            total += package["size"]
            require(total <= MAX_INPUT_BYTES, "package inputs exceed byte limit")
            key = package["name"], package["version"]
            if key in seen:
                require(seen[key] == package["sha256"], "different bytes for one package identity")
                continue
            seen[key] = package["sha256"]
            repositories = [repository for repository in REPOSITORIES if key in index_records[repository]]
            require(len(repositories) <= 1, "ambiguous package repository membership")
            if not repositories:
                unmatched.append(package)
                continue
            repository = repositories[0]
            membership(metadata, package, index_records[repository][key])
            destination = repository + "/" + architecture + "/" + key[0] + "-" + key[1] + ".apk"
            require(destination not in selected, "canonical package filename collision")
            selected[destination] = (path, dict(package, repository=repository))
    require(inputs, "no input packages")
    gaps = []
    matched_keys = {(record["name"], record["version"]) for _, record in selected.values()}
    for key in sorted({required_identity(value) for value in required}):
        repositories = [repository for repository in REPOSITORIES if key in index_records[repository]]
        require(len(repositories) <= 1, "ambiguous required package repository")
        if key not in matched_keys:
            gaps.append({"name": key[0], "version": key[1],
                         "reason": "archive_missing" if repositories else "identity_not_indexed"})
    report = {"schema": 1, "architecture": architecture,
              "status": "incomplete" if gaps or unmatched else "assembled",
              "signature_verification": "not_performed", "dependency_closure": "not_established",
              "input_archives": inputs, "duplicate_archives": inputs - len(seen),
              "indexes": {name: record for name, (_, record) in sorted(index_files.items())},
              "packages": {name: record for name, (_, record) in sorted(selected.items())},
              "required_gaps": gaps, "unindexed_archives": sorted(unmatched, key=lambda item: (item["name"], item["version"]))}
    return report, index_files | selected


@contextmanager
def output_directory(files, path):
    path = Path(path).absolute()
    require(path.name not in {"", ".", ".."}, "invalid output directory")
    with files.pin(path.parent) as (parent, descriptor, info):
        require(stat.S_ISDIR(info.st_mode), "output parent is not a directory")
        if descriptor is None:
            path.mkdir(mode=0o700)
            yield None
        else:
            os.mkdir(path.name, mode=0o700, dir_fd=descriptor)
            output = os.open(path.name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=descriptor)
            try:
                yield output
            finally:
                os.close(output)


def copy_original(files, source, destination, directory_fd, record):
    with files.regular(source, max(MAX_APK, MAX_INDEX)) as (stream, size):
        require(size == record["size"], "input size changed before copy")
        if directory_fd is None:
            target = Path(destination).open("xb")
        else:
            target = os.fdopen(os.open(str(destination), os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                                      0o600, dir_fd=directory_fd), "wb")
        digest = hashlib.sha256()
        with target:
            remaining = size
            while remaining:
                block = stream.read(min(CHUNK, remaining))
                require(block, "input truncated during copy")
                digest.update(block)
                target.write(block)
                remaining -= len(block)
            require(not stream.read(1), "input grew during copy")
        require(digest.hexdigest() == record["sha256"], "input bytes changed before copy")


def assemble(architecture, main_index, community_index, directories, output, report_path, required=(), *, fixture=False):
    files = Files(fixture=fixture)
    output, report_path = Path(output), Path(report_path)
    require(output.absolute() != report_path.absolute(), "cache and report paths must differ")
    require(not output.exists() and not output.is_symlink() and not report_path.exists() and not report_path.is_symlink(),
            "output and report must be new")
    report, objects = plan(files, architecture, {"main": main_index, "community": community_index}, directories, required)
    # Reserve the separate report before creating cache files. Existing inputs
    # are never overwritten. Failed copies retain only this invocation's partial
    # output for diagnosis; a complete report is emitted only after all copies.
    with files.pin(report_path.parent) as (_, parent_fd, parent_info):
        require(stat.S_ISDIR(parent_info.st_mode), "report parent must be a directory")
        if parent_fd is None:
            report_stream = report_path.open("x", encoding="utf-8", newline="\n")
        else:
            report_stream = os.fdopen(os.open(report_path.name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                                             0o600, dir_fd=parent_fd), "w", encoding="utf-8", newline="\n")
        with report_stream:
            if report["status"] == "assembled":
                with output_directory(files, output) as output_fd:
                    destinations = {}
                    try:
                        for repository in REPOSITORIES:
                            if output_fd is None:
                                (output / repository / architecture).mkdir(parents=True, mode=0o700)
                            else:
                                os.mkdir(repository, mode=0o700, dir_fd=output_fd)
                                repository_fd = os.open(repository, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=output_fd)
                                try:
                                    os.mkdir(architecture, mode=0o700, dir_fd=repository_fd)
                                    destinations[repository] = os.open(architecture, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                                                                       dir_fd=repository_fd)
                                finally:
                                    os.close(repository_fd)
                        for name, (source, record) in sorted(objects.items()):
                            repository, _, basename = name.split("/")
                            copy_original(files, source, output / name if output_fd is None else basename,
                                          destinations.get(repository), record)
                    finally:
                        for descriptor in destinations.values():
                            os.close(descriptor)
            json.dump(report, report_stream, indent=2, sort_keys=True)
            report_stream.write("\n")
    return report


class RedactedParser(argparse.ArgumentParser):
    def error(self, message):
        raise ValueError("unsupported assembler arguments")


def main():
    parser = RedactedParser(description=__doc__)
    parser.add_argument("--arch", choices=sorted(ARCHES), required=True)
    parser.add_argument("--main-index", type=Path, required=True)
    parser.add_argument("--community-index", type=Path, required=True)
    parser.add_argument("--packages", type=Path, action="append", required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--report", type=Path, required=True)
    parser.add_argument("--require", action="append", default=[], metavar="NAME=VERSION")
    args = parser.parse_args()
    report = assemble(args.arch, args.main_index, args.community_index, args.packages, args.output, args.report, args.require)
    print("Alpine cache assembly: " + report["status"] + "; signatures and dependency closure require separate validation")
    return 0 if report["status"] == "assembled" else 2


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, TypeError, UnicodeError, tarfile.TarError, zlib.error):
        print("Alpine cache assembly: invalid, changed, or unsupported input; assembly blocked", file=sys.stderr)
        sys.exit(1)
