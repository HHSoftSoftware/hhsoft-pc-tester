#!/bin/sh
set -eu

# lddtree needs ELF contents, not fakeroot's ownership/device metadata. scanelf
# creates its own IPC namespace, which cannot reach the outer faked daemon.
# Accept only lddtree's single-file read queries; keep scanelf's sandbox intact.
if test "$#" -ne 3; then
	echo unsupported build scanelf invocation >&2
	exit 2
fi
case "$1:$2" in
	'-BF:#F%a %M %D %I'|'-qF:#F%i'|'-qF:#F%n'|'-qF:#F%r') ;;
	*) echo unsupported build scanelf invocation >&2; exit 2 ;;
esac
test -n "$3" || { echo missing build scanelf input >&2; exit 2; }
case ${HHPT_REAL_SCANELF:-} in
	/*) ;;
	*) echo missing controlled scanelf executable >&2; exit 2 ;;
esac
# The image's clean environment supplies only fakeroot in LD_PRELOAD. This
# changes the scanner child only; packaging operations retain fake metadata.
unset LD_PRELOAD FAKEROOTKEY
exec "$HHPT_REAL_SCANELF" "$1" "$2" -- "$3"
