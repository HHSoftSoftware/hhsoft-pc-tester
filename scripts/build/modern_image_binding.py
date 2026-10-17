#!/usr/bin/python3 -IB
"""Bind a freshly compiled image application to committed source inputs.

Records describe this build invocation, not approval, extracted-image validation,
or full builder qualification. Production requires a standalone Linux Git clone.
Git reads raw objects in a disposable configuration-free object-store copy.
"""

import argparse
from contextlib import contextmanager
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import tempfile
import threading
import types


def local_module(name, relative):
    # Execute the reviewed source itself, never an ignored local .pyc cache.
    path = Path(os.path.abspath(Path(__file__).parent / relative))
    pinned = None
    try:
        if sys.platform.startswith("linux") and hasattr(os, "O_PATH"):
            pinned = os.open(path.anchor, os.O_PATH | os.O_DIRECTORY | os.O_NOFOLLOW)
            for part in path.parts[1:]:
                child = os.open(part, os.O_PATH | os.O_NOFOLLOW, dir_fd=pinned)
                os.close(pinned)
                pinned = child
                if stat.S_ISLNK(os.fstat(pinned).st_mode):
                    raise ValueError("linked build policy")
            info = os.fstat(pinned)
            source = "/proc/self/fd/" + str(pinned)
        else:
            # Module import is portable for pure fixture tests; production
            # operations separately require Linux descriptor pinning.
            for prefix in (path, *path.parents):
                info = prefix.lstat()
                if stat.S_ISLNK(info.st_mode) or getattr(info, "st_file_attributes", 0) & 0x400:
                    raise ValueError("linked build policy")
            info, source = path.stat(), path
        if not stat.S_ISREG(info.st_mode) or not 0 < info.st_size <= 1 << 20:
            raise ValueError("build policy source exceeds bound")
        with open(source, "rb") as stream:
            opened = os.fstat(stream.fileno())
            if (opened.st_dev, opened.st_ino, opened.st_mode, opened.st_size) != (info.st_dev, info.st_ino, info.st_mode, info.st_size):
                raise ValueError("build policy source changed")
            content = stream.read(info.st_size + 1)
            after = os.fstat(stream.fileno())
            if len(content) != info.st_size or (after.st_size, after.st_mtime_ns, after.st_ctime_ns) != (opened.st_size, opened.st_mtime_ns, opened.st_ctime_ns):
                raise ValueError("build policy source changed")
    finally:
        if pinned is not None:
            os.close(pinned)
    module = types.ModuleType(name)
    module.__file__ = str(path)
    exec(compile(content, str(path), "exec"), module.__dict__)
    module.__binding_source_sha256__ = hashlib.sha256(content).hexdigest()
    return module


try:
    files = local_module("binding_pinned_files", "buildroot_source_cache.py")
    host = local_module("binding_host_policy", "builder_host_manifest.py")
    source_policy = local_module("binding_source_policy", "../security/scan_source.py")
except (OSError, ValueError, SyntaxError, ImportError):
    if __name__ != "__main__":
        raise
    print("Modern application binding: unavailable or unsupported build policy; build blocked", file=sys.stderr)
    sys.exit(1)
ARCHITECTURES = {"x86": (1, 3), "x86_64": (2, 62), "aarch64": (2, 183), "legacy-x86": (1, 3)}
MAX_SOURCE_FILES = 5000
MAX_SOURCE_BYTES = 64 << 20
MAX_SOURCE_FILE = 16 << 20
MAX_GIT_BYTES = 512 << 20
MAX_GIT_FILES = 100000
MAX_RECORD_BYTES = 8 << 20
MAX_BINARY_BYTES = 64 << 20
OID = re.compile(r"[0-9a-f]{40}\Z")
POLICIES = ("VERSION", ".go-version", "go.mod", "scripts/build/build-go.sh",
            "scripts/build/build-alpine-image.sh", "scripts/build/modern_image_binding.py",
            "scripts/build/buildroot_source_cache.py", "scripts/build/builder_host_manifest.py",
            "scripts/build/alpine_package_cache.py", "scripts/build/record_alpine_apks.py",
            "scripts/build/prepare_alpine_kernel.py", "scripts/build/filter_alpine_firmware.py",
            "platform/alpine/firmware-exclusions.json",
            "scripts/build/write-alpine-sbom.sh", "scripts/security/scan_source.py",
            "platform/alpine/base.lock", "platform/alpine/mkimage/genapkovl-hhpt.sh",
            "platform/alpine/mkimage/mkimg.hhpt.sh", "platform/alpine/mkimage/upstream-scripts.sha256")
LEGACY_POLICIES = ("VERSION", ".go-version", "go.mod", "scripts/build/build-go.sh",
                   "scripts/build/build-buildroot-image.sh", "scripts/build/modern_image_binding.py",
                   "scripts/build/buildroot_source_cache.py", "scripts/build/builder_host_manifest.py",
                   "scripts/build/write_buildroot_sbom.py", "scripts/security/scan_source.py",
                   "platform/buildroot/base.lock", "platform/buildroot/configs/hhpt_legacy_x86_defconfig",
                   "platform/buildroot/board/legacy-x86/post-build.sh")


def policies(architecture):
    return LEGACY_POLICIES if architecture == "legacy-x86" else POLICIES


def require(value, message):
    if not value:
        raise ValueError(message)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True, allow_nan=False).encode("ascii")


def blob_oid(kind, content):
    return hashlib.sha1(kind.encode("ascii") + b" " + str(len(content)).encode("ascii") + b"\0" + content).hexdigest()


def read_file(root, relative, maximum=MAX_SOURCE_FILE):
    require(source_policy.safe_relative_name(relative), "unsupported relative input path")
    return files.relative_file(root, relative, maximum, keep=True)


def exists(root, relative):
    parent = os.dup(root)
    try:
        parts = relative.split("/")
        for part in parts[:-1]:
            child = os.open(part, files.directory_flags(), dir_fd=parent)
            os.close(parent)
            parent = child
        os.stat(parts[-1], dir_fd=parent, follow_symlinks=False)
        return True
    except FileNotFoundError:
        return False
    finally:
        os.close(parent)


def committed_head(git_root):
    _, content = read_file(git_root, "HEAD", 1024)
    value = content.decode("ascii").strip()
    if OID.fullmatch(value):
        return value
    require(value.startswith("ref: refs/heads/"), "unsupported Git HEAD")
    reference = value[5:]
    require(source_policy.safe_relative_name(reference) and
            all(re.fullmatch(r"[A-Za-z0-9_.-]+", part) for part in reference.split("/")),
            "unsupported source reference")
    try:
        _, content = read_file(git_root, reference, 128)
        commit = content.decode("ascii").strip()
    except FileNotFoundError:
        _, content = read_file(git_root, "packed-refs", 4 << 20)
        matches = [line.split(" ", 1)[0] for line in content.decode("ascii").splitlines()
                   if line.endswith(" " + reference)]
        require(len(matches) == 1, "source reference missing or ambiguous")
        commit = matches[0]
    require(OID.fullmatch(commit), "invalid source commit")
    return commit


def copy_object_store(git_root, destination):
    for path in ("shallow", "info/grafts", "refs/replace", "objects/info/alternates", "objects/info/http-alternates"):
        require(not exists(git_root, path), "unsupported Git history or alternate object state")
    source = os.open("objects", files.directory_flags(), dir_fd=git_root)
    count = total = 0
    try:
        for group in files.entries(source, 260):
            require(group in {"info", "pack"} or re.fullmatch(r"[0-9a-f]{2}", group), "unsupported Git object directory")
            directory = os.open(group, files.directory_flags(), dir_fd=source)
            try:
                # Graph indexes and other optional info are never supplied to
                # Git. In particular, no alternates can cross this boundary.
                if group == "info":
                    continue
                target = destination / group
                target.mkdir(mode=0o700)
                for name in files.entries(directory, MAX_GIT_FILES):
                    count += 1
                    require(count <= MAX_GIT_FILES, "too many Git object files")
                    if group == "pack":
                        require(re.fullmatch(r"pack-[0-9a-f]{40}\.(?:pack|idx|rev|bitmap)", name),
                                "unsupported or incomplete Git pack")
                    else:
                        require(re.fullmatch(r"[0-9a-f]{38}", name), "unsupported loose Git object")
                    with (target / name).open("xb") as output:
                        record, _ = files.regular_bytes(directory, name, MAX_GIT_BYTES - total,
                                                        destination=output.fileno())
                    total += record["size"]
                    require(total <= MAX_GIT_BYTES, "Git object store exceeds bound")
            finally:
                os.close(directory)
    finally:
        os.close(source)


@contextmanager
def git_objects(repository, work):
    root = files.open_directory(repository)
    try:
        # Linked worktrees and gitdir indirection require a separate policy.
        git_root = os.open(".git", files.directory_flags(), dir_fd=root)
        try:
            commit = committed_head(git_root)
            with tempfile.TemporaryDirectory(prefix="source-git-", dir=work) as temporary:
                controlled = Path(temporary)
                (controlled / "objects").mkdir(mode=0o700)
                (controlled / "refs").mkdir(mode=0o700)
                (controlled / "HEAD").write_text(commit + "\n", encoding="ascii")
                copy_object_store(git_root, controlled / "objects")
                require(committed_head(git_root) == commit, "source reference changed")
                environment = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "HOME": "/dev/null", "LC_ALL": "C",
                               "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": "/dev/null",
                               "GIT_NO_REPLACE_OBJECTS": "1", "GIT_NO_LAZY_FETCH": "1",
                               "GIT_OPTIONAL_LOCKS": "0", "GIT_TERMINAL_PROMPT": "0", "GIT_ALLOW_PROTOCOL": ""}

                def object_bytes(kind, oid):
                    require(kind in {"commit", "tree"} and OID.fullmatch(oid), "invalid Git object request")
                    process = subprocess.Popen(["/usr/bin/git", "--git-dir=" + str(controlled), "cat-file", kind, oid],
                                               cwd=controlled, env=environment, stdout=subprocess.PIPE,
                                               stderr=subprocess.DEVNULL)
                    timer = threading.Timer(20, process.kill)
                    timer.start()
                    try:
                        content = process.stdout.read(MAX_SOURCE_FILE + 1)
                        if len(content) > MAX_SOURCE_FILE:
                            process.kill()
                        result = process.wait(timeout=25)
                        require(result == 0 and len(content) <= MAX_SOURCE_FILE and blob_oid(kind, content) == oid,
                                "Git object is missing, changed, or excessive")
                        return content
                    finally:
                        timer.cancel()
                        process.stdout.close()
                        if process.poll() is None:
                            process.kill()
                        process.wait()

                yield root, commit, object_bytes
                require(committed_head(git_root) == commit, "source reference changed")
        finally:
            os.close(git_root)
    finally:
        os.close(root)


def tree_files(tree, object_bytes):
    result = {}
    directories = 0

    def walk(oid, prefix="", depth=0):
        nonlocal directories
        directories += 1
        require(depth <= 32 and directories <= MAX_SOURCE_FILES, "source tree exceeds directory bound")
        content, position, names = object_bytes("tree", oid), 0, set()
        while position < len(content):
            space, zero = content.find(b" ", position), content.find(b"\0", position)
            require(position < space < zero and zero + 21 <= len(content), "invalid raw Git tree")
            mode = content[position:space].decode("ascii")
            name = content[space + 1:zero].decode("utf-8")
            child_oid = content[zero + 1:zero + 21].hex()
            position = zero + 21
            require("/" not in name and name not in names, "duplicate or unsafe tree entry")
            names.add(name)
            relative = prefix + name
            require(not source_policy.forbidden_path(relative), "forbidden source input path")
            if mode == "40000":
                walk(child_oid, relative + "/", depth + 1)
            else:
                require(mode in {"100644", "100755"} and len(result) < MAX_SOURCE_FILES,
                        "source entry is not a bounded regular blob")
                result[relative] = {"mode": mode, "git_blob": child_oid}

    walk(tree)
    require(result, "empty source tree")
    return dict(sorted(result.items()))


def reject_extra_compilation_inputs(root, tracked):
    def walk(descriptor, prefix):
        for name in files.entries(descriptor, MAX_SOURCE_FILES):
            relative = prefix + name
            info = os.stat(name, dir_fd=descriptor, follow_symlinks=False)
            require(not stat.S_ISLNK(info.st_mode), "extra or linked compilation input")
            if stat.S_ISDIR(info.st_mode):
                require(any(path.startswith(relative + "/") for path in tracked), "extra compilation directory")
                child = os.open(name, files.directory_flags(), dir_fd=descriptor)
                try:
                    walk(child, relative + "/")
                finally:
                    os.close(child)
            else:
                require(stat.S_ISREG(info.st_mode) and relative in tracked, "extra compilation input")

    for directory in ("cmd", "internal"):
        descriptor = os.open(directory, files.directory_flags(), dir_fd=root)
        try:
            walk(descriptor, directory + "/")
        finally:
            os.close(descriptor)
    for name in files.entries(root, MAX_SOURCE_FILES):
        if name.endswith((".go", ".s", ".S", ".c", ".h")) or name in {"go.mod", "go.sum", "go.work", "go.work.sum"}:
            require(name in tracked, "extra top-level compilation input")


def commit_timestamp(content):
    headers = content.split(b"\n\n", 1)[0].split(b"\n")
    committers = [line for line in headers if line.startswith(b"committer ")]
    require(len(committers) == 1, "source commit timestamp is missing or ambiguous")
    match = re.fullmatch(rb"committer [^\r\n]+ (0|[1-9][0-9]{0,11}) [+-][0-9]{4}", committers[0])
    require(match is not None, "unsupported source commit timestamp")
    timestamp = int(match.group(1))
    require(timestamp <= 253402300799, "source commit timestamp exceeds bound")
    return timestamp


def source_identity(repository, work, destination=None, architecture=None):
    with git_objects(repository, work) as (root, commit, object_bytes):
        commit_content = object_bytes("commit", commit)
        header = commit_content.split(b"\n", 1)[0]
        require(re.fullmatch(rb"tree [0-9a-f]{40}", header), "invalid committed source tree")
        tree = header[5:].decode("ascii")
        tracked = tree_files(tree, object_bytes)
        require(set(policies(architecture)) <= tracked.keys(), "required build policy missing from committed source")
        reject_extra_compilation_inputs(root, tracked)
        total = 0
        if destination is not None:
            destination.mkdir(mode=0o700)
        for relative, entry in tracked.items():
            record, content = read_file(root, relative, min(MAX_SOURCE_FILE, MAX_SOURCE_BYTES - total))
            require(blob_oid("blob", content) == entry["git_blob"], "working source differs from raw committed bytes")
            total += record["size"]
            require(total <= MAX_SOURCE_BYTES, "source snapshot exceeds byte bound")
            entry.update(record)
            if destination is not None:
                target = destination / relative
                target.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
                with target.open("xb") as stream:
                    stream.write(content)
                target.chmod(0o755 if entry["mode"] == "100755" else 0o644)
        for relative, module in (("scripts/build/buildroot_source_cache.py", files),
                                 ("scripts/build/builder_host_manifest.py", host),
                                 ("scripts/security/scan_source.py", source_policy)):
            require(tracked[relative]["sha256"] == module.__binding_source_sha256__, "executed build policy differs from source")
        actual_parent = files.open_directory(Path(__file__).parent)
        try:
            actual, _ = read_file(actual_parent, Path(__file__).name)
        finally:
            os.close(actual_parent)
        require(tracked["scripts/build/modern_image_binding.py"]["sha256"] == actual["sha256"],
                "executed binding policy differs from source")
        result = {"commit": commit, "tree": tree, "files": tracked,
                  "files_sha256": hashlib.sha256(canonical(tracked)).hexdigest()}
        if architecture == "legacy-x86":
            result["commit_epoch"] = commit_timestamp(commit_content)
        return result


def verify_source_copy(source, expected):
    root = files.open_directory(source)
    try:
        reject_extra_compilation_inputs(root, expected["files"])
        for relative, entry in expected["files"].items():
            record, _ = read_file(root, relative)
            require(record == {name: entry[name] for name in ("size", "sha256")}, "copied source input changed")
    finally:
        os.close(root)


def read_json(path):
    root = files.open_directory(Path(path).parent)
    try:
        _, content = read_file(root, Path(path).name, MAX_RECORD_BYTES)
    finally:
        os.close(root)
    def unique(pairs):
        value = {}
        for key, item in pairs:
            require(key not in value, "duplicate binding field")
            value[key] = item
        return value
    return json.loads(content, object_pairs_hook=unique)


def write_json(path, value):
    content = canonical(value) + b"\n"
    require(len(content) <= MAX_RECORD_BYTES, "binding record exceeds byte bound")
    root = files.open_directory(Path(path).parent)
    try:
        descriptor = os.open(Path(path).name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                             0o600, dir_fd=root)
        with os.fdopen(descriptor, "wb") as stream:
            stream.write(content)
    finally:
        os.close(root)


def host_binding(repository, classification, architecture):
    if classification == "development":
        return {"status": "unqualified-development", "manifest_sha256": None}
    profile, platform = ("buildroot-image", "buildroot") if architecture == "legacy-x86" else ("alpine-image", "alpine")
    host_architecture = host.actual_host_architecture()
    relative = "platform/builder-hosts/" + profile + "-" + host_architecture + ".json"
    manifest = repository / relative
    host.verify_manifest(manifest, profile, repository / "platform" / platform / "base.lock", repository / ".go-version")
    root = files.open_directory(repository)
    try:
        record, _ = read_file(root, relative, host.MAX_MANIFEST_BYTES)
    finally:
        os.close(root)
    return {"status": "matched-reviewed-manifest", "manifest_sha256": record["sha256"], "host_architecture": host_architecture}


def context(repository, architecture):
    require(architecture in ARCHITECTURES, "unsupported image target")
    root = files.open_directory(repository)
    try:
        _, version = read_file(root, "VERSION", 256)
        _, go_version = read_file(root, ".go-version", 64)
    finally:
        os.close(root)
    classification = host.classify_version(repository / "VERSION")
    version = version.decode("ascii").rstrip("\r\n")
    go_version = go_version.decode("ascii").rstrip("\r\n")
    require(re.fullmatch(r"[1-9][0-9]*\.[0-9]+\.[0-9]+", go_version), "invalid Go version policy")
    return {"architecture": architecture, "version": version, "go_version": go_version,
            "host": host_binding(repository, classification, architecture), "qualification": "not_established"}


def prepare(repository, work, architecture):
    require(not (work / "application-binding.json").exists(), "binding output must be new")
    if architecture == "legacy-x86":
        require(not any(os.path.lexists(work / name) for name in
                        ("application-inputs.json", "application-bin", "application-overlay.sha256")),
                "legacy application inputs and receipt must be new")
    source = source_identity(repository, work, work / "application-source", architecture)
    value = {"schema": 1, "source": source, **context(repository, architecture)}
    value["policy"] = {path: source["files"][path]["sha256"] for path in policies(architecture)}
    write_json(work / "application-inputs.json", value)


def binary_record(work, architecture):
    relative = "application-bin/linux_" + architecture + "/hhpt"
    root = files.open_directory(work)
    try:
        record, content = read_file(root, relative, MAX_BINARY_BYTES)
    finally:
        os.close(root)
    elf_class, machine = ARCHITECTURES[architecture]
    require(len(content) >= 64 and content[:4] == b"\x7fELF" and content[4:7] == bytes((elf_class, 1, 1)) and
            int.from_bytes(content[16:18], "little") in (2, 3) and
            int.from_bytes(content[18:20], "little") == machine, "compiled binary target differs")
    return {"filename": "hhpt", "installed_path": "/usr/bin/hhpt", "target_os": "linux", **record}


def verify_inputs(repository, work, value, architecture):
    fields = {"schema", "source", "architecture", "version", "go_version", "host", "qualification", "policy"}
    require(isinstance(value, dict) and type(value.get("schema")) is int and value["schema"] == 1 and
            set(value) in (fields, fields | {"binary"}), "unsupported application binding")
    require(value["architecture"] == architecture, "application target differs from requested target")
    expected = context(repository, architecture)
    require(all(value.get(key) == item for key, item in expected.items()), "application build policy changed")
    require(source_identity(repository, work, architecture=architecture) == value["source"], "source identity changed")
    require(value["policy"] == {path: value["source"]["files"][path]["sha256"] for path in policies(architecture)},
            "source policy binding differs")
    verify_source_copy(work / "application-source", value["source"])


def complete(repository, work, architecture):
    value = read_json(work / "application-inputs.json")
    verify_inputs(repository, work, value, architecture)
    value["binary"] = binary_record(work, architecture)
    write_json(work / "application-binding.json", value)


def installed_binary(target):
    root = files.open_directory(Path(target) / "usr/bin")
    pinned = None
    try:
        # Inspect permissions without opening a substituted device or FIFO.
        # Revalidate this pinned inode and the directory entry around the
        # shared bounded reader; no root ownership is assumed before fakeroot.
        pinned = os.open("hhpt", os.O_PATH | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=root)
        before = os.fstat(pinned)
        require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and stat.S_IMODE(before.st_mode) == 0o755,
                "installed application must be a regular executable with mode 0755")
        record, _ = files.regular_bytes(root, "hhpt", MAX_BINARY_BYTES)
        after = os.fstat(pinned)
        current = os.stat("hhpt", dir_fd=root, follow_symlinks=False)
        fields = ("st_dev", "st_ino", "st_mode", "st_nlink", "st_uid", "st_gid", "st_size", "st_mtime_ns", "st_ctime_ns")
        require(all(getattr(before, field) == getattr(after, field) == getattr(current, field) for field in fields),
                "installed application metadata changed while reading")
        return record
    finally:
        if pinned is not None:
            os.close(pinned)
        os.close(root)


def verify(repository, work, architecture, overlay=False, installed_root=None):
    value = read_json(work / "application-binding.json")
    verify_inputs(repository, work, value, architecture)
    require(value["binary"] == binary_record(work, architecture), "application binary changed")
    if overlay:
        root = files.open_directory(work)
        try:
            _, digest = read_file(root, "application-overlay.sha256", 65)
        finally:
            os.close(root)
        require(digest == value["binary"]["sha256"].encode("ascii") + b"\n", "overlay application differs from bound binary")
        if architecture == "legacy-x86":
            require(installed_root is not None, "legacy receipt requires the installed application")
            require(installed_binary(installed_root) == {key: value["binary"][key] for key in ("size", "sha256")},
                    "installed application changed after its receipt")
    return value


def record_installed(repository, work, architecture, installed_root):
    require(architecture == "legacy-x86" and installed_root is not None, "unsupported installed application receipt")
    value = verify(repository, work, architecture)
    require(installed_binary(installed_root) == {key: value["binary"][key] for key in ("size", "sha256")},
            "installed application differs from bound binary")
    root = files.open_directory(work)
    try:
        descriptor = os.open("application-overlay.sha256", os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                             0o600, dir_fd=root)
        with os.fdopen(descriptor, "wb") as stream:
            stream.write(value["binary"]["sha256"].encode("ascii") + b"\n")
    finally:
        os.close(root)


def source_epoch(repository, work, architecture):
    require(architecture == "legacy-x86", "source epoch is only used by the legacy image")
    value = read_json(work / "application-inputs.json")
    verify_inputs(repository, work, value, architecture)
    return value["source"]["commit_epoch"]


class RedactedParser(argparse.ArgumentParser):
    def error(self, message):
        raise ValueError("unsupported binding arguments")


def main():
    parser = RedactedParser(description=__doc__)
    parser.add_argument("command", choices=("prepare", "complete", "verify", "source-epoch", "record-installed"))
    parser.add_argument("--repository", type=Path, required=True)
    parser.add_argument("--work", type=Path, required=True)
    parser.add_argument("--arch", choices=sorted(ARCHITECTURES), required=True)
    parser.add_argument("--overlay", action="store_true")
    parser.add_argument("--installed-root", type=Path)
    arguments = parser.parse_args()
    require(not arguments.overlay or arguments.command == "verify", "overlay check requires completed binding")
    require(arguments.installed_root is None or arguments.arch == "legacy-x86" and
            (arguments.command == "record-installed" or arguments.command == "verify" and arguments.overlay),
            "installed target is only accepted for legacy application checks")
    repository, work = files.absolute_path(arguments.repository), files.absolute_path(arguments.work)
    if arguments.command == "prepare":
        prepare(repository, work, arguments.arch)
    elif arguments.command == "complete":
        complete(repository, work, arguments.arch)
    elif arguments.command == "record-installed":
        record_installed(repository, work, arguments.arch, arguments.installed_root)
    elif arguments.command == "source-epoch":
        print(source_epoch(repository, work, arguments.arch))
        return
    else:
        verify(repository, work, arguments.arch, arguments.overlay, arguments.installed_root)
    print("Image application binding: " + arguments.command + " passed; image extraction and qualification remain separate")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, TypeError, KeyError, UnicodeError, RecursionError, subprocess.SubprocessError):
        print("Modern application binding: missing, changed, or unsupported input; build blocked", file=sys.stderr)
        sys.exit(1)
