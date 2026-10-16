#!/bin/sh
set -eu
repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
if command -v python3 >/dev/null 2>&1 && python3 -c 'import sys' >/dev/null 2>&1; then
    exec python3 "$repository_root/scripts/security/scan_source.py" "$@"
fi
exec python "$repository_root/scripts/security/scan_source.py" "$@"
