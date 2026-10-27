import copy
import unittest
from unittest.mock import patch
import test_importer as fixtures

imp = fixtures.imp


class VariantImporterTest(unittest.TestCase):
    def setUp(self):
        self.fixture = fixtures.ImporterTest()
        self.fixture.setUp()
        self.addCleanup(self.fixture.doCleanups)
        self.root = self.fixture.root
        self.runtime = self.fixture.runtime
        self.api = self.fixture.api
        card = self.api.value["cards"][self.fixture.card_id]
        card["game"]["data"]["releases"]["Original (Japan)"] = {"regions": ["Japan"]}
        self.info = {"kind": "translation", "based_on": "Original (Japan)", "chain": ["English"], "languages": ["en"], "group": "Translation team", "version": "1.0"}
        for item in self.api.value["inputs"]:
            item["record"].pop("release", None)
            item["record"].update(variant="English", metadata_release="Original (Japan)", variant_info=copy.deepcopy(self.info), matched_by="hash")
        self.api.value["digest"] = imp.digest(self.api.value)

    def project(self, selection, rows):
        result = {}
        for item, row in rows:
            record = item["record"]
            form = {}
            if not row["columns"].get("summary"):
                form["summary"] = "Inherited original prose"
            result[record["rom"]] = {
                "form": form,
                "providers": {
                    "slots": {},
                    "derivative": {"variant": record["variant"], "metadata_release": record["metadata_release"], "matched_by": record["matched_by"], "variant_info": record["variant_info"]},
                    "verification": {"flags": {}, "authority": "", "payload_sha1": item["outer"]["sha256"][:40]},
                    "unsupported": ["Translation credits retained in derivative provenance"],
                },
            }
        return result

    def plan(self):
        return imp.build_plan(self.runtime, self.api, 49, "roms/Test/retail", {1, 2}, projector=self.project)

    def test_variant_apply_preserves_owner_identity_and_repeat_has_no_writes(self):
        self.runtime.rows[1]["columns"]["summary"] = ""
        before = {i: (copy.deepcopy(r["files"]), copy.deepcopy(r["users"]), r["columns"]["name"]) for i, r in self.runtime.rows.items()}
        with patch.object(imp, "project", self.project):
            plan = self.plan()
            for entry in plan["entries"]:
                self.assertEqual(entry["record"]["variant"], "English")
                self.assertEqual(entry["providers"]["derivative"]["variant_info"], self.info)
                self.assertFalse(entry["providers"]["verification"]["flags"])
                self.assertNotIn("release", entry["record"])
            imp.apply_plan(self.runtime, self.api, plan, self.root / "variants.json", self.root)
            self.assertEqual(self.runtime.rows[1]["columns"]["summary"], "Inherited original prose")
            counts = self.runtime.updates, self.runtime.publications, self.api.downloads
            repeated = self.plan()
            self.assertFalse(any(e["patch"] or e["files"] for e in repeated["entries"]))
            self.assertEqual(repeated["resource_bytes"], 0)
            imp.apply_plan(self.runtime, self.api, repeated, self.root / "variants-repeat.json", self.root)
            self.assertEqual((self.runtime.updates, self.runtime.publications, self.api.downloads), counts)
        for i, row in self.runtime.rows.items():
            self.assertEqual((row["files"], row["users"], row["columns"]["name"]), before[i])
            self.assertFalse(row["columns"].get("hasheous_metadata", {}).get("nointro_match", False))

    def test_variant_owner_media_precedes_base_and_donor_without_promoting_release(self):
        record = self.api.value["inputs"][0]["record"]
        card = self.api.value["cards"][self.fixture.card_id]
        record["variant_info"]["chain"] = ["English", "Parent translation"]
        source = {"source": "igdb", "region": "japan", "variant": "unknown"}
        assets = [dict(source, path="card/media/" + owner + "/box-front.japan.igdb.png") for owner in ("English", "Parent translation", "Original (Japan)")]
        assets += [dict(source, path="card/media/box-front.japan.igdb.png"), dict(source, source="emumovies", path="card/media/Other/box-front.japan.emumovies.png")]
        ranks = [imp.asset_rank(a, record, card, {}) for a in assets]
        self.assertEqual(sorted(range(len(ranks)), key=lambda i: ranks[i]), list(range(len(ranks))))
        self.assertNotIn("release", record)

    def test_header_variant_remains_explicitly_unverified(self):
        for item in self.api.value["inputs"]:
            item["record"]["matched_by"] = "variant-chd-header"
        plan = self.plan()
        quality = imp.reconcile_identities(plan["entries"])
        self.assertEqual(set(quality["unverified_game_bindings"]), {1, 2})


if __name__ == "__main__":
    unittest.main()
