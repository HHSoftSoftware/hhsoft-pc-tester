import os
import ctypes.util
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import unittest

import test_scanelf_readonly as scanelf_helpers

SHELL = scanelf_helpers.SHELL
shell_path = scanelf_helpers.shell_path


LAUNCHER = Path(__file__).with_name("fakeroot-apkovl.sh").resolve()


@unittest.skipUnless(SHELL, "apkovl launcher tests require a POSIX shell")
class ApkovlLauncherTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        # A real harmless library keeps the host loader valid while checking
        # that the shim leaves LD_PRELOAD intact. This is not a fake daemon.
        preload = "/usr/bin/msys-2.0.dll" if os.name == "nt" else ctypes.util.find_library("c")
        self.assertTrue(preload, "host C runtime must be available for preload fixture")
        (self.root / "working-directory-fixture").write_bytes(b"fixture")
        self.generator = self.root / "genapkovl-hhpt.sh"
        self.generator.write_text(
            "#!/bin/sh\n"
            "test \"$FAKEROOTKEY\" = fixture-session || exit 90\n"
            "test \"$LD_PRELOAD\" = \"$HHPT_FIXTURE_PRELOAD\" || exit 91\n"
            "test -f working-directory-fixture || exit 92\n"
            "printf '%s\\n' same-working-directory \"$@\"\n"
            "exit 23\n", encoding="utf-8", newline="\n")
        self.generator.chmod(0o755)
        self.environment = dict(os.environ, HHPT_APKOVL_SCRIPT=shell_path(self.generator),
                                HHPT_FIXTURE_PRELOAD=preload)
        self.environment["PATH"] = str(Path(SHELL).parent) + os.pathsep + self.environment.get("PATH", "")
        self.environment.pop("LD_PRELOAD", None)
        self.environment.pop("FAKEROOTKEY", None)

    def invoke(self, arguments, setup=None):
        if setup is None:
            setup = ('FAKEROOTKEY=fixture-session; LD_PRELOAD=$HHPT_FIXTURE_PRELOAD; '
                     'export FAKEROOTKEY LD_PRELOAD; ')
        return subprocess.run([SHELL, "-c", setup + '. "$0"', shell_path(LAUNCHER), *arguments],
                              cwd=self.root, env=self.environment, capture_output=True,
                              text=True, timeout=5)

    def test_exact_generator_retains_session_working_directory_and_exit_status(self):
        result = self.invoke([shell_path(self.generator), "hhpt"])
        self.assertEqual(result.returncode, 23, result.stderr)
        self.assertEqual(result.stdout.splitlines(), ["same-working-directory", "hhpt"])

    def test_unrelated_commands_options_and_extra_arguments_cannot_run(self):
        invalid = ([], ["--", shell_path(self.generator), "hhpt"],
                   [shell_path(self.generator), "another-host"],
                   [shell_path(self.generator), "hhpt", "extra"],
                   ["/bin/true", "hhpt"], ["genapkovl-hhpt.sh", "hhpt"],
                   ["-s", "state-file"])
        for arguments in invalid:
            with self.subTest(arguments=arguments):
                result = self.invoke(arguments)
                self.assertEqual(result.returncode, 2, result.stderr)
                self.assertEqual(result.stdout, "")

    def test_missing_outer_session_fails_without_starting_a_replacement(self):
        for setup in ("", "FAKEROOTKEY=fixture-session; export FAKEROOTKEY; ",
                      "LD_PRELOAD=$HHPT_FIXTURE_PRELOAD; export LD_PRELOAD; "):
            with self.subTest(setup=setup):
                result = self.invoke([shell_path(self.generator), "hhpt"], setup)
                self.assertEqual(result.returncode, 2, result.stderr)
                self.assertEqual(result.stdout, "")

    def test_shell_syntax(self):
        subprocess.run([SHELL, "-n", shell_path(LAUNCHER)], check=True, timeout=5)


@unittest.skipUnless(os.name == "posix" and shutil.which("fakeroot") and shutil.which("tar"),
                    "Native apkovl metadata regression requires fakeroot and tar")
class NativeApkovlTests(unittest.TestCase):
    run_bounded = scanelf_helpers.NativeScanelfTests.run_bounded

    def test_pinned_apkovl_call_preserves_metadata_from_outer_session(self):
        with tempfile.TemporaryDirectory(prefix="hhpt-apkovl-") as temporary:
            root = Path(temporary)
            generator = root / "genapkovl-hhpt.sh"
            generator.write_text(
                "#!/bin/sh\nset -eu\n"
                "test \"$#\" -eq 1 && test \"$1\" = hhpt\n"
                "tar -cf inherited.tar marker\n", encoding="utf-8", newline="\n")
            generator.chmod(0o755)
            launcher = root / "fakeroot"
            shutil.copyfile(LAUNCHER, launcher)
            launcher.chmod(0o755)
            marker = root / "marker"
            marker.write_bytes(b"fixture metadata\n")
            original = marker.stat()
            environment = dict(os.environ, HHPT_APKOVL_SCRIPT=str(generator),
                               FAKEROOTDONTTRYCHOWN="1")
            for name in ("FAKEROOTKEY", "LD_PRELOAD"):
                environment.pop(name, None)
            environment["PATH"] = str(root) + os.pathsep + environment["PATH"]
            # This is pinned build_apkovl's invocation after it selects the
            # generator. Fake ownership is created before the nested call,
            # so a new database or cleared preload cannot satisfy the test.
            command = ('set -eu; cd "$1"; chown 12345:23456 marker; '
                       'DESTDIR=$PWD; _script=$HHPT_APKOVL_SCRIPT; _host=hhpt; '
                       '(cd "$DESTDIR"; fakeroot "$_script" "$_host")')
            self.run_bounded([shutil.which("fakeroot"), "--", "/bin/sh", "-c", command,
                              "fixture", str(root)], environment)
            with tarfile.open(root / "inherited.tar") as archive:
                member = archive.getmember("marker")
                self.assertEqual((member.uid, member.gid), (12345, 23456))
                self.assertEqual(archive.extractfile(member).read(), b"fixture metadata\n")
            actual = marker.stat()
            self.assertEqual((actual.st_uid, actual.st_gid), (original.st_uid, original.st_gid))


if __name__ == "__main__":
    unittest.main()
