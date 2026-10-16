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
        (self.root / "source.txt").write_text(payload)
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
