import io
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile
import unittest

import record_alpine_apks as recorder


SHELL = shutil.which("sh")
if SHELL is None and os.name == "nt":
    git_shell = Path("C:/Program Files/Git/usr/bin/sh.exe")
    if git_shell.is_file():
        SHELL = str(git_shell)


def shell_path(path):
    value = path.resolve().as_posix()
    return "/" + value[0].lower() + value[2:] if os.name == "nt" else value


def apk_bytes(name, version="1.0-r0"):
    data = f"pkgname = {name}\npkgver = {version}\narch = x86_64\nlicense = MIT\n".encode()
    buffer = io.BytesIO()
    with tarfile.open(fileobj=buffer, mode="w:gz") as archive:
        item = tarfile.TarInfo(".PKGINFO")
        item.size = len(data)
        archive.addfile(item, io.BytesIO(data))
    return buffer.getvalue()


class RecordingTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        self.root = self.base / "build/apkroot"
        self.database = self.root / "lib/apk/db/installed"
        self.database.parent.mkdir(parents=True)
        self.database.write_text("")
        self.destination = self.base / "recorded"
        self.environment = {
            "HHPT_REAL_APK": "/sbin/apk",
            "HHPT_APK_RECORD_DIR": str(self.destination),
            "HHPT_APK_BUILD_ROOT": str(self.base / "build"),
            "HHPT_APK_TMP_ROOT": str(self.base / "tmp"),
            "HHPT_APK_OFFLINE": "1",
        }
        self.calls = []
        self.archives = {"syslinux.apk": apk_bytes("syslinux")}

    def fake_apk(self, arguments, **kwargs):
        self.calls.append((arguments, kwargs))
        if "--output" in arguments:
            target = Path(arguments[arguments.index("--output") + 1])
            if target.parent == self.destination:
                for name, content in self.archives.items():
                    (target / name).write_bytes(content)
        return subprocess.CompletedProcess(arguments, 0)

    def run_apk(self, arguments):
        recorder.run(arguments, self.environment, self.fake_apk)

    def test_stdout_bootloader_fetch_is_recorded_without_polluting_stdout(self):
        self.run_apk(["fetch", "--root", str(self.root), "--stdout", "syslinux"])
        self.assertEqual(len(self.calls), 2)
        original, capture = self.calls
        self.assertIn("--stdout", original[0])
        self.assertNotIn("--stdout", capture[0])
        self.assertIs(capture[1]["stdout"], sys.stderr)
        self.assertEqual(len(list(self.destination.glob("*.apk"))), 1)
        self.assertTrue(all("--no-network" in args for args, _ in self.calls))

    def test_install_records_exact_dependency_and_install_if_versions(self):
        self.database.write_text("P:linux-lts\nV:6.12.1-r0\n\nP:firmware-dependency\nV:2.0-r1\n\n")
        self.archives = {name + ".apk": apk_bytes(name) for name in ("linux-lts", "firmware-dependency")}
        self.run_apk(["add", "-p", str(self.root), "--no-scripts", "linux-lts"])
        capture = self.calls[1][0]
        self.assertIn("linux-lts=6.12.1-r0", capture)
        self.assertIn("firmware-dependency=2.0-r1", capture)
        self.assertNotIn("--no-scripts", capture)
        self.assertEqual(len(list(self.destination.glob("*.apk"))), 2)

    def test_output_redirection_and_link_are_replaced_for_recording(self):
        self.run_apk(["fetch", "--root=" + str(self.root), "--link", "--recursive", "--output", "elsewhere", "syslinux"])
        capture = self.calls[1][0]
        self.assertEqual(capture.count("--output"), 1)
        self.assertNotIn("elsewhere", capture)
        self.assertNotIn("--link", capture)
        self.assertIn("--recursive", capture)

    def test_host_or_external_root_mutations_fail_before_apk_runs(self):
        for arguments in (["add", "mkinitfs"], ["update"], ["del", ".tmp-update-kernel"],
                          ["add", "--root", str(self.base / "outside"), "mkinitfs"]):
            with self.subTest(arguments=arguments), self.assertRaises(ValueError):
                self.run_apk(arguments)
        self.assertEqual(self.calls, [])

    def test_simulations_and_empty_initialization_do_not_invent_inputs(self):
        self.run_apk(["fetch", "--root", str(self.root), "--simulate", "syslinux"])
        self.run_apk(["add", "--root", str(self.root), "--initdb"])
        self.assertEqual(len(self.calls), 2)
        self.assertFalse(self.destination.exists())

    def test_missing_capture_and_modified_previous_archive_fail(self):
        self.archives = {}
        with self.assertRaises(ValueError):
            self.run_apk(["fetch", "--root", str(self.root), "syslinux"])
        self.archives = {"syslinux.apk": apk_bytes("syslinux")}
        self.run_apk(["fetch", "--root", str(self.root), "syslinux"])
        next(self.destination.glob("*.apk")).write_bytes(b"changed")
        with self.assertRaises(ValueError):
            self.run_apk(["fetch", "--root", str(self.root), "syslinux"])


@unittest.skipUnless(SHELL, "Alpine SBOM integration requires a POSIX shell")
class AlpineSBOMTests(unittest.TestCase):
    def test_consumed_kernel_and_bootloaders_are_required_unused_cache_is_omitted(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            records = root / "recorded"
            records.mkdir()
            image = root / "fixture.iso"
            image.write_bytes(b"fixture image")
            binary = root / "hhpt"
            binary.write_bytes(b"fixture executable")
            (root / "unused-cache.apk").write_bytes(apk_bytes("unused-cache-package"))
            for name in ("alpine-base", "linux-lts", "grub-efi", "syslinux"):
                (records / (name + ".apk")).write_bytes(apk_bytes(name))
            script = Path(__file__).with_name("write-alpine-sbom.sh").resolve()
            arguments = [SHELL, shell_path(script), "x86_64", shell_path(image), shell_path(binary), shell_path(records)]
            environment = dict(os.environ, SOURCE_DATE_EPOCH="1700000000")
            environment["PATH"] = str(Path(SHELL).parent) + os.pathsep + environment.get("PATH", "")
            result = subprocess.run(arguments, env=environment, capture_output=True)
            self.assertEqual(result.returncode, 0, result.stderr.decode(errors="replace"))
            output = Path(str(image) + ".spdx").read_text()
            for name in ("linux-lts", "grub-efi", "syslinux"):
                self.assertIn("PackageName: " + name + "\n", output)
            self.assertNotIn("unused-cache-package", output)
            self.assertIn("SPDXRef-DOCUMENT DESCRIBES SPDXRef-Image", output)
            (records / "syslinux.apk").unlink()
            result = subprocess.run(arguments, env=environment, capture_output=True)
            self.assertNotEqual(result.returncode, 0)


if __name__ == "__main__":
    unittest.main()
