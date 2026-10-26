import copy
import io
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import test_importer as fixtures
from test_importer import imp


class DiscImporterTest(unittest.TestCase):
    def setUp(self):
        self.fixture = fixtures.ImporterTest()
        self.fixture.setUp()
        self.addCleanup(self.fixture.doCleanups)
        self.runtime, self.root = self.fixture.runtime, self.fixture.root
        self.platform = {"id": 49, "name": "3DO Interactive Multiplayer", "custom_name": None, "slug": "3do", "fs_slug": "Panasonic - 3DO Interactive Multiplayer"}
        self.runtime.platforms = lambda: [copy.deepcopy(self.platform)]
        self.observations = {}
        self.original_hash = imp.hash_file
        for rom_id, count in ((1, 2), (2, 1)):
            row = self.runtime.rows[rom_id]
            c = row["columns"]
            (self.runtime.library / c["fs_path"] / c["fs_name"]).unlink()
            c["fs_name"] = "Disc Game (USA)" if count > 1 else "Single (USA).chd"
            c["fs_size_bytes"] = 512 * count
            row["platform_fs_slug"] = self.platform["fs_slug"]
            names = [f"Disc Game (USA) (Disc {n}).chd" for n in range(1, count + 1)] if count > 1 else [c["fs_name"]]
            parent = Path(c["fs_path"]) / c["fs_name"] if count > 1 else Path(c["fs_path"])
            files = []
            row["files"] = []
            for index, name in enumerate(names):
                target = self.runtime.library / parent / name
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(b"x" * 512)
                files.append({"name": name, "size": 512, "mtime_ns": target.stat().st_mtime_ns, "header": "header-byte-fixture"})
                row["files"].append({"id": rom_id * 10 + index, "rom_id": rom_id, "file_path": parent.as_posix(), "file_name": name, "file_size_bytes": 512})
            name = str(Path(c["fs_path"]) / c["fs_name"]).replace("\\", "/")
            self.observations[name] = {"rom": name, "outer": {"size": 512 * count, "sha256": ""}, "payload": {"size": 0, "sha1": "", "sha256": "", "md5": "", "crc32": ""}, "disc": {"kind": "chd-header-stat-v1", "directory": count > 1, "files": files}}
        self.probe = patch.object(imp, "inspect_inputs", self.inspect)
        self.probe.start()
        self.addCleanup(self.probe.stop)
        self.projection = patch.object(imp, "project", self.project)
        self.projection.start()
        self.addCleanup(self.projection.stop)
        output = patch("sys.stdout", io.StringIO())
        output.start()
        self.addCleanup(output.stop)
        class Catalog(fixtures.FakeAPI):
            def capabilities(self):
                return {"schema_version": 1, "mode": "catalog", "platforms": [{"platform": "3DO Interactive Multiplayer", "system": "3do", "aliases": ["Panasonic - 3DO Interactive Multiplayer"], "supported": True, "game_binding": True}]}
            def lookup(self, query):
                value = copy.deepcopy(self.value)
                value.update(platform=query["platform"], system="3do", game_binding=query.get("game_binding", False), inputs=[])
                for observed in query["inputs"]:
                    item = copy.deepcopy(self.value["inputs"][0])
                    item.update(outer=observed["outer"], payload=observed["payload"], disc=observed["disc"])
                    item["record"].update(rom=observed["rom"], matched_by="game-name", metadata_release="Game (USA)", disc=observed["disc"])
                    item["record"].pop("release", None)  # Actual Go omitempty game-binding shape.
                    value["inputs"].append(item)
                value["coverage"] = {"considered": len(value["inputs"]), "included": len(value["inputs"]), "skipped": []}
                value.pop("digest", None)
                value["digest"] = imp.digest(value)
                return value
        self.api = Catalog(self.fixture.api.value, self.fixture.content)

    def inspect(self, runtime, system, names):
        self.assertEqual(system, "3do")
        return {"inputs": [copy.deepcopy(self.observations[name]) for name in names], "skipped": []}

    def project(self, selection, rows):
        value = fixtures.projection(selection, rows)
        for item in value.values():
            item["providers"]["verification"] = {"flags": {}, "payload_sha1": "", "payload_sha256": ""}
        return value

    def test_disc_alias_grouping_apply_preserves_members_and_never_hashes_chd(self):
        before = {key: (copy.deepcopy(row["files"]), copy.deepcopy(row["users"])) for key, row in self.runtime.rows.items()}
        def guarded_hash(path):
            self.assertNotEqual(Path(path).suffix, ".chd", "CHD payload read instead of bounded observation")
            return self.original_hash(path)
        with patch.object(imp, "hash_file", guarded_hash):
            path = self.root / "disc-plan.json"
            plan = imp.build_batch(self.runtime, self.api, path)
            self.assertEqual((plan["entries"], plan["platforms"][0]["game_only"], plan["platforms"][0]["exact"]), (2, 2, 0))
            child = json.loads((path.parent / plan["parts"][0]["path"]).read_text())
            self.assertTrue(child["lookup"]["game_binding"])
            self.assertEqual(child["platform_fs_slug"], self.platform["fs_slug"])
            self.assertEqual(child["lookup"]["platform"], "3DO Interactive Multiplayer")
            self.assertTrue(all(not e["providers"]["verification"]["flags"] for e in child["entries"]))
            imp.run_batch(self.runtime, self.api, plan, path)
            imp.run_batch(self.runtime, self.api, plan, path, self.root / "disc-receipt.json")
        for key, row in self.runtime.rows.items():
            self.assertEqual((row["files"], row["users"]), before[key])

    def test_changed_header_or_native_member_fails_before_writes(self):
        path = self.root / "changed-plan.json"
        plan = imp.build_batch(self.runtime, self.api, path)
        name = next(iter(self.observations))
        self.observations[name]["disc"]["files"][0]["header"] = "changed"
        with self.assertRaisesRegex(ValueError, "observations changed"):
            imp.run_batch(self.runtime, self.api, plan, path, self.root / "rejected.json")
        self.assertEqual(self.runtime.updates, 0)
        snapshot = self.runtime.snapshot(1)
        expected = copy.deepcopy(self.observations[name])
        snapshot["files"][0]["file_size_bytes"] += 1
        with self.assertRaisesRegex(ValueError, "members differ"):
            self.runtime.verify_input(snapshot, imp.input_identity(expected, "3do"))

    def test_match_filters_before_limit_and_ambiguous_alias_is_rejected(self):
        path = self.root / "matched-plan.json"
        plan = imp.build_batch(self.runtime, self.api, path, limit=1, match=["sINgle", "absent"])
        report = plan["platforms"][0]
        self.assertEqual((report["total"], report["considered"], report["included"], report["filtered_out"]), (2, 1, 1, 1))
        self.assertEqual(plan["parts"][0]["ids"], [2])
        caps = self.api.capabilities()["platforms"]
        with self.assertRaisesRegex(ValueError, "multiple catalog"):
            imp.lookup_capability(self.platform, caps + [dict(caps[0], platform="Alternate")])
        # A full filesystem match still takes precedence over a loose system alias.
        exact = dict(caps[0], platform=self.platform["fs_slug"])
        self.assertEqual(imp.lookup_capability(self.platform, caps + [exact]), exact)

    def test_game_only_go_json_uses_metadata_region_without_proving_release(self):
        record = json.loads('{"rom":"Game (USA).chd","matched_by":"game-name","metadata_release":"Game (USA)"}')
        card = {"game": {"data": {"releases": {"Game (USA)": {"regions": ["USA"]}, "Game (Europe)": {"regions": ["Europe"]}}}}}
        usa = {"source": "igdb", "region": "usa", "path": "card/media/box-front.usa.igdb.png"}
        europe = dict(usa, region="europe", path="card/media/box-front.europe.igdb.png")
        self.assertLess(imp.asset_rank(usa, record, card, {}), imp.asset_rank(europe, record, card, {}))
        donor = dict(usa, path="card/media/Game (USA)/box-front.usa.igdb.png")
        self.assertLess(imp.asset_rank(usa, record, card, {}), imp.asset_rank(donor, record, card, {}))
        self.assertNotIn("release", record)  # Ranking hints never invent exact identity.
        exact = dict(record, release="Game (Europe)")
        self.assertLess(imp.asset_rank(europe, exact, card, {}), imp.asset_rank(usa, exact, card, {}))
        imp.asset_rank(usa, {"rom": "Unknown.chd"}, card, {})
