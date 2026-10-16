#!/bin/sh
set -eu
umask 077
PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PATH

architecture=${1:-x86_64}
output_root=${2:-build/images}
aports_root=${3:-/tmp/aports-v3.24.2}
signing_key=${4:?signing key path required}
work_root=${5:-/tmp/hhpt-image-work}
package_cache=${6:-}
case $architecture in x86|x86_64|aarch64) ;; *) echo unsupported image architecture >&2; exit 2 ;; esac
repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
sh "$repository_root/scripts/security/scan-source.sh"
lock_file=$repository_root/platform/alpine/base.lock
case $output_root in
	/*) ;;
	*) output_root=$repository_root/$output_root ;;
esac
aports_root=$(CDPATH= cd -- "$aports_root" && pwd)
signing_key=$(CDPATH= cd -- "$(dirname -- "$signing_key")" && printf '%s/%s\n' "$PWD" "$(basename -- "$signing_key")")
case $work_root in /*) ;; *) work_root=$repository_root/$work_root ;; esac
# Upstream mkimage/update-kernel split several path arguments as shell words.
for path in "$repository_root" "$aports_root" "$signing_key" "$work_root" "$output_root"; do
	case $path in *[!A-Za-z0-9_./-]*) echo unsupported image build path >&2; exit 2 ;; esac
done

lock_value() {
	sed -n "s/^$1=//p" "$lock_file"
}

branch=$(lock_value BRANCH)
tag=$(lock_value APORTS_TAG)
tag_object=$(lock_value APORTS_TAG_OBJECT)
commit=$(lock_value APORTS_COMMIT)
product_version=$(tr -d '\r\n' < "$repository_root/VERSION")
printf '%s\n' "$product_version" | grep -Eq '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$'

main_repository=https://dl-cdn.alpinelinux.org/alpine/$branch/main
community_repository=https://dl-cdn.alpinelinux.org/alpine/$branch/community
if test -n "$package_cache"; then
	package_cache=$(CDPATH= cd -- "$package_cache" && pwd)
	# apk's repository syntax requires an unambiguous local path.
	case $package_cache in *[!A-Za-z0-9_./-]*) echo unsupported package cache path >&2; exit 2 ;; esac
	package_lock=$repository_root/platform/alpine/package-locks/$architecture.json
	python3 "$repository_root/scripts/build/alpine_package_cache.py" verify \
		--cache "$package_cache" --arch "$architecture" --base-lock "$lock_file" --lock "$package_lock"
	main_repository=$package_cache/main
	community_repository=$package_cache/community
	# Fail before starting work if network namespace isolation is unavailable.
	unshare --net true
else
	case $product_version in
		*-dev.*) ;;
		*) echo 'non-development images require a reviewed offline package cache' >&2; exit 1 ;;
	esac
fi
test "$(git -c safe.directory="$aports_root" -C "$aports_root" rev-parse "$tag")" = "$tag_object"
test "$(git -c safe.directory="$aports_root" -C "$aports_root" rev-parse "$tag^{commit}")" = "$commit"
test "$(git -c safe.directory="$aports_root" -C "$aports_root" rev-parse HEAD)" = "$commit"
(
	cd "$aports_root"
	sha256sum -c "$repository_root/platform/alpine/mkimage/upstream-scripts.sha256"
)

binary=$repository_root/build/bin/linux_$architecture/hhpt
test -x "$binary"
test -f "$signing_key"
test -f "$signing_key.pub"

# The image step must use already-installed builder tools, never host apk add.
for tool in apk fakeroot update-kernel mkinitfs mksquashfs modinfo depmod openssl \
	xorrisofs grub-mkimage mformat mcopy python3 scanelf lddtree; do
	command -v "$tool" >/dev/null || { echo missing image builder tool >&2; exit 1; }
done
real_apk=$(command -v apk)
real_scanelf=$(command -v scanelf)
real_fakeroot=$(command -v fakeroot)
mkdir -p "$work_root"
if test -n "$package_cache"; then
	cache_digest=$(sha256sum "$package_lock" | cut -d' ' -f1)
	image_work=$work_root/locked-$architecture-$product_version-$cache_digest
	test ! -e "$image_work" # A release build must not inherit an old apk root.
	mkdir "$image_work"
else
	# Fresh sections also keep the consumed-input inventory exact in dev builds.
	image_work=$(mktemp -d "$work_root/dev-$architecture.XXXXXX")
fi
tools=$(mktemp -d "$work_root/tools-$architecture.XXXXXX")
mkdir -p "$tools/host-applets" "$tools/user-config/.abuild" "$tools/tmp" "$output_root" "$image_work/consumed-apks"
# Compare public encodings without exposing private key bytes to logs.
openssl pkey -in "$signing_key" -pubout -out "$tools/derived.pub"
openssl pkey -pubin -in "$tools/derived.pub" -outform DER -out "$tools/derived.der"
openssl pkey -pubin -in "$signing_key.pub" -outform DER -out "$tools/supplied.der"
cmp -s "$tools/derived.der" "$tools/supplied.der" || { echo signing key pair differs >&2; exit 1; }
cat > "$tools/abuild.conf" <<'EOF'
PACKAGER="HHSoft Software <info.hhsoft@gmail.com>"
PACKAGER_PRIVKEY="$HHPT_SIGNING_KEY"
PACKAGER_PUBKEY="$HHPT_SIGNING_PUBLIC_KEY"
EOF
apk_root=$image_work/apkroot-$architecture
if test "$(id -u)" -eq 0 && ! test -f "$apk_root/lib/apk/db/installed"; then
	mkdir -p "$apk_root/etc/apk/cache" "$apk_root/etc/apk/keys"
	cp /usr/share/apk/keys/"$architecture"/* "$apk_root/etc/apk/keys/"
	cp "$signing_key.pub" "$apk_root/etc/apk/keys/"
	"$real_apk" --no-network --arch "$architecture" --root "$apk_root" add --initdb
	printf '%s\n%s\n' \
		"$main_repository" \
		"$community_repository" \
		> "$apk_root/etc/apk/repositories"
fi
ln -s /bin/busybox "$tools/host-applets/cp"
cp "$repository_root/scripts/build/record_alpine_apks.py" "$tools/host-applets/apk"
cp "$repository_root/scripts/build/scanelf-readonly.sh" "$tools/host-applets/scanelf"
cp "$repository_root/scripts/build/fakeroot-apkovl.sh" "$tools/host-applets/fakeroot"
chmod 0755 "$tools/host-applets/apk" "$tools/host-applets/scanelf" "$tools/host-applets/fakeroot"
for script in mkimage.sh mkimage-yaml.sh mkimg.base.sh mkimg.standard.sh; do
	cp "$aports_root/scripts/$script" "$tools/$script"
done
cp "$repository_root/platform/alpine/mkimage/mkimg.hhpt.sh" "$tools/mkimg.zz-hhpt.sh"
cp "$repository_root/platform/alpine/mkimage/genapkovl-hhpt.sh" "$tools/"
chmod 0755 "$tools/mkimage.sh" "$tools/mkimage-yaml.sh" "$tools/genapkovl-hhpt.sh"

export SOURCE_DATE_EPOCH=$(git -c safe.directory="$aports_root" -C "$aports_root" show -s --format=%ct "$commit")
(
	cd "$tools"
	set -- ./mkimage.sh \
		--profile hhpt \
		--arch "$architecture" \
		--tag "$product_version" \
		--workdir "$image_work" \
		--outdir "$output_root" \
		--repository "$main_repository" \
		--repository "$community_repository" \
		--checksum
	offline=0
	if test -n "$package_cache"; then offline=1; fi
	# A real fakeroot session is an upstream-supported update-kernel mode. Its
	# FAKEROOTKEY prevents both host apk add and the corresponding host apk del.
	# Clean HOME/config prevent user plugins and ambient signing overrides.
	set -- env -i \
		PATH="$tools/host-applets:/usr/sbin:/usr/bin:/sbin:/bin" \
		HOME="$tools/user-config" LC_ALL=C TZ=UTC TMPDIR="$tools/tmp" \
		ABUILD_CONF="$tools/abuild.conf" ABUILD_USERDIR="$tools/user-config/.abuild" \
		ABUILD_USERCONF="$tools/abuild.conf" ABUILD_SHAREDIR=/usr/share/abuild \
		HHPT_BINARY="$binary" HHPT_SIGNING_KEY="$signing_key" \
		HHPT_SIGNING_PUBLIC_KEY="$signing_key.pub" \
		PACKAGER_PRIVKEY="$signing_key" PACKAGER_PUBKEY="$signing_key.pub" \
		SOURCE_DATE_EPOCH="$SOURCE_DATE_EPOCH" \
		HHPT_APKOVL_SCRIPT="$tools/genapkovl-hhpt.sh" \
		HHPT_REAL_SCANELF="$real_scanelf" \
		HHPT_REAL_APK="$real_apk" HHPT_APK_OFFLINE="$offline" \
		HHPT_APK_RECORD_DIR="$image_work/consumed-apks" \
		HHPT_APK_BUILD_ROOT="$image_work" HHPT_APK_TMP_ROOT="$tools/tmp" \
		"$real_fakeroot" -- "$@"
	if test -n "$package_cache"; then
		unshare --net "$@"
	else
		"$@"
	fi
)
if test -n "$package_cache"; then
	python3 "$repository_root/scripts/build/alpine_package_cache.py" verify \
		--cache "$package_cache" --arch "$architecture" --base-lock "$lock_file" --lock "$package_lock"
fi
image=$output_root/hhsoft-pc-tester-$product_version-$architecture.iso
sh "$repository_root/scripts/build/write-alpine-sbom.sh" "$architecture" "$image" "$binary" "$image_work/consumed-apks"
