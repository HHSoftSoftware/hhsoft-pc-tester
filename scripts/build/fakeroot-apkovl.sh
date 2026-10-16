#!/bin/sh
set -eu

# Pinned mkimage starts fakeroot again for apkovl. This build already runs in
# one real session: execute only its controlled generator in that same session.
if test "$#" -ne 2 || test "$1" != "${HHPT_APKOVL_SCRIPT:-}" || test "$2" != hhpt; then
	echo unsupported nested image fakeroot invocation >&2
	exit 2
fi
case "$1" in
	/*/genapkovl-hhpt.sh) ;;
	*) echo invalid controlled apkovl generator >&2; exit 2 ;;
esac
if test -z "${FAKEROOTKEY:-}" || test -z "${LD_PRELOAD:-}"; then
	echo apkovl requires the outer image fakeroot session >&2
	exit 2
fi
# Keep both variables unchanged so chown/install/tar share the outer database.
exec "$1" "$2"
