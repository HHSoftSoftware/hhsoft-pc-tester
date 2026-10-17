"""Intent-based cache assembly tests; signature bytes are deliberately synthetic."""

import base64
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest import mock

import assemble_alpine_cache as assembler


def tar_bytes(members):
    stream = io.BytesIO()
    with tarfile.open(fileobj=stream, mode="w", format=tarfile.USTAR_FORMAT) as archive:
        for name, contents, kind in members:
            member = tarfile.TarInfo(name)
            member.type = kind
            if kind == tarfile.SYMTYPE:
                member.linkname = "ordinary-payload-link"
            else:
                member.size = len(contents)
            archive.addfile(member, io.BytesIO(contents))
    return stream.getvalue()


def regular(name, data):
    return name, data, tarfile.REGTYPE


def signed_member(value=b"synthetic signature; never trusted"):
    return gzip.compress(tar_bytes([regular(".SIGN.RSA.fixture.rsa.pub", value)]), mtime=0)


def pax_tar(name, contents, attributes):
    stream = io.BytesIO()
    with tarfile.open(fileobj=stream, mode="w", format=tarfile.PAX_FORMAT) as archive:
        member = tarfile.TarInfo(name)
        member.size = len(contents)
        member.pax_headers = attributes
        archive.addfile(member, io.BytesIO(contents))
    return stream.getvalue()


def package(name="fixture-a", version="1.0-r0", arch="x86_64", *, metadata=None, payload=None, control_members=()):
    if payload is None:
        payload = gzip.compress(tar_bytes([regular("usr/share/fixture", b"original payload\n")]), mtime=0)
    fields = {"pkgname": name, "pkgver": version, "arch": arch,
              "size": "17", "pkgdesc": "Synthetic package", "url": "https://example.invalid/fixture",
              "license": "MIT", "datahash": hashlib.sha256(payload).hexdigest()}
    fields.update(metadata or {})
    control = gzip.compress(tar_bytes([regular(".PKGINFO", "".join(key + " = " + value + "\n" for key, value in fields.items()).encode()),
                                      *control_members]), mtime=0)
    blob = signed_member() + control + payload
    entry = {"C": "Q1" + base64.b64encode(hashlib.sha1(control).digest()).decode(), "P": name, "V": version,
             "A": "x86_64" if arch == "noarch" else arch, "S": str(len(blob)), "I": fields["size"],
             "T": fields["pkgdesc"], "U": fields["url"], "L": fields["license"]}
    return blob, entry


def index_bytes(entries):
    text = "\n\n".join("\n".join(key + ":" + value for key, value in entry.items()) for entry in entries) + "\n"
    body = tar_bytes([regular("DESCRIPTION", b"synthetic repository\n"), regular("APKINDEX", text.encode())])
    return signed_member() + gzip.compress(body, mtime=0)


class AssemblyTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="hhpt-cache-assembly-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.inputs = self.root / "packages"
        self.inputs.mkdir()
        self.output = self.root / "assembled"
        self.report = self.root / "report.json"
        self.main = self.root / "main.original"
        self.community = self.root / "community.original"
        self.blob, self.entry = package()
        # The misleading name must never decide identity or repository.
        self.package_path = self.inputs / ("f" * 64 + ".apk")
        self.package_path.write_bytes(self.blob)
        _, self.other = package("fixture-b")
        self.indexes([self.entry], [self.other])

    def indexes(self, main, community):
        self.main.write_bytes(index_bytes(main))
        self.community.write_bytes(index_bytes(community))

    def assemble(self, required=(), directories=None):
        return assembler.assemble("x86_64", self.main, self.community, directories or [self.inputs],
                                  self.output, self.report, required, fixture=True)

    def assert_rejected(self):
        with self.assertRaises((ValueError, OSError, tarfile.TarError)):
            self.assemble()
        self.assertFalse(self.output.exists())

    def test_original_bytes_and_index_membership_choose_canonical_output(self):
        result = self.assemble(["fixture-a=1.0-r0"])
        self.assertEqual(result["status"], "assembled")
        destination = self.output / "main/x86_64/fixture-a-1.0-r0.apk"
        self.assertEqual(destination.read_bytes(), self.blob)
        self.assertEqual((self.output / "main/x86_64/APKINDEX.tar.gz").read_bytes(), self.main.read_bytes())
        self.assertEqual((self.output / "community/x86_64/APKINDEX.tar.gz").read_bytes(), self.community.read_bytes())
        self.assertEqual(result["signature_verification"], "not_performed")
        self.assertEqual(result["dependency_closure"], "not_established")
        self.assertEqual(result["packages"]["main/x86_64/fixture-a-1.0-r0.apk"]["sha256"], hashlib.sha256(self.blob).hexdigest())
        self.assertNotIn(str(self.root), self.report.read_text())
        self.assertEqual(json.loads(self.report.read_text()), result)

    def test_community_membership_is_not_inferred_from_input_name(self):
        self.indexes([self.other], [self.entry])
        self.assemble()
        self.assertTrue((self.output / "community/x86_64/fixture-a-1.0-r0.apk").is_file())
        self.assertFalse((self.output / "main/x86_64/fixture-a-1.0-r0.apk").exists())

    def test_payload_symlinks_and_script_bytes_are_preserved_without_extraction_or_execution(self):
        payload = gzip.compress(tar_bytes([("usr/bin/link", b"", tarfile.SYMTYPE)]), mtime=0)
        self.blob, self.entry = package(payload=payload, control_members=[regular(".post-install", b"exit 97\n")])
        self.package_path.write_bytes(self.blob)
        self.indexes([self.entry], [self.other])
        self.assemble()
        self.assertEqual((self.output / "main/x86_64/fixture-a-1.0-r0.apk").read_bytes(), self.blob)
        self.assertFalse((self.root / "usr").exists())

    def test_noarch_is_matched_to_target_index_without_sharing_other_architecture_bytes(self):
        self.blob, self.entry = package(arch="noarch")
        self.package_path.write_bytes(self.blob)
        self.indexes([self.entry], [self.other])
        result = self.assemble()
        self.assertEqual(next(iter(result["packages"].values()))["architecture"], "noarch")

    def test_identical_duplicates_deduplicate_but_changed_identity_bytes_are_rejected(self):
        second = self.root / "second"
        second.mkdir()
        (second / "alias.apk").write_bytes(self.blob)
        result = self.assemble(directories=[self.inputs, second])
        self.assertEqual(result["input_archives"], 2)
        self.assertEqual(result["duplicate_archives"], 1)
        self.assertEqual(len(result["packages"]), 1)
        self.setUp()
        changed, _ = package(payload=gzip.compress(tar_bytes([regular("different", b"different payload")]), mtime=0))
        (self.inputs / "other.apk").write_bytes(changed)
        self.assert_rejected()

    def test_required_gaps_report_exact_identities_and_produce_no_partial_cache(self):
        result = self.assemble(["fixture-b=1.0-r0", "unknown=2.0-r0", "fixture-a=1.0-r0"])
        self.assertEqual(result["status"], "incomplete")
        self.assertEqual(result["required_gaps"], [
            {"name": "fixture-b", "version": "1.0-r0", "reason": "archive_missing"},
            {"name": "unknown", "version": "2.0-r0", "reason": "identity_not_indexed"}])
        self.assertFalse(self.output.exists())
        self.assertTrue(self.report.is_file())

    def test_unindexed_archive_is_reported_and_cannot_silently_enter_cache(self):
        blob, _ = package("unindexed")
        (self.inputs / "extra.apk").write_bytes(blob)
        result = self.assemble()
        self.assertEqual(result["status"], "incomplete")
        self.assertEqual(result["unindexed_archives"][0]["name"], "unindexed")
        self.assertFalse(self.output.exists())

    def test_cross_repository_ambiguity_and_duplicate_index_identity_are_rejected(self):
        for main, community in (([self.entry], [self.entry]), ([self.entry, self.entry], [self.other])):
            with self.subTest(main_count=len(main), community=community[0]["P"]):
                self.indexes(main, community)
                self.assert_rejected()

    def test_checksum_is_compressed_control_identity_not_full_archive_sha1(self):
        wrong = dict(self.entry, C="Q1" + base64.b64encode(hashlib.sha1(self.blob).digest()).decode())
        self.indexes([wrong], [self.other])
        self.assert_rejected()

    def test_original_index_timestamp_pax_headers_are_preserved_without_applying_path_overrides(self):
        timestamped = gzip.compress(pax_tar(".SIGN.RSA.fixture.rsa.pub", b"synthetic signature", {
            "mtime": "1792054256.030027291", "ctime": "0", "atime": "0"}), mtime=0)
        original = self.main.read_bytes()
        reader = io.BytesIO(original)
        assembler.signatures(reader)
        self.main.write_bytes(timestamped + original[reader.tell():])
        self.assemble()
        self.assertEqual((self.output / "main/x86_64/APKINDEX.tar.gz").read_bytes(), self.main.read_bytes())
        for key, value in (("path", "../escaped"), ("size", "1"), ("linkpath", "elsewhere"),
                           ("GNU.sparse.size", "512"), ("mtime", "not-a-timestamp")):
            with self.subTest(key=key):
                with self.assertRaises(ValueError):
                    assembler.metadata_tar(pax_tar(".PKGINFO", b"test", {key: value}), lambda name: name == ".PKGINFO")
        self.assertFalse((self.root / "escaped").exists())

    def test_unselected_large_packages_and_provides_fields_do_not_block_small_selected_archives(self):
        other = dict(self.other, S=str(assembler.MAX_APK + 1), p="fixture-provider " * 5000)
        self.indexes([self.entry], [other])
        self.assertEqual(self.assemble()["status"], "assembled")
        self.setUp()
        # The archive limit still applies before any archive parsing, even if
        # the exact original bytes have valid index membership.
        with mock.patch.object(assembler, "MAX_APK", len(self.blob) - 1), \
                mock.patch.object(assembler, "signatures", wraps=assembler.signatures) as signatures:
            self.assert_rejected()
        self.assertEqual(signatures.call_count, 2)  # Only the two indexes were parsed.

    def test_changed_control_payload_size_and_metadata_cannot_match_original_index(self):
        for kind in ("control", "payload", "size", "license", "architecture"):
            with self.subTest(kind=kind):
                self.setUp()
                if kind == "control":
                    changed, _ = package(metadata={"pkgdesc": "Changed description"})
                    self.package_path.write_bytes(changed)
                elif kind == "payload":
                    # Keep the original signed control identity, but replace the
                    # third compressed member with another well-formed payload.
                    reader = io.BytesIO(self.blob)
                    assembler.signatures(reader)
                    assembler.gzip_member(reader, assembler.MAX_CONTROL)
                    replacement = gzip.compress(tar_bytes([regular("changed", b"changed")]), mtime=0)
                    self.package_path.write_bytes(self.blob[:reader.tell()] + replacement)
                else:
                    changed = dict(self.entry)
                    changed[{"size": "S", "license": "L", "architecture": "A"}[kind]] = {
                        "size": str(len(self.blob) + 1), "license": "GPL-3.0-only", "architecture": "aarch64"}[kind]
                    self.indexes([changed], [self.other])
                self.assert_rejected()

    def test_metadata_paths_links_duplicates_and_unknown_formats_fail_closed(self):
        for kind in ("traversal", "symlink", "duplicate", "extra-member", "unsigned", "unknown-checksum"):
            with self.subTest(kind=kind):
                self.setUp()
                if kind == "traversal":
                    blob, entry = package(control_members=[regular("../escaped", b"never extracted")])
                elif kind == "symlink":
                    blob, entry = package(control_members=[(".post-install", b"", tarfile.SYMTYPE)])
                elif kind == "duplicate":
                    blob, entry = package(control_members=[regular(".PKGINFO", b"pkgname = other\n")])
                elif kind == "extra-member":
                    blob, entry = self.blob + gzip.compress(b"extra", mtime=0), self.entry
                elif kind == "unsigned":
                    reader = io.BytesIO(self.blob)
                    assembler.signatures(reader)
                    blob, entry = self.blob[reader.tell():], self.entry
                else:
                    blob, entry = self.blob, dict(self.entry, C="unsupported-format")
                self.package_path.write_bytes(blob)
                self.indexes([entry], [self.other])
                self.assert_rejected()
                self.assertFalse((self.root / "escaped").exists())

    def test_unsafe_package_identity_and_ambiguous_canonical_filename_are_rejected(self):
        for name in ("../escape", "/escape", ".", "..", "bad\\name"):
            with self.subTest(name=name):
                changed = dict(self.entry, P=name)
                self.indexes([changed], [self.other])
                self.assert_rejected()
        self.setUp()
        first, a = package("a-b", "1")
        second, b = package("a", "b-1")
        self.package_path.write_bytes(first)
        (self.inputs / "second.apk").write_bytes(second)
        self.indexes([a, b], [self.other])
        self.assert_rejected()

    def test_existing_output_or_report_is_never_reused_or_overwritten(self):
        self.output.mkdir()
        marker = self.output / "previous-evidence"
        marker.write_text("preserve\n")
        with self.assertRaises(ValueError):
            self.assemble()
        self.assertEqual(marker.read_text(), "preserve\n")
        self.setUp()
        self.report.write_text("preserve existing report\n")
        self.assert_rejected()
        self.assertEqual(self.report.read_text(), "preserve existing report\n")

    def test_filesystem_symlinks_and_subdirectories_are_not_discovery_authority(self):
        (self.inputs / "nested").mkdir()
        self.assert_rejected()
        self.setUp()
        target = self.root / "source-original"
        target.write_bytes(self.blob)
        self.package_path.unlink()
        try:
            self.package_path.symlink_to(target)
        except (OSError, NotImplementedError):
            self.skipTest("symlink creation is unavailable")
        self.assert_rejected()

    def test_expansion_counts_and_changed_inputs_are_bounded(self):
        with mock.patch.object(assembler, "MAX_PAYLOAD", 1024):
            self.assert_rejected()
        with mock.patch.object(assembler, "MAX_FILES", 0):
            self.assert_rejected()
        original = assembler.plan

        def change_after_plan(*args):
            result = original(*args)
            self.package_path.write_bytes(self.blob + b"changed")
            return result

        with mock.patch.object(assembler, "plan", side_effect=change_after_plan), self.assertRaises(ValueError):
            self.assemble()
        self.assertEqual(self.report.read_bytes(), b"")

    def test_shared_compressed_and_expanded_budgets_block_before_excess_processing(self):
        with mock.patch.object(assembler, "MAX_INPUT_BYTES", len(self.blob) - 1), \
                mock.patch.object(assembler, "signatures", wraps=assembler.signatures) as signatures:
            self.assert_rejected()
        self.assertEqual(signatures.call_count, 2)
        # Each fixture index expands to 20 KiB, and each package to 30 KiB.
        # A second identical input must consume the same shared budget too.
        (self.inputs / "duplicate.apk").write_bytes(self.blob)
        with mock.patch.object(assembler, "MAX_EXPANDED_BYTES", 80 << 10):
            self.assert_rejected()
        with mock.patch.object(assembler, "MAX_EXPANDED_BYTES", 100 << 10):
            self.assertEqual(self.assemble()["duplicate_archives"], 1)

    def test_file_growth_cannot_extend_hashing_or_copying_beyond_the_captured_extent(self):
        original_read = assembler.BoundedReader.read

        def append_on_first_read(reader, count):
            if self.package_path.stat().st_size == len(self.blob):
                with self.package_path.open("ab") as growing:
                    growing.write(b"extra" * (assembler.CHUNK // 5 + 1))
            return original_read(reader, count)

        files = assembler.Files(fixture=True)
        with mock.patch.object(assembler.BoundedReader, "read", append_on_first_read), self.assertRaises(ValueError):
            with files.regular(self.package_path, assembler.MAX_APK) as (stream, size):
                with self.assertRaises(ValueError):
                    assembler.sha256(stream, size)
                self.assertEqual(stream.stream.tell(), len(self.blob) + 1)
                # The context also rejects the changed file identity on exit.
        self.setUp()
        copied = self.root / "growth-copy.apk"
        with mock.patch.object(assembler.BoundedReader, "read", append_on_first_read), self.assertRaises(ValueError):
            assembler.copy_original(files, self.package_path, copied, None,
                                    {"size": len(self.blob), "sha256": hashlib.sha256(self.blob).hexdigest()})
        self.assertEqual(copied.read_bytes(), self.blob)

    def test_unrelated_regular_files_are_not_read_or_published(self):
        (self.inputs / "unrelated.txt").write_text("fixture-local-data\n")
        result = self.assemble()
        self.assertEqual(result["input_archives"], 1)
        self.assertNotIn("fixture-local-data", self.report.read_text())
        self.assertFalse((self.output / "unrelated.txt").exists())

    def cli(self, *extra):
        return [sys.executable, "-I", "-B", str(Path(assembler.__file__)), "--arch", "x86_64",
                "--main-index", str(self.main), "--community-index", str(self.community),
                "--packages", str(self.inputs), "--output", str(self.output), "--report", str(self.report), *extra]

    def test_cli_errors_do_not_repeat_rejected_private_arguments(self):
        private = "fixture-private-location-token"
        result = subprocess.run(self.cli("--unsupported", private), capture_output=True, text=True, timeout=5)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn(private, result.stdout + result.stderr)
        self.assertNotIn(str(self.root), result.stdout + result.stderr)
        self.assertNotIn("Traceback", result.stderr)

    @unittest.skipUnless(assembler.LINUX_PINNING, "production descriptor and FIFO checks require Linux")
    def test_fifo_inputs_fail_without_opening_or_waiting_for_a_writer(self):
        self.main.unlink()
        os.mkfifo(self.main)
        result = subprocess.run(self.cli(), capture_output=True, text=True, timeout=3)
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.output.exists())
        self.assertNotIn(str(self.root), result.stdout + result.stderr)

    @unittest.skipUnless(assembler.LINUX_PINNING, "production CLI requires Linux")
    def test_incomplete_cli_returns_failure_and_writes_only_gap_report(self):
        result = subprocess.run(self.cli("--require", "fixture-b=1.0-r0"), capture_output=True, text=True, timeout=5)
        self.assertEqual(result.returncode, 2, result.stderr)
        self.assertFalse(self.output.exists())
        self.assertEqual(json.loads(self.report.read_text())["status"], "incomplete")


if __name__ == "__main__":
    unittest.main()
