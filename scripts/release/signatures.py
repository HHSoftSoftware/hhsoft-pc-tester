"""Verify release signatures against one configured public primary key offline."""

from pathlib import Path
import re
import subprocess
import tempfile


def verify_detached(payload: Path, signature: Path, public_key: Path, fingerprint: str):
    if not re.fullmatch(r"(?:[0-9A-F]{40}|[0-9A-F]{64})", fingerprint):
        raise ValueError("approved signing fingerprint is not configured")
    for path in (payload, signature, public_key):
        if not path.is_file() or path.is_symlink():
            raise ValueError("signature input must be a regular file")
    if public_key.stat().st_size > 256 * 1024 or signature.stat().st_size > 64 * 1024:
        raise ValueError("oversized signature input")
    key = public_key.read_text(encoding="ascii")
    if not key.startswith("-----BEGIN PGP PUBLIC KEY BLOCK-----") or "PRIVATE KEY" in key:
        raise ValueError("only an armored public verification key is allowed")
    with tempfile.TemporaryDirectory(prefix="hhpt-verification-") as directory:
        Path(directory).chmod(0o700)
        command = ["gpg", "--no-options", "--homedir", directory, "--batch", "--no-tty",
                   "--no-auto-key-retrieve", "--no-auto-key-import", "--no-auto-key-locate"]
        subprocess.run([*command, "--import", str(public_key.resolve())], capture_output=True, check=True)
        listing = subprocess.run([*command, "--with-colons", "--list-keys"],
                                 capture_output=True, text=True, check=True)
        primary = []
        awaiting_primary = False
        for line in listing.stdout.splitlines():
            fields = line.split(":")
            if fields[0] == "pub":
                awaiting_primary = True
            elif fields[0] == "fpr" and awaiting_primary:
                primary.append(fields[9])
                awaiting_primary = False
        if primary != [fingerprint]:
            raise ValueError("verification key differs from configured primary fingerprint")
        result = subprocess.run([*command, "--status-fd", "1", "--verify",
                                 str(signature.resolve()), str(payload.resolve())],
                                capture_output=True, text=True, check=True)
        check_status(result.stdout, fingerprint)


def check_status(status, fingerprint):
    valid = []
    rejected = {"BADSIG", "ERRSIG", "EXPSIG", "EXPKEYSIG", "REVKEYSIG", "KEYEXPIRED",
                "SIGEXPIRED", "KEYREVOKED", "NO_PUBKEY", "NODATA", "FAILURE", "ERROR"}
    for line in status.splitlines():
        fields = line.split()
        if len(fields) < 2 or fields[0] != "[GNUPG:]":
            continue
        if fields[1] in rejected:
            raise ValueError("invalid, revoked, or expired signature")
        if fields[1] == "VALIDSIG":
            values = fields[2:]
            if len(values) not in (9, 10) or values[7] not in {"8", "9", "10", "11"}:
                raise ValueError("unsupported signature status or digest")
            valid.append(values[9] if len(values) == 10 else values[0])
    if valid != [fingerprint]:
        raise ValueError("signature does not match the approved primary key")
