#!/bin/sh
set -eu

target_directory=${1:?target directory required}
binary=${HHPT_BINARY:?HHPT_BINARY required}
binding_repository=${HHPT_BINDING_REPOSITORY:?HHPT_BINDING_REPOSITORY required}
binding_work=${HHPT_BINDING_WORK:?HHPT_BINDING_WORK required}
source_export=$(CDPATH= cd -- "$(dirname -- "$0")/../../../.." && pwd)
test -x "$binary"
test ! -e "$binding_work/application-overlay.sha256"
test ! -L "$binding_work/application-overlay.sha256"
install -D -m 0755 "$binary" "$target_directory/usr/bin/hhpt"
chmod 0755 "$target_directory/etc/init.d/S50hhpt"
rm -f "$target_directory/etc/init.d/S40network" "$target_directory/etc/init.d/S50crond"
sed -i 's/^root:[^:]*:/root:*:/' "$target_directory/etc/shadow"
# Read the installed inode through the shared descriptor-pinned verifier. A
# copied link, special file, changed byte, or reused receipt cannot succeed.
/usr/bin/python3 -I -B "$source_export/scripts/build/modern_image_binding.py" record-installed \
	--repository "$binding_repository" --work "$binding_work" --arch legacy-x86 \
	--installed-root "$target_directory"
