#!/bin/sh
set -eu

# Python supplies structured QMP messages, bounded waits, and cleanup of only
# the QEMU child started here. No host mount, loop device, or physical disk is used.
exec python3 - "$@" <<'PY'
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import signal
import socket
import stat
import struct
import subprocess
import sys
import time


PARTITION_OFFSET = 1 << 20
PARTITION_BYTES = 64 << 20
GUEST_TIMEOUT = 240
MAX_SERIAL_BYTES = 8 << 20


class HarnessError(Exception):
    pass


def require(condition, message):
    if not condition:
        raise HarnessError(message)


def sha256(path):
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1 << 20), b""):
            digest.update(chunk)
    return digest.hexdigest()


def regular_input(value, executable=False):
    path = Path(value)
    require(not path.is_symlink() and path.is_file(), "input must be a regular, non-symlink file")
    path = path.resolve(strict=True)
    require(not executable or os.access(path, os.X_OK), "HHPT verifier must be executable")
    require(all(ord(char) >= 32 and char != "," for char in str(path)), "input path contains unsupported characters")
    return path


def new_work_directory(value):
    requested = Path(value).absolute()
    require(not os.path.lexists(requested), "work directory must not already exist")
    parent = requested.parent.resolve(strict=True)
    require(parent.is_dir(), "work directory parent must exist")
    require(not any(parent == Path(root) or Path(root) in parent.parents for root in ("/dev", "/proc", "/sys")),
            "work directory must use an ordinary filesystem")
    work = parent / requested.name
    require(all(ord(char) >= 32 and char != "," for char in str(work)), "work path contains unsupported characters")
    require(len(os.fsencode(work / "monitor.sock")) < 104, "work path is too long for the QMP socket")
    work.mkdir(mode=0o700)
    return work


def run_tool(arguments, log, timeout=30, environment=None):
    with log.open("xb") as output:
        result = subprocess.run(arguments, stdin=subprocess.DEVNULL, stdout=output, stderr=subprocess.STDOUT,
                                timeout=timeout, check=False, env=environment)
    require(result.returncode == 0, log.name + ": tool failed; inspect the retained log")


def create_disk(work, mkfs):
    # Format a new regular partition file, then assemble an MBR disk as bytes.
    # The image is never attached to a host loop device or mounted on the host.
    partition = work / "partition.fat"
    with partition.open("xb") as target:
        target.truncate(PARTITION_BYTES)
    run_tool([mkfs, "-F", "32", "-h", "2048", "-n", "HHPTREPORT", str(partition)], work / "mkfs.log")
    require(partition.stat().st_size == PARTITION_BYTES, "formatter changed partition capacity")
    mbr = bytearray(512)
    mbr[446:462] = struct.pack("<B3sB3sII", 0, b"\xfe\xff\xff", 0x0C, b"\xfe\xff\xff",
                               PARTITION_OFFSET // 512, PARTITION_BYTES // 512)
    mbr[510:512] = b"\x55\xaa"
    disk = work / "report-usb.raw"
    with disk.open("xb") as target, partition.open("rb") as source:
        target.truncate(PARTITION_OFFSET + PARTITION_BYTES)
        target.write(mbr)
        target.seek(PARTITION_OFFSET)
        shutil.copyfileobj(source, target, 1 << 20)
    return disk


class QMP:
    def __init__(self, path):
        self.socket = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.socket.settimeout(3)
        self.reader = None
        self.identifier = 0
        try:
            self.socket.connect(str(path))
            self.reader = self.socket.makefile("rb")
            require("QMP" in self.read(), "QMP greeting missing")
            self.command("qmp_capabilities")
        except BaseException:
            self.close()
            raise

    def close(self):
        if self.reader is not None:
            self.reader.close()
        self.socket.close()

    def read(self):
        line = self.reader.readline(65537)
        require(bool(line) and len(line) <= 65536, "missing or oversized QMP response")
        value = json.loads(line)
        require(isinstance(value, dict), "invalid QMP response")
        return value

    def command(self, name, arguments=None):
        self.identifier += 1
        request = {"execute": name, "id": self.identifier}
        if arguments is not None:
            request["arguments"] = arguments
        self.socket.sendall(json.dumps(request).encode("ascii") + b"\n")
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            response = self.read()
            if response.get("id") == self.identifier:
                require("return" in response and "error" not in response, "QMP command failed: " + name)
                return response["return"]
        raise HarnessError("QMP command deadline expired")

    def type_line(self, value):
        # No monitor command or shell text is constructed from guest output.
        require(re.fullmatch(r"[A-Z0-9-]{1,16}", value) is not None, "invalid keyboard input")
        for char in value + "\n":
            key = "ret" if char == "\n" else "minus" if char == "-" else char.lower()
            keys = ([{"type": "qcode", "data": "shift"}] if char.isupper() else [])
            keys.append({"type": "qcode", "data": key})
            self.command("send-key", {"keys": keys, "hold-time": 50})
            time.sleep(0.10)

    def disk_writes(self):
        devices = self.command("query-blockstats")
        require(isinstance(devices, list), "invalid QMP block statistics")
        matching = [entry for entry in devices if isinstance(entry, dict) and entry.get("device") == "reportdisk"]
        require(len(matching) == 1, "report disk statistics are missing or ambiguous")
        counters = matching[0].get("stats", {})
        result = {name: counters.get(name) for name in ("wr_bytes", "wr_operations")}
        require(all(type(value) is int and value >= 0 for value in result.values()), "invalid report write counters")
        return result


def serial_text(path):
    if not path.exists():
        return ""
    require(path.stat().st_size <= MAX_SERIAL_BYTES, "serial log exceeded its limit")
    with path.open("rb") as source:
        data = source.read(MAX_SERIAL_BYTES + 1)
    require(len(data) <= MAX_SERIAL_BYTES, "serial log exceeded its limit")
    return data.decode("ascii", errors="replace").replace("\r", "")


def require_no_guest_failure(text):
    require(re.search(r"^HHPT_(?:FAILED|SAFETY_FAIL|SAVE_FAILED)\b", text, re.MULTILINE) is None,
            "guest reported a failed run or save")


def ready_fields(line):
    tokens = line.split(" ")
    require(tokens[0] == "HHPT_READY", "invalid ready marker")
    fields = {}
    for token in tokens[1:]:
        match = re.fullmatch(r"([a-z_]+)=([A-Za-z0-9_/.:+-]+)", token, re.ASCII)
        require(match is not None and match[1] not in fields, "invalid or duplicate ready field")
        fields[match[1]] = match[2]
    return fields


def wait_marker(process, serial, pattern, deadline, start=0):
    expression = re.compile(pattern, re.MULTILINE | re.ASCII)
    while time.monotonic() < deadline:
        text = serial_text(serial)
        require_no_guest_failure(text)
        match = expression.search(text, start)
        if match:
            return match
        require(process.poll() is None, "QEMU exited before the expected marker")
        time.sleep(0.2)
    raise HarnessError("guest deadline expired while waiting for " + pattern.split(" ")[0])


def stop_child(process, qmp):
    # Never signal a PID found in a log, an existing VM, or a process group.
    if process.poll() is None and qmp is not None:
        try:
            qmp.command("quit")
            process.wait(timeout=5)
        except (HarnessError, OSError, ValueError, subprocess.TimeoutExpired):
            pass
    if process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=5)
    if qmp is not None:
        qmp.close()


def run_guest(iso, disk, work, qemu):
    serial = work / "serial.log"
    monitor = work / "monitor.sock"
    initial_hash = sha256(disk)
    with disk.open("rb") as source:
        initial_prefix = source.read(PARTITION_OFFSET)
    arguments = [qemu, "-no-user-config", "-machine", "accel=tcg", "-m", "1024", "-smp", "2",
                 "-uuid", "44444444-4444-4444-8444-444444444444",
                 "-cdrom", str(iso), "-boot", "once=d", "-no-reboot", "-nic", "none",
                 "-device", "qemu-xhci", "-drive",
                 "file=" + str(disk) + ",format=raw,if=none,id=reportdisk,cache=writethrough",
                 "-device", "usb-storage,drive=reportdisk,serial=HHPTLABREPORT,removable=on",
                 "-serial", "file:" + str(serial), "-qmp", "unix:" + str(monitor) + ",server=on,wait=off",
                 "-monitor", "none", "-display", "none"]
    qmp = None
    with (work / "qemu.log").open("xb") as qemu_log:
        process = subprocess.Popen(arguments, stdin=subprocess.DEVNULL, stdout=qemu_log, stderr=subprocess.STDOUT)
        try:
            deadline = time.monotonic() + GUEST_TIMEOUT
            monitor_deadline = time.monotonic() + 15
            while not monitor.exists():
                require(process.poll() is None, "QEMU failed to start")
                require(time.monotonic() < monitor_deadline, "QMP socket did not appear")
                time.sleep(0.1)
            require(stat.S_ISSOCK(monitor.lstat().st_mode), "QMP path is not a socket")
            qmp = QMP(monitor)
            wait_marker(process, serial, r"^HHPT_UI_WAIT timeout=15s architecture=x86_64$", deadline)
            ready = wait_marker(process, serial, r"^HHPT_READY report=/run/hhpt/session\.hhpcr [^\n]+$", deadline)
            fields = ready_fields(ready.group(0))
            require(all(fields.get(name) == value for name, value in {
                "integrity": "pass", "cpu": "pass", "memory": "pass", "storage": "pass",
                "writable_block_mounts": "0", "signature": "self-issued"}.items()),
                "safe run did not meet the required report-save prerequisites")
            candidate_pattern = r"^HHPT_SAVE_CANDIDATE index=1 device=/dev/sda1 confirmation=(RPT-[0-9A-F]{6})$"
            candidate = wait_marker(process, serial, candidate_pattern, deadline, ready.end())
            require(qmp.disk_writes() == {"wr_bytes": 0, "wr_operations": 0},
                    "guest wrote the report disk before authorization")
            require(sha256(disk) == initial_hash, "report disk changed before authorization")
            qmp.type_line("1")
            confirm_pattern = r"^HHPT_SAVE_CONFIRM index=1 device=/dev/sda1 confirmation=(RPT-[0-9A-F]{6})$"
            confirm = wait_marker(process, serial, confirm_pattern, deadline, candidate.end())
            require(confirm[1] == candidate[1], "candidate changed before confirmation")
            wrong_code = "RPT-000000" if confirm[1] != "RPT-000000" else "RPT-000001"
            qmp.type_line(wrong_code)
            rejected = wait_marker(process, serial, r"^HHPT_SAVE_REJECTED reason=confirmation$", deadline, confirm.end())
            require(qmp.disk_writes() == {"wr_bytes": 0, "wr_operations": 0},
                    "wrong confirmation caused a guest disk write")
            require(sha256(disk) == initial_hash, "wrong confirmation changed report disk bytes")
            require("HHPT_SAVE_SAVED " not in serial_text(serial), "wrong confirmation unexpectedly saved a report")
            candidate = wait_marker(process, serial, candidate_pattern, deadline, rejected.end())
            qmp.type_line("1")
            confirm = wait_marker(process, serial, confirm_pattern, deadline, candidate.end())
            require(confirm[1] == candidate[1], "candidate changed before correct confirmation")
            require(qmp.disk_writes() == {"wr_bytes": 0, "wr_operations": 0},
                    "second selection wrote the report disk before correct confirmation")
            require(sha256(disk) == initial_hash,
                    "second selection changed report disk bytes before correct confirmation")
            qmp.type_line(confirm[1])
            saved = wait_marker(process, serial,
                                r"^HHPT_SAVE_SAVED file=([A-Za-z0-9_.-]{1,122}\.hhpcr) bytes=([1-9][0-9]{0,7}) sha256=([0-9a-f]{64})$",
                                deadline, confirm.end())
            require(int(saved[2]) <= PARTITION_BYTES, "saved report exceeds the fixture capacity")
            writes = qmp.disk_writes()
            require(writes["wr_bytes"] > 0 and writes["wr_operations"] > 0, "save receipt lacks actual disk writes")
            qmp.command("quit")
            require(process.wait(timeout=10) == 0, "QEMU failed to exit cleanly")
            final_serial = serial_text(serial)
            require_no_guest_failure(final_serial)
            require(len(re.findall(r"^HHPT_SAVE_SAVED ", final_serial, re.MULTILINE)) == 1,
                    "save receipt is missing or ambiguous in the final serial log")
            with disk.open("rb") as source:
                require(source.read(PARTITION_OFFSET) == initial_prefix,
                        "authorized save changed bytes outside the report partition")
            return {"file": saved[1], "bytes": int(saved[2]), "sha256": saved[3],
                    "wrong_confirmation_unchanged": True, "writes_before_authorization": 0,
                    "bytes_outside_partition_unchanged": True, "saved_disk_writes": writes, "ready": fields}
        finally:
            stop_child(process, qmp)


def verify_saved(disk, work, verifier, mcopy, receipt):
    require(disk.stat().st_size == PARTITION_OFFSET + PARTITION_BYTES, "guest changed the disk capacity")
    disk_hash = sha256(disk)
    extracted_partition = work / "saved-partition.fat"
    with disk.open("rb") as source, extracted_partition.open("xb") as target:
        source.seek(PARTITION_OFFSET)
        shutil.copyfileobj(source, target, 1 << 20)
    extracted_partition.chmod(0o400)
    report = work / "saved-report.hhpcr"
    # Isolate mtools from local drive aliases/configuration. Its explicit -i
    # source is a read-only copy of the disposable image, after QEMU has exited.
    mtools_environment = {key: value for key, value in os.environ.items() if not key.startswith("MTOOLS")}
    mtools_environment.update({"MTOOLSRC": "/dev/null", "LC_ALL": "C"})
    run_tool([mcopy, "-i", str(extracted_partition), "::" + receipt["file"], str(report)],
             work / "mcopy.log", environment=mtools_environment)
    require(report.is_file() and not report.is_symlink(), "mtools did not extract a regular report")
    require(report.stat().st_size == receipt["bytes"] and sha256(report) == receipt["sha256"],
            "extracted report does not match the guest read-back receipt")
    run_tool([str(verifier), "verify", "-self-issued", str(report)], work / "verify.log", timeout=20)
    require((work / "verify.log").read_text(encoding="utf-8").strip() ==
            "verified: contents/signature valid; signer is self-issued and does not prove operator identity",
            "verifier did not confirm self-issued contents and signature")
    require(sha256(disk) == disk_hash, "host extraction changed the disposable disk")
    return disk_hash


def interrupted(_number, _frame):
    raise HarnessError("interrupted; stopping only the QEMU child started by this harness")


def main():
    require(len(sys.argv) == 4, "usage: test-report-save-qemu.sh ISO HHPT_BINARY NEW_WORK_DIRECTORY")
    require(sys.platform.startswith("linux"), "this harness requires a Linux host")
    os.umask(0o077)
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    iso = regular_input(sys.argv[1])
    verifier = regular_input(sys.argv[2], executable=True)
    tools = {name: shutil.which(name) for name in ("qemu-system-x86_64", "mkfs.vfat", "mcopy")}
    require(all(tools.values()), "required host tools: qemu-system-x86_64, mkfs.vfat, mcopy, python3")
    work = new_work_directory(sys.argv[3])
    iso_hash, verifier_hash = sha256(iso), sha256(verifier)
    disk = create_disk(work, tools["mkfs.vfat"])
    receipt = run_guest(iso, disk, work, tools["qemu-system-x86_64"])
    disk_hash = verify_saved(disk, work, verifier, tools["mcopy"], receipt)
    require(sha256(iso) == iso_hash and sha256(verifier) == verifier_hash, "ISO or verifier changed during validation")
    result = {"schema_version": 1, "status": "pass", "scope": "qemu-file-backed-report-save",
              "signature_assurance": "self-issued", "iso_sha256": iso_hash, "verifier_sha256": verifier_hash,
              "disk_sha256": disk_hash, "receipt": receipt}
    with (work / "validation.json").open("x", encoding="utf-8") as output:
        json.dump(result, output, indent=2, sort_keys=True)
        output.write("\n")
    print("PASS: wrong confirmation caused no disk writes; authorized report was saved, extracted, and verified (self-issued)")


if __name__ == "__main__":
    try:
        main()
    except HarnessError as error:
        print("FAIL: " + str(error), file=sys.stderr)
        sys.exit(1)
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        # Retain detailed tool logs locally, but do not echo arbitrary guest or
        # environment content as host commands or public diagnostics.
        print("FAIL: harness operation failed (" + type(error).__name__ + "); inspect retained work logs", file=sys.stderr)
        sys.exit(1)
PY
