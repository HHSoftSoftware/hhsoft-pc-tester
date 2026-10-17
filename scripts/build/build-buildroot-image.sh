#!/bin/sh
set -eu
jobs=${HHPT_JOBS:-2}
case $jobs in [1-9]|[1-9][0-9]) ;; *) echo 'HHPT_JOBS must be an integer from 1 to 64' >&2; exit 2 ;; esac
test "$jobs" -le 64 || { echo 'HHPT_JOBS must be an integer from 1 to 64' >&2; exit 2; }
exec /usr/bin/env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin HOME=/dev/null LC_ALL=C TZ=UTC \
	GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null \
	GIT_NO_LAZY_FETCH=1 GIT_NO_REPLACE_OBJECTS=1 \
	/bin/sh -s -- "$0" "$jobs" "$@" <<'HHPT_BUILD'
set -eu
umask 077
script_path=$1
jobs=$2
shift 2
test "$#" -ge 6 && test "$#" -le 9 || { echo 'expected six to nine legacy image arguments' >&2; exit 2; }

source_archive=${1:?verified Buildroot source archive required}
signed_manifest=${2:?Buildroot signed manifest required}
release_key=${3:?Buildroot release public key required}
linux_archive=${4:?verified Linux source archive required}
linux_signature=${5:?Linux release signature required}
linux_release_key=${6:?Linux release public key required}
output_root=${7:-build/images}
work_root=${8:-build/buildroot-work}
source_cache=${9:-}
repository_root=$(CDPATH= cd -- "$(dirname "$script_path")/../.." && pwd)
version_class=$(env -i PATH="$PATH" /usr/bin/python3 -I -B \
	"$repository_root/scripts/build/builder_host_manifest.py" classify-version \
	--version-file "$repository_root/VERSION")
verify_builder_host() {
	env -i PATH="$PATH" /usr/bin/python3 -I -B \
		"$repository_root/scripts/build/builder_host_manifest.py" gate \
		--repository "$repository_root" --profile buildroot-image
}
# A missing or changed reviewed host blocks before extraction or output writes.
verify_builder_host

verify_source_cache() {
	/usr/bin/python3 -I -B "$repository_root/scripts/build/buildroot_source_cache.py" verify \
		--cache "$1" --repository "$repository_root"
}
if test -n "$source_cache"; then
	case $source_cache in /*) ;; *) source_cache=$PWD/$source_cache ;; esac
	verify_source_cache "$source_cache"
	# Namespace creation must work before signer checks or new build work.
	unshare --net true
else
	test "$version_class" = development || {
		echo 'non-development legacy images require a reviewed offline source cache' >&2; exit 1;
	}
fi

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
source_checksum=$(sha256sum "$source_archive")
source_checksum=${source_checksum%% *}
test "${#source_checksum}" -eq 64
case $source_checksum in *[!0-9a-f]*) echo invalid source checksum >&2; exit 1 ;; esac
test "$source_checksum" = "$expected_hash"
test "$(sed -n "s/^SHA256: \([0-9a-f]*\)  $expected_source\$/\1/p" "$signed_manifest")" = "$expected_hash"
test "$linux_source" = "linux-$linux_version.tar.xz"
test "$(basename "$linux_archive")" = "$linux_source"
linux_checksum=$(sha256sum "$linux_archive")
linux_checksum=${linux_checksum%% *}
test "${#linux_checksum}" -eq 64
case $linux_checksum in *[!0-9a-f]*) echo invalid kernel checksum >&2; exit 1 ;; esac
test "$linux_checksum" = "$linux_hash"
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
application_work=$work_root/application-$version_label-legacy-x86
test ! -e "$application_work" && test ! -L "$application_work"
mkdir "$application_work"
application_binding() {
	/usr/bin/python3 -I -B "$repository_root/scripts/build/modern_image_binding.py" "$@" \
		--repository "$repository_root" --work "$application_work" --arch legacy-x86
}
application_binding prepare
source_export=$application_work/application-source
# Only the verified Gitless export may invoke the source scanner or compiler.
/usr/bin/python3 -I -B "$source_export/scripts/security/scan_source.py"
SOURCE_DATE_EPOCH=$(application_binding source-epoch)
case $SOURCE_DATE_EPOCH in ''|*[!0-9]*) echo 'invalid source timestamp' >&2; exit 1 ;; esac
export SOURCE_DATE_EPOCH
# Linux otherwise embeds the native builder's account and hostname in its
# version banner. Keep these public values fixed after clearing caller state.
KBUILD_BUILD_USER=HHSoftSoftware
KBUILD_BUILD_HOST=hhsoft-build
export KBUILD_BUILD_USER KBUILD_BUILD_HOST
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
downloads=$work_root/downloads
if test -n "$source_cache"; then
	/usr/bin/python3 -I -B "$repository_root/scripts/build/buildroot_source_cache.py" stage \
		--cache "$source_cache" --repository "$repository_root" --destination "$downloads"
else
	# Even unqualified builds cannot inherit a previous invocation's downloads.
	test ! -e "$downloads"
	mkdir -p "$downloads/linux"
	cp "$linux_archive" "$downloads/linux/$linux_source"
fi
tar -xJf "$source_archive" --strip-components=1 -C "$source_root"
# Buildroot's legal-info helper otherwise hardlinks both downloaded archives
# and external patches, changing the reviewed inputs' link counts. Apply the
# copy-only host patch to this exact authenticated upstream helper before Make.
test "$version" = 2025.02.18
legal_copy_helper=$source_root/support/scripts/hardlink-or-copy
test -f "$legal_copy_helper" && test ! -L "$legal_copy_helper"
legal_copy_hash=$(sha256sum "$legal_copy_helper")
test "${legal_copy_hash%% *}" = 521e9b58758395b7eb5d9109a42d5db972f3cc5f3b117f0a43c1562295d20384
patch --batch --forward --fuzz=0 -p1 -d "$source_root" \
	< "$source_export/platform/buildroot/host-patches/0001-legal-info-copy-sources.patch"
legal_copy_hash=$(sha256sum "$legal_copy_helper")
test "${legal_copy_hash%% *}" = 367b0615bd8479c4baa1fd87705749d4f7f1da548ad14f8eb7614074ea6baadc
chmod 0755 "$source_export/platform/buildroot/board/legacy-x86/post-build.sh"

set -- /bin/sh "$source_export/scripts/build/build-go.sh" legacy-x86 "$application_work/application-bin"
if test -n "$source_cache"; then unshare --net "$@"; else "$@"; fi
binary=$application_work/application-bin/linux_legacy-x86/hhpt
test -x "$binary"
application_binding complete
test ! -e "$application_work/application-overlay.sha256"
test ! -L "$application_work/application-overlay.sha256"
run_make() {
	if test -n "$source_cache"; then
		unshare --net make "$@"
	else
		make "$@"
	fi
}
run_make -C "$source_root" O="$build_root" BR2_DL_DIR="$downloads" BR2_EXTERNAL="$source_export/platform/buildroot" hhpt_legacy_x86_defconfig
HHPT_BINARY=$binary HHPT_BINDING_REPOSITORY=$repository_root HHPT_BINDING_WORK=$application_work \
	run_make -C "$source_root" -j"$jobs" O="$build_root" BR2_DL_DIR="$downloads" BR2_EXTERNAL="$source_export/platform/buildroot"
application_binding verify --overlay --installed-root "$build_root/target"
run_make -C "$source_root" O="$build_root" BR2_DL_DIR="$downloads" BR2_EXTERNAL="$source_export/platform/buildroot" legal-info
application_binding verify --overlay --installed-root "$build_root/target"
if test -n "$source_cache"; then
	verify_source_cache "$source_cache"
	verify_source_cache "$downloads"
fi

artifact_name=hhsoft-pc-tester-$version_label-legacy-x86.iso
mkdir -p "$output_root"
cp "$build_root/images/rootfs.iso9660" "$output_root/$artifact_name"
cp "$build_root/legal-info/manifest.csv" "$output_root/$artifact_name.packages.csv"
printf '"hhsoft-pc-tester","%s","BSD-3-Clause","LICENSE","NOASSERTION","https://github.com/HHSoftSoftware/hhsoft-pc-tester",""\n' "$version_label" >> "$output_root/$artifact_name.packages.csv"
cp "$build_root/legal-info/host-manifest.csv" "$output_root/$artifact_name.host-packages.csv"
cp "$source_export/LICENSE" "$output_root/$artifact_name.LICENSE.txt"
(cd "$output_root" && sha256sum "$artifact_name" > "$artifact_name.sha256")
/usr/bin/python3 -I -B "$source_export/scripts/build/write_buildroot_sbom.py" \
    --manifest "$output_root/$artifact_name.packages.csv" \
    --image "$output_root/$artifact_name" \
    --version "$version_label" \
    --epoch "$SOURCE_DATE_EPOCH" \
    --output "$output_root/$artifact_name.spdx"
application_binding verify --overlay --installed-root "$build_root/target"
verify_builder_host
HHPT_BUILD
