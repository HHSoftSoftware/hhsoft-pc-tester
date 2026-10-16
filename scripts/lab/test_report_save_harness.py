"""File-only safety and evidence regressions for the report-save QEMU harness.

Compile the actual embedded Python: shell syntax checking alone cannot detect
errors in a here-document. No test launches QEMU, formats a filesystem, mounts
media, or accesses a host block device.
"""

import hashlib
from pathlib import Path
import struct
import subprocess
import tempfile
import types
import unittest
from unittest import mock


def load_harness():
    path = Path(__file__).with_name("test-report-save-qemu.sh")
    text = path.read_text(encoding="utf-8")
    opening = "<<'PY'\n"
    if text.count(opening) != 1:
        raise AssertionError("expected one embedded Python here-document")
    body, closing, remainder = text.split(opening, 1)[1].rpartition("\nPY\n")
    if not closing or remainder.strip():
        raise AssertionError("embedded Python must end at its here-document delimiter")
    module = types.ModuleType("hhpt_report_save_harness_fixture")
    exec(compile(body, str(path) + ":embedded-python", "exec"), module.__dict__)
    return module


class ChildFixture:
    """A process handle with no associated operating-system process."""

    def __init__(self, stubborn=False, exited=False):
        self.stubborn = stubborn
        self.returncode = 0 if exited else None
        self.actions = []

    def poll(self):
        return self.returncode

    def terminate(self):
        self.actions.append("terminate")

    def kill(self):
        self.actions.append("kill")
        self.returncode = -9

    def wait(self, timeout):
        self.actions.append(("wait", timeout))
        if self.stubborn and self.returncode is None:
            raise subprocess.TimeoutExpired("fixture-only", timeout)
        if self.returncode is None:
            self.returncode = 0
        return self.returncode


class ReportSaveHarnessTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.harness = load_harness()

    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="hhpt-report-save-test-")
        self.addCleanup(temporary.cleanup)
        self.work = Path(temporary.name)
        # An accidental external command or monitor connection must fail the
        # tests, even on a host which happens to have the real lab tools.
        for owner, name in ((self.harness.subprocess, "run"), (self.harness.subprocess, "Popen"),
                            (self.harness.socket, "socket"), (self.harness.os, "system"),
                            (self.harness.os, "kill")):
            patcher = mock.patch.object(owner, name, side_effect=AssertionError("unexpected host operation: " + name))
            patcher.start()
            self.addCleanup(patcher.stop)
        if hasattr(self.harness.os, "killpg"):
            patcher = mock.patch.object(self.harness.os, "killpg", side_effect=AssertionError("process-group signaling is forbidden"))
            patcher.start()
            self.addCleanup(patcher.stop)

    def test_existing_work_and_files_are_never_reused(self):
        sentinel = self.work / "existing-report.hhpcr"
        sentinel.write_bytes(b"preserve prior evidence")
        for path in (self.work, sentinel):
            with self.subTest(kind="directory" if path.is_dir() else "file"):
                with self.assertRaises(self.harness.HarnessError):
                    self.harness.new_work_directory(str(path))
        self.assertEqual(sentinel.read_bytes(), b"preserve prior evidence")
        self.assertEqual(list(self.work.iterdir()), [sentinel])

    def test_mbr_addresses_only_the_new_partition_payload(self):
        header, footer = b"FILE-ONLY PARTITION START", b"FILE-ONLY PARTITION END"
        formatted = []

        def format_fixture(arguments, _log, **_kwargs):
            target = Path(arguments[-1])
            self.assertEqual(target.parent, self.work)
            self.assertTrue(target.is_file())
            self.assertFalse(target.is_symlink())
            self.assertEqual(target.stat().st_size, 64 << 20)
            self.assertEqual(arguments[1:7], ["-F", "32", "-h", "2048", "-n", "HHPTREPORT"])
            with target.open("r+b") as output:
                output.write(header)
                output.seek(-len(footer), 2)
                output.write(footer)
            formatted.append(target)

        with mock.patch.object(self.harness, "run_tool", side_effect=format_fixture):
            disk = self.harness.create_disk(self.work, "fixture-formatter")
        self.assertEqual(len(formatted), 1)
        self.assertEqual(disk.parent, self.work)
        self.assertEqual(disk.stat().st_size, 65 << 20)
        with disk.open("rb") as source:
            mbr = source.read(512)
            self.assertEqual(mbr[510:512], b"\x55\xaa")
            self.assertEqual(mbr[446], 0)  # report data is not a boot partition
            self.assertEqual(mbr[450], 0x0C)  # DOS FAT32 LBA
            first_sector, sectors = struct.unpack_from("<II", mbr, 454)
            self.assertEqual(first_sector * 512, 1 << 20)
            self.assertEqual(sectors * 512, 64 << 20)
            self.assertEqual((first_sector + sectors) * 512, disk.stat().st_size)
            self.assertEqual(mbr[462:510], bytes(48))  # no hidden extra partition
            self.assertEqual(source.read((1 << 20) - 512), bytes((1 << 20) - 512))
            copied = hashlib.sha256()
            for chunk in iter(lambda: source.read(1 << 20), b""):
                copied.update(chunk)
        expected = hashlib.sha256()
        with formatted[0].open("rb") as source:
            for chunk in iter(lambda: source.read(1 << 20), b""):
                expected.update(chunk)
        self.assertEqual(copied.digest(), expected.digest())

    def test_guest_text_cannot_inject_monitor_or_shell_commands(self):
        qmp = self.harness.QMP.__new__(self.harness.QMP)
        qmp.command = mock.Mock()
        for text in ("RPT-ABC123\nquit", "RPT-ABC123;quit", "$(touch fixture)",
                     "../target", "RPT-ABC123\x00", "", "A" * 17):
            with self.subTest(text=repr(text)):
                with self.assertRaises(self.harness.HarnessError):
                    qmp.type_line(text)
        qmp.command.assert_not_called()
        with mock.patch.object(self.harness.time, "sleep"):
            qmp.type_line("RPT-ABC123")
        self.assertTrue(qmp.command.called)
        self.assertTrue(all(call.args[0] == "send-key" for call in qmp.command.call_args_list))
        self.assertEqual(qmp.command.call_args_list[0].args[1]["keys"],
                         [{"type": "qcode", "data": "shift"}, {"type": "qcode", "data": "r"}])
        self.assertEqual(qmp.command.call_args_list[-1].args[1]["keys"], [{"type": "qcode", "data": "ret"}])

    def test_missing_ambiguous_or_invalid_write_counters_cannot_prove_zero_writes(self):
        qmp = self.harness.QMP.__new__(self.harness.QMP)
        valid = {"device": "reportdisk", "stats": {"wr_bytes": 0, "wr_operations": 0}}
        invalid = [None, [], [dict(valid, device="another-disk")], [valid, valid],
                   [{"device": "reportdisk", "stats": {"wr_bytes": 0}}]]
        for counter in ("wr_bytes", "wr_operations"):
            for value in (-1, False, "0", None):
                invalid.append([{"device": "reportdisk", "stats": dict(valid["stats"], **{counter: value})}])
        for response in invalid:
            with self.subTest(response=response):
                qmp.command = mock.Mock(return_value=response)
                with self.assertRaises(self.harness.HarnessError):
                    qmp.disk_writes()
        qmp.command = mock.Mock(return_value=[valid])
        self.assertEqual(qmp.disk_writes(), {"wr_bytes": 0, "wr_operations": 0})
        qmp.command = mock.Mock(return_value=[{"device": "reportdisk", "stats": {"wr_bytes": 512, "wr_operations": 1}}])
        self.assertEqual(qmp.disk_writes(), {"wr_bytes": 512, "wr_operations": 1})

    def test_failure_before_or_after_readiness_overrules_a_ready_marker(self):
        ready = "HHPT_READY report=/run/hhpt/session.hhpcr integrity=pass\n"
        serial = self.work / "serial.log"
        for marker in ("HHPT_FAILED", "HHPT_SAFETY_FAIL", "HHPT_SAVE_FAILED"):
            for text in (marker + " reason=fixture\n" + ready, ready + marker + " reason=fixture\n"):
                with self.subTest(text=text):
                    serial.write_text(text, encoding="ascii")
                    with self.assertRaises(self.harness.HarnessError):
                        self.harness.wait_marker(ChildFixture(), serial, r"^HHPT_READY .*$",
                                                 self.harness.time.monotonic() + 1)

    def test_duplicate_or_unparsed_ready_fields_are_not_trusted(self):
        for text in ("HHPT_READY integrity=fail integrity=pass",
                     "HHPT_READY writable_block_mounts=1 writable_block_mounts=0",
                     "HHPT_READY integrity=pass arbitrary",
                     "HHPT_READY integrity=pass\nHHPT_OTHER status=pass"):
            with self.subTest(text=text):
                with self.assertRaises(self.harness.HarnessError):
                    self.harness.ready_fields(text)
        self.assertEqual(self.harness.ready_fields("HHPT_READY integrity=pass writable_block_mounts=0"),
                         {"integrity": "pass", "writable_block_mounts": "0"})

    def test_failure_after_save_receipt_during_shutdown_prevents_success(self):
        # Exercise run_guest's final decision, not just its failure-parser helper:
        # the late line does not exist when the valid receipt is first observed.
        for late_failure in (None, "HHPT_SAVE_FAILED", "HHPT_SAFETY_FAIL", "HHPT_FAILED"):
            with self.subTest(late_failure=late_failure):
                work = self.work / (late_failure or "successful-control")
                work.mkdir()
                if late_failure is None:
                    receipt = self.run_guest_fixture(work, late_failure)
                    self.assertEqual(receipt["file"], "fixture.hhpcr")
                else:
                    with self.assertRaisesRegex(self.harness.HarnessError, "guest reported a failed run or save"):
                        self.run_guest_fixture(work, late_failure)

    def test_second_selection_cannot_write_before_correct_confirmation(self):
        # Independent witnesses: counters must catch a write even if bytes were
        # restored, and the image digest must catch a write missing from counters.
        for witness in ("counters", "bytes"):
            with self.subTest(witness=witness):
                work = self.work / witness
                work.mkdir()
                with self.assertRaisesRegex(self.harness.HarnessError, "second selection"):
                    self.run_guest_fixture(work, None, premature_write=witness)

    def test_cleanup_escalates_only_the_owned_child_with_finite_waits(self):
        child, unrelated = ChildFixture(stubborn=True), ChildFixture()
        qmp = mock.Mock()
        qmp.command.side_effect = self.harness.HarnessError("fixture monitor unavailable")
        self.harness.stop_child(child, qmp)
        self.assertEqual(child.actions, ["terminate", ("wait", 5), "kill", ("wait", 5)])
        self.assertEqual(unrelated.actions, [])
        qmp.command.assert_called_once_with("quit")
        qmp.close.assert_called_once_with()

    def test_cleanup_does_not_signal_a_completed_child(self):
        child = ChildFixture(exited=True)
        qmp = mock.Mock()
        self.harness.stop_child(child, qmp)
        self.assertEqual(child.actions, [])
        qmp.command.assert_not_called()
        qmp.close.assert_called_once_with()

    def run_guest_fixture(self, work, late_failure, premature_write=None):
        serial, monitor, disk = (work / name for name in ("serial.log", "monitor.sock", "disk.raw"))
        monitor.touch()  # the socket-type boundary is injected; no socket opens
        with disk.open("xb") as output:
            output.truncate(2 << 20)
        serial.write_text(
            "HHPT_UI_WAIT timeout=15s architecture=x86_64\n"
            "HHPT_READY report=/run/hhpt/session.hhpcr integrity=pass cpu=pass memory=pass storage=pass "
            "writable_block_mounts=0 signature=self-issued\n"
            "HHPT_SAVE_CANDIDATE index=1 device=/dev/sda1 confirmation=RPT-ABC123\n", encoding="ascii")
        child = ChildFixture()
        owner = self

        class MonitorFixture:
            def __init__(self):
                self.step = 0
                self.closed = False

            def append(self, line):
                with serial.open("a", encoding="ascii") as output:
                    output.write(line + "\n")

            def type_line(self, text):
                expected = ("1", "RPT-000000", "1", "RPT-ABC123")
                owner.assertEqual(text, expected[self.step])
                if self.step == 3 and premature_write:
                    owner.fail("correct confirmation was typed despite a premature write")
                if self.step == 2 and premature_write == "bytes":
                    with disk.open("r+b") as output:
                        output.seek(1 << 20)
                        output.write(b"premature fixture write")
                if self.step in (0, 2):
                    self.append("HHPT_SAVE_CONFIRM index=1 device=/dev/sda1 confirmation=RPT-ABC123")
                elif self.step == 1:
                    self.append("HHPT_SAVE_REJECTED reason=confirmation")
                    self.append("HHPT_SAVE_CANDIDATE index=1 device=/dev/sda1 confirmation=RPT-ABC123")
                else:
                    payload = b"fixture report"
                    with disk.open("r+b") as output:
                        output.seek(1 << 20)
                        output.write(payload)
                    self.append("HHPT_SAVE_SAVED file=fixture.hhpcr bytes=" + str(len(payload)) +
                                " sha256=" + hashlib.sha256(payload).hexdigest())
                self.step += 1

            def disk_writes(self):
                written = self.step == 4 or (premature_write == "counters" and self.step >= 3)
                return {"wr_bytes": 512, "wr_operations": 1} if written else {"wr_bytes": 0, "wr_operations": 0}

            def command(self, name):
                owner.assertEqual(name, "quit")
                if late_failure:
                    self.append(late_failure + " reason=shutdown-fixture")
                child.returncode = 0
                return {}

            def close(self):
                self.closed = True

        qmp = MonitorFixture()
        iso = work / "unused-fixture.iso"
        with mock.patch.object(self.harness.subprocess, "Popen", return_value=child) as launch, \
                mock.patch.object(self.harness, "QMP", return_value=qmp), \
                mock.patch.object(self.harness.stat, "S_ISSOCK", return_value=True):
            try:
                return self.harness.run_guest(iso, disk, work, "fixture-qemu")
            finally:
                launch.assert_called_once()
                arguments = launch.call_args.args[0]
                self.assertEqual(arguments[arguments.index("-nic") + 1], "none")
                self.assertIn("-no-user-config", arguments)
                self.assertEqual(arguments[arguments.index("-cdrom") + 1], str(iso))
                drives = [arguments[index + 1] for index, value in enumerate(arguments) if value == "-drive"]
                self.assertEqual(len(drives), 1)
                self.assertTrue(drives[0].startswith("file=" + str(disk) + ",format=raw,"))
                self.assertEqual(child.poll(), 0)
                self.assertTrue(qmp.closed)
                if premature_write:
                    self.assertEqual(qmp.step, 3)


if __name__ == "__main__":
    unittest.main()
