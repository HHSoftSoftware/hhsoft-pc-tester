#!/bin/sh
set -eu

iso=${1:?usage: test-hotplug-qemu.sh IMAGE.iso NEW-WORK-DIRECTORY}
work=${2:?usage: test-hotplug-qemu.sh IMAGE.iso NEW-WORK-DIRECTORY}
for tool in qemu-system-x86_64 socat timeout truncate; do
	command -v "$tool" >/dev/null
done
test -f "$iso"
test ! -e "$work"
mkdir -m 0700 "$work"

truncate -s 32M "$work/hotplug.raw"
serial_log=$work/serial.log
monitor_socket=$work/monitor.sock
qemu_pid=
stop_qemu() {
	if test -n "$qemu_pid" && kill -0 "$qemu_pid" 2>/dev/null; then
		printf 'quit\n' | socat -t 3 - UNIX-CONNECT:"$monitor_socket" >/dev/null 2>&1 || kill "$qemu_pid" || true
		wait "$qemu_pid" 2>/dev/null || true
	fi
}
trap stop_qemu EXIT INT TERM

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

timeout 150 qemu-system-x86_64 \
	-m 1024 -smp 2 -uuid 22222222-2222-4222-8222-222222222222 -cdrom "$iso" -boot once=d -no-reboot \
	-device qemu-xhci -drive if=none,file="$work/hotplug.raw",format=raw,readonly=on,id=hotplugdrive \
	-serial file:"$serial_log" -monitor unix:"$monitor_socket",server=on,wait=off -display none >/dev/null 2>&1 &
qemu_pid=$!
while ! test -S "$monitor_socket"; do sleep 1; done
wait_for_log 'HHPT_PLAN id=' 100
printf 'device_add usb-storage,drive=hotplugdrive,id=hotplugusb,serial=HHPTLABHOTPLUG,removable=on\n' | socat -t 3 - UNIX-CONNECT:"$monitor_socket" >/dev/null
wait_for_log 'HHPT_RUN_INTEGRITY status=fail' 100
wait_for_log 'HHPT_READY report=/run/hhpt/session.hhpcr' 20
stop_qemu
qemu_pid=

integrity_marker=$(tr -d '\r' < "$serial_log" | grep -E '^HHPT_RUN_INTEGRITY status=fail .*added=[1-9][0-9]*' | head -n 1)
planned_components=$(printf '%s\n' "$integrity_marker" | sed -n 's/^.* planned_components=\([0-9]*\).*$/\1/p')
final_components=$(printf '%s\n' "$integrity_marker" | sed -n 's/^.* final_components=\([0-9]*\).*$/\1/p')
added_components=$(printf '%s\n' "$integrity_marker" | sed -n 's/^.* added=\([0-9]*\).*$/\1/p')
removed_components=$(printf '%s\n' "$integrity_marker" | sed -n 's/^.* removed=\([0-9]*\).*$/\1/p')
test "$final_components" -eq $((planned_components + added_components - removed_components))
printf '%s\n' "$integrity_marker"
grep -E "HHPT_READY .*components=$final_components integrity=fail .*writable_block_mounts=0 .*signature=self-issued" "$serial_log"
! grep -E 'HHPT_(FAILED|SAFETY_FAIL)' "$serial_log"
