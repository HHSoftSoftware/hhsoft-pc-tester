#!/usr/bin/env python3
"""Inventory Alpine builder host inputs for review, or verify an approved inventory.

Candidates are not approvals. This is a bounded host-file/package gate, not
proof of a trusted kernel, immutable builder, hermetic build, or qualified image.
No installed program or configuration is executed while taking the inventory.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import posixpath
import re
import stat
import sys


MAX_FILES = 250000
MAX_FILE_BYTES = 512 * 1024 * 1024
MAX_TOTAL_BYTES = 16 * 1024 * 1024 * 1024
MAX_MANIFEST_BYTES = 64 * 1024 * 1024
LINUX_PINNING = sys.platform.startswith("linux") and hasattr(os, "O_PATH")
SYSTEM_PATH = ("/usr/sbin", "/usr/bin", "/sbin", "/bin")
RUNTIME_TREES = ("/bin", "/sbin", "/lib", "/usr/bin", "/usr/sbin", "/usr/lib",
                 "/usr/libexec", "/usr/include", "/usr/share", "/usr/local/lib")
CONFIG_TREES = ("/etc/apk/keys", "/etc/apk/protected_paths.d", "/etc/alternatives",
                "/etc/mkinitfs", "/etc/depmod.d", "/etc/modprobe.d", "/etc/ssl/certs",
                "/etc/ssl/openssl.cnf.d")
CONFIG_FILES = ("/etc/alpine-release", "/etc/apk/arch", "/etc/apk/world",
                "/etc/apk/repositories", "/etc/mtools.conf", "/etc/ssl/openssl.cnf",
                "/etc/ssl/ct_log_list.cnf", "/etc/ssl/cert.pem", "/etc/wgetrc",
                "/usr/local/etc/wgetrc", "/etc/gnupg/common.conf", "/etc/gnupg/gpg.conf")
MUSL_LOADER_FILES = ("/etc/ld-musl-x86_64.path", "/etc/ld-musl-i386.path",
                     "/etc/ld-musl-aarch64.path")
PRIVATE_LINKS = {"/usr/lib/ssl/private": "/etc/ssl/private"}
EXCLUDED = ("/lib/modules", "/lib/firmware", "/usr/lib/modules", "/usr/lib/firmware")
FORBIDDEN_CONFIG = ("/etc/ld.so.preload", "/etc/ld.so.conf", "/etc/ld.so.conf.d",
                    "/etc/gitconfig", "/etc/apk/repositories.d")
COMMON_TOOLS = ("sh", "env", "id", "awk", "sed", "grep", "find", "sort", "cut", "tr",
                "cat", "cp", "mv", "rm", "mkdir", "mktemp", "chmod", "chown", "ln",
                "readlink", "tar", "gzip", "sha256sum", "sha1sum", "git", "openssl", "python3")
PROFILE_TOOLS = {
    "alpine-image": COMMON_TOOLS + ("apk", "go", "fakeroot", "update-kernel", "mkinitfs", "mksquashfs",
                                    "modinfo", "depmod", "xorrisofs", "grub-mkimage", "mformat",
                                    "mcopy", "scanelf", "lddtree", "unshare"),
    "buildroot-image": COMMON_TOOLS + ("apk", "go", "make", "gcc", "g++", "ld", "ar", "as",
                                       "nm", "objcopy", "objdump", "strip", "bash", "perl",
                                       "patch", "xz", "gpg", "bzip2", "cpio", "rsync", "wget"),
}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def within(path, prefix):
    return path == prefix or path.startswith(prefix + "/")


def valid_path(path):
    require(isinstance(path, str) and len(path) <= 4096 and path.startswith("/") and
            not path.startswith("//") and
            "\\" not in path and not any(ord(char) < 32 or ord(char) == 127 for char in path) and
            posixpath.normpath(path) == path, "invalid host input path")
    return path


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True, allow_nan=False)


class RedactedParser(argparse.ArgumentParser):
    def error(self, message):
        raise ValueError("invalid builder manifest arguments")


def identity(info):
    if os.name == "nt":
        # Windows stat derives executable bits from the pathname, while fstat
        # does not; their ctime fields also have different creation-time legacy
        # semantics. This portability path is only for candidate fixtures.
        return (info.st_dev, info.st_ino, info.st_mode & ~0o111, info.st_uid, info.st_gid,
                info.st_nlink, info.st_size, info.st_mtime_ns)
    return (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid,
            info.st_nlink, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


class HostInventory:
    def __init__(self, root):
        self.root = Path(root).resolve()
        self.files = {}
        self.total_bytes = 0
        self.hardlinks = {}
        self.loader_files = list(MUSL_LOADER_FILES)

    def local(self, path):
        return self.root / valid_path(path).lstrip("/")

    def covered(self, path):
        return (any(within(path, prefix) for prefix in RUNTIME_TREES + CONFIG_TREES) or
                path in CONFIG_FILES or path in self.loader_files)

    def ancestor(self, path):
        return path == "/" or any(prefix.startswith(path + "/") for prefix in
                                  RUNTIME_TREES + CONFIG_TREES + CONFIG_FILES + tuple(self.loader_files))

    def pin(self, path):
        # Walk directory descriptors without following *any* symlink. resolve()
        # translates allowed links first; a concurrent replacement then fails
        # closed instead of opening a device or a file outside the boundary.
        descriptor = os.open(self.root, os.O_PATH | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            parts = valid_path(path).lstrip("/").split("/") if path != "/" else []
            for index, part in enumerate(parts):
                child = os.open(part, os.O_PATH | os.O_NOFOLLOW, dir_fd=descriptor)
                os.close(descriptor)
                descriptor = child
                if index != len(parts) - 1:
                    require(stat.S_ISDIR(os.fstat(descriptor).st_mode), "host input parent changed")
            return descriptor
        except BaseException:
            os.close(descriptor)
            raise

    def info(self, path):
        if not LINUX_PINNING:
            return self.local(path).lstat()  # Portable fixture creation only.
        descriptor = self.pin(path)
        try:
            return os.fstat(descriptor)
        finally:
            os.close(descriptor)

    def no_xattrs(self, path, expected):
        # The portable branch supports synthetic fixtures only. Production
        # Linux must inspect metadata safely; an unsupported operation fails.
        if not LINUX_PINNING:
            return
        require(hasattr(os, "listxattr"), "host extended attribute inspection is unsupported")
        descriptor = self.pin(path)
        readable = parent = None
        try:
            before = os.fstat(descriptor)
            require(identity(before) == identity(expected), "host metadata changed before inspection")
            if stat.S_ISLNK(before.st_mode):
                # O_PATH cannot service flistxattr. Pin the parent and inspect
                # only its leaf with llistxattr semantics, never the target.
                parent = self.pin(posixpath.dirname(path))
                require(stat.S_ISDIR(os.fstat(parent).st_mode), "host link parent changed")
                name = posixpath.basename(path)
                require(identity(os.stat(name, dir_fd=parent, follow_symlinks=False)) == identity(before),
                        "host metadata changed before inspection")
                attributes = os.listxattr("/proc/self/fd/" + str(parent) + "/" + name,
                                          follow_symlinks=False)
                require(identity(os.stat(name, dir_fd=parent, follow_symlinks=False)) == identity(before),
                        "host metadata changed during inspection")
            else:
                require(stat.S_ISREG(before.st_mode) or stat.S_ISDIR(before.st_mode),
                        "unsupported host metadata type")
                flags = os.O_RDONLY | os.O_NONBLOCK
                if stat.S_ISDIR(before.st_mode):
                    flags |= os.O_DIRECTORY
                readable = os.open("/proc/self/fd/" + str(descriptor), flags)
                require(identity(os.fstat(readable)) == identity(before), "host metadata changed before inspection")
                attributes = os.listxattr(readable)
                require(identity(os.fstat(readable)) == identity(before), "host metadata changed during inspection")
            # ACLs and file capabilities are included. Never retrieve attribute
            # values or expose attribute names in a manifest or diagnostic.
            require(not attributes, "host extended attributes are unsupported")
            require(identity(os.fstat(descriptor)) == identity(before) == identity(self.info(path)),
                    "host metadata changed during inspection")
        except (AttributeError, NotImplementedError):
            raise ValueError("host extended attribute inspection is unsupported") from None
        finally:
            for opened in (readable, parent, descriptor):
                if opened is not None:
                    os.close(opened)

    def link(self, path):
        if not LINUX_PINNING:
            return os.readlink(self.local(path))
        descriptor = self.pin(path)
        try:
            info = os.fstat(descriptor)
            require(stat.S_ISLNK(info.st_mode), "host input link changed")
            self.no_xattrs(path, info)
            return os.readlink("", dir_fd=descriptor)
        finally:
            os.close(descriptor)

    def children(self, path):
        def names(directory):
            result = []
            with os.scandir(directory) as entries:
                for entry in entries:
                    result.append(entry.name)
                    require(len(result) <= MAX_FILES, "host inventory file count exceeds limit")
            return sorted(result)
        if not LINUX_PINNING:
            require(not self.local(path).is_symlink(), "host input directory changed")
            return names(self.local(path))
        descriptor = self.pin(path)
        try:
            before = os.fstat(descriptor)
            require(stat.S_ISDIR(before.st_mode), "host input directory changed")
            self.no_xattrs(path, before)
            readable = os.open("/proc/self/fd/" + str(descriptor), os.O_RDONLY | os.O_DIRECTORY)
            try:
                require(identity(os.fstat(readable)) == identity(before), "host input directory changed")
                result = names(readable)
                require(identity(os.fstat(readable)) == identity(before), "host input directory changed")
                return result
            finally:
                os.close(readable)
        finally:
            os.close(descriptor)

    def excluded(self, path):
        return (any(within(path, prefix) for prefix in EXCLUDED + tuple(PRIVATE_LINKS)) or
                self.private_name(path))

    @staticmethod
    def private_name(path):
        return any(part.endswith(".key") or part in {"id_rsa", "id_ed25519", "id_ecdsa", "id_dsa"}
                   for part in path.split("/"))

    def resolve(self, path, allow_ancestor=False):
        # Absolute links are interpreted inside --root, never the invoking host.
        parts = valid_path(path).lstrip("/").split("/") if path != "/" else []
        resolved, count = [], 0
        while parts:
            part = parts.pop(0)
            if part in {"", "."}:
                continue
            if part == "..":
                if resolved:
                    resolved.pop()
                continue
            resolved.append(part)
            current = "/" + "/".join(resolved)
            # Kernel pathname traversal resolves each symlink before processing
            # the following '..'. Lexically normalizing an entire target first
            # could inspect a different file from the one the builder executes.
            require((self.covered(current) or self.ancestor(current)) and not self.excluded(current),
                    "host input symlink leaves inventoried runtime inputs")
            info = self.info(current)
            if stat.S_ISLNK(info.st_mode):
                count += 1
                require(count <= 40, "host input symlink loop")
                target = self.link(current)
                require(identity(self.info(current)) == identity(info), "host input link changed")
                require(target and len(target) <= 4096 and "\\" not in target and
                        not target.startswith("//") and
                        not any(ord(char) < 32 or ord(char) == 127 for char in target), "invalid link target")
                resolved = [] if target.startswith("/") else resolved[:-1]
                parts = target.split("/") + parts
            elif parts:
                require(stat.S_ISDIR(info.st_mode), "host input parent is not a directory")
        result = "/" + "/".join(resolved)
        require((self.covered(result) or (allow_ancestor and self.ancestor(result))) and
                not self.excluded(result), "uncovered host input")
        return result

    def bytes(self, path, limit=MAX_FILE_BYTES):
        resolved = self.resolve(path)
        before = self.info(resolved)
        require(stat.S_ISREG(before.st_mode) and before.st_size <= limit, "invalid host input file")
        self.no_xattrs(resolved, before)
        flags = os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0) | getattr(os, "O_BINARY", 0) | getattr(os, "O_NONBLOCK", 0)
        if LINUX_PINNING:
            pinned = self.pin(resolved)
            try:
                require(identity(os.fstat(pinned)) == identity(before) and
                        stat.S_ISREG(os.fstat(pinned).st_mode), "host input changed before reading")
                # O_PATH never performs device I/O. Only a pinned regular file
                # is reopened for data, even if its pathname changes now.
                descriptor = os.open("/proc/self/fd/" + str(pinned), os.O_RDONLY | os.O_NONBLOCK)
            finally:
                os.close(pinned)
        else:
            descriptor = os.open(self.local(resolved), flags)
        with os.fdopen(descriptor, "rb") as stream:
            require(identity(os.fstat(stream.fileno())) == identity(before), "host input changed before reading")
            content = stream.read(limit + 1)
            after = os.fstat(stream.fileno())
        require(len(content) == before.st_size and len(content) <= limit and
                identity(before) == identity(after) == identity(self.info(resolved)),
                "host input changed while reading")
        return content

    def add(self, path, optional=False):
        valid_path(path)
        if path in self.files or any(within(path, prefix) for prefix in EXCLUDED):
            return
        require(len(self.files) < MAX_FILES, "host inventory file count exceeds limit")
        require(not self.private_name(path), "private-key input is outside inventory scope")
        # Validate parents before even listing a directory or inspecting a
        # configuration child; an allowed leaf must not hide an escaping parent.
        try:
            self.resolve(posixpath.dirname(path), allow_ancestor=True)
            info = self.info(path)
        except FileNotFoundError:
            require(optional, "host input missing")
            self.files[path] = {"type": "absent"}
            return
        record = {"mode": stat.S_IMODE(info.st_mode), "uid": info.st_uid, "gid": info.st_gid}
        self.files[path] = record
        if stat.S_ISLNK(info.st_mode):
            target = self.link(path)
            require(len(target) <= 4096 and "\\" not in target and
                    not any(ord(char) < 32 or ord(char) == 127 for char in target), "invalid link target")
            record.update(type="symlink", target=target)
            if path in PRIVATE_LINKS:
                require(target in {PRIVATE_LINKS[path], posixpath.relpath(PRIVATE_LINKS[path], posixpath.dirname(path))},
                        "unexpected private-directory link")
                require(identity(self.info(path)) == identity(info), "host input changed while inventorying")
                return  # Deliberately never stat or read the private destination.
            self.resolve(path)
        elif stat.S_ISDIR(info.st_mode):
            require(path not in PRIVATE_LINKS, "private input directory is outside inventory scope")
            record.update(type="directory")
            for child in self.children(path):
                self.add(path + "/" + child)
        elif stat.S_ISREG(info.st_mode):
            require(path not in PRIVATE_LINKS and not path.endswith(".key") and
                    posixpath.basename(path) not in {"id_rsa", "id_ed25519", "id_ecdsa", "id_dsa"},
                    "private-key input is outside inventory scope")
            content = self.bytes(path)
            self.total_bytes += len(content)
            require(self.total_bytes <= MAX_TOTAL_BYTES, "host inventory byte limit exceeded")
            inode = (info.st_dev, info.st_ino)
            record.update(type="file", size=len(content), sha256=hashlib.sha256(content).hexdigest(),
                          links=info.st_nlink)
            if info.st_nlink > 1:
                record["hardlink_group"] = self.hardlinks.setdefault(inode, path)
        else:
            raise ValueError("special host input file is unsupported")
        require(identity(self.info(path)) == identity(info), "host input changed while inventorying")

    def parent_metadata(self):
        parents = {"/"}
        for path in RUNTIME_TREES + CONFIG_TREES + CONFIG_FILES + tuple(self.loader_files):
            parent = posixpath.dirname(path)
            while parent != "/":
                if not self.covered(parent):
                    parents.add(parent)
                parent = posixpath.dirname(parent)
        for path in sorted(parents):
            try:
                info = self.info(path)
            except FileNotFoundError:
                self.files[path] = {"type": "absent"}
                continue
            require(stat.S_ISDIR(info.st_mode), "unsupported host runtime parent")
            self.no_xattrs(path, info)
            self.files[path] = {"type": "directory", "mode": stat.S_IMODE(info.st_mode),
                                "uid": info.st_uid, "gid": info.st_gid}

    def configuration(self):
        for path in ("/etc", "/etc/apk"):
            require(stat.S_ISDIR(self.info(path).st_mode), "invalid host configuration directory")
        for path in FORBIDDEN_CONFIG:
            require(not os.path.lexists(self.local(path)), "unsupported host execution configuration")
        self.loader_files = sorted(set(MUSL_LOADER_FILES) | {"/etc/" + name for name in self.children("/etc")
                                   if re.fullmatch(r"ld-musl-[A-Za-z0-9_-]+\.path", name)})
        for path in self.loader_files:
            try:
                self.info(path)
            except FileNotFoundError:
                continue
            text = self.bytes(path, 65536).decode("utf-8")
            for directory in re.split(r"[:\n]", text):
                directory = directory.strip()
                if directory:
                    valid_path(directory)
                    require(any(within(directory, prefix) for prefix in RUNTIME_TREES),
                            "loader searches outside inventoried runtime inputs")
                    resolved = self.resolve(directory)
                    require(any(within(resolved, prefix) for prefix in RUNTIME_TREES) and
                            not self.excluded(resolved) and stat.S_ISDIR(self.info(resolved).st_mode),
                            "loader searches outside inventoried runtime inputs")
        require(set(self.children("/etc/apk")) <=
                {"arch", "world", "repositories", "keys", "protected_paths.d", "cache"},
                "unexpected host APK configuration")
        # Host download caches are transient and are not used by explicit target
        # --root operations. Runtime and package identity come from installed DB.
        for path in CONFIG_FILES + tuple(self.loader_files):
            self.add(path, optional=path not in {"/etc/alpine-release", "/etc/apk/arch", "/etc/apk/world"})
        for path in CONFIG_TREES:
            self.add(path, optional=True)


def installed_packages(content):
    require(len(content) <= 64 * 1024 * 1024, "installed APK database too large")
    packages, names = [], set()
    for entry in content.decode("utf-8").split("\n\n"):
        if not entry.strip():
            continue
        fields = {}
        for line in entry.splitlines():
            if len(line) >= 2 and line[1] == ":" and line[0] in "PVADp":
                require(line[0] not in fields, "duplicate installed APK identity field")
                fields[line[0]] = line[2:]
        require(all(field in fields for field in ("P", "V", "A")), "incomplete installed APK identity")
        require(all(re.fullmatch(r"[A-Za-z0-9_+~.-]+", fields[field]) for field in ("P", "V", "A")),
                "invalid installed APK identity")
        require(fields["P"] not in names, "duplicate installed APK package")
        for field in ("D", "p"):
            # APK records the POSIX test command as cmd:[ (and BusyBox's
            # related command as cmd:[[). Brackets are not general token syntax.
            require(all(re.fullmatch(r"[A-Za-z0-9_+~.!=<>:/@-]+", value) or
                        re.fullmatch(r"cmd:\[{1,2}(?:=[A-Za-z0-9_+~.-]+)?", value)
                        for value in fields.get(field, "").split()), "invalid installed APK dependency")
        names.add(fields["P"])
        packages.append({"name": fields["P"], "version": fields["V"], "architecture": fields["A"],
                         "dependencies": fields.get("D", "").split(), "provides": fields.get("p", "").split()})
        require(len(packages) <= 20000, "installed APK package count exceeds limit")
    require(packages, "installed APK database empty")
    return sorted(packages, key=lambda package: package["name"])


def policy_bytes(path, limit=65536):
    path = Path(path).absolute()
    # Explicit policy files are the only reads outside the fixed host boundary.
    # Keep the same Linux pin/type gate even for these small, supplied inputs.
    host = HostInventory(path.parent)
    leaf = "/" + path.name
    host.loader_files = [leaf]
    require(stat.S_ISREG(host.info(leaf).st_mode), "invalid builder policy input")
    return host.bytes(leaf, limit)


def create_manifest(root, profile, base_lock, go_version):
    require(profile in PROFILE_TOOLS, "unsupported builder profile")
    host = HostInventory(root)
    host.configuration()
    host.parent_metadata()
    architecture = host.bytes("/etc/apk/arch", 64).decode("ascii").strip()
    require(architecture in {"x86", "x86_64", "aarch64"}, "unsupported Alpine builder architecture")
    database = host.bytes("/lib/apk/db/installed", 64 * 1024 * 1024)
    packages = installed_packages(database)
    require(all(package["architecture"] in {architecture, "noarch"} for package in packages),
            "installed APK architecture differs from builder")
    go_policy = policy_bytes(go_version, 64)
    go_pin = go_policy.decode("ascii").strip()
    require(re.fullmatch(r"[1-9][0-9]*\.[0-9]+\.[0-9]+", go_pin), "invalid pinned Go version")
    require(any(package["name"] == "go" and
                re.fullmatch(re.escape(go_pin) + r"-r[0-9]+", package["version"])
                for package in packages), "host Go package does not match the repository Go version")
    for path in RUNTIME_TREES:
        host.add(path, optional=True)
    for path, content in (("/lib/apk/db/installed", database),
                          ("/etc/apk/arch", host.bytes("/etc/apk/arch", 64))):
        record = host.files.get(host.resolve(path), {})
        require(record.get("type") == "file" and record.get("sha256") == hashlib.sha256(content).hexdigest(),
                "host package policy changed or unrecorded while inventorying")
    tools = {}
    for tool in PROFILE_TOOLS[profile]:
        for directory in SYSTEM_PATH:
            path = directory + "/" + tool
            try:
                resolved = host.resolve(path)
            except FileNotFoundError:
                continue
            info = host.info(resolved)
            require(stat.S_ISREG(info.st_mode) and info.st_mode & 0o111, "builder tool is not executable")
            require(resolved in host.files and host.files[resolved]["type"] == "file", "builder tool is unrecorded")
            tools[tool] = {"path": path, "resolved": resolved}
            break
        require(tool in tools, "required builder tool missing")
    return {"schema": 1, "profile": profile, "host_architecture": architecture,
            "policy": {"inventory_sha256": hashlib.sha256(policy_bytes(__file__, 1024 * 1024)).hexdigest(),
                       "base_lock_sha256": hashlib.sha256(policy_bytes(base_lock)).hexdigest(),
                       "go_version_sha256": hashlib.sha256(go_policy).hexdigest(), "go_version": go_pin},
            "installed_apk_database_sha256": hashlib.sha256(database).hexdigest(),
            "packages": packages, "tools": tools, "files": dict(sorted(host.files.items()))}


def read_manifest(path):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            require(key not in result, "duplicate manifest field")
            result[key] = value
        return result
    def invalid_number(value):
        raise ValueError("noninteger manifest number")
    return json.loads(policy_bytes(path, MAX_MANIFEST_BYTES).decode("utf-8"), object_pairs_hook=unique,
                      parse_float=invalid_number, parse_constant=invalid_number)


def verify_manifest(manifest, profile, base_lock, go_version, root=Path("/")):
    approved = read_manifest(manifest)
    actual = create_manifest(root, profile, base_lock, go_version)
    require(canonical(approved) == canonical(actual), "builder differs from the reviewed host manifest")


def classify_version(version_file):
    version = policy_bytes(version_file, 256).decode("ascii").removesuffix("\n").removesuffix("\r")
    core = r"(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)"
    identifiers = r"[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*"
    parsed = re.fullmatch(core + r"(?:-(" + identifiers + r"))?(?:\+(" + identifiers + r"))?", version)
    require(parsed is not None, "invalid product version")
    if parsed.group(1):
        require(all(not (part.isdigit() and len(part) > 1 and part.startswith("0"))
                    for part in parsed.group(1).split(".")), "invalid product prerelease version")
    return "development" if re.fullmatch(core + r"-dev\.(?:0|[1-9][0-9]*)", version) else "release"


def actual_host_architecture():
    require(LINUX_PINNING, "production host verification requires Linux descriptor pinning")
    architecture = HostInventory(Path("/")).bytes("/etc/apk/arch", 64).decode("ascii").strip()
    require(architecture in {"x86", "x86_64", "aarch64"}, "unsupported Alpine builder architecture")
    return architecture


def gate(repository, profile):
    require(profile in PROFILE_TOOLS, "unsupported builder profile")
    repository = Path(repository).resolve()
    if classify_version(repository / "VERSION") == "development":
        print("Development image: builder host remains unqualified")
        return
    architecture = actual_host_architecture()
    platform = {"alpine-image": "alpine", "buildroot-image": "buildroot"}[profile]
    manifest = repository / "platform" / "builder-hosts" / (profile + "-" + architecture + ".json")
    verify_manifest(manifest, profile, repository / "platform" / platform / "base.lock", repository / ".go-version")
    print("Builder host matches reviewed manifest; full toolchain qualification remains required")


def main():
    parser = RedactedParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    candidate = commands.add_parser("candidate")
    candidate.add_argument("--root", type=Path, default=Path("/"), help="fixture root; not available to production verification")
    verify = commands.add_parser("verify")
    classify = commands.add_parser("classify-version")
    classify.add_argument("--version-file", type=Path, required=True)
    build_gate = commands.add_parser("gate")
    build_gate.add_argument("--repository", type=Path, required=True)
    build_gate.add_argument("--profile", choices=sorted(PROFILE_TOOLS), required=True)
    for command in (candidate, verify):
        command.add_argument("--profile", choices=sorted(PROFILE_TOOLS), required=True)
        command.add_argument("--base-lock", type=Path, required=True)
        command.add_argument("--go-version", type=Path, required=True)
        command.add_argument("--manifest", type=Path, required=True)
    args = parser.parse_args()
    if args.command == "classify-version":
        print(classify_version(args.version_file))
    elif args.command == "gate":
        gate(args.repository, args.profile)
    elif args.command == "candidate":
        payload = create_manifest(args.root, args.profile, args.base_lock, args.go_version)
        encoded = json.dumps(payload, indent=2, sort_keys=True) + "\n"
        require(len(encoded.encode()) <= MAX_MANIFEST_BYTES, "candidate manifest too large")
        with args.manifest.open("x", encoding="utf-8", newline="\n") as stream:
            stream.write(encoded)
        print("Builder host candidate created; independent review and qualification are still required")
    else:
        require(LINUX_PINNING, "production host verification requires Linux descriptor pinning")
        verify_manifest(args.manifest, args.profile, args.base_lock, args.go_version)
        print("Builder host matches reviewed manifest")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, TypeError, UnicodeError, RecursionError):
        print("Builder host manifest: missing, unsupported, or changed input; build blocked", file=sys.stderr)
        sys.exit(1)
