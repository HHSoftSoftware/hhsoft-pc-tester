import os
from pathlib import Path
import shutil
import signal
import subprocess
import tempfile
import unittest


SHELL = shutil.which("sh")
if SHELL is None and os.name == "nt":
    candidate = Path("C:/Program Files/Git/usr/bin/sh.exe")
    if candidate.is_file():
        SHELL = str(candidate)

LAUNCHER = Path(__file__).with_name("scanelf-readonly.sh").resolve()
QUERIES = (("-BF", "#F%a %M %D %I"), ("-qF", "#F%i"),
           ("-qF", "#F%n"), ("-qF", "#F%r"))


def shell_path(path):
    value = path.resolve().as_posix()
    return "/" + value[0].lower() + value[2:] if os.name == "nt" else value


@unittest.skipUnless(SHELL, "scanelf launcher tests require a POSIX shell")
class ReadOnlyLauncherTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.scanner = self.root / "scanner"
        self.scanner.write_text(
            "#!/bin/sh\n"
            "test -z \"${LD_PRELOAD+x}\" && test -z \"${FAKEROOTKEY+x}\" || exit 90\n"
            "test \"$HHPT_FIXTURE_MARKER\" = retained || exit 91\n"
            "printf '%s\\n' called \"$@\"\n"
            "exit 23\n", encoding="utf-8", newline="\n")
        self.scanner.chmod(0o755)
        self.environment = dict(os.environ, HHPT_REAL_SCANELF=shell_path(self.scanner),
                                HHPT_FIXTURE_MARKER="retained", FAKEROOTKEY="fixture")
        self.environment["PATH"] = str(Path(SHELL).parent) + os.pathsep + self.environment.get("PATH", "")
        self.environment.pop("LD_PRELOAD", None)

    def invoke(self, arguments):
        # Set the fixture preload after starting sh so no nonexistent library is
        # loaded. The launcher must remove it before starting the fake scanner.
        return subprocess.run(
            [SHELL, "-c", 'LD_PRELOAD=fixture-preload; export LD_PRELOAD; . "$0"',
             shell_path(LAUNCHER), *arguments], env=self.environment,
            capture_output=True, text=True, timeout=5)

    def test_queries_preserve_arguments_exit_status_and_unrelated_environment(self):
        for query in QUERIES:
            for filename in ("fixture ELF", "-X"):
                with self.subTest(query=query, filename=filename):
                    result = self.invoke([*query, filename])
                    self.assertEqual(result.returncode, 23, result.stderr)
                    self.assertEqual(result.stdout.splitlines(), ["called", *query, "--", filename])
        self.assertEqual(self.environment["FAKEROOTKEY"], "fixture")

    def test_write_options_extra_inputs_and_unknown_queries_never_reach_scanner(self):
        invalid = ([], ["--help"], ["-X", "#F%i", "fixture"],
                   ["-qXF", "#F%i", "fixture"], ["-qF", "#F%O", "fixture"],
                   ["-BF", "#F%a %M %D %I", ""],
                   ["-qF", "#F%i", "fixture", "second"],
                   ["-qF", "#F%i", "-o", "output"],
                   ["-qF", "#F%i", "fixture", "--fix"])
        for arguments in invalid:
            with self.subTest(arguments=arguments):
                result = self.invoke(arguments)
                self.assertEqual(result.returncode, 2, result.stderr)
                self.assertEqual(result.stdout, "")

    def test_scanner_requires_an_explicit_absolute_executable(self):
        for executable in ("", "scanelf"):
            with self.subTest(executable=executable):
                self.environment["HHPT_REAL_SCANELF"] = executable
                self.assertEqual(self.invoke(["-qF", "#F%i", "fixture"]).returncode, 2)


@unittest.skipUnless(os.name == "posix" and all(shutil.which(tool) for tool in
                    ("fakeroot", "scanelf", "lddtree")) and Path("/bin/busybox").is_file(),
                    "Native fakeroot/scanelf regression requires Alpine builder tools")
class NativeScanelfTests(unittest.TestCase):
    def run_bounded(self, arguments, environment, timeout=10):
        process = subprocess.Popen(arguments, env=environment, stdout=subprocess.PIPE,
                                   stderr=subprocess.PIPE, start_new_session=True)
        try:
            stdout, stderr = process.communicate(timeout=timeout)
        except subprocess.TimeoutExpired:
            # Limit failure cleanup to this fixture's new process group; never
            # target another fakeroot session or the existing image build.
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.communicate(timeout=2)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.communicate(timeout=2)
            self.fail("native scanelf fixture exceeded its timeout")
        self.assertEqual(process.returncode, 0, stderr.decode(errors="replace"))
        return stdout

    def test_real_queries_and_lddtree_complete_without_losing_fakeroot_state(self):
        with tempfile.TemporaryDirectory(prefix="hhpt-scanelf-") as temporary:
            root = Path(temporary)
            launcher = root / "scanelf"
            shutil.copyfile(LAUNCHER, launcher)
            launcher.chmod(0o755)
            real_scanner = shutil.which("scanelf")
            fakeroot = shutil.which("fakeroot")
            lddtree = shutil.which("lddtree")
            environment = dict(os.environ, HHPT_REAL_SCANELF=real_scanner)
            for name in ("LD_PRELOAD", "FAKEROOTKEY"):
                environment.pop(name, None)
            for query in QUERIES:
                with self.subTest(query=query):
                    expected = self.run_bounded([real_scanner, *query, "--", "/bin/busybox"], environment)
                    actual = self.run_bounded([fakeroot, "--", str(launcher), *query, "/bin/busybox"], environment)
                    self.assertEqual(actual, expected)
            expected = self.run_bounded([lddtree, "-l", "--no-auto-root", "/bin/busybox"], environment)
            environment["PATH"] = str(root) + os.pathsep + environment["PATH"]
            # The parent retains its fakeroot session after the scanner child
            # exits. No session identifier or environment values are printed.
            command = ('before_key=$FAKEROOTKEY; before_preload=$LD_PRELOAD; '
                       'test -n "$before_key" && test -n "$before_preload" || exit 90; '
                       '"$1" -l --no-auto-root /bin/busybox || exit; '
                       'test "$FAKEROOTKEY" = "$before_key" && '
                       'test "$LD_PRELOAD" = "$before_preload"')
            actual = self.run_bounded([fakeroot, "--", "/bin/sh", "-c", command, "fixture", lddtree],
                                      environment, timeout=15)
            self.assertEqual(actual, expected)
            self.assertIn(b"/bin/busybox\n", actual)
            self.assertGreater(len(actual.splitlines()), 1)


if __name__ == "__main__":
    unittest.main()
