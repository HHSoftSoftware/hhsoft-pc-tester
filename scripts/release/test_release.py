import hashlib
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

import release
import remote


class ReleaseGateTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.candidate = self.root / "candidate"
        self.candidate.mkdir()
        self.tag = "v0.1.0-alpha.1"
        self.version = "0.1.0-alpha.1"
        self.commit = "a" * 40
        (self.root / "VERSION").write_text(self.version + "\n")
        for platform in ("alpine", "buildroot"):
            lock = self.root / f"platform/{platform}/base.lock"
            lock.parent.mkdir(parents=True)
            lock.write_text("fixture lock\n")
        records = {}
        for arch in release.ARCHES:
            name = release.image_name(self.version, arch)
            # Synthetic ISO header only: tests exercise publication validation,
            # not bootability. Real release records come from the lab.
            (self.candidate / name).write_bytes(b"\0" * 32769 + b"CD001" + b"\0" * 2048)
            (self.candidate / (name + ".spdx")).write_text("SPDXVersion: SPDX-2.3\nPackageName: fixture\n")
            suffix = ".packages.csv" if arch == "legacy-x86" else ".packages.lock"
            (self.candidate / (name + suffix)).write_text("fixture inventory\n")
            def result(check):
                detail = {"schema": 1, "source_commit": self.commit,
                          "image_sha256": release.digest(self.candidate / name), "check": check,
                          "status": "pass", "tool": "synthetic test fixture", "tool_version": "1",
                          "output": "Fixture only; no real boot or security validation."}
                record_hash = release.record_digest(detail)
                records[record_hash] = detail
                return {"status": "pass", "evidence_sha256": record_hash}
            validation = {"schema": 1, "source_commit": self.commit,
                          "image_sha256": release.digest(self.candidate / name),
                          "boot": {mode: result("boot:" + mode) for mode in release.BOOT_MODES[arch]},
                          "reviews": {gate: result("review:" + gate) for gate in release.REVIEW_GATES}}
            (self.candidate / (name + ".validation.json")).write_text(json.dumps(validation))
        (self.candidate / "lab-evidence.json").write_text(json.dumps({"schema": 1, "records": records}))
        for name in ("THIRD-PARTY-NOTICES.txt", "SOURCE-OFFER.md"):
            (self.candidate / name).write_text("Fixture review content\n")
        self.reseal()
        signature = mock.patch.object(release, "verify_detached")
        self.verifier = signature.start()
        self.addCleanup(signature.stop)

    def reseal(self):
        evidence = self.candidate / "release-evidence.json"
        evidence.unlink(missing_ok=True)
        (self.candidate / "release-evidence.json.asc").unlink(missing_ok=True)
        release.seal(self.candidate, self.tag, self.commit, self.root)
        (self.candidate / "release-evidence.json.asc").write_text("test signature; verifier mocked")

    def check(self):
        return release.validate(self.candidate, self.tag, self.commit, self.root)

    def test_complete_reviewed_candidate_passes(self):
        self.check()
        self.verifier.assert_called_once()

    def test_invalid_signature_is_rejected(self):
        self.verifier.side_effect = ValueError("invalid signature")
        with self.assertRaises(ValueError):
            self.check()

    def test_missing_or_forged_evidence_record_cannot_be_sealed(self):
        path = self.candidate / "lab-evidence.json"
        lab = json.loads(path.read_text())
        record_hash = next(iter(lab["records"]))
        lab["records"][record_hash]["output"] = "forged result"
        path.write_text(json.dumps(lab))
        with self.assertRaises(ValueError):
            self.reseal()
        del lab["records"][record_hash]
        path.write_text(json.dumps(lab))
        with self.assertRaises(ValueError):
            self.reseal()

    def test_checksum_manifest_covers_utility_provenance(self):
        for name in release.companion_names(self.version) | {"utilities.intoto.jsonl"}:
            (self.candidate / name).write_text("fixture download")
        release.manifest(self.candidate, self.tag, self.commit, self.root)
        manifest = (self.candidate / "SHA256SUMS").read_text()
        self.assertIn(release.digest(self.candidate / "utilities.intoto.jsonl") + "  utilities.intoto.jsonl", manifest)
        self.assertNotIn("checksums.intoto.jsonl", manifest)

    def test_missing_iso_blocks_utilities_only_release(self):
        (self.candidate / release.image_name(self.version, "aarch64")).unlink()
        with self.assertRaises(ValueError):
            self.check()

    def test_tampered_image_is_rejected(self):
        path = self.candidate / release.image_name(self.version, "x86_64")
        with path.open("ab") as stream:
            stream.write(b"changed")
        with self.assertRaises(ValueError):
            self.check()

    def test_wrong_source_or_lock_is_rejected(self):
        with self.assertRaises(ValueError):
            release.validate(self.candidate, self.tag, "b" * 40, self.root)
        (self.root / "platform/alpine/base.lock").write_text("different inputs")
        with self.assertRaises(ValueError):
            self.check()

    def test_extra_private_asset_is_rejected(self):
        (self.candidate / "AGENTS.md").write_text("local instructions")
        with self.assertRaises(ValueError):
            self.check()

    def test_missing_boot_mode_or_pending_review_cannot_be_sealed(self):
        path = self.candidate / (release.image_name(self.version, "x86_64") + ".validation.json")
        record = json.loads(path.read_text())
        record["boot"].pop("uefi")
        path.write_text(json.dumps(record))
        with self.assertRaises(ValueError):
            self.reseal()
        self.assertFalse((self.candidate / "release-evidence.json").exists())

    def test_false_image_secret_review_is_rejected(self):
        path = self.candidate / (release.image_name(self.version, "x86") + ".validation.json")
        record = json.loads(path.read_text())
        record["reviews"]["image-secret-scan"]["status"] = "pending"
        path.write_text(json.dumps(record))
        with self.assertRaises(ValueError):
            self.reseal()

    def test_development_and_malformed_tags_cannot_publish(self):
        for tag in ("v0.1.0-dev.0", "0.1.0", "v01.0.0", "v0.1.0-alpha.01", "v1.0.0+build", "../v1.0.0"):
            with self.subTest(tag=tag), self.assertRaises(ValueError):
                release.version_for(tag)

    def test_only_alpha_can_omit_physical_qualification(self):
        for version in ("0.1.0", "0.1.0-beta.1", "0.1.0-rc.1", "0.1.0-beta.foo-alpha.1",
                        "0.1.0-rc.foo-alpha.1"):
            with self.subTest(version=version):
                release.version_for("v" + version)
                self.assertIn("physical-qualification", release.required_review_gates(version))
        self.assertNotIn("physical-qualification", release.required_review_gates("0.1.0-alpha.1"))

    def test_duplicate_json_fields_fail(self):
        path = self.candidate / "release-evidence.json"
        path.write_text('{"schema":1,"schema":1}')
        with self.assertRaises(ValueError):
            self.check()

    def test_unsigned_tag_is_rejected(self):
        responses = [{"object": {"type": "tag", "sha": "c" * 40}},
                     {"verification": {"verified": False}, "object": {"type": "commit", "sha": self.commit}}]
        with mock.patch.object(remote, "api", side_effect=responses), self.assertRaises(ValueError):
            remote.verify_tag(self.tag, self.commit)

    def test_tag_moved_to_another_commit_is_rejected(self):
        responses = [{"object": {"type": "tag", "sha": "c" * 40}},
                     {"verification": {"verified": True}, "object": {"type": "commit", "sha": "b" * 40}}]
        with mock.patch.object(remote, "api", side_effect=responses), self.assertRaises(ValueError):
            remote.verify_tag(self.tag, self.commit)

    def test_drafts_use_paginated_lists_and_reject_published_release(self):
        draft = {"id": 10, "draft": True, "tag_name": self.tag}
        asset = {"id": 11, "name": "example.iso", "size": 42, "digest": "sha256:" + "a" * 64}
        with mock.patch.object(remote, "api", side_effect=[[draft], [asset]]) as api:
            state = remote.release_state(self.tag)
            self.assertEqual(state["assets"], [asset])
            api.assert_any_call("releases?per_page=100", paginate=True)
            api.assert_any_call("releases/10/assets?per_page=100", paginate=True)
        draft["draft"] = False
        with mock.patch.object(remote, "api", return_value=[draft]), self.assertRaises(ValueError):
            remote.release_state(self.tag)


if __name__ == "__main__":
    unittest.main()
