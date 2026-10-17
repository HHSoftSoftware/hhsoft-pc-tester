"""Run the product supervisors with inert collectors in isolated file fixtures.

These tests exercise the actual generated Alpine script and legacy init script.
Only device/runtime paths and hhpt collectors are replaced; no VM, mount, network,
physical device, signing key, or real report is involved.
"""

import json
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
ALPINE = ROOT / "platform/alpine/mkimage/genapkovl-hhpt.sh"
LEGACY = ROOT / "platform/buildroot/board/legacy-x86/rootfs-overlay/etc/init.d/S50hhpt"

STUB = r'''import json
from pathlib import Path
import sys

root = Path(__file__).resolve().parent.parent
configuration = json.loads((root / "configuration.json").read_text())
command, arguments = sys.argv[1], sys.argv[2:]
output = Path(arguments[arguments.index("-output") + 1]) if "-output" in arguments else None
with (root / "events").open("a") as events:
    events.write(command + (" " + output.stem if command == "safety" else "") + "\n")

def write(value):
    with output.open("x") as stream:
        json.dump(value, stream, indent=2)

if command == "ui":
    pass
elif command == "inventory":
    write({"components": [{"type": "cpu"}, {"type": "memory"}],
           "system_fingerprint": {"value": "hmac-sha256:" + "a" * 64}})
elif command == "plan":
    tests = []
    for name in ("cpu.quick", "memory.quick", "audio.readiness.quick", "input.readiness.quick",
                 "network.readiness.quick", "storage.readiness.quick", "usb.readiness.quick",
                 "graphics.quick", "display.operator.quick"):
        status = "scheduled"
        if configuration["legacy"] and name in ("graphics.quick", "display.operator.quick"):
            status = "unsupported"
        if configuration.get("absent") and name in ("network.readiness.quick", "storage.readiness.quick"):
            status = "not_present"
        status = configuration.get("plan_statuses", {}).get(name, status)
        tests.append({"id": name, "status": status})
    write({"id": "sha256:" + "b" * 64, "tests": tests})
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
    outcomes = dict(arguments[index + 1].split("=", 1) for index, value in enumerate(arguments) if value == "-outcome")
    (root / "final-outcomes.json").write_text(json.dumps(outcomes))
    output.write_bytes(b"synthetic bundle fixture; no signature or hardware data\n")
    print("HHPT_RUN_INTEGRITY status=pass final_components=2 added=0 removed=0 changed=0")
elif command == "save":
    pass
else:
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
