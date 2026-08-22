#!/usr/bin/env python3
import json
import tempfile
import unittest
from pathlib import Path

from build_fortune import assemble, divine_json


class BuildFortuneTest(unittest.TestCase):
    def test_today_not_double_quoted(self):
        html, preset = assemble("2026-08-22", "witch-1", 1)
        self.assertIn('const TODAY = "2026-08-22";', html)
        self.assertNotIn('""2026-08-22""', html)
        self.assertNotIn("__TODAY__", html)
        self.assertNotIn("__DATA_JS__", html)
        self.assertNotIn("__PRESET_JSON__", html)
        self.assertIn("const HEXAGRAMS", html)
        self.assertEqual(preset["cheer"], 1)
        self.assertEqual(preset["salt"], "witch-1")
        self.assertIn("泽山咸", preset["original_hexagram"]["name"])
        self.assertTrue(preset["blessing"])

    def test_preset_matches_divine(self):
        html, preset = assemble("2026-08-22", "witch-1", 1)
        raw = divine_json("2026-08-22", "witch-1", 1)
        self.assertEqual(preset["original_hexagram"]["number"], raw["original_hexagram"]["number"])
        self.assertEqual(preset["fortune"], raw["fortune"])
        dumped = json.dumps(raw, ensure_ascii=False)
        self.assertIn(dumped[:80], html)

    def test_write_output(self):
        from build_fortune import main
        import sys
        with tempfile.TemporaryDirectory() as td:
            out = Path(td) / "fortune.html"
            sys.argv = ["build_fortune.py", "--date", "2026-08-22", "--salt", "witch-1", "--cheer", "1", "-o", str(out)]
            main()
            text = out.read_text(encoding="utf-8")
            self.assertGreater(out.stat().st_size, 20_000)
            self.assertIn("const PRESET = {", text)


if __name__ == "__main__":
    unittest.main()
