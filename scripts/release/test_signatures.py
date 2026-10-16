import unittest
from pathlib import Path
import shutil
import subprocess
import tempfile

from signatures import check_status, verify_detached


class SignatureStatusTests(unittest.TestCase):
    def setUp(self):
        self.fingerprint = "A1" * 20
        self.valid = "[GNUPG:] VALIDSIG " + "B2" * 20 + " 2026-10-15 1792022400 0 4 0 22 10 00 " + self.fingerprint

    def test_expected_primary_accepts_signing_subkey(self):
        check_status(self.valid, self.fingerprint)

    def test_wrong_signer_weak_hash_and_missing_signature_fail(self):
        for status in ("", self.valid.replace(self.fingerprint, "C3" * 20),
                       self.valid.replace("22 10 00", "22 2 00"), self.valid + "\n" + self.valid):
            with self.subTest(status=status), self.assertRaises(ValueError):
                check_status(status, self.fingerprint)

    def test_expired_revoked_and_error_statuses_fail(self):
        for state in ("EXPKEYSIG", "REVKEYSIG", "EXPSIG", "BADSIG", "ERRSIG", "FAILURE", "KEYEXPIRED"):
            with self.subTest(state=state), self.assertRaises(ValueError):
                check_status(self.valid + "\n[GNUPG:] " + state, self.fingerprint)

    @unittest.skipUnless(shutil.which("gpg"), "GnuPG required for real signature integration")
    def test_real_signature_and_tampering(self):
        # Disposable fixture key only. No user keyring or network access.
        with tempfile.TemporaryDirectory(prefix="hhpt-signature-test-") as temporary:
            directory = Path(temporary)
            signer = directory / "signer"
            signer.mkdir(mode=0o700)
            command = ["gpg", "--no-options", "--homedir", str(signer), "--batch", "--no-tty",
                       "--pinentry-mode", "loopback", "--passphrase", ""]
            try:
                subprocess.run([*command, "--quick-generate-key", "HHPT Test <test@example.invalid>",
                                "ed25519", "sign", "0"], capture_output=True, check=True)
                keys = subprocess.check_output([*command, "--with-colons", "--list-keys"], stderr=subprocess.DEVNULL, text=True)
                fingerprint = next(line.split(":")[9] for line in keys.splitlines() if line.startswith("fpr:"))
                public = directory / "public.asc"
                public.write_bytes(subprocess.check_output([*command, "--armor", "--export", fingerprint]))
                payload = directory / "record.json"
                payload.write_bytes(b'{"fixture":true}\n')
                signature = directory / "record.json.asc"
                subprocess.run([*command, "--armor", "--output", str(signature), "--detach-sign", str(payload)],
                               capture_output=True, check=True)
                verify_detached(payload, signature, public, fingerprint)
                with self.assertRaises(ValueError):
                    verify_detached(payload, signature, public, "C3" * 20)
                payload.write_bytes(b'{"fixture":false}\n')
                with self.assertRaises(subprocess.CalledProcessError):
                    verify_detached(payload, signature, public, fingerprint)
            finally:
                if shutil.which("gpgconf"):
                    subprocess.run(["gpgconf", "--homedir", str(signer), "--kill", "all"], capture_output=True)


if __name__ == "__main__":
    unittest.main()
