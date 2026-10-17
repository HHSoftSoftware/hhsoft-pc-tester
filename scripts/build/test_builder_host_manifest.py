import copy
import errno
import json
import os
from pathlib import Path
import stat
import struct
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

import builder_host_manifest as inventory


class HostManifestTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.work = Path(self.temp.name)
        self.root = self.work / "host"
        self.root.mkdir()
        self.base = self.work / "base.lock"
        self.base.write_text("VERSION=fixture\n")
        self.go_version = self.work / "go-version"
        self.go_version.write_text("1.27.2\n")
        # Windows synthesizes executable bits from the .exe extension. These
        # files are never executed; Linux fixtures use the real required names.
        self.suffix = ".exe" if os.name == "nt" else ""
        fixture_tools = {"alpine-image": ("sh", "go", "mksquashfs"), "buildroot-image": ("sh", "go", "make")}
        self.tools = {profile: tuple(tool + self.suffix for tool in tools)
                      for profile, tools in fixture_tools.items()}
        patcher = mock.patch.object(inventory, "PROFILE_TOOLS", self.tools)
        patcher.start()
        self.addCleanup(patcher.stop)
        for tool in sorted(set(sum(self.tools.values(), ()))):
            self.write("/usr/bin/" + tool, b"synthetic executable\n", 0o755)
        self.write("/etc/alpine-release", b"3.24.2\n")
        self.write("/etc/apk/arch", b"x86_64\n")
        self.write("/etc/apk/world", b"fixture-tool\n")
        self.write("/etc/apk/repositories", b"https://example.invalid/alpine/main\n")
        self.write("/lib/apk/db/installed", b"P:fixture-tool\nV:1.0-r0\nA:x86_64\nD:so:libfixture.so.1\n\n"
                   b"P:fixture-library\nV:2.0-r0\nA:x86_64\np:so:libfixture.so.1=2.0\n\n"
                   b"P:go\nV:1.27.2-r0\nA:x86_64\n\n")
        self.write("/usr/lib/libfixture.so.1", b"shared library consumed at runtime\n")
        self.write("/usr/share/fixture-tool/runtime.data", b"runtime package data\n")
        self.manifest = self.work / "reviewed.json"
        self.approve_fixture()

    def path(self, name):
        return self.root / name.lstrip("/")

    def write(self, name, content, mode=0o644):
        path = self.path(name)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(content)
        path.chmod(mode)
        return path

    def snapshot(self, profile="alpine-image"):
        return inventory.create_manifest(self.root, profile, self.base, self.go_version)

    def approve_fixture(self, profile="alpine-image"):
        self.manifest.write_text(json.dumps(self.snapshot(profile)))

    def verify(self, profile="alpine-image"):
        inventory.verify_manifest(self.manifest, profile, self.base, self.go_version, root=self.root)

    def rejected(self, profile="alpine-image"):
        with self.assertRaises((ValueError, OSError)):
            self.verify(profile)

    def symlink(self, target, name, directory=False):
        path = self.path(name)
        path.parent.mkdir(parents=True, exist_ok=True)
        try:
            path.symlink_to(target, target_is_directory=directory)
        except (OSError, NotImplementedError) as error:
            self.skipTest("symlink creation unavailable: " + type(error).__name__)
        return path

    def test_exact_host_matches_and_inventory_includes_dependency_closure(self):
        self.verify()
        result = self.snapshot()
        self.assertEqual([package["name"] for package in result["packages"]],
                         ["fixture-library", "fixture-tool", "go"])
        self.assertIn("/usr/lib/libfixture.so.1", result["files"])
        self.assertIn("/usr/share/fixture-tool/runtime.data", result["files"])
        self.assertIn("/lib/apk/db/installed", result["files"])
        self.assertIn("/etc/apk/repositories", result["files"])
        self.assertEqual(result["policy"]["go_version"], "1.27.2")

    def test_changed_indirect_library_rejected_with_same_tool_and_package_versions(self):
        self.path("/usr/lib/libfixture.so.1").write_bytes(b"mutated runtime library\n")
        self.rejected()

    def test_changed_runtime_data_rejected(self):
        self.path("/usr/share/fixture-tool/runtime.data").write_bytes(b"mutated runtime data\n")
        self.rejected()

    def test_missing_library_rejected(self):
        self.path("/usr/lib/libfixture.so.1").unlink()
        self.rejected()

    def test_extra_unmanaged_runtime_file_rejected(self):
        self.write("/usr/lib/unreviewed-plugin.so", b"additional plugin\n")
        self.rejected()

    def test_missing_required_image_tool_cannot_generate_candidate(self):
        self.path("/usr/bin/mksquashfs" + self.suffix).unlink()
        with self.assertRaisesRegex(ValueError, "required builder tool missing"):
            self.snapshot()

    def test_changed_transitive_package_identity_rejected(self):
        database = self.path("/lib/apk/db/installed")
        database.write_bytes(database.read_bytes().replace(b"V:2.0-r0", b"V:2.1-r0"))
        self.rejected()

    def test_whole_database_changes_rejected_even_without_identity_change(self):
        database = self.path("/lib/apk/db/installed")
        database.write_bytes(database.read_bytes() + b"\n")
        self.rejected()

    def test_apk_bracket_command_provides_are_supported_narrowly(self):
        content = b"P:coreutils\nV:9.11-r0\nA:x86_64\np:cmd:[=9.11-r0 cmd:[[=1.37.0-r0\n\n"
        self.assertEqual(inventory.installed_packages(content)[0]["provides"],
                         ["cmd:[=9.11-r0", "cmd:[[=1.37.0-r0"])
        for token in (b"cmd:[[[=9.11-r0", b"cmd:[evil=9.11-r0", b"cmd:\x01=9.11-r0", b"pkg:[=1"):
            with self.subTest(token=token):
                with self.assertRaises(ValueError):
                    inventory.installed_packages(content.replace(b"cmd:[=9.11-r0", token))

    def test_database_required_and_duplicate_package_identity_rejected(self):
        database = self.path("/lib/apk/db/installed")
        original = database.read_bytes()
        for invalid in (b"", b"P:incomplete\n\n", original + original,
                        original.replace(b"A:x86_64", b"A:aarch64", 1)):
            with self.subTest(invalid=invalid[:20]):
                database.write_bytes(invalid)
                with self.assertRaises(ValueError):
                    self.snapshot()
        database.unlink()
        self.rejected()

    def test_profile_base_and_go_policy_mismatch_rejected(self):
        self.rejected("buildroot-image")
        self.base.write_text("VERSION=different\n")
        self.rejected()
        self.base.write_text("VERSION=fixture\n")
        self.go_version.write_text("1.27.3\n")
        self.rejected()

    def test_every_image_compiler_package_must_match_the_go_policy(self):
        for profile in ("alpine-image", "buildroot-image"):
            self.approve_fixture(profile)
            self.verify(profile)
        database = self.path("/lib/apk/db/installed")
        original = database.read_bytes()
        for invalid in (original.replace(b"V:1.27.2-r0", b"V:1.26.9-r0"),
                        original.replace(b"P:go\nV:1.27.2-r0\nA:x86_64\n\n", b"")):
            database.write_bytes(invalid)
            for profile in ("alpine-image", "buildroot-image"):
                with self.subTest(profile=profile), self.assertRaisesRegex(ValueError, "host Go package"):
                    self.snapshot(profile)

    def test_modern_image_requires_an_inventoried_go_executable(self):
        self.path("/usr/bin/go" + self.suffix).unlink()
        with self.assertRaisesRegex(ValueError, "required builder tool missing"):
            self.snapshot("alpine-image")

    def test_public_configuration_change_rejected(self):
        self.write("/etc/mkinitfs/mkinitfs.conf", b'features="base ext4"\n')
        self.approve_fixture()
        self.write("/etc/mkinitfs/mkinitfs.conf", b'features="base ext4 nvme"\n')
        self.rejected()

    def test_selected_global_config_presence_and_bytes_are_bound(self):
        ordinary = ("/etc/wgetrc", "/usr/local/etc/wgetrc",
                    "/etc/gnupg/common.conf", "/etc/gnupg/gpg.conf")
        loaders = ("/etc/ld-musl-x86_64.path", "/etc/ld-musl-i386.path", "/etc/ld-musl-aarch64.path")
        for name in ordinary + loaders:
            with self.subTest(config=name):
                self.setUp()
                self.assertEqual(self.snapshot()["files"][name], {"type": "absent"})
                content = b"/lib:/usr/lib\n" if name in loaders else b"# fixture config A\n"
                path = self.write(name, content)
                self.rejected()
                self.approve_fixture()
                self.verify()
                changed = b"/usr/lib:/lib\n" if name in loaders else b"# fixture config B\n"
                path.write_bytes(changed)
                self.rejected()

    def test_new_dynamic_loader_name_is_still_detected(self):
        name = "/etc/ld-musl-fixture.path"
        path = self.write(name, b"/lib:/usr/lib\n")
        self.rejected()
        self.approve_fixture()
        self.verify()
        self.assertEqual(self.snapshot()["files"][name]["type"], "file")
        path.unlink()
        self.rejected()

    def set_fixture_xattr(self, path, name="user.hhpt-fixture", value=b"synthetic metadata"):
        try:
            os.setxattr(path, name, value, follow_symlinks=False)
        except OSError as error:
            if error.errno in {errno.ENOTSUP, errno.EOPNOTSUPP, errno.EPERM, errno.EACCES}:
                self.skipTest("fixture filesystem does not permit this extended attribute")
            raise

    @unittest.skipUnless(inventory.LINUX_PINNING and hasattr(os, "setxattr"),
                         "native Linux extended attribute fixture required")
    def test_file_directory_parent_and_policy_xattrs_fail_without_reading_values(self):
        paths = (self.path("/usr/lib/libfixture.so.1"), self.path("/usr/lib"),
                 self.path("/etc"), self.root, self.base)
        for path in paths:
            with self.subTest(kind="file" if path.is_file() else "directory"):
                content = path.read_bytes() if path.is_file() else None
                mode = stat.S_IMODE(path.stat().st_mode)
                self.set_fixture_xattr(path)
                try:
                    self.assertEqual(stat.S_IMODE(path.stat().st_mode), mode)
                    if content is not None:
                        self.assertEqual(path.read_bytes(), content)
                    with mock.patch.object(inventory.os, "getxattr", side_effect=AssertionError("attribute values must not be read")):
                        with self.assertRaisesRegex(ValueError, "extended attributes"):
                            self.snapshot()
                finally:
                    os.removexattr(path, "user.hhpt-fixture", follow_symlinks=False)
                self.verify()

    @unittest.skipUnless(inventory.LINUX_PINNING and hasattr(os, "setxattr"),
                         "native Linux POSIX ACL fixture required")
    def test_named_user_acl_is_rejected_even_when_bytes_and_unix_mode_match(self):
        path = self.path("/usr/lib/libfixture.so.1")
        original = path.read_bytes(), stat.S_IMODE(path.stat().st_mode)
        # Version 2 POSIX ACL: owner rw, named synthetic user denied, group r,
        # mask r, other r. Named-user access changes without changing this
        # fixture's ordinary 0644 mode or file bytes.
        entries = ((1, 6, 0xffffffff), (2, 0, 12345), (4, 4, 0xffffffff),
                   (16, 4, 0xffffffff), (32, 4, 0xffffffff))
        acl = struct.pack("<I", 2) + b"".join(struct.pack("<HHI", *entry) for entry in entries)
        self.set_fixture_xattr(path, "system.posix_acl_access", acl)
        self.assertEqual((path.read_bytes(), stat.S_IMODE(path.stat().st_mode)), original)
        with mock.patch.object(inventory.os, "getxattr", side_effect=AssertionError("ACL values must not be read")):
            with self.assertRaisesRegex(ValueError, "extended attributes"):
                self.snapshot()

    @unittest.skipUnless(inventory.LINUX_PINNING and hasattr(os, "setxattr"),
                         "native Linux no-follow extended attribute fixture required")
    def test_private_link_metadata_does_not_inspect_target_attributes(self):
        target = self.path("/etc/ssl/private")
        target.mkdir(parents=True)
        self.set_fixture_xattr(target)
        self.symlink("/etc/ssl/private", "/usr/lib/ssl/private", directory=True)
        original = inventory.os.listxattr
        inspected_links = []

        def inspect(path, *args, **kwargs):
            if isinstance(path, str):
                self.assertTrue(path.startswith("/proc/self/fd/"))
                self.assertIs(kwargs.get("follow_symlinks"), False)
                inspected_links.append(path)
            return original(path, *args, **kwargs)

        with mock.patch.object(inventory.os, "listxattr", side_effect=inspect), \
                mock.patch.object(inventory.os, "getxattr", side_effect=AssertionError("attribute values must not be read")):
            self.approve_fixture()
            self.verify()
        self.assertTrue(inspected_links)
        self.assertNotIn("/etc/ssl/private", self.snapshot()["files"])

    @unittest.skipUnless(inventory.LINUX_PINNING, "native Linux symlink metadata fixture required")
    def test_unsupported_or_nonempty_symlink_attributes_fail_closed_and_redacted(self):
        self.symlink("/etc/ssl/private", "/usr/lib/ssl/private", directory=True)
        original = inventory.os.listxattr
        for unsupported in (False, True):
            with self.subTest(unsupported=unsupported):
                def inspect(path, *args, **kwargs):
                    if isinstance(path, str):
                        self.assertIs(kwargs.get("follow_symlinks"), False)
                        if unsupported:
                            raise NotImplementedError("private-attribute-sentinel")
                        return ["user.private-attribute-sentinel"]
                    return original(path, *args, **kwargs)
                with mock.patch.object(inventory.os, "listxattr", side_effect=inspect):
                    with self.assertRaises(ValueError) as failure:
                        self.snapshot()
                self.assertNotIn("private-attribute-sentinel", str(failure.exception))

    @unittest.skipUnless(inventory.LINUX_PINNING, "native Linux symlink replacement fixture required")
    def test_symlink_replacement_during_metadata_check_is_rejected_without_target_io(self):
        link = self.symlink("/etc/ssl/private", "/usr/lib/ssl/private", directory=True)
        original = inventory.os.listxattr
        replaced = False

        def inspect(path, *args, **kwargs):
            nonlocal replaced
            if isinstance(path, str) and not replaced:
                self.assertIs(kwargs.get("follow_symlinks"), False)
                replaced = True
                link.unlink()
                os.mkfifo(link)
            return original(path, *args, **kwargs)

        with mock.patch.object(inventory.os, "listxattr", side_effect=inspect):
            with self.assertRaisesRegex(ValueError, "metadata changed"):
                self.snapshot()
        self.assertTrue(replaced)

    def test_unsupported_execution_configuration_rejected(self):
        for name in ("/etc/ld.so.preload", "/etc/ld.so.conf", "/etc/gitconfig", "/etc/apk/extra.conf"):
            with self.subTest(name=name):
                path = self.write(name, b"unreviewed configuration\n")
                with self.assertRaises(ValueError):
                    self.snapshot()
                path.unlink()

    def test_loader_search_path_cannot_escape_runtime_inventory(self):
        path = self.write("/etc/ld-musl-x86_64.path", b"/lib:/usr/lib\n")
        self.approve_fixture()
        self.verify()
        path.write_bytes(b"/lib:/var/local-runtime\n")
        with self.assertRaisesRegex(ValueError, "loader searches"):
            self.snapshot()

    def test_loader_search_cannot_use_excluded_directories(self):
        for directory in ("/lib/modules", "/lib/firmware", "/usr/lib/ssl/private"):
            with self.subTest(directory=directory):
                self.write(directory + "/fixture-library.so", b"outside the inventoried runtime\n")
                self.write("/etc/ld-musl-x86_64.path", directory.encode() + b"\n")
                with self.assertRaises((ValueError, OSError)):
                    inventory.HostInventory(self.root).configuration()

    def test_loader_search_cannot_use_symlink_to_excluded_directory(self):
        self.write("/lib/modules/fixture-library.so", b"outside the inventoried runtime\n")
        self.symlink("../../lib/modules", "/usr/lib/loader", directory=True)
        self.write("/etc/ld-musl-x86_64.path", b"/usr/lib/loader\n")
        with self.assertRaises((ValueError, OSError)):
            inventory.HostInventory(self.root).configuration()

    def test_private_user_and_work_directories_are_never_read(self):
        # Construct a synthetic home prefix; public source rejects literal
        # machine-specific user-home paths, including their bare prefix.
        home_root = "/" + "home"
        self.write("/root/.ssh/id_ed25519", b"synthetic forbidden input\n")
        self.write(home_root + "/fixture-user/work/unrelated", b"synthetic unrelated input\n")
        self.write("/etc/ssl/private/fixture.key", b"synthetic forbidden input\n")
        self.write("/var/cache/apk/unused", b"unused cache data\n")
        self.approve_fixture()
        self.path("/etc/ssl/private/fixture.key").write_bytes(b"different synthetic private input\n")
        self.verify()
        self.assertFalse(any(name.startswith(("/root/", home_root + "/", "/var/", "/etc/ssl/private/"))
                             for name in self.snapshot()["files"]))

    def test_recognizable_private_key_in_runtime_tree_rejected_without_read(self):
        self.write("/usr/share/fixture.key", b"synthetic forbidden input\n")
        original = inventory.HostInventory.bytes
        def guarded(host, name, *args):
            self.assertNotEqual(name, "/usr/share/fixture.key")
            return original(host, name, *args)
        with mock.patch.object(inventory.HostInventory, "bytes", guarded):
            with self.assertRaisesRegex(ValueError, "private-key"):
                self.snapshot()

    def test_runtime_symlink_target_identity_and_escape_rejected(self):
        link = self.symlink("libfixture.so.1", "/usr/lib/libfixture.so")
        self.approve_fixture()
        self.verify()
        self.write("/usr/lib/another.so.1", b"another library\n")
        self.approve_fixture()
        link.unlink()
        self.symlink("another.so.1", "/usr/lib/libfixture.so")
        self.rejected()
        link.unlink()
        self.symlink("../../root/.ssh/id_ed25519", "/usr/lib/libfixture.so")
        with self.assertRaisesRegex(ValueError, "symlink leaves"):
            self.snapshot()

    def test_intermediate_link_escape_and_loop_rejected(self):
        self.symlink("/root", "/usr/lib/escape", directory=True)
        with self.assertRaisesRegex(ValueError, "symlink leaves"):
            self.snapshot()
        self.path("/usr/lib/escape").unlink()
        self.symlink("loop", "/usr/lib/loop")
        with self.assertRaisesRegex(ValueError, "symlink loop"):
            self.snapshot()

    def test_dotdot_after_intermediate_symlink_uses_kernel_order(self):
        self.write("/usr/lib/fixture-target", b"lexically normalized decoy\n", 0o755)
        outside = self.write("/usr/fixture-target", b"actual out-of-scope executable\n", 0o755)
        self.symlink("../share", "/usr/lib/alias", directory=True)
        tool = self.path("/usr/bin/sh" + self.suffix)
        tool.unlink()
        self.symlink("../lib/alias/../fixture-target", "/usr/bin/sh" + self.suffix)
        self.assertEqual(tool.resolve(), outside.resolve())
        with mock.patch.object(inventory.os, "fdopen", side_effect=AssertionError("unexpected data open")):
            with self.assertRaisesRegex(ValueError, "symlink leaves"):
                inventory.HostInventory(self.root).bytes("/usr/bin/sh" + self.suffix)

    def test_regular_file_is_not_a_traversable_parent(self):
        self.write("/usr/lib/fixture-file", b"not a directory\n")
        for target in ("fixture-file/../libfixture.so.1", "fixture-file/"):
            link = self.symlink(target, "/usr/lib/invalid-parent")
            with self.assertRaisesRegex(ValueError, "not a directory"):
                inventory.HostInventory(self.root).resolve("/usr/lib/invalid-parent")
            link.unlink()

    def test_package_database_and_architecture_links_have_recorded_resolved_bytes(self):
        for policy, destination in (("/lib/apk/db/installed", "/usr/share/fixture-installed"),
                                    ("/etc/apk/arch", "/usr/share/fixture-arch")):
            original = self.path(policy)
            self.write(destination, original.read_bytes())
            original.unlink()
            target = os.path.relpath(self.path(destination), original.parent).replace(os.sep, "/")
            self.symlink(target, policy)
        self.approve_fixture()
        self.verify()
        result = subprocess.run([sys.executable, "-I", "-B", str(Path(inventory.__file__).resolve()),
                                 "candidate", "--root", str(self.root), "--profile", "alpine-image",
                                 "--base-lock", str(self.base), "--go-version", str(self.go_version),
                                 "--manifest", str(self.work / "linked-candidate.json")],
                                capture_output=True, text=True, timeout=10)
        # The isolated CLI intentionally has the full real tool requirements,
        # whereas this unit fixture supplies only a representative tool subset.
        self.assertEqual(result.returncode, 1)
        self.assertNotIn("Traceback", result.stdout + result.stderr)
        self.assertNotIn(str(self.work), result.stdout + result.stderr)
        self.path("/usr/share/fixture-installed").write_bytes(b"changed package database\n")
        self.rejected()

    def test_config_link_to_private_key_or_subtree_rejected_before_data_open(self):
        config = self.path("/etc/apk/arch")
        for target in ("/usr/share/fixture.key", "/usr/share/id_ed25519",
                       "/usr/lib/ssl/private/nested/input"):
            with self.subTest(target=target):
                self.write(target, b"synthetic private input must not be read\n")
                config.unlink(missing_ok=True)
                self.symlink("../.." + target, "/etc/apk/arch")
                with mock.patch.object(inventory.os, "fdopen", side_effect=AssertionError("unexpected data open")):
                    with self.assertRaisesRegex(ValueError, "symlink leaves"):
                        inventory.HostInventory(self.root).bytes("/etc/apk/arch")

    def test_parent_symlink_cannot_redirect_public_config_into_private_directory(self):
        self.write("/root/private-config/openssl.cnf", b"must never be read\n")
        self.symlink("../root/private-config", "/etc/ssl", directory=True)
        with self.assertRaises((ValueError, OSError)):
            self.snapshot()

    def test_exact_openssl_private_link_is_recorded_without_inspecting_destination(self):
        self.symlink("/etc/ssl/private", "/usr/lib/ssl/private", directory=True)
        self.approve_fixture()
        self.verify()  # Destination does not exist and must not be stat'ed.
        self.assertEqual(self.snapshot()["files"]["/usr/lib/ssl/private"]["target"], "/etc/ssl/private")

    def test_hardlink_identity_changes_rejected_with_same_content(self):
        original = self.path("/usr/lib/libfixture.so.1")
        alias = self.path("/usr/lib/libfixture-alias.so.1")
        try:
            os.link(original, alias)
        except OSError as error:
            self.skipTest("hardlinks unavailable: " + type(error).__name__)
        self.approve_fixture()
        alias.unlink()
        alias.write_bytes(original.read_bytes())
        self.rejected()

    @unittest.skipIf(os.name == "nt", "Windows does not preserve Unix executable/ownership modes")
    def test_executable_and_parent_modes_are_exact(self):
        executable = self.path("/usr/bin/mksquashfs")
        executable.chmod(0o644)
        with self.assertRaisesRegex(ValueError, "not executable"):
            self.snapshot()
        executable.chmod(0o755)
        self.path("/usr").chmod(0o777)
        self.rejected()

    def test_manifest_missing_truncated_duplicate_fields_and_wrong_types_rejected(self):
        original = json.loads(self.manifest.read_text())
        invalid_type = copy.deepcopy(original)
        invalid_type["schema"] = True
        for content in ("{", '{"schema":1,"schema":1}', json.dumps(invalid_type), '{"schema":1.0}'):
            with self.subTest(content=content[:40]):
                self.manifest.write_text(content)
                self.rejected()
        self.manifest.unlink()
        self.rejected()

    def test_inventory_bounds_fail_closed(self):
        with mock.patch.object(inventory, "MAX_FILES", 2):
            with self.assertRaisesRegex(ValueError, "file count"):
                self.snapshot()
        with mock.patch.object(inventory, "MAX_TOTAL_BYTES", 2):
            with self.assertRaisesRegex(ValueError, "byte limit"):
                self.snapshot()

    @unittest.skipUnless(inventory.LINUX_PINNING, "Linux O_PATH/FIFO fixture requires native Linux")
    def test_fifo_database_and_config_are_rejected_before_data_open(self):
        # A subprocess timeout makes a blocking FIFO open a deterministic test
        # failure instead of leaving the whole suite hung.
        for name in ("/etc/apk/arch", "/lib/apk/db/installed"):
            path = self.path(name)
            original = path.read_bytes()
            path.unlink()
            os.mkfifo(path)
            result = subprocess.run([sys.executable, "-I", "-B", str(Path(inventory.__file__).resolve()),
                                     "candidate", "--root", str(self.root), "--profile", "alpine-image",
                                     "--base-lock", str(self.base), "--go-version", str(self.go_version),
                                     "--manifest", str(self.work / "fifo-candidate.json")],
                                    capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
            self.assertNotIn(str(self.root), result.stderr)
            path.unlink()
            path.write_bytes(original)

    def test_production_cli_rejects_fixture_root_and_errors_are_redacted(self):
        command = [sys.executable, "-I", "-B", str(Path(inventory.__file__).resolve()), "verify",
                   "--profile", "alpine-image", "--base-lock", str(self.base),
                   "--go-version", str(self.go_version), "--manifest", str(self.manifest)]
        for additional in (["--root", str(self.root)], ["--profile", "private-argument-sentinel"],
                           ["--unknown=private-argument-sentinel"]):
            result = subprocess.run(command + additional, capture_output=True, text=True, timeout=10)
            self.assertNotEqual(result.returncode, 0)
            self.assertNotIn(str(self.work), result.stdout + result.stderr)
            self.assertNotIn("private-argument-sentinel", result.stdout + result.stderr)
        self.manifest.unlink()
        result = subprocess.run(command, capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 1)
        self.assertNotIn(str(self.work), result.stderr)
        self.assertNotIn("Traceback", result.stderr)
        gate_command = [sys.executable, "-I", "-B", str(Path(inventory.__file__).resolve()), "gate",
                        "--repository", str(self.work), "--profile", "alpine-image"]
        for additional in (["--root", str(self.root)], ["--manifest", str(self.manifest)],
                           ["--profile", "private-argument-sentinel"]):
            result = subprocess.run(gate_command + additional, capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 1)
            self.assertNotIn(str(self.work), result.stdout + result.stderr)
            self.assertNotIn("private-argument-sentinel", result.stdout + result.stderr)

    def test_only_canonical_development_prerelease_is_exempt(self):
        version = self.work / "VERSION"
        for text in ("0.0.0-dev.0", "1.2.3-dev.123"):
            version.write_text(text + "\n")
            self.assertEqual(inventory.classify_version(version), "development")
        for text in ("1.0.0", "1.0.0-alpha.1-dev.0", "1.0.0-beta.1-dev.0", "1.0.0-rc.1-dev.0",
                     "1.0.0+fixture-dev.0", "1.0.0-dev.0+metadata", "1.0.0-development.0"):
            version.write_text(text + "\n")
            self.assertEqual(inventory.classify_version(version), "release", text)
        for text in ("01.0.0-dev.0", "1.0.0-dev.01", "1.0-dev.0", "1.0.0-", "1.0.0-alpha..1",
                     "1.0.0\n\n", " 1.0.0"):
            version.write_text(text + "\n")
            with self.assertRaises(ValueError, msg=text):
                inventory.classify_version(version)


if __name__ == "__main__":
    unittest.main()
