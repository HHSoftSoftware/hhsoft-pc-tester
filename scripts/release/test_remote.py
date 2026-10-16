import copy
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock

import remote


class RemoteReleaseTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.tag = "v0.1.0-alpha.1"
        self.commit = "a" * 40
        self.tag_object = "b" * 40
        self.environment = {
            "GITHUB_REPOSITORY": remote.REPOSITORY,
            "GITHUB_EVENT_NAME": "workflow_dispatch",
            "GITHUB_REF": "refs/heads/main",
            "GITHUB_REF_PROTECTED": "true",
            "GITHUB_SHA": self.commit,
            "RELEASES_ENABLED": "true",
            "RELEASE_SIGNING_KEY_FINGERPRINT": "C" * 40,
        }
        self.addCleanup(mock.patch.stopall)
        mock.patch.dict(os.environ, self.environment).start()

    def tag_responses(self):
        return [
            {"object": {"type": "tag", "sha": self.tag_object}},
            {"verification": {"verified": True}, "object": {"type": "commit", "sha": self.commit}},
            {"object": {"type": "commit", "sha": self.commit}},
        ]

    def raw_tag(self):
        return (f"object {self.commit}\ntype commit\ntag {self.tag}\n"
                "tagger HHSoft Software <info.hhsoft@gmail.com> 1700000000 +0000\n\n"
                "Release test fixture\n-----BEGIN PGP SIGNATURE-----\nfixture\n"
                "-----END PGP SIGNATURE-----\n").encode("ascii")

    def local_responses(self):
        return [self.commit.encode() + b"\n", self.tag_object.encode() + b"\n", self.raw_tag()]

    def saved_state(self):
        self.candidate = self.root / "candidate"
        self.candidate.mkdir()
        asset = self.candidate / "example.iso"
        asset.write_bytes(b"validated fixture bytes")
        self.state = {"id": 10, "tag": self.tag, "tag_object": self.tag_object,
                      "assets": [{"id": 11, "name": asset.name, "size": asset.stat().st_size,
                                  "digest": "sha256:" + remote.digest(asset)}]}
        self.state_file = self.root / "state.json"
        self.state_file.write_text(json.dumps(self.state))
        current = copy.deepcopy(self.state)
        current.pop("tag_object")
        return current

    def test_only_enabled_protected_main_dispatch_can_control_release(self):
        remote.verify_context(self.commit)
        for field, value in (("GITHUB_REPOSITORY", "example/fork"),
                             ("GITHUB_EVENT_NAME", "push"),
                             ("GITHUB_REF", "refs/tags/" + self.tag),
                             ("GITHUB_REF_PROTECTED", "false"),
                             ("GITHUB_SHA", "d" * 40),
                             ("RELEASES_ENABLED", "false")):
            with self.subTest(field=field), mock.patch.dict(os.environ, {field: value}):
                with self.assertRaises(ValueError):
                    remote.verify_context(self.commit)

    def test_current_source_and_authorized_signature_are_both_required(self):
        def inspect_signature(payload, signature, public_key, fingerprint):
            self.assertEqual(payload.read_bytes(), self.raw_tag().split(b"-----BEGIN PGP SIGNATURE-----")[0])
            self.assertTrue(signature.read_bytes().startswith(b"-----BEGIN PGP SIGNATURE-----\n"))
            self.assertEqual(public_key, self.root / "security/release-signing-key.asc")
            self.assertEqual(fingerprint, self.environment["RELEASE_SIGNING_KEY_FINGERPRINT"])
        with mock.patch.object(remote, "api", side_effect=self.tag_responses()), \
             mock.patch.object(remote, "git", side_effect=self.local_responses()), \
             mock.patch.object(remote, "verify_detached", side_effect=inspect_signature) as verifier:
            self.assertEqual(remote.verify_tag(self.tag, self.commit, self.root), self.tag_object)
            verifier.assert_called_once()

    def test_github_verified_signature_from_other_key_is_rejected(self):
        with mock.patch.object(remote, "api", side_effect=self.tag_responses()), \
             mock.patch.object(remote, "git", side_effect=self.local_responses()), \
             mock.patch.object(remote, "verify_detached", side_effect=ValueError("wrong signing key")):
            with self.assertRaises(ValueError):
                remote.verify_tag(self.tag, self.commit, self.root)

    def test_older_main_ancestor_cannot_control_release(self):
        responses = self.tag_responses()
        responses[-1]["object"]["sha"] = "d" * 40
        with mock.patch.object(remote, "api", side_effect=responses), \
             mock.patch.object(remote, "git") as git, self.assertRaises(ValueError):
            remote.verify_tag(self.tag, self.commit, self.root)
        git.assert_not_called()

    def test_local_tag_swap_or_different_signed_name_is_rejected(self):
        for position, value in ((0, b"d" * 40), (1, b"d" * 40),
                                (2, self.raw_tag().replace(self.tag.encode(), b"v2.0.0"))):
            responses = self.local_responses()
            responses[position] = value
            with self.subTest(position=position), \
                 mock.patch.object(remote, "api", side_effect=self.tag_responses()), \
                 mock.patch.object(remote, "git", side_effect=responses), \
                 mock.patch.object(remote, "verify_detached") as verifier, self.assertRaises(ValueError):
                remote.verify_tag(self.tag, self.commit, self.root)
            verifier.assert_not_called()

    def test_git_replacement_objects_are_disabled(self):
        with mock.patch.object(remote.subprocess, "check_output", return_value=b"raw") as call:
            remote.git("cat-file", "tag", self.tag_object)
        self.assertEqual(call.call_args.args[0][:2], ["git", "--no-replace-objects"])

    def test_missing_or_multiple_tag_signatures_are_rejected(self):
        for value in (b"unsigned\n", self.raw_tag() * 2):
            with self.subTest(value=len(value)), self.assertRaises(ValueError):
                remote.verify_tag_signature(value, self.root)

    def test_saved_release_and_tag_object_cannot_be_replaced(self):
        current = self.saved_state()
        for field, value in (("id", 12), ("tag", "v2.0.0")):
            changed = copy.deepcopy(current)
            changed[field] = value
            with self.subTest(field=field), mock.patch.object(remote, "release_state", return_value=changed):
                with self.assertRaises(ValueError):
                    remote.check_state("uploaded", self.tag, self.tag_object, self.state_file, self.candidate)
        with mock.patch.object(remote, "release_state", return_value=current), self.assertRaises(ValueError):
            remote.check_state("uploaded", self.tag, "d" * 40, self.state_file, self.candidate)

    def test_published_postcondition_checks_all_remote_bytes(self):
        current = self.saved_state()
        with mock.patch.object(remote, "release_state", return_value=current) as state:
            remote.check_state("published", self.tag, self.tag_object, self.state_file, self.candidate)
            state.assert_called_once_with(self.tag, published=True)
        for field, value in (("size", 999), ("digest", None), ("digest", "sha256:" + "0" * 64),
                             ("name", "unexpected.iso")):
            changed = copy.deepcopy(current)
            changed["assets"][0][field] = value
            with self.subTest(field=field, value=value), \
                 mock.patch.object(remote, "release_state", return_value=changed), self.assertRaises(ValueError):
                remote.check_state("published", self.tag, self.tag_object, self.state_file, self.candidate)

    def test_publication_requires_immutable_release_and_correct_classification(self):
        published = {"id": 10, "tag_name": self.tag, "draft": False,
                     "immutable": True, "published_at": "2026-10-15T00:00:00Z", "prerelease": True}
        with mock.patch.object(remote, "api", side_effect=[[published], []]):
            remote.release_state(self.tag, published=True)
        for field, value in (("draft", True), ("immutable", False), ("immutable", None),
                             ("published_at", None), ("prerelease", False)):
            changed = dict(published, **{field: value})
            with self.subTest(field=field), mock.patch.object(remote, "api", return_value=[changed]), \
                 self.assertRaises(ValueError):
                remote.release_state(self.tag, published=True)

    def test_snapshot_will_not_overwrite_previous_approval_state(self):
        current = self.saved_state()
        with mock.patch.object(remote, "expected_candidate", return_value={"example.iso"}), \
             mock.patch.object(remote, "release_state", return_value=current), self.assertRaises(FileExistsError):
            remote.check_state("snapshot", self.tag, self.tag_object, self.state_file)


if __name__ == "__main__":
    unittest.main()
