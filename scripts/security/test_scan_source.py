import importlib.util
import io
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest import mock

import publication_check

SCRIPT = Path(__file__).with_name("scan_source.py")
spec = importlib.util.spec_from_file_location("scan_source", SCRIPT)
scanner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(scanner)


def pinned_gitleaks():
    candidates = [os.environ.get("HHPT_GITLEAKS"),
                  str(Path(os.environ["RUNNER_TEMP"]) / "gitleaks/gitleaks") if os.environ.get("RUNNER_TEMP") else None,
                  str(SCRIPT.resolve().parents[2] / "build/tools/gitleaks/gitleaks.exe")]
    return next((Path(name).resolve() for name in candidates if name and Path(name).is_file()), None)


GITLEAKS = pinned_gitleaks()
AGENT_VARIANTS = (
    "AGENTS.md.bak", "nested/SKILL.private.md", "nested/AgEnTs.LoCaL.MD.Backup",
    "nested/agents_saved.md", "nested/AGENTS-old.MD", "CLAUDE.md~",
    ".SKILL.private.md.swp", "#GEMINI.md#", "nested/GeMiNi.private.md.bak",
    ".github/copilot-instructions.MD.old", "nested/review.Instructions.private.MD.bak",
    "nested/.CuRsOrRuLeS.backup", "nested/.WindSurfRules~", "nested/.AiDeR.conf.bak",
    "nested/.GeMiNi/context.md", "nested/.GiThUb/AgEnTs/review.md",
)


class PublicationScanTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.git("init", "-q")
        self.git("config", "user.name", "Fixture")
        self.git("config", "user.email", "fixture@example.invalid")
        self.git("config", "commit.gpgsign", "false")
        self.git("config", "core.autocrlf", "false")
        (self.root / ".gitignore").write_text(".hhpt-source-denylist\nAGENTS.md\n__pycache__/\n")
        (self.root / "source.txt").write_text("public source\n")

    def git(self, *args, input=None):
        return subprocess.run(["git", "-C", str(self.root), *args], check=True,
                              capture_output=True, input=input).stdout

    def run_scan(self, *args):
        return subprocess.run([sys.executable, str(SCRIPT), "--root", str(self.root), "--history", *args],
                              capture_output=True, text=True)

    def commit(self):
        self.git("add", ".")
        self.git("commit", "-qm", "Fixture")

    def test_clean_unborn_repository(self):
        self.assertEqual(self.run_scan().returncode, 0)

    def test_agent_paths_at_any_depth(self):
        for path in ("AGENTS.md", "nested/Agents.local.md", ".codex/config.toml",
                     "a/.github/agents/review.md", "docs/review.instructions.md",
                     "nested/CLAUDE.md", "skills/example/SKILL.md"):
            with self.subTest(path=path):
                self.assertTrue(scanner.forbidden_path(path))
        self.assertFalse(scanner.forbidden_path("docs/release-process.md"))

    def test_ignored_agent_is_local_only_but_forced_index_is_blocked(self):
        (self.root / "AGENTS.md").write_text("local instructions")
        self.assertEqual(self.run_scan().returncode, 0)
        self.git("add", "-f", "AGENTS.md")
        self.assertNotEqual(self.run_scan().returncode, 0)

    def test_agent_backups_and_variants_are_rejected_at_any_depth(self):
        for name in AGENT_VARIANTS:
            with self.subTest(name=name):
                self.assertTrue(scanner.forbidden_path(name))
        for name in ("docs/agent-design.md", "docs/skills-overview.md", "internal/agents.go"):
            with self.subTest(name=name):
                self.assertFalse(scanner.forbidden_path(name))

    def test_agent_variants_ignore_archive_and_history_guards_agree(self):
        project = SCRIPT.resolve().parents[2]
        for name in (".gitignore", ".gitattributes"):
            shutil.copyfile(project / name, self.root / name)
        for name in AGENT_VARIANTS:
            path = self.root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("local instruction fixture\n")
        names = ("\0".join(AGENT_VARIANTS) + "\0").encode()
        ignored = self.git("-c", "core.ignorecase=false", "check-ignore", "--no-index", "-z", "--stdin", input=names)
        self.assertEqual(set(ignored.decode().rstrip("\0").split("\0")), set(AGENT_VARIANTS))
        self.assertEqual(self.run_scan().returncode, 0)
        self.commit()
        self.git("add", "-f", "--", *AGENT_VARIANTS)
        result = self.run_scan()
        self.assertEqual(result.returncode, 1)
        for name in AGENT_VARIANTS:
            self.assertIn(name, result.stderr)
        self.git("commit", "-qm", "Fixture forced agent variants")
        with tarfile.open(fileobj=io.BytesIO(self.git("archive", "--format=tar", "HEAD"))) as archive:
            exported = set(archive.getnames())
        self.assertIn("source.txt", exported)
        self.assertFalse(exported.intersection(AGENT_VARIANTS))
        self.git("rm", "--", *AGENT_VARIANTS)
        self.commit()
        result = self.run_scan()
        self.assertEqual(result.returncode, 1)
        for name in AGENT_VARIANTS:
            self.assertIn(name, result.stderr)

    def test_staged_secret_hidden_by_clean_worktree_is_blocked(self):
        secret = "gh" + "p_" + "Q" * 40
        (self.root / "source.txt").write_text(secret)
        self.git("add", "source.txt")
        (self.root / "source.txt").write_text("clean now")
        result = self.run_scan()
        self.assertEqual(result.returncode, 1)
        self.assertNotIn(secret, result.stdout + result.stderr)

    def test_private_review_marker_blocks_renamed_worktree_index_and_history(self):
        marker = b"HHPT_PRIVATE_" + b"REVIEW_ONLY_V1"
        path = self.root / "renamed-review.txt"
        path.write_bytes(marker + b"\nInert review fixture\n")
        result = self.run_scan()
        self.assertEqual(result.returncode, 1)
        self.assertIn("private review artifact", result.stderr)
        self.assertNotIn(marker.decode(), result.stdout + result.stderr)
        self.git("add", "renamed-review.txt")
        path.write_text("clean worktree replacement\n")
        result = self.run_scan()
        self.assertEqual(result.returncode, 1)
        self.assertIn("index:renamed-review.txt", result.stderr)
        self.git("commit", "-qm", "Fixture staged review marker")
        self.git("rm", "-f", "renamed-review.txt")
        self.commit()
        result = self.run_scan()
        self.assertEqual(result.returncode, 1)
        self.assertIn("private review artifact", result.stderr)
        self.assertNotIn(marker.decode(), result.stdout + result.stderr)

    def test_review_directory_ignore_forced_index_history_and_archive_guards(self):
        project = SCRIPT.resolve().parents[2]
        for name in (".gitignore", ".gitattributes"):
            shutil.copyfile(project / name, self.root / name)
        name = "build/private-review/context.json"
        path = self.root / name
        path.parent.mkdir(parents=True)
        path.write_text('{"fixture": "inert"}\n')
        self.assertEqual(self.git("check-ignore", "--", name).decode().strip(), name)
        self.assertEqual(self.run_scan().returncode, 0)
        self.git("add", "-f", "--", name)
        result = self.run_scan()
        self.assertEqual(result.returncode, 1)
        self.assertIn("forbidden artifact", result.stderr)
        self.commit()
        with tarfile.open(fileobj=io.BytesIO(self.git("archive", "--format=tar", "HEAD"))) as archive:
            exported = set(archive.getnames())
        self.assertIn("source.txt", exported)
        self.assertFalse(any(item == "build" or item.startswith("build/") for item in exported))
        self.git("rm", "--", name)
        self.commit()
        result = self.run_scan()
        self.assertEqual(result.returncode, 1)
        self.assertIn("forbidden artifact", result.stderr)

    def test_private_research_variants_stay_ignored_and_cannot_enter_history_or_archives(self):
        project = SCRIPT.resolve().parents[2]
        for name in (".gitignore", ".gitattributes"):
            shutil.copyfile(project / name, self.root / name)
        names = ("_Reference_vendor/notes.txt", "nested/_reference_archive/data.txt",
                 "deep/nested/_REFERENCELOCAL/document.txt")
        for name in names:
            path = self.root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("private research fixture\n")
        ignored = self.git("-c", "core.ignorecase=false", "check-ignore", "--no-index", "-z", "--stdin",
                           input=("\0".join(names) + "\0").encode())
        self.assertEqual(set(ignored.decode().rstrip("\0").split("\0")), set(names))
        self.assertEqual(self.run_scan().returncode, 0)
        self.commit()
        self.git("add", "-f", "--", *names)
        result = self.run_scan()
        self.assertEqual(result.returncode, 1)
        self.assertIn("[redacted location]", result.stderr)
        for name in names:
            self.assertNotIn(name, result.stderr)
            self.assertNotIn(Path(name).parent.name, result.stderr)
        self.git("commit", "-qm", "Fixture forced private research")
        with tarfile.open(fileobj=io.BytesIO(self.git("archive", "--format=tar", "HEAD"))) as archive:
            exported = set(archive.getnames())
        self.assertIn("source.txt", exported)
        self.assertFalse(exported.intersection(names))
        self.git("rm", "--", *names)
        self.commit()
        result = self.run_scan()
        self.assertEqual(result.returncode, 1)
        self.assertIn("[redacted location]", result.stderr)
        for name in names:
            self.assertNotIn(name, result.stderr)
            self.assertNotIn(Path(name).parent.name, result.stderr)
        self.assertFalse(scanner.forbidden_path("docs/reference-guide.md"))

    def test_private_path_is_redacted_in_worktree_index_and_history(self):
        value = "private" + "-path-fixture"
        (self.root / ".hhpt-source-denylist").write_text(value)
        private_path = self.root / (value + ".txt")
        private_path.write_text("public content\n")
        self.commit()
        result = self.run_scan()
        self.assertEqual(result.returncode, 1)
        self.assertNotIn(value, result.stdout + result.stderr)
        self.assertIn("[redacted location]", result.stderr)
        self.git("rm", private_path.name)
        self.commit()
        result = self.run_scan()
        self.assertEqual(result.returncode, 1)
        self.assertNotIn(value, result.stdout + result.stderr)
        self.assertIn("[redacted location]", result.stderr)

    def test_path_diagnostics_escape_control_characters(self):
        checker = scanner.Scanner(self.root)
        checker.fail("fixture", "name\nline\x1b[31m")
        failure = next(iter(checker.failures))
        self.assertNotIn("\n", failure)
        self.assertNotIn("\x1b", failure)
        self.assertIn("\\n", failure)

    def test_host_absolute_and_traversing_paths_are_rejected_before_access(self):
        for name in ("/absolute", "C:/folder/file", "C:relative", "../file", "a/../file", "a\\file", "file:stream"):
            with self.subTest(name=name):
                self.assertTrue(scanner.forbidden_path(name))
                with self.assertRaises(RuntimeError):
                    scanner.worktree_path(self.root, name)

    def test_gitlinks_are_rejected_in_index_and_history(self):
        self.commit()
        oid = self.git("rev-parse", "HEAD").decode().strip()
        self.git("update-index", "--add", "--cacheinfo", f"160000,{oid},dependency")
        result = self.run_scan()
        self.assertEqual(result.returncode, 1)
        self.assertIn("non-regular or unmerged index entry", result.stderr)
        self.git("commit", "-qm", "Fixture gitlink")
        self.git("update-index", "--force-remove", "dependency")
        self.git("commit", "-qm", "Fixture cleanup")
        result = self.run_scan()
        self.assertEqual(result.returncode, 1)
        self.assertIn("non-regular history entry", result.stderr)

    def test_symlinks_are_rejected_in_index_and_history(self):
        self.commit()
        oid = self.git("hash-object", "-w", "--stdin", input=b"fixture-target").decode().strip()
        self.git("update-index", "--add", "--cacheinfo", f"120000,{oid},fixture-link")
        result = self.run_scan()
        self.assertEqual(result.returncode, 1)
        self.assertIn("non-regular or unmerged index entry", result.stderr)
        self.git("commit", "-qm", "Fixture symlink")
        self.git("update-index", "--force-remove", "fixture-link")
        self.git("commit", "-qm", "Fixture cleanup")
        result = self.run_scan()
        self.assertEqual(result.returncode, 1)
        self.assertIn("non-regular history entry", result.stderr)

    def test_worktree_ancestor_symlink_blocks_external_file_reads(self):
        directory = self.root / "tracked"
        directory.mkdir()
        (directory / "source.txt").write_text("public source\n")
        self.commit()
        with tempfile.TemporaryDirectory() as destination:
            (Path(destination) / "source.txt").write_text("gh" + "p_" + "Q" * 40)
            (directory / "source.txt").unlink()
            directory.rmdir()
            try:
                directory.symlink_to(destination, target_is_directory=True)
            except OSError:
                self.skipTest("filesystem symlinks are unavailable")
            result = self.run_scan()
            self.assertEqual(result.returncode, 1)
            self.assertIn("filesystem link", result.stderr)
            self.assertNotIn("credential:", result.stderr)

    def test_deleted_agent_file_is_still_blocked_in_history(self):
        (self.root / "AGENTS.md").write_text("local instructions")
        self.git("add", "-f", "AGENTS.md")
        self.commit()
        self.git("rm", "AGENTS.md")
        self.commit()
        self.assertEqual(self.run_scan().returncode, 1)

    def test_local_denylist_checks_history_and_commit_metadata(self):
        term = "private" + "-fixture-identity"
        (self.root / ".hhpt-source-denylist").write_text(term)
        self.git("add", ".")
        self.git("commit", "-qm", term)
        result = self.run_scan()
        self.assertEqual(result.returncode, 1)
        self.assertNotIn(term, result.stdout + result.stderr)

    def test_tag_metadata_is_checked(self):
        self.commit()
        term = "private" + "-tag-fixture"
        (self.root / ".hhpt-source-denylist").write_text(term)
        self.git("-c", "tag.gpgsign=false", "tag", "-a", "v0.0.1", "-m", term)
        self.assertEqual(self.run_scan().returncode, 1)

    def test_shallow_clone_is_incomplete(self):
        self.commit()
        clone = self.root / "shallow"
        subprocess.run(["git", "clone", "-q", "--depth=1", self.root.as_uri(), str(clone)], check=True)
        result = subprocess.run([sys.executable, str(SCRIPT), "--root", str(clone), "--history"],
                                capture_output=True)
        self.assertEqual(result.returncode, 2)

    def test_exact_outgoing_dangling_commit_is_scanned(self):
        self.commit()
        base = self.git("rev-parse", "HEAD").decode().strip()
        value = "gh" + "p_" + "Q" * 40
        (self.root / "source.txt").write_text(value)
        self.commit()
        outgoing = self.git("rev-parse", "HEAD").decode().strip()
        # Only this disposable fixture is reset; the project's repository is untouched.
        self.git("reset", "--hard", base)
        self.assertEqual(self.run_scan().returncode, 0)
        result = self.run_scan("--object", outgoing)
        self.assertEqual(result.returncode, 1)
        self.assertNotIn(value, result.stdout + result.stderr)

    def test_replacement_cannot_hide_original_history(self):
        self.commit()
        base = self.git("rev-parse", "HEAD").decode().strip()
        (self.root / "source.txt").write_text("gh" + "p_" + "Q" * 40)
        self.commit()
        original = self.git("rev-parse", "HEAD").decode().strip()
        (self.root / "source.txt").write_text("public source\n")
        self.commit()
        self.git("replace", original, base)
        self.assertEqual(self.run_scan().returncode, 1)

    def test_blob_and_tree_tags_fail_closed(self):
        self.commit()
        for expression in ("HEAD:source.txt", "HEAD^{tree}"):
            with self.subTest(expression=expression):
                oid = self.git("rev-parse", expression).decode().strip()
                self.git("-c", "tag.gpgsign=false", "tag", "fixture-object", oid)
                self.assertEqual(self.run_scan().returncode, 2)
                self.git("tag", "-d", "fixture-object")

    def test_nested_tag_metadata_is_scanned(self):
        self.commit()
        value = "private" + "-nested-tag-fixture"
        (self.root / ".hhpt-source-denylist").write_text(value)
        self.git("-c", "tag.gpgsign=false", "tag", "-a", "inner", "-m", value)
        self.git("-c", "tag.gpgsign=false", "tag", "-a", "outer", "inner", "-m", "Fixture")
        self.git("tag", "-d", "inner")
        self.assertEqual(self.run_scan().returncode, 1)

    def empty_tree_commit(self, name):
        """Create a reachable path without materializing it on the host filesystem."""
        raw = name.encode("utf-8") if isinstance(name, str) else name
        tree = self.git("mktree", input=b"").strip()
        for component in reversed(raw.split(b"/")):
            tree = self.git("mktree", "-z", input=b"040000 tree " + tree + b"\t" + component + b"\0").strip()
        parent = self.git("rev-parse", "HEAD").strip()
        commit = self.git("commit-tree", tree.decode("ascii"), "-p", parent.decode("ascii"),
                          "-m", "Fixture empty subtree").decode().strip()
        self.git("update-ref", "refs/heads/tree-fixture", commit)
        return commit

    def test_normal_directories_and_empty_trees_remain_allowed(self):
        (self.root / "docs").mkdir()
        (self.root / "docs/guide.txt").write_text("public source\n")
        self.commit()
        commit = self.empty_tree_commit("ordinary/nested")
        self.assertEqual(self.git("ls-tree", "-rz", "--full-tree", commit), b"")
        self.assertEqual(self.run_scan().returncode, 0)
        entries = list(scanner.tree_entries(self.root, commit))
        self.assertEqual([(mode, kind, name) for mode, kind, _, name in entries],
                         [("040000", "tree", "ordinary"), ("040000", "tree", "ordinary/nested")])

    def test_forbidden_empty_tree_paths_are_scanned(self):
        self.commit()
        for name in ("build", "nested/.codex", "nested/credentials", "_Reference_fixture",
                     "nested/AGENTS.md", "nested/keys.pem"):
            with self.subTest(name=name):
                self.empty_tree_commit(name)
                result = self.run_scan()
                self.assertEqual(result.returncode, 1)
                self.assertIn("forbidden artifact", result.stderr)

    def test_removed_private_empty_tree_is_redacted_in_history(self):
        self.commit()
        term = "private" + "-empty-tree-fixture"
        (self.root / ".hhpt-source-denylist").write_text(term)
        hidden = self.empty_tree_commit("ordinary/" + term)
        clean_tree = self.git("rev-parse", "HEAD^{tree}").decode().strip()
        cleaned = self.git("commit-tree", clean_tree, "-p", hidden,
                           "-m", "Fixture removed subtree").decode().strip()
        self.git("update-ref", "refs/heads/tree-fixture", cleaned)
        result = self.run_scan()
        self.assertEqual(result.returncode, 1)
        self.assertIn("[redacted location]", result.stderr)
        self.assertNotIn(term, result.stdout + result.stderr)

    def test_history_path_encoding_and_controls_fail_closed(self):
        self.commit()
        for name in (b"invalid-\xff", b"line\nbreak", b"tab\tname", b"delete\x7fname"):
            with self.subTest(name=name):
                self.empty_tree_commit(name)
                result = self.run_scan()
                self.assertEqual(result.returncode, 2)
                self.assertEqual(result.stderr.strip(), "source scan: inspection failed; publication blocked")
                with tempfile.TemporaryDirectory() as directory:
                    with self.assertRaises(RuntimeError):
                        publication_check.snapshot_history(self.root, Path(directory))

    def test_current_and_index_names_share_strict_encoding_policy(self):
        for name in (b"invalid-\xff", b"line\nbreak", b"delete\x7fname"):
            for domain in ("current", "index"):
                with self.subTest(name=name, domain=domain):
                    current = name + b"\0" if domain == "current" else b""
                    index = b"100644 " + b"a" * 40 + b" 0\t" + name + b"\0"
                    with mock.patch.object(scanner, "git", side_effect=[current, index]):
                        with self.assertRaises(RuntimeError):
                            scanner.Scanner(self.root).current()
        for name in ("line\nbreak", "tab\tname", "delete\x7fname", "bad\udcff"):
            self.assertFalse(scanner.safe_relative_name(name))

    def test_unicode_tree_path_metadata_preserves_literal_utf8(self):
        self.commit()
        name = "ordinary/caf\u00e9-\u6587\u4ef6"
        commit = self.empty_tree_commit(name)
        self.assertEqual(self.run_scan().returncode, 0)
        with tempfile.TemporaryDirectory() as directory:
            snapshot = Path(directory)
            publication_check.snapshot_history(self.root, snapshot)
            self.assertEqual((snapshot / "history/paths" / commit).read_bytes(),
                             ("ordinary\n" + name + "\n").encode("utf-8"))
        self.assertEqual(scanner.decode_source_name(name.encode("utf-8")), name)
        self.assertTrue(scanner.safe_relative_name(name))

    def generic_secret_path(self):
        value = "".join(("n7az5H0qk9", "DM2pw4vc8s", "Tf6JY1xb3L", "rNQ0UZd9Eo"))
        return "api_" + "key = " + value

    def test_snapshot_includes_generic_secret_only_in_empty_tree_name(self):
        self.commit()
        name = self.generic_secret_path()
        commit = self.empty_tree_commit(name)
        self.assertEqual(self.run_scan().returncode, 0)
        with tempfile.TemporaryDirectory() as directory:
            snapshot = Path(directory)
            publication_check.snapshot_history(self.root, snapshot)
            self.assertEqual((snapshot / "history/paths" / commit).read_bytes(), name.encode() + b"\n")

    @unittest.skipUnless(GITLEAKS, "pinned Gitleaks executable is unavailable")
    def test_gitleaks_rejects_generic_secret_only_in_empty_tree_name(self):
        self.commit()
        name = self.generic_secret_path()
        self.empty_tree_commit(name)
        self.assertEqual(self.run_scan().returncode, 0)
        result = subprocess.run(self.publication_command(), capture_output=True, text=True)
        self.assertEqual(result.returncode, 1)
        self.assertIn("Gitleaks found a secret", result.stderr)
        self.assertNotIn(name, result.stdout + result.stderr)

    @unittest.skipUnless(GITLEAKS, "pinned Gitleaks executable is unavailable")
    def test_gitleaks_scans_current_and_index_path_names(self):
        self.commit()
        name = self.generic_secret_path()
        path = self.root / name
        path.write_text("public source\n")
        command = self.publication_command()
        for domain in ("current", "index"):
            with self.subTest(domain=domain):
                if domain == "index":
                    self.git("add", "--", name)
                    path.unlink()
                self.assertEqual(self.run_scan().returncode, 0)
                result = subprocess.run(command, capture_output=True, text=True)
                self.assertEqual(result.returncode, 1)
                self.assertIn("Gitleaks found a secret", result.stderr)
                self.assertNotIn(name, result.stdout + result.stderr)

    def test_pre_push_protocol_uses_actual_oids_and_ignores_deletions(self):
        oid = "a" * 40
        zero = "0" * 40
        updates = [f"HEAD~1 {oid} refs/heads/main {zero}",
                   f"(delete) {zero} refs/heads/old {oid}"]
        self.assertEqual(scanner.push_objects(updates), [oid])
        for update in ("incomplete", "HEAD --all refs/heads/main " + zero):
            with self.subTest(update=update), self.assertRaises(ValueError):
                scanner.push_objects([update])

    def merge_only_credential(self):
        """Construct a credential introduced only by a merge, then removed."""
        self.commit()
        base = self.git("rev-parse", "HEAD").decode().strip()
        self.git("checkout", "-qb", "left")
        (self.root / "left.txt").write_text("left\n")
        self.commit()
        left = self.git("rev-parse", "HEAD").decode().strip()
        self.git("checkout", "-qb", "right", base)
        (self.root / "right.txt").write_text("right\n")
        self.commit()
        right = self.git("rev-parse", "HEAD").decode().strip()
        value = "".join(("n7az5H0qk9", "DM2pw4vc8s", "Tf6JY1xb3L", "rNQ0UZd9Eo"))
        payload = "api_" + "key = '" + value + "'\n"
        (self.root / "source.txt").write_bytes(payload.encode())
        self.git("add", "source.txt")
        tree = self.git("write-tree").decode().strip()
        merge = self.git("commit-tree", tree, "-p", left, "-p", right, "-m", "Fixture merge").decode().strip()
        (self.root / "source.txt").write_text("public source\n")
        self.git("add", "source.txt")
        clean_tree = self.git("write-tree").decode().strip()
        clean = self.git("commit-tree", clean_tree, "-p", merge, "-m", "Fixture cleanup").decode().strip()
        self.git("update-ref", "refs/heads/merged", clean)
        return payload

    def publication_command(self):
        scripts = self.root / "scripts/security"
        scripts.mkdir(parents=True, exist_ok=True)
        for name in ("publication_check.py", "scan_source.py", "gitleaks.lock"):
            shutil.copyfile(SCRIPT.with_name(name), scripts / name)
        return [sys.executable, str(scripts / "publication_check.py"), "--gitleaks", str(GITLEAKS)]

    def test_raw_snapshot_includes_merge_only_credential(self):
        payload = self.merge_only_credential()
        # Narrow custom patterns deliberately do not recognize this generic key.
        self.assertEqual(self.run_scan().returncode, 0)
        with tempfile.TemporaryDirectory() as directory:
            snapshot = Path(directory)
            self.assertTrue(publication_check.snapshot_history(self.root, snapshot))
            self.assertIn(payload.encode(), [path.read_bytes() for path in (snapshot / "history/blob").iterdir()])

    @unittest.skipUnless(GITLEAKS, "pinned Gitleaks executable is unavailable")
    def test_gitleaks_rejects_merge_only_credential(self):
        payload = self.merge_only_credential()
        command = self.publication_command()
        with tempfile.TemporaryDirectory() as directory:
            rules = Path(directory) / "rules.toml"
            rules.write_text("[extend]\nuseDefault = true\n")
            # Demonstrate why the old history diff scan was insufficient.
            previous = subprocess.run([str(GITLEAKS), "git", str(self.root), "--log-opts=--all HEAD",
                                       "--config", str(rules), "--no-banner", "--redact=100",
                                       "--ignore-gitleaks-allow"], capture_output=True, env=scanner.git_environment())
            self.assertEqual(previous.returncode, 0)
        result = subprocess.run(command, capture_output=True, text=True)
        self.assertEqual(result.returncode, 1)
        self.assertIn("Gitleaks found a secret", result.stderr)
        self.assertNotIn(payload.strip(), result.stdout + result.stderr)

    @unittest.skipUnless(GITLEAKS, "pinned Gitleaks executable is unavailable")
    def test_gitleaks_inspects_dangling_outgoing_commit(self):
        self.commit()
        base = self.git("rev-parse", "HEAD").decode().strip()
        value = "".join(("n7az5H0qk9", "DM2pw4vc8s", "Tf6JY1xb3L", "rNQ0UZd9Eo"))
        (self.root / "source.txt").write_text("api_" + "key = '" + value + "'\n")
        self.commit()
        outgoing = self.git("rev-parse", "HEAD").decode().strip()
        self.git("reset", "--hard", base)
        command = self.publication_command()
        self.assertEqual(subprocess.run(command, capture_output=True).returncode, 0)
        update = "HEAD~1 " + outgoing + " refs/heads/outgoing " + "0" * 40 + "\n"
        result = subprocess.run([*command, "--pre-push"], input=update, capture_output=True, text=True)
        self.assertEqual(result.returncode, 1)
        self.assertIn("Gitleaks found a secret", result.stderr)
        self.assertNotIn(value, result.stdout + result.stderr)


if __name__ == "__main__":
    unittest.main()
