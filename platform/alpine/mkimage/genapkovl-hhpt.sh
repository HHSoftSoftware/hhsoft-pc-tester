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
ui_console=/dev/tty0
if ! test -c "\$ui_console"; then
	ui_console=/dev/console
fi
printf '\n=== HHSoft PC Tester ===\nOffline hardware inspection and qualification\n\n' > /dev/console
printf 'HHPT_UI_WAIT timeout=15s architecture=$target_architecture\n' > /dev/console
if /usr/bin/hhpt ui -auto-start 15s -deployment-key-media <"\$ui_console" >"\$ui_console" 2>&1; then
	ui_status=0
else
	ui_status=\$?
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
		if awk '\$1 ~ "^/dev/" && \$4 ~ "(^|,)rw(,|$)" { found=1 } END { exit found ? 0 : 1 }' /proc/mounts; then
			printf 'HHPT_SAFETY_FAIL writable-block-mount\n' > /dev/console
			exit 1
		fi
		if /usr/bin/hhpt plan -fingerprint-key /run/hhpt/fingerprint.key -key-epoch "\$key_epoch" \$fixture_flag -profile quick -architecture $target_architecture -output /run/hhpt/plan.json; then
			chmod 0400 /run/hhpt/plan.json || { printf 'HHPT_FAILED freeze-test-plan\n' > /dev/console; exit 1; }
			plan_id=\$(sed -n 's/^[[:space:]]*"id": "\(sha256:[^"]*\)",*$/\1/p' /run/hhpt/plan.json | head -n 1)
			scheduled_count=\$(grep -c '"status": "scheduled"' /run/hhpt/plan.json || true)
			unsupported_count=\$(grep -c '"status": "unsupported"' /run/hhpt/plan.json || true)
			printf 'HHPT_PLAN id=%s profile=quick scheduled=%s unsupported=%s output=/run/hhpt/plan.json\n' "\$plan_id" "\$scheduled_count" "\$unsupported_count" > /dev/console
		else
			printf 'HHPT_FAILED test-plan\n' > /dev/console
			exit 1
		fi
		cpu_plan_status=\$(awk '/"id": "cpu.quick"/ { wanted=1 } wanted && /"status":/ { gsub(/[,\"]/, ""); sub(/^[[:space:]]*status:[[:space:]]*/, ""); print; exit }' /run/hhpt/plan.json)
		memory_plan_status=\$(awk '/"id": "memory.quick"/ { wanted=1 } wanted && /"status":/ { gsub(/[,\"]/, ""); sub(/^[[:space:]]*status:[[:space:]]*/, ""); print; exit }' /run/hhpt/plan.json)
		if test "\$cpu_plan_status" != scheduled; then
			cpu_status=\$cpu_plan_status
		elif /usr/bin/timeout -s KILL 25 /usr/bin/hhpt cpu -duration 3s -timeout 20s -thermal-limit-c 90 -output /run/hhpt/cpu.json; then
			cpu_status=\$(sed -n 's/^[[:space:]]*"status": "\([^"]*\)",*$/\1/p' /run/hhpt/cpu.json | head -n 1)
			test -n "\$cpu_status" || cpu_status=error
		else
			cpu_status=error
		fi
		printf 'HHPT_CPU status=%s output=/run/hhpt/cpu.json\n' "\$cpu_status" > /dev/console
		if test "\$cpu_plan_status" = scheduled && test "\$cpu_status" != pass; then
			memory_status=aborted
		elif test "\$memory_plan_status" != scheduled; then
			memory_status=\$memory_plan_status
		elif /usr/bin/timeout -s KILL 35 /usr/bin/hhpt memory -passes 2 -timeout 30s -thermal-limit-c 90 -output /run/hhpt/memory.json; then
			memory_status=\$(sed -n 's/^[[:space:]]*"status": "\([^"]*\)",*$/\1/p' /run/hhpt/memory.json | head -n 1)
			test -n "\$memory_status" || memory_status=error
		else
			memory_status=error
		fi
		printf 'HHPT_MEMORY status=%s output=/run/hhpt/memory.json\n' "\$memory_status" > /dev/console
		audio_plan_status=\$(awk '/"id": "audio.readiness.quick"/ { wanted=1 } wanted && /"status":/ { gsub(/[,\"]/, ""); sub(/^[[:space:]]*status:[[:space:]]*/, ""); print; exit }' /run/hhpt/plan.json)
		test -n "\$audio_plan_status" || audio_plan_status=error
		if test "\$audio_plan_status" != scheduled; then
			audio_status=\$audio_plan_status
		elif /usr/bin/timeout -s KILL 5 /usr/bin/hhpt audio -output /run/hhpt/audio.json; then
			audio_status=\$(sed -n 's/^[[:space:]]*"status": "\([^"]*\)",*$/\1/p' /run/hhpt/audio.json | head -n 1)
			test -n "\$audio_status" || audio_status=error
		else
			audio_status=error
		fi
		printf 'HHPT_AUDIO_READINESS status=%s output=/run/hhpt/audio.json\n' "\$audio_status" > /dev/console
		input_plan_status=\$(awk '/"id": "input.readiness.quick"/ { wanted=1 } wanted && /"status":/ { gsub(/[,\"]/, ""); sub(/^[[:space:]]*status:[[:space:]]*/, ""); print; exit }' /run/hhpt/plan.json)
		test -n "\$input_plan_status" || input_plan_status=error
		if test "\$input_plan_status" != scheduled; then
			input_status=\$input_plan_status
		elif /usr/bin/timeout -s KILL 5 /usr/bin/hhpt input -duration 0 -output /run/hhpt/input.json; then
			input_status=\$(sed -n 's/^[[:space:]]*"status": "\([^"]*\)",*$/\1/p' /run/hhpt/input.json | head -n 1)
			test -n "\$input_status" || input_status=error
		else
			input_status=error
		fi
		printf 'HHPT_INPUT_READINESS status=%s output=/run/hhpt/input.json\n' "\$input_status" > /dev/console
		network_plan_status=\$(awk '/"id": "network.readiness.quick"/ { wanted=1 } wanted && /"status":/ { gsub(/[,\"]/, ""); sub(/^[[:space:]]*status:[[:space:]]*/, ""); print; exit }' /run/hhpt/plan.json)
		test -n "\$network_plan_status" || network_plan_status=error
		if test "\$network_plan_status" != scheduled; then
			network_status=\$network_plan_status
		elif /usr/bin/timeout -s KILL 5 /usr/bin/hhpt network -output /run/hhpt/network.json; then
			network_status=\$(sed -n 's/^[[:space:]]*"status": "\([^"]*\)",*$/\1/p' /run/hhpt/network.json | head -n 1)
			test -n "\$network_status" || network_status=error
		else
			network_status=error
		fi
		printf 'HHPT_NETWORK_READINESS status=%s output=/run/hhpt/network.json\n' "\$network_status" > /dev/console
		storage_plan_status=\$(awk '/"id": "storage.readiness.quick"/ { wanted=1 } wanted && /"status":/ { gsub(/[,\"]/, ""); sub(/^[[:space:]]*status:[[:space:]]*/, ""); print; exit }' /run/hhpt/plan.json)
		test -n "\$storage_plan_status" || storage_plan_status=error
		if test "\$storage_plan_status" != scheduled; then
			storage_status=\$storage_plan_status
		elif /usr/bin/timeout -s KILL 5 /usr/bin/hhpt storage -output /run/hhpt/storage.json; then
			storage_status=\$(sed -n 's/^[[:space:]]*"status": "\([^"]*\)",*$/\1/p' /run/hhpt/storage.json | head -n 1)
			test -n "\$storage_status" || storage_status=error
		else
			storage_status=error
		fi
		printf 'HHPT_STORAGE_READINESS status=%s output=/run/hhpt/storage.json\n' "\$storage_status" > /dev/console
		usb_plan_status=\$(awk '/"id": "usb.readiness.quick"/ { wanted=1 } wanted && /"status":/ { gsub(/[,\"]/, ""); sub(/^[[:space:]]*status:[[:space:]]*/, ""); print; exit }' /run/hhpt/plan.json)
		test -n "\$usb_plan_status" || usb_plan_status=error
		if test "\$usb_plan_status" != scheduled; then
			usb_status=\$usb_plan_status
		elif /usr/bin/timeout -s KILL 5 /usr/bin/hhpt usb -output /run/hhpt/usb.json; then
			usb_status=\$(sed -n 's/^[[:space:]]*"status": "\([^"]*\)",*$/\1/p' /run/hhpt/usb.json | head -n 1)
			test -n "\$usb_status" || usb_status=error
		else
			usb_status=error
		fi
		printf 'HHPT_USB_READINESS status=%s output=/run/hhpt/usb.json\n' "\$usb_status" > /dev/console
		graphics_plan_status=\$(awk '/"id": "graphics.quick"/ { wanted=1 } wanted && /"status":/ { gsub(/[,\"]/, ""); sub(/^[[:space:]]*status:[[:space:]]*/, ""); print; exit }' /run/hhpt/plan.json)
		test -n "\$graphics_plan_status" || graphics_plan_status=error
		active_stop=0
		if test "\$cpu_plan_status" = scheduled && test "\$cpu_status" != pass; then active_stop=1; fi
		if test "\$memory_plan_status" = scheduled && test "\$memory_status" != pass; then active_stop=1; fi
		if test "\$active_stop" -eq 1; then
			graphics_status=aborted
			printf 'HHPT_GRAPHICS status=aborted detail=earlier-safety-or-correctness-stop\n' > /dev/console
		elif test "\$graphics_plan_status" != scheduled; then
			graphics_status=\$graphics_plan_status
			printf 'HHPT_GRAPHICS status=%s detail=not-scheduled\n' "\$graphics_status" > /dev/console
		elif ! test -x /usr/bin/eglinfo || ! test -x /usr/bin/kmscube; then
			graphics_status=error
			printf 'HHPT_GRAPHICS status=error detail=missing-tools\n' > /dev/console
		elif /usr/bin/hhpt graphics -pattern-frames 120 -output /run/hhpt/graphics.json; then
			graphics_status=\$(sed -n 's/^[[:space:]]*"status": "\([^"]*\)",*$/\1/p' /run/hhpt/graphics.json | head -n 1)
			test -n "\$graphics_status" || graphics_status=error
			printf 'HHPT_GRAPHICS status=%s output=/run/hhpt/graphics.json\n' "\$graphics_status" > /dev/console
		else
			graphics_status=error
			printf 'HHPT_GRAPHICS status=error output=/run/hhpt/graphics.json\n' > /dev/console
		fi
		display_plan_status=\$(awk '/"id": "display.operator.quick"/ { wanted=1 } wanted && /"status":/ { gsub(/[,"]/, ""); sub(/^[[:space:]]*status:[[:space:]]*/, ""); print; exit }' /run/hhpt/plan.json)
		test -n "\$display_plan_status" || display_plan_status=error
		if test "\$active_stop" -eq 1; then
			display_status=aborted
		elif test "\$display_plan_status" != scheduled; then
			display_status=\$display_plan_status
		elif /usr/bin/hhpt display -pattern-duration 5s -finding-timeout 15s -output /run/hhpt/display.json <"\$ui_console" >"\$ui_console" 2>&1; then
			display_status=\$(sed -n 's/^[[:space:]]*"status": "\([^"]*\)",*$/\1/p' /run/hhpt/display.json | head -n 1)
			test -n "\$display_status" || display_status=error
		else
			display_status=error
		fi
		printf 'HHPT_DISPLAY status=%s output=/run/hhpt/display.json\n' "\$display_status" > /dev/console
		if /usr/bin/hhpt finalize -fingerprint-key /run/hhpt/fingerprint.key -key-epoch "\$key_epoch" \$fixture_flag -plan /run/hhpt/plan.json -evidence-dir /run/hhpt -outcome audio.readiness.quick="\$audio_status" -outcome cpu.quick="\$cpu_status" -outcome memory.quick="\$memory_status" -outcome input.readiness.quick="\$input_status" -outcome network.readiness.quick="\$network_status" -outcome storage.readiness.quick="\$storage_status" -outcome usb.readiness.quick="\$usb_status" -outcome graphics.quick="\$graphics_status" -outcome display.operator.quick="\$display_status" -output /run/hhpt/session.hhpcr > /run/hhpt/finalize.log 2>&1; then
			cat /run/hhpt/finalize.log > /dev/console
			integrity_status=\$(sed -n 's/^HHPT_RUN_INTEGRITY status=\([^ ]*\).*$/\1/p' /run/hhpt/finalize.log)
			final_component_count=\$(sed -n 's/^HHPT_RUN_INTEGRITY .* final_components=\([0-9]*\).*$/\1/p' /run/hhpt/finalize.log)
			test "\$integrity_status" = pass || test "\$integrity_status" = fail || { printf 'HHPT_FAILED run-integrity-metadata\n' > /dev/console; exit 1; }
			test -n "\$final_component_count" || { printf 'HHPT_FAILED run-integrity-metadata\n' > /dev/console; exit 1; }
			printf 'HHPT_READY report=/run/hhpt/session.hhpcr plan=%s components=%s integrity=%s audio=%s cpu=%s memory=%s input=%s network=%s storage=%s usb=%s graphics=%s display=%s key_epoch=%s writable_block_mounts=0 signature=self-issued\n' "\$plan_id" "\$final_component_count" "\$integrity_status" "\$audio_status" "\$cpu_status" "\$memory_status" "\$input_status" "\$network_status" "\$storage_status" "\$usb_status" "\$graphics_status" "\$display_status" "\$key_epoch" > /dev/console
			/usr/bin/hhpt save -fingerprint-key /run/hhpt/fingerprint.key /run/hhpt/session.hhpcr <"\$ui_console" >"\$ui_console" 2>&1 || printf 'HHPT_SAVE_FAILED report-retained-in-ram\n' > /dev/console
		else
			printf 'HHPT_FAILED report-finalization\n' > /dev/console
			exit 1
		fi
		while :; do sleep 3600; done
else
	printf 'HHPT_FAILED inventory\n' > /dev/console
	exit 1
fi
EOF

install -D -m 0755 -o root -g root "$binary" "$temporary_directory/usr/bin/hhpt"

for service_and_level in \
	devfs:sysinit dmesg:sysinit mdev:sysinit hwdrivers:sysinit modloop:sysinit \
	hwclock:boot modules:boot sysctl:boot hostname:boot bootmisc:boot syslog:boot \
	local:default mount-ro:shutdown killprocs:shutdown savecache:shutdown; do
	rc_add "${service_and_level%:*}" "${service_and_level#*:}"
done

tar --owner=0 --group=0 --numeric-owner -c -C "$temporary_directory" etc usr | gzip -9n > "$hostname.apkovl.tar.gz"
