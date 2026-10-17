"""Synthetic release-contract fixtures; these are never real qualification evidence."""

import csv
import hashlib
import io
import json
from pathlib import Path
import tarfile

import release
import source_assets as sources


def write_json(path, value):
    path.write_text(json.dumps(value, sort_keys=True, indent=2) + "\n", encoding="utf-8")


def prepare_root(root, version):
    real_root = Path(__file__).resolve().parents[2]
    files = {"VERSION": (version + "\n").encode(), ".go-version": b"1.27.2\n", "LICENSE": b"Synthetic project license.\n",
             "platform/alpine/base.lock": b"fixture Alpine lock\n", "platform/buildroot/base.lock": b"fixture Buildroot lock\n",
             "platform/builder-hosts/alpine-image-x86_64.json": b"fixture Alpine host\n",
             "platform/builder-hosts/buildroot-image-x86_64.json": b"fixture Buildroot host\n",
             "platform/buildroot/fixture.config": b"fixture config\n", "scripts/build/fixture.sh": b"#!/bin/sh\nexit 0\n",
             "scripts/build/filter_alpine_firmware.py": b"fixture filter policy\n",
             "scripts/security/scan_source.py": (real_root / "scripts/security/scan_source.py").read_bytes(),
             "platform/alpine/firmware-exclusions.json": (real_root / "platform/alpine/firmware-exclusions.json").read_bytes()}
    for name, data in files.items():
        path = root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
    project = {name: {"size": len(data), "sha256": sources.sha(data), "mode": "100644"} for name, data in files.items()}
    return files, project


def payload_files(project_files):
    content = io.BytesIO()
    with tarfile.open(fileobj=content, mode="w:gz", format=tarfile.USTAR_FORMAT) as archive:
        data = b"int fixture(void) { return 1; }\n"
        info = tarfile.TarInfo("fixture/source.c")
        info.size = len(data)
        archive.addfile(info, io.BytesIO(data))
    files = {"project/" + name: (data, "project-source") for name, data in project_files.items()}
    files.update({"sources/fixture/source.tar.gz": (content.getvalue(), "source-archive"),
                  "sources/fixture/COPYING": (b"Synthetic fixture permission and attribution.\n", "license-text"),
                  "sources/fixture/build.sh": (b"#!/bin/sh\n# synthetic build controls\n", "build-script")})
    return files


def write_parts(candidate, version, files, count=1):
    names = sources.part_names(version, count)
    rows, parts = [], []
    ordered = sorted(files)
    for position, name in enumerate(names):
        selected = ordered[len(ordered) * position // count:len(ordered) * (position + 1) // count]
        buffer = io.BytesIO()
        with tarfile.open(fileobj=buffer, mode="w", format=tarfile.USTAR_FORMAT) as archive:
            for path in selected:
                data, role = files[path]
                info = tarfile.TarInfo(path)
                info.size, info.mode = len(data), 0o644
                archive.addfile(info, io.BytesIO(data))
                rows.append({"path": path, "role": role, "part": name, "size": len(data),
                             "sha256": sources.sha(data), "mode": "0644"})
        data = buffer.getvalue()
        (candidate / name).write_bytes(data)
        parts.append({"name": name, "size": len(data), "sha256": sources.sha(data)})
    return parts, sorted(rows, key=lambda row: row["path"])


def write_inventories(candidate, version):
    for arch in release.ARCHES:
        image = release.image_name(version, arch)
        app_hash = sources.sha(("synthetic binary " + arch).encode())
        if arch == "legacy-x86":
            output = io.StringIO(newline="")
            writer = csv.writer(output, lineterminator="\n")
            writer.writerow(sources.CSV_FIELDS)
            writer.writerow(("hhsoft-pc-tester", version, "BSD-3-Clause", "LICENSE", "NOASSERTION", "https://example.invalid/project", ""))
            writer.writerow(("fixture-library", "2.3", "MIT", "COPYING", "source.tar.gz", "https://example.invalid/source", ""))
            (candidate / (image + ".packages.csv")).write_text(output.getvalue(), encoding="utf-8")
        else:
            (candidate / (image + ".packages.lock")).write_text(
                f"hhsoft-pc-tester={version} arch={arch} license=BSD-3-Clause origin=hhsoft-pc-tester commit=NOASSERTION sha256={app_hash}\n"
                f"fixture-library=2.3 arch={arch} license=MIT origin=fixture-library commit={'b' * 40} sha256={sources.sha(b'fixture APK')}\n", encoding="utf-8")


def write_source_manifest(candidate, version, commit, files, count=1):
    parts, rows = write_parts(candidate, version, files, count)
    notices = b"Synthetic dependency notices, reviewed for this fixture only.\n"
    (candidate / "THIRD-PARTY-NOTICES.txt").write_bytes(notices)
    (candidate / "SOURCE-OFFER.md").write_text("The supplied fixture source parts reconstruct reviewed source files.\n", encoding="utf-8")
    inventory = sources.inventories(candidate, version)
    packages = {}
    for arch, entries in inventory.items():
        packages[arch] = [{"inventory": row, "source_form": "preferred-source", "sources": ["sources/fixture/source.tar.gz"],
                           "licenses": ["sources/fixture/COPYING"], "build": ["sources/fixture/build.sh"],
                           "notice": {"offset": 0, "size": len(notices), "sha256": sources.sha(notices)},
                           "review": "Synthetic coverage record; not a release approval."} for row in entries]
    manifest = {"schema": 1, "version": version, "source_commit": commit, "parts": parts, "files": rows, "packages": packages}
    write_json(candidate / "SOURCE-MANIFEST.json", manifest)
    return manifest, inventory


def write_image_records(root, candidate, version, commit, project, inventory):
    policy = sources.read_json(root / "platform/alpine/firmware-exclusions.json")
    for arch in release.ARCHES:
        image = release.image_name(version, arch)
        (candidate / image).write_bytes(b"\0" * 32769 + b"CD001" + arch.encode() + b"\0" * 2048)
        image_hash = release.digest(candidate / image)
        target, cpu = {"x86_64": ("amd64", "v1"), "x86": ("386", "sse2"),
                       "legacy-x86": ("386", "softfloat"), "aarch64": ("arm64", "v8.0")}[arch]
        app = {"path": "/usr/bin/hhpt", "size": 128, "sha256": sources.sha(("synthetic binary " + arch).encode()),
               "mode": "0755", "uid": 0, "gid": 0, "version": version, "go_version": "1.27.2", "target_os": "linux",
               "target_arch": target, "cpu_policy": cpu}
        inspection = {"schema": 1, "source_commit": commit, "version": version, "architecture": arch,
                      "image_sha256": image_hash, "application": app, "firmware": None}
        profile = "buildroot-image" if arch == "legacy-x86" else "alpine-image"
        host_name = profile + "-x86_64.json"
        binding = {"schema": 1, "source_commit": commit, "version": version, "architecture": arch,
                   "go_version": "1.27.2", "host_manifest": {"name": host_name, "sha256": release.digest(root / "platform/builder-hosts" / host_name)},
                   "project_files_sha256": sources.sha(sources.canonical({name: {key: row[key] for key in ("size", "sha256")} for name, row in project.items()})),
                   "binary": {key: app[key] for key in ("size", "sha256")}}
        write_json(candidate / (image + ".application.json"), binding)
        if arch != "legacy-x86":
            removed = {"rootfs": [], "modloop": []}
            for entry in policy["entries"]:
                row = {"path": entry["path"] + ".zst", "kind": entry["rootfs_kind"], **entry["stored"]}
                removed["rootfs"].append(row)
                if arch in entry["modloop_architectures"]:
                    removed["modloop"].append(dict(row, kind="file"))
            receipt = {"schema": 1, "status": "firmware_exclusions_applied", "architecture": arch,
                       "policy_sha256": release.digest(root / "platform/alpine/firmware-exclusions.json"),
                       "helper_sha256": release.digest(root / "scripts/build/filter_alpine_firmware.py"),
                       "source_archive_sha256": policy["source_archive"]["sha256"], "removed": removed,
                       "counts": {layer: len(rows) for layer, rows in removed.items()},
                       "checked": {"rootfs_files": 50, "modloop_files": 50, "bytes": 10000}, "release_approval": False}
            write_json(candidate / (image + ".firmware-exclusions.json"), receipt)
            layer = {"status": "pass", "checked_paths": sorted(row["path"] for row in policy["entries"]),
                     "checked_sha256": sorted({row["stored"]["sha256"] for row in policy["entries"]} |
                                              {row["source"]["sha256"] for row in policy["entries"]}),
                     "found_paths": [], "found_sha256": []}
            inspection["firmware"] = {"policy_sha256": receipt["policy_sha256"],
                                       "receipt_sha256": release.digest(candidate / (image + ".firmware-exclusions.json")),
                                       "layers": {"initramfs": layer, "modloop": layer}}
        write_json(candidate / (image + ".inspection.json"), inspection)
        spdx = ["SPDXVersion: SPDX-2.3", "SPDXID: SPDXRef-DOCUMENT", "Creator: Organization: HHSoftSoftware",
                "Creator: Tool: synthetic fixture", "", "PackageName: hhsoft-pc-tester-" + arch + "-image",
                "SPDXID: SPDXRef-Image", "PackageVersion: " + version, "PackageFileName: " + image,
                "PackageChecksum: SHA256: " + image_hash, "PackageLicenseDeclared: NOASSERTION", "PackageLicenseConcluded: NOASSERTION",
                "Relationship: SPDXRef-DOCUMENT DESCRIBES SPDXRef-Image", ""]
        for index, row in enumerate(inventory[arch]):
            name, package_version = (row["PACKAGE"], row["VERSION"]) if arch == "legacy-x86" else (row["name"], row["version"])
            spdx += ["PackageName: " + name, "SPDXID: SPDXRef-Package-" + str(index), "PackageVersion: " + package_version]
            spdx += ["PackageLicenseDeclared: " + ("BSD-3-Clause" if name == "hhsoft-pc-tester" else "NOASSERTION"),
                     "PackageLicenseConcluded: NOASSERTION"]
            if arch != "legacy-x86":
                spdx += ["PackageChecksum: SHA256: " + row["sha256"]]
                spdx += ["PackageComment: consumed build input; arch=" + row["architecture"] + " origin=" + row["origin"] +
                         " source-commit=" + row["commit"] + " declared-license=" + row["license"],
                         "Relationship: SPDXRef-Image GENERATED_FROM SPDXRef-Package-" + str(index)]
            else:
                spdx += ["PackageComment: Buildroot declared licenses: " + row["LICENSE"] + "; license files: " + row["LICENSE FILES"],
                         "Relationship: SPDXRef-Image CONTAINS SPDXRef-Package-" + str(index)]
            spdx.append("")
        (candidate / (image + ".spdx")).write_text("\n".join(spdx), encoding="utf-8", newline="\n")


def write_lab_records(candidate, version, commit):
    records = {}
    evidence = {"assets": {path.name: release.digest(path) for path in candidate.iterdir() if path.is_file()}}
    for arch in release.ARCHES:
        image = release.image_name(version, arch)
        def result(check):
            detail = {"schema": 2, "source_commit": commit, "image_sha256": release.digest(candidate / image),
                      "check": check, "status": "pass", "tool": "synthetic test fixture", "tool_version": "1",
                      "output": "Fixture only; no real boot, source fulfillment or security validation.",
                      "assets": release.review_assets(version, arch, evidence)}
            record_hash = release.record_digest(detail)
            records[record_hash] = detail
            return {"status": "pass", "evidence_sha256": record_hash}
        validation = {"schema": 2, "source_commit": commit, "image_sha256": release.digest(candidate / image),
                      "boot": {mode: result("boot:" + mode) for mode in release.BOOT_MODES[arch]},
                      "reviews": {gate: result("review:" + gate) for gate in release.required_review_gates(version, arch)}}
        write_json(candidate / (image + ".validation.json"), validation)
    write_json(candidate / "lab-evidence.json", {"schema": 2, "records": records})


def candidate(root, version, commit):
    project_files, project = prepare_root(root, version)
    destination = root / "candidate"
    destination.mkdir()
    write_inventories(destination, version)
    payloads = payload_files(project_files)
    _manifest, inventory = write_source_manifest(destination, version, commit, payloads)
    write_image_records(root, destination, version, commit, project, inventory)
    write_lab_records(destination, version, commit)
    return destination, project, payloads
