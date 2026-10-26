"""Exercise real startup scripts with an inert quick-session boundary.

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
    "input.pointer.quick", "input.readiness.quick", "memory.quick", "network.readiness.quick",
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
    assert arguments == expected + ["-selection-output", str(run / "selection.json")]
    selection = run / "selection.json"
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
                json.dump({"excluded_tests": configuration.get("excluded_tests", [])}, stream)
    sys.exit(10 if configuration.get("deployment_key") else 0)
elif command == "deployment-key":
    assert not configuration["legacy"]
    assert arguments == ["load-media", "-discovery-key", str(run / "media-discovery.key"),
                         "-audit-output", str(root / "console")]
    assert stat.S_IMODE((run / "selection.json").stat().st_mode) == 0o400
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
    assert arguments == common + ["-profile", "quick", "-architecture", architecture,
                                 "-selection-file", str(run / "selection.json"), "-output", str(run / "plan.json")]
    selection = run / "selection.json"
    assert not selection.is_symlink() and selection.is_file()
    assert stat.S_IMODE(selection.stat().st_mode) == 0o400, "selection must be frozen before planning"
    try:
        decoded = json.loads(selection.read_text())
    except ValueError:
        sys.exit(2)
    exclusions = decoded["excluded_tests"]
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
                   "excluded_tests": sorted(exclusions), "tests": []})
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
    assert arguments == common + ["-plan", str(run / "plan.json"), "-evidence-dir", str(run),
                                 "-timeout", "5m", "-output", str(run / "session.hhpcr")]
    frozen = run / "plan.json"
    assert frozen.is_file() and not frozen.is_symlink()
    assert stat.S_IMODE(frozen.stat().st_mode) == 0o400, "plan must be frozen before session entry"
    assert frozen.read_bytes() == (root / "frozen-plan-bytes").read_bytes(), "frozen plan changed in transit"
    assert json.loads(frozen.read_text())["excluded_tests"] == sorted(configuration.get("excluded_tests", []))
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


class SupervisorFixtureSyntaxTests(unittest.TestCase):
    def test_embedded_session_fixture_compiles(self):
        compile(STUB, "runtime-session-boundary-fixture", "exec")


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
            (root / "run/selection.json").write_text("existing selection must not be replaced\n")
        executable = root / "tools/hhpt"
        executable.write_text("#!" + sys.executable + "\n" + STUB)
        executable.chmod(0o700)
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
        if configuration.get("outer_timeout"):
            guard = "/usr/bin/timeout -s KILL 350 /usr/bin/hhpt session "
            self.assertEqual(script.count(guard), 1, "expected one terminal session watchdog")
            script = script.replace(guard, "/usr/bin/timeout -s KILL 1 /usr/bin/hhpt session ")
        substitutions = {
            "/run/hhpt": str(root / "run"), "/usr/bin/hhpt": str(executable),
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

    def assert_ready(self, root, result, events, console, save_failed=False):
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr + console)
        expected = ["ui"] + (["deployment-key"] if "deployment-key" in events else [])
        self.assertEqual(events, expected + ["inventory", "safety safety-preflight", "plan", "session", "safety safety-final", "save"])
        self.assertEqual(console.count("HHPT_READY fixture=inert-session-handoff"), 1)
        self.assertEqual("HHPT_SAVE_FAILED" in console, save_failed)
        self.assertEqual((root / "run/session.hhpcr").read_bytes(), REPORT_FIXTURE)
        for name in ("plan.json", "selection.json"):
            self.assertEqual(stat.S_IMODE((root / "run" / name).stat().st_mode), 0o400)
        self.assertEqual((root / "run/plan.json").read_bytes(), (root / "frozen-plan-bytes").read_bytes())

    def test_default_selection_hands_frozen_plan_to_one_session(self):
        for legacy, architecture in ((False, "x86_64"), (False, "x86"), (False, "aarch64"), (True, "legacy-x86")):
            with self.subTest(legacy=legacy, architecture=architecture):
                root, result, events, console = self.run_supervisor(legacy, architecture=architecture)
                self.assert_ready(root, result, events, console)
                self.assertEqual(json.loads((root / "run/selection.json").read_text()), {"excluded_tests": []})
                self.assertEqual(json.loads((root / "run/plan.json").read_text())["excluded_tests"], [])
                self.assertIn("architecture=" + architecture, console)

    def test_custom_and_all_exclusions_survive_the_session_handoff(self):
        for legacy in (False, True):
            selectable = [name for name in SELECTABLE_IDS if not legacy or name not in LEGACY_UNSUPPORTED]
            for exclusions in (["memory.quick", "cpu.quick"], ["network.readiness.quick", "storage.readiness.quick"], selectable):
                with self.subTest(legacy=legacy, exclusions=exclusions):
                    root, result, events, console = self.run_supervisor(legacy, excluded_tests=exclusions)
                    self.assert_ready(root, result, events, console)
                    self.assertEqual(json.loads((root / "run/selection.json").read_text()), {"excluded_tests": exclusions})
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
                        self.assertEqual((root / "run/selection.json").read_text(), "existing selection must not be replaced\n")
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
                    for name in ("inventory.json", "selection.json", "plan.json", "safety-preflight.json"):
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

    def test_save_failure_preserves_completed_report(self):
        for legacy in (False, True):
            with self.subTest(legacy=legacy):
                root, result, events, console = self.run_supervisor(legacy, save_failure=True)
                self.assert_ready(root, result, events, console, save_failed=True)
                self.assertIn("HHPT_SAVE_FAILED report-retained-in-ram", console)


if __name__ == "__main__":
    unittest.main()
