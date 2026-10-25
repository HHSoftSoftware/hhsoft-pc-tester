#!/bin/sh
set -eu

hostname=${1:?hostname required}
binary=${HHPT_BINARY:?HHPT_BINARY required}
target_architecture=${HHPT_TARGET_ARCH:?HHPT_TARGET_ARCH required}
test -x "$binary"

temporary_directory=$(mktemp -d)
cleanup() {
	rm -rf -- "$temporary_directory"
}
trap cleanup EXIT

make_file() {
	owner=$1
	permissions=$2
	filename=$3
	mkdir -p "$(dirname "$filename")"
	cat > "$filename"
	chown "$owner" "$filename"
	chmod "$permissions" "$filename"
}

rc_add() {
	mkdir -p "$temporary_directory/etc/runlevels/$2"
	ln -sf "/etc/init.d/$1" "$temporary_directory/etc/runlevels/$2/$1"
}

make_file root:root 0644 "$temporary_directory/etc/hostname" <<EOF
$hostname
EOF

make_file root:root 0644 "$temporary_directory/etc/network/interfaces" <<'EOF'
auto lo
iface lo inet loopback
EOF

make_file root:root 0644 "$temporary_directory/etc/inittab" <<'EOF'
::sysinit:/sbin/openrc sysinit
::sysinit:/sbin/openrc boot
::wait:/sbin/openrc default
::shutdown:/sbin/openrc shutdown
EOF

make_file root:root 0644 "$temporary_directory/etc/apk/world" <<'EOF'
alpine-base
kbd-bkeymaps
kmscube
libdrm-tests
mesa-dri-gallium
mesa-egl
mesa-gbm
mesa-utils
openssl
pciutils
usbutils
util-linux
EOF

make_file root:root 0755 "$temporary_directory/etc/local.d/hhpt.start" <<EOF
#!/bin/sh
set -eu
umask 077
sed -i 's/^root:[^:]*:/root:*:/' /etc/shadow
mkdir -p /run/hhpt
check_safety() {
	safety_phase=\$1
	if safety_summary=\$(/usr/bin/timeout -s KILL 5 /usr/bin/hhpt safety -output "/run/hhpt/safety-\$safety_phase.json" -summary 2>"/run/hhpt/safety-\$safety_phase.log"); then
		case "\$safety_summary" in
			'writable_block_mounts=0 active_non_loop_interfaces=0') return ;;
		esac
	fi
	printf 'HHPT_SAFETY_FAIL stage=%s evidence=/run/hhpt/safety-%s.json detail=unsafe-or-incomplete-observations\n' "\$safety_phase" "\$safety_phase" > /dev/console
	exit 1
}
ui_console=/dev/tty0
if ! test -c "\$ui_console"; then
	ui_console=/dev/console
fi
printf '\n=== HHSoft PC Tester ===\nOffline hardware inspection and qualification\n\n' > /dev/console
printf 'HHPT_UI_WAIT timeout=15s architecture=$target_architecture\n' > /dev/console
if /usr/bin/hhpt ui -auto-start 15s -deployment-key-media -selection-output /run/hhpt/selection.json <"\$ui_console" >"\$ui_console" 2>&1; then
	ui_status=0
else
	ui_status=\$?
fi
if test "\$ui_status" -eq 0 || test "\$ui_status" -eq 10; then
	if ! test -f /run/hhpt/selection.json || test -L /run/hhpt/selection.json || ! chmod 0400 /run/hhpt/selection.json; then
		printf 'HHPT_FAILED freeze-test-selection\n' > /dev/console
		exit 1
	fi
fi
key_epoch=ephemeral-session
key_mode=ephemeral
key_id=ephemeral-session
fixture_flag=
case \$ui_status in
	0)
		head -c 32 /dev/urandom > /run/hhpt/fingerprint.key
		;;
	10)
		head -c 32 /dev/urandom > /run/hhpt/media-discovery.key
		printf 'HHPT_KEY_MEDIA_WAIT action=select-read-only-token\n' > /dev/console
		if /usr/bin/hhpt deployment-key load-media -discovery-key /run/hhpt/media-discovery.key -audit-output /dev/console <"\$ui_console" >"\$ui_console" 2>&1; then
			rm -f /run/hhpt/media-discovery.key
			key_epoch=\$(sed -n 's/^.*"epoch":"\([^"]*\)".*\$/\1/p' /run/hhpt/deployment-key.json)
			key_id=\$(sed -n 's/^.*"key_id":"\([^"]*\)".*\$/\1/p' /run/hhpt/deployment-key.json)
			if test -z "\$key_epoch" || test -z "\$key_id"; then
				printf 'HHPT_FAILED deployment-key-metadata\n' > /dev/console
				exit 1
			fi
			key_mode=deployment
			fixture_flag=-fixture-components=/run/hhpt/fixture-components
		else
			printf 'HHPT_FAILED deployment-key-activation\n' > /dev/console
			exit 1
		fi
		;;
	*)
		printf 'HHPT_FAILED user-interface\n' > /dev/console
		exit 1
		;;
esac
printf 'HHPT_BOOT architecture=$target_architecture mode=safe-quick fingerprint_mode=%s key_epoch=%s key_id=%s output=tmpfs\n' "\$key_mode" "\$key_epoch" "\$key_id" > /dev/console
if /usr/bin/hhpt inventory -fingerprint-key /run/hhpt/fingerprint.key -key-epoch "\$key_epoch" \$fixture_flag -output /run/hhpt/inventory.json; then
		system_fingerprint=\$(awk '/"system_fingerprint":/ { wanted=1 } wanted && /"value":/ { gsub(/[,\"]/, ""); sub(/^[[:space:]]*value:[[:space:]]*/, ""); print; exit }' /run/hhpt/inventory.json)
		test -n "\$system_fingerprint" || system_fingerprint=unavailable
		printf 'HHPT_ID system_fingerprint=%s\n' "\$system_fingerprint" > /dev/console
		component_count=\$(grep -c '"type":' /run/hhpt/inventory.json || true)
		if test "\$component_count" -eq 0; then
			printf 'HHPT_SAFETY_FAIL empty-inventory\n' > /dev/console
			exit 1
		fi
		check_safety preflight
		if /usr/bin/hhpt plan -fingerprint-key /run/hhpt/fingerprint.key -key-epoch "\$key_epoch" \$fixture_flag -profile quick -architecture $target_architecture -selection-file /run/hhpt/selection.json -output /run/hhpt/plan.json; then
			chmod 0400 /run/hhpt/plan.json || { printf 'HHPT_FAILED freeze-test-plan\n' > /dev/console; exit 1; }
			plan_id=\$(sed -n 's/^[[:space:]]*"id": "\(sha256:[^"]*\)",*$/\1/p' /run/hhpt/plan.json | head -n 1)
			scheduled_count=\$(grep -c '"status": "scheduled"' /run/hhpt/plan.json || true)
			unsupported_count=\$(grep -c '"status": "unsupported"' /run/hhpt/plan.json || true)
			printf 'HHPT_PLAN id=%s profile=quick scheduled=%s unsupported=%s output=/run/hhpt/plan.json\n' "\$plan_id" "\$scheduled_count" "\$unsupported_count" > /dev/console
		else
			printf 'HHPT_FAILED test-plan\n' > /dev/console
			exit 1
		fi
		if /usr/bin/timeout -s KILL 340 /usr/bin/hhpt session -fingerprint-key /run/hhpt/fingerprint.key -key-epoch "\$key_epoch" \$fixture_flag -plan /run/hhpt/plan.json -evidence-dir /run/hhpt -timeout 5m -output /run/hhpt/session.hhpcr <"\$ui_console" >"\$ui_console" 2>&1; then
			check_safety final
			if ! test -s /run/hhpt/session-ready.txt || test -L /run/hhpt/session-ready.txt || ! test -f /run/hhpt/session-ready.txt; then
				printf 'HHPT_FAILED session-ready-metadata\n' > /dev/console
				exit 1
			fi
			cat /run/hhpt/session-ready.txt > /dev/console
			/usr/bin/hhpt save -fingerprint-key /run/hhpt/fingerprint.key -audit-output /dev/console /run/hhpt/session.hhpcr <"\$ui_console" >"\$ui_console" 2>&1 || printf 'HHPT_SAVE_FAILED report-retained-in-ram\n' > /dev/console
		else
			session_status=\$?
			printf 'HHPT_FAILED quick-session exit=%s cleanup=unconfirmed evidence=/run/hhpt\n' "\$session_status" > /dev/console
			exit 1
		fi
		while :; do sleep 3600; done
else
	printf 'HHPT_FAILED inventory\n' > /dev/console
	exit 1
fi
EOF

install -D -m 0755 -o root -g root "$binary" "$temporary_directory/usr/bin/hhpt"
test -f "$temporary_directory/usr/bin/hhpt"
test ! -L "$temporary_directory/usr/bin/hhpt"
installed_binary_checksum=$(sha256sum "$temporary_directory/usr/bin/hhpt")
installed_binary_checksum=${installed_binary_checksum%% *}
test "${#installed_binary_checksum}" -eq 64
case $installed_binary_checksum in *[!0-9a-f]*) echo invalid installed application digest >&2; exit 1 ;; esac
test "$installed_binary_checksum" = "${HHPT_BINARY_SHA256:?bound binary checksum required}"
# The image wrapper supplies a fresh invocation-only destination. noclobber
# rejects an old file or link; this records the bytes installed in the overlay.
(set -C; printf '%s\n' "$installed_binary_checksum" > "${HHPT_OVERLAY_RECEIPT:?overlay receipt required}")

for service_and_level in \
	devfs:sysinit dmesg:sysinit mdev:sysinit hwdrivers:sysinit modloop:sysinit \
	hwclock:boot modules:boot sysctl:boot hostname:boot bootmisc:boot syslog:boot \
	local:default mount-ro:shutdown killprocs:shutdown savecache:shutdown; do
	rc_add "${service_and_level%:*}" "${service_and_level#*:}"
done

tar --owner=0 --group=0 --numeric-owner -c -C "$temporary_directory" etc usr | gzip -9n > "$hostname.apkovl.tar.gz"
