#!/bin/sh
set -eu

iso=${1:?usage: test-audio-qemu.sh IMAGE.iso NEW-WORK-DIRECTORY [QEMU-CPU]}
work=${2:?usage: test-audio-qemu.sh IMAGE.iso NEW-WORK-DIRECTORY [QEMU-CPU]}
emulated_cpu=${3:-qemu64}
for tool in qemu-system-x86_64 socat timeout; do
	command -v "$tool" >/dev/null
done
test -f "$iso"
test ! -e "$work"
mkdir -m 0700 "$work"

serial_log=$work/serial.log
monitor_socket=$work/monitor.sock
qemu_log=$work/qemu.log
qemu_pid=
stop_qemu() {
	if test -n "$qemu_pid" && kill -0 "$qemu_pid" 2>/dev/null; then
		printf 'quit\n' | socat -t 3 - UNIX-CONNECT:"$monitor_socket" >/dev/null 2>&1 || kill "$qemu_pid" || true
		wait "$qemu_pid" 2>/dev/null || true
	fi
}
trap stop_qemu EXIT INT TERM

timeout 150 qemu-system-x86_64 \
	-m 1024 -smp 2 -cpu "$emulated_cpu" -uuid 33333333-3333-4333-8333-333333333333 -cdrom "$iso" -boot once=d -no-reboot -nic none \
	-device intel-hda -device hda-duplex,audiodev=audio0 -audiodev driver=none,id=audio0 \
	-serial file:"$serial_log" -monitor unix:"$monitor_socket",server=on,wait=off -display none >"$qemu_log" 2>&1 &
qemu_pid=$!
monitor_wait=10
while ! test -S "$monitor_socket" && test "$monitor_wait" -gt 0; do
	kill -0 "$qemu_pid" 2>/dev/null
	sleep 1
	monitor_wait=$((monitor_wait - 1))
done
test -S "$monitor_socket"

seconds=140
while test "$seconds" -gt 0; do
	if test -f "$serial_log" && grep -E '^HHPT_READY .* integrity=pass audio=pass .*writable_block_mounts=0 ' "$serial_log" >/dev/null; then
		break
	fi
	kill -0 "$qemu_pid" 2>/dev/null
	sleep 1
	seconds=$((seconds - 1))
done
test "$seconds" -gt 0
stop_qemu
qemu_pid=

grep -E '^HHPT_AUDIO_READINESS status=pass ' "$serial_log"
grep -E '^HHPT_RUN_INTEGRITY status=pass .*added=0 removed=0 changed=0' "$serial_log"
grep -E '^HHPT_READY .* integrity=pass audio=pass .*writable_block_mounts=0 .*signature=self-issued' "$serial_log"
! grep -E 'HHPT_(FAILED|SAFETY_FAIL)' "$serial_log"
