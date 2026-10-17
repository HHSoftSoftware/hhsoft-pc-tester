#!/bin/sh
set -eu

# setup-go and native builders install Go in different locations. Select one
# executable once, then discard the caller's tool/configuration environment.
go_executable=$(command -v go) || { echo 'install the pinned Go toolchain first' >&2; exit 1; }
case $go_executable in /*) ;; *) echo 'Go must resolve to an absolute executable' >&2; exit 1 ;; esac
test -f "$go_executable" && test -x "$go_executable"
exec /usr/bin/env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin HOME=/dev/null LC_ALL=C TZ=UTC \
    GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null \
    GOENV=off GOWORK=off GOTOOLCHAIN=local GO111MODULE=on \
    GOPROXY=off GOSUMDB=off GOVCS='*:off' GOAUTH=off CGO_ENABLED=0 \
    GOAMD64=v1 GOARM64=v8.0 \
    /bin/sh -s -- "$0" "$go_executable" "$@" <<'HHPT_BUILD'
set -eu
umask 077
script_path=$1
go_executable=$2
shift 2
test "$#" -le 2 || { echo 'unexpected Go build arguments' >&2; exit 2; }
architecture=${1:-x86_64}
output_root=${2:-build/bin}
repository_root=$(CDPATH= cd -- "$(dirname -- "$script_path")/../.." && pwd)
case $output_root in /*) ;; *) output_root=$repository_root/$output_root ;; esac
cd "$repository_root"
/usr/bin/python3 -I -B "$repository_root/scripts/security/scan_source.py"
go_pin=$(cat "$repository_root/.go-version")
go_pin=${go_pin%$(printf '\r')}
case $go_pin in ''|*[!0-9.]*) echo 'invalid Go version policy' >&2; exit 1 ;; esac
printf '%s\n' "$go_pin" | grep -Eq '^[1-9][0-9]*\.[0-9]+\.[0-9]+$'
actual_go_version=$("$go_executable" env GOVERSION)
test "$actual_go_version" = "go$go_pin" || {
    echo 'installed Go toolchain differs from .go-version; downloads are disabled' >&2; exit 1;
}
version=$(cat "$repository_root/VERSION")
version=${version%$(printf '\r')}
case $version in ''|*[!0-9A-Za-z.+-]*) echo 'invalid product version' >&2; exit 1 ;; esac
target_os=linux
output_architecture=$architecture
printf '%s\n' "$version" | grep -Eq '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$'

case $architecture in
    x86)
        goarch=386
        export GO386=sse2
        ;;
    legacy-x86)
        goarch=386
        export GO386=softfloat
        ;;
    x86_64)
        goarch=amd64
        export GO386=sse2
        ;;
    aarch64)
        goarch=arm64
        export GO386=sse2
        export GOARM64=v8.0
        ;;
    windows-x86_64)
        target_os=windows
        output_architecture=x86_64
        goarch=amd64
        export GO386=sse2
        ;;
    *)
        echo unsupported architecture: "$architecture" >&2
        exit 2
        ;;
esac

# Every invocation starts with empty user configuration and build/module caches.
# Keep the work directory for diagnosis; never reuse or clear a caller's cache.
mkdir -p "$repository_root/build"
build_environment=$(mktemp -d "$repository_root/build/go-environment.XXXXXX")
HOME=$build_environment/home
TMPDIR=$build_environment/tmp
XDG_CONFIG_HOME=$build_environment/config
XDG_CACHE_HOME=$build_environment/cache
GOCACHE=$build_environment/go-cache
GOMODCACHE=$build_environment/go-mod-cache
GOPATH=$build_environment/go-path
mkdir -p "$HOME" "$TMPDIR" "$XDG_CONFIG_HOME" "$XDG_CACHE_HOME" "$GOCACHE" "$GOMODCACHE" "$GOPATH"
export HOME TMPDIR XDG_CONFIG_HOME XDG_CACHE_HOME GOCACHE GOMODCACHE GOPATH

artifact_directory=$output_root/${target_os}_$output_architecture
artifact=$artifact_directory/hhpt
if test "$target_os" = windows; then
    artifact=$artifact.exe
fi
"$go_executable" test -mod=readonly ./...
mkdir -p "$artifact_directory"
GOOS=$target_os GOARCH=$goarch "$go_executable" build \
    -mod=readonly \
    -trimpath \
    -buildvcs=false \
    -ldflags "-buildid= -X github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product.Version=$version" \
    -o "$artifact" \
    ./cmd/hhpt
(cd "$artifact_directory" && sha256sum "$(basename "$artifact")") >"$artifact.sha256"
HHPT_BUILD
