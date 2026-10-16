#!/bin/sh
set -eu

if test "$(id -u)" -ne 0; then
	echo 'run only as root inside the disposable Alpine builder VM' >&2
	exit 1
fi

repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
branch=$(sed -n 's/^BRANCH=//p' "$repository_root/platform/alpine/base.lock")
case $branch in
	v[0-9]*.[0-9]*) ;;
	*) echo 'invalid Alpine branch lock' >&2; exit 1 ;;
esac

printf '%s\n%s\n' \
	"https://dl-cdn.alpinelinux.org/alpine/$branch/main" \
	"https://dl-cdn.alpinelinux.org/alpine/$branch/community" \
	> /etc/apk/repositories
apk update
apk add \
	alpine-sdk bash bc bison cpio coreutils curl diffutils dosfstools \
	e2fsprogs findutils flex gawk git gnupg go grep linux-headers mtools \
	ncurses-dev openssh openssl patch perl python3 qemu-hw-display-virtio-gpu-pci \
	qemu-img qemu-system-aarch64 qemu-system-x86_64 qpdf rsync sed socat \
	squashfs-tools tar util-linux wget xorriso xz

printf 'builder ready for branch %s; keep build work on verified VDI scratch\n' "$branch"
