#!/bin/sh
set -eu

architecture=${1:?architecture required}
image=${2:?image required}
binary=${3:?binary required}
package_root=${4:?package root required}
case $architecture in x86|x86_64|aarch64) ;; *) echo unsupported image architecture >&2; exit 2 ;; esac
test -d "$package_root"
repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
temporary_directory=$(mktemp -d)
cleanup() {
	rm -rf -- "$temporary_directory"
}
trap cleanup EXIT

records=$temporary_directory/records
: > "$records"
find "$package_root" -type f -name '*.apk' -print > "$temporary_directory/archives"
sort "$temporary_directory/archives" -o "$temporary_directory/archives"
test -s "$temporary_directory/archives" || { echo missing consumed package archives >&2; exit 1; }
while IFS= read -r package; do
	metadata=$(tar -xOf "$package" .PKGINFO)
	field() {
		printf '%s\n' "$metadata" | sed -n "s/^$1 = //p" | head -1 | tr -d '\r\n' | tr '\t' ' '
	}
	name=$(field pkgname)
	version=$(field pkgver)
	package_architecture=$(field arch)
	license=$(field license)
	origin=$(field origin)
	commit=$(field commit)
	test -n "$name"
	test -n "$version"
	test -n "$package_architecture"
	test -n "$license" || license=NOASSERTION
	test -n "$origin" || origin=NOASSERTION
	test -n "$commit" || commit=NOASSERTION
	checksum=$(sha256sum "$package" | cut -d' ' -f1)
	printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$name" "$version" "$package_architecture" "$license" "$origin" "$commit" "$checksum" >> "$records"
done < "$temporary_directory/archives"

# These are consumed outside the ISO's ordinary runtime package directory.
# Missing entries indicate an incomplete recording, even if the ISO booted.
required_packages="alpine-base linux-lts grub-efi"
case $architecture in x86|x86_64) required_packages="$required_packages syslinux" ;; esac
for required in $required_packages; do
	awk -F '\t' -v name="$required" '$1 == name { found=1 } END { exit !found }' "$records" || {
		echo incomplete kernel or bootloader package inventory >&2; exit 1;
	}
done

version=$(tr -d '\r\n' < "$repository_root/VERSION")
binary_checksum=$(sha256sum "$binary" | cut -d' ' -f1)
printf 'hhsoft-pc-tester\t%s\t%s\tBSD-3-Clause\thhsoft-pc-tester\tNOASSERTION\t%s\n' "$version" "$architecture" "$binary_checksum" >> "$records"
sort -u "$records" -o "$records.sorted"
awk -F '\t' '{ key=$1 FS $2 FS $3; if (key in checksum && checksum[key] != $7) exit 1; checksum[key]=$7 }' "$records.sorted"

image_checksum=$(sha256sum "$image" | cut -d' ' -f1)
created=$(date -u -d "@${SOURCE_DATE_EPOCH:?SOURCE_DATE_EPOCH required}" '+%Y-%m-%dT%H:%M:%SZ')
sbom=$image.spdx
package_lock=$image.packages.lock
{
	printf 'SPDXVersion: SPDX-2.3\n'
	printf 'DataLicense: CC0-1.0\n'
	printf 'SPDXID: SPDXRef-DOCUMENT\n'
	printf 'DocumentName: hhsoft-pc-tester-%s-%s\n' "$version" "$architecture"
	printf 'DocumentNamespace: https://github.com/HHSoftSoftware/hhsoft-pc-tester/sbom/%s/%s/%s\n' "$version" "$architecture" "$image_checksum"
	printf 'Creator: Organization: HHSoftSoftware\n'
	printf 'Creator: Tool: scripts/build/write-alpine-sbom.sh\n'
	printf 'Created: %s\n\n' "$created"
	printf 'PackageName: hhsoft-pc-tester-%s-image\n' "$architecture"
	printf 'SPDXID: SPDXRef-Image\nPackageVersion: %s\n' "$version"
	printf 'PackageFileName: %s\n' "$(basename "$image")"
	printf 'PackageDownloadLocation: NOASSERTION\nFilesAnalyzed: false\n'
	printf 'PackageChecksum: SHA256: %s\n' "$image_checksum"
	printf 'PackageLicenseConcluded: NOASSERTION\nPackageLicenseDeclared: NOASSERTION\n'
	printf 'PackageCopyrightText: NOASSERTION\n'
	printf 'Relationship: SPDXRef-DOCUMENT DESCRIBES SPDXRef-Image\n\n'
	while IFS="$(printf '\t')" read -r name package_version package_architecture license origin commit checksum; do
		package_id=$(printf '%s\t%s\t%s' "$name" "$package_version" "$package_architecture" | sha256sum | cut -c1-24)
		printf 'PackageName: %s\n' "$name"
		printf 'SPDXID: SPDXRef-Package-%s\n' "$package_id"
		printf 'PackageVersion: %s\n' "$package_version"
		if test "$name" = hhsoft-pc-tester; then
			printf 'PackageFileName: hhpt\n'
		else
			printf 'PackageFileName: %s-%s.apk\n' "$name" "$package_version"
		fi
		printf 'PackageDownloadLocation: NOASSERTION\n'
		printf 'FilesAnalyzed: false\n'
		printf 'PackageChecksum: SHA256: %s\n' "$checksum"
		printf 'PackageLicenseConcluded: NOASSERTION\n'
		if test "$name" = hhsoft-pc-tester; then declared=BSD-3-Clause; else declared=NOASSERTION; fi
		printf 'PackageLicenseDeclared: %s\n' "$declared"
		printf 'PackageCopyrightText: NOASSERTION\n'
		printf 'PackageComment: consumed build input; arch=%s origin=%s source-commit=%s declared-license=%s\n' "$package_architecture" "$origin" "$commit" "$license"
		printf 'Relationship: SPDXRef-Image GENERATED_FROM SPDXRef-Package-%s\n\n' "$package_id"
	done < "$records.sorted"
} > "$sbom"

awk -F '\t' '{ printf "%s=%s arch=%s license=%s origin=%s commit=%s sha256=%s\n", $1, $2, $3, $4, $5, $6, $7 }' "$records.sorted" > "$package_lock"
