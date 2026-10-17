"""Check the actual overlay generator against independent application bytes."""

import hashlib
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import tarfile
import tempfile
import unittest


GENERATOR = Path(__file__).resolve().parents[2] / "platform/alpine/mkimage/genapkovl-hhpt.sh"
NATIVE = sys.platform.startswith("linux") and all(shutil.which(name) for name in ("fakeroot", "install", "tar"))


@unittest.skipUnless(NATIVE, "overlay binding requires native Linux, fakeroot, install, and tar")
class OverlayBindingTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="hhpt-overlay-binding-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.binary = self.root / "hhpt"
        self.content = b"synthetic application bytes; this file is never executed\n"
        self.binary.write_bytes(self.content)
        self.binary.chmod(0o755)
        self.receipt = self.root / "overlay.sha256"
        self.environment = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL": "C", "TZ": "UTC",
                            "TMPDIR": str(self.root), "HOME": str(self.root), "FAKEROOTDONTTRYCHOWN": "1",
                            "HHPT_BINARY": str(self.binary), "HHPT_TARGET_ARCH": "x86_64",
                            "HHPT_BINARY_SHA256": hashlib.sha256(self.content).hexdigest(),
                            "HHPT_OVERLAY_RECEIPT": str(self.receipt)}

    def invoke(self):
        process = subprocess.Popen([shutil.which("fakeroot"), "--", "/bin/sh", str(GENERATOR), "hhpt"],
                                   cwd=self.root, env=self.environment, start_new_session=True,
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        try:
            stdout, stderr = process.communicate(timeout=15)
        except subprocess.TimeoutExpired:
            # Only the fixture's new process group is eligible for cleanup.
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.communicate(timeout=2)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.communicate(timeout=2)
            self.fail("overlay fixture exceeded its bounded execution time")
        return process.returncode, stdout + stderr

    def test_each_target_overlay_contains_the_bound_bytes_and_root_owned_executable(self):
        for architecture in ("x86", "x86_64", "aarch64"):
            with self.subTest(architecture=architecture):
                self.setUp()
                self.environment["HHPT_TARGET_ARCH"] = architecture
                code, output = self.invoke()
                self.assertEqual(code, 0, output.decode(errors="replace"))
                self.assertEqual(self.receipt.read_text(), hashlib.sha256(self.content).hexdigest() + "\n")
                with tarfile.open(self.root / "hhpt.apkovl.tar.gz", "r:gz") as archive:
                    member = archive.getmember("usr/bin/hhpt")
                    self.assertTrue(member.isfile())
                    self.assertEqual((member.uid, member.gid, member.mode), (0, 0, 0o755))
                    self.assertEqual(archive.extractfile(member).read(len(self.content) + 1), self.content)

    def test_wrong_digest_or_missing_binding_context_prevents_archive_creation(self):
        for failure in ("wrong-digest", "HHPT_BINARY_SHA256", "HHPT_OVERLAY_RECEIPT"):
            with self.subTest(failure=failure):
                self.setUp()
                if failure == "wrong-digest":
                    self.environment["HHPT_BINARY_SHA256"] = "0" * 64
                else:
                    del self.environment[failure]
                code, output = self.invoke()
                self.assertNotEqual(code, 0, output.decode(errors="replace"))
                self.assertFalse(self.receipt.exists())
                self.assertFalse((self.root / "hhpt.apkovl.tar.gz").exists())

    def test_existing_file_and_link_receipts_are_never_reused_or_overwritten(self):
        for kind in ("file", "link"):
            with self.subTest(kind=kind):
                self.setUp()
                preserved = self.root / "preserved-fixture"
                preserved.write_bytes(b"preserve this fixture\n")
                if kind == "file":
                    self.receipt.write_bytes(b"old receipt\n")
                else:
                    self.receipt.symlink_to(preserved)
                code, output = self.invoke()
                self.assertNotEqual(code, 0, output.decode(errors="replace"))
                self.assertEqual(preserved.read_bytes(), b"preserve this fixture\n")
                self.assertEqual(self.receipt.read_bytes(), b"old receipt\n" if kind == "file" else preserved.read_bytes())
                self.assertFalse((self.root / "hhpt.apkovl.tar.gz").exists())

    def test_matching_digest_output_cannot_hide_failed_checksum_command(self):
        stub = self.root / "tool-fixture"
        stub.mkdir()
        (stub / "sha256sum").write_text(
            '#!/bin/sh\nprintf "%s  %s\\n" "$HHPT_BINARY_SHA256" "$1"\nexit 47\n')
        (stub / "sha256sum").chmod(0o755)
        self.environment["PATH"] = str(stub) + ":" + self.environment["PATH"]
        code, output = self.invoke()
        self.assertNotEqual(code, 0, output.decode(errors="replace"))
        self.assertFalse(self.receipt.exists())
        self.assertFalse((self.root / "hhpt.apkovl.tar.gz").exists())

    def test_copy_mutation_links_and_fifo_are_rejected_before_receipt(self):
        for mutation in ("bytes", "symlink", "fifo"):
            with self.subTest(mutation=mutation):
                self.setUp()
                stub = self.root / "tool-fixture"
                stub.mkdir()
                (stub / "install").write_text(
                    '#!/bin/sh\nset -eu\n"$HHPT_FIXTURE_INSTALL" "$@"\n'
                    'for argument do destination=$argument; done\n'
                    'case "$HHPT_FIXTURE_MUTATION" in\n'
                    ' bytes) printf "changed installed bytes\\n" > "$destination" ;;\n'
                    ' symlink) rm -f -- "$destination"; ln -s "$HHPT_BINARY" "$destination" ;;\n'
                    ' fifo) rm -f -- "$destination"; mkfifo "$destination" ;;\n'
                    'esac\n')
                (stub / "install").chmod(0o755)
                self.environment.update(PATH=str(stub) + ":" + self.environment["PATH"],
                                        HHPT_FIXTURE_INSTALL=shutil.which("install"), HHPT_FIXTURE_MUTATION=mutation)
                code, output = self.invoke()
                self.assertNotEqual(code, 0, output.decode(errors="replace"))
                self.assertFalse(self.receipt.exists())
                self.assertFalse((self.root / "hhpt.apkovl.tar.gz").exists())
                self.assertEqual(self.binary.read_bytes(), self.content)


if __name__ == "__main__":
    unittest.main()
