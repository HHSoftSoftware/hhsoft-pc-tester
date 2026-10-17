#!/usr/bin/python3 -IB
"""Apply the committed firmware omission policy to two fresh build trees.

This filters owned, disposable update-kernel staging directories, never an
installed system. Descriptor pins and complete preflight catch observed changes;
they do not make pathname unlink atomic against a concurrent privileged writer.
The build must exclusively own these trees for the duration of this operation.
"""

import argparse
from contextlib import ExitStack
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import sys

POLICY_SHA256 = "f7e9828d04a5468bc28831dbc568afdd470447d74992d23a5609ea4e8a8bdfa5"
MAX_PATHS = 20000
MAX_DEPTH = 32
MAX_FILE = 512 << 20
MAX_TOTAL = 8 << 30
MAX_POLICY = 128 << 10
HASH = re.compile(r"[0-9a-f]{64}\Z")
ARCHES = {"x86_64", "x86", "aarch64"}


class Rejected(ValueError):
    pass


def require(condition):
    if not condition:
        raise Rejected("unsupported or changed firmware build inputs")


def identity(info):
    return (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid,
            info.st_nlink, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def owned(info):
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.geteuid() and not info.st_mode & 0o022)


def safe_relative(value):
    require(isinstance(value, str) and 0 < len(value) <= 1024)
    require(re.fullmatch(r"[A-Za-z0-9_.+@=-]+(?:/[A-Za-z0-9_.+@=-]+)*", value) is not None)
    require(all(part not in (".", "..") for part in value.split("/")))
    require(len(value.split("/")) <= MAX_DEPTH)
    return value


def absolute_parts(value):
    require(isinstance(value, str) and value.startswith("/") and "\\" not in value)
    parts = value.split("/")[1:]
    require(parts and all(part not in ("", ".", "..") and all(ord(c) >= 32 for c in part) for part in parts))
    return parts


def pin_at(parent, name, stack):
    fd = os.open(name, os.O_PATH | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=parent)
    stack.callback(os.close, fd)
    return fd


def directory_at(parent, name, stack, expected_device=None, bindings=None):
    pin = pin_at(parent, name, stack)
    info = os.fstat(pin)
    require(stat.S_ISDIR(info.st_mode))
    if expected_device is not None:
        require(info.st_dev == expected_device)
    fd = os.open("/proc/self/fd/" + str(pin), os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
    stack.callback(os.close, fd)
    require(identity(os.fstat(fd)) == identity(info))
    if bindings is not None:
        bindings.append((parent, name, fd))
    return fd


def absolute_directory(path, stack, bindings=None):
    parts = absolute_parts(path)
    fd = os.open("/", os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
    stack.callback(os.close, fd)
    for part in parts:
        fd = directory_at(fd, part, stack, bindings=bindings)
    return fd


def validate_bindings(bindings):
    for parent, name, fd in bindings:
        # File removal changes directory times/size; identity/ownership/type
        # remain fixed. Rebinding any ancestor must still stop the build.
        require(identity(os.stat(name, dir_fd=parent, follow_symlinks=False))[:6] == identity(os.fstat(fd))[:6])


def exact_bytes(pin, limit):
    info = os.fstat(pin)
    require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and 0 <= info.st_size <= limit)
    fd = os.open("/proc/self/fd/" + str(pin), os.O_RDONLY | os.O_CLOEXEC)
    try:
        require(identity(os.fstat(fd)) == identity(info))
        chunks, remaining = [], info.st_size
        while remaining:
            data = os.read(fd, min(1 << 20, remaining))
            require(bool(data))
            chunks.append(data)
            remaining -= len(data)
        require(not os.read(fd, 1) and identity(os.fstat(fd)) == identity(info))
        return b"".join(chunks)
    finally:
        os.close(fd)


def file_hash(pin, budget):
    info = os.fstat(pin)
    require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and 0 <= info.st_size <= MAX_FILE)
    require(budget[0] + info.st_size <= MAX_TOTAL)
    budget[0] += info.st_size
    fd = os.open("/proc/self/fd/" + str(pin), os.O_RDONLY | os.O_CLOEXEC)
    try:
        require(identity(os.fstat(fd)) == identity(info))
        hasher, remaining = hashlib.sha256(), info.st_size
        while remaining:
            chunk = os.read(fd, min(1 << 20, remaining))
            require(bool(chunk))
            hasher.update(chunk)
            remaining -= len(chunk)
        require(not os.read(fd, 1) and identity(os.fstat(fd)) == identity(info))
        return {"sha256": hasher.hexdigest(), "size": info.st_size}
    finally:
        os.close(fd)


def validate_policy(policy):
    require(isinstance(policy, dict) and policy.get("schema") == 1)
    entries = policy.get("entries")
    require(isinstance(entries, list) and 0 < len(entries) <= 128)
    paths = []
    for entry in entries:
        path = safe_relative(entry["path"])
        require(not path.endswith((".xz", ".zst")))
        paths.append(path)
        architectures = entry["modloop_architectures"]
        require(isinstance(architectures, list) and all(isinstance(arch, str) and arch in ARCHES for arch in architectures))
        require(architectures == sorted(set(architectures)))
        for field in ("source", "stored"):
            value = entry[field]
            require(set(value) == {"sha256", "size"} and HASH.fullmatch(value["sha256"]) is not None)
            require(type(value["size"]) is int and 0 < value["size"] <= MAX_FILE)
        require(entry["rootfs_kind"] in ("file", "symlink"))
        require(entry["rootfs_mode"] == (0o644 if entry["rootfs_kind"] == "file" else 0o777))
        if entry["rootfs_kind"] == "symlink":
            target = safe_relative(entry["link_target"])
            require("/" not in target and target.endswith(".zst"))
        else:
            require("link_target" not in entry)
    require(paths == sorted(set(paths)))
    return entries


def load_policy():
    # Production has no alternate policy/source-root option. The application
    # export additionally binds these bytes to the authenticated source tree.
    script = os.path.abspath(__file__)
    root = str(Path(script).parents[2])
    with ExitStack() as stack:
        source = absolute_directory(root, stack)
        policy_dir = directory_at(directory_at(source, "platform", stack), "alpine", stack)
        raw = exact_bytes(pin_at(policy_dir, "firmware-exclusions.json", stack), MAX_POLICY)
        require(hashlib.sha256(raw).hexdigest() == POLICY_SHA256)
        scripts = directory_at(directory_at(source, "scripts", stack), "build", stack)
        helper = exact_bytes(pin_at(scripts, "filter_alpine_firmware.py", stack), 64 << 10)
    policy = json.loads(raw)
    validate_policy(policy)
    require(len(policy["entries"]) == 41)
    return policy, POLICY_SHA256, hashlib.sha256(helper).hexdigest()


def inventory(fd, device, budget):
    records = {}

    def visit(parent, prefix, depth):
        require(depth <= MAX_DEPTH)
        before = identity(os.fstat(parent))
        names = sorted(os.listdir(parent))
        require(len(records) + len(names) <= MAX_PATHS)
        for name in names:
            require(len(records) < MAX_PATHS)
            safe_relative(name)
            require("/" not in name)
            path = prefix + name
            with ExitStack() as stack:
                pin = pin_at(parent, name, stack)
                info = os.fstat(pin)
                require(info.st_dev == device and info.st_uid == os.geteuid() and info.st_gid == os.getegid())
                record = {"identity": identity(info), "mode": stat.S_IMODE(info.st_mode)}
                if stat.S_ISDIR(info.st_mode):
                    owned(info)
                    record["kind"] = "directory"
                    records[path] = record
                    child = directory_at(parent, name, stack, device)
                    require(identity(os.fstat(child)) == identity(info))
                    visit(child, path + "/", depth + 1)
                elif stat.S_ISREG(info.st_mode):
                    record.update(file_hash(pin, budget), kind="file")
                    records[path] = record
                elif stat.S_ISLNK(info.st_mode):
                    require(info.st_nlink == 1 and 0 < info.st_size <= 1024)
                    target = os.readlink(name, dir_fd=parent)
                    require(not target.startswith("/") and "\\" not in target and 0 < len(target) <= 1024)
                    require(all(32 <= ord(c) < 127 for c in target))
                    require(identity(os.stat(name, dir_fd=parent, follow_symlinks=False)) == identity(info))
                    record.update(kind="symlink", target=target)
                    records[path] = record
                else:
                    raise Rejected("special firmware node")
        require(identity(os.fstat(parent)) == before)

    visit(fd, "", 0)
    return records


def resolve(records, name):
    todo, done, seen = name.split("/"), [], set()
    while todo:
        part = todo.pop(0)
        if part in ("", "."):
            continue
        if part == "..":
            require(bool(done))
            done.pop()
            continue
        candidate = "/".join(done + [part])
        node = records.get(candidate)
        if node is None:
            # Missing unrelated targets remain metadata, never followed. Still
            # normalize the rest to reject an escaping dangling symlink.
            done.append(part)
            while todo:
                rest = todo.pop(0)
                if rest in ("", "."):
                    continue
                if rest == "..":
                    require(bool(done))
                    done.pop()
                else:
                    done.append(rest)
            return "/".join(done), None
        if node["kind"] == "symlink":
            require(candidate not in seen and len(seen) < MAX_DEPTH)
            seen.add(candidate)
            todo = node["target"].split("/") + todo
        else:
            require(not todo or node["kind"] == "directory")
            done.append(part)
    final = "/".join(done)
    return final, records.get(final)


def removal_plan(records, entries, layer):
    selected = {entry["path"] + ".zst": entry for entry in entries}
    forbidden_hashes = {entry[field]["sha256"] for entry in entries for field in ("source", "stored")}
    plan = []
    for path, entry in selected.items():
        require(entry["path"] not in records and entry["path"] + ".xz" not in records)
        node = records.get(path)
        if node is None:
            require(layer == "modloop")
            continue
        expected_kind = entry["rootfs_kind"] if layer == "rootfs" else "file"
        require(node["kind"] == expected_kind)
        require(node["mode"] == (entry["rootfs_mode"] if layer == "rootfs" else 0o755))
        if expected_kind == "symlink":
            require(node["target"] == entry["link_target"])
        resolved, content = resolve(records, path)
        require(content is not None and content["kind"] == "file")
        require({k: content[k] for k in ("size", "sha256")} == entry["stored"])
        plan.append({"path": path, "kind": node["kind"], **entry["stored"]})
    for path, node in records.items():
        if node["kind"] == "directory":
            continue
        resolved, content = resolve(records, path)
        if path not in selected:
            require(resolved not in selected)
            require(content is None or content.get("sha256") not in forbidden_hashes)
    return plan


def parent_for(fd, path, records, stack, cache):
    parts = path.split("/")
    prefix = []
    for part in parts[:-1]:
        prefix.append(part)
        key = "/".join(prefix)
        if key not in cache:
            cache[key] = directory_at(fd, part, stack)
        fd = cache[key]
        require(identity(os.fstat(fd)) == records[key]["identity"])
    return fd, parts[-1]


def validate_snapshot(fd, records):
    # Recheck the complete observed tree before the first unlink, including
    # names, so a late unexpected alias cannot be silently ignored.
    seen = set()

    def visit(parent, prefix):
        for name in os.listdir(parent):
            path = prefix + name
            require(path in records)
            row = records[path]
            require(identity(os.stat(name, dir_fd=parent, follow_symlinks=False)) == row["identity"])
            seen.add(path)
            if row["kind"] == "directory":
                with ExitStack() as stack:
                    visit(directory_at(parent, name, stack), path + "/")
    visit(fd, "")
    require(seen == records.keys())


def apply_filter(work_parent, rootfs, modloop, architecture, receipt, policy, policy_hash, helper_hash):
    require(sys.platform.startswith("linux") and hasattr(os, "O_PATH") and architecture in ARCHES)
    require(HASH.fullmatch(policy_hash) is not None and HASH.fullmatch(helper_hash) is not None)
    entries = validate_policy(policy)
    parent_parts, root_parts, mod_parts = map(absolute_parts, (work_parent, rootfs, modloop))
    require(root_parts[:-2] == parent_parts and mod_parts[:-2] == parent_parts)
    require(root_parts[-1] == "root" and mod_parts[-1] == "modloop" and root_parts[-2] == mod_parts[-2])
    require(re.fullmatch(r"update-kernel\.[A-Za-z0-9]{6}", root_parts[-2]) is not None)
    output_parts = absolute_parts(receipt)
    require(receipt not in (rootfs, modloop) and not receipt.startswith(rootfs + "/") and not receipt.startswith(modloop + "/"))
    budget = [0]
    with ExitStack() as stack:
        bindings = []
        parent = absolute_directory(work_parent, stack, bindings)
        owned(os.fstat(parent))
        device = os.fstat(parent).st_dev
        temporary = directory_at(parent, root_parts[-2], stack, device, bindings)
        owned(os.fstat(temporary))
        root = directory_at(temporary, "root", stack, device, bindings)
        mod = directory_at(temporary, "modloop", stack, device, bindings)
        for fd in (root, mod):
            owned(os.fstat(fd))
        require((os.fstat(root).st_dev, os.fstat(root).st_ino) != (os.fstat(mod).st_dev, os.fstat(mod).st_ino))
        lib = pin_at(root, "lib", stack)
        lib_info = os.fstat(lib)
        require(stat.S_ISLNK(lib_info.st_mode) and lib_info.st_nlink == 1 and os.readlink("lib", dir_fd=root) == "usr/lib")
        require(identity(os.stat("lib", dir_fd=root, follow_symlinks=False)) == identity(lib_info))
        usr = directory_at(root, "usr", stack, device, bindings)
        libdir = directory_at(usr, "lib", stack, device, bindings)
        root_firmware = directory_at(libdir, "firmware", stack, device, bindings)
        modules = directory_at(mod, "modules", stack, device, bindings)
        mod_firmware = directory_at(modules, "firmware", stack, device, bindings)
        for fd in (usr, libdir, root_firmware, modules, mod_firmware):
            owned(os.fstat(fd))
        output_parent = absolute_directory("/" + "/".join(output_parts[:-1]), stack, bindings)
        owned(os.fstat(output_parent))
        try:
            os.stat(output_parts[-1], dir_fd=output_parent, follow_symlinks=False)
        except FileNotFoundError:
            pass
        else:
            raise Rejected("receipt already exists")
        trees = {"rootfs": root_firmware, "modloop": mod_firmware}
        snapshots = {layer: inventory(fd, device, budget) for layer, fd in trees.items()}
        plans = {layer: removal_plan(snapshots[layer], entries, layer) for layer in trees}
        expected_modloop = {entry["path"] + ".zst" for entry in entries if architecture in entry["modloop_architectures"]}
        require({row["path"] for row in plans["modloop"]} == expected_modloop)
        # Pin every selected leaf and its parent before committing any removal.
        leaves = []
        for layer, plan in plans.items():
            parents = {}
            for row in plan:
                parent_fd, name = parent_for(trees[layer], row["path"], snapshots[layer], stack, parents)
                pin = pin_at(parent_fd, name, stack)
                require(identity(os.fstat(pin)) == snapshots[layer][row["path"]]["identity"])
                leaves.append((parent_fd, name, pin, snapshots[layer][row["path"]]["identity"]))
        for layer, fd in trees.items():
            validate_snapshot(fd, snapshots[layer])
        validate_bindings(bindings)
        require(identity(os.stat("lib", dir_fd=root, follow_symlinks=False)) == identity(lib_info))
        for parent_fd, name, pin, expected in leaves:
            require(identity(os.fstat(pin)) == expected)
            require(identity(os.stat(name, dir_fd=parent_fd, follow_symlinks=False)) == expected)
            os.unlink(name, dir_fd=parent_fd)
        # Rescan bytes and aliases in both filtered trees before a receipt can
        # claim success. A concurrent change fails this build, not its evidence.
        post_budget = [0]
        for layer, fd in trees.items():
            after = inventory(fd, device, post_budget)
            removed = {row["path"] for row in plans[layer]}
            require(after.keys() == snapshots[layer].keys() - removed)
            for path, row in after.items():
                old = snapshots[layer][path]
                if row["kind"] == "directory":
                    require(row["identity"][:6] == old["identity"][:6])
                else:
                    require(row == old)
            require(not removal_plan(after, entries, "modloop"))
        validate_bindings(bindings)
        require(identity(os.stat("lib", dir_fd=root, follow_symlinks=False)) == identity(lib_info))
        result = {"schema": 1, "status": "firmware_exclusions_applied", "architecture": architecture,
                  "policy_sha256": policy_hash, "helper_sha256": helper_hash,
                  "source_archive_sha256": policy["source_archive"]["sha256"],
                  "removed": plans, "counts": {layer: len(plan) for layer, plan in plans.items()},
                  "checked": {"rootfs_files": sum(r["kind"] == "file" for r in snapshots["rootfs"].values()),
                              "modloop_files": sum(r["kind"] == "file" for r in snapshots["modloop"].values()),
                              "bytes": budget[0]}, "release_approval": False}
        data = (json.dumps(result, sort_keys=True, indent=2) + "\n").encode("ascii")
        fd = os.open(output_parts[-1], os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=output_parent)
        with os.fdopen(fd, "wb") as stream:
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())
        os.fsync(output_parent)
    return result


class Parser(argparse.ArgumentParser):
    def error(self, message):
        raise Rejected("unsupported arguments")


def main():
    parser = Parser(description="Filter fresh update-kernel firmware staging trees.")
    for name in ("work-parent", "rootfs", "modloop", "architecture", "receipt"):
        parser.add_argument("--" + name, required=True)
    args = parser.parse_args()
    require(sys.platform.startswith("linux") and hasattr(os, "O_PATH"))
    policy, policy_hash, helper_hash = load_policy()
    apply_filter(args.work_parent, args.rootfs, args.modloop, args.architecture, args.receipt, policy, policy_hash, helper_hash)
    print("Firmware exclusion policy applied to both staging trees.")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError, TypeError, RuntimeError, RecursionError):
        print("Firmware exclusion check failed; image build blocked.", file=sys.stderr)
        raise SystemExit(1)
