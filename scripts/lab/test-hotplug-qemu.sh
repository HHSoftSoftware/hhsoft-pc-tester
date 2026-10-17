#!/bin/sh
set -eu

iso=${1:?usage: test-hotplug-qemu.sh IMAGE.iso NEW-WORK-DIRECTORY}
work=${2:?usage: test-hotplug-qemu.sh IMAGE.iso NEW-WORK-DIRECTORY}
for tool in date qemu-system-x86_64 socat timeout truncate; do
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
		kill "$qemu_pid" || true
		stop_seconds=5
		while kill -0 "$qemu_pid" 2>/dev/null && test "$stop_seconds" -gt 0; do
			sleep 1
			stop_seconds=$((stop_seconds - 1))
		done
		kill -KILL "$qemu_pid" 2>/dev/null || true
	fi
	if test -n "$qemu_pid"; then
		wait "$qemu_pid" 2>/dev/null || true
		qemu_pid=
	fi
}
trap stop_qemu EXIT
trap 'exit 1' INT TERM

qemu_running() {
	guest_now=$(date +%s) || return 1
	case $guest_now in ''|*[!0-9]*) return 1 ;; esac
	kill -0 "$qemu_pid" 2>/dev/null && test "$guest_now" -lt "$guest_deadline"
}

wait_for_monitor() {
	monitor_seconds=10
	while test "$monitor_seconds" -gt 0; do
		qemu_running || return 1
		test ! -S "$monitor_socket" || return 0
		sleep 1
		monitor_seconds=$((monitor_seconds - 1))
	done
	return 1
}

wait_for_exit() {
	exit_seconds=5
	while kill -0 "$qemu_pid" 2>/dev/null; do
		test "$exit_seconds" -gt 0 || return 1
		sleep 1
		exit_seconds=$((exit_seconds - 1))
	done
	wait "$qemu_pid"
	qemu_pid=
}

hmp() {
	qemu_running
	printf '%s\n' "$1" | timeout -k 1 4 socat -t 3 - UNIX-CONNECT:"$monitor_socket" >/dev/null
}

wait_for_log() {
	pattern=$1
	seconds=$2
	while test "$seconds" -gt 0; do
		qemu_running || return 1
		if test -f "$serial_log" && grep -F "$pattern" "$serial_log" >/dev/null; then
			return 0
		fi
		sleep 1
		seconds=$((seconds - 1))
	done
	return 1
}

guest_start=$(date +%s)
case $guest_start in ''|*[!0-9]*) echo invalid clock >&2; exit 1 ;; esac
guest_deadline=$((guest_start + 150))
qemu-system-x86_64 \
	-no-user-config -m 1024 -smp 2 -uuid 22222222-2222-4222-8222-222222222222 -cdrom "$iso" -boot once=d -no-reboot -nic none \
	-device qemu-xhci -drive if=none,file="$work/hotplug.raw",format=raw,readonly=on,id=hotplugdrive \
	-serial file:"$serial_log" -monitor unix:"$monitor_socket",server=on,wait=off -display none >"$work/qemu.log" 2>&1 &
qemu_pid=$!
wait_for_monitor
wait_for_log 'HHPT_PLAN id=' 100
hmp 'device_add usb-storage,drive=hotplugdrive,id=hotplugusb,serial=HHPTLABHOTPLUG,removable=on'
wait_for_log 'HHPT_RUN_INTEGRITY status=fail' 100
wait_for_log 'HHPT_READY report=/run/hhpt/session.hhpcr' 20
hmp 'quit'
wait_for_exit

integrity_marker=$(tr -d '\r' < "$serial_log" | grep -E '^HHPT_RUN_INTEGRITY status=fail .*added=[1-9][0-9]*' | head -n 1)
planned_components=$(printf '%s\n' "$integrity_marker" | sed -n 's/^.* planned_components=\([0-9]*\).*$/\1/p')
final_components=$(printf '%s\n' "$integrity_marker" | sed -n 's/^.* final_components=\([0-9]*\).*$/\1/p')
added_components=$(printf '%s\n' "$integrity_marker" | sed -n 's/^.* added=\([0-9]*\).*$/\1/p')
removed_components=$(printf '%s\n' "$integrity_marker" | sed -n 's/^.* removed=\([0-9]*\).*$/\1/p')
test "$final_components" -eq $((planned_components + added_components - removed_components))
printf '%s\n' "$integrity_marker"
grep -E "HHPT_READY .*components=$final_components integrity=fail .*writable_block_mounts=0 .*signature=self-issued" "$serial_log"
! grep -E 'HHPT_(FAILED|SAFETY_FAIL)' "$serial_log"
