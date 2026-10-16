#!/usr/bin/python3
"""Run apk and retain the exact archives consumed by successful image steps.

Installed databases include dependency and install_if selections. Recording
their exact versions avoids treating every package in an offline cache as an
image component. This wrapper is used only in the controlled image environment.
"""

import hashlib
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile


VALUE_OPTIONS = {
    "--root", "-p", "--arch", "--keys-dir", "--repositories-file",
    "--repository", "-X", "--cache-dir", "--output", "-o",
    "--description", "--index", "--rewrite-arch",
}
SCOPE_OPTIONS = {
    "--root", "-p", "--arch", "--keys-dir", "--repositories-file",
    "--repository", "-X", "--cache-dir",
}
COMMANDS = {"add", "del", "fix", "update", "upgrade", "cache", "index", "fetch", "info", "search"}
MUTATIONS = {"add", "del", "fix", "update", "upgrade", "cache", "index"}


def parse(arguments):
    command, root, scope, fetch = None, None, [], []
    simulated = False
    index = 0
    while index < len(arguments):
        arg = arguments[index]
        option, separator, attached = arg.partition("=")
        if option in VALUE_OPTIONS:
            if separator:
                value = attached
            else:
                index += 1
                if index == len(arguments):
                    raise ValueError("missing apk option value")
                value = arguments[index]
            if option in SCOPE_OPTIONS:
                scope.extend((option, value))
            if option in {"--root", "-p"}:
                if root is not None:
                    raise ValueError("multiple apk roots")
                root = Path(value)
            if option not in {"--output", "-o"}:
                fetch.extend((option, value))
        elif arg in {"--simulate", "-s"}:
            # -s is fetch's stdout short option, not a simulation flag.
            simulated |= arg == "--simulate"
        elif arg in {"--stdout", "--link", "-l"}:
            pass
        else:
            if command is None and arg in COMMANDS:
                command = arg
            fetch.append(arg)
        index += 1
    return command, root, scope, fetch, simulated


def require_build_root(root, environment):
    if root is None:
        raise ValueError("host apk mutations are forbidden")
    resolved = root.resolve()
    allowed = [Path(environment[key]).resolve() for key in ("HHPT_APK_BUILD_ROOT", "HHPT_APK_TMP_ROOT")]
    if not any(resolved != parent and resolved.is_relative_to(parent) for parent in allowed):
        raise ValueError("apk root is outside this image build")


def installed_packages(root):
    packages = []
    text = (root / "lib/apk/db/installed").read_text(encoding="utf-8")
    for entry in text.split("\n\n"):
        if not entry.strip():
            continue
        fields = {}
        for line in entry.splitlines():
            if line.startswith(("P:", "V:")):
                key, value = line.split(":", 1)
                if key in fields:
                    raise ValueError("duplicate installed package field")
                fields[key] = value
        if set(fields) != {"P", "V"} or not all(re.fullmatch(r"[A-Za-z0-9_+~.-]+", value) for value in fields.values()):
            raise ValueError("invalid installed package identity")
        packages.append(fields["P"] + "=" + fields["V"])
    if len(packages) != len(set(packages)):
        raise ValueError("duplicate installed package")
    return sorted(packages)


def retain_archives(real_apk, arguments, destination, runner):
    destination.mkdir(parents=True, exist_ok=True)
    if destination.is_symlink():
        raise ValueError("archive directory must be regular")
    with tempfile.TemporaryDirectory(prefix=".fetch-", dir=destination) as temporary:
        runner([real_apk, *arguments, "--output", temporary], check=True, stdout=sys.stderr)
        archives = list(Path(temporary).iterdir())
        if not archives:
            raise ValueError("apk recording produced no archives")
        for archive in archives:
            if archive.suffix != ".apk" or archive.is_symlink() or not archive.is_file() or archive.stat().st_size == 0:
                raise ValueError("invalid recorded archive")
            with archive.open("rb") as stream:
                digest = hashlib.file_digest(stream, "sha256").hexdigest()
            target = destination / (digest + ".apk")
            if target.exists():
                if target.is_symlink() or not target.is_file():
                    raise ValueError("invalid previous archive")
                with target.open("rb") as stream:
                    if hashlib.file_digest(stream, "sha256").hexdigest() != digest:
                        raise ValueError("previous archive changed")
            else:
                # The temporary capture is on this filesystem. A hard link
                # avoids a second large firmware copy and fails if replaced.
                os.link(archive, target)


def run(arguments, environment, runner=subprocess.run):
    real_apk = environment["HHPT_REAL_APK"]
    command, root, scope, fetch, simulated = parse(arguments)
    if command in MUTATIONS:
        require_build_root(root, environment)
    offline = ["--no-network"] if environment["HHPT_APK_OFFLINE"] == "1" else []
    runner([real_apk, *offline, *arguments], check=True)
    if simulated:
        return
    destination = Path(environment["HHPT_APK_RECORD_DIR"])
    if command == "fetch":
        retain_archives(real_apk, [*offline, *fetch], destination, runner)
    elif command == "add":
        packages = installed_packages(root)
        if packages:
            retain_archives(real_apk, [*offline, *scope, "fetch", *packages], destination, runner)


if __name__ == "__main__":
    try:
        run(sys.argv[1:], os.environ)
    except (OSError, ValueError, KeyError, subprocess.CalledProcessError):
        print("apk input recording failed; image build blocked", file=sys.stderr)
        sys.exit(1)
