#!/usr/bin/env python3
"""Validate a complete, reviewed ISO candidate and package companion downloads.

Image production and hardware/VM validation happen in the isolated lab. This
script verifies their digest-bound evidence; it never fabricates a test pass.
"""

import argparse
import csv
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import zipfile
import tarfile

from signatures import verify_detached
import source_assets

ROOT = Path(__file__).resolve().parents[2]
ARCHES = ("x86_64", "x86", "legacy-x86", "aarch64")
BOOT_MODES = {"x86_64": ["bios", "uefi"], "x86": ["bios"],
              "legacy-x86": ["bios-nosse2"], "aarch64": ["uefi"]}
REVIEW_GATES = ("locked-package-inputs", "image-secret-scan", "license-and-source-review",
                "offline-and-storage-safety", "report-integrity", "source-content-scan", "application-inspection")
SEMVER = re.compile(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)"
                    r"(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?")


def require(condition, message):
    if not condition:
        raise ValueError(message)


def digest(path):
    return source_assets.digest(path)


def strict_json(path):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            require(key not in result, "duplicate JSON key")
            result[key] = value
        return result
    return json.loads(source_assets.read_bytes(path), object_pairs_hook=unique,
                      parse_constant=lambda _: require(False, "invalid JSON number"))


def version_for(tag):
    version = tag.removeprefix("v")
    match = SEMVER.fullmatch(version)
    require(tag.startswith("v") and match, "tag must be v followed by SemVer (no build metadata)")
    prerelease = match.group(4)
    if prerelease:
        require(all(not item.isdigit() or item == "0" or not item.startswith("0")
                    for item in prerelease.split(".")), "numeric prerelease identifiers cannot have leading zeros")
        require(prerelease.split(".")[0] in {"alpha", "beta", "rc"}, "development versions cannot be published")
    return version


def image_name(version, arch):
    return f"hhsoft-pc-tester-{version}-{arch}.iso"


def required_review_gates(version, arch=None):
    prerelease = SEMVER.fullmatch(version).group(4)
    gates = set(REVIEW_GATES)
    if prerelease is None or prerelease.split(".")[0] != "alpha":
        gates.add("physical-qualification")
    if arch in {"x86_64", "x86", "aarch64"}:
        gates.add("firmware-exclusion")
    return gates


def expected_candidate(version, part_count=1):
    names = {"release-evidence.json", "release-evidence.json.asc", "lab-evidence.json",
             "THIRD-PARTY-NOTICES.txt", "SOURCE-OFFER.md", "SOURCE-MANIFEST.json"}
    names.update(source_assets.part_names(version, part_count))
    for arch in ARCHES:
        image = image_name(version, arch)
        names.update({image, image + ".spdx", image + ".validation.json", image + ".inspection.json", image + ".application.json",
                      image + (".packages.csv" if arch == "legacy-x86" else ".packages.lock")})
        if arch != "legacy-x86":
            names.add(image + ".firmware-exclusions.json")
    return names


def regular_files(directory):
    require(directory.is_dir() and not directory.is_symlink(), "candidate directory must be regular")
    files = list(directory.iterdir())
    require(len(files) <= 128, "too many candidate assets")
    for path in files:
        with source_assets.regular(path):
            pass
    return {p.name for p in files}


def review_assets(version, arch, evidence):
    image = image_name(version, arch)
    names = {"SOURCE-MANIFEST.json", "THIRD-PARTY-NOTICES.txt", "SOURCE-OFFER.md",
             image + ".inspection.json", image + ".application.json", image + ".spdx",
             image + (".packages.csv" if arch == "legacy-x86" else ".packages.lock")}
    if arch != "legacy-x86":
        names.add(image + ".firmware-exclusions.json")
    return {name: evidence["assets"][name] for name in sorted(names)}


def record_digest(record):
    return hashlib.sha256(json.dumps(record, sort_keys=True, separators=(",", ":"),
                                     ensure_ascii=True, allow_nan=False).encode("ascii")).hexdigest()


def companion_names(version):
    return {f"hhpt-{version}-{target}.zip" for target in
            ("windows_x86_64", "linux_x86_64", "linux_x86", "linux_legacy-x86", "linux_aarch64")} | {
                "LICENSE", "VERSION", "operator-guide.md"}


def validate(candidate, tag, commit, root=ROOT):
    return _validate(candidate, tag, commit, root)


def _validate(candidate, tag, commit, root, *, unsigned=False, generated=False):
    version = version_for(tag)
    require(re.fullmatch(r"[a-f0-9]{40}", commit), "full source commit required")
    require((root / "VERSION").read_text().strip() == version, "tag and VERSION differ")
    actual_names = regular_files(candidate)
    names = expected_candidate(version, source_assets.part_count(version, actual_names))
    present = names - {"release-evidence.json.asc"} if unsigned else set(names)
    if generated:
        present |= companion_names(version) | {"utilities.intoto.jsonl"}
    require(actual_names == present, "candidate must contain exactly all four ISOs, sidecars, source parts, and review files")
    if not unsigned:
        verify_detached(candidate / "release-evidence.json", candidate / "release-evidence.json.asc",
                        root / "security/lab-signing-key.asc", os.environ.get("LAB_SIGNING_KEY_FINGERPRINT", ""))
    evidence = strict_json(candidate / "release-evidence.json")
    require(set(evidence) == {"schema", "version", "source_commit", "base_locks", "assets"}, "invalid evidence fields")
    require(type(evidence["schema"]) is int and evidence["schema"] == 2 and evidence["version"] == version and evidence["source_commit"] == commit,
            "candidate evidence does not match tagged source")
    expected_locks = {name: digest(root / f"platform/{name}/base.lock") for name in ("alpine", "buildroot")}
    require(evidence["base_locks"] == expected_locks, "candidate base locks differ from tagged source")
    require(set(evidence["assets"]) == names - {"release-evidence.json", "release-evidence.json.asc"},
            "evidence must cover every candidate asset")
    for name, expected_hash in evidence["assets"].items():
        require(isinstance(expected_hash, str) and re.fullmatch(r"[a-f0-9]{64}", expected_hash), "invalid asset digest")
        require(digest(candidate / name) == expected_hash, "candidate asset checksum mismatch: " + name)
    lab = strict_json(candidate / "lab-evidence.json")
    require(set(lab) == {"schema", "records"} and type(lab["schema"]) is int and lab["schema"] == 2 and isinstance(lab["records"], dict),
            "invalid lab evidence document")
    project = source_assets.project_inventory(root, commit)
    _source_manifest, inventory = source_assets.validate_sources(candidate, version, commit, root, project)
    referenced = set()
    for arch in ARCHES:
        image = image_name(version, arch)
        require((candidate / image).stat().st_size > 32774, "truncated ISO")
        with source_assets.regular(candidate / image) as (stream, _size):
            stream.seek(32769)
            require(stream.read(5) == b"CD001", "missing ISO9660 volume descriptor")
        source_assets.validate_sbom(source_assets.read_bytes(candidate / (image + ".spdx")), inventory[arch], arch,
                                    image, version, evidence["assets"][image])
        packages = candidate / (image + (".packages.csv" if arch == "legacy-x86" else ".packages.lock"))
        require(packages.stat().st_size > 0, "empty package inventory")
        source_assets.validate_inspection(candidate, image, arch, version, commit, root, project, inventory[arch])
        validation = strict_json(candidate / (image + ".validation.json"))
        require(set(validation) == {"schema", "source_commit", "image_sha256", "boot", "reviews"}, "invalid image validation fields")
        require(type(validation["schema"]) is int and validation["schema"] == 2 and validation["source_commit"] == commit
                and validation["image_sha256"] == evidence["assets"][image], "validation is for a different image or commit")
        require(set(validation["boot"]) == set(BOOT_MODES[arch]), "required boot mode evidence missing")
        gates = required_review_gates(version, arch)
        require(set(validation["reviews"]) == gates, "required image release review missing")
        checks = [("boot:" + key, value) for key, value in validation["boot"].items()]
        checks += [("review:" + key, value) for key, value in validation["reviews"].items()]
        for check, record in checks:
            require(isinstance(record, dict) and set(record) == {"status", "evidence_sha256"}, "invalid review record")
            require(record["status"] == "pass" and isinstance(record["evidence_sha256"], str)
                    and re.fullmatch(r"[a-f0-9]{64}", record["evidence_sha256"])
                    and len(set(record["evidence_sha256"])) > 1, "incomplete boot or review evidence")
            record_hash = record["evidence_sha256"]
            require(record_hash in lab["records"], "review evidence does not resolve to a supplied lab record")
            detail = lab["records"][record_hash]
            require(isinstance(detail, dict) and set(detail) == {
                "schema", "source_commit", "image_sha256", "check", "status", "tool", "tool_version", "output", "assets"},
                "invalid lab result fields")
            require(type(detail["schema"]) is int and detail["schema"] == 2 and detail["source_commit"] == commit
                    and detail["image_sha256"] == evidence["assets"][image]
                    and detail["check"] == check and detail["status"] == "pass", "lab result binding differs")
            require(detail["assets"] == review_assets(version, arch, evidence), "lab result does not bind supplied source and inspection records")
            for field in ("tool", "tool_version", "output"):
                require(isinstance(detail[field], str) and 0 < len(detail[field].strip()) <= 16384,
                        "lab result must include bounded tool/version/output evidence")
            require(record_digest(detail) == record_hash, "lab evidence record digest differs")
            referenced.add(record_hash)
    require(referenced == set(lab["records"]), "unreferenced or missing lab evidence")
    for name in ("THIRD-PARTY-NOTICES.txt", "SOURCE-OFFER.md"):
        require((candidate / name).stat().st_size > 0, "required redistribution information is empty")
    return evidence


def seal(candidate, tag, commit, root=ROOT):
    """Hash supplied evidence; validation still refuses pending/missing reviews."""
    version = version_for(tag)
    actual = regular_files(candidate)
    require(actual == expected_candidate(version, source_assets.part_count(version, actual)) - {"release-evidence.json", "release-evidence.json.asc"},
            "supply the complete candidate without manifest or signature before sealing")
    evidence = {"schema": 2, "version": version, "source_commit": commit,
                "base_locks": {name: digest(root / f"platform/{name}/base.lock") for name in ("alpine", "buildroot")},
                "assets": {name: digest(candidate / name) for name in sorted(regular_files(candidate))}}
    target = candidate / "release-evidence.json"
    with target.open("x", encoding="utf-8", newline="\n") as stream:
        json.dump(evidence, stream, indent=2, sort_keys=True)
        stream.write("\n")
    try:
        _validate(candidate, tag, commit, root, unsigned=True)
    except Exception:
        target.unlink()  # Remove only the manifest this invocation just created.
        raise


def metadata(candidate, destination):
    destination.mkdir(parents=True, exist_ok=False)
    for source in candidate.iterdir():
        if source.suffix not in {".iso", ".tar"}:
            require(source.stat().st_size <= 4 * 1024 * 1024, "oversized text asset")
            source_assets.read_bytes(source).decode("utf-8")
            shutil.copyfile(source, destination / source.name)


def package(candidate, tag, commit, root=ROOT):
    validate(candidate, tag, commit, root)
    version = version_for(tag)
    goroot = Path(subprocess.check_output(["go", "env", "GOROOT"], text=True).strip())
    for target in ("windows_x86_64", "linux_x86_64", "linux_x86", "linux_legacy-x86", "linux_aarch64"):
        executable = "hhpt.exe" if target.startswith("windows") else "hhpt"
        binary = root / "build/bin" / target / executable
        require(binary.is_file(), "companion application build missing")
        entries = {executable: binary, "LICENSE.txt": root / "LICENSE", "GO-LICENSE.txt": goroot / "LICENSE",
                   "VERSION": root / "VERSION", "operator-guide.md": root / "docs/operator-guide.md",
                   "report-integrity.md": root / "docs/report-integrity.md"}
        with zipfile.ZipFile(candidate / f"hhpt-{version}-{target}.zip", "x", compression=zipfile.ZIP_DEFLATED) as archive:
            for name, source in sorted(entries.items()):
                info = zipfile.ZipInfo(name, date_time=(1980, 1, 1, 0, 0, 0))
                info.create_system = 3
                info.external_attr = (0o100755 if name == executable else 0o100644) << 16
                archive.writestr(info, source.read_bytes(), compress_type=zipfile.ZIP_DEFLATED)
    for source in (root / "LICENSE", root / "VERSION", root / "docs/operator-guide.md"):
        shutil.copyfile(source, candidate / source.name)
    predicate = {"sourceCommit": commit, "version": version, "imageBuildLocation": "external-reviewed-lab",
                 "checks": ["source-scans", "go-tests", "candidate-digests", "authenticated-lab-records"],
                 "limitations": "VM, hardware, image-content, and pinned-input reviews are performed in the lab; CI validates their records."}
    (root / "build/release-validation.json").write_text(json.dumps(predicate, indent=2) + "\n", encoding="utf-8")


def manifest(candidate, tag, commit, root=ROOT):
    # Utility provenance must exist and be verified by the workflow first.
    _validate(candidate, tag, commit, root, generated=True)
    names = sorted(regular_files(candidate))
    with (candidate / "SHA256SUMS").open("x", encoding="ascii", newline="\n") as stream:
        for name in names:
            stream.write(digest(candidate / name) + "  " + name + "\n")


class RedactedParser(argparse.ArgumentParser):
    def error(self, _message):
        raise ValueError("unsupported release arguments")


def main():
    parser = RedactedParser(description=__doc__)
    parser.add_argument("command", choices=("check", "seal", "metadata", "package", "manifest"))
    parser.add_argument("--candidate", type=Path, required=True)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--destination", type=Path)
    args = parser.parse_args()
    if args.command == "metadata":
        validate(args.candidate, args.tag, args.commit)
        require(args.destination is not None, "metadata destination required")
        metadata(args.candidate, args.destination)
    elif args.command == "seal":
        seal(args.candidate, args.tag, args.commit)
    elif args.command == "package":
        package(args.candidate, args.tag, args.commit)
    elif args.command == "manifest":
        manifest(args.candidate, args.tag, args.commit)
    else:
        validate(args.candidate, args.tag, args.commit)
    print("release candidate: " + args.command + " passed")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError, TypeError, AttributeError, RecursionError, csv.Error, tarfile.TarError, subprocess.SubprocessError):
        print("release candidate: incomplete or invalid; publication blocked", file=sys.stderr)
        sys.exit(1)
