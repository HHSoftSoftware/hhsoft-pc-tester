"""Run the two real shell harnesses using inert tools and private socket fixtures.

No QEMU, block device, mount, filesystem formatter, or external network is used.
The fixture owns all child processes and files under one temporary directory.
"""

import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import unittest


ROOT = Path(__file__).resolve().parent
SCRIPTS = ("test-deployment-key-qemu.sh", "test-hotplug-qemu.sh")

STUB = r'''import json
import os
from pathlib import Path
import signal
import socket
import sys
import time

root = Path(__file__).resolve().parent.parent
configuration = json.loads((root / "configuration.json").read_text())
mode = configuration["mode"]
name = Path(sys.argv[0]).name
arguments = sys.argv[1:]
event = json.dumps({"command": name, "arguments": arguments, "pid": os.getpid()}) + "\n"
descriptor = os.open(root / "events.jsonl", os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
os.write(descriptor, event.encode())
os.close(descriptor)

if name == "date":
    counter = root / "clock-count"
    count = int(counter.read_text()) if counter.exists() else 0
    counter.write_text(str(count + 1))
    print(251 if mode == "deadline" and count else 100)
    sys.exit(7 if mode == "clock-failure" and count else 0)
elif name == "sleep":
    time.sleep(0.025)
elif name == "truncate":
    Path(arguments[-1]).write_bytes(b"fixture disk; no real filesystem\n")
elif name == "losetup":
    if arguments[0] != "-d":
        print(root / "inert-loop-fixture")
elif name == "sfdisk":
    sys.stdin.read()
elif name in ("mkfs.vfat", "mcopy"):
    pass
elif name == "hhpt-fixture":
    if arguments[1] == "init":
        Path(arguments[arguments.index("-output") + 1]).write_text("synthetic fixture only\n")
    else:
        print("key_id=hmac-sha256:" + "a" * 64)
elif name == "socat":
    data = sys.stdin.buffer.read(4096)
    if mode == "hmp-failure":
        sys.exit(9)
    address = next(value.removeprefix("UNIX-CONNECT:") for value in arguments if value.startswith("UNIX-CONNECT:"))
    assert Path(address).parent == root / "work"
    with socket.socket(socket.AF_UNIX) as client:
        client.settimeout(1)
        client.connect(address)
        client.sendall(data)
elif name == "qemu-system-x86_64":
    print("inert QEMU fixture mode=" + mode, flush=True)
    if mode == "early-exit":
        sys.exit(7)
    monitor = arguments[arguments.index("-monitor") + 1].removeprefix("unix:").split(",")[0]
    serial = Path(arguments[arguments.index("-serial") + 1].removeprefix("file:"))
    assert Path(monitor).parent == serial.parent == root / "work"
    if mode in ("missing-monitor", "stuck-quit"):
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
    if mode == "missing-monitor":
        # A regular file must not count as a ready Unix monitor socket.
        Path(monitor).write_text("not a socket\n")
        while True:
            time.sleep(0.05)
    deployment = "deploymentkey" in " ".join(arguments)
    with socket.socket(socket.AF_UNIX) as server:
        server.bind(monitor)
        server.listen(4)
        if mode != "missing-log":
            if deployment:
                serial.write_text("HHPT_UI_WAIT timeout=15s architecture=x86_64\n"
                    "HHPT_KEY_MEDIA_WAIT action=select-read-only-token discovered=1 eligible=1\n"
                    "HHPT_KEY_MEDIA_SELECTED device=/dev/fixture\n"
                    "HHPT_BOOT architecture=x86_64 mode=safe-quick fingerprint_mode=deployment key_epoch=lab-stable-1 key_id=hmac-sha256:" + "a" * 64 + " output=tmpfs\n"
                    "HHPT_RUN_INTEGRITY status=pass final_components=2 added=0 removed=0 changed=0\n"
                    "HHPT_AUDIO_READINESS status=not_present\n"
                    "HHPT_READY report=/run/hhpt/session.hhpcr integrity=pass key_epoch=lab-stable-1 writable_block_mounts=0 signature=self-issued\n"
                    "HHPT_ID system_fingerprint=hmac-sha256:" + "b" * 64 + "\n")
            else:
                serial.write_text("HHPT_PLAN id=sha256:" + "c" * 64 + "\n")
        while True:
            connection, _ = server.accept()
            with connection:
                command = connection.recv(4096).decode().strip()
            if command.startswith("device_add "):
                with serial.open("a") as output:
                    output.write("HHPT_RUN_INTEGRITY status=fail planned_components=2 final_components=3 added=1 removed=0 changed=0\n"
                                 "HHPT_READY report=/run/hhpt/session.hhpcr components=3 integrity=fail writable_block_mounts=0 signature=self-issued\n")
            if command == "quit" and mode != "stuck-quit":
                break
else:
    raise AssertionError("unexpected fixture command")
'''


class ShellHarnessSyntaxTests(unittest.TestCase):
    def test_shell_syntax_when_a_shell_is_available(self):
        shell = shutil.which("sh")
        if shell is None:
            self.skipTest("POSIX shell unavailable")
        for name in SCRIPTS:
            with self.subTest(script=name):
                subprocess.run([shell, "-n", str(ROOT / name)], check=True, timeout=10)


@unittest.skipUnless(sys.platform.startswith("linux"), "native Linux shell/socket fixtures")
class QemuShellHarnessTests(unittest.TestCase):
    def run_fixture(self, script, mode):
        temporary = tempfile.TemporaryDirectory(prefix="hhpt-shell-")
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        tools = root / "tools"
        tools.mkdir()
        (root / "configuration.json").write_text(json.dumps({"mode": mode}))
        names = ("date", "sleep", "truncate", "losetup", "sfdisk", "mkfs.vfat", "mcopy",
                 "hhpt-fixture", "socat", "qemu-system-x86_64")
        for name in names:
            path = tools / name
            path.write_text("#!" + sys.executable + "\n" + STUB)
            path.chmod(0o700)
        iso = root / "fixture.iso"
        iso.write_bytes(b"not an ISO; never executed\n")
        work = root / "work"
        arguments = ["/bin/sh", str(ROOT / script), str(iso)]
        if script == SCRIPTS[0]:
            arguments.append(str(tools / "hhpt-fixture"))
        arguments.append(str(work))
        environment = {"PATH": str(tools) + ":/usr/bin:/bin", "HOME": str(root), "LC_ALL": "C"}
        started = time.monotonic()
        process = subprocess.Popen(arguments, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                                   stderr=subprocess.PIPE, env=environment, start_new_session=True)
        try:
            stdout, stderr = process.communicate(timeout=20)
            events = [json.loads(line) for line in (root / "events.jsonl").read_text().splitlines()]
            for event in events:
                if event["command"] == "qemu-system-x86_64":
                    with self.assertRaises(ProcessLookupError, msg="harness left its guest child running"):
                        os.kill(event["pid"], 0)
        finally:
            # Only this fixture's newly created session; never a preexisting PID.
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            process.wait(timeout=5)
        return process.returncode, stdout, stderr, root, work, events, time.monotonic() - started

    def assert_offline(self, events):
        guests = [entry for entry in events if entry["command"] == "qemu-system-x86_64"]
        self.assertTrue(guests)
        for guest in guests:
            arguments = guest["arguments"]
            self.assertIn("-no-user-config", arguments)
            self.assertEqual(arguments.count("-nic"), 1)
            self.assertEqual(arguments[arguments.index("-nic") + 1], "none")
            self.assertNotIn("-net", arguments)
            self.assertNotIn("-netdev", arguments)
            self.assertTrue(any("readonly=on" in value for value in arguments))

    def test_success_retains_evidence_and_both_boots_are_offline(self):
        for script in SCRIPTS:
            with self.subTest(script=script):
                result, stdout, stderr, root, work, events, elapsed = self.run_fixture(script, "success")
                self.assertEqual(result, 0, (stdout, stderr))
                self.assert_offline(events)
                self.assertTrue(list(work.glob("qemu*.log")))
                self.assertTrue(list(work.glob("serial*.log")))
                if script == SCRIPTS[0]:
                    self.assertEqual((work / "system-first.id").read_bytes(), (work / "system-second.id").read_bytes())
                    self.assertEqual(sum(event["command"] == "qemu-system-x86_64" for event in events), 2)
                self.assertLess(elapsed, 20)

    def test_early_child_exit_fails_without_unbounded_monitor_wait(self):
        self.check_failure("early-exit")

    def test_alive_child_without_socket_fails_and_is_killed(self):
        self.check_failure("missing-monitor")

    def test_missing_serial_marker_fails_with_bounded_wait(self):
        self.check_failure("missing-log")

    def test_global_guest_deadline_stops_waits(self):
        self.check_failure("deadline")

    def test_clock_output_with_failure_is_rejected(self):
        self.check_failure("clock-failure")

    def test_hmp_failure_is_not_accepted(self):
        self.check_failure("hmp-failure")

    def test_quit_ignored_and_sigterm_ignored_fails_without_hanging(self):
        self.check_failure("stuck-quit")

    def check_failure(self, mode):
        for script in SCRIPTS:
            with self.subTest(script=script, mode=mode):
                result, stdout, stderr, root, work, events, elapsed = self.run_fixture(script, mode)
                self.assertNotEqual(result, 0, (stdout, stderr))
                self.assert_offline(events)
                logs = list(work.glob("qemu*.log"))
                self.assertTrue(logs, "failure logs must remain available")
                self.assertTrue(work.is_dir(), "failed fixture artifacts must be preserved")
                self.assertEqual((root / "fixture.iso").read_bytes(), b"not an ISO; never executed\n")
                self.assertLess(elapsed, 20)


if __name__ == "__main__":
    unittest.main()
