#!/usr/bin/env python3
"""Run both scanners on exact publishable bytes before committing or pushing."""

import argparse
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

from scan_source import Scanner, git, git_environment, history_objects, push_objects, safe_relative_name, worktree_path


def snapshot_history(root, snapshot, outgoing=()):
    objects = history_objects(root, outgoing)
    for oid, kind in objects.items():
        if kind in {"blob", "commit", "tag"}:
            # Scan raw objects independently of diffs. Merge resolutions and
            # binary-marked files can otherwise disappear from `git log -p`.
            target = snapshot / "history" / kind / oid
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(git(root, "cat-file", kind, oid))
    (snapshot / "git-reference-names.txt").write_bytes(git(root, "for-each-ref", "--format=%(refname)"))
    return bool(objects)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--gitleaks", required=True, type=Path)
    parser.add_argument("--pre-push", action="store_true", help="inspect outgoing updates supplied by Git on stdin")
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[2]
    scanner = args.gitleaks.resolve()
    push_metadata = sys.stdin.read() if args.pre_push else ""
    outgoing = push_objects(push_metadata.splitlines())
    reference_scan = Scanner(root)
    reference_scan.content(push_metadata.encode("utf-8"), "outgoing Git reference metadata")
    if reference_scan.failures:
        raise RuntimeError("Private outgoing Git reference metadata; publication blocked")
    command = [sys.executable, str(root / "scripts/security/scan_source.py"), "--history"]
    for oid in outgoing:
        command += ["--object", oid]
    subprocess.run(command, check=True, env=git_environment())
    expected = dict(line.split("=", 1) for line in (root / "scripts/security/gitleaks.lock").read_text().splitlines())
    actual = subprocess.run([str(scanner), "version"], capture_output=True, check=True, text=True)
    if actual.stdout.strip().removeprefix("v") != expected["VERSION"]:
        raise RuntimeError("Gitleaks version differs from the reviewed lock")
    with tempfile.TemporaryDirectory(prefix="hhpt-publication-") as temporary:
        workspace = Path(temporary)
        snapshot = workspace / "candidate"
        snapshot.mkdir()
        for raw in set(git(root, "ls-files", "-z", "--cached", "--others", "--exclude-standard").split(b"\0")) - {b""}:
            name = raw.decode("utf-8")
            source = worktree_path(root, name)
            if source.is_file():
                target = snapshot / "worktree" / name
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(source, target)
        for entry in git(root, "ls-files", "--stage", "-z").split(b"\0"):
            if entry:
                metadata, raw = entry.split(b"\t", 1)
                mode, oid, stage = metadata.decode("ascii").split()
                name = raw.decode("utf-8")
                if mode not in {"100644", "100755"} or stage != "0" or not safe_relative_name(name):
                    raise RuntimeError("Unsupported index entry; publication blocked")
                target = snapshot / "index" / name
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(git(root, "cat-file", "blob", oid))
        has_history = snapshot_history(root, snapshot, outgoing)
        (snapshot / "outgoing-reference-metadata.txt").write_text(push_metadata, encoding="utf-8")
        # Use built-in rules explicitly; candidate files cannot weaken this gate.
        config = workspace / "rules.toml"
        config.write_text("[extend]\nuseDefault = true\n")
        ignored = workspace / "empty-ignore"
        ignored.write_text("")
        common = ["--no-banner", "--redact=100", "--ignore-gitleaks-allow",
                  "--config", str(config), "--gitleaks-ignore-path", str(ignored)]
        result = subprocess.run([str(scanner), "dir", str(snapshot), *common], cwd=workspace,
                                capture_output=True, env=git_environment())
        if result.returncode:
            # Gitleaks logs can contain private paths even with value redaction.
            raise RuntimeError("Gitleaks found a secret or could not complete its scan; publication blocked")
        print("publication check: custom rules and pinned Gitleaks passed on candidate, index, and available history")
        if not has_history:
            print("publication check: no commits yet; rerun after the first commit to check author/tag metadata")


if __name__ == "__main__":
    try:
        main()
    except (OSError, RuntimeError, ValueError, subprocess.CalledProcessError) as error:
        print("publication check failed: " + (str(error) if isinstance(error, RuntimeError) else "tool execution error"), file=sys.stderr)
        sys.exit(1)
