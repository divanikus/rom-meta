import copy
import unittest
from unittest.mock import patch
from test_importer import imp

class CatalogObservationTest(unittest.TestCase):
    def test_platform_aliases_do_not_guess_between_platforms(self):
        native = {"fs_slug": "MAME", "name": "Arcade", "slug": "arcade"}
        cap = {"platform": "Arcade", "system": "catalog", "aliases": ["MAME"]}
        self.assertEqual(imp.lookup_capability(native, [cap]), cap)
        self.assertIsNone(imp.lookup_capability({"fs_slug": "Family BASIC"}, [cap]))
        self.assertIsNone(imp.lookup_capability({"fs_slug": "Nintendo - Family BASIC", "slug": "nes"}, [{"platform": "Nintendo - Nintendo Entertainment System", "system": "nes"}]))
        with self.assertRaisesRegex(ValueError, "multiple catalog"):
            imp.lookup_capability(native, [cap, dict(cap, platform="Other")])

    def test_catalog_observation_is_part_of_pinned_input(self):
        catalog = {"kind": "name-stat-v1", "directory": False, "files": [{"name": "Game.rvz", "size": 7, "mtime_ns": 123}]}
        item = {"rom": "Game.rvz", "outer": {"size": 7, "sha256": ""}, "payload": {}, "catalog": catalog}
        query = {"platform": "GameCube", "game_binding": True, "inputs": [item]}
        selection = {"platform": "GameCube", "game_binding": True, "inputs": [{**copy.deepcopy(item), "record": {"rom": item["rom"]}}]}
        imp.validate_lookup(query, selection)
        self.assertEqual(imp.input_identity(item, "catalog")["catalog"], catalog)
        selection["inputs"][0]["catalog"]["files"][0]["mtime_ns"] += 1
        with self.assertRaisesRegex(ValueError, "proof"):
            imp.validate_lookup(query, selection)

    def test_native_catalog_drift_stops_before_updates(self):
        expected = {"size": 7, "catalog": {"kind": "name-stat-v1", "directory": False, "files": []}, "system": "catalog"}
        runtime = object.__new__(imp.NativeRuntime)
        from pathlib import Path
        runtime.library = Path.cwd()
        snapshot = {"columns": {"fs_path": "roms", "fs_name": "Game.rvz"}}
        observed = {"inputs": [{"outer": {"size": 8}, "catalog": expected["catalog"]}], "skipped": []}
        with patch.object(imp, "inspect_inputs", return_value=observed):
            with self.assertRaisesRegex(ValueError, "stats or folder membership"):
                runtime.verify_input(snapshot, expected)
