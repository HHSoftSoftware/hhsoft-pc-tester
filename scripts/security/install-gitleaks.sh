#!/bin/sh
set -eu

destination=${1:?usage: install-gitleaks.sh DESTINATION}
repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
lock_file=$repository_root/scripts/security/gitleaks.lock

lock_value() {
    sed -n "s/^$1=//p" "$lock_file"
}

version=$(lock_value VERSION)
source_url=$(lock_value SOURCE)
archive_hash=$(lock_value SHA256)
case $version in
    ''|*[!0-9.]*) echo invalid Gitleaks version lock >&2; exit 1 ;;
esac
printf '%s\n' "$archive_hash" | grep -Eq '^[0-9a-f]{64}$'
case $source_url in
    https://github.com/gitleaks/gitleaks/releases/download/v"$version"/*) ;;
    *) echo invalid Gitleaks source lock >&2; exit 1 ;;
esac

test ! -e "$destination"
mkdir -m 0700 "$destination"
archive=$destination/gitleaks.tar.gz
curl --fail --location --proto '=https' --tlsv1.2 --retry 3 --output "$archive" "$source_url"
printf '%s  %s\n' "$archive_hash" "$archive" | sha256sum -c -
tar -xzf "$archive" -C "$destination" gitleaks
chmod 0755 "$destination/gitleaks"
"$destination/gitleaks" version | grep -F "$version"
