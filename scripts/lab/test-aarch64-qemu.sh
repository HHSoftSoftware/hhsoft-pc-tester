#!/bin/sh
set -eu

image=${1:?usage: test-aarch64-qemu.sh IMAGE.iso [NEW-WORK-DIRECTORY]}
firmware_code=${HHPT_AARCH64_FIRMWARE_CODE:-/usr/share/qemu/edk2-aarch64-code.fd}
firmware_vars=${HHPT_AARCH64_FIRMWARE_VARS:-/usr/share/qemu/edk2-arm-vars.fd}
timeout_seconds=${HHPT_QEMU_TIMEOUT_SECONDS:-180}
memory_mebibytes=${HHPT_QEMU_MEMORY_MIB:-1024}

test -f "$image"
test ! -L "$image"
test -f "$firmware_code"
test -f "$firmware_vars"
case $timeout_seconds in
	*[!0-9]*|'') echo invalid timeout >&2; exit 2 ;;
esac
test "$timeout_seconds" -ge 10 && test "$timeout_seconds" -le 600
case $memory_mebibytes in
	*[!0-9]*|'') echo invalid memory size >&2; exit 2 ;;
esac
test "$memory_mebibytes" -ge 768 && test "$memory_mebibytes" -le 8192

if [ "${2:-}" ]; then
	work_directory=$2
	case $work_directory in
		/dev/*) echo work directory cannot be a device path >&2; exit 2 ;;
	esac
	test ! -e "$work_directory"
	mkdir -m 0700 "$work_directory"
else
	work_directory=$(mktemp -d -t hhpt-aarch64-qemu.XXXXXX)
fi

for path in "$image" "$firmware_code" "$firmware_vars" "$work_directory"; do
	case $path in
		*,*) echo paths containing commas are unsupported by this QEMU harness >&2; exit 2 ;;
	esac
done

qemu-img create -q -f qcow2 "$work_directory/scratch.qcow2" 4G
cp "$firmware_vars" "$work_directory/vars.fd"

qemu-system-aarch64 \
	-machine virt,accel=tcg,gic-version=max -cpu max -smp 2 -m "$memory_mebibytes" \
	-drive if=pflash,format=raw,unit=0,readonly=on,file="$firmware_code" \
	-drive if=pflash,format=raw,unit=1,file="$work_directory/vars.fd" \
	-device virtio-gpu-pci -device qemu-xhci -device usb-kbd -device usb-tablet \
	-device virtio-scsi-pci,id=scsi0 \
	-drive if=none,file="$image",format=raw,readonly=on,media=cdrom,id=cdrom \
	-device scsi-cd,drive=cdrom,bus=scsi0.0 \
	-drive if=none,file="$work_directory/scratch.qcow2",format=qcow2,id=disk0 \
	-device virtio-blk-pci,drive=disk0 -nic none -boot order=d \
	-serial file:"$work_directory/serial.log" -monitor none -display none -no-reboot &
qemu_pid=$!
stop_qemu() {
	kill "$qemu_pid" 2>/dev/null || true
	wait "$qemu_pid" 2>/dev/null || true
}
trap stop_qemu EXIT INT TERM

attempt=0
while [ "$attempt" -lt "$timeout_seconds" ]; do
	if grep -q 'HHPT_READY' "$work_directory/serial.log" 2>/dev/null; then
		if grep -Eq 'HHPT_(FAILED|SAFETY_FAIL)' "$work_directory/serial.log"; then
			tail -n 100 "$work_directory/serial.log" >&2
			exit 1
		fi
		grep -Eq 'HHPT_UI_WAIT .*architecture=aarch64' "$work_directory/serial.log"
		grep -Eq 'HHPT_PLAN id=sha256:.*profile=quick' "$work_directory/serial.log"
		grep -Eq 'HHPT_CPU status=pass' "$work_directory/serial.log"
		grep -Eq 'HHPT_MEMORY status=pass' "$work_directory/serial.log"
		grep -Eq 'HHPT_AUDIO_READINESS status=(pass|not_present|missing_driver)' "$work_directory/serial.log"
		grep -Eq 'HHPT_INPUT_READINESS status=pass' "$work_directory/serial.log"
		grep -Eq 'HHPT_NETWORK_READINESS status=(pass|not_present|missing_driver)' "$work_directory/serial.log"
		grep -Eq 'HHPT_STORAGE_READINESS status=pass' "$work_directory/serial.log"
		grep -Eq 'HHPT_USB_READINESS status=(pass|not_present|missing_driver)' "$work_directory/serial.log"
		grep -Eq 'HHPT_GRAPHICS status=(pass|skip|inconclusive)' "$work_directory/serial.log"
		grep -Eq 'HHPT_DISPLAY status=(pass|fail|skip|inconclusive)' "$work_directory/serial.log"
		grep -Eq 'HHPT_RUN_INTEGRITY status=pass .*added=0 removed=0 changed=0' "$work_directory/serial.log"
		grep -Eq 'HHPT_READY .*integrity=pass .*signature=self-issued' "$work_directory/serial.log"
		stop_qemu
		grep -E 'HHPT_(UI_WAIT|BOOT|PLAN|CPU|MEMORY|AUDIO|INPUT|NETWORK|STORAGE|USB|GRAPHICS|DISPLAY|RUN_INTEGRITY|READY)' "$work_directory/serial.log"
		exit 0
	fi
	if ! kill -0 "$qemu_pid" 2>/dev/null; then
		wait "$qemu_pid" || true
		tail -n 100 "$work_directory/serial.log" >&2
		exit 1
	fi
	attempt=$((attempt + 1))
	sleep 1
done

tail -n 100 "$work_directory/serial.log" >&2
exit 1
