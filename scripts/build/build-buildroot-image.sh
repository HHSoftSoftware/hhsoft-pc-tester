#!/bin/sh
set -eu
jobs=${HHPT_JOBS:-2}
case $jobs in [1-9]|[1-9][0-9]) ;; *) echo 'HHPT_JOBS must be an integer from 1 to 64' >&2; exit 2 ;; esac
test "$jobs" -le 64 || { echo 'HHPT_JOBS must be an integer from 1 to 64' >&2; exit 2; }
exec /usr/bin/env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin HOME=/dev/null LC_ALL=C TZ=UTC \
	GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null \
	/bin/sh -s -- "$0" "$jobs" "$@" <<'HHPT_BUILD'
set -eu
umask 077
script_path=$1
jobs=$2
shift 2
test "$#" -ge 6 && test "$#" -le 8 || { echo 'expected six to eight legacy image arguments' >&2; exit 2; }

source_archive=${1:?verified Buildroot source archive required}
signed_manifest=${2:?Buildroot signed manifest required}
release_key=${3:?Buildroot release public key required}
linux_archive=${4:?verified Linux source archive required}
linux_signature=${5:?Linux release signature required}
linux_release_key=${6:?Linux release public key required}
output_root=${7:-build/images}
work_root=${8:-build/buildroot-work}
repository_root=$(CDPATH= cd -- "$(dirname "$script_path")/../.." && pwd)
/usr/bin/python3 -I -B "$repository_root/scripts/security/scan_source.py"
verify_builder_host() {
	env -i PATH="$PATH" /usr/bin/python3 -I -B \
		"$repository_root/scripts/build/builder_host_manifest.py" gate \
		--repository "$repository_root" --profile buildroot-image
}
# A missing or changed reviewed host blocks before extraction or output writes.
verify_builder_host

# Inputs keep their caller-relative meaning; work and output paths must be
# absolute because upstream make changes its working directory.
case $source_archive in /*) ;; *) source_archive=$PWD/$source_archive ;; esac
case $signed_manifest in /*) ;; *) signed_manifest=$PWD/$signed_manifest ;; esac
case $release_key in /*) ;; *) release_key=$PWD/$release_key ;; esac
case $linux_archive in /*) ;; *) linux_archive=$PWD/$linux_archive ;; esac
case $linux_signature in /*) ;; *) linux_signature=$PWD/$linux_signature ;; esac
case $linux_release_key in /*) ;; *) linux_release_key=$PWD/$linux_release_key ;; esac
case $output_root in /*) ;; *) output_root=$repository_root/$output_root ;; esac
case $work_root in /*) ;; *) work_root=$repository_root/$work_root ;; esac

lock_value() {
	sed -n "s/^$1=//p" "$repository_root/platform/buildroot/base.lock"
}

version=$(lock_value VERSION)
expected_source=$(lock_value SOURCE)
expected_hash=$(lock_value SHA256)
expected_fingerprint=$(lock_value UPSTREAM_GPG_FINGERPRINT)
linux_version=$(lock_value LINUX_VERSION)
linux_source=$(lock_value LINUX_SOURCE)
linux_hash=$(lock_value LINUX_SHA256)
linux_signature_name=$(lock_value LINUX_SIGNATURE)
linux_fingerprint=$(lock_value LINUX_SIGNER_FINGERPRINT)
version_label=$(tr -d '\r\n' <"$repository_root/VERSION")

test "$(basename "$source_archive")" = "$expected_source"
test "$(sha256sum "$source_archive" | awk '{print $1}')" = "$expected_hash"
test "$(sed -n "s/^SHA256: \([0-9a-f]*\)  $expected_source\$/\1/p" "$signed_manifest")" = "$expected_hash"
test "$linux_source" = "linux-$linux_version.tar.xz"
test "$(basename "$linux_archive")" = "$linux_source"
test "$(sha256sum "$linux_archive" | awk '{print $1}')" = "$linux_hash"
test "$(basename "$linux_signature")" = "$linux_signature_name"
test "$(sed -n 's/^BR2_LINUX_KERNEL_CUSTOM_VERSION_VALUE="\([^"]*\)"$/\1/p' "$repository_root/platform/buildroot/configs/hhpt_legacy_x86_defconfig")" = "$linux_version"
for kernel_hash_file in linux/linux.hash linux-headers/linux-headers.hash; do
	test "$(sed -n "s/^sha256  \([0-9a-f]*\)  $linux_source\$/\1/p" "$repository_root/platform/buildroot/patches/$kernel_hash_file")" = "$linux_hash"
done

mkdir -p "$work_root"
build_environment=$(mktemp -d "$work_root/environment.XXXXXX")
HOME=$build_environment/home
TMPDIR=$build_environment/tmp
XDG_CONFIG_HOME=$build_environment/config
XDG_CACHE_HOME=$build_environment/cache
mkdir -p "$HOME" "$TMPDIR" "$XDG_CONFIG_HOME" "$XDG_CACHE_HOME"
export HOME TMPDIR XDG_CONFIG_HOME XDG_CACHE_HOME
SOURCE_DATE_EPOCH=$(git -c safe.directory="$repository_root" -C "$repository_root" show -s --format=%ct HEAD)
case $SOURCE_DATE_EPOCH in ''|*[!0-9]*) echo 'invalid source timestamp' >&2; exit 1 ;; esac
export SOURCE_DATE_EPOCH
gnupg_home=$(mktemp -d "$work_root/gnupg.XXXXXX")
cleanup() {
	rm -rf -- "$gnupg_home"
}
trap cleanup EXIT
GNUPGHOME=$gnupg_home gpg --batch --quiet --import "$release_key"
actual_fingerprint=$(GNUPGHOME=$gnupg_home gpg --batch --with-colons --list-keys | awk -F: '$1 == "fpr" { print $10; exit }')
test "$actual_fingerprint" = "$expected_fingerprint"
GNUPGHOME=$gnupg_home gpg --batch --quiet --verify "$signed_manifest"

linux_gnupg_home=$(mktemp -d "$work_root/linux-gnupg.XXXXXX")
cleanup_linux() {
	rm -rf -- "$linux_gnupg_home"
}
trap 'cleanup_linux; cleanup' EXIT
GNUPGHOME=$linux_gnupg_home gpg --batch --quiet --import "$linux_release_key"
actual_linux_fingerprint=$(GNUPGHOME=$linux_gnupg_home gpg --batch --with-colons --list-keys | awk -F: '$1 == "fpr" { print $10; exit }')
test "$actual_linux_fingerprint" = "$linux_fingerprint"
xz -cd "$linux_archive" | GNUPGHOME=$linux_gnupg_home gpg --batch --quiet --verify "$linux_signature" -

source_root=$work_root/source-$version
build_root=$work_root/output-$version_label-legacy-x86
test ! -e "$source_root"
test ! -e "$build_root"
mkdir -p "$source_root" "$build_root"
tar -xJf "$source_archive" --strip-components=1 -C "$source_root"
chmod 0755 "$repository_root/platform/buildroot/board/legacy-x86/post-build.sh"
mkdir -p "$work_root/downloads/linux"
cp "$linux_archive" "$work_root/downloads/linux/$linux_source"

sh "$repository_root/scripts/build/build-go.sh" legacy-x86 "$work_root/hhpt-bin"
binary=$work_root/hhpt-bin/linux_legacy-x86/hhpt
make -C "$source_root" O="$build_root" BR2_DL_DIR="$work_root/downloads" BR2_EXTERNAL="$repository_root/platform/buildroot" hhpt_legacy_x86_defconfig
HHPT_BINARY=$binary make -C "$source_root" -j"$jobs" O="$build_root" BR2_DL_DIR="$work_root/downloads" BR2_EXTERNAL="$repository_root/platform/buildroot"
HHPT_BINARY=$binary make -C "$source_root" O="$build_root" BR2_DL_DIR="$work_root/downloads" BR2_EXTERNAL="$repository_root/platform/buildroot" legal-info

artifact_name=hhsoft-pc-tester-$version_label-legacy-x86.iso
mkdir -p "$output_root"
cp "$build_root/images/rootfs.iso9660" "$output_root/$artifact_name"
cp "$build_root/legal-info/manifest.csv" "$output_root/$artifact_name.packages.csv"
printf '"hhsoft-pc-tester","%s","BSD-3-Clause","LICENSE","NOASSERTION","https://github.com/HHSoftSoftware/hhsoft-pc-tester",""\n' "$version_label" >> "$output_root/$artifact_name.packages.csv"
cp "$build_root/legal-info/host-manifest.csv" "$output_root/$artifact_name.host-packages.csv"
cp "$repository_root/LICENSE" "$output_root/$artifact_name.LICENSE.txt"
(cd "$output_root" && sha256sum "$artifact_name" > "$artifact_name.sha256")
/usr/bin/python3 -I -B "$repository_root/scripts/build/write_buildroot_sbom.py" \
    --manifest "$output_root/$artifact_name.packages.csv" \
    --image "$output_root/$artifact_name" \
    --version "$version_label" \
    --epoch "$SOURCE_DATE_EPOCH" \
    --output "$output_root/$artifact_name.spdx"
verify_builder_host
HHPT_BUILD
