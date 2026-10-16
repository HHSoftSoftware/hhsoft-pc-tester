#!/usr/bin/env python3
"""Write deterministic SPDX 2.3 from Buildroot legal-info and the built ISO.

Keep Buildroot's compound license descriptions as comments. They are not
necessarily valid SPDX expressions, so do not invent license conclusions.
"""

import argparse
import csv
from datetime import datetime, timezone
import hashlib
from pathlib import Path
import re
import sys
from urllib.parse import quote, urlsplit


FIELDS = ("PACKAGE", "VERSION", "LICENSE", "LICENSE FILES", "SOURCE ARCHIVE", "SOURCE SITE", "DEPENDENCIES WITH LICENSES")


def sha256(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def line(value):
    if not isinstance(value, str) or any(ord(char) < 32 or ord(char) == 127 for char in value):
        raise ValueError("invalid multiline or control-character package metadata")
    return value


def download_location(site, archive):
    if archive in ("", "NOASSERTION"):
        return "NOASSERTION"
    parsed = urlsplit(site)
    if (parsed.scheme not in ("https", "http", "ftp") or not parsed.netloc or
            parsed.username or parsed.password or parsed.query or parsed.fragment or
            any(char.isspace() for char in site)):
        return "NOASSERTION"
    if Path(archive).name != archive or "/" in archive or "\\" in archive or archive in (".", ".."):
        raise ValueError("source archive must be a basename")
    return site.rstrip("/") + "/" + quote(archive, safe="-._~")


def render(manifest, image, version, epoch):
    if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.-]+)?", version):
        raise ValueError("invalid product version")
    if image.is_symlink() or not image.is_file():
        raise ValueError("image must be a regular file")
    with manifest.open(encoding="utf-8-sig", newline="") as stream:
        reader = csv.DictReader(stream, strict=True)
        if reader.fieldnames != list(FIELDS):
            raise ValueError("unexpected Buildroot manifest schema")
        packages = []
        seen = set()
        for row in reader:
            if set(row) != set(FIELDS):
                raise ValueError("invalid manifest row")
            row = {key: line(value) for key, value in row.items()}
            if not row["PACKAGE"] or not row["VERSION"] or row["PACKAGE"] in seen:
                raise ValueError("empty or duplicate package")
            seen.add(row["PACKAGE"])
            packages.append(row)
    if not packages:
        raise ValueError("empty package manifest")
    product = [row for row in packages if row["PACKAGE"] == "hhsoft-pc-tester"]
    if len(product) != 1 or product[0]["VERSION"] != version:
        raise ValueError("product version missing or inconsistent in manifest")
    image_hash = sha256(image)
    created = datetime.fromtimestamp(epoch, timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    output = [
        "SPDXVersion: SPDX-2.3", "DataLicense: CC0-1.0", "SPDXID: SPDXRef-DOCUMENT",
        f"DocumentName: hhsoft-pc-tester-{version}-legacy-x86",
        "DocumentNamespace: https://github.com/HHSoftSoftware/hhsoft-pc-tester/sbom/"
        + f"{version}/legacy-x86/{image_hash}",
        "Creator: Organization: HHSoftSoftware", "Creator: Tool: scripts/build/write_buildroot_sbom.py",
        "Created: " + created, "", "PackageName: hhsoft-pc-tester-legacy-x86-image",
        "SPDXID: SPDXRef-Image", "PackageVersion: " + version,
        "PackageFileName: " + line(image.name), "PackageDownloadLocation: NOASSERTION",
        "FilesAnalyzed: false", "PackageChecksum: SHA256: " + image_hash,
        "PackageLicenseConcluded: NOASSERTION", "PackageLicenseDeclared: NOASSERTION",
        "PackageCopyrightText: NOASSERTION", "Relationship: SPDXRef-DOCUMENT DESCRIBES SPDXRef-Image", "",
    ]
    for row in sorted(packages, key=lambda item: item["PACKAGE"]):
        package_id = "SPDXRef-Package-" + hashlib.sha256((row["PACKAGE"] + "\0" + row["VERSION"]).encode()).hexdigest()[:24]
        declared = "BSD-3-Clause" if row["PACKAGE"] == "hhsoft-pc-tester" else "NOASSERTION"
        output += [
            "PackageName: " + row["PACKAGE"], "SPDXID: " + package_id,
            "PackageVersion: " + row["VERSION"],
            "PackageDownloadLocation: " + download_location(row["SOURCE SITE"], row["SOURCE ARCHIVE"]),
            "FilesAnalyzed: false", "PackageLicenseConcluded: NOASSERTION",
            "PackageLicenseDeclared: " + declared, "PackageCopyrightText: NOASSERTION",
            "PackageComment: Buildroot declared licenses: " + row["LICENSE"]
            + "; license files: " + row["LICENSE FILES"],
            "Relationship: SPDXRef-Image CONTAINS " + package_id, "",
        ]
    return "\n".join(output)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--image", type=Path, required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--epoch", type=int, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    content = render(args.manifest, args.image, args.version, args.epoch)
    with args.output.open("x", encoding="utf-8", newline="\n") as stream:
        stream.write(content)
    print("legacy SPDX inventory: written")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, csv.Error, OverflowError):
        print("legacy SPDX inventory: invalid input or output already exists", file=sys.stderr)
        sys.exit(1)
