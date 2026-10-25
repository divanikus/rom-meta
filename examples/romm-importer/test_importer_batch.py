import copy
import hashlib
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import zlib

import test_importer as fixtures
from test_importer import imp, FakeAPI, FakeRuntime

# Use the same real temporary native files as the existing guarded apply tests.
# Go's raw/ZIP reader is exercised separately; this seam computes raw observations.
def inspect_raw(runtime, system, names):
    inputs, skipped = [], []
    for name in names:
        data = (runtime.library / name).read_bytes()
        if Path(name).suffix != ".gg":
            skipped.append({"rom": name, "status": "unsupported", "reason": "unsupported archive"})
            continue
        proof = {"size": len(data), "sha1": hashlib.sha1(data).hexdigest(), "sha256": hashlib.sha256(data).hexdigest(), "md5": hashlib.md5(data).hexdigest(), "crc32": f"{zlib.crc32(data):08x}"}
        inputs.append({"rom": name, "outer": imp.hash_file(runtime.library / name), "payload": proof})
    return {"inputs": inputs, "skipped": skipped}


class Catalog(FakeAPI):
    def capabilities(self):
        return {"schema_version": 1, "mode": "catalog", "platforms": [
            {"platform": "Sega - Game Gear", "system": "gamegear", "supported": True},
            {"platform": "Sega - Master System - Mark III", "system": "mastersystem", "supported": True},
            {"platform": "Sony - PlayStation 2", "system": "ps2", "supported": False, "reason": "disc proof not supported"}]}

    def lookup(self, query):
        value = copy.deepcopy(self.value)
        value["platform"] = query["platform"]
        value["inputs"] = []
        skipped = []
        for observation in query["inputs"]:
            if "Unknown" in observation["rom"]:
                skipped.append({"rom": observation["rom"], "status": "unmatched", "reason": "no exact payload fingerprint"})
                continue
            item = copy.deepcopy(self.value["inputs"][0])
            item["record"]["rom"] = observation["rom"]
            item["outer"] = observation["outer"]
            item["payload"] = observation["payload"]
            value["inputs"].append(item)
        value["coverage"] = {"considered": len(query["inputs"]), "included": len(value["inputs"]), "skipped": skipped}
        value.pop("digest", None)
        value["digest"] = imp.digest(value)
        return value


class BatchTest(unittest.TestCase):
    def setUp(self):
        # Reuse the established rich media/owner fixtures without inheriting tests.
        self.fixture = fixtures.ImporterTest()
        self.fixture.setUp()
        self.addCleanup(self.fixture.doCleanups)
        self.runtime = self.fixture.runtime
        self.api = Catalog(self.fixture.api.value, self.fixture.content)
        self.root = self.fixture.root
        self.plan_path = self.root / "plan.json"
        self.probe = patch.object(imp, "inspect_inputs", inspect_raw)
        self.probe.start()
        self.addCleanup(self.probe.stop)
        self.output = patch("sys.stdout", io.StringIO())
        self.output.start()
        self.addCleanup(self.output.stop)

    def build(self, **kw):
        return imp.build_batch(self.runtime, self.api, self.plan_path, **kw)

    def add_platform(self, platform_id, folder, supported_game=True):
        platforms = self.runtime.platforms()
        platforms.append({"id": platform_id, "name": folder, "custom_name": None, "slug": "other", "fs_slug": folder})
        self.runtime.platforms = lambda: copy.deepcopy(platforms)
        row = copy.deepcopy(self.runtime.rows[1])
        row["platform_fs_slug"] = folder
        row["columns"].update(id=platform_id * 100, platform_id=platform_id, fs_path=f"roms/Other{platform_id}", fs_name="Game.gg", fs_size_bytes=4)
        self.runtime.rows[row["columns"]["id"]] = row
        if supported_game:
            path = self.runtime.library / row["columns"]["fs_path"] / "Game.gg"
            path.parent.mkdir(parents=True)
            path.write_bytes(b"ROM1")

    def test_multiple_platforms_native_inventory_skip_and_noop(self):
        self.add_platform(50, "Sega - Master System - Mark III")
        self.add_platform(51, "Sony - PlayStation 2", False)
        self.add_platform(52, "Unknown System", False)
        plan = self.build(batch_size=1)
        self.assertEqual(plan["entries"], 3)
        self.assertEqual([r["status"] for r in plan["platforms"]], ["processed", "processed", "skipped", "skipped"])
        self.assertIn("disc", plan["platforms"][2]["reason"])
        imp.run_batch(self.runtime, self.api, plan, self.plan_path)
        imp.run_batch(self.runtime, self.api, plan, self.plan_path, self.root / "receipt.json")
        self.assertEqual(self.runtime.updates, 3)
        fresh = imp.build_batch(self.runtime, self.api, self.root / "fresh.json")
        self.assertEqual((fresh["changed"], fresh["resource_bytes"]), (0, 0))

    def test_unmatched_and_unsupported_inputs_are_reported(self):
        row = self.runtime.rows[1]["columns"]
        old = self.runtime.library / row["fs_path"] / row["fs_name"]
        row["fs_name"] = "Unknown.gg"
        old.rename(old.with_name(row["fs_name"]))
        row = self.runtime.rows[2]["columns"]
        old = self.runtime.library / row["fs_path"] / row["fs_name"]
        row["fs_name"] = "Unsupported.7z"
        old.rename(old.with_name(row["fs_name"]))
        plan = self.build()
        self.assertEqual(plan["status"], "ready")
        self.assertEqual(plan["entries"], 0)
        self.assertEqual(len(plan["platforms"][0]["skipped"]), 2)
        imp.run_batch(self.runtime, self.api, plan, self.plan_path)
        self.assertEqual(self.runtime.publications, 0)

    def test_payload_tamper_inventory_drift_and_part_digest_reject_before_writes(self):
        plan = self.build(batch_size=1)
        part_path = self.root / plan["parts"][0]["path"]
        child = json.loads(part_path.read_text(encoding="utf-8"))
        child["lookup"]["inputs"][0]["payload"]["sha1"] = "0" * 40
        imp.save_receipt(part_path, child)
        plan["parts"][0]["sha256"] = imp.hash_file(part_path)["sha256"]
        with self.assertRaisesRegex(ValueError, "observations changed"):
            imp.run_batch(self.runtime, self.api, plan, self.plan_path)
        self.runtime.rows[2]["columns"]["fs_size_bytes"] += 1
        with self.assertRaisesRegex(ValueError, "inventory changed"):
            imp.run_batch(self.runtime, self.api, plan, self.plan_path)
        self.assertEqual((self.runtime.updates, self.api.downloads), (0, 0))

    def test_partial_failure_has_per_part_receipts(self):
        plan = self.build(batch_size=1)
        self.runtime.fail_id = 2
        receipt = self.root / "receipt.json"
        with self.assertRaisesRegex(ValueError, "injected"):
            imp.run_batch(self.runtime, self.api, plan, self.plan_path, receipt)
        result = json.loads(receipt.read_text(encoding="utf-8"))
        self.assertEqual(len(result["results"]), 1)
        self.assertIsNotNone(result["error"])
        self.assertTrue((self.root / result["failed_part_receipt"]).is_file())
        self.assertEqual(self.runtime.updates, 1)

    def test_more_than_1000_inventory_with_no_total_ceiling(self):
        with tempfile.TemporaryDirectory() as folder:
            runtime = FakeRuntime(Path(folder), 1005)
            api = Catalog(self.api.value, {})
            for card in api.value["cards"].values():
                card["assets"] = []
            path = Path(folder) / "large.json"
            plan = imp.build_batch(runtime, api, path, batch_size=300)
            self.assertEqual((plan["entries"], len(plan["parts"])), (1005, 4))
            imp.run_batch(runtime, api, plan, path)

    def test_named_sample_cli_and_legacy_api_error(self):
        with patch.object(imp, "NativeRuntime", return_value=self.runtime), patch.object(imp, "API", return_value=self.api):
            imp.main(["plan", "--all", "--api", self.api.origin, "--limit", "1", "--out", str(self.plan_path)])
        value = json.loads(self.plan_path.read_text(encoding="utf-8"))
        self.assertEqual((value["entries"], value["platforms"][0]["status"]), (1, "sampled"))
        with patch.object(self.api, "capabilities", return_value=None):
            with self.assertRaisesRegex(ValueError, "serve"):
                imp.build_batch(self.runtime, self.api, self.root / "old.json")
        with self.assertRaisesRegex(ValueError, "already exist"):
            self.build()


    def test_repeated_platform_cli_limits_native_membership_and_preserves_order(self):
        self.add_platform(50, "Sega - Master System - Mark III")
        self.add_platform(51, "Sony - PlayStation 2", False)
        with patch.object(imp, "NativeRuntime", return_value=self.runtime), patch.object(imp, "API", return_value=self.api):
            imp.main(["plan", "--platform", "Sega - Master System - Mark III", "--platform", "Sega Game Gear", "--api", self.api.origin, "--out", str(self.plan_path)])
        value = json.loads(self.plan_path.read_text(encoding="utf-8"))
        self.assertEqual([r["id"] for r in value["platforms"]], [50, 49])
        self.assertEqual(value["entries"], 3)
        imp.run_batch(self.runtime, self.api, value, self.plan_path)
        with self.assertRaisesRegex(ValueError, "more than once"):
            imp.build_batch(self.runtime, self.api, self.root / "duplicates.json", ["Sega Game Gear", "gamegear"])
        self.assertFalse((self.root / "duplicates.json").exists())
        with self.assertRaisesRegex(ValueError, "ambiguous or not found"):
            imp.build_batch(self.runtime, self.api, self.root / "unknown.json", ["Sega Game Gear", "Unknown System"])
        self.assertFalse((self.root / "unknown.json").exists())


    def test_identity_conflict_is_reported_and_excluded_from_native_apply(self):
        def conflict_projection(selection, rows):
            conflicts = [{"rom": item["record"]["rom"], "status": "metadata-conflict", "reason": "conflicting database_id values in explicit catalog card"} for item, _ in rows if item["record"]["rom"] == "Game 2.gg"]
            if conflicts:
                raise imp.ProjectionConflicts(conflicts)
            return fixtures.projection(selection, rows)
        before = copy.deepcopy(self.runtime.rows[2])
        with patch.object(imp, "project", conflict_projection):
            plan = self.build()
            self.assertEqual((plan["entries"], plan["platforms"][0]["considered"]), (1, 2))
            self.assertEqual(plan["platforms"][0]["skipped"][0]["status"], "metadata-conflict")
            child = json.loads((self.root / plan["parts"][0]["path"]).read_text(encoding="utf-8"))
            self.assertEqual([i["rom"] for i in child["lookup"]["inputs"]], ["Game 1.gg"])
            imp.run_batch(self.runtime, self.api, plan, self.plan_path, self.root / "receipt.json")
        self.assertEqual(self.runtime.rows[2], before)
        self.assertEqual(self.runtime.updates, 1)

    def test_payload_or_transport_failure_is_never_a_metadata_skip(self):
        with patch.object(imp, "project", side_effect=ValueError("authoritative fingerprint disagrees with payload")):
            with self.assertRaisesRegex(ValueError, "fingerprint disagrees"):
                self.build()
        failed = json.loads(self.plan_path.read_text(encoding="utf-8"))
        self.assertEqual(failed["status"], "failed")
        self.assertEqual(failed["parts"], [])
        self.assertEqual((self.runtime.updates, self.api.downloads), (0, 0))


    def test_check_skips_native_media_hashes_but_keeps_rom_proof_and_actions(self):
        ordinary = self.build()
        original = imp.hash_file
        observed = []
        def without_media(path):
            target = Path(path).resolve()
            self.assertFalse(target.is_relative_to(self.runtime.resources))
            observed.append(target)
            return original(path)
        with patch.object(imp, "hash_file", side_effect=without_media):
            checked = imp.check_batch(self.runtime, self.api, self.root / "check.json")
        self.assertEqual(checked["kind"], "check")
        self.assertFalse(checked["media_content_audited"])
        for key in ("entries", "changed", "resource_bytes", "unique_download_bytes", "platforms"):
            self.assertEqual(checked[key], ordinary[key])
        for left, right in zip(ordinary["parts"], checked["parts"]):
            plan = json.loads((self.root / left["path"]).read_text(encoding="utf-8"))
            report = json.loads((self.root / right["path"]).read_text(encoding="utf-8"))
            self.assertEqual([e["patch"] for e in report["entries"]], [e["patch"] for e in plan["entries"]])
            self.assertTrue(all(e["before"]["resources"] == {} for e in report["entries"]))
        self.assertTrue(any(p.is_relative_to(self.runtime.library) for p in observed))
        self.assertFalse(self.runtime.metadata_only)
        self.assertTrue(self.runtime.snapshot(1)["resources"])
        self.assertEqual((self.runtime.updates, self.runtime.publications, self.api.downloads), (0, 0, 0))

    def test_check_after_apply_is_noop_and_reports_cannot_be_applied(self):
        plan = self.build()
        imp.run_batch(self.runtime, self.api, plan, self.plan_path, self.root / "receipt.json")
        checked = imp.check_batch(self.runtime, self.api, self.root / "check.json")
        self.assertEqual((checked["entries"], checked["changed"], checked["resource_bytes"], checked["unique_download_bytes"]), (2, 0, 0, 0))
        counts = (self.runtime.updates, self.runtime.publications, self.api.downloads)
        with self.assertRaisesRegex(ValueError, "invalid or incomplete batch plan"):
            imp.run_batch(self.runtime, self.api, checked, self.root / "check.json", self.root / "rejected.json")
        child = json.loads((self.root / checked["parts"][0]["path"]).read_text(encoding="utf-8"))
        self.assertEqual(child["kind"], "check")
        with self.assertRaisesRegex(ValueError, "check report cannot be applied"):
            imp.apply_plan(self.runtime, self.api, child, self.root / "rejected-child.json", self.root)
        self.assertFalse((self.root / "rejected.json").exists())
        self.assertFalse((self.root / "rejected-child.json").exists())
        self.assertEqual((self.runtime.updates, self.runtime.publications, self.api.downloads), counts)

    def test_check_failure_restores_full_resource_snapshots(self):
        with patch.object(self.api, "lookup", side_effect=ValueError("source changed")):
            with self.assertRaisesRegex(ValueError, "source changed"):
                imp.check_batch(self.runtime, self.api, self.root / "failed-check.json")
        self.assertFalse(self.runtime.metadata_only)
        self.assertTrue(self.runtime.snapshot(1)["resources"])
        self.assertEqual((self.runtime.updates, self.api.downloads), (0, 0))

    def test_check_cli_named_all_and_apply_rejection(self):
        with patch.object(imp, "NativeRuntime", return_value=self.runtime), patch.object(imp, "API", return_value=self.api):
            for target in (["--all"], ["--platform", "Sega Game Gear"]):
                out = self.root / ("check-all.json" if target == ["--all"] else "check-named.json")
                imp.main(["check", *target, "--api", self.api.origin, "--limit", "1", "--out", str(out)])
                report = json.loads(out.read_text(encoding="utf-8"))
                self.assertEqual((report["kind"], report["entries"]), ("check", 1))
                with self.assertRaises(SystemExit) as rejected:
                    imp.main(["apply", "--plan", str(out), "--apply", "--out", str(self.root / "never.json")])
                self.assertEqual(rejected.exception.code, 2)
        self.assertEqual((self.runtime.updates, self.api.downloads), (0, 0))
