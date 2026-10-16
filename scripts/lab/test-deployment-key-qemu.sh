#!/bin/sh
set -eu

iso=${1:?usage: test-deployment-key-qemu.sh ISO HHPT_BINARY WORK_DIRECTORY}
hhpt=${2:?usage: test-deployment-key-qemu.sh ISO HHPT_BINARY WORK_DIRECTORY}
work=${3:?usage: test-deployment-key-qemu.sh ISO HHPT_BINARY WORK_DIRECTORY}

for tool in losetup mcopy mkfs.vfat qemu-system-x86_64 sfdisk socat timeout; do
	command -v "$tool" >/dev/null
done
test -f "$iso"
test -x "$hhpt"
test ! -e "$work"
mkdir -p "$work"

key_image=$work/deployment-key.raw
loop_device=
qemu_pid=
cleanup() {
	if test -n "$qemu_pid" && kill -0 "$qemu_pid" 2>/dev/null; then
		kill "$qemu_pid" || true
		wait "$qemu_pid" 2>/dev/null || true
	fi
	if test -n "$loop_device"; then
		losetup -d "$loop_device" || true
	fi
}
trap cleanup EXIT INT TERM

truncate -s 64M "$key_image"
printf 'label: dos\n2048,,c,*\n' | sfdisk --quiet "$key_image"
loop_device=$(losetup --find --show --partscan "$key_image")
mkfs.vfat -F 32 "${loop_device}p1" >/dev/null
"$hhpt" deployment-key init -epoch lab-stable-1 -output "$work/hhpt-deployment-key.v1"
key_metadata=$("$hhpt" deployment-key inspect "$work/hhpt-deployment-key.v1")
mcopy -i "${loop_device}p1" "$work/hhpt-deployment-key.v1" ::hhpt-deployment-key.v1
losetup -d "$loop_device"
loop_device=

wait_for_log() {
	pattern=$1
	seconds=$2
	while test "$seconds" -gt 0; do
		if test -f "$serial_log" && grep -F "$pattern" "$serial_log" >/dev/null; then
			return 0
		fi
		kill -0 "$qemu_pid" 2>/dev/null || return 1
		sleep 1
		seconds=$((seconds - 1))
	done
	return 1
}

hmp() {
	printf '%s\n' "$1" | socat -t 3 - UNIX-CONNECT:"$monitor_socket" >/dev/null
}

key_id=$(printf '%s\n' "$key_metadata" | sed -n 's/^.*key_id=\(hmac-sha256:[0-9a-f]*\).*$/\1/p')
test -n "$key_id"

boot_with_token() {
	label=$1
	serial_log=$work/serial-$label.log
	monitor_socket=$work/monitor-$label.sock
	timeout 150 qemu-system-x86_64 \
		-m 1024 -smp 2 -uuid 11111111-1111-4111-8111-111111111111 -cdrom "$iso" -boot once=d -no-reboot \
		-drive file="$key_image",format=raw,if=none,readonly=on,id=deploymentkey \
		-device qemu-xhci -device usb-storage,drive=deploymentkey,serial=HHPTLABKEY,removable=on \
		-serial file:"$serial_log" -monitor unix:"$monitor_socket",server=on,wait=off -display none >/dev/null 2>&1 &
	qemu_pid=$!
	while ! test -S "$monitor_socket"; do sleep 1; done
	wait_for_log 'HHPT_UI_WAIT timeout=15s architecture=x86_64' 90
	hmp 'sendkey shift'
	sleep 2
	hmp 'sendkey k'
	sleep 1
	hmp 'sendkey ret'
	wait_for_log 'HHPT_KEY_MEDIA_WAIT action=select-read-only-token' 15
	wait_for_log 'discovered=1 eligible=1' 10
	sleep 2
	hmp 'sendkey 1'
	sleep 1
	hmp 'sendkey ret'
	wait_for_log 'HHPT_KEY_MEDIA_SELECTED device=' 10
	sleep 2
	hmp 'sendkey l'
	hmp 'sendkey o'
	hmp 'sendkey a'
	hmp 'sendkey d'
	hmp 'sendkey ret'
	wait_for_log "fingerprint_mode=deployment key_epoch=lab-stable-1 key_id=$key_id" 30
	wait_for_log 'HHPT_READY report=/run/hhpt/session.hhpcr' 100
	hmp 'quit'
	wait "$qemu_pid"
	qemu_pid=
	grep -F "HHPT_BOOT architecture=x86_64 mode=safe-quick fingerprint_mode=deployment key_epoch=lab-stable-1 key_id=$key_id output=tmpfs" "$serial_log"
	grep -E 'HHPT_RUN_INTEGRITY status=pass .*added=0 removed=0 changed=0' "$serial_log"
	grep -E 'HHPT_AUDIO_READINESS status=(pass|not_present|missing_driver)' "$serial_log"
	grep -E 'HHPT_READY .*integrity=pass .*key_epoch=lab-stable-1 .*writable_block_mounts=0 .*signature=self-issued' "$serial_log"
	! grep -F 'HHPT_FAILED' "$serial_log"
	system_id=$(tr -d '\r' < "$serial_log" | sed -n 's/^HHPT_ID system_fingerprint=\(hmac-sha256:[0-9a-f]*\)$/\1/p' | head -n 1)
	test -n "$system_id"
	printf '%s\n' "$system_id" > "$work/system-$label.id"
}

boot_with_token first
boot_with_token second
test "$(cat "$work/system-first.id")" = "$(cat "$work/system-second.id")"
printf 'stable system pseudonym across boots: %s\n' "$(cat "$work/system-first.id")"
