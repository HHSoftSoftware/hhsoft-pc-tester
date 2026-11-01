"""Exercise real startup scripts with an inert quick/standard/extended session boundary.

The fixture checks arguments and files. It does not schedule workers, inspect
hardware, sign reports, or reproduce Go coordinator safety decisions. No VM,
mount, network, physical device, real signing key, or real report is involved.
"""

import json
from pathlib import Path
import re
import stat
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
ALPINE = ROOT / "platform/alpine/mkimage/genapkovl-hhpt.sh"
LEGACY = ROOT / "platform/buildroot/board/legacy-x86/rootfs-overlay/etc/init.d/S50hhpt"
SELECTABLE_IDS = (
    "audio.readiness.quick", "cpu.quick", "display.operator.quick", "graphics.quick",
    "input.keyboard.quick", "input.pointer.quick", "input.readiness.quick",
    "input.touchpad.quick", "input.touchscreen.quick", "inspection.exterior.quick",
    "memory.quick", "network.readiness.quick",
    "storage.readiness.quick", "usb.readiness.quick",
)
LEGACY_UNSUPPORTED = {"graphics.quick", "display.operator.quick"}
REPORT_FIXTURE = b"synthetic bundle fixture; no signature or hardware data\n"

STUB = "SELECTABLE_IDS = " + repr(SELECTABLE_IDS) + "\n" + r'''import json
import os
from pathlib import Path
import stat
import sys
import time

root = Path(__file__).resolve().parent.parent
run = root / "run"
configuration = json.loads((root / "configuration.json").read_text())
command, arguments = sys.argv[1], sys.argv[2:]
output = Path(arguments[arguments.index("-output") + 1]) if "-output" in arguments else None
with (root / "events").open("a") as events:
    events.write(command + (" " + output.stem if command == "safety" else "") + "\n")
(root / (command + "-arguments.json")).write_text(json.dumps(arguments))
profile = configuration.get("profile", "quick")
architecture = "legacy-x86" if configuration["legacy"] else configuration.get("architecture", "x86_64")
epoch = "fixture-deployment" if configuration.get("deployment_key") else "ephemeral-session"
common = ["-fingerprint-key", str(run / "fingerprint.key"), "-key-epoch", epoch]
if configuration.get("deployment_key"):
    common += ["-fixture-components=" + str(run / "fixture-components")]

def write(path, value):
    with path.open("x") as stream:
        json.dump(value, stream, indent=2)

def ready_file():
    ready = run / "session-ready.txt"
    content = "HHPT_READY fixture=inert-session-handoff\n"
    kind = configuration.get("ready_kind", "regular")
    if kind == "missing":
        return
    if kind == "directory":
        ready.mkdir()
    elif kind == "fifo":
        os.mkfifo(ready, 0o600)
    elif kind == "symlink":
        target = root / "ready-target.txt"
        target.write_text(content)
        ready.symlink_to(target)
    else:
        ready.write_text("" if kind == "empty" else content)

if command == "ui":
    expected = (["-legacy"] if configuration["legacy"] else []) + ["-auto-start", "15s"]
    if not configuration["legacy"]:
        expected += ["-deployment-key-media"]
    assert arguments == expected + ["-launch-selection-output", str(run / "launch-selection.json")]
    selection = run / "launch-selection.json"
    if configuration.get("ui_failure") or selection.exists() or selection.is_symlink():
        sys.exit(2)
    kind = configuration.get("selection_kind", "regular")
    if kind == "symlink":
        target = root / "selection-target.json"
        target.write_text('{"excluded_tests":[]}')
        target.chmod(0o600)
        selection.symlink_to(target)
    elif kind == "directory":
        selection.mkdir()
    elif kind == "fifo":
        os.mkfifo(selection, 0o600)
    elif kind != "missing":
        with selection.open("x") as stream:
            if kind == "malformed":
                stream.write('{"excluded_tests":')
            else:
                json.dump({"schema_version": configuration.get("document_version", 1),
                           "profile": configuration.get("document_profile", profile),
                           "architecture": configuration.get("document_architecture", architecture),
                           "excluded_tests": configuration.get("excluded_tests", [])}, stream)
    sys.exit(configuration.get("ui_status", 10 if configuration.get("deployment_key") else (12 if profile == "extended" else 11 if profile == "standard" else 0)))
elif command == "deployment-key":
    assert not configuration["legacy"]
    assert arguments == ["load-media", "-discovery-key", str(run / "media-discovery.key"),
                         "-audit-output", str(root / "console")]
    assert stat.S_IMODE((run / "launch-selection.json").stat().st_mode) == 0o400
    if configuration.get("deployment_failure"):
        sys.exit(1)
    (run / "deployment-key.json").write_text(json.dumps(
        {"epoch": "fixture-deployment", "key_id": "fixture-key-id"}, separators=(",", ":")))
    (run / "fingerprint.key").write_bytes(b"synthetic-deployment-fixture-only")
    (run / "fixture-components").write_text("block:fixture\n")
elif command == "inventory":
    assert arguments == common + ["-output", str(run / "inventory.json")]
    if configuration.get("inventory_failure"):
        sys.exit(1)
    components = [] if configuration.get("empty_inventory") else [{"type": "cpu"}, {"type": "memory"}]
    write(output, {"components": components,
                   "system_fingerprint": {"value": "hmac-sha256:" + "a" * 64}})
elif command == "plan":
    architecture = "legacy-x86" if configuration["legacy"] else configuration.get("architecture", "x86_64")
    assert arguments == common + ["-profile", profile, "-architecture", architecture,
                                 "-launch-selection-file", str(run / "launch-selection.json"), "-output", str(run / "plan.json")]
    selection = run / "launch-selection.json"
    assert not selection.is_symlink() and selection.is_file()
    assert stat.S_IMODE(selection.stat().st_mode) == 0o400, "selection must be frozen before planning"
    try:
        decoded = json.loads(selection.read_text())
    except ValueError:
        sys.exit(2)
    if not isinstance(decoded, dict) or set(decoded) != {"schema_version", "profile", "architecture", "excluded_tests"}:
        sys.exit(2)
    if type(decoded["schema_version"]) is not int or decoded["schema_version"] != 1 or decoded["profile"] != profile or decoded["architecture"] != architecture:
        sys.exit(2)
    exclusions = decoded["excluded_tests"]
    if profile in ("standard", "extended") and exclusions != []:
        sys.exit(2)
    if not isinstance(exclusions, list) or any(not isinstance(name, str) or name not in SELECTABLE_IDS for name in exclusions):
        sys.exit(2)
    if len(set(exclusions)) != len(exclusions):
        sys.exit(2)
    if configuration["legacy"] and set(exclusions) & {"graphics.quick", "display.operator.quick"}:
        sys.exit(2)
    if configuration.get("plan_failure"):
        sys.exit(2)
    # Deliberately not a production plan. Go tests own catalog validation and
    # scheduling; this marker proves the exact frozen file is handed on.
    write(output, {"id": "sha256:" + "b" * 64, "fixture": "inert handoff only",
                   "profile": profile, "architecture": architecture, "excluded_tests": sorted(exclusions), "tests": []})
    (root / "frozen-plan-bytes").write_bytes(output.read_bytes())
elif command == "safety":
    phase = output.stem.removeprefix("safety-")
    assert phase in ("preflight", "final"), "worker safety orchestration belongs to the Go session"
    assert arguments == ["-output", str(run / ("safety-" + phase + ".json")), "-summary"]
    failure = configuration.get("phase") == phase
    kind = configuration.get("kind", "mount")
    result = {"status": "pass", "storage_observed": True, "network_observed": True,
              "writable_block_mounts": 0, "active_non_loop_interfaces": 0}
    if failure:
        result["status"] = "error" if kind == "unknown" else "fail"
        if kind == "mount": result["writable_block_mounts"] = 1
        elif kind == "network": result["active_non_loop_interfaces"] = 1
        elif kind == "unknown": result["storage_observed"] = False
    write(output, result)
    if failure and kind != "malformed-summary":
        sys.exit(1)
    print("writable_block_mounts=0 active_non_loop_interfaces=0" +
          (" writable_block_mounts=1" if failure else ""))
elif command == "session":
    assert arguments == common + ["-profile", profile, "-plan", str(run / "plan.json"), "-evidence-dir", str(run),
                                 "-timeout", {"quick": "5m", "standard": "15m", "extended": "45m"}[profile], "-output", str(run / "session.hhpcr")]
    frozen = run / "plan.json"
    assert frozen.is_file() and not frozen.is_symlink()
    assert stat.S_IMODE(frozen.stat().st_mode) == 0o400, "plan must be frozen before session entry"
    assert frozen.read_bytes() == (root / "frozen-plan-bytes").read_bytes(), "frozen plan changed in transit"
    assert json.loads(frozen.read_text())["excluded_tests"] == sorted(configuration.get("excluded_tests", []))
    assert json.loads(frozen.read_text())["profile"] == profile
    assert json.loads(frozen.read_text())["architecture"] == architecture
    assert (root / "events").read_text().splitlines()[-3:] == ["safety safety-preflight", "plan", "session"]
    exit_code = configuration.get("session_exit", 0)
    write(run / "session-diagnostic.json", {"fixture": "session-boundary", "exit": exit_code})
    if exit_code == 0 or configuration.get("failure_artifacts"):
        output.write_bytes(b"synthetic bundle fixture; no signature or hardware data\n")
        ready_file()
    if configuration.get("outer_timeout"):
        # Stall only this inert process. The one-second outer guard must kill
        # it; the finite fallback prevents an orphan if that guard regresses.
        time.sleep(5)
    sys.exit(exit_code)
elif command == "save":
    assert arguments == ["-fingerprint-key", str(run / "fingerprint.key"), "-audit-output", str(root / "console"),
                         str(run / "session.hhpcr")]
    assert (run / "session.hhpcr").read_bytes() == b"synthetic bundle fixture; no signature or hardware data\n"
    assert (root / "events").read_text().splitlines()[-3:] == ["session", "safety safety-final", "save"]
    assert "HHPT_READY fixture=inert-session-handoff" in (root / "console").read_text()
    sys.exit(1 if configuration.get("save_failure") else 0)
else:
    raise AssertionError("startup script invoked an unexpected command: " + command)
'''


TIMEOUT_STUB = r'''import json
import os
from pathlib import Path
import sys
root = Path(__file__).resolve().parent.parent
configuration = json.loads((root / "configuration.json").read_text())
arguments = sys.argv[1:]
assert len(arguments) >= 5 and arguments[:2] == ["-s", "KILL"]
assert arguments[3] == str(root / "tools/hhpt")
command = arguments[4]
expected = {"quick": "350", "standard": "950", "extended": "2750"}[configuration.get("profile", "quick")]
assert arguments[2] == (expected if command == "session" else "5")
assert command in ("safety", "session")
effective = list(arguments)
if command == "session" and configuration.get("outer_timeout"):
    effective[2] = "1"
with (root / "timeout-arguments.jsonl").open("a") as events:
    events.write(json.dumps({"requested": arguments, "effective": effective}) + "\n")
os.execv("/usr/bin/timeout", ["/usr/bin/timeout"] + effective)
'''


class SupervisorFixtureSyntaxTests(unittest.TestCase):
    def test_embedded_session_fixture_compiles(self):
        compile(STUB, "runtime-session-boundary-fixture", "exec")
        compile(TIMEOUT_STUB, "runtime-timeout-argument-fixture", "exec")


@unittest.skipUnless(sys.platform.startswith("linux") and Path("/bin/sh").is_file(),
                     "supervisor execution fixtures require a native POSIX shell")
class RuntimeSafetySupervisorTests(unittest.TestCase):
    def run_supervisor(self, legacy, **configuration):
        temporary = tempfile.TemporaryDirectory(prefix="hhpt-runtime-safety-")
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        (root / "tools").mkdir()
        (root / "console").touch()
        (root / "entropy").write_bytes(b"synthetic-not-key-material-00000000")
        (root / "shadow").write_text("root:fixture:0:0:0:0:0:0:0\n")
        (root / "configuration.json").write_text(json.dumps({"legacy": legacy, **configuration}))
        if configuration.get("selection_kind") == "existing":
            (root / "run").mkdir()
            (root / "run/launch-selection.json").write_text("existing selection must not be replaced\n")
        executable = root / "tools/hhpt"
        executable.write_text("#!" + sys.executable + "\n" + STUB)
        executable.chmod(0o700)
        timeout = root / "tools/timeout"
        timeout.write_text("#!" + sys.executable + "\n" + TIMEOUT_STUB)
        timeout.chmod(0o700)
        if legacy:
            script = LEGACY.read_text()
        else:
            source = ALPINE.read_text()
            start = 'make_file root:root 0755 "$temporary_directory/etc/local.d/hhpt.start" <<EOF\n'
            body = source.split(start, 1)[1].split("\nEOF\n", 1)[0]
            # Runtime substitutions must remain data during outer expansion.
            self.assertIsNone(re.search(r"(?<!\\)\$\(", body))
            architecture = configuration.get("architecture", "x86_64")
            self.assertIn(architecture, ("x86_64", "x86", "aarch64"))
            generated = subprocess.run(["/bin/sh", "-c", "target_architecture=" + architecture + "\ncat <<EOF\n" + body + "\nEOF\n"],
                                       capture_output=True, text=True, timeout=5, check=True)
            script = generated.stdout
        substitutions = {
            "/run/hhpt": str(root / "run"), "/usr/bin/hhpt": str(executable),
            "/usr/bin/timeout": str(timeout),
            "/dev/console": str(root / "console"), "/dev/tty0": str(root / "console"),
            "/dev/tty1": str(root / "console"), "/dev/urandom": str(root / "entropy"),
            "/etc/shadow": str(root / "shadow"),
        }
        for source, destination in substitutions.items():
            script = script.replace(source, destination)
        # A regular fixture file stands in for the append-like console device.
        script = script.replace("> " + str(root / "console"), ">> " + str(root / "console"))
        script = script.replace('>"$ui_console"', '>>"$ui_console"')
        script = script.replace("while :; do sleep 3600; done", "exit 0")
        path = root / "supervisor.sh"
        path.write_text(script)
        result = subprocess.run(["/bin/sh", str(path), "start"], cwd=root,
                                env={"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL": "C"},
                                capture_output=True, text=True, timeout=15)
        events = (root / "events").read_text().splitlines()
        return root, result, events, (root / "console").read_text()

    @staticmethod
    def expected_launch(legacy, exclusions=None, profile="quick", architecture="x86_64"):
        return {"schema_version": 1, "profile": profile,
                "architecture": "legacy-x86" if legacy else architecture,
                "excluded_tests": [] if exclusions is None else exclusions}

    def assert_ready(self, root, result, events, console, save_failed=False):
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr + console)
        expected = ["ui"] + (["deployment-key"] if "deployment-key" in events else [])
        self.assertEqual(events, expected + ["inventory", "safety safety-preflight", "plan", "session", "safety safety-final", "save"])
        self.assertEqual(console.count("HHPT_READY fixture=inert-session-handoff"), 1)
        self.assertEqual("HHPT_SAVE_FAILED" in console, save_failed)
        self.assertEqual((root / "run/session.hhpcr").read_bytes(), REPORT_FIXTURE)
        for name in ("plan.json", "launch-selection.json"):
            self.assertEqual(stat.S_IMODE((root / "run" / name).stat().st_mode), 0o400)
        self.assertEqual((root / "run/plan.json").read_bytes(), (root / "frozen-plan-bytes").read_bytes())
        profile = json.loads((root / "configuration.json").read_text()).get("profile", "quick")
        timing = [json.loads(line) for line in (root / "timeout-arguments.jsonl").read_text().splitlines()]
        self.assertEqual([(row["requested"][2], row["requested"][4]) for row in timing],
                         [("5", "safety"), ({"quick": "350", "standard": "950", "extended": "2750"}[profile], "session"), ("5", "safety")])
        self.assertTrue(all(row["requested"] == row["effective"] for row in timing))
        self.assertIn("mode=safe-" + profile, console)
        self.assertIn("profile=" + profile, console)

    def test_default_selection_hands_frozen_plan_to_one_session(self):
        for legacy, architecture in ((False, "x86_64"), (False, "x86"), (False, "aarch64"), (True, "legacy-x86")):
            with self.subTest(legacy=legacy, architecture=architecture):
                root, result, events, console = self.run_supervisor(legacy, architecture=architecture)
                self.assert_ready(root, result, events, console)
                self.assertEqual(json.loads((root / "run/launch-selection.json").read_text()), self.expected_launch(legacy, architecture=architecture))
                self.assertEqual(json.loads((root / "run/plan.json").read_text())["excluded_tests"], [])
                self.assertIn("architecture=" + architecture, console)

    def test_custom_and_all_exclusions_survive_the_session_handoff(self):
        for legacy in (False, True):
            selectable = [name for name in SELECTABLE_IDS if not legacy or name not in LEGACY_UNSUPPORTED]
            self.assertEqual(len(selectable), 12 if legacy else 14)
            self.assertTrue({"input.touchpad.quick", "input.touchscreen.quick"} <= set(selectable))
            for exclusions in (["memory.quick", "cpu.quick"], ["network.readiness.quick", "storage.readiness.quick"], selectable):
                with self.subTest(legacy=legacy, exclusions=exclusions):
                    root, result, events, console = self.run_supervisor(legacy, excluded_tests=exclusions)
                    self.assert_ready(root, result, events, console)
                    self.assertEqual(json.loads((root / "run/launch-selection.json").read_text()), self.expected_launch(legacy, exclusions))
                    self.assertEqual(json.loads((root / "run/plan.json").read_text())["excluded_tests"], sorted(exclusions))

    def test_deployment_start_preserves_identity_and_selection_arguments(self):
        for exclusions in ([], ["cpu.quick", "network.readiness.quick"]):
            with self.subTest(exclusions=exclusions):
                root, result, events, console = self.run_supervisor(False, deployment_key=True, excluded_tests=exclusions)
                self.assert_ready(root, result, events, console)
                for command in ("inventory", "plan", "session"):
                    arguments = json.loads((root / (command + "-arguments.json")).read_text())
                    self.assertEqual(arguments[arguments.index("-key-epoch") + 1], "fixture-deployment")
                    self.assertIn("-fixture-components=" + str(root / "run/fixture-components"), arguments)
                self.assertFalse((root / "run/media-discovery.key").exists())
                self.assertIn("fingerprint_mode=deployment", console)

    def test_deployment_failure_or_legacy_request_stops_before_inventory(self):
        for legacy in (False, True):
            with self.subTest(legacy=legacy):
                _, result, events, console = self.run_supervisor(legacy, deployment_key=True, deployment_failure=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(events, ["ui"] if legacy else ["ui", "deployment-key"])
                self.assertNotIn("HHPT_READY", console)

    def test_missing_unsafe_or_malformed_selection_stops_before_session(self):
        for legacy in (False, True):
            for kind in ("missing", "symlink", "directory", "fifo", "malformed", "existing"):
                with self.subTest(legacy=legacy, kind=kind):
                    root, result, events, console = self.run_supervisor(legacy, selection_kind=kind)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertNotIn("session", events)
                    self.assertNotIn("save", events)
                    self.assertNotIn("HHPT_READY", console)
                    if kind == "malformed":
                        self.assertEqual(events, ["ui", "inventory", "safety safety-preflight", "plan"])
                        self.assertIn("HHPT_FAILED test-plan", console)
                    elif kind == "existing":
                        self.assertEqual(events, ["ui"])
                        self.assertIn("HHPT_FAILED user-interface", console)
                        self.assertEqual((root / "run/launch-selection.json").read_text(), "existing selection must not be replaced\n")
                    else:
                        self.assertEqual(events, ["ui"])
                        self.assertIn("HHPT_FAILED freeze-test-selection", console)
                    if kind == "symlink":
                        self.assertEqual(stat.S_IMODE((root / "selection-target.json").stat().st_mode), 0o600)

    def test_planner_rejection_never_falls_back_to_default_selection(self):
        for legacy in (False, True):
            selections = [["unknown.quick"], ["cpu.quick", "cpu.quick"], None]
            if legacy:
                selections += [["graphics.quick"], ["display.operator.quick"]]
            for exclusions in selections:
                with self.subTest(legacy=legacy, exclusions=exclusions):
                    _, result, events, console = self.run_supervisor(legacy, excluded_tests=exclusions)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertEqual(events, ["ui", "inventory", "safety safety-preflight", "plan"])
                    self.assertIn("HHPT_FAILED test-plan", console)
                    self.assertNotIn("HHPT_READY", console)

    def test_setup_failures_stop_before_session(self):
        cases = (("ui_failure", ["ui"], "user-interface"),
                 ("inventory_failure", ["ui", "inventory"], "inventory"),
                 ("empty_inventory", ["ui", "inventory"], "empty-inventory"),
                 ("plan_failure", ["ui", "inventory", "safety safety-preflight", "plan"], "test-plan"))
        for legacy in (False, True):
            for failure, expected, diagnostic in cases:
                with self.subTest(legacy=legacy, failure=failure):
                    _, result, events, console = self.run_supervisor(legacy, **{failure: True})
                    self.assertNotEqual(result.returncode, 0)
                    self.assertEqual(events, expected)
                    self.assertIn(diagnostic, console)
                    self.assertNotIn("HHPT_READY", console)

    def test_nonzero_session_retains_diagnostics_and_prevents_final_check_ready_or_save(self):
        for legacy in (False, True):
            for exit_code, artifacts in ((1, False), (2, False), (12, False), (130, False), (12, True)):
                with self.subTest(legacy=legacy, exit_code=exit_code, artifacts=artifacts):
                    root, result, events, console = self.run_supervisor(
                        legacy, session_exit=exit_code, failure_artifacts=artifacts)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertEqual(events, ["ui", "inventory", "safety safety-preflight", "plan", "session"])
                    self.assertIn("HHPT_FAILED quick-session exit=" + str(exit_code), console)
                    self.assertNotIn("HHPT_READY", console)
                    self.assertEqual(json.loads((root / "run/session-diagnostic.json").read_text())["exit"], exit_code)
                    for name in ("inventory.json", "launch-selection.json", "plan.json", "safety-preflight.json"):
                        self.assertTrue((root / "run" / name).is_file(), name)
                    self.assertEqual((root / "run/session.hhpcr").exists(), artifacts)
                    self.assertEqual((root / "run/session-ready.txt").exists(), artifacts)
                    self.assertFalse((root / "run/safety-final.json").exists())

    def test_missing_unsafe_or_empty_ready_metadata_prevents_ready_and_save(self):
        for legacy in (False, True):
            for kind in ("missing", "symlink", "empty", "directory", "fifo"):
                with self.subTest(legacy=legacy, kind=kind):
                    root, result, events, console = self.run_supervisor(legacy, ready_kind=kind)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertEqual(events, ["ui", "inventory", "safety safety-preflight", "plan", "session", "safety safety-final"])
                    self.assertIn("HHPT_FAILED session-ready-metadata", console)
                    self.assertNotIn("HHPT_READY", console)
                    self.assertEqual((root / "run/session.hhpcr").read_bytes(), REPORT_FIXTURE)

    def test_terminal_watchdog_blocks_ready_and_save_even_with_existing_artifacts(self):
        for legacy in (False, True):
            with self.subTest(legacy=legacy):
                root, result, events, console = self.run_supervisor(legacy, outer_timeout=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(events, ["ui", "inventory", "safety safety-preflight", "plan", "session"])
                self.assertIn("HHPT_FAILED quick-session", console)
                self.assertNotIn("HHPT_READY", console)
                self.assertTrue((root / "run/session-diagnostic.json").is_file())
                self.assertEqual((root / "run/session.hhpcr").read_bytes(), REPORT_FIXTURE)
                self.assertIn("HHPT_READY", (root / "run/session-ready.txt").read_text())
                self.assertFalse((root / "run/safety-final.json").exists())

    def test_independent_shell_safety_failures_block_ready_and_save(self):
        for legacy in (False, True):
            for phase in ("preflight", "final"):
                for kind in ("mount", "network", "unknown", "malformed-summary"):
                    with self.subTest(legacy=legacy, phase=phase, kind=kind):
                        root, result, events, console = self.run_supervisor(
                            legacy, phase=phase, kind=kind,
                            excluded_tests=["network.readiness.quick", "storage.readiness.quick"])
                        self.assertNotEqual(result.returncode, 0)
                        expected = ["ui", "inventory", "safety safety-preflight"]
                        if phase == "final":
                            expected += ["plan", "session", "safety safety-final"]
                        self.assertEqual(events, expected)
                        self.assertIn("HHPT_SAFETY_FAIL stage=" + phase, console)
                        self.assertNotIn("HHPT_READY", console)
                        self.assertTrue((root / ("run/safety-" + phase + ".json")).is_file())
                        self.assertEqual((root / "run/session.hhpcr").exists(), phase == "final")
                        self.assertEqual((root / "run/session-ready.txt").exists(), phase == "final")
                        if phase == "final":
                            self.assertEqual((root / "run/session.hhpcr").read_bytes(), REPORT_FIXTURE)

    def test_standard_handoff_uses_matching_profile_and_real_timeout_arguments(self):
        for legacy, architecture in ((False, "x86_64"), (False, "x86"), (False, "aarch64"), (True, "legacy-x86")):
            with self.subTest(legacy=legacy, architecture=architecture):
                root, result, events, console = self.run_supervisor(legacy, profile="standard", architecture=architecture)
                self.assert_ready(root, result, events, console)
                self.assertEqual(json.loads((root / "run/launch-selection.json").read_text()), self.expected_launch(legacy, profile="standard", architecture=architecture))
                for command in ("plan", "session"):
                    arguments = json.loads((root / (command + "-arguments.json")).read_text())
                    self.assertEqual(arguments[arguments.index("-profile") + 1], "standard")
                self.assertNotIn("deployment-key", events)

    def test_unknown_status_never_reaches_inventory_or_session(self):
        for legacy in (False, True):
            for status in (2, 13, 99, 255):
                with self.subTest(legacy=legacy, status=status):
                    _, result, events, console = self.run_supervisor(legacy, ui_status=status)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertEqual(events, ["ui"])
                    self.assertIn("HHPT_FAILED user-interface", console)
                    self.assertNotIn("HHPT_READY", console)

    def test_profile_architecture_or_standard_exclusion_mismatch_stops_before_session(self):
        cases = ({"document_profile": "standard"},
                 {"profile": "standard", "document_profile": "quick"},
                 {"profile": "standard", "document_architecture": "not-the-image"},
                 {"profile": "standard", "document_version": 2},
                 {"profile": "standard", "excluded_tests": ["cpu.quick"]})
        for legacy in (False, True):
            for configuration in cases:
                with self.subTest(legacy=legacy, configuration=configuration):
                    _, result, events, console = self.run_supervisor(legacy, **configuration)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertEqual(events, ["ui", "inventory", "safety safety-preflight", "plan"])
                    self.assertIn("HHPT_FAILED test-plan", console)
                    self.assertNotIn("HHPT_READY", console)

    def test_standard_failure_paths_prevent_ready_and_save(self):
        cases = ({"selection_kind": "malformed"}, {"session_exit": 12},
                 {"phase": "final", "kind": "mount"}, {"ready_kind": "missing"})
        for legacy in (False, True):
            for configuration in cases:
                with self.subTest(legacy=legacy, configuration=configuration):
                    _, result, events, console = self.run_supervisor(legacy, profile="standard", **configuration)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertNotIn("save", events)
                    self.assertNotIn("HHPT_READY", console)
                    if "session_exit" in configuration:
                        self.assertIn("HHPT_FAILED standard-session exit=12", console)

    def test_standard_terminal_watchdog_preserves_diagnostics_without_ready(self):
        for legacy in (False, True):
            with self.subTest(legacy=legacy):
                root, result, events, console = self.run_supervisor(legacy, profile="standard", outer_timeout=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(events, ["ui", "inventory", "safety safety-preflight", "plan", "session"])
                self.assertIn("HHPT_FAILED standard-session", console)
                self.assertNotIn("HHPT_READY", console)
                self.assertTrue((root / "run/session-diagnostic.json").is_file())
                self.assertEqual((root / "run/session.hhpcr").read_bytes(), REPORT_FIXTURE)
                calls = [json.loads(line) for line in (root / "timeout-arguments.jsonl").read_text().splitlines()]
                self.assertEqual(calls[-1]["requested"][2], "950")
                self.assertEqual(calls[-1]["effective"][2], "1")


    def test_extended_handoff_preserves_ephemeral_identity_and_exact_bounds(self):
        for legacy, architecture in ((False, "x86_64"), (False, "x86"), (False, "aarch64"), (True, "legacy-x86")):
            with self.subTest(legacy=legacy, architecture=architecture):
                root, result, events, console = self.run_supervisor(legacy, profile="extended", architecture=architecture)
                self.assert_ready(root, result, events, console)
                self.assertEqual(json.loads((root / "run/launch-selection.json").read_text()), self.expected_launch(legacy, profile="extended", architecture=architecture))
                for command in ("inventory", "plan", "session"):
                    arguments = json.loads((root / (command + "-arguments.json")).read_text())
                    self.assertEqual(arguments[arguments.index("-key-epoch") + 1], "ephemeral-session")
                    self.assertFalse(any(value.startswith("-fixture-components=") for value in arguments))
                    if command != "inventory":
                        self.assertEqual(arguments[arguments.index("-profile") + 1], "extended")
                self.assertNotIn("deployment-key", events)
                self.assertFalse((root / "run/media-discovery.key").exists())
                arguments = json.loads((root / "session-arguments.json").read_text())
                self.assertEqual(arguments[arguments.index("-timeout") + 1], "45m")
                calls = [json.loads(line) for line in (root / "timeout-arguments.jsonl").read_text().splitlines()]
                self.assertEqual(calls[1]["requested"][2], "2750")

    def test_extended_mismatched_or_excluded_launch_never_enters_session(self):
        cases = ({"document_profile": "quick"}, {"document_profile": "standard"},
                 {"document_architecture": "wrong"}, {"excluded_tests": ["memory.quick"]})
        for legacy in (False, True):
            for configuration in cases:
                with self.subTest(legacy=legacy, configuration=configuration):
                    _, result, events, console = self.run_supervisor(legacy, profile="extended", **configuration)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertEqual(events, ["ui", "inventory", "safety safety-preflight", "plan"])
                    self.assertNotIn("HHPT_READY", console)

    def test_extended_failures_never_publish_ready_or_save(self):
        cases = ({"selection_kind": "malformed"}, {"session_exit": 12},
                 {"phase": "final", "kind": "mount"}, {"ready_kind": "missing"})
        for legacy in (False, True):
            for configuration in cases:
                with self.subTest(legacy=legacy, configuration=configuration):
                    _, result, events, console = self.run_supervisor(legacy, profile="extended", **configuration)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertNotIn("save", events)
                    self.assertNotIn("HHPT_READY", console)
                    if "session_exit" in configuration:
                        self.assertIn("HHPT_FAILED extended-session exit=12", console)

    def test_extended_outer_policy_preserves_diagnostics_without_claiming_cleanup(self):
        for legacy in (False, True):
            with self.subTest(legacy=legacy):
                root, result, events, console = self.run_supervisor(legacy, profile="extended", outer_timeout=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(events, ["ui", "inventory", "safety safety-preflight", "plan", "session"])
                self.assertIn("HHPT_FAILED extended-session", console)
                self.assertIn("cleanup=unconfirmed", console)
                self.assertNotIn("HHPT_READY", console)
                self.assertTrue((root / "run/session-diagnostic.json").is_file())
                self.assertEqual((root / "run/session.hhpcr").read_bytes(), REPORT_FIXTURE)
                calls = [json.loads(line) for line in (root / "timeout-arguments.jsonl").read_text().splitlines()]
                self.assertEqual(calls[-1]["requested"][2], "2750")
                self.assertEqual(calls[-1]["effective"][2], "1")

    def test_save_failure_preserves_completed_report(self):
        for legacy in (False, True):
            with self.subTest(legacy=legacy):
                root, result, events, console = self.run_supervisor(legacy, save_failure=True)
                self.assert_ready(root, result, events, console, save_failed=True)
                self.assertIn("HHPT_SAVE_FAILED report-retained-in-ram", console)


if __name__ == "__main__":
    unittest.main()
