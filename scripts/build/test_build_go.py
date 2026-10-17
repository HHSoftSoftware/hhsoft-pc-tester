"""Requirements tests for the public Go build entrypoint using inert tools."""

import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


SOURCE = Path(__file__).with_name("build-go.sh")
PIN = "1.27.2"
GO = r'''#!/usr/bin/python3 -IB
import json
import os
from pathlib import Path
import sys
fixture = Path(__file__).resolve().parents[1]
with (fixture / "go-calls.jsonl").open("a") as stream:
    stream.write(json.dumps({"arguments": sys.argv[1:], "cwd": os.getcwd(),
                            "environment": dict(os.environ)}) + "\n")
if sys.argv[1:] == ["env", "GOVERSION"]:
    print("go" + (fixture / "compiler-version").read_text().strip())
    sys.exit(31 if (fixture / "fail-version-query").exists() else 0)
elif sys.argv[1] == "test":
    sys.exit(19 if (fixture / "fail-tests").exists() else 0)
elif sys.argv[1] == "build":
    target = Path(sys.argv[sys.argv.index("-o") + 1])
    target.write_text(Path("source.marker").read_text())
else:
    sys.exit(97)
'''

SCANNER = r'''import json
import os
from pathlib import Path
import sys
fixture = Path(__file__).resolve().parents[3]
(fixture / "scanner.json").write_text(json.dumps({"cwd": os.getcwd(), "isolated": sys.flags.isolated,
                                                "no_bytecode": sys.dont_write_bytecode,
                                                "environment": dict(os.environ)}))
sys.exit(23 if (fixture / "fail-scanner").exists() else 0)
'''


@unittest.skipUnless(sys.platform.startswith("linux") and Path("/usr/bin/python3").is_file(),
                     "Go wrapper fixtures require native Linux and /usr/bin/python3")
class ControlledGoBuildTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="hhpt-go-environment-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.repository = self.root / "repository"
        self.script = self.repository / "scripts/build/build-go.sh"
        self.script.parent.mkdir(parents=True)
        self.script.write_text(SOURCE.read_text())
        scanner = self.repository / "scripts/security/scan_source.py"
        scanner.parent.mkdir(parents=True)
        scanner.write_text(SCANNER)
        (self.repository / "VERSION").write_text("1.2.3-alpha.1\n")
        (self.repository / ".go-version").write_text(PIN + "\n")
        (self.repository / "source.marker").write_text("intended reviewed source\n")
        (self.root / "compiler-version").write_text(PIN + "\n")
        self.tools = self.root / "tools"
        self.tools.mkdir()
        compiler = self.tools / "go"
        compiler.write_text(GO)
        compiler.chmod(0o755)
        self.foreign = self.root / "foreign-working-directory"
        self.foreign.mkdir()
        (self.foreign / "VERSION").write_text("9.9.9-dev.0\n")
        (self.foreign / "source.marker").write_text("unreviewed foreign source\n")

    def invoke(self, target="x86_64", environment=None, output=None):
        env = {"PATH": str(self.tools) + ":/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL": "C"}
        env.update(environment or {})
        arguments = ["/bin/sh", str(self.script), target]
        if output is not None:
            arguments.append(str(output))
        return subprocess.run(arguments, cwd=self.foreign, env=env, capture_output=True,
                              text=True, timeout=10)

    def calls(self):
        path = self.root / "go-calls.jsonl"
        return [json.loads(line) for line in path.read_text().splitlines()] if path.exists() else []

    def test_all_five_targets_compile_the_reviewed_source_with_fixed_cpu_baselines(self):
        targets = (("x86_64", "linux", "amd64", "sse2", "linux_x86_64/hhpt"),
                   ("x86", "linux", "386", "sse2", "linux_x86/hhpt"),
                   ("legacy-x86", "linux", "386", "softfloat", "linux_legacy-x86/hhpt"),
                   ("aarch64", "linux", "arm64", "sse2", "linux_aarch64/hhpt"),
                   ("windows-x86_64", "windows", "amd64", "sse2", "windows_x86_64/hhpt.exe"))
        for target, operating_system, architecture, go386, artifact in targets:
            with self.subTest(target=target):
                self.setUp()
                result = self.invoke(target)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                calls = self.calls()
                self.assertEqual([call["arguments"][0] for call in calls], ["env", "test", "build"])
                self.assertTrue(all(call["cwd"] == str(self.repository) for call in calls))
                build = calls[-1]
                self.assertEqual(build["environment"]["GOOS"], operating_system)
                self.assertEqual(build["environment"]["GOARCH"], architecture)
                self.assertEqual(build["environment"]["GO386"], go386)
                self.assertEqual(build["environment"]["GOAMD64"], "v1")
                self.assertEqual(build["environment"]["GOARM64"], "v8.0")
                self.assertIn("-buildid= -X github.com/HHSoftSoftware/hhsoft-pc-tester/internal/product.Version=1.2.3-alpha.1",
                              build["arguments"])
                output = self.repository / "build/bin" / artifact
                self.assertEqual(output.read_text(), "intended reviewed source\n")
                self.assertTrue(Path(str(output) + ".sha256").is_file())
                checksum = Path(str(output) + ".sha256").read_text()
                self.assertNotIn(str(self.root), checksum)
                self.assertRegex(checksum, r"^[0-9a-f]{64}  hhpt(?:\.exe)?\n$")
                self.assertFalse((self.foreign / "build").exists())

    def test_ambient_go_compiler_workspace_python_and_user_settings_are_discarded(self):
        poison = self.root / "unreviewed"
        poison.mkdir()
        marker = self.root / "python-startup-ran"
        (poison / "sitecustomize.py").write_text(
            "from pathlib import Path\nPath(" + repr(str(marker)) + ").write_text('unexpected')\n")
        variables = ("GOFLAGS", "GOENV", "GOWORK", "GOTOOLCHAIN", "GOCACHEPROG", "GOROOT", "GCCGO",
                     "GOEXPERIMENT", "GOPROXY", "GOSUMDB", "GOVCS", "GOAUTH", "GOPRIVATE", "GONOPROXY",
                     "GONOSUMDB", "GOOS", "GOARCH", "GOAMD64", "GO386", "GOARM64", "CGO_ENABLED",
                     "CC", "CXX", "CGO_CFLAGS", "CGO_LDFLAGS", "GOCACHE", "GOMODCACHE", "GOPATH",
                     "HOME", "TMPDIR", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "PYTHONHOME", "PYTHONPATH",
                     "BASH_ENV", "ENV", "MAKEFLAGS", "MAKEFILES", "GIT_CONFIG_COUNT", "GIT_CONFIG_GLOBAL")
        environment = dict.fromkeys(variables, str(poison))
        result = self.invoke(environment=environment)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertFalse(marker.exists())
        scanner = json.loads((self.root / "scanner.json").read_text())
        self.assertEqual(scanner["cwd"], str(self.repository))
        self.assertEqual(scanner["isolated"], 1)
        self.assertTrue(scanner["no_bytecode"])
        for call in self.calls():
            actual = call["environment"]
            for name, value in environment.items():
                self.assertNotEqual(actual.get(name), value, name)
            self.assertEqual(actual["PATH"], "/usr/sbin:/usr/bin:/sbin:/bin")
            for name, value in {"GOENV": "off", "GOWORK": "off", "GOTOOLCHAIN": "local",
                                "GOPROXY": "off", "GOSUMDB": "off", "GOVCS": "*:off",
                                "GOAUTH": "off", "CGO_ENABLED": "0"}.items():
                self.assertEqual(actual[name], value)
            if call["arguments"][0] in {"test", "build"}:
                for name in ("HOME", "TMPDIR", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "GOCACHE", "GOMODCACHE", "GOPATH"):
                    path = Path(actual[name])
                    self.assertTrue(path.is_relative_to(self.repository / "build"), name)
                    self.assertTrue(path.is_dir(), name)

    def test_wrong_installed_compiler_fails_before_tests_build_and_output(self):
        (self.root / "compiler-version").write_text("1.26.9\n")
        result = self.invoke(environment={"GOTOOLCHAIN": "go" + PIN + "+auto"})
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual([call["arguments"] for call in self.calls()], [["env", "GOVERSION"]])
        self.assertFalse((self.repository / "build").exists())
        self.assertIn("downloads are disabled", result.stderr)

    def test_source_scan_and_test_failures_prevent_artifact_creation(self):
        for failure in ("fail-scanner", "fail-tests"):
            with self.subTest(failure=failure):
                self.setUp()
                (self.root / failure).touch()
                result = self.invoke()
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse((self.repository / "build/bin").exists())
                self.assertNotIn("build", [call["arguments"][0] for call in self.calls()])

    def test_failed_version_query_cannot_pass_by_printing_the_matching_version(self):
        (self.root / "fail-version-query").touch()
        result = self.invoke()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual([call["arguments"] for call in self.calls()], [["env", "GOVERSION"]])
        self.assertFalse((self.repository / "build").exists())

    def test_repeated_invocations_use_fresh_caches_and_preserve_prior_work(self):
        first = self.invoke()
        self.assertEqual(first.returncode, 0, first.stdout + first.stderr)
        old_cache = Path(self.calls()[-1]["environment"]["GOCACHE"])
        sentinel = old_cache / "preserved-fixture"
        sentinel.write_text("prior cache data\n")
        second = self.invoke()
        self.assertEqual(second.returncode, 0, second.stdout + second.stderr)
        new_cache = Path(self.calls()[-1]["environment"]["GOCACHE"])
        self.assertNotEqual(old_cache, new_cache)
        self.assertEqual(sentinel.read_text(), "prior cache data\n")
        self.assertFalse((new_cache / sentinel.name).exists())

    def test_explicit_absolute_output_is_preserved(self):
        output = self.root / "requested-output"
        result = self.invoke(output=output)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual((output / "linux_x86_64/hhpt").read_text(), "intended reviewed source\n")

    def test_crlf_version_files_are_supported_without_accepting_embedded_lines(self):
        (self.repository / "VERSION").write_bytes(b"1.2.3-alpha.1\r\n")
        (self.repository / ".go-version").write_bytes((PIN + "\r\n").encode())
        result = self.invoke()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.setUp()
        (self.repository / "VERSION").write_bytes(b"1.2.3-alpha.1\n9.9.9-dev.0\n")
        result = self.invoke()
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("build", [call["arguments"][0] for call in self.calls()])


if __name__ == "__main__":
    unittest.main()
