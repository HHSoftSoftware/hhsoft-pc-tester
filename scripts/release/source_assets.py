"""Validate bounded source payloads and image inspection records, without extraction.

Source archives inside the canonical tar parts remain opaque. Their contents,
licensing and privacy must be reviewed by the authenticated lab separately.
"""

from contextlib import contextmanager
import csv
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import tarfile

ARCHES = ("x86_64", "x86", "legacy-x86", "aarch64")
MAX_PART = 1 << 30
MAX_PARTS = 16
MAX_TEXT = 4 << 20
MAX_MEMBERS = 20000
MAX_PROJECT = 64 << 20
HASH = re.compile(r"[0-9a-f]{64}")
TOKEN = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.+:-]{0,255}")
CSV_FIELDS = ("PACKAGE", "VERSION", "LICENSE", "LICENSE FILES", "SOURCE ARCHIVE", "SOURCE SITE", "DEPENDENCIES WITH LICENSES")
ROLES = {"project-source", "source-archive", "source-file", "license-text", "build-config", "build-patch", "build-script"}


def require(value, message="invalid release source or inspection evidence"):
    if not value:
        raise ValueError(message)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True, allow_nan=False).encode("ascii")


def sha(data):
    return hashlib.sha256(data).hexdigest()


def fields(value, expected):
    require(isinstance(value, dict) and set(value) == set(expected))


def integer(value, low, high):
    require(type(value) is int and low <= value <= high)
    return value


def identity(info):
    # Windows pathname/handle metadata can disagree about executable bits and
    # creation/change time. That branch supports portable fixtures only; the
    # publisher runs on Linux and compares the complete mode and change time.
    mode = stat.S_IFMT(info.st_mode) if os.name == "nt" else info.st_mode
    changed = 0 if os.name == "nt" else info.st_ctime_ns
    return (info.st_dev, info.st_ino, mode, info.st_nlink, info.st_size, info.st_mtime_ns, changed)


@contextmanager
def regular(path, maximum=2 * MAX_PART - 1):
    """Pin the regular Linux inode before any data open; fixtures work elsewhere."""
    path = Path(path)
    pinned = None
    stream = None
    try:
        if hasattr(os, "O_PATH"):
            pinned = os.open(path, os.O_PATH | os.O_NOFOLLOW)
            before = os.fstat(pinned)
            require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1)
            integer(before.st_size, 0, maximum)
            descriptor = os.open("/proc/self/fd/" + str(pinned), os.O_RDONLY)
            stream = os.fdopen(descriptor, "rb")
        else:
            before = path.lstat()
            require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and
                    not getattr(before, "st_file_attributes", 0) & 0x400)
            integer(before.st_size, 0, maximum)
            stream = path.open("rb")
        require(identity(before) == identity(os.fstat(stream.fileno())))
        yield stream, before.st_size
        require(identity(before) == identity(os.fstat(stream.fileno())) == identity(path.lstat()))
    finally:
        if stream is not None:
            stream.close()
        if pinned is not None:
            os.close(pinned)


def read_bytes(path, maximum=MAX_TEXT):
    with regular(path, maximum) as (stream, size):
        data = stream.read(size)
        require(len(data) == size and not stream.read(1))
        return data


def digest(path, maximum=2 * MAX_PART - 1):
    with regular(path, maximum) as (stream, size):
        result = hashlib.sha256()
        remaining = size
        while remaining:
            block = stream.read(min(1 << 20, remaining))
            require(block)
            result.update(block)
            remaining -= len(block)
        require(not stream.read(1))
        return result.hexdigest()


def read_json(path):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            require(key not in result)
            result[key] = value
        return result
    return json.loads(read_bytes(path), object_pairs_hook=unique,
                      parse_constant=lambda _: require(False))


def source_policy(root):
    path = root / "scripts/security/scan_source.py"
    spec = importlib.util.spec_from_file_location("release_source_policy", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def safe_name(name, policy, role):
    require(isinstance(name, str) and 0 < len(name) <= 240 and "\\" not in name)
    pieces = name.split("/")
    require(all(re.fullmatch(r"[A-Za-z0-9_.+-]{1,100}", part) and part not in {".", ".."} and
                not part.endswith(".") and not re.fullmatch(r"(?:CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])(?:\..*)?", part, re.I)
                for part in pieces))
    require(pieces[0] == ("project" if role == "project-source" else "sources"))
    require(len(pieces) >= 2)
    checked = "/".join(pieces[1:])
    if role == "source-archive":
        # Only the archive suffix is exceptional. Agent/private names underneath
        # that suffix remain prohibited, as do every parent path component.
        suffix = re.search(r"(?:\.tar\.(?:gz|xz|bz2|zst)|\.tgz|\.zip|\.tar)$", checked)
        require(suffix is not None)
        checked = checked[:suffix.start()]
    require(not policy.forbidden_path(checked))
    return name


def part_names(version, count):
    integer(count, 1, MAX_PARTS)
    return [f"hhsoft-pc-tester-{version}.sources-{number:02d}.tar" for number in range(1, count + 1)]


def part_count(version, names):
    prefix = f"hhsoft-pc-tester-{version}.sources-"
    found = {name for name in names if name.startswith(prefix)}
    require(found == set(part_names(version, len(found))))
    return len(found)


def project_inventory(root, commit):
    """Compare actual public checkout bytes to raw tree IDs, without filters."""
    environment = {"PATH": os.environ.get("PATH", ""), "LC_ALL": "C", "GIT_CONFIG_NOSYSTEM": "1",
                   "GIT_CONFIG_GLOBAL": os.devnull, "GIT_NO_REPLACE_OBJECTS": "1", "GIT_NO_LAZY_FETCH": "1"}
    command = ["git", "--no-replace-objects", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=" + os.devnull,
               "-C", str(root), "ls-tree", "-r", "-l", "-z", "--full-tree", commit]
    raw = subprocess.run(command, env=environment, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                         timeout=30, check=True).stdout
    require(len(raw) <= MAX_TEXT)
    policy = source_policy(root)
    result, total = {}, 0
    entries = raw.rstrip(b"\0").split(b"\0")
    require(0 < len(entries) <= MAX_MEMBERS)
    for entry in entries:
        header, name = entry.split(b"\t", 1)
        mode, kind, object_id, size = header.split()
        require(mode in {b"100644", b"100755"} and kind == b"blob" and re.fullmatch(rb"[0-9a-f]{40}", object_id))
        require(re.fullmatch(rb"0|[1-9][0-9]*", size) is not None)
        total += integer(int(size), 0, 8 << 20)
        require(total <= MAX_PROJECT)
        name = name.decode("ascii")
        safe_name("project/" + name, policy, "project-source")
        require(name not in result)
        path = root
        for component in name.split("/"):
            path /= component
            require(not path.is_symlink() and not policy.filesystem_link(path))
        data = read_bytes(path, 8 << 20)
        require(str(len(data)).encode("ascii") == size)
        require(hashlib.sha1(b"blob " + size + b"\0" + data).hexdigest().encode("ascii") == object_id)
        result[name] = {"size": len(data), "sha256": sha(data), "mode": mode.decode("ascii")}
    require(0 < len(result) <= MAX_MEMBERS)
    return result


def inventories(candidate, version):
    result = {}
    for arch in ARCHES:
        image = f"hhsoft-pc-tester-{version}-{arch}.iso"
        data = read_bytes(candidate / (image + (".packages.csv" if arch == "legacy-x86" else ".packages.lock")))
        rows = []
        if arch == "legacy-x86":
            reader = csv.DictReader(io.StringIO(data.decode("utf-8-sig"), newline=""), strict=True)
            require(reader.fieldnames == list(CSV_FIELDS))
            for row in reader:
                fields(row, CSV_FIELDS)
                require(all(isinstance(value, str) and not any(ord(c) < 32 or ord(c) == 127 for c in value)
                            for value in row.values()))
                require(TOKEN.fullmatch(row["PACKAGE"]) and TOKEN.fullmatch(row["VERSION"]))
                rows.append(row)
        else:
            for line in data.decode("utf-8").splitlines():
                match = re.fullmatch(r"([^\s=]+)=([^\s]+) arch=([^\s]+) license=(.+) origin=([^\s]+) commit=([^\s]+) sha256=([0-9a-f]{64})", line)
                require(match is not None)
                row = dict(zip(("name", "version", "architecture", "license", "origin", "commit", "sha256"), match.groups()))
                require(TOKEN.fullmatch(row["name"]) and TOKEN.fullmatch(row["version"]) and
                        row["architecture"] in {arch, "noarch"} and not any(ord(c) < 32 or ord(c) == 127 for c in line))
                rows.append(row)
        names = [row["PACKAGE"] if arch == "legacy-x86" else row["name"] for row in rows]
        require(0 < len(rows) <= 4096 and len(names) == len(set(names)))
        product = [row for row, name in zip(rows, names) if name == "hhsoft-pc-tester"]
        require(len(product) == 1 and product[0]["VERSION" if arch == "legacy-x86" else "version"] == version)
        result[arch] = rows
    return result


def member_header(row):
    info = tarfile.TarInfo(row["path"])
    info.size, info.mode = row["size"], int(row["mode"], 8)
    return info.tobuf(format=tarfile.USTAR_FORMAT, encoding="ascii", errors="strict")


def verify_part(path, part, members):
    require(members and [row["path"] for row in members] == sorted(row["path"] for row in members))
    body_size = sum(512 + ((row["size"] + 511) // 512) * 512 for row in members)
    expected_size = ((body_size + 1024 + 10239) // 10240) * 10240
    require(part["size"] == expected_size)
    with regular(path, MAX_PART) as (stream, size):
        require(size == expected_size)
        total_hash = hashlib.sha256()
        def take(amount):
            data = stream.read(amount)
            require(len(data) == amount)
            total_hash.update(data)
            return data
        for row in members:
            require(take(512) == member_header(row))
            content_hash = hashlib.sha256()
            remaining = row["size"]
            while remaining:
                amount = min(remaining, 1 << 20)
                content_hash.update(take(amount))
                remaining -= amount
            require(content_hash.hexdigest() == row["sha256"])
            padding = (-row["size"]) % 512
            require(take(padding) == b"\0" * padding)
        ending = expected_size - body_size
        require(take(ending) == b"\0" * ending and not stream.read(1))
        require(total_hash.hexdigest() == part["sha256"])


def validate_sources(candidate, version, commit, root, project):
    manifest = read_json(candidate / "SOURCE-MANIFEST.json")
    fields(manifest, {"schema", "version", "source_commit", "parts", "files", "packages"})
    require(type(manifest["schema"]) is int and manifest["schema"] == 1 and
            manifest["version"] == version and manifest["source_commit"] == commit)
    parts, rows, packages = manifest["parts"], manifest["files"], manifest["packages"]
    require(isinstance(parts, list) and isinstance(rows, list) and 0 < len(rows) <= MAX_MEMBERS)
    expected_parts = part_names(version, len(parts))
    require({path.name for path in candidate.iterdir() if path.name.startswith(f"hhsoft-pc-tester-{version}.sources-")} == set(expected_parts))
    for part, name in zip(parts, expected_parts):
        fields(part, {"name", "size", "sha256"})
        require(part["name"] == name and isinstance(part["sha256"], str) and HASH.fullmatch(part["sha256"]))
        integer(part["size"], 10240, MAX_PART)
    policy = source_policy(root)
    files, case_names, directories, grouped = {}, set(), {}, {name: [] for name in expected_parts}
    for row in rows:
        fields(row, {"path", "role", "part", "size", "sha256", "mode"})
        require(row["role"] in ROLES and row["part"] in grouped and row["mode"] in {"0644", "0755"})
        name = safe_name(row["path"], policy, row["role"])
        require(name not in files and name.casefold() not in case_names and name.casefold() not in directories)
        components = name.split("/")
        for length in range(1, len(components)):
            parent = "/".join(components[:length])
            require(parent.casefold() not in case_names and directories.get(parent.casefold(), parent) == parent)
            directories[parent.casefold()] = parent
        integer(row["size"], 0, MAX_PART - 1536)
        require(isinstance(row["sha256"], str) and HASH.fullmatch(row["sha256"]))
        files[name] = row
        case_names.add(name.casefold())
        grouped[row["part"]].append(row)
    require([row["path"] for row in rows] == sorted(files))
    project_rows = {name.removeprefix("project/"): row for name, row in files.items() if row["role"] == "project-source"}
    require(set(project_rows) == set(project))
    for name, observed in project.items():
        row = project_rows[name]
        require(row["size"] == observed["size"] and row["sha256"] == observed["sha256"] and
                row["mode"] == ("0755" if observed["mode"] == "100755" else "0644"))
    for part in parts:
        verify_part(candidate / part["name"], part, grouped[part["name"]])
    observed = inventories(candidate, version)
    fields(packages, ARCHES)
    notices = read_bytes(candidate / "THIRD-PARTY-NOTICES.txt")
    require(notices.strip())
    notices.decode("utf-8")
    used = set("project/" + name for name in project)
    for arch, inventory in observed.items():
        entries = packages[arch]
        require(isinstance(entries, list) and len(entries) == len(inventory))
        actual, wanted = set(), {canonical(row) for row in inventory}
        for entry in entries:
            fields(entry, {"inventory", "source_form", "sources", "licenses", "build", "notice", "review"})
            key = canonical(entry["inventory"])
            require(key in wanted and key not in actual)
            actual.add(key)
            # Both forms require included material. There is deliberately no
            # unavailable/not-required disposition that can skip fulfillment.
            require(entry["source_form"] in {"preferred-source", "upstream-binary-form"})
            require(isinstance(entry["review"], str) and 0 < len(entry["review"].strip()) <= 2048)
            for field, roles in (("sources", {"project-source", "source-archive", "source-file"}),
                                 ("licenses", {"project-source", "license-text"}),
                                 ("build", {"project-source", "source-archive", "build-config", "build-patch", "build-script"})):
                refs = entry[field]
                require(isinstance(refs, list) and 0 < len(refs) <= 4096 and len(refs) == len(set(refs)))
                require(all(name in files and files[name]["role"] in roles for name in refs))
                used.update(refs)
            notice = entry["notice"]
            fields(notice, {"offset", "size", "sha256"})
            start = integer(notice["offset"], 0, len(notices))
            length = integer(notice["size"], 1, len(notices) - start)
            block = notices[start:start + length]
            require(block.strip() and sha(block) == notice["sha256"])
            block.decode("utf-8")
        require(actual == wanted)
    require(used == set(files))
    return manifest, observed


def validate_sbom(data, inventory, arch, image, version, image_hash):
    """Check the stable tag/value inventory emitted by our two generators."""
    document, packages, relationships, current = {}, [], [], None
    for line in data.decode("utf-8").splitlines():
        if not line:
            continue
        require(": " in line and not any(ord(c) < 32 or ord(c) == 127 for c in line))
        key, value = line.split(": ", 1)
        if key == "PackageName":
            current = {}
            packages.append(current)
        destination = document if current is None else current
        if key == "Relationship":
            relationships.append(value)
            continue
        if key == "Creator" and current is None:
            document.setdefault(key, []).append(value)
            continue
        require(key not in destination)
        destination[key] = value
    require(document.get("SPDXVersion") == "SPDX-2.3" and packages)
    first, rows = packages[0], packages[1:]
    require(first.get("PackageName") == "hhsoft-pc-tester-" + arch + "-image" and
            first.get("SPDXID") == "SPDXRef-Image" and first.get("PackageVersion") == version and
            first.get("PackageFileName") == image and first.get("PackageChecksum") == "SHA256: " + image_hash and
            first.get("PackageLicenseDeclared") == first.get("PackageLicenseConcluded") == "NOASSERTION")
    expected = {(row["PACKAGE"], row["VERSION"]): row for row in inventory} if arch == "legacy-x86" else {
        (row["name"], row["version"]): row for row in inventory}
    seen, identifiers = set(), {"SPDXRef-Image", "SPDXRef-DOCUMENT"}
    expected_relationships = {"SPDXRef-DOCUMENT DESCRIBES SPDXRef-Image"}
    for row in rows:
        identity = (row.get("PackageName"), row.get("PackageVersion"))
        require(identity in expected and identity not in seen)
        seen.add(identity)
        identifier = row.get("SPDXID")
        require(isinstance(identifier, str) and re.fullmatch(r"SPDXRef-[A-Za-z0-9.-]+", identifier) and identifier not in identifiers)
        identifiers.add(identifier)
        declared = "BSD-3-Clause" if identity[0] == "hhsoft-pc-tester" else "NOASSERTION"
        require(row.get("PackageLicenseDeclared") == declared and row.get("PackageLicenseConcluded") == "NOASSERTION")
        original = expected[identity]
        if arch != "legacy-x86":
            require(row.get("PackageChecksum") == "SHA256: " + original["sha256"])
            comment = ("consumed build input; arch=" + original["architecture"] + " origin=" + original["origin"] +
                       " source-commit=" + original["commit"] + " declared-license=" + original["license"])
            relationship = "GENERATED_FROM"
        else:
            comment = "Buildroot declared licenses: " + original["LICENSE"] + "; license files: " + original["LICENSE FILES"]
            relationship = "CONTAINS"
        require(row.get("PackageComment") == comment)
        expected_relationships.add("SPDXRef-Image " + relationship + " " + identifier)
    require(seen == set(expected))
    require(len(relationships) == len(expected_relationships) and set(relationships) == expected_relationships)


def validate_inspection(candidate, image, arch, version, commit, root, project, inventory):
    value = read_json(candidate / (image + ".inspection.json"))
    fields(value, {"schema", "source_commit", "version", "architecture", "image_sha256", "application", "firmware"})
    require(type(value["schema"]) is int and value["schema"] == 1 and value["source_commit"] == commit and
            value["version"] == version and value["architecture"] == arch and value["image_sha256"] == digest(candidate / image))
    application = value["application"]
    fields(application, {"path", "size", "sha256", "mode", "uid", "gid", "version", "go_version", "target_os", "target_arch", "cpu_policy"})
    integer(application["size"], 64, 128 << 20)
    require(isinstance(application["sha256"], str) and HASH.fullmatch(application["sha256"]))
    target, cpu = {"x86_64": ("amd64", "v1"), "x86": ("386", "sse2"),
                   "legacy-x86": ("386", "softfloat"), "aarch64": ("arm64", "v8.0")}[arch]
    require(application["path"] == "/usr/bin/hhpt" and application["mode"] == "0755" and
            type(application["uid"]) is int and application["uid"] == 0 and
            type(application["gid"]) is int and application["gid"] == 0 and
            application["version"] == version and application["go_version"] == read_bytes(root / ".go-version").decode("ascii").strip() and
            application["target_os"] == "linux" and application["target_arch"] == target and application["cpu_policy"] == cpu)
    if arch != "legacy-x86":
        require(next(row for row in inventory if row["name"] == "hhsoft-pc-tester")["sha256"] == application["sha256"])
    binding = read_json(candidate / (image + ".application.json"))
    fields(binding, {"schema", "source_commit", "version", "architecture", "go_version", "host_manifest", "project_files_sha256", "binary"})
    expected_project = {name: {key: row[key] for key in ("size", "sha256")} for name, row in project.items()}
    require(type(binding["schema"]) is int and binding["schema"] == 1 and binding["source_commit"] == commit and
            binding["version"] == version and binding["architecture"] == arch and binding["go_version"] == application["go_version"] and
            binding["project_files_sha256"] == sha(canonical(expected_project)) and
            binding["binary"] == {key: application[key] for key in ("size", "sha256")})
    host = binding["host_manifest"]
    fields(host, {"name", "sha256"})
    profile = "buildroot-image" if arch == "legacy-x86" else "alpine-image"
    require(isinstance(host["name"], str) and re.fullmatch(profile + r"-(?:x86_64|x86|aarch64)\.json", host["name"]) and
            host["sha256"] == digest(root / "platform/builder-hosts" / host["name"]))
    if arch == "legacy-x86":
        require(value["firmware"] is None)
        return
    receipt_name = image + ".firmware-exclusions.json"
    receipt = read_json(candidate / receipt_name)
    policy = read_json(root / "platform/alpine/firmware-exclusions.json")
    rows = policy["entries"]
    expected = {"rootfs": [], "modloop": []}
    for entry in rows:
        row = {"path": entry["path"] + ".zst", "kind": entry["rootfs_kind"], **entry["stored"]}
        expected["rootfs"].append(row)
        if arch in entry["modloop_architectures"]:
            expected["modloop"].append(dict(row, kind="file"))
    fields(receipt, {"schema", "status", "architecture", "policy_sha256", "helper_sha256", "source_archive_sha256", "removed", "counts", "checked", "release_approval"})
    require(type(receipt["schema"]) is int and receipt["schema"] == 1 and receipt["status"] == "firmware_exclusions_applied" and
            receipt["architecture"] == arch and receipt["release_approval"] is False and
            receipt["policy_sha256"] == digest(root / "platform/alpine/firmware-exclusions.json") and
            receipt["helper_sha256"] == digest(root / "scripts/build/filter_alpine_firmware.py") and
            receipt["source_archive_sha256"] == policy["source_archive"]["sha256"] and receipt["removed"] == expected and
            receipt["counts"] == {layer: len(entries) for layer, entries in expected.items()})
    fields(receipt["checked"], {"rootfs_files", "modloop_files", "bytes"})
    for count in receipt["checked"].values():
        integer(count, 1, 64 << 30)
    firmware = value["firmware"]
    fields(firmware, {"policy_sha256", "receipt_sha256", "layers"})
    require(firmware["policy_sha256"] == receipt["policy_sha256"] and firmware["receipt_sha256"] == digest(candidate / receipt_name))
    fields(firmware["layers"], {"initramfs", "modloop"})
    # Finished-image inspection must consider every excluded path and digest in
    # both layers, including uncompressed/alternate-name payloads. This statement
    # is supplied by the reviewed inspector, not inferred from the build receipt.
    paths = sorted(entry["path"] for entry in rows)
    hashes = sorted({entry["stored"]["sha256"] for entry in rows} |
                    {entry["source"]["sha256"] for entry in rows})
    for layer in firmware["layers"].values():
        fields(layer, {"status", "checked_paths", "checked_sha256", "found_paths", "found_sha256"})
        require(layer["status"] == "pass" and layer["checked_paths"] == paths and
                layer["checked_sha256"] == hashes and layer["found_paths"] == [] and layer["found_sha256"] == [])
