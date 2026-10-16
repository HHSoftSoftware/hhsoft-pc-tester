import csv
from pathlib import Path
import tempfile
import unittest

import write_buildroot_sbom as sbom


class BuildrootSBOMTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.image = self.root / "fixture.iso"
        self.image.write_bytes(b"synthetic image")
        self.manifest = self.root / "packages.csv"
        self.rows = [
            ["kernel", "1.2.3", "GPL-2.0, MIT", "COPYING LICENSE", "kernel.tar.xz", "https://example.invalid/sources", ""],
            ["hhsoft-pc-tester", "0.1.0-alpha.1", "BSD-3-Clause", "LICENSE", "NOASSERTION", "", ""],
        ]

    def render(self):
        with self.manifest.open("w", encoding="utf-8", newline="") as stream:
            writer = csv.writer(stream)
            writer.writerow(sbom.FIELDS)
            writer.writerows(self.rows)
        return sbom.render(self.manifest, self.image, "0.1.0-alpha.1", 1700000000)

    def test_inventory_binds_image_and_preserves_license_details(self):
        output = self.render()
        self.assertIn("PackageChecksum: SHA256: " + sbom.sha256(self.image), output)
        self.assertIn("Buildroot declared licenses: GPL-2.0, MIT", output)
        self.assertNotIn("PackageLicenseDeclared: GPL-2.0, MIT", output)
        self.assertIn("https://example.invalid/sources/kernel.tar.xz", output)
        self.assertEqual(output.count("PackageName:"), 3)

    def test_reordered_manifest_is_deterministic(self):
        output = self.render()
        self.rows.reverse()
        self.assertEqual(self.render(), output)

    def test_duplicate_and_wrong_product_versions_fail(self):
        self.rows.append(self.rows[0])
        with self.assertRaises(ValueError):
            self.render()
        self.rows.pop()
        self.rows[1][1] = "0.2.0"
        with self.assertRaises(ValueError):
            self.render()

    def test_multiline_metadata_cannot_inject_spdx_fields(self):
        self.rows[0][2] = "MIT\nPackageName: forged"
        with self.assertRaises(ValueError):
            self.render()

    def test_private_source_locations_are_not_exported(self):
        credential_url = "https://" + "user:password@" + "example.invalid"
        self.assertEqual(sbom.download_location(credential_url, "source.tar.xz"), "NOASSERTION")
        self.assertEqual(sbom.download_location("file:///private/build", "source.tar.xz"), "NOASSERTION")
        with self.assertRaises(ValueError):
            sbom.download_location("https://example.invalid", "../source.tar.xz")


if __name__ == "__main__":
    unittest.main()
