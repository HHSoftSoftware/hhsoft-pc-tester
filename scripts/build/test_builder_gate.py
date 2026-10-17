"""Exercise the real image wrappers without building images or inspecting a host.

The copied policy keeps its CLI, version classification, and approved-path
selection. Only host observation and manifest comparison are fixture boundaries;
their independent file/security tests live in test_builder_host_manifest.py.
All image tools below are inert sentinels confined to a temporary directory.
"""

import hashlib
import json
from pathlib import Path
import shlex
import subprocess
import sys
import tempfile
import unittest


SOURCE = Path(__file__).resolve().parent
PROFILES = ("alpine-image", "buildroot-image")
TOOLS = ("apk", "fakeroot", "update-kernel", "mkinitfs", "mksquashfs", "modinfo",
         "depmod", "openssl", "xorrisofs", "grub-mkimage", "mformat", "mcopy",
         "python3", "scanelf", "lddtree")

ADAPTER = r'''import importlib.util
import json
import os
from pathlib import Path
import sys

repository = Path(__file__).resolve().parents[2]
fixture = repository.parent
spec = importlib.util.spec_from_file_location("fixture_builder_policy", Path(__file__).with_name("policy.py"))
policy = importlib.util.module_from_spec(spec)
spec.loader.exec_module(policy)

def record(value):
    with (fixture / "calls.jsonl").open("a") as stream:
        stream.write(json.dumps(value) + "\n")

def event(value):
    with (fixture / "events").open("a") as stream:
        stream.write(value + "\n")

record({"kind": "cli", "arguments": sys.argv[1:], "isolated": sys.flags.isolated,
        "no_bytecode": sys.dont_write_bytecode,
        "environment": {key: os.environ[key] for key in
            ("PYTHONPATH", "PYTHONHOME", "VERSION", "HHPT_BUILDER_ROOT", "HHPT_BUILDER_MANIFEST")
            if key in os.environ}, "path": os.environ.get("PATH")})

def architecture():
    record({"kind": "host-observation"})
    return "x86_64"

def verify(manifest, profile, base_lock, go_version, root=Path("/")):
    event("host-check")
    record({"kind": "verify", "manifest": str(manifest), "profile": profile,
            "base_lock": str(base_lock), "go_version": str(go_version), "root": str(root)})
    count_path = fixture / "verify-count"
    count = int(count_path.read_text()) + 1 if count_path.exists() else 1
    count_path.write_text(str(count))
    if not Path(manifest).is_file() or ((fixture / "fail-final").exists() and count > 1):
        raise ValueError("fixture host comparison failed")

policy.actual_host_architecture = architecture
policy.verify_manifest = verify
try:
    policy.main()
except (OSError, ValueError, TypeError, UnicodeError, RecursionError):
    print("fixture policy rejected input", file=sys.stderr)
    sys.exit(1)
'''


# The copied wrapper keeps its real unconditional environment boundary and
# complete build body. Inject inert tool functions into that body's fixture
# copy; caller-supplied shell functions cannot cross the real exec boundary.
DRIVER = r'''
set -eu
fixture_root=FIXTURE_ROOT
event() { printf '%s\n' "$1" >> "$fixture_root/events"; }
phase() { /usr/bin/python3 -I -B "$fixture_root/repository/scripts/build/phase.py" "$@"; }
command() {
    if test "$1" = -v && test -x "$fixture_root/tool-stubs/$2"; then
        printf '%s\n' "$fixture_root/tool-stubs/$2"
    else
        /bin/sh -c 'command "$@"' fixture-command "$@"
    fi
}
git() {
    case "$*" in
        *'show -s --format=%ct'*)
            test ! -f "$fixture_root/fail-source-time" || return 29
            if test -f "$fixture_root/bad-source-time"; then printf 'invalid\n'; else printf '1700000000\n'; fi
            ;;
        *)
            printf 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n'
            if test -f "$fixture_root/fail-aports-query"; then
                last_argument=
                for argument do last_argument=$argument; done
                test "$last_argument" != "$(cat "$fixture_root/fail-aports-query")" || return 31
            fi
            ;;
    esac
}
sha256sum() {
    if test "$1" = -c; then return 0; fi
    /bin/sh -c 'sha256sum "$@"' fixture-hash "$@"
}
cp() {
    case "$1" in /usr/share/apk/keys/*) return 0 ;; esac
    /bin/cp "$@"
}
openssl() {
    phase openssl
    destination=
    while test "$#" -gt 0; do
        if test "$1" = -out; then shift; destination=$1; fi
        shift
    done
    test -n "$destination"
    printf 'fixture public encoding\n' > "$destination"
}
unshare() { test "$1" = --net; shift; "$@"; }
gpg() {
    phase gpg "$@"
    event signing-input-check
    case "$*" in *--list-keys*) printf 'fpr:::::::::BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB:\n' ;; esac
}
xz() { printf 'fixture archive stream\n'; }
tar() { event archive-extraction; }
make() {
    phase make "$@"
    event build-image
    mkdir -p "$build_root/images" "$build_root/legal-info"
    printf 'synthetic image\n' > "$build_root/images/rootfs.iso9660"
    printf 'synthetic manifest\n' > "$build_root/legal-info/manifest.csv"
    printf 'synthetic host manifest\n' > "$build_root/legal-info/host-manifest.csv"
}
'''

PHASE = r'''import json
import os
from pathlib import Path
import sys
fixture = Path(__file__).resolve().parents[3]
with (fixture / "phases.jsonl").open("a") as stream:
    stream.write(json.dumps({"phase": sys.argv[1], "arguments": sys.argv[2:], "cwd": os.getcwd(),
                            "environment": dict(os.environ)}) + "\n")
'''


@unittest.skipUnless(sys.platform.startswith("linux") and Path("/usr/bin/python3").is_file(),
                     "image wrapper integration requires native Linux and /usr/bin/python3")
class BuilderGateIntegrationTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="hhpt-builder-gate-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.repository = self.root / "repository"
        self.work = self.root / "image-work"
        self.output = self.root / "output"
        self.inputs = self.root / "inputs"
        self.inputs.mkdir()
        for name in ("build-alpine-image.sh", "build-buildroot-image.sh"):
            content = (SOURCE / name).read_text()
            boundary = "<<'HHPT_BUILD'\n"
            self.assertEqual(content.count(boundary), 1)
            fixture_tools = DRIVER.replace("FIXTURE_ROOT", shlex.quote(str(self.root)))
            self.write("scripts/build/" + name, content.replace(boundary, boundary + fixture_tools))
        self.write("scripts/build/policy.py", (SOURCE / "builder_host_manifest.py").read_text())
        self.write("scripts/build/builder_host_manifest.py", ADAPTER)
        self.write(".go-version", "1.27.2\n")
        self.write("LICENSE", "Synthetic license fixture\n")
        self.write("scripts/security/scan_source.py", "# Synthetic source scanner.\n")
        self.write("scripts/build/phase.py", PHASE)
        self.write("scripts/build/alpine_package_cache.py", "# Synthetic cache fixture; no host/package access.\n")
        self.write("scripts/build/write-alpine-sbom.sh", "#!/bin/sh\nexit 0\n")
        self.write("scripts/build/write_buildroot_sbom.py", "# Synthetic SBOM fixture.\n")
        self.write("scripts/build/build-go.sh", '#!/bin/sh\nmkdir -p "$2/linux_legacy-x86"\n'
                   'printf "synthetic executable\\n" > "$2/linux_legacy-x86/hhpt"\n')
        for name in ("record_alpine_apks.py", "scanelf-readonly.sh", "fakeroot-apkovl.sh"):
            self.write("scripts/build/" + name, "#!/bin/sh\nexit 97\n")
        for name in ("mkimg.hhpt.sh", "genapkovl-hhpt.sh"):
            self.write("platform/alpine/mkimage/" + name, "#!/bin/sh\nexit 97\n")
        self.write("platform/alpine/mkimage/upstream-scripts.sha256", "synthetic checksum input\n")
        self.write("platform/alpine/base.lock", "BRANCH=v3.24\nAPORTS_TAG=v3.24.2\n"
                   "APORTS_TAG_OBJECT=" + "a" * 40 + "\nAPORTS_COMMIT=" + "a" * 40 + "\n")
        self.write("platform/alpine/package-locks/aarch64.json", "{}\n")
        self.write("build/bin/linux_aarch64/hhpt", "synthetic executable\n", 0o755)
        self.aports = self.inputs / "aports"
        (self.aports / "scripts").mkdir(parents=True)
        for name in ("mkimage.sh", "mkimage-yaml.sh", "mkimg.base.sh", "mkimg.standard.sh"):
            (self.aports / "scripts" / name).write_text("#!/bin/sh\nexit 97\n")
        self.signing_key = self.inputs / "fixture-signing-input"
        self.signing_key.write_text("Synthetic fixture; not a private key.\n")
        self.signing_key.with_suffix(".pub").write_text("Synthetic public fixture.\n")
        self.cache = self.inputs / "cache"
        self.cache.mkdir()
        for name in ("buildroot-2026.02.1.tar.xz", "linux-6.12.1.tar.xz", "linux-6.12.1.tar.sign",
                     "release-public-key", "linux-public-key"):
            (self.inputs / name).write_text("Synthetic input only.\n")
        archive_hash = hashlib.sha256(b"Synthetic input only.\n").hexdigest()
        (self.inputs / "buildroot.sign").write_text(
            "SHA256: " + archive_hash + "  buildroot-2026.02.1.tar.xz\n")
        self.write("platform/buildroot/base.lock", "VERSION=2026.02.1\nSOURCE=buildroot-2026.02.1.tar.xz\n"
                   "SHA256=" + archive_hash + "\nUPSTREAM_GPG_FINGERPRINT=" + "B" * 40 + "\n"
                   "LINUX_VERSION=6.12.1\nLINUX_SOURCE=linux-6.12.1.tar.xz\nLINUX_SHA256=" + archive_hash + "\n"
                   "LINUX_SIGNATURE=linux-6.12.1.tar.sign\nLINUX_SIGNER_FINGERPRINT=" + "B" * 40 + "\n")
        self.write("platform/buildroot/configs/hhpt_legacy_x86_defconfig",
                   'BR2_LINUX_KERNEL_CUSTOM_VERSION_VALUE="6.12.1"\n')
        for name in ("linux/linux.hash", "linux-headers/linux-headers.hash"):
            self.write("platform/buildroot/patches/" + name,
                       "sha256  " + archive_hash + "  linux-6.12.1.tar.xz\n")
        self.write("platform/buildroot/board/legacy-x86/post-build.sh", "#!/bin/sh\nexit 97\n")
        (self.root / "tool-stubs").mkdir()
        for name in TOOLS:
            path = self.root / "tool-stubs" / name
            path.write_text("#!/bin/sh\nexit 97\n")
            path.chmod(0o755)
        (self.root / "tool-stubs/apk").write_text("#!/bin/sh\nexit 0\n")
        (self.root / "tool-stubs/fakeroot").write_text(
            "#!/bin/sh\nset -eu\nfixture=" + shlex.quote(str(self.root)) + "\n"
            'printf "build-image\\n" >> "$fixture/events"\n'
            'mkdir -p "$fixture/output"\n'
            'version=$(cat "$fixture/repository/VERSION")\n'
            'printf "synthetic image\\n" > "$fixture/output/hhsoft-pc-tester-$version-aarch64.iso"\n')

    def write(self, name, content, mode=0o644):
        path = self.repository / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8", newline="\n")
        path.chmod(mode)
        return path

    def records(self, kind=None):
        path = self.root / "calls.jsonl"
        records = [json.loads(line) for line in path.read_text().splitlines()] if path.exists() else []
        return [record for record in records if kind is None or record["kind"] == kind]

    def events(self):
        path = self.root / "events"
        return path.read_text().splitlines() if path.exists() else []

    def invoke(self, profile, version="1.2.3-alpha.1", approved=True, cache=True, environment=None,
               working_directory=None, override_arguments=None):
        # A fresh fixture per invocation prevents earlier output from hiding an
        # unintended build effect. Callers with a matrix use separate setUps.
        self.write("VERSION", version + "\n")
        if approved:
            self.write("platform/builder-hosts/" + profile + "-x86_64.json", "{}\n")
        if profile == "alpine-image":
            script = self.repository / "scripts/build/build-alpine-image.sh"
            arguments = ["aarch64", str(self.output), str(self.aports), str(self.signing_key), str(self.work)]
            if cache:
                arguments.append(str(self.cache))
        else:
            script = self.repository / "scripts/build/build-buildroot-image.sh"
            arguments = [str(self.inputs / name) for name in
                         ("buildroot-2026.02.1.tar.xz", "buildroot.sign", "release-public-key",
                          "linux-6.12.1.tar.xz", "linux-6.12.1.tar.sign", "linux-public-key")]
            arguments += [str(self.output), str(self.work)]
        process_environment = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL": "C", "TZ": "UTC"}
        process_environment.update(environment or {})
        if override_arguments is not None:
            arguments = override_arguments
        return subprocess.run(["/bin/sh", str(script), *arguments],
                              cwd=working_directory or self.repository, env=process_environment, capture_output=True,
                              text=True, timeout=15)

    def no_build_effects(self):
        self.assertFalse(self.work.exists(), self.events())
        self.assertFalse(self.output.exists(), self.events())
        self.assertNotIn("archive-extraction", self.events())
        self.assertNotIn("signing-input-check", self.events())
        self.assertNotIn("build-image", self.events())

    def for_profiles(self, callback):
        for profile in PROFILES:
            with self.subTest(profile=profile):
                # Reinitialize only temporary fixtures; cleanup callbacks retain
                # ownership of every directory created by this test method.
                self.setUp()
                callback(profile)

    def test_missing_approval_stops_both_wrappers_before_build_effects(self):
        def check(profile):
            result = self.invoke(profile, approved=False)
            self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertEqual(len(self.records("verify")), 1)
            self.no_build_effects()
        self.for_profiles(check)

    def test_canonical_development_builds_remain_unqualified_and_can_run(self):
        def check(profile):
            result = self.invoke(profile, "1.2.3-dev.12", approved=False, cache=False)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn("build-image", self.events())
            self.assertEqual(self.records("verify"), [])
            self.assertEqual(self.records("host-observation"), [])
        self.for_profiles(check)

    def test_development_substrings_metadata_and_malformed_versions_cannot_exempt_builds(self):
        versions = ("1.2.3", "1.2.3-alpha.1-dev.0", "1.2.3-beta.1-dev.0", "1.2.3-rc.1-dev.0",
                    "1.2.3+fixture-dev.0", "1.2.3-dev.0+metadata", "1.2.3-dev.foo",
                    "1.2.3-dev.00", "01.2.3-dev.0", "1.2.3-dev.0\n1.2.3-dev.1")
        for profile in PROFILES:
            for version in versions:
                with self.subTest(profile=profile, version=version):
                    self.setUp()
                    # Supply the Alpine cache so its separate cache check
                    # cannot hide a host-gate development-classification bug.
                    result = self.invoke(profile, version, approved=False, cache=True)
                    self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
                    self.no_build_effects()

    def test_approved_host_does_not_remove_non_development_offline_cache_requirement(self):
        for version in ("1.2.3-alpha.1", "1.2.3-alpha.1-dev.0", "1.2.3-dev.0+metadata"):
            with self.subTest(version=version):
                self.setUp()
                result = self.invoke("alpine-image", version, cache=False)
                self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertEqual(len(self.records("verify")), 1)
                self.no_build_effects()

    def test_fixed_host_architecture_profile_policy_paths_and_actual_root_are_used_twice(self):
        def check(profile):
            result = self.invoke(profile)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            platform = "alpine" if profile == "alpine-image" else "buildroot"
            expected = {"kind": "verify", "profile": profile, "root": "/",
                        "manifest": str(self.repository / "platform/builder-hosts" / (profile + "-x86_64.json")),
                        "base_lock": str(self.repository / "platform" / platform / "base.lock"),
                        "go_version": str(self.repository / ".go-version")}
            self.assertEqual(self.records("verify"), [expected, expected])
            events = self.events()
            self.assertEqual(events[0], "host-check")
            self.assertEqual(events[-1], "host-check")
            self.assertIn("build-image", events[1:-1])
            for call in self.records("cli"):
                self.assertEqual(call["isolated"], 1)
                self.assertTrue(call["no_bytecode"])
                self.assertEqual(call["path"], "/usr/sbin:/usr/bin:/sbin:/bin")
                self.assertNotIn("--root", call["arguments"])
                self.assertNotIn("--manifest", call["arguments"])
        self.for_profiles(check)

    def test_environment_cannot_supply_development_version_root_or_unreviewed_manifest(self):
        def check(profile):
            alternate = self.root / "alternate-manifest.json"
            alternate.write_text("{}\n")
            poison = self.root / "python-poison"
            poison.mkdir()
            marker = self.root / "python-environment-ran"
            (poison / "sitecustomize.py").write_text(
                "from pathlib import Path\nPath(" + repr(str(marker)) + ").write_text('unexpected')\n")
            result = self.invoke(profile, approved=False, environment={
                "VERSION": "1.2.3-dev.0", "HHPT_BUILDER_ROOT": str(self.inputs),
                "HHPT_BUILDER_MANIFEST": str(alternate), "PYTHONPATH": str(poison),
                "PYTHONHOME": str(poison)})
            self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertEqual(len(self.records("verify")), 1)
            self.assertFalse(marker.exists())
            for call in self.records("cli"):
                self.assertEqual(call["environment"], {})
            self.no_build_effects()
        self.for_profiles(check)

    def test_changed_host_at_completion_fails_after_build_without_success_exit(self):
        def check(profile):
            (self.root / "fail-final").touch()
            result = self.invoke(profile)
            self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertEqual(len(self.records("verify")), 2)
            self.assertIn("build-image", self.events())
            self.assertEqual(self.events()[-1], "host-check")
        self.for_profiles(check)

    def test_failed_aports_queries_cannot_pass_by_printing_matching_identifiers(self):
        for query in ("v3.24.2", "v3.24.2^{commit}", "HEAD"):
            with self.subTest(query=query):
                self.setUp()
                (self.root / "fail-aports-query").write_text(query + "\n")
                result = self.invoke("alpine-image")
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(len(self.records("verify")), 1)
                self.no_build_effects()

    def test_image_build_stages_discard_ambient_tools_configuration_and_user_state(self):
        def check(profile):
            poison = self.root / "unreviewed-user-state"
            poison.mkdir()
            environment = {name: str(poison) for name in (
                "HOME", "TMPDIR", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "PYTHONHOME", "PYTHONPATH",
                "MAKEFILES", "MAKEFLAGS", "GNUMAKEFLAGS", "MAKEOVERRIDES", "MFLAGS", "BASH_ENV", "ENV",
                "CC", "CXX", "HOSTCC", "HOSTCXX", "CFLAGS", "CXXFLAGS", "LDFLAGS", "CONFIG_SITE",
                "CPATH", "LIBRARY_PATH", "PKG_CONFIG_PATH", "OPENSSL_CONF", "OPENSSL_MODULES",
                "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_COUNT", "GOENV", "GOWORK",
                "GOTOOLCHAIN", "GOFLAGS", "GOCACHEPROG")}
            environment.update(SOURCE_DATE_EPOCH="123", HHPT_JOBS="3")
            result = self.invoke(profile, environment=environment)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            phases = [json.loads(line) for line in (self.root / "phases.jsonl").read_text().splitlines()]
            self.assertIn("openssl" if profile == "alpine-image" else "make", [phase["phase"] for phase in phases])
            for phase in phases:
                actual = phase["environment"]
                for name, value in environment.items():
                    self.assertNotEqual(actual.get(name), value, (phase["phase"], name))
                self.assertEqual(actual["PATH"], "/usr/sbin:/usr/bin:/sbin:/bin")
                self.assertEqual(actual["GIT_CONFIG_GLOBAL"], "/dev/null")
                for name in ("HOME", "TMPDIR", "XDG_CONFIG_HOME", "XDG_CACHE_HOME"):
                    self.assertTrue(Path(actual[name]).is_relative_to(self.work), (phase["phase"], name))
                if phase["phase"] == "make":
                    self.assertEqual(actual["SOURCE_DATE_EPOCH"], "1700000000")
            if profile == "buildroot-image":
                self.assertTrue(any("-j3" in phase["arguments"] for phase in phases if phase["phase"] == "make"))
        self.for_profiles(check)

    def test_missing_or_invalid_source_timestamp_prevents_image_production(self):
        for profile in PROFILES:
            for failure in ("fail-source-time", "bad-source-time"):
                with self.subTest(profile=profile, failure=failure):
                    self.setUp()
                    (self.root / failure).touch()
                    result = self.invoke(profile)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertNotIn("build-image", self.events())

    def test_legacy_inputs_follow_caller_directory_but_relative_outputs_follow_checkout(self):
        foreign = self.root / "foreign-working-directory"
        foreign.mkdir()
        self.work = self.repository / "relative-work"
        self.output = self.repository / "relative-output"
        names = ("buildroot-2026.02.1.tar.xz", "buildroot.sign", "release-public-key",
                 "linux-6.12.1.tar.xz", "linux-6.12.1.tar.sign", "linux-public-key")
        arguments = ["../inputs/" + name for name in names] + ["relative-output", "relative-work"]
        result = self.invoke("buildroot-image", working_directory=foreign, override_arguments=arguments)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        phases = [json.loads(line) for line in (self.root / "phases.jsonl").read_text().splitlines()]
        make_calls = [phase["arguments"] for phase in phases if phase["phase"] == "make"]
        self.assertEqual(len(make_calls), 3)
        for call in make_calls:
            self.assertEqual(call[call.index("-C") + 1], str(self.work / "source-2026.02.1"))
            self.assertIn("O=" + str(self.work / "output-1.2.3-alpha.1-legacy-x86"), call)
            self.assertIn("BR2_DL_DIR=" + str(self.work / "downloads"), call)
        imported_keys = [Path(phase["arguments"][-1]).resolve() for phase in phases
                         if phase["phase"] == "gpg" and "--import" in phase["arguments"]]
        self.assertEqual(imported_keys, [self.inputs / "release-public-key", self.inputs / "linux-public-key"])
        self.assertTrue((self.output / "hhsoft-pc-tester-1.2.3-alpha.1-legacy-x86.iso").is_file())
        self.assertFalse((foreign / "relative-work").exists())
        self.assertFalse((foreign / "relative-output").exists())

    def test_invalid_parallelism_fails_before_host_or_build_steps(self):
        for jobs in ("0", "-1", "65", "99999999999999999999", "2 -f extra", "02", "two"):
            with self.subTest(jobs=jobs):
                self.setUp()
                result = self.invoke("buildroot-image", environment={"HHPT_JOBS": jobs})
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(self.records(), [])
                self.no_build_effects()


if __name__ == "__main__":
    unittest.main()
