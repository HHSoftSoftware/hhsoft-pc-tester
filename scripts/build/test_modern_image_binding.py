"""Requirements tests for source identity and fresh modern application inputs."""

import hashlib
import json
import os
from pathlib import Path
import subprocess
import struct
import sys
import tempfile
import unittest
from unittest import mock
import zlib

import modern_image_binding as binding


SOURCE = Path(__file__).resolve().parents[2]
NATIVE = sys.platform.startswith("linux") and Path("/usr/bin/git").is_file()


def elf(architecture):
    result = bytearray(64)
    elf_class, machine = {"x86": (1, 3), "x86_64": (2, 62), "aarch64": (2, 183)}[architecture]
    result[:7] = b"\x7fELF" + bytes((elf_class, 1, 1))
    result[16:20] = (2).to_bytes(2, "little") + machine.to_bytes(2, "little")
    return bytes(result) + b"synthetic compiled application; never executed\n"


class PortablePolicyTests(unittest.TestCase):
    def test_raw_tree_rejects_links_submodules_private_paths_and_duplicates(self):
        for mode, name in ((b"120000", b"linked"), (b"160000", b"submodule"),
                           (b"100644", b"../escape"), (b"100644", b"fixture.key")):
            with self.subTest(mode=mode, name=name), self.assertRaises(ValueError):
                binding.tree_files("a" * 40, lambda *_: mode + b" " + name + b"\0" + bytes(20))
        member = b"100644 go.mod\0" + bytes(20)
        with self.assertRaises(ValueError):
            binding.tree_files("a" * 40, lambda *_: member + member)

    def test_rejected_cli_arguments_are_redacted(self):
        token = "fixture-private-source-location"
        result = subprocess.run([sys.executable, "-I", "-B", str(Path(binding.__file__)), "verify",
                                 "--unsupported", token], capture_output=True, text=True, timeout=10)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn(token, result.stdout + result.stderr)
        self.assertNotIn("Traceback", result.stderr)


@unittest.skipUnless(NATIVE, "source binding requires native Linux descriptors and Git")
class SourceBindingTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="hhpt-modern-binding-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.repository = self.root / "repository"
        self.repository.mkdir()
        self.work = self.root / "work"
        self.work.mkdir()
        self.contents = {}
        for relative in binding.POLICIES:
            self.contents[relative] = (SOURCE / relative).read_bytes()
        self.contents.update({"VERSION": b"0.1.0-dev.0\n", ".go-version": b"1.27.2\n",
                              "cmd/hhpt/main.go": b"package main\nfunc main() {}\n",
                              "internal/plan/plan.go": b"package plan\n",
                              "internal/plan/definitions.json": b'{"fixture":true}\n',
                              ".gitignore": b"build/\ncmd/hhpt/ignored.go\n"})
        for relative, content in self.contents.items():
            self.write(relative, content)
        (self.repository / ".git/objects").mkdir(parents=True)
        (self.repository / ".git/refs").mkdir()
        self.commit_source()

    def write(self, relative, content):
        path = self.repository / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(content)
        return path

    def object(self, kind, content):
        oid = binding.blob_oid(kind, content)
        destination = self.repository / ".git/objects" / oid[:2] / oid[2:]
        destination.parent.mkdir(exist_ok=True)
        destination.write_bytes(zlib.compress(kind.encode() + b" " + str(len(content)).encode() + b"\0" + content))
        return oid

    def commit_source(self):
        # Construct only disposable fixture objects; never run git commit or
        # change the project's index, references, configuration, or history.
        tree = {}
        for relative in self.contents:
            parent = tree
            parts = relative.split("/")
            for part in parts[:-1]:
                parent = parent.setdefault(part, {})
            parent[parts[-1]] = (self.repository / relative).read_bytes()

        def emit(directory):
            entries = []
            for name, value in sorted(directory.items()):
                mode = b"40000" if isinstance(value, dict) else b"100644"
                oid = emit(value) if isinstance(value, dict) else self.object("blob", value)
                entries.append(mode + b" " + name.encode() + b"\0" + bytes.fromhex(oid))
            return self.object("tree", b"".join(entries))

        self.tree = emit(tree)
        self.commit = self.object("commit", ("tree " + self.tree + "\n"
            "author HHSoft Software <info.hhsoft@gmail.com> 1700000000 +0000\n"
            "committer HHSoft Software <info.hhsoft@gmail.com> 1700000000 +0000\n\nSynthetic fixture\n").encode())
        (self.repository / ".git/HEAD").write_text(self.commit + "\n")

    def prepare(self, architecture="x86_64"):
        binding.prepare(self.repository, self.work, architecture)

    def compile_fixture(self, architecture="x86_64"):
        path = self.work / ("application-bin/linux_" + architecture + "/hhpt")
        path.parent.mkdir(parents=True)
        path.write_bytes(elf(architecture))
        path.chmod(0o755)
        return path

    def complete(self, architecture="x86_64"):
        binding.complete(self.repository, self.work, architecture)
        return json.loads((self.work / "application-binding.json").read_text())

    def test_old_prebuilt_poison_is_not_a_source_or_binary_input(self):
        poison = self.write("build/bin/linux_x86_64/hhpt", b"poisoned old executable\n")
        poison.chmod(0o755)
        self.prepare()
        self.assertFalse((self.work / "application-source/build").exists())
        self.compile_fixture()
        record = self.complete()
        binding.verify(self.repository, self.work, "x86_64")
        self.assertEqual(record["source"]["commit"], self.commit)
        self.assertEqual(record["source"]["tree"], self.tree)
        self.assertEqual(record["binary"]["sha256"], hashlib.sha256(elf("x86_64")).hexdigest())
        self.assertEqual(record["binary"]["filename"], "hhpt")
        self.assertEqual(record["binary"]["installed_path"], "/usr/bin/hhpt")
        self.assertEqual(record["host"], {"status": "unqualified-development", "manifest_sha256": None})
        self.assertEqual(record["qualification"], "not_established")
        self.assertNotIn(str(self.root), json.dumps(record))

    def test_each_requested_target_and_policy_are_bound(self):
        for architecture in ("x86", "x86_64", "aarch64"):
            with self.subTest(architecture=architecture):
                self.work = self.root / architecture
                self.work.mkdir()
                self.prepare(architecture)
                self.compile_fixture(architecture)
                record = self.complete(architecture)
                self.assertEqual(record["architecture"], architecture)
                self.assertEqual(record["version"], "0.1.0-dev.0")
                self.assertEqual(record["go_version"], "1.27.2")
                self.assertEqual(set(record["policy"]), set(binding.POLICIES))
                with self.assertRaises(ValueError):
                    binding.verify(self.repository, self.work, "aarch64" if architecture != "aarch64" else "x86")

    def test_dirty_source_and_ignored_compile_inputs_fail_before_compilation(self):
        self.write("cmd/hhpt/ignored.go", b"package main\n")
        with self.assertRaises(ValueError):
            self.prepare()
        (self.repository / "cmd/hhpt/ignored.go").unlink()
        self.write("cmd/hhpt/main.go", b"package main\nfunc main() { panic(1) }\n")
        with self.assertRaises(ValueError):
            self.prepare()
        self.assertFalse((self.work / "application-inputs.json").exists())

    def test_changed_original_and_copied_source_are_rejected(self):
        self.prepare()
        self.compile_fixture()
        original = self.repository / "internal/plan/definitions.json"
        original.write_bytes(b'{"changed":true}\n')
        with self.assertRaises(ValueError):
            self.complete()
        original.write_bytes(self.contents["internal/plan/definitions.json"])
        (self.work / "application-source/internal/plan/definitions.json").write_bytes(b'{"changed":true}\n')
        with self.assertRaises(ValueError):
            self.complete()

    def test_wrong_machine_and_changed_final_binary_are_rejected(self):
        self.prepare()
        binary = self.compile_fixture()
        binary.write_bytes(elf("aarch64"))
        with self.assertRaises(ValueError):
            self.complete()
        binary.write_bytes(elf("x86_64"))
        self.complete()
        binary.write_bytes(elf("x86_64") + b"changed")
        with self.assertRaises(ValueError):
            binding.verify(self.repository, self.work, "x86_64")

    def test_overlay_proof_is_required_and_must_match_installed_bytes(self):
        self.prepare()
        self.compile_fixture()
        record = self.complete()
        with self.assertRaises(OSError):
            binding.verify(self.repository, self.work, "x86_64", overlay=True)
        proof = self.work / "application-overlay.sha256"
        proof.write_text("0" * 64 + "\n")
        with self.assertRaises(ValueError):
            binding.verify(self.repository, self.work, "x86_64", overlay=True)
        proof.write_text(record["binary"]["sha256"] + "\n")
        binding.verify(self.repository, self.work, "x86_64", overlay=True)

    def test_history_rewriting_and_alternate_object_sources_are_rejected(self):
        for relative in ("shallow", "info/grafts", "refs/replace", "objects/info/alternates", "objects/info/http-alternates"):
            with self.subTest(relative=relative):
                path = self.write(".git/" + relative, b"synthetic unsupported metadata\n")
                with self.assertRaises(ValueError):
                    self.prepare()
                path.unlink()

    def test_local_config_cannot_execute_hooks_filters_or_fsmonitor(self):
        marker = self.root / "must-not-execute"
        hook = self.root / "fsmonitor-fixture"
        hook.write_text('#!/bin/sh\nprintf invoked > "' + str(marker) + '"\nprintf "fixture-token\\000"\n')
        hook.chmod(0o755)
        self.write(".git/config", ("[core]\nrepositoryformatversion = 0\nfsmonitor = " + str(hook) +
                                  "\nfsmonitorHookVersion = 2\nhooksPath = untrusted\n"
                                  "[filter \"fixture\"]\nclean = " + str(hook) + "\n").encode())
        # Supply a valid fixture index directly, without changing the project's
        # Git state or invoking write-capable Git commands. The positive control
        # runs the same ls-files query used by the original checkout scanner.
        index = b"DIRC" + struct.pack(">II", 2, len(self.contents))
        for relative in sorted(self.contents):
            path = self.repository / relative
            info, name = path.stat(), relative.encode()
            fields = (int(info.st_ctime), info.st_ctime_ns % 10**9,
                      int(info.st_mtime), info.st_mtime_ns % 10**9,
                      info.st_dev, info.st_ino, 0o100644, info.st_uid, info.st_gid, info.st_size)
            entry = struct.pack(">10I", *(value & 0xffffffff for value in fields))
            entry += bytes.fromhex(binding.blob_oid("blob", path.read_bytes())) + struct.pack(">H", len(name))
            entry += name + b"\0"
            index += entry + b"\0" * (-len(entry) % 8)
        self.write(".git/index", index + hashlib.sha1(index).digest())
        result = subprocess.run(["/usr/bin/git", "-C", str(self.repository), "ls-files", "-z",
                                 "--cached", "--others", "--exclude-standard"], capture_output=True,
                                env={"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "HOME": "/dev/null",
                                     "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": "/dev/null"}, timeout=10)
        self.assertEqual(result.returncode, 0)
        self.assertTrue(marker.exists(), "positive control must prove that original-checkout Git executes the hook")
        marker.unlink()
        self.prepare()
        self.assertFalse(marker.exists())
        self.assertNotIn(".git/config", json.loads((self.work / "application-inputs.json").read_text())["source"]["files"])

    def test_gitless_exports_and_reference_fifo_fail_closed(self):
        head = self.repository / ".git/HEAD"
        head.unlink()
        os.mkfifo(head)
        result = subprocess.run([sys.executable, "-I", "-B", str(Path(binding.__file__)), "prepare",
                                 "--repository", str(self.repository), "--work", str(self.work), "--arch", "x86_64"],
                                capture_output=True, text=True, timeout=10)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("Traceback", result.stderr)
        self.assertNotIn(str(self.root), result.stdout + result.stderr)
        archive_only = self.root / "export"
        archive_only.mkdir()
        with self.assertRaises(OSError):
            binding.prepare(archive_only, self.work, "x86_64")

    def test_source_and_object_limits_reject_before_receipt(self):
        with mock.patch.object(binding, "MAX_GIT_BYTES", 1), self.assertRaises(ValueError):
            self.prepare()
        with mock.patch.object(binding, "MAX_SOURCE_BYTES", 1), self.assertRaises(ValueError):
            self.prepare()
        self.assertFalse((self.work / "application-inputs.json").exists())

    def test_source_links_and_unrelated_executed_policy_are_rejected(self):
        source = self.repository / "cmd/hhpt/main.go"
        source.unlink()
        source.symlink_to(self.repository / "internal/plan/plan.go")
        with self.assertRaises(ValueError):
            self.prepare()
        source.unlink()
        source.write_bytes(self.contents["cmd/hhpt/main.go"])
        self.write("scripts/build/builder_host_manifest.py", b"# different committed policy\n")
        self.commit_source()
        with self.assertRaises(ValueError):
            self.prepare()

    def test_nondevelopment_record_requires_successful_actual_host_comparison(self):
        self.write("VERSION", b"0.1.0-alpha.1\n")
        manifest = "platform/builder-hosts/alpine-image-x86_64.json"
        self.contents[manifest] = b'{"synthetic":"reviewed fixture only"}\n'
        self.write(manifest, self.contents[manifest])
        self.commit_source()
        with mock.patch.object(binding.host, "actual_host_architecture", return_value="x86_64"), \
                mock.patch.object(binding.host, "verify_manifest", side_effect=ValueError("host changed")):
            with self.assertRaises(ValueError):
                self.prepare()
        self.assertFalse((self.work / "application-inputs.json").exists())


if __name__ == "__main__":
    unittest.main()
