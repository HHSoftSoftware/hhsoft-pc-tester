#!/usr/bin/env python3
"""Scan publishable source, index bytes, and optionally all reachable Git objects.

Only locations and rule names are reported; matched private data is never printed.
Python's standard library and Git are the only dependencies.
"""

import argparse
import os
from pathlib import Path, PurePosixPath
import re
import stat
import subprocess
import sys


PRIVATE_DIRS = {
    "private", "secret", "secrets",
    "credentials", ".agents", ".codex", ".claude", ".cursor", ".gemini",
    ".windsurf", ".aws", ".ssh", ".gnupg", "__pycache__",
}
GENERATED_DIRS = {"build", "dist", "out", "lab", "reports", ".cache"}
AGENT_INSTRUCTION_NAMES = re.compile(
    r"^[.#]?(?:agents|claude|gemini|skill|copilot-instructions)(?:[._-].*)?\.md.*$", re.I
)
SCOPED_INSTRUCTION_NAMES = re.compile(r"^.*\.instructions(?:[._-].*)?$", re.I)
PRIVATE_NAMES = re.compile(
    r"^(?:\.env(?:\..*)?|\.hhpt-source-denylist|known_hosts.*|id_(?:rsa|ed25519).*|"
    r"hhpt-deployment-key\.v1|\.aider.*|\.cursorrules(?:[._~-].*)?|\.windsurfrules(?:[._~-].*)?)$", re.I
)
FORBIDDEN_EXTENSIONS = re.compile(
    r"\.(?:iso|img|raw|qcow2|vdi|vmdk|vhdx?|ova|ovf|pem|key|p12|pfx|hhpcr|"
    r"exe|dll|zip|tar|tgz|gz|pyc|log|pid)$", re.I
)
PATTERNS = {
    "credential": re.compile(
        rb"gh[pousr]_[A-Za-z0-9_]{36,255}|github_pat_[A-Za-z0-9_]{70,255}|"
        rb"(?:AKIA|ASIA)[0-9A-Z]{16}|xox[baprs]-[A-Za-z0-9-]{20,}"
    ),
    "credential URL": re.compile(rb"https?://[^/@\s]+:[^/@\s]+@", re.I),
    "private key": re.compile(b"-----BEGIN " + rb"(?:[A-Z0-9]+ )*PRIVATE KEY-----"),
    "local user path": re.compile(
        rb"[A-Z]:[\\/]" + rb"Users[\\/]|/" + rb"(?:Users|home)/", re.I
    ),
}


def safe_relative_name(name):
    normalized = name.replace("\\", "/")
    # Reject host-absolute paths, traversal, Windows drive/ADS paths, and NULs
    # before any Git-supplied name is used to read or create a filesystem path.
    return bool(normalized) and "\\" not in name and ":" not in normalized and "\0" not in normalized and all(
        part not in {"", ".", ".."} for part in normalized.split("/"))


def filesystem_link(path):
    try:
        metadata = path.lstat()
    except FileNotFoundError:
        return False
    return stat.S_ISLNK(metadata.st_mode) or bool(
        getattr(metadata, "st_file_attributes", 0) & getattr(stat, "FILE_ATTRIBUTE_REPARSE_POINT", 0x400))


def worktree_path(root, name):
    if not safe_relative_name(name):
        raise RuntimeError("Unsafe source path; publication blocked")
    path = root
    for part in name.replace("\\", "/").split("/"):
        path = path / part
        if filesystem_link(path):
            raise RuntimeError("Source filesystem link; publication blocked")
    return path


def forbidden_path(name):
    if not safe_relative_name(name):
        return True
    parts = PurePosixPath(name.replace("\\", "/")).parts
    lower = tuple(part.lower() for part in parts)
    if not parts or any(part in ("..", ".git") for part in lower):
        return True
    if lower[0] in GENERATED_DIRS or any(part in PRIVATE_DIRS or part.startswith("_reference") for part in lower):
        return True
    if (PRIVATE_NAMES.fullmatch(parts[-1]) or AGENT_INSTRUCTION_NAMES.fullmatch(parts[-1])
            or SCOPED_INSTRUCTION_NAMES.fullmatch(parts[-1]) or FORBIDDEN_EXTENSIONS.search(parts[-1])):
        return True
    return any(
        lower[i] == ".github" and lower[i + 1] in {
            "copilot-instructions.md", "instructions", "agents", "prompts", "skills"
        }
        for i in range(len(lower) - 1)
    )


def git_environment():
    environment = os.environ.copy()
    # Push transfers original objects, even when local replace refs hide them.
    environment["GIT_NO_REPLACE_OBJECTS"] = "1"
    return environment


def git(root, *args, input=None):
    result = subprocess.run(["git", "--no-replace-objects", "-C", str(root), *args],
                            input=input, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            env=git_environment(), check=False)
    if result.returncode:
        raise RuntimeError("Git read failed; scan is incomplete")
    return result.stdout


def object_id(value):
    if not re.fullmatch(r"(?:[0-9a-f]{40}|[0-9a-f]{64})", value):
        raise ValueError("invalid Git object ID")
    return value


def push_objects(stream):
    """Read exact outgoing object IDs from Git's pre-push protocol."""
    result = set()
    for line in stream:
        fields = line.split()
        if len(fields) != 4:
            raise ValueError("invalid pre-push input")
        local = object_id(fields[1])
        object_id(fields[3])
        if set(local) != {"0"}:  # A deletion sends no objects.
            result.add(local)
    return sorted(result)


def history_objects(root, outgoing=()):
    if git(root, "rev-parse", "--is-shallow-repository").strip() == b"true":
        raise RuntimeError("Full history is required; fetch with --unshallow first")
    grafts = Path(os.fsdecode(git(root, "rev-parse", "--git-path", "info/grafts").strip()))
    if not grafts.is_absolute():
        grafts = root / grafts
    if grafts.exists():
        raise RuntimeError("Legacy Git grafts can hide history; publication blocked")
    roots = set(git(root, "for-each-ref", "--format=%(objectname)").decode("ascii").splitlines())
    head = subprocess.run(["git", "--no-replace-objects", "-C", str(root),
                           "rev-parse", "--verify", "--quiet", "HEAD"],
                          capture_output=True, env=git_environment())
    if head.returncode == 0:
        roots.add(head.stdout.decode("ascii").strip())
    elif head.returncode != 1:
        raise RuntimeError("Git HEAD inspection failed; publication blocked")
    roots.update(object_id(value) for value in outgoing)
    if not roots:
        return {}
    # Tags may form chains, but every published root must peel to a commit.
    # Blob/tree refs and tags would otherwise be omitted by a commit walk.
    for oid in sorted(roots):
        git(root, "rev-parse", "--verify", oid + "^{commit}")
    ids = git(root, "rev-list", "--objects", "--no-object-names", "--stdin",
              input=("\n".join(sorted(roots)) + "\n").encode("ascii"))
    objects = {}
    for line in git(root, "cat-file", "--batch-check=%(objectname) %(objecttype)", input=ids).splitlines():
        oid, kind = line.decode("ascii").split()
        if kind not in {"commit", "tag", "tree", "blob"}:
            raise RuntimeError("Unsupported Git object; publication blocked")
        objects[oid] = kind
    return objects


class Scanner:
    def __init__(self, root):
        self.root = root
        self.failures = set()
        self.seen = set()
        denylist = root / ".hhpt-source-denylist"
        self.private_terms = []
        if denylist.exists():
            self.private_terms = [line.strip().lower() for line in denylist.read_bytes().splitlines()
                                  if line.strip() and not line.lstrip().startswith(b"#")]

    def fail(self, rule, location):
        # JSON-style escaping prevents control characters in malicious paths/logs.
        import json
        encoded = os.fsencode(location)
        private_research = any(part.lower().startswith("_reference")
                               for part in re.split(r"[\\/:\s]", location))
        if private_research or any(pattern.search(encoded) for pattern in PATTERNS.values()) or any(
                term in encoded.lower() for term in self.private_terms):
            location = "[redacted location]"
        self.failures.add(f"{rule}: {json.dumps(location, ensure_ascii=True)}")

    def content(self, data, location):
        for rule, pattern in PATTERNS.items():
            if pattern.search(data):
                self.fail(rule, location)
        lowered = data.lower()
        if any(term in lowered for term in self.private_terms):
            self.fail("local denylist", location)

    def path(self, name, location):
        if forbidden_path(name):
            self.fail("forbidden artifact or agent path", location)
        self.content(os.fsencode(name), "path at " + location)

    def blob(self, oid, location):
        if oid not in self.seen:
            self.content(git(self.root, "cat-file", "blob", oid), location)
            self.seen.add(oid)

    def current(self):
        names = git(self.root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
        for raw in sorted(set(names.split(b"\0")) - {b""}):
            name = os.fsdecode(raw)
            self.path(name, name)
            try:
                path = worktree_path(self.root, name)
            except RuntimeError:
                self.fail("unsafe path or filesystem link", name)
                continue
            if path.is_file():
                self.content(path.read_bytes(), name)
            elif path.exists():
                self.fail("non-regular file", name)
            # A tracked deletion is still inspected from the index below.
        for entry in git(self.root, "ls-files", "--stage", "-z").split(b"\0"):
            if not entry:
                continue
            metadata, raw = entry.split(b"\t", 1)
            mode, oid, stage = metadata.decode("ascii").split()
            name = os.fsdecode(raw)
            if mode not in ("100644", "100755") or stage != "0":
                self.fail("non-regular or unmerged index entry", name)
                continue
            self.blob(oid, "index:" + name)

    def history(self, outgoing=()):
        objects = history_objects(self.root, outgoing)
        self.content(git(self.root, "for-each-ref", "--format=%(refname)"), "Git reference names")
        for commit, object_kind in objects.items():
            if object_kind == "tag":
                self.content(git(self.root, "cat-file", "tag", commit), "tag metadata:" + commit)
            if object_kind != "commit":
                continue
            self.content(git(self.root, "cat-file", "commit", commit), "commit metadata:" + commit)
            tree = git(self.root, "ls-tree", "-rz", "--full-tree", commit)
            for entry in tree.split(b"\0"):
                if not entry:
                    continue
                metadata, name_raw = entry.split(b"\t", 1)
                mode, kind, oid = metadata.decode("ascii").split()
                name = os.fsdecode(name_raw)
                location = commit + ":" + name
                self.path(name, location)
                if mode not in ("100644", "100755") or kind != "blob":
                    self.fail("non-regular history entry", location)
                else:
                    self.blob(oid, location)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--history", action="store_true")
    parser.add_argument("--object", action="append", default=[], help="additional outgoing Git object ID to inspect")
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[2])
    args = parser.parse_args()
    try:
        root = args.root.resolve()
        if args.object and not args.history:
            raise RuntimeError("Outgoing objects require a history scan")
        outgoing = [object_id(value) for value in args.object]
        scanner = Scanner(root)
        if (root / ".git").exists():
            scanner.current()
            if args.history:
                scanner.history(outgoing)
        else:
            if args.history:
                raise RuntimeError("History scan requires a Git repository")
            # Exported source has no Git ignore engine. Skip only root build dirs;
            # agent/private paths are inspected and rejected, never skipped.
            for directory, dirs, files in os.walk(root, followlinks=False):
                if Path(directory) == root:
                    dirs[:] = [d for d in dirs if d not in GENERATED_DIRS]
                for name in list(dirs):
                    path = Path(directory) / name
                    if filesystem_link(path):
                        relative = path.relative_to(root).as_posix()
                        scanner.path(relative, relative)
                        scanner.fail("filesystem link", relative)
                        dirs.remove(name)
                for name in files:
                    path = Path(directory) / name
                    relative = path.relative_to(root).as_posix()
                    scanner.path(relative, relative)
                    try:
                        path = worktree_path(root, relative)
                    except RuntimeError:
                        scanner.fail("unsafe path or filesystem link", relative)
                        continue
                    scanner.content(path.read_bytes(), relative)
        if scanner.failures:
            for failure in sorted(scanner.failures):
                print("source scan: " + failure, file=sys.stderr)
            return 1
        print("source scan: clean" + (" (including index and full history)" if args.history else ""))
        return 0
    except (OSError, RuntimeError, ValueError):
        print("source scan: inspection failed; publication blocked", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
