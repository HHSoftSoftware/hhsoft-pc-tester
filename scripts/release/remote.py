#!/usr/bin/env python3
"""Check the approved source, release identity, and published bytes.

These checks detect mutations; they cannot make GitHub's separate upload and
publish requests atomic. Protected release tags and exclusive release-writer
access are prerequisites, acknowledged by the repository's RELEASES_ENABLED
switch only after administrators have configured and tested them.
"""

import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile

from release import ROOT, digest, expected_candidate, regular_files, require, strict_json, version_for
from signatures import verify_detached
from source_assets import part_count

REPOSITORY = "HHSoftSoftware/hhsoft-pc-tester"


def api(endpoint, paginate=False):
    command = ["gh", "api", f"repos/{REPOSITORY}/{endpoint}"]
    if paginate:
        command += ["--paginate", "--slurp"]
    result = json.loads(subprocess.check_output(command, stderr=subprocess.PIPE))
    return [item for page in result for item in page] if paginate else result


def git(*arguments):
    # Local replacement objects must never change what the signed object means.
    return subprocess.check_output(["git", "--no-replace-objects", *arguments], stderr=subprocess.PIPE)


def verify_context(commit):
    require(os.environ.get("GITHUB_REPOSITORY") == REPOSITORY, "official repository required")
    require(os.environ.get("GITHUB_EVENT_NAME") == "workflow_dispatch"
            and os.environ.get("GITHUB_REF") == "refs/heads/main"
            and os.environ.get("GITHUB_REF_PROTECTED") == "true",
            "release controller must run from protected main")
    require(os.environ.get("GITHUB_SHA") == commit, "source must equal the dispatched workflow commit")
    require(os.environ.get("RELEASES_ENABLED") == "true", "release protections have not been enabled")


def verify_tag_signature(raw, root=ROOT):
    marker = b"-----BEGIN PGP SIGNATURE-----\n"
    require(raw.count(marker) == 1, "one OpenPGP tag signature required")
    payload, signature = raw.split(marker)
    require(payload.endswith(b"\n"), "invalid signed tag payload")
    with tempfile.TemporaryDirectory(prefix="hhpt-tag-verification-") as directory:
        payload_file = Path(directory) / "tag.txt"
        signature_file = Path(directory) / "tag.asc"
        payload_file.write_bytes(payload)
        signature_file.write_bytes(marker + signature)
        verify_detached(payload_file, signature_file, root / "security/release-signing-key.asc",
                        os.environ.get("RELEASE_SIGNING_KEY_FINGERPRINT", ""))


def verify_tag(tag, commit, root=ROOT):
    version_for(tag)
    require(re.fullmatch(r"[a-f0-9]{40}", commit), "full source commit required")
    ref = api("git/ref/tags/" + tag)
    require(ref["object"]["type"] == "tag", "release requires an annotated signed tag")
    tag_object = ref["object"]["sha"]
    require(re.fullmatch(r"[a-f0-9]{40}", tag_object), "invalid remote tag object")
    annotation = api("git/tags/" + tag_object)
    require(annotation["verification"]["verified"] is True, "GitHub has not verified the tag signature")
    require(annotation["object"]["type"] == "commit" and annotation["object"]["sha"] == commit,
            "tag points to a different commit")
    current_main = api("git/ref/heads/main")
    require(current_main["object"]["type"] == "commit" and current_main["object"]["sha"] == commit,
            "release source is no longer the current main tip")
    require(git("rev-parse", "HEAD").decode("ascii").strip() == commit,
            "checked-out controller differs from the dispatched source")
    require(git("rev-parse", "refs/tags/" + tag).decode("ascii").strip() == tag_object,
            "remote tag changed after checkout")
    raw = git("cat-file", "tag", tag_object)
    headers = raw.split(b"\n\n", 1)[0].splitlines()
    require(headers[:3] == [b"object " + commit.encode("ascii"), b"type commit", b"tag " + tag.encode("ascii")],
            "signed tag object identifies a different source or name")
    verify_tag_signature(raw, root)
    return tag_object


def release_state(tag, published=False):
    # GitHub's by-tag endpoint is for published releases. Enumerate drafts
    # explicitly, and paginate both releases and assets to avoid truncation.
    matches = [item for item in api("releases?per_page=100", paginate=True) if item["tag_name"] == tag]
    require(len(matches) == 1, "exactly one visible release must match the tag")
    release = matches[0]
    require(release["draft"] is (not published) and release["tag_name"] == tag,
            "release publication state differs")
    if published:
        require(release.get("immutable") is True and bool(release.get("published_at")),
                "published release must be immutable")
        require(release["prerelease"] is ("-" in tag), "published prerelease classification differs")
    return {"id": release["id"], "tag": tag,
            "assets": sorted([{key: asset[key] for key in ("id", "name", "size", "digest")}
                              for asset in api(f"releases/{release['id']}/assets?per_page=100", paginate=True)],
                             key=lambda asset: asset["name"])}


def verify_assets(current, candidate):
    files = regular_files(candidate)
    require({a["name"] for a in current["assets"]} == files and len(current["assets"]) == len(files),
            "uploaded asset set differs")
    for asset in current["assets"]:
        path = candidate / asset["name"]
        require(asset["size"] == path.stat().st_size and asset["digest"] == "sha256:" + digest(path),
                "GitHub asset digest differs from validated bytes")


def check_state(command, tag, tag_object, state_file=None, candidate=None):
    current = release_state(tag, published=command == "published")
    current["tag_object"] = tag_object
    require(state_file is not None, "saved draft identity required")
    if command == "snapshot":
        version = version_for(tag)
        names = expected_candidate(version, part_count(version, {a["name"] for a in current["assets"]}))
        require({a["name"] for a in current["assets"]} == names and len(current["assets"]) == len(names),
                "draft asset set incomplete or unexpected")
        with state_file.open("x", encoding="utf-8", newline="\n") as stream:
            json.dump(current, stream, sort_keys=True)
            stream.write("\n")
        return
    saved = strict_json(state_file)
    require(all(current[key] == saved[key] for key in ("id", "tag", "tag_object")),
            "release or signed tag was replaced after validation")
    if command == "unchanged":
        require(current == saved, "draft changed after validation")
    else:
        require(candidate is not None, "validated candidate required")
        verify_assets(current, candidate)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("tag", "snapshot", "unchanged", "uploaded", "published"))
    parser.add_argument("--tag", required=True)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--state", type=Path)
    parser.add_argument("--candidate", type=Path)
    args = parser.parse_args()
    verify_context(args.commit)
    tag_object = verify_tag(args.tag, args.commit)
    if args.command == "tag":
        return
    check_state(args.command, args.tag, tag_object, args.state, args.candidate)


if __name__ == "__main__":
    try:
        main()
        print("release remote check: passed")
    except (OSError, ValueError, KeyError, TypeError, subprocess.CalledProcessError):
        print("release remote check: failed; publication blocked", file=sys.stderr)
        sys.exit(1)
