"""Run the product supervisors with inert collectors in isolated file fixtures.

These tests exercise the actual generated Alpine script and legacy init script.
Only device/runtime paths and hhpt collectors are replaced; no VM, mount, network,
physical device, signing key, or real report is involved.
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

QUICK_TESTS = {
    "audio.readiness.quick": "audio", "cpu.quick": "cpu",
    "display.operator.quick": "display", "graphics.quick": "graphics",
    "input.readiness.quick": "input", "memory.quick": "memory",
    "network.readiness.quick": "network", "storage.readiness.quick": "storage",
    "usb.readiness.quick": "usb",
}
LEGACY_UNSUPPORTED = {"graphics.quick", "display.operator.quick"}

STUB = "TEST_WORKERS = " + repr(QUICK_TESTS) + "\n" + r'''import json
import os
from pathlib import Path
import stat
import sys

root = Path(__file__).resolve().parent.parent
configuration = json.loads((root / "configuration.json").read_text())
command, arguments = sys.argv[1], sys.argv[2:]
output = Path(arguments[arguments.index("-output") + 1]) if "-output" in arguments else None
with (root / "events").open("a") as events:
    events.write(command + (" " + output.stem if command == "safety" else "") + "\n")
if command in ("ui", "inventory", "plan", "finalize", "deployment-key"):
    (root / (command + "-arguments.json")).write_text(json.dumps(arguments))

def write(value):
    with output.open("x") as stream:
        json.dump(value, stream, indent=2)

if command == "ui":
    selection = Path(arguments[arguments.index("-selection-output") + 1])
    assert selection == root / "run/selection.json", "UI must use the fixed RAM selection path"
    if configuration.get("ui_failure"):
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
    assert arguments[0] == "load-media" and not configuration["legacy"]
    assert stat.S_IMODE((root / "run/selection.json").stat().st_mode) == 0o400
    (root / "run/deployment-key.json").write_text(json.dumps(
        {"epoch": "fixture-deployment", "key_id": "fixture-key-id"}, separators=(",", ":")))
    (root / "run/fingerprint.key").write_bytes(b"synthetic-deployment-fixture-only")
    (root / "run/fixture-components").write_text("block:fixture\n")
elif command == "inventory":
    write({"components": [{"type": "cpu"}, {"type": "memory"}],
           "system_fingerprint": {"value": "hmac-sha256:" + "a" * 64}})
elif command == "plan":
    selection_path = Path(arguments[arguments.index("-selection-file") + 1])
    assert selection_path == root / "run/selection.json"
    assert not selection_path.is_symlink() and selection_path.is_file()
    assert stat.S_IMODE(selection_path.stat().st_mode) == 0o400, "selection must be frozen before planning"
    selection = json.loads(selection_path.read_text())
    assert isinstance(selection, dict) and set(selection) == {"excluded_tests"}
    exclusions = selection["excluded_tests"]
    assert isinstance(exclusions, list) and all(isinstance(name, str) and name in TEST_WORKERS for name in exclusions)
    assert len(set(exclusions)) == len(exclusions)
    if configuration["legacy"]:
        assert not set(exclusions) & {"graphics.quick", "display.operator.quick"}
    tests = []
    for name in TEST_WORKERS:
        status = "scheduled"
        if configuration["legacy"] and name in ("graphics.quick", "display.operator.quick"):
            status = "unsupported"
        if configuration.get("absent") and name in ("network.readiness.quick", "storage.readiness.quick"):
            status = "not_present"
        status = configuration.get("plan_statuses", {}).get(name, status)
        if status == "scheduled" and name in exclusions:
            status = "excluded"
        tests.append({"id": name, "status": status})
    write({"id": "sha256:" + "b" * 64, "excluded_tests": exclusions, "tests": tests})
elif command == "safety":
    phase = output.stem.removeprefix("safety-")
    failure = configuration.get("phase") == phase
    kind = configuration.get("kind", "mount")
    result = {"status": "pass", "storage_observed": True, "network_observed": True,
              "writable_block_mounts": 0, "active_non_loop_interfaces": 0}
    if failure:
        result["status"] = "error" if kind == "unknown" else "fail"
        if kind == "mount": result["writable_block_mounts"] = 1
        elif kind == "network": result["active_non_loop_interfaces"] = 1
        elif kind == "unknown": result["storage_observed"] = False
    write(result)
    if failure and kind != "malformed-summary":
        sys.exit(1)
    print("writable_block_mounts=0 active_non_loop_interfaces=0" +
          (" writable_block_mounts=1" if failure else ""))
elif command == "finalize":
    pairs = [arguments[index + 1].split("=", 1) for index, value in enumerate(arguments) if value == "-outcome"]
    outcomes = dict(pairs)
    assert len(pairs) == len(outcomes) == 9 and set(outcomes) == set(TEST_WORKERS), "exactly nine unique outcomes required"
    frozen = json.loads(Path(arguments[arguments.index("-plan") + 1]).read_text())
    events = (root / "events").read_text().splitlines()
    for test in frozen["tests"]:
        worker = TEST_WORKERS[test["id"]]
        evidence = root / ("run/" + worker + ".json")
        outcome = outcomes[test["id"]]
        if test["status"] != "scheduled":
            assert outcome == test["status"], "non-scheduled outcome differs from frozen plan"
            assert worker not in events and not evidence.exists(), "non-scheduled worker executed"
        elif outcome not in ("error", "aborted"):
            assert worker in events and json.loads(evidence.read_text())["status"] == outcome
    (root / "final-outcomes.json").write_text(json.dumps(outcomes))
    output.write_bytes(b"synthetic bundle fixture; no signature or hardware data\n")
    print("HHPT_RUN_INTEGRITY status=pass final_components=2 added=0 removed=0 changed=0")
elif command == "save":
    pass
else:
    assert command in TEST_WORKERS.values(), "unexpected worker"
    frozen = json.loads((root / "run/plan.json").read_text())
    assert any(TEST_WORKERS[test["id"]] == command and test["status"] == "scheduled" for test in frozen["tests"])
    status = "error" if command == configuration.get("worker_error") else "pass"
    if command == configuration.get("worker_fail"):
        status = "fail"
    write({"status": status})
'''


class SupervisorFixtureSyntaxTests(unittest.TestCase):
    def test_embedded_collector_fixture_compiles(self):
        compile(STUB, "runtime-safety-collector-fixture", "exec")


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
        for name in ("eglinfo", "kmscube"):
            (root / "tools" / name).write_text("#!/bin/sh\nexit 99\n")
            (root / "tools" / name).chmod(0o700)
        if legacy:
            script = LEGACY.read_text()
        else:
            source = ALPINE.read_text()
            start = 'make_file root:root 0755 "$temporary_directory/etc/local.d/hhpt.start" <<EOF\n'
            body = source.split(start, 1)[1].split("\nEOF\n", 1)[0]
            # Expansion must remain data-only: runtime command substitutions in
            # this generated product script must be escaped in the outer heredoc.
            self.assertIsNone(re.search(r"(?<!\\)\$\(", body))
            generated = subprocess.run(["/bin/sh", "-c", "target_architecture=x86_64\ncat <<EOF\n" + body + "\nEOF\n"],
                                       capture_output=True, text=True, timeout=5, check=True)
            script = generated.stdout
        substitutions = {
            "/run/hhpt": str(root / "run"), "/usr/bin/hhpt": str(executable),
            "/dev/console": str(root / "console"), "/dev/tty0": str(root / "console"),
            "/dev/tty1": str(root / "console"), "/dev/urandom": str(root / "entropy"),
            "/etc/shadow": str(root / "shadow"), "/usr/bin/eglinfo": str(root / "tools/eglinfo"),
            "/usr/bin/kmscube": str(root / "tools/kmscube"),
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

    def test_default_selection_preserves_every_applicable_worker(self):
        for legacy in (False, True):
            with self.subTest(legacy=legacy):
                root, result, events, console = self.run_supervisor(legacy)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertEqual(json.loads((root / "run/selection.json").read_text()), {"excluded_tests": []})
                self.assertEqual(stat.S_IMODE((root / "run/selection.json").stat().st_mode), 0o400)
                outcomes = json.loads((root / "final-outcomes.json").read_text())
                self.assertEqual(set(outcomes), set(QUICK_TESTS))
                for name, worker in QUICK_TESTS.items():
                    applicable = not legacy or name not in LEGACY_UNSUPPORTED
                    self.assertEqual(events.count(worker), 1 if applicable else 0)
                    self.assertEqual(outcomes[name], "pass" if applicable else "unsupported")
                self.assertIn("HHPT_READY", console)

    def test_excluding_an_active_worker_does_not_abort_other_scheduled_workers(self):
        for legacy in (False, True):
            for excluded in ("cpu.quick", "memory.quick", "graphics.quick", "display.operator.quick"):
                if legacy and excluded in LEGACY_UNSUPPORTED:
                    continue
                with self.subTest(legacy=legacy, excluded=excluded):
                    root, result, events, console = self.run_supervisor(legacy, excluded_tests=[excluded])
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                    outcomes = json.loads((root / "final-outcomes.json").read_text())
                    for name, worker in QUICK_TESTS.items():
                        expected = "excluded" if name == excluded else "pass"
                        if legacy and name in LEGACY_UNSUPPORTED:
                            expected = "unsupported"
                        self.assertEqual(outcomes[name], expected)
                        self.assertEqual(events.count(worker), 1 if expected == "pass" else 0)
                    self.assertIn("HHPT_READY", console)

    def test_excluding_all_selectable_workers_retains_safety_and_all_outcomes(self):
        for legacy in (False, True):
            with self.subTest(legacy=legacy):
                exclusions = [name for name in QUICK_TESTS if not legacy or name not in LEGACY_UNSUPPORTED]
                root, result, events, console = self.run_supervisor(legacy, excluded_tests=exclusions)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertFalse(set(events) & set(QUICK_TESTS.values()), events)
                self.assertEqual([event for event in events if event.startswith("safety ")],
                                 ["safety safety-preflight", "safety safety-after-readiness", "safety safety-final"])
                outcomes = json.loads((root / "final-outcomes.json").read_text())
                self.assertEqual(outcomes, {name: "excluded" if name in exclusions else "unsupported" for name in QUICK_TESTS})
                self.assertIn("finalize", events)
                self.assertIn("save", events)
                self.assertIn("HHPT_READY", console)

    def test_prior_failure_preserves_exclusions_and_aborts_only_scheduled_active_work(self):
        scenarios = ((False, "cpu", ["memory.quick"]),
                     (False, "cpu", ["graphics.quick", "display.operator.quick"]),
                     (False, "memory", ["graphics.quick", "display.operator.quick"]),
                     (True, "cpu", ["memory.quick"]))
        for legacy, failed, exclusions in scenarios:
            with self.subTest(legacy=legacy, failed=failed, exclusions=exclusions):
                root, result, events, console = self.run_supervisor(legacy, worker_fail=failed, excluded_tests=exclusions)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                outcomes = json.loads((root / "final-outcomes.json").read_text())
                for name in ("cpu.quick", "memory.quick", "graphics.quick", "display.operator.quick"):
                    worker = QUICK_TESTS[name]
                    if legacy and name in LEGACY_UNSUPPORTED:
                        expected = "unsupported"
                    elif name in exclusions:
                        expected = "excluded"
                    elif worker == failed:
                        expected = "fail"
                    elif worker == "cpu":
                        expected = "pass"
                    else:
                        expected = "aborted"
                    self.assertEqual(outcomes[name], expected)
                    self.assertEqual(events.count(worker), 1 if expected in ("pass", "fail") else 0)
                self.assertIn("HHPT_READY", console)

    def test_exclusions_preserve_absent_and_missing_driver_reasons(self):
        unavailable = {"network.readiness.quick": "not_present", "storage.readiness.quick": "missing_driver"}
        for legacy in (False, True):
            with self.subTest(legacy=legacy):
                root, result, events, console = self.run_supervisor(
                    legacy, excluded_tests=list(unavailable), plan_statuses=unavailable)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                outcomes = json.loads((root / "final-outcomes.json").read_text())
                for name, reason in unavailable.items():
                    self.assertEqual(outcomes[name], reason)
                    self.assertNotIn(QUICK_TESTS[name], events)
                self.assertIn("HHPT_READY", console)

    def test_excluded_readiness_never_bypasses_global_safety_failures(self):
        for legacy in (False, True):
            for phase in ("preflight", "after-readiness", "final"):
                for kind in ("mount", "network", "unknown", "malformed-summary"):
                    with self.subTest(legacy=legacy, phase=phase, kind=kind):
                        root, result, events, console = self.run_supervisor(
                            legacy, excluded_tests=["network.readiness.quick", "storage.readiness.quick"],
                            phase=phase, kind=kind)
                        self.assertNotEqual(result.returncode, 0, result.stderr)
                        self.assertIn("HHPT_SAFETY_FAIL stage=" + phase, console)
                        self.assertNotIn("HHPT_READY", console)
                        self.assertNotIn("network", events)
                        self.assertNotIn("storage", events)
                        self.assertNotIn("save", events)
                        self.assertEqual("finalize" in events, phase == "final")
                        self.assertTrue((root / ("run/safety-" + phase + ".json")).is_file())

    def test_exclusions_retain_the_next_active_stage_safety_check(self):
        scenarios = ((False, "before-memory", "memory", ["cpu.quick"]),
                     (True, "before-memory", "memory", ["cpu.quick"]),
                     (False, "before-graphics", "graphics", ["cpu.quick", "memory.quick"]),
                     (False, "before-display", "display", ["cpu.quick", "memory.quick", "graphics.quick"]))
        for legacy, phase, blocked, exclusions in scenarios:
            with self.subTest(legacy=legacy, phase=phase):
                root, result, events, console = self.run_supervisor(
                    legacy, excluded_tests=exclusions, phase=phase, kind="network")
                self.assertNotEqual(result.returncode, 0, result.stderr)
                self.assertIn("safety safety-" + phase, events)
                self.assertNotIn(blocked, events)
                self.assertNotIn("finalize", events)
                self.assertNotIn("HHPT_READY", console)

    def test_missing_unsafe_or_malformed_selection_stops_before_workers(self):
        for legacy in (False, True):
            for kind in ("missing", "symlink", "directory", "fifo", "malformed", "existing"):
                with self.subTest(legacy=legacy, kind=kind):
                    root, result, events, console = self.run_supervisor(legacy, selection_kind=kind)
                    self.assertNotEqual(result.returncode, 0, result.stderr)
                    self.assertFalse(set(events) & (set(QUICK_TESTS.values()) | {"finalize", "save"}), events)
                    self.assertNotIn("HHPT_READY", console)
                    if kind == "malformed":
                        self.assertIn("HHPT_FAILED test-plan", console)
                    elif kind == "existing":
                        self.assertIn("HHPT_FAILED user-interface", console)
                        self.assertEqual((root / "run/selection.json").read_text(), "existing selection must not be replaced\n")
                    else:
                        self.assertIn("HHPT_FAILED freeze-test-selection", console)
                        self.assertNotIn("inventory", events)
                        self.assertNotIn("plan", events)
                    if kind == "symlink":
                        self.assertEqual(stat.S_IMODE((root / "selection-target.json").stat().st_mode), 0o600)

    def test_selection_output_failure_stops_before_inventory_or_workers(self):
        for legacy in (False, True):
            with self.subTest(legacy=legacy):
                root, result, events, console = self.run_supervisor(legacy, ui_failure=True)
                self.assertNotEqual(result.returncode, 0, result.stderr)
                self.assertEqual(events, ["ui"])
                self.assertIn("HHPT_FAILED user-interface", console)
                self.assertFalse((root / "run/selection.json").exists())

    def test_stable_key_start_preserves_default_and_custom_selection(self):
        for exclusions in ([], ["cpu.quick", "network.readiness.quick"]):
            with self.subTest(exclusions=exclusions):
                root, result, events, console = self.run_supervisor(False, deployment_key=True, excluded_tests=exclusions)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertEqual(events.count("deployment-key"), 1)
                self.assertEqual(json.loads((root / "run/selection.json").read_text()), {"excluded_tests": exclusions})
                for command in ("inventory", "plan", "finalize"):
                    arguments = json.loads((root / (command + "-arguments.json")).read_text())
                    self.assertEqual(arguments[arguments.index("-key-epoch") + 1], "fixture-deployment")
                    self.assertIn("-fixture-components=" + str(root / "run/fixture-components"), arguments)
                outcomes = json.loads((root / "final-outcomes.json").read_text())
                for name in exclusions:
                    self.assertEqual(outcomes[name], "excluded")
                    self.assertNotIn(QUICK_TESTS[name], events)
                self.assertIn("fingerprint_mode=deployment", console)
                self.assertIn("HHPT_READY", console)

    def test_preflight_blocks_unsafe_unknown_and_malformed_observations_before_active_work(self):
        for legacy in (False, True):
            for kind in ("mount", "network", "unknown", "malformed-summary"):
                with self.subTest(legacy=legacy, kind=kind):
                    root, result, events, console = self.run_supervisor(legacy, phase="preflight", kind=kind, absent=True)
                    self.assertNotEqual(result.returncode, 0, result.stderr)
                    self.assertIn("HHPT_SAFETY_FAIL", console)
                    self.assertNotIn("HHPT_READY", console)
                    self.assertFalse(set(events) & {"cpu", "memory", "graphics", "display", "finalize", "save"}, events)
                    self.assertTrue((root / "run/safety-preflight.json").is_file())

    def test_new_unsafe_state_prevents_the_next_active_stage(self):
        for legacy, phase, previous, blocked in ((False, "before-memory", "cpu", "memory"),
                                                (True, "before-memory", "cpu", "memory"),
                                                (False, "before-graphics", "memory", "graphics"),
                                                (False, "before-display", "graphics", "display")):
            with self.subTest(legacy=legacy, phase=phase):
                root, result, events, console = self.run_supervisor(legacy, phase=phase, kind="network")
                self.assertNotEqual(result.returncode, 0, result.stderr)
                self.assertIn(previous, events)
                self.assertNotIn(blocked, events)
                self.assertNotIn("HHPT_READY", console)
                self.assertTrue((root / ("run/safety-" + phase + ".json")).is_file())

    def test_readiness_safety_error_cannot_be_forgotten_when_later_state_looks_safe(self):
        for legacy in (False, True):
            for worker in ("storage", "network"):
                with self.subTest(legacy=legacy, worker=worker):
                    root, result, events, console = self.run_supervisor(legacy, worker_error=worker)
                    self.assertNotEqual(result.returncode, 0, result.stderr)
                    self.assertIn("HHPT_SAFETY_FAIL stage=readiness", console)
                    self.assertNotIn("HHPT_READY", console)
                    self.assertFalse(set(events) & {"graphics", "display", "finalize", "save"}, events)
                    self.assertEqual(json.loads((root / ("run/" + worker + ".json")).read_text())["status"], "error")

    def test_final_observation_blocks_ready_and_save_while_retaining_completed_evidence(self):
        for legacy in (False, True):
            with self.subTest(legacy=legacy):
                root, result, events, console = self.run_supervisor(legacy, phase="final", kind="mount")
                self.assertNotEqual(result.returncode, 0, result.stderr)
                self.assertIn("finalize", events)
                self.assertNotIn("save", events)
                self.assertNotIn("HHPT_READY", console)
                self.assertTrue((root / "run/session.hhpcr").is_file())
                self.assertEqual(json.loads((root / "run/safety-final.json").read_text())["writable_block_mounts"], 1)

    def test_cpu_failure_preserves_frozen_absent_outcomes_and_aborts_only_scheduled_workers(self):
        absent = {"memory.quick": "not_present", "graphics.quick": "not_present",
                  "display.operator.quick": "not_present"}
        for legacy in (False, True):
            for plan_statuses in (absent, {}):
                with self.subTest(legacy=legacy, absent=bool(plan_statuses)):
                    root, result, events, console = self.run_supervisor(
                        legacy, worker_fail="cpu", plan_statuses=plan_statuses)
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                    self.assertIn("finalize", events)
                    self.assertIn("save", events)
                    self.assertFalse(set(events) & {"memory", "graphics", "display"}, events)
                    outcomes = json.loads((root / "final-outcomes.json").read_text())
                    self.assertEqual(outcomes["cpu.quick"], "fail")
                    self.assertEqual(json.loads((root / "run/cpu.json").read_text())["status"], "fail")
                    for name in absent:
                        expected = "not_present" if plan_statuses else "aborted"
                        if not plan_statuses and legacy and name != "memory.quick":
                            expected = "unsupported"
                        self.assertEqual(outcomes[name], expected, (name, outcomes))
                    self.assertNotIn("HHPT_FAILED report-finalization", console)
                    self.assertTrue((root / "run/session.hhpcr").is_file())

    def test_safe_diskless_profile_still_checks_global_safety_and_reports_observed_counts(self):
        for legacy in (False, True):
            with self.subTest(legacy=legacy):
                root, result, events, console = self.run_supervisor(legacy, absent=True)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertNotIn("storage", events)
                self.assertNotIn("network", events)
                self.assertIn("safety safety-preflight", events)
                self.assertIn("safety safety-final", events)
                self.assertIn("save", events)
                ready = [line for line in console.splitlines() if line.startswith("HHPT_READY ")]
                self.assertEqual(len(ready), 1)
                observed = json.loads((root / "run/safety-final.json").read_text())
                for field in ("writable_block_mounts", "active_non_loop_interfaces"):
                    self.assertIn(field + "=" + str(observed[field]), ready[0])


if __name__ == "__main__":
    unittest.main()
