#!/bin/sh
set -eu

target_directory=${1:?target directory required}
binary=${HHPT_BINARY:?HHPT_BINARY required}
test -x "$binary"
install -D -m 0755 "$binary" "$target_directory/usr/bin/hhpt"
chmod 0755 "$target_directory/etc/init.d/S50hhpt"
rm -f "$target_directory/etc/init.d/S40network" "$target_directory/etc/init.d/S50crond"
sed -i 's/^root:[^:]*:/root:*:/' "$target_directory/etc/shadow"
