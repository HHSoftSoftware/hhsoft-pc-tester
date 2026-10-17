import copy
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

import release
import release_fixtures as fixture
import source_assets as sources


class SourceFixture(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.version, self.commit = "0.1.0-alpha.1", "a" * 40
        self.candidate, self.project, self.payloads = fixture.candidate(self.root, self.version, self.commit)
        self.path = self.candidate / "SOURCE-MANIFEST.json"

    def check(self):
        return sources.validate_sources(self.candidate, self.version, self.commit, self.root, self.project)

    def change(self, change):
        value = sources.read_json(self.path)
        change(value)
        fixture.write_json(self.path, value)

    def rewrite_part(self, mutate):
        value = sources.read_json(self.path)
        part = value["parts"][0]
        path = self.candidate / part["name"]
        data = bytearray(path.read_bytes())
        mutate(data)
        path.write_bytes(data)
        part["size"], part["sha256"] = len(data), sources.sha(data)
        fixture.write_json(self.path, value)


class SourceAssetTests(SourceFixture):
    def test_standard_tar_parts_preserve_every_whole_source_file(self):
        fixture.write_source_manifest(self.candidate, self.version, self.commit, self.payloads, count=2)
        manifest, _ = self.check()
        observed = {}
        # Independently read standard tar members, never restore their paths.
        for part in manifest["parts"]:
            with tarfile.open(self.candidate / part["name"], "r:") as archive:
                for member in archive:
                    self.assertTrue(member.isfile())
                    observed[member.name] = archive.extractfile(member).read()
        self.assertEqual(observed, {name: data for name, (data, _role) in self.payloads.items()})
        with tarfile.open(fileobj=io.BytesIO(observed["sources/fixture/source.tar.gz"]), mode="r:gz") as archive:
            self.assertEqual(archive.extractfile("fixture/source.c").read(), b"int fixture(void) { return 1; }\n")

    def test_a_changed_member_is_not_hidden_by_rehashing_outer_tar(self):
        self.rewrite_part(lambda data: data.__setitem__(512, data[512] ^ 1))
        with self.assertRaises(ValueError):
            self.check()

    def test_changed_part_checksum_or_missing_part_blocks(self):
        self.change(lambda value: value["parts"][0].__setitem__("sha256", "0" * 64))
        with self.assertRaises(ValueError):
            self.check()
        (self.candidate / sources.part_names(self.version, 1)[0]).unlink()
        with self.assertRaises(ValueError):
            self.check()

    def test_an_extra_well_named_part_is_not_an_unvalidated_attachment(self):
        name = sources.part_names(self.version, 2)[1]
        (self.candidate / name).write_bytes(b"not a declared source part")
        with self.assertRaises(ValueError):
            self.check()

    def test_hidden_footer_bytes_and_extra_block_are_not_ignored(self):
        original = (self.candidate / sources.part_names(self.version, 1)[0]).read_bytes()
        manifest = self.path.read_bytes()
        for mutation in (lambda data: data.__setitem__(-1, 1), lambda data: data.extend(b"\0" * 10240)):
            with self.subTest(mutation=mutation.__name__):
                (self.candidate / sources.part_names(self.version, 1)[0]).write_bytes(original)
                self.path.write_bytes(manifest)
                self.rewrite_part(mutation)
                with self.assertRaises(ValueError):
                    self.check()

    def test_tar_links_devices_pax_and_noncanonical_metadata_are_rejected(self):
        original = (self.candidate / sources.part_names(self.version, 1)[0]).read_bytes()
        manifest = self.path.read_bytes()
        for typeflag in (b"1", b"2", b"3", b"5", b"x", b"g"):
            with self.subTest(typeflag=typeflag):
                (self.candidate / sources.part_names(self.version, 1)[0]).write_bytes(original)
                self.path.write_bytes(manifest)
                def mutation(data):
                    data[156:157] = typeflag
                    data[148:156] = b"        "
                    data[148:156] = ("%06o\0 " % sum(data[:512])).encode("ascii")
                self.rewrite_part(mutation)
                with self.assertRaises(ValueError):
                    self.check()

    def test_padding_is_checked_even_when_part_hash_matches(self):
        first = sources.read_json(self.path)["files"][0]
        self.assertNotEqual(first["size"] % 512, 0)
        self.rewrite_part(lambda data: data.__setitem__(512 + first["size"], 1))
        with self.assertRaises(ValueError):
            self.check()

    def test_unsafe_agent_and_ambiguous_logical_names_are_rejected(self):
        original = self.path.read_bytes()
        names = ("sources/../escape", "/absolute/file", "sources/drive:C", "sources/back\\slash", "sources/A/CON.txt",
                 "sources/AGENTS.md", "sources/AGENTS.md.tar.gz", "sources/.github/instructions/test.instructions.md",
                 "sources/.ssh/config", "sources/file.", "sources/secret/credential.txt")
        for name in names:
            with self.subTest(name=name):
                self.path.write_bytes(original)
                self.change(lambda value: value["files"][-1].__setitem__("path", name))
                with self.assertRaises(ValueError):
                    self.check()

    def test_duplicate_case_alias_and_file_as_parent_are_rejected(self):
        original = self.path.read_bytes()
        for suffix in ("", "/child"):
            self.path.write_bytes(original)
            def change(value):
                first = value["files"][0]
                value["files"][1]["path"] = first["path"].upper() if not suffix else first["path"] + suffix
            self.change(change)
            with self.assertRaises(ValueError):
                self.check()

    def test_package_missing_or_changed_provenance_is_not_fulfilled(self):
        original = self.path.read_bytes()
        changes = (lambda value: value["packages"]["x86"].pop(),
                   lambda value: value["packages"]["x86"][0]["inventory"].__setitem__("sha256", "0" * 64),
                   lambda value: value["packages"]["legacy-x86"][0]["inventory"].__setitem__("SOURCE SITE", "https://example.invalid/changed"))
        for change in changes:
            self.path.write_bytes(original)
            self.change(change)
            with self.assertRaises(ValueError):
                self.check()

    def test_no_unavailable_or_empty_fulfillment_escape(self):
        original = self.path.read_bytes()
        for field, value in (("source_form", "not-required"), ("source_form", "unavailable"),
                             ("sources", []), ("licenses", []), ("build", []), ("review", "  ")):
            with self.subTest(field=field, value=value):
                self.path.write_bytes(original)
                self.change(lambda manifest: manifest["packages"]["x86"][0].__setitem__(field, value))
                with self.assertRaises(ValueError):
                    self.check()

    def test_binary_preferred_form_still_requires_included_material(self):
        self.change(lambda value: value["packages"]["x86"][0].__setitem__("source_form", "upstream-binary-form"))
        self.check()
        self.change(lambda value: value["packages"]["x86"][0].__setitem__("sources", []))
        with self.assertRaises(ValueError):
            self.check()

    def test_notice_bytes_and_reference_boundaries_are_checked(self):
        original = self.path.read_bytes()
        for field, value in (("offset", -1), ("offset", True), ("size", 0), ("size", 1 << 30), ("sha256", "0" * 64)):
            self.path.write_bytes(original)
            self.change(lambda manifest: manifest["packages"]["x86"][0]["notice"].__setitem__(field, value))
            with self.assertRaises(ValueError):
                self.check()

    def test_tagged_project_file_set_and_bytes_are_required(self):
        original = self.path.read_bytes()
        for change in (lambda value: value["files"].pop(0),
                       lambda value: value["files"][0].__setitem__("sha256", "0" * 64),
                       lambda value: value["files"][0].__setitem__("mode", "0755")):
            self.path.write_bytes(original)
            self.change(change)
            with self.assertRaises(ValueError):
                self.check()

    def test_part_count_size_and_member_resource_bounds_fail_closed(self):
        for names in ([], [f"hhsoft-pc-tester-{self.version}.sources-02.tar"], sources.part_names(self.version, 16) +
                      [f"hhsoft-pc-tester-{self.version}.sources-17.tar"]):
            with self.assertRaises(ValueError):
                sources.part_count(self.version, names)
        with mock.patch.object(sources, "MAX_PART", 10240), self.assertRaises(ValueError):
            self.check()

    def test_regular_inputs_reject_hardlinks(self):
        path = self.candidate / "THIRD-PARTY-NOTICES.txt"
        alias = self.root / "fixture-hardlink"
        try:
            os.link(path, alias)
        except OSError:
            self.skipTest("fixture filesystem does not permit hardlinks")
        with self.assertRaises(ValueError):
            sources.read_bytes(path)

    @unittest.skipUnless(sys.platform.startswith("linux") and hasattr(os, "O_PATH"), "Linux pinned descriptor fixture")
    def test_fifo_is_rejected_before_a_data_open_without_hanging(self):
        path = self.root / "fixture-fifo"
        os.mkfifo(path)
        code = "import source_assets,sys; source_assets.read_bytes(sys.argv[1])"
        result = subprocess.run([sys.executable, "-B", "-c", code, str(path)],
                                cwd=Path(sources.__file__).parent, capture_output=True, timeout=3)
        self.assertNotEqual(result.returncode, 0)

    def test_growing_source_part_does_not_read_unbounded_extra_data(self):
        path = self.root / "fixture-growth"
        path.write_bytes(b"old")
        with self.assertRaises(ValueError):
            with sources.regular(path) as (stream, size):
                self.assertEqual(size, 3)
                with path.open("ab") as output:
                    output.write(b"changed")
                self.assertEqual(stream.read(size), b"old")

    def test_project_byte_binding_uses_raw_tree_ids_and_clean_git_environment(self):
        path = self.root / "VERSION"
        data = path.read_bytes()
        object_id = hashlib.sha1(b"blob " + str(len(data)).encode() + b"\0" + data).hexdigest()
        tree = f"100644 blob {object_id} {len(data)}\tVERSION\0".encode()
        with mock.patch.object(sources.subprocess, "run", return_value=mock.Mock(stdout=tree)) as command:
            result = sources.project_inventory(self.root, self.commit)
            self.assertEqual(result["VERSION"]["sha256"], sources.sha(data))
            self.assertIn("--no-replace-objects", command.call_args.args[0])
            self.assertEqual(command.call_args.kwargs["env"]["GIT_NO_LAZY_FETCH"], "1")
            self.assertNotIn("PYTHONPATH", command.call_args.kwargs["env"])
            with mock.patch.object(sources, "MAX_PROJECT", 1), self.assertRaises(ValueError):
                sources.project_inventory(self.root, self.commit)
            path.write_bytes(data + b"changed")
            with self.assertRaises(ValueError):
                sources.project_inventory(self.root, self.commit)

    def test_nested_json_failure_is_redacted_at_the_real_cli_boundary(self):
        program = self.root / "scripts/release"
        program.mkdir(parents=True)
        for name in ("release.py", "source_assets.py", "signatures.py"):
            (program / name).write_bytes((Path(release.__file__).parent / name).read_bytes())
        (self.candidate / "lab-evidence.json").write_bytes(b"[" * 2000 + b"0" + b"]" * 2000)
        result = subprocess.run([sys.executable, "-B", str(program / "release.py"), "seal", "--candidate", str(self.candidate),
                                 "--tag", "v" + self.version, "--commit", self.commit], capture_output=True, timeout=10)
        self.assertEqual(result.returncode, 1)
        self.assertIn(b"publication blocked", result.stderr)
        self.assertNotIn(b"Traceback", result.stderr)
        self.assertNotIn(str(self.root).encode(), result.stderr + result.stdout)
        self.assertFalse((self.candidate / "release-evidence.json").exists())


class InspectionTests(SourceFixture):
    def inspect(self, arch="x86_64"):
        inventory = sources.inventories(self.candidate, self.version)
        sources.validate_inspection(self.candidate, release.image_name(self.version, arch), arch, self.version,
                                    self.commit, self.root, self.project, inventory[arch])

    def test_inspected_application_has_exact_version_target_permissions_and_host(self):
        image = release.image_name(self.version, "x86_64")
        path = self.candidate / (image + ".inspection.json")
        original = path.read_bytes()
        for field, value in (("version", "0.1.0-dev.0"), ("target_arch", "arm64"), ("cpu_policy", "v3"),
                             ("mode", "4755"), ("uid", True), ("gid", 1000), ("go_version", "1.27.1"),
                             ("path", "/usr/local/bin/hhpt"), ("sha256", "0" * 64)):
            with self.subTest(field=field):
                record = json.loads(original)
                record["application"][field] = value
                fixture.write_json(path, record)
                with self.assertRaises(ValueError):
                    self.inspect()
        path.write_bytes(original)
        self.inspect()
        binding_path = self.candidate / (image + ".application.json")
        binding = sources.read_json(binding_path)
        binding["host_manifest"]["sha256"] = "0" * 64
        fixture.write_json(binding_path, binding)
        with self.assertRaises(ValueError):
            self.inspect()

    def test_firmware_build_receipt_does_not_replace_finished_layer_checks(self):
        image = release.image_name(self.version, "x86_64")
        path = self.candidate / (image + ".inspection.json")
        original = path.read_bytes()
        for change in (lambda row: row["firmware"].__setitem__("layers", {}),
                       lambda row: row["firmware"]["layers"]["initramfs"]["checked_paths"].pop(),
                       lambda row: row["firmware"]["layers"]["modloop"]["checked_sha256"].pop(),
                       lambda row: row["firmware"]["layers"]["modloop"]["found_sha256"].append("a" * 64)):
            record = json.loads(original)
            change(record)
            fixture.write_json(path, record)
            with self.assertRaises(ValueError):
                self.inspect()
        path.write_bytes(original)
        receipt_path = self.candidate / (image + ".firmware-exclusions.json")
        receipt = sources.read_json(receipt_path)
        receipt["removed"]["modloop"].pop()
        receipt["counts"]["modloop"] -= 1
        fixture.write_json(receipt_path, receipt)
        record = sources.read_json(path)
        record["firmware"]["receipt_sha256"] = sources.digest(receipt_path)
        fixture.write_json(path, record)
        with self.assertRaises(ValueError):
            self.inspect()

    def test_legacy_requires_386_softfloat_and_has_no_alpine_firmware_claim(self):
        self.inspect("legacy-x86")
        image = release.image_name(self.version, "legacy-x86")
        path = self.candidate / (image + ".inspection.json")
        value = sources.read_json(path)
        value["application"]["cpu_policy"] = "sse2"
        fixture.write_json(path, value)
        with self.assertRaises(ValueError):
            self.inspect("legacy-x86")

    def test_stale_image_or_project_binding_cannot_be_relabelled(self):
        image = release.image_name(self.version, "aarch64")
        path = self.candidate / (image + ".application.json")
        original = path.read_bytes()
        for field, value in (("source_commit", "b" * 40), ("project_files_sha256", "c" * 64), ("version", "0.1.0-dev.0")):
            record = json.loads(original)
            record[field] = value
            fixture.write_json(path, record)
            with self.assertRaises(ValueError):
                self.inspect("aarch64")

    def test_spdx_must_match_inventory_and_image_not_just_header_text(self):
        image = release.image_name(self.version, "x86")
        path = self.candidate / (image + ".spdx")
        inventory = sources.inventories(self.candidate, self.version)["x86"]
        original = path.read_bytes()
        for data in (b"SPDXVersion: SPDX-2.3\nPackageName: fixture\n",
                     original.replace(b"PackageVersion: 2.3", b"PackageVersion: 9.9"),
                     original.replace(sources.digest(self.candidate / image).encode(), b"0" * 64)):
            with self.assertRaises(ValueError):
                sources.validate_sbom(data, inventory, "x86", image, self.version, sources.digest(self.candidate / image))

    def test_spdx_preserves_declarations_upstream_provenance_and_relationships(self):
        for arch in ("x86", "legacy-x86"):
            image = release.image_name(self.version, arch)
            original = (self.candidate / (image + ".spdx")).read_bytes()
            inventory = sources.inventories(self.candidate, self.version)[arch]
            relationship = b"GENERATED_FROM" if arch == "x86" else b"CONTAINS"
            changes = [original.replace(b"PackageLicenseDeclared: BSD-3-Clause", b"PackageLicenseDeclared: NOASSERTION"),
                       original.replace(b"PackageLicenseDeclared: BSD-3-Clause\n", b"", 1),
                       original.replace(b"PackageLicenseDeclared: NOASSERTION\n", b"", 1),
                       original.replace(b"PackageLicenseConcluded: NOASSERTION", b"PackageLicenseConcluded: MIT"),
                       original.replace(b"PackageComment:", b"DiscardedComment:", 1),
                       original.replace(b"DESCRIBES", b"CONTAINS"),
                       original.replace(b"SPDXRef-Image " + relationship, b"SPDXRef-DOCUMENT " + relationship)]
            if arch == "x86":
                changes += [original.replace(b"arch=x86", b"arch=aarch64"),
                            original.replace(b"origin=fixture-library", b"origin=different-source"),
                            original.replace(b"source-commit=" + b"b" * 40, b"source-commit=" + b"c" * 40),
                            original.replace(b"declared-license=MIT", b"declared-license=Unknown")]
            else:
                changes += [original.replace(b"Buildroot declared licenses: MIT", b"Buildroot declared licenses: Unknown"),
                            original.replace(b"license files: COPYING", b"license files: MISSING")]
            for number, data in enumerate(changes):
                self.assertNotEqual(data, original)
                with self.subTest(arch=arch, mutation=number), self.assertRaises(ValueError):
                    sources.validate_sbom(data, inventory, arch, image, self.version, sources.digest(self.candidate / image))


if __name__ == "__main__":
    unittest.main()
