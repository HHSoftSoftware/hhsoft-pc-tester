#!/bin/sh
set -eu

architecture=${1:-x86_64}
output_root=${2:-build/bin}
repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
sh "$repository_root/scripts/security/scan-source.sh"
version=$(tr -d '\r\n' <VERSION)
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
        ;;
    aarch64)
        goarch=arm64
        export GOARM64=v8.0
        ;;
    windows-x86_64)
        target_os=windows
        output_architecture=x86_64
        goarch=amd64
        ;;
    *)
        echo unsupported architecture: "$architecture" >&2
        exit 2
        ;;
esac

artifact_directory=$output_root/${target_os}_$output_architecture
artifact=$artifact_directory/hhpt
if test "$target_os" = windows; then
    artifact=$artifact.exe
fi
mkdir -p "$artifact_directory"
go test -mod=readonly ./...
CGO_ENABLED=0 GOOS=$target_os GOARCH=$goarch go build \
    -mod=readonly \
    -trimpath \
    -buildvcs=false \
    -ldflags "-buildid= -X github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product.Version=$version" \
    -o "$artifact" \
    ./cmd/hhpt
sha256sum "$artifact" >"$artifact.sha256"
