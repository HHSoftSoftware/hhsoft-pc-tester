import copy
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

import buildroot_source_cache as cache


def record(data=b"archive fixture"):
    return {"size": len(data), "sha256": hashlib.sha256(data).hexdigest()}


class RedactedArgumentTests(unittest.TestCase):
    private_value = "synthetic-private-cli-value"

    def assert_redacted_failure(self, arguments):
        with tempfile.TemporaryDirectory(prefix="hhpt-source-arguments-") as temporary:
            result = subprocess.run([sys.executable, "-I", "-B", str(Path(cache.__file__).resolve()), *arguments],
                                    cwd=temporary, capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 1)
            self.assertEqual(result.stdout, "")
            self.assertEqual(result.stderr, "Buildroot source cache check failed; inputs or reviewed policy differ.\n")
            self.assertEqual(list(Path(temporary).iterdir()), [])

    def test_invalid_command_and_unknown_options_do_not_echo_values(self):
        for arguments in (
                [self.private_value, "--cache", ".", "--repository", "."],
                ["verify", "--cache", ".", "--repository", ".", "--" + self.private_value],
                ["verify", "--cache", ".", "--repository", ".", "--unknown", self.private_value]):
            with self.subTest(arguments=arguments):
                self.assert_redacted_failure(arguments)

    def test_missing_required_arguments_and_option_values_are_redacted(self):
        for arguments in (
                [],
                ["verify", "--repository", self.private_value],
                ["verify", "--cache", self.private_value],
                ["verify", "--repository", self.private_value, "--cache"],
                ["verify", "--cache", self.private_value, "--repository"],
                ["candidate", "--cache", ".", "--repository", self.private_value, "--output"],
                ["stage", "--cache", ".", "--repository", self.private_value, "--destination"]):
            with self.subTest(arguments=arguments):
                self.assert_redacted_failure(arguments)

    def test_command_specific_argument_errors_keep_the_same_generic_failure(self):
        for arguments in (
                ["candidate", "--cache", ".", "--repository", self.private_value],
                ["stage", "--cache", ".", "--repository", self.private_value],
                ["verify", "--cache", ".", "--repository", ".", "--output", self.private_value],
                ["verify", "--cache", ".", "--repository", ".", "--destination", self.private_value]):
            with self.subTest(arguments=arguments):
                self.assert_redacted_failure(arguments)


class SourceLockSchemaTests(unittest.TestCase):
    def setUp(self):
        self.lock = {"schema": 1, "profile": "legacy-x86",
                     "policy": {".go-version": record()}, "files": {"linux/linux-1.tar.xz": record()}}

    def test_unsafe_payload_paths_and_unexpected_types_are_rejected(self):
        for path in ("../outside.tar.gz", "/tmp/source.tar.gz", "linux/../source.tar.gz",
                     "linux//source.tar.gz", "linux/source.tar.gz/extra", "linux\\source.tar.gz",
                     "linux/.private-key", "linux/id_rsa", "linux/source\n.tar.gz", "linux/source.tar.gz "):
            with self.subTest(path=path):
                value = copy.deepcopy(self.lock)
                value["files"] = {path: record()}
                with self.assertRaises(ValueError):
                    cache.validate_lock(value)

    def test_schema_types_digests_bounds_and_unknown_fields_are_strict(self):
        for mutation in (lambda item: item.update(schema=True), lambda item: item.update(schema=2),
                         lambda item: item.update(profile="aarch64"), lambda item: item.update(extra=1),
                         lambda item: item["files"].clear(),
                         lambda item: item["files"]["linux/linux-1.tar.xz"].update(size=True),
                         lambda item: item["files"]["linux/linux-1.tar.xz"].update(size=0),
                         lambda item: item["files"]["linux/linux-1.tar.xz"].update(size=-1),
                         lambda item: item["files"]["linux/linux-1.tar.xz"].update(size=cache.MAX_OBJECT_BYTES + 1),
                         lambda item: item["files"]["linux/linux-1.tar.xz"].update(sha256="a" * 63),
                         lambda item: item["policy"].update({"platform/buildroot/../secret": record()})):
            with self.subTest(mutation=mutation):
                value = copy.deepcopy(self.lock)
                mutation(value)
                with self.assertRaises(ValueError):
                    cache.validate_lock(value)


@unittest.skipUnless(sys.platform.startswith("linux"), "descriptor-bound source cache checks require Linux")
class BuildrootSourceCacheTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="hhpt-buildroot-sources-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.repository = self.root / "repository"
        self.sources = self.root / "sources"
        self.work = self.root / "work"
        self.work.mkdir()
        (self.repository / "build").mkdir(parents=True)
        for name in cache.POLICY_FILES:
            self.write(self.repository / name, b"reviewed build policy fixture\n")
        kernel = b"synthetic kernel archive\n"
        self.write(self.repository / "platform/buildroot/base.lock",
                   ("LINUX_SOURCE=linux-1.tar.xz\nLINUX_SHA256=" + hashlib.sha256(kernel).hexdigest() + "\n").encode())
        self.write(self.repository / "platform/buildroot/configs/hhpt_legacy_x86_defconfig", b"configuration fixture\n")
        self.write(self.repository / "platform/buildroot/patches/linux/linux.hash", b"patch policy fixture\n")
        self.write(self.sources / "linux/linux-1.tar.xz", kernel)
        self.write(self.sources / "busybox/busybox-1.tar.bz2", b"synthetic busybox archive\n")
        self.approved = self.repository / cache.APPROVED
        self.lock = cache.create_lock(self.sources, self.repository)
        self.approved.write_text(json.dumps(self.lock), encoding="utf-8")

    def write(self, path, content):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(content)
        return path

    def verify(self):
        cache.verify(self.sources, self.repository)

    def test_candidate_does_not_authorize_release_and_cannot_write_public_policy(self):
        self.approved.unlink()
        output = self.repository / "build/source-cache.candidate.json"
        cache.candidate(self.sources, self.repository, output)
        self.assertEqual(json.loads(output.read_text()), self.lock)
        self.assertFalse(self.approved.exists())
        with self.assertRaises(OSError):
            self.verify()
        with self.assertRaises(ValueError):
            cache.candidate(self.sources, self.repository, self.approved)
        with self.assertRaises(OSError):
            cache.candidate(self.sources, self.repository, output)

    def test_exact_cache_stages_independent_files_without_mutating_preserved_inputs(self):
        self.write(self.sources / "linux/.lock", b"")
        self.write(self.sources / "busybox/.lock", b"")
        self.verify()
        destination = self.work / "downloads"
        cache.stage(self.sources, self.repository, destination)
        self.assertEqual(cache.inventory(destination), self.lock["files"])
        self.assertFalse((destination / "linux/.lock").exists())
        self.assertEqual((self.sources / "linux/.lock").read_bytes(), b"")
        source = self.sources / "linux/linux-1.tar.xz"
        staged = destination / "linux/linux-1.tar.xz"
        self.assertNotEqual(source.stat().st_ino, staged.stat().st_ino)
        staged.write_bytes(b"changed staged bytes")
        self.assertEqual(source.read_bytes(), b"synthetic kernel archive\n")
        self.verify()
        with self.assertRaises(ValueError):
            cache.verify(destination, self.repository)

    def test_candidate_cannot_add_output_to_a_cache_under_build_storage(self):
        nested_cache = self.repository / "build/preserved-cache"
        for relative in self.lock["files"]:
            self.write(nested_cache / relative, (self.sources / relative).read_bytes())
        output = nested_cache / "candidate.json"
        with self.assertRaises(ValueError):
            cache.candidate(nested_cache, self.repository, output)
        self.assertFalse(output.exists())
        self.assertEqual(cache.inventory(nested_cache), self.lock["files"])
        self.verify()

    def test_staging_refuses_reuse_and_caches_nested_in_original_inputs(self):
        destination = self.work / "downloads"
        destination.mkdir()
        sentinel = self.write(destination / "keep", b"prior evidence")
        with self.assertRaises(FileExistsError):
            cache.stage(self.sources, self.repository, destination)
        self.assertEqual(sentinel.read_bytes(), b"prior evidence")
        with self.assertRaises(ValueError):
            cache.stage(self.sources, self.repository, self.sources / "fresh")

    def test_double_slash_alias_cannot_bypass_input_cache_preservation(self):
        destination = self.sources / "fresh"
        with self.assertRaises(ValueError):
            cache.stage(self.sources, self.repository, "//" + str(destination).lstrip("/"))
        self.assertFalse(destination.exists())
        nested_cache = self.repository / "build/preserved-cache"
        for relative in self.lock["files"]:
            self.write(nested_cache / relative, (self.sources / relative).read_bytes())
        output = nested_cache / "candidate.json"
        with self.assertRaises(ValueError):
            cache.candidate(nested_cache, "//" + str(self.repository).lstrip("/"),
                            "//" + str(output).lstrip("/"))
        self.assertFalse(output.exists())
        self.assertEqual(cache.inventory(nested_cache), self.lock["files"])
        self.verify()

    def test_modified_missing_and_extra_payloads_fail(self):
        target = self.sources / "busybox/busybox-1.tar.bz2"
        original = target.read_bytes()
        target.write_bytes(b"X" * len(original))
        with self.assertRaises(ValueError):
            self.verify()
        target.unlink()
        with self.assertRaises(ValueError):
            self.verify()
        target.write_bytes(original)
        self.write(self.sources / "busybox/extra.tar.gz", b"unreviewed archive")
        with self.assertRaises(ValueError):
            self.verify()

    def test_base_go_config_patch_and_added_policy_changes_invalidate_lock(self):
        paths = ("platform/buildroot/base.lock", ".go-version",
                 "platform/buildroot/configs/hhpt_legacy_x86_defconfig",
                 "platform/buildroot/patches/linux/linux.hash", "scripts/build/build-go.sh")
        for relative in paths:
            with self.subTest(relative=relative):
                path = self.repository / relative
                original = path.read_bytes()
                path.write_bytes(original + b"changed policy\n")
                with self.assertRaises(ValueError):
                    self.verify()
                path.write_bytes(original)
        self.write(self.repository / "platform/buildroot/patches/linux/new.patch", b"new patch")
        with self.assertRaises(ValueError):
            self.verify()

    def test_candidate_requires_pinned_kernel_even_if_cache_otherwise_nonempty(self):
        (self.sources / "linux/linux-1.tar.xz").write_bytes(b"different kernel")
        with self.assertRaises(ValueError):
            cache.create_lock(self.sources, self.repository)

    def test_duplicates_malformed_and_oversized_lock_are_rejected(self):
        for content in ('{"schema":1,"schema":1}', '{"files":', '[]', '{"files": NaN}'):
            with self.subTest(content=content):
                self.approved.write_text(content)
                with self.assertRaises((ValueError, TypeError)):
                    self.verify()
        self.approved.write_text(json.dumps(self.lock))
        with mock.patch.object(cache, "MAX_LOCK_BYTES", 4):
            with self.assertRaises(ValueError):
                self.verify()

    def test_root_parent_directory_payload_policy_and_lock_symlinks_fail_closed(self):
        link = self.root / "linked"
        link.symlink_to(self.sources, target_is_directory=True)
        for root in (link, link / "linux/.."):
            with self.subTest(root=root):
                with self.assertRaises((OSError, ValueError)):
                    cache.inventory(root)
        for path in (self.sources / "busybox/busybox-1.tar.bz2", self.approved,
                     self.repository / "scripts/build/build-go.sh"):
            with self.subTest(path=path):
                original = path.read_bytes()
                outside = self.write(self.root / "outside-fixture", original)
                path.unlink()
                path.symlink_to(outside)
                with self.assertRaises((OSError, ValueError)):
                    self.verify()
                path.unlink()
                path.write_bytes(original)
        nested = self.sources / "busybox/nested"
        nested.symlink_to(self.root, target_is_directory=True)
        with self.assertRaises(ValueError):
            self.verify()

    def test_hardlinked_payloads_and_nonempty_lock_metadata_are_rejected(self):
        target = self.sources / "busybox/busybox-1.tar.bz2"
        os.link(target, self.root / "hardlink")
        with self.assertRaises(ValueError):
            self.verify()
        (self.root / "hardlink").unlink()
        self.write(self.sources / "busybox/.lock", b"unexpected state")
        with self.assertRaises(ValueError):
            self.verify()

    def test_fifo_payload_or_lock_is_rejected_before_blocking_read(self):
        for name in ("extra.tar.gz", ".lock"):
            with self.subTest(name=name):
                path = self.sources / "busybox" / name
                os.mkfifo(path)
                result = subprocess.run([sys.executable, "-I", "-B", str(Path(cache.__file__).resolve()),
                                         "verify", "--cache", str(self.sources), "--repository", str(self.repository)],
                                        capture_output=True, timeout=3)
                self.assertNotEqual(result.returncode, 0)
                path.unlink()

    def test_replacement_special_file_is_never_opened_for_data(self):
        for replacement_phase in ("before-pin", "before-data-open"):
            with self.subTest(replacement_phase=replacement_phase):
                self.setUp()
                target = self.sources / "busybox/busybox-1.tar.bz2"
                parent = cache.open_directory(target.parent)
                original_open = os.open
                calls = []
                replaced = False

                def racing_open(path, flags, *args, **kwargs):
                    nonlocal replaced
                    calls.append((str(path), flags))
                    phase_matches = (path == target.name if replacement_phase == "before-pin" else
                                     str(path).startswith("/proc/self/fd/"))
                    if phase_matches and not replaced:
                        replaced = True
                        target.unlink()
                        os.mkfifo(target)
                    return original_open(path, flags, *args, **kwargs)

                try:
                    with mock.patch.object(cache.os, "open", side_effect=racing_open):
                        with self.assertRaises(ValueError):
                            cache.regular_bytes(parent, target.name, cache.MAX_OBJECT_BYTES)
                finally:
                    os.close(parent)
                self.assertTrue(replaced)
                self.assertTrue(all(flags & os.O_PATH for path, flags in calls if path == target.name))
                self.assertTrue(all(path.startswith("/proc/self/fd/") for path, flags in calls
                                    if not flags & os.O_PATH))

    def test_growth_read_stops_after_captured_size_plus_one_byte(self):
        target = self.sources / "busybox/busybox-1.tar.bz2"
        captured = target.stat().st_size
        parent = cache.open_directory(target.parent)
        original_read = os.read
        requested = []
        consumed = 0

        def growing_read(descriptor, amount):
            nonlocal consumed
            if not requested:
                with target.open("ab") as stream:
                    stream.write(b"X" * 4096)
            requested.append(amount)
            data = original_read(descriptor, amount)
            consumed += len(data)
            return data

        try:
            with mock.patch.object(cache.os, "read", side_effect=growing_read):
                with self.assertRaises(ValueError):
                    cache.regular_bytes(parent, target.name, cache.MAX_OBJECT_BYTES)
        finally:
            os.close(parent)
        self.assertEqual(consumed, captured + 1)
        self.assertEqual(requested, [captured + 1])

    def test_payload_counts_per_file_and_total_bytes_are_bounded(self):
        for name, bound in (("MAX_FILES", 1), ("MAX_OBJECT_BYTES", 4),
                            ("MAX_TOTAL_BYTES", 30), ("MAX_DIRECTORIES", 1)):
            with self.subTest(name=name), mock.patch.object(cache, name, bound):
                with self.assertRaises(ValueError):
                    cache.inventory(self.sources)

    def test_empty_unknown_layout_and_non_source_objects_are_rejected(self):
        for relative, content in (("busybox/empty.tar.gz", b""), ("busybox/private.pem", b"fixture"),
                                  ("top.tar.gz", b"fixture")):
            with self.subTest(relative=relative):
                path = self.write(self.sources / relative, content)
                with self.assertRaises((OSError, ValueError)):
                    cache.inventory(self.sources)
                path.unlink()
        (self.sources / "empty-package").mkdir()
        with self.assertRaises(ValueError):
            cache.inventory(self.sources)

    def test_staging_revalidates_a_changed_source_before_returning(self):
        original_verify = cache.verify
        calls = 0

        def change_after_first_check(source, repository):
            nonlocal calls
            result = original_verify(source, repository)
            calls += 1
            if calls == 1:
                (self.sources / "busybox/busybox-1.tar.bz2").write_bytes(b"changed after approval check")
            return result

        with mock.patch.object(cache, "verify", side_effect=change_after_first_check):
            with self.assertRaises(ValueError):
                cache.stage(self.sources, self.repository, self.work / "downloads")


if __name__ == "__main__":
    unittest.main()
