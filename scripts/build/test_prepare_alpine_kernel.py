"""Requirements for the firmware hook's placement, identity and failure gate."""
import json
import os
from pathlib import Path, PurePosixPath
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

import prepare_alpine_kernel as kernel


class PatchTests(unittest.TestCase):
    def setUp(self):
        self.upstream = b"#!/bin/sh -e\nif false; then\n" + kernel.CONTEXT + b"*) : ;;\nesac\nmksquashfs\nmkinitfs\n"
        self.source = PurePosixPath("/fixture/application-source")
        self.work = PurePosixPath("/fixture/tools-x86_64.abcdef/tmp")
        self.receipt = PurePosixPath("/fixture/firmware-exclusions.json")

    def render(self, upstream=None, **kwargs):
        with mock.patch.object(kernel, "UPSTREAM", kernel.record(self.upstream)):
            return kernel.render(self.upstream if upstream is None else upstream,
                                 kwargs.get("source", self.source), kwargs.get("work", self.work),
                                 kwargs.get("receipt", self.receipt), kwargs.get("architecture", "x86_64"))

    def test_exact_copy_changes_only_the_single_pre_compression_boundary(self):
        result = self.render()
        self.assertEqual(result.count(b"/usr/bin/python3 -I -B"), 1)
        self.assertLess(result.index(b"filter_alpine_firmware.py"), result.index(b"mksquashfs\n"))
        self.assertLess(result.index(b"filter_alpine_firmware.py"), result.index(b"mkinitfs\n"))
        self.assertIn(b'--rootfs "$ROOTFS" --modloop "$MODLOOP"', result)
        self.assertIn(b"--work-parent '/fixture/tools-x86_64.abcdef/tmp'", result)
        self.assertIn(b"--architecture 'x86_64'", result)
        self.assertIn(b"|| exit 1\n", result)
        start = result.index(b"# HHSoft:")
        stop = result.index(b"case $ARCH in\n", start)
        self.assertEqual(result[:start] + result[stop:], self.upstream)

    def test_changed_upstream_or_ambiguous_context_never_produces_a_script(self):
        for changed in (self.upstream + b"# drift\n", self.upstream.replace(b"fi\n", b"fi # drift\n")):
            with self.subTest(changed=changed), self.assertRaises(ValueError):
                self.render(changed)
        for content in (b"#!/bin/sh\n", self.upstream + kernel.CONTEXT):
            with mock.patch.object(kernel, "UPSTREAM", kernel.record(content)), self.assertRaises(ValueError):
                kernel.render(content, self.source, self.work, self.receipt, "x86_64")

    def test_shell_metacharacters_relative_paths_and_unreviewed_architectures_reject(self):
        for text in ("/fixture/a'b", "/fixture/a;touch-x", "/fixture/$(id)", "/fixture/a\nb", "relative/path", "/fixture/../outside"):
            for key in ("source", "work", "receipt"):
                with self.subTest(text=text, key=key), self.assertRaises(ValueError):
                    self.render(**{key: PurePosixPath(text)})
        with self.assertRaises(ValueError):
            self.render(architecture="legacy-x86")

    def test_missing_stale_and_duplicate_filter_receipts_fail(self):
        helper = b"reviewed helper"
        policy = json.dumps({"source_archive": {"sha256": "b" * 64}, "entries": [
            {"path": "fixture/%02d" % number, "rootfs_kind": "file",
             "stored": {"size": 1, "sha256": "a" * 64},
             "modloop_architectures": ["x86_64"] if number == 0 else []} for number in range(41)]}).encode()
        rows = [{"path": "fixture/%02d.zst" % number, "kind": "file", "size": 1, "sha256": "a" * 64}
                for number in range(41)]
        value = {"schema": 1, "status": "firmware_exclusions_applied", "architecture": "x86_64",
                 "release_approval": False, "policy_sha256": kernel.record(policy)["sha256"],
                 "helper_sha256": kernel.record(helper)["sha256"],
                 "source_archive_sha256": "b" * 64,
                 "removed": {"rootfs": rows, "modloop": rows[:1]}, "counts": {"rootfs": 41, "modloop": 1}}
        self.assertEqual(kernel.check_receipt(json.dumps(value), "x86_64", helper, policy), value)
        for key, changed in (("architecture", "x86"), ("status", "failed"), ("release_approval", True),
                             ("policy_sha256", "0" * 64), ("helper_sha256", "0" * 64),
                             ("source_archive_sha256", "0" * 64),
                             ("counts", {"rootfs": 40, "modloop": 1}), ("removed", {})):
            with self.subTest(key=key), self.assertRaises(ValueError):
                kernel.check_receipt(json.dumps(dict(value, **{key: changed})), "x86_64", helper, policy)
        for tree in ("rootfs", "modloop"):
            for field, changed in (("path", "unapproved.zst"), ("sha256", "0" * 64),
                                   ("size", 2), ("kind", "symlink")):
                altered = json.loads(json.dumps(value))
                altered["removed"][tree][0][field] = changed
                with self.subTest(tree=tree, field=field), self.assertRaises(ValueError):
                    kernel.check_receipt(json.dumps(altered), "x86_64", helper, policy)
        for replacement in ([], rows[:2], rows[1:2]):
            altered = json.loads(json.dumps(value))
            altered["removed"]["modloop"] = replacement
            altered["counts"]["modloop"] = len(replacement)
            with self.subTest(subset=replacement), self.assertRaises(ValueError):
                kernel.check_receipt(json.dumps(altered), "x86_64", helper, policy)
        value["removed"]["rootfs"] = [rows[0]] * 41
        with self.assertRaises(ValueError):
            kernel.check_receipt(json.dumps(value), "x86_64", helper, policy)
        with self.assertRaises(ValueError):
            kernel.strict_json('{"schema":1,"schema":1}')

    def test_cli_does_not_echo_rejected_private_arguments(self):
        marker = "fixture-private-argument"
        result = subprocess.run([sys.executable, "-I", "-B", kernel.__file__, "--unsupported", marker],
                                capture_output=True, timeout=10)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn(marker.encode(), result.stdout + result.stderr)


@unittest.skipUnless(sys.platform.startswith("linux"), "native inert shell packaging fixture")
class PackagingTests(unittest.TestCase):
    def test_both_outputs_use_filtered_trees_and_failure_stops_before_compression(self):
        for fail in (False, True):
            with self.subTest(fail=fail), tempfile.TemporaryDirectory(prefix="hhpt-kernel-hook-") as temporary:
                base = Path(temporary)
                source, work = base / "application-source", base / "tools-x86_64.abcdef/tmp"
                helper = source / kernel.HELPER
                helper.parent.mkdir(parents=True)
                work.mkdir(parents=True)
                root, modloop = base / "root", base / "modloop"
                root.mkdir()
                modloop.mkdir()
                for folder in (root, modloop):
                    (folder / "denied.bin").write_bytes(b"synthetic omitted bytes")
                helper.write_text("import argparse\nfrom pathlib import Path\np=argparse.ArgumentParser()\n"
                                  "for name in ['rootfs','modloop','architecture','work-parent','receipt']:\n"
                                  " p.add_argument('--'+name,required=True)\na=p.parse_args()\n" +
                                  ("raise SystemExit(1)\n" if fail else
                                   "for value in [a.rootfs,a.modloop]:\n (Path(value)/'denied.bin').unlink()\n"), encoding="utf-8")
                original = (b'#!/bin/sh -e\nARCH=x86_64\nROOTFS=$1\nMODLOOP=$2\ntrace=$3\n'
                            b'if false; then\n' + kernel.CONTEXT + b'*) : ;;\nesac\n'
                            b'test ! -e "$MODLOOP/denied.bin"\nprintf "mksquashfs\\n" >> "$trace"\n'
                            b'test ! -e "$ROOTFS/denied.bin"\nprintf "mkinitfs\\n" >> "$trace"\n')
                with mock.patch.object(kernel, "UPSTREAM", kernel.record(original)):
                    patched = kernel.render(original, source, work, base / "firmware-exclusions.json", "x86_64")
                script = base / "update-kernel"
                script.write_bytes(patched)
                trace = base / "trace"
                result = subprocess.run(["/bin/sh", "-e", str(script), str(root), str(modloop), str(trace)],
                                        capture_output=True, timeout=10, env={"PATH": "/usr/bin:/bin", "HOME": "/dev/null"})
                self.assertEqual(result.returncode == 0, not fail)
                if fail:
                    self.assertFalse(trace.exists())
                    self.assertTrue(all((folder / "denied.bin").exists() for folder in (root, modloop)))
                else:
                    self.assertEqual(trace.read_bytes(), b"mksquashfs\nmkinitfs\n")


if __name__ == "__main__":
    unittest.main()
