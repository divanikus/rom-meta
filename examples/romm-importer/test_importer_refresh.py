import copy
import unittest
from unittest.mock import patch

import test_importer as fixtures
import test_importer_batch as batches

imp, projection, sha = fixtures.imp, fixtures.projection, fixtures.sha


class IdentityConsensusTest(unittest.TestCase):
    def entry(self, number, ids, before=None):
        return {"id": number, "record": {"game": "platforms/Test/Card" + str(number)},
                "before": {"columns": before or {"locked_fields": []}}, "patch": {},
                "providers": {"slots": {k: {"id": v} for k, v in ids.items()}}}

    def test_shared_source_identity_fills_missing_id_but_equal_names_do_not(self):
        a = self.entry(1, {"igdb": 123, "hasheous": 456})
        b = self.entry(2, {"hasheous": 456})
        c = self.entry(3, {"hasheous": 789})
        a["providers"]["slots"]["igdb"]["source_slug"] = "proven-slug"
        result = imp.reconcile_identities([a, b, c])
        self.assertEqual(b["patch"], {"igdb_id": 123, "slug": "proven-slug"})
        self.assertEqual(c["patch"], {})
        self.assertEqual(result["identity_fills"], 1)
        self.assertEqual(imp.reconcile_identities([a, b, c]), result)
        self.assertEqual(b["patch"], {"igdb_id": 123, "slug": "proven-slug"})

    def test_explicit_reference_review_blocks_cross_card_inheritance(self):
        donor = self.entry(1, {"igdb": 123, "screenscraper": 55, "hasheous": 456})
        curated = self.entry(2, {"hasheous": 456})
        curated["providers"]["reference_reviews"] = [{"from_id": 123, "action": "suppress"}]
        curated["patch"] = {"igdb_id": 123, "slug": "wrong-product"}
        curated["identity_fills"] = [{"field": "igdb_id", "value": 123}]
        result = imp.reconcile_identities([donor, curated])
        self.assertEqual(curated["patch"], {})
        self.assertEqual(result["identity_fills"], 0)

    def test_conflicting_ids_owner_values_and_unbound_metadata_block_fills(self):
        entries = [self.entry(1, {"igdb": 123, "hasheous": 456}),
                   self.entry(2, {"igdb": 999, "hasheous": 456}),
                   self.entry(3, {"hasheous": 456})]
        result = imp.reconcile_identities(entries)
        self.assertEqual(len(result["identity_conflicts"]), 1)
        self.assertTrue(all(not e["patch"] for e in entries))
        for before in ({"igdb_id": 999}, {"locked_fields": ["igdb_id"]}, {"igdb_metadata": {"owner": "data"}}):
            a = self.entry(1, {"igdb": 123, "hasheous": 456})
            b = self.entry(2, {"hasheous": 456}, before)
            imp.reconcile_identities([a, b])
            self.assertEqual(b["patch"], {})

    def test_indirect_reference_does_not_join_distinct_games(self):
        a = self.entry(1, {"igdb": 1, "screenscraper": 2, "hasheous": 3}, {"tgdb_id": 777})
        b = self.entry(2, {"igdb": 4, "screenscraper": 5, "hasheous": 6}, {"tgdb_id": 777})
        for e in (a, b):
            e["providers"]["slots"]["tgdb"] = {"id": 777, "source": "hasheous-reference"}
        result = imp.reconcile_identities([a, b])
        self.assertEqual(len(result["reference_only"]), 1)
        self.assertTrue(all(e["patch"] == {"tgdb_id": None} for e in (a, b)))

    def test_regional_provider_ids_do_not_block_unambiguous_identity_or_covers(self):
        entries = [self.entry(1, {"igdb": 123, "launchbox": 456}),
                   self.entry(2, {"igdb": 123, "launchbox": 789}),
                   self.entry(3, {"launchbox": 456})]
        entries[0]["patch"] = {"path_cover_l": "new/front.png"}
        result = imp.reconcile_identities(entries)
        self.assertEqual(entries[2]["patch"], {"igdb_id": 123})
        self.assertEqual(entries[0]["patch"], {"path_cover_l": "new/front.png"})
        self.assertEqual(result["held_ids"], [])


class CoverUpdateTest(unittest.TestCase):
    def setUp(self):
        self.fixture = fixtures.ImporterTest()
        self.fixture.setUp()
        self.addCleanup(self.fixture.doCleanups)
        self.runtime, self.api = self.fixture.runtime, self.fixture.api
        for row in self.runtime.rows.values():
            row["columns"]["slug"] = "source-proven-game"
        self.asset = {"id": "2" * 64, "uri": "/v1/assets/" + "2" * 64,
            "path": self.fixture.card_id + "/media/box-front.usa.igdb.png", "kind": "box-front",
            "source": "igdb", "size": 3, "sha256": sha(b"NEW")}
        self.api.value["media_mode"] = "covers"
        self.api.value["cards"][self.fixture.card_id]["assets"] = [self.asset]
        self.api.content[self.asset["id"]] = b"NEW"
        self.baseline = {"_digest": "a" * 64}
        for item, row in zip(self.api.value["inputs"], self.runtime.rows.values()):
            item["record"]["media"] = {"cover": {"source_path": self.asset["path"]}}
            cols = row["columns"]
            self.baseline[str(cols["id"])] = {"source_sha256": sha(b"OLD"),
                "before": {k: cols.get(k) for k in ("path_cover_l", "path_cover_s", "url_cover")}}

    def plan(self):
        return imp.build_plan(self.runtime, self.api, 49, "roms/Test/retail",
                              projector=projection, update_mode=True, cover_baseline=self.baseline)

    def test_cover_only_update_preserves_other_media_and_repeat_reads_none(self):
        actual = imp.hash_file
        reads = []
        with patch.object(imp, "hash_file", lambda p: (reads.append(str(p)), actual(p))[1]), patch.object(self.runtime, "verify_input", wraps=self.runtime.verify_input) as verified:
            plan = self.plan()
        self.assertEqual(verified.call_count, 0)  # already observed during lookup
        media_reads = [p for p in reads if "resources" in p]
        self.assertEqual(len(media_reads), 4)  # exactly changed old big/small pairs
        self.assertTrue(all("/cover/" in p.replace("\\", "/") for p in media_reads))
        self.assertTrue(all(f["role"] == "cover" for e in plan["entries"] for f in e["files"]))
        imp.apply_plan(self.runtime, self.api, plan, self.fixture.root / "receipt.json", self.fixture.root)
        def no_media(path):
            if "resources" in str(path):
                raise AssertionError("repeat reread media")
            return actual(path)
        with patch.object(imp, "hash_file", side_effect=no_media):
            repeat = self.plan()
        self.assertTrue(all(not e["patch"] and not e["files"] for e in repeat["entries"]))

    def test_scope_or_consensus_tamper_is_rejected_before_any_publication(self):
        plan = self.plan()
        bad = copy.deepcopy(plan)
        bad["resource_scope"]["1"].append("outside/video.mp4")
        with self.assertRaisesRegex(ValueError, "resource scope"):
            imp.preflight(self.runtime, self.api, bad)
        bad = copy.deepcopy(plan)
        bad["quality"]["identity_fills"] += 1
        with self.assertRaisesRegex(ValueError, "identity consensus"):
            imp.preflight(self.runtime, self.api, bad)
        self.assertEqual((self.runtime.updates, self.runtime.publications, self.api.downloads), (0, 0, 0))

    def test_explicit_owner_cover_lock_and_same_source_are_not_refreshed(self):
        self.runtime.rows[1]["columns"]["locked_fields"].append("path_cover_l")
        self.baseline["2"]["source_sha256"] = self.asset["sha256"]
        plan = self.plan()
        self.assertTrue(all(not e["patch"] and not e["files"] for e in plan["entries"]))

    def test_generated_baseline_refresh_apply_and_repeat_preserve_video(self):
        class CoverCatalog(batches.Catalog):
            def lookup(self, query):
                value = super().lookup(query)
                value["media_mode"] = query.get("media", "")
                value["game_binding"] = query.get("game_binding", False)
                value.pop("digest", None)
                value["digest"] = imp.digest(value)
                return value
        old = copy.deepcopy(self.asset)
        old.update(id="1" * 64, sha256=sha(b"OLD"), path=old["path"].replace("igdb", "emumovies"))
        self.api.value["cards"][self.fixture.card_id]["assets"] = [old]
        for item in self.api.value["inputs"]:
            item["record"]["media"]["cover"]["source_path"] = old["path"]
        catalog = CoverCatalog(self.api.value, self.api.content)
        video_path = "owner/large-video.mp4"
        self.runtime.resource_path(video_path).parent.mkdir(parents=True)
        self.runtime.resource_path(video_path).write_bytes(b"preserved video")
        for row in self.runtime.rows.values():
            row["columns"]["gamelist_metadata"]["video_path"] = video_path
        self.runtime.rows[2]["columns"]["locked_fields"].append("path_cover_l")
        actual = imp.hash_file
        def no_video(path):
            if str(path).endswith(".mp4"):
                raise AssertionError("read existing video")
            return actual(path)
        before = self.fixture.root / "baseline.json"
        target = self.fixture.root / "refresh.json"
        with patch.object(imp, "inspect_inputs", batches.inspect_raw), patch.object(imp, "hash_file", side_effect=no_video):
            baseline = imp.update_batch(self.runtime, catalog, before, ["Sega Game Gear"])
            self.assertEqual(len(baseline["covers"]), 2)
            with self.assertRaises(ValueError):
                imp.run_batch(self.runtime, catalog, baseline, before)
            catalog.value["cards"][self.fixture.card_id]["assets"] = [self.asset]
            for item in catalog.value["inputs"]:
                item["record"]["media"]["cover"]["source_path"] = self.asset["path"]
            plan = imp.update_batch(self.runtime, catalog, target, ["Sega Game Gear"], before)
            self.assertEqual(len(plan["parts"]), 1)
            self.assertEqual(plan["changed"], 1)
            with patch.object(imp, "preflight", wraps=imp.preflight) as checked, patch.object(imp, "inspect_inputs", wraps=batches.inspect_raw) as observed:
                imp.run_batch(self.runtime, catalog, plan, target, self.fixture.root / "batch-result.json")
            self.assertEqual(checked.call_count, 1)
            self.assertEqual(len(observed.call_args.args[2]), 1)  # changed ROM only
            repeat = imp.update_batch(self.runtime, catalog, self.fixture.root / "repeat.json", ["Sega Game Gear"], before)
            self.assertEqual((repeat["changed"], repeat["resource_bytes"]), (0, 0))
            self.assertEqual(len(imp.cached_observations([target])), 2)
            with patch.object(imp, "inspect_inputs", side_effect=AssertionError("unnecessary repeat input observation")):
                reused = imp.update_batch(self.runtime, catalog, self.fixture.root / "reused.json", ["Sega Game Gear"], before, [target])
            self.assertEqual(reused["changed"], 0)
        self.assertEqual(self.runtime.resource_path(video_path).read_bytes(), b"preserved video")
        self.assertTrue(all(row["columns"]["gamelist_metadata"]["video_path"] == video_path for row in self.runtime.rows.values()))


if __name__ == "__main__":
    unittest.main()
