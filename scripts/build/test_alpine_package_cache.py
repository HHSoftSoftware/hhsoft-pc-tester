import json
from pathlib import Path
import tempfile
import unittest

import alpine_package_cache as cache


class AlpineCacheTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.base = self.root / "base.lock"
        self.base.write_text("VERSION=fixture\n")
        self.cache = self.root / "cache"
        for repository in ("main", "community"):
            path = self.cache / repository / "x86_64"
            path.mkdir(parents=True)
            (path / "APKINDEX.tar.gz").write_bytes(b"synthetic signed index")
        self.package = self.cache / "main/x86_64/fixture-1.0-r0.apk"
        self.package.write_bytes(b"synthetic package")
        self.lock = self.root / "packages.json"
        self.lock.write_text(json.dumps(cache.create_lock(self.cache, "x86_64", self.base)))

    def check(self):
        cache.verify(self.cache, "x86_64", self.base, self.lock)

    def test_exact_cache_matches_reviewed_lock(self):
        self.check()

    def test_changed_package_is_rejected(self):
        self.package.write_bytes(b"replacement package")
        with self.assertRaises(ValueError):
            self.check()

    def test_changed_index_is_rejected(self):
        (self.cache / "community/x86_64/APKINDEX.tar.gz").write_bytes(b"new index")
        with self.assertRaises(ValueError):
            self.check()

    def test_unlocked_package_and_missing_package_are_rejected(self):
        extra = self.package.with_name("extra.apk")
        extra.write_bytes(b"unreviewed dependency")
        with self.assertRaises(ValueError):
            self.check()
        extra.unlink()
        self.package.unlink()
        with self.assertRaises(ValueError):
            self.check()

    def test_different_base_or_architecture_is_rejected(self):
        with self.assertRaises(ValueError):
            cache.verify(self.cache, "aarch64", self.base, self.lock)
        self.base.write_text("VERSION=another-base\n")
        with self.assertRaises(ValueError):
            self.check()

    def test_extra_state_and_duplicate_json_keys_are_rejected(self):
        extra = self.cache / "local-state.txt"
        extra.write_text("local only")
        with self.assertRaises(ValueError):
            self.check()
        extra.unlink()
        self.lock.write_text('{"schema":1,"schema":1}')
        with self.assertRaises(ValueError):
            self.check()


if __name__ == "__main__":
    unittest.main()
