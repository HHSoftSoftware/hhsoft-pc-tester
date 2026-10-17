"""Requirements fixtures use disposable files, never installed firmware."""

import copy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

SCRIPT = Path(__file__).with_name("filter_alpine_firmware.py")
SPEC = importlib.util.spec_from_file_location("firmware_filter", SCRIPT)
filtering = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(filtering)
NATIVE = sys.platform.startswith("linux") and hasattr(os, "O_PATH")


def content(data):
    return {"sha256": hashlib.sha256(data).hexdigest(), "size": len(data)}


def entry(path, data, target=None):
    value = {"path": path, "source": content(b"original-" + data), "stored": content(data),
             "rootfs_kind": "symlink" if target else "file", "rootfs_mode": 0o777 if target else 0o644,
             "modloop_architectures": [] if path == "z-last.bin" else ["aarch64", "x86", "x86_64"]}
    if target:
        value["link_target"] = target
    return value


def fixture_policy():
    return {"schema": 1, "source_archive": {"sha256": "1" * 64}, "entries": [
        entry("adaptec/rx.bin", b"reviewed-rx"),
        entry("adaptec/tx.bin", b"reviewed-rx", "rx.bin.zst"),
        entry("z-last.bin", b"reviewed-last"),
    ]}


class PolicyTests(unittest.TestCase):
    def test_exact_public_policy_has_no_conditional_grant_overreach(self):
        raw = SCRIPT.parents[2].joinpath("platform/alpine/firmware-exclusions.json").read_bytes()
        self.assertEqual(hashlib.sha256(raw).hexdigest(), filtering.POLICY_SHA256)
        policy = json.loads(raw)
        entries = filtering.validate_policy(policy)
        self.assertEqual(len(entries), 41)
        self.assertEqual(sum("unknown_redistribution_evidence" in e["reason"] for e in entries), 22)
        self.assertEqual(sum("claimed_copyleft_without_visible_source" in e["reason"] for e in entries), 19)
        self.assertFalse(any(e["path"].startswith(("keyspan/", "emi26/", "cxgb3/", "inside-secure/")) for e in entries))
        links = [e for e in entries if e["rootfs_kind"] == "symlink"]
        self.assertEqual([(e["path"], e["link_target"]) for e in links], [("adaptec/starfire_tx.bin", "starfire_rx.bin.zst")])
        self.assertEqual({arch: sum(arch in e["modloop_architectures"] for e in entries) for arch in filtering.ARCHES},
                         {"x86_64": 29, "x86": 29, "aarch64": 26})
        expected_absent_on_arm = {"ess/maestro3_assp_kernel.fw", "ess/maestro3_assp_minisrc.fw", "ositech/Xilinx7OD.bin"}
        self.assertEqual({e["path"] for e in entries if "x86_64" in e["modloop_architectures"] and "aarch64" not in e["modloop_architectures"]},
                         expected_absent_on_arm)

    def test_paths_duplicates_and_unknown_encoding_are_not_policy_inputs(self):
        for path in ("../outside", "/outside", "a//b", "a/../b", "a\\b", "a\nb", "a.bin.zst"):
            with self.subTest(path=path):
                policy = fixture_policy()
                policy["entries"][0]["path"] = path
                with self.assertRaises(filtering.Rejected):
                    filtering.validate_policy(policy)
        policy = fixture_policy()
        policy["entries"].append(copy.deepcopy(policy["entries"][-1]))
        with self.assertRaises(filtering.Rejected):
            filtering.validate_policy(policy)

    def test_architecture_policy_must_be_known_unique_and_sorted(self):
        for values in (["unknown"], ["x86", "x86"], ["x86_64", "x86"], "x86", [1]):
            with self.subTest(values=values):
                policy = fixture_policy()
                policy["entries"][0]["modloop_architectures"] = values
                with self.assertRaises(filtering.Rejected):
                    filtering.validate_policy(policy)

    def test_symlink_resolution_uses_component_order_and_rejects_escape(self):
        rows = {"d": {"kind": "directory"}, "d/link": {"kind": "symlink", "target": "../file"},
                "file": {"kind": "file"}}
        self.assertEqual(filtering.resolve(rows, "d/link")[0], "file")
        rows["d/link"]["target"] = "../../outside"
        with self.assertRaises(filtering.Rejected):
            filtering.resolve(rows, "d/link")
        rows["d/link"]["target"] = "../file/../d"
        with self.assertRaises(filtering.Rejected):
            filtering.resolve(rows, "d/link")

    def test_cli_cannot_replace_policy_or_echo_rejected_paths(self):
        result = subprocess.run([sys.executable, "-I", "-B", str(SCRIPT), "--policy", "private-fixture-marker"],
                                capture_output=True, timeout=10, check=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn(b"private-fixture-marker", result.stdout + result.stderr)
        self.assertNotIn(b"Traceback", result.stderr)


@unittest.skipUnless(NATIVE, "Linux descriptor and disposable filesystem fixtures")
class FilesystemTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="hhpt-firmware-fixture-")
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        self.work = self.base / "tmp"
        self.session = self.work / "update-kernel.Abc123"
        self.root = self.session / "root"
        self.mod = self.session / "modloop"
        self.root_fw = self.root / "usr/lib/firmware"
        self.mod_fw = self.mod / "modules/firmware"
        for folder in (self.root_fw, self.mod_fw):
            folder.mkdir(parents=True, mode=0o755)
        (self.root / "lib").symlink_to("usr/lib")
        self.receipt = self.base / "receipt.json"
        self.policy = fixture_policy()
        for row in self.policy["entries"]:
            data = b"reviewed-last" if row["path"] == "z-last.bin" else b"reviewed-rx"
            root_path = self.root_fw / (row["path"] + ".zst")
            root_path.parent.mkdir(parents=True, exist_ok=True)
            if row["rootfs_kind"] == "symlink":
                root_path.symlink_to(row["link_target"])
            else:
                root_path.write_bytes(data)
                root_path.chmod(0o644)
            if row["path"] != "z-last.bin":
                mod_path = self.mod_fw / (row["path"] + ".zst")
                mod_path.parent.mkdir(parents=True, exist_ok=True)
                mod_path.write_bytes(data)
                mod_path.chmod(0o755)
        for folder in (self.root_fw, self.mod_fw):
            (folder / "retained.bin").write_bytes(b"permitted unrelated firmware")

    def run_filter(self):
        return filtering.apply_filter(str(self.work), str(self.root), str(self.mod), "x86_64", str(self.receipt),
                                      self.policy, "2" * 64, "3" * 64)

    def require_no_removal(self):
        for row in self.policy["entries"]:
            self.assertTrue(os.path.lexists(self.root_fw / (row["path"] + ".zst")))
        self.assertTrue((self.mod_fw / "adaptec/rx.bin.zst").exists())
        self.assertFalse(self.receipt.exists())

    def test_both_output_trees_filtered_including_root_only_firmware(self):
        result = self.run_filter()
        self.assertEqual(result["counts"], {"rootfs": 3, "modloop": 2})
        self.assertEqual(json.loads(self.receipt.read_text()), result)
        self.assertNotIn(str(self.base), self.receipt.read_text())
        for folder in (self.root_fw, self.mod_fw):
            self.assertEqual((folder / "retained.bin").read_bytes(), b"permitted unrelated firmware")
            self.assertEqual(sorted(p.relative_to(folder).as_posix() for p in folder.rglob("*") if p.is_file()), ["retained.bin"])

    def test_final_bad_entry_causes_zero_earlier_removals(self):
        (self.root_fw / "z-last.bin.zst").write_bytes(b"changed bytes")
        with self.assertRaises(filtering.Rejected):
            self.run_filter()
        self.require_no_removal()

    def test_missing_root_member_is_not_hidden_by_modloop_presence(self):
        (self.root_fw / "z-last.bin.zst").unlink()
        with self.assertRaises(filtering.Rejected):
            self.run_filter()
        self.assertTrue((self.root_fw / "adaptec/rx.bin.zst").exists())
        self.assertFalse(self.receipt.exists())

    def test_missing_architecture_selected_modloop_file_blocks_before_removal(self):
        (self.mod_fw / "adaptec/tx.bin.zst").unlink()
        with self.assertRaises(filtering.Rejected):
            self.run_filter()
        self.require_no_removal()

    def test_extra_architecture_unselected_modloop_file_blocks_before_removal(self):
        extra = self.mod_fw / "z-last.bin.zst"
        extra.write_bytes(b"reviewed-last")
        extra.chmod(0o755)
        with self.assertRaises(filtering.Rejected):
            self.run_filter()
        self.require_no_removal()
        self.assertEqual(extra.read_bytes(), b"reviewed-last")

    def test_unreviewed_encoding_or_copy_cannot_survive_under_an_alias(self):
        for name, data in [("adaptec/rx.bin.xz", b"new encoding"), ("unexpected.bin", b"reviewed-rx"),
                           ("source-alias.bin", b"original-reviewed-rx")]:
            with self.subTest(name=name):
                path = self.mod_fw / name
                path.write_bytes(data)
                with self.assertRaises(filtering.Rejected):
                    self.run_filter()
                self.require_no_removal()
                path.unlink()

    def test_escaping_link_and_forbidden_target_alias_are_rejected(self):
        for target in ("../../../outside", "adaptec/rx.bin.zst"):
            with self.subTest(target=target):
                alias = self.mod_fw / "alias"
                alias.symlink_to(target)
                with self.assertRaises(filtering.Rejected):
                    self.run_filter()
                self.require_no_removal()
                alias.unlink()

    def test_hardlinked_or_wrong_mode_target_is_rejected(self):
        target = self.root_fw / "z-last.bin.zst"
        os.link(target, self.base / "external-hardlink")
        with self.assertRaises(filtering.Rejected):
            self.run_filter()
        self.require_no_removal()
        (self.base / "external-hardlink").unlink()
        target.chmod(0o600)
        with self.assertRaises(filtering.Rejected):
            self.run_filter()
        self.require_no_removal()

    def test_fifo_is_classified_before_any_data_open(self):
        os.mkfifo(self.mod_fw / "untrusted-fifo")
        original = os.open
        def guarded(path, flags, *args, **kwargs):
            if path == "untrusted-fifo":
                self.assertTrue(flags & os.O_PATH)
            return original(path, flags, *args, **kwargs)
        with mock.patch.object(filtering.os, "open", side_effect=guarded):
            with self.assertRaises(filtering.Rejected):
                self.run_filter()
        self.require_no_removal()

    def test_fifo_without_writer_does_not_block_in_a_child_process(self):
        os.mkfifo(self.mod_fw / "writerless-fifo")
        runner = """import json, runpy, sys
namespace = runpy.run_path(sys.argv[1])
try:
    namespace['apply_filter'](*sys.argv[2:7], json.loads(sys.argv[7]), '2' * 64, '3' * 64)
except namespace['Rejected']:
    raise SystemExit(12)
raise SystemExit(13)
"""
        result = subprocess.run([sys.executable, "-I", "-B", "-c", runner, str(SCRIPT), str(self.work),
                                 str(self.root), str(self.mod), "x86_64", str(self.receipt), json.dumps(self.policy)],
                                capture_output=True, timeout=10, check=False)
        self.assertEqual(result.returncode, 12, "writerless special file must fail without an I/O wait")
        self.require_no_removal()

    def test_hash_budget_failure_is_before_mutation(self):
        with mock.patch.object(filtering, "MAX_TOTAL", 1):
            with self.assertRaises(filtering.Rejected):
                self.run_filter()
        self.require_no_removal()

    def test_group_writable_parent_or_replaced_lib_layout_is_not_authorized(self):
        self.work.chmod(0o770)
        with self.assertRaises(filtering.Rejected):
            self.run_filter()
        self.require_no_removal()
        self.work.chmod(0o700)
        (self.root / "lib").unlink()
        (self.root / "lib").symlink_to(str(self.root / "usr/lib"))
        with self.assertRaises(filtering.Rejected):
            self.run_filter()
        self.require_no_removal()

    def test_path_replacement_before_commit_is_detected_before_unlink(self):
        original = filtering.validate_snapshot
        changed = False
        def replace(fd, rows):
            nonlocal changed
            if not changed:
                changed = True
                target = self.root_fw / "z-last.bin.zst"
                target.rename(self.root_fw / "preserved-original")
                target.write_bytes(b"reviewed-last")
            original(fd, rows)
        with mock.patch.object(filtering, "validate_snapshot", side_effect=replace):
            with self.assertRaises(filtering.Rejected):
                self.run_filter()
        self.require_no_removal()

    def test_rebinding_firmware_directory_cannot_filter_a_detached_tree(self):
        original = filtering.validate_snapshot
        changed = False
        def replace(fd, rows):
            nonlocal changed
            if not changed:
                changed = True
                self.root_fw.rename(self.root_fw.with_name("preserved-firmware"))
                self.root_fw.mkdir()
            original(fd, rows)
        with mock.patch.object(filtering, "validate_snapshot", side_effect=replace):
            with self.assertRaises(filtering.Rejected):
                self.run_filter()
        self.assertTrue((self.root_fw.with_name("preserved-firmware") / "adaptec/rx.bin.zst").exists())
        self.assertTrue((self.mod_fw / "adaptec/rx.bin.zst").exists())
        self.assertFalse(self.receipt.exists())

    def test_existing_receipt_and_wrong_staging_layout_block_mutation(self):
        self.receipt.write_bytes(b"preserved")
        with self.assertRaises(filtering.Rejected):
            self.run_filter()
        self.assertEqual(self.receipt.read_bytes(), b"preserved")
        self.receipt.unlink()
        with self.assertRaises(filtering.Rejected):
            filtering.apply_filter(str(self.base), str(self.root), str(self.mod), "x86_64", str(self.receipt),
                                   self.policy, "2" * 64, "3" * 64)
        self.require_no_removal()

    def test_fixed_policy_reader_matches_committed_bytes(self):
        policy, policy_hash, helper_hash = filtering.load_policy()
        self.assertEqual(len(policy["entries"]), 41)
        self.assertEqual(policy_hash, filtering.POLICY_SHA256)
        self.assertEqual(helper_hash, hashlib.sha256(SCRIPT.read_bytes()).hexdigest())


if __name__ == "__main__":
    unittest.main()
