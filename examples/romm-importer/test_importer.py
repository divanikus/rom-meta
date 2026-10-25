import copy
import io
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("rommeta_importer", Path(__file__).with_name("importer.py"))
imp = importlib.util.module_from_spec(spec)
spec.loader.exec_module(imp)


def sha(data):
    return hashlib.sha256(data).hexdigest()


def projection(selection, rows):
    return {item["record"]["rom"]: {"form": {}, "providers": {"slots": {"igdb": {"id": 123, "source_slug": "source-proven-game"}, "screenscraper": {"id": 321}, "launchbox": {"id": 456}}, "verification": {"flags": {"nointro_match": True}}}} for item, _ in rows}


class FakeAPI:
    origin = "http://catalog.test"
    def __init__(self, selection, content):
        self.value = selection
        self.content = content
        self.downloads = 0
        self.corrupt = False
    def selection(self):
        return copy.deepcopy(self.value)
    def download(self, asset, target):
        self.downloads += 1
        data = self.content[asset["id"]]
        if self.corrupt:
            raise ValueError("asset differs from reviewed digest")
        Path(target).write_bytes(data)


class FakeRuntime(imp.NativeRuntime):
    def __init__(self, root, count):
        self.library = root / "library"
        self.resources = root / "resources"
        self.library.mkdir(parents=True)
        self.resources.mkdir()
        self.rows = {}
        self.updates = 0
        self.publications = 0
        self.fail_id = None
        for rom_id in range(1, count + 1):
            name = f"Game {rom_id}.gg"
            path = self.library / "roms/Test/retail" / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(f"ROM{rom_id}".encode())
            columns = {"id": rom_id, "platform_id": 49, "fs_path": "roms/Test/retail", "fs_name": name, "fs_size_bytes": path.stat().st_size, "name": "Owner title", "summary": "Owner prose", "manual_metadata": {"first_release_date": 741571200000}, "updated_at": "before", "locked_fields": ["url_cover"], "path_cover_l": f"roms/49/{rom_id}/cover/big.png", "path_cover_s": f"roms/49/{rom_id}/cover/small.png", "path_screenshots": [], "igdb_id": 123, "igdb_metadata": {"total_rating": "90"}, "ss_id": 321, "ss_metadata": {}, "launchbox_id": 456, "launchbox_metadata": {}, "gamelist_metadata": {}}
            for field in ("path_cover_l", "path_cover_s"):
                cover = self.resources / columns[field]
                cover.parent.mkdir(parents=True, exist_ok=True)
                cover.write_bytes(b"owner cover unchanged")
            self.rows[rom_id] = {"columns": columns, "files": [{"id": rom_id * 10, "rom_id": rom_id}], "users": [], "platform_fs_slug": "Sega - Game Gear"}
    def platforms(self):
        return [{"id": 49, "name": "Sega Game Gear", "custom_name": "My Game Gear", "slug": "gamegear", "fs_slug": "Sega - Game Gear"}]
    def inventory(self, platform_id):
        return [copy.deepcopy(r["columns"]) for r in self.rows.values() if r["columns"]["platform_id"] == platform_id]
    def find(self, platform_id, fs_path, fs_name):
        rows = [r for r in self.rows.values() if r["columns"]["platform_id"] == platform_id and r["columns"]["fs_path"] == fs_path and r["columns"]["fs_name"] == fs_name]
        if len(rows) != 1:
            raise ValueError("native inventory has no unique exact path")
        return self.snapshot(rows[0]["columns"]["id"])
    def snapshot(self, rom_id):
        value = copy.deepcopy(self.rows[rom_id])
        value["resources"] = self.resource_snapshot(value["columns"])
        return value
    def media_path(self, platform, rom, role, asset):
        return f"roms/{platform}/{rom}/{role}/rommeta-{asset['sha256']}{Path(asset['path']).suffix}"
    def prepare_cover(self, file, source, staging):
        private = Path(staging) / ("cover-" + str(file["id"]))
        private.mkdir()
        outputs = []
        for name in file["paths"]:
            target = private / Path(name).name
            target.write_bytes(Path(source).read_bytes() + Path(name).name.encode())
            output = copy.deepcopy(file)
            output.pop("role", None)
            output["path"] = name
            output["asset"].update(imp.hash_file(target))
            outputs.append((output, target))
        return outputs
    def validate_patch(self, patch):
        allowed = imp.FIELDS | {"slug", "path_screenshots", "gamelist_metadata", "path_manual", "path_cover_l", "path_cover_s", "locked_fields"}
        if not set(patch) <= allowed:
            raise ValueError("unsupported field")
    def validate_media(self, asset, staged):
        if imp.hash_file(staged) != {"size": asset["size"], "sha256": asset["sha256"]}:
            raise ValueError("invalid staged asset")
    def publish(self, file, staged):
        super().publish(file, staged)
        self.publications += 1
    def update(self, rom_id, patch, expected_hash):
        if imp.digest(self.snapshot(rom_id)) != expected_hash:
            raise ValueError("native row changed under update lock")
        if rom_id == self.fail_id:
            raise ValueError("injected native response failure")
        self.rows[rom_id]["columns"].update(copy.deepcopy(patch))
        self.rows[rom_id]["columns"]["updated_at"] = "updated"
        self.updates += 1
    def refresh(self, ids):
        return "confirmed"


class ImporterTest(unittest.TestCase):
    def setUp(self):
        self.output = io.StringIO()
        logs = patch("sys.stderr", self.output)
        logs.start()
        self.addCleanup(logs.stop)
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.runtime = FakeRuntime(self.root, 2)
        self.card_id = "platforms/Sega - Game Gear/Game"
        card = {"game": {"data": {"releases": {"Game (USA)": {"regions": ["USA"]}}}}, "assets": []}
        self.content = {}
        for kind, source, data, suffix in [("gameplay-video", "emumovies", b"VIDEO", ".mp4"), ("box-back", "launchbox", b"BACK", ".png"), ("fanart", "emumovies", b"invisible local fanart", ".png"), ("fanart", "screenscraper", b"visible SS fanart", ".png"), ("logo", "screenscraper", b"LOGO", ".png"), ("title-screen", "screenscraper", b"TITLE", ".png"), ("screenshot", "igdb", b"SCREEN1", ".png"), ("screenshot", "launchbox", b"SCREEN2", ".png"), ("screenshot", "screenscraper", b"SCREEN1", ".png"), ("banner", "launchbox", b"BANNER", ".png")]:
            path = f"{self.card_id}/media/{kind}.usa.{source}{suffix}"
            asset_id = sha(path.encode())
            asset = {"id": asset_id, "uri": "/v1/assets/" + asset_id, "path": path, "game": self.card_id, "kind": kind, "source": source, "region": "usa", "variant": "unknown", "size": len(data), "sha256": sha(data)}
            card["assets"].append(asset)
            self.content[asset_id] = data
        inputs = []
        for rom_id in range(1, 3):
            name = f"Game {rom_id}.gg"
            outer = imp.hash_file(self.runtime.library / "roms/Test/retail" / name)
            inputs.append({"record": {"rom": name, "game": self.card_id, "release": "Game (USA)"}, "outer": outer})
        selection = {"schema_version": 1, "platform": "Sega - Game Gear", "digest": "a" * 64, "inputs": inputs, "cards": {self.card_id: card}, "policy": {"media": {"cover": {"sources": ["emumovies", "launchbox", "screenscraper"]}}}}
        self.api = FakeAPI(selection, self.content)
        self.project_patch = patch.object(imp, "project", projection)
        self.project_patch.start()
        self.addCleanup(self.project_patch.stop)
    def make_plan(self):
        return imp.build_plan(self.runtime, self.api, 49, "roms/Test/retail", {1, 2}, projector=projection)
    def test_invalid_images_skip_only_media_keep_metadata_and_repeat_noop(self):
        bad = next(a for a in self.api.value["cards"][self.card_id]["assets"] if a["kind"] == "box-back")
        original = self.runtime.validate_media
        def validate(asset, target):
            original(asset, target)
            if asset["sha256"] == bad["sha256"]:
                raise imp.InvalidImageError("UnidentifiedImageError")
        self.runtime.invalid_images_path = self.root / "invalid-images.json"
        with patch.object(self.runtime, "validate_media", validate):
            receipt = imp.apply_plan(self.runtime, self.api, self.make_plan(), self.root / "invalid.json", self.root)
        self.assertEqual(len(receipt["skipped_media"]), 2)
        self.assertTrue(all(r["status"] == "applied" for r in receipt["results"]))
        for row in self.runtime.rows.values():
            self.assertNotIn("box2d_back_path", row["columns"]["launchbox_metadata"])
            self.assertEqual(row["columns"]["slug"], "source-proven-game")
        self.assertEqual(imp.load_invalid_images(self.runtime.invalid_images_path), {bad["sha256"]: "UnidentifiedImageError"})
        fresh = self.make_plan()
        self.assertEqual(fresh["resource_bytes"], 0)
        self.assertFalse(any(e["patch"] for e in fresh["entries"]))
        self.assertTrue(all(any(o["reason"].startswith("invalid image:") for o in e["omissions"]) for e in fresh["entries"]))
        imp.preflight(self.runtime, self.api, fresh)

    def test_later_part_keeps_frozen_plan_checks_after_new_rejection(self):
        plan = self.make_plan()
        bad = next(a for a in self.api.value["cards"][self.card_id]["assets"] if a["kind"] == "box-back")
        self.runtime.invalid_images = {bad["sha256"]: "UnidentifiedImageError"}
        imp.preflight(self.runtime, self.api, plan)
        receipt = imp.apply_plan(self.runtime, self.api, plan, self.root / "later-part.json", self.root)
        self.assertEqual(len(receipt["skipped_media"]), 2)
        self.assertTrue(all(r["status"] == "applied" for r in receipt["results"]))

    def test_invalid_image_removes_blank_cover_paths_and_only_its_added_lock(self):
        row = self.runtime.rows[1]["columns"]
        for field in ("path_cover_l", "path_cover_s"):
            (self.runtime.resources / row[field]).unlink()
        row.update(path_cover_l=None, path_cover_s=None, locked_fields=["owner_lock"])
        asset = copy.deepcopy(self.api.value["cards"][self.card_id]["assets"][1])
        asset.update(kind="box-front", id="new-cover", path=self.card_id + "/media/cover.png")
        self.api.value["cards"][self.card_id]["assets"].append(asset)
        self.api.value["inputs"][0]["record"]["media"] = {"cover": {"source_path": asset["path"]}}
        self.content[asset["id"]] = self.content[self.api.value["cards"][self.card_id]["assets"][1]["id"]]
        plan = self.make_plan()
        validate = self.runtime.validate_media
        def reject_cover(a, target):
            validate(a, target)
            if a["id"] == "new-cover":
                raise imp.InvalidImageError("UnidentifiedImageError")
        with patch.object(self.runtime, "validate_media", reject_cover):
            receipt = imp.apply_plan(self.runtime, self.api, plan, self.root / "bad-cover.json", self.root)
        after = self.runtime.rows[1]["columns"]
        self.assertIsNone(after["path_cover_l"])
        self.assertEqual(after["locked_fields"], ["owner_lock"])
        self.assertTrue(any(o["kind"] == "box-front" for o in receipt["skipped_media"]))

    def test_invalid_screenshot_keeps_owner_and_other_gallery_images(self):
        owner = self.runtime.resources / "owner.png"
        owner.write_bytes(b"owner image")
        for row in self.runtime.rows.values():
            row["columns"]["path_screenshots"] = ["owner.png"]
        bad = next(a for a in self.api.value["cards"][self.card_id]["assets"] if a["kind"] == "screenshot")
        validate = self.runtime.validate_media
        def reject(a, target):
            validate(a, target)
            if a["sha256"] == bad["sha256"]:
                raise imp.InvalidImageError("UnidentifiedImageError")
        with patch.object(self.runtime, "validate_media", reject):
            result = imp.apply_plan(self.runtime, self.api, self.make_plan(), self.root / "gallery.json", self.root)
        for row in self.runtime.rows.values():
            paths = row["columns"]["path_screenshots"]
            self.assertEqual(len(paths), 2)
            self.assertEqual(paths[0], "owner.png")
            self.assertTrue(all(bad["sha256"] not in path for path in paths))
        self.assertEqual(owner.read_bytes(), b"owner image")
        self.assertTrue(result["skipped_media"])
        self.assertEqual(self.make_plan()["resource_bytes"], 0)

    def test_media_workers_run_concurrently_and_join_before_failure_returns(self):
        import threading
        barrier = threading.Barrier(4)
        threads = set()
        guard = threading.Lock()
        def task(value):
            with guard:
                threads.add(threading.get_ident())
            barrier.wait(timeout=5)
            return value * 2
        self.assertEqual(list(imp.media_map(task, range(4))), [0, 2, 4, 6])
        self.assertEqual(len(threads), 4)
        finished = threading.Event()
        running = threading.Event()
        def fail(value):
            if value == 0:
                running.wait(timeout=5)
                raise ValueError("hash failure")
            running.set()
            finished.set()
            return value
        with self.assertRaisesRegex(ValueError, "hash failure"):
            list(imp.media_map(fail, range(2)))
        self.assertTrue(finished.is_set())

    def test_native_decoder_classifies_corrupt_png_but_not_missing_files(self):
        from PIL import Image
        path = self.root / "bad.png"
        Image.new("RGB", (2, 3), "red").save(path)
        good = path.read_bytes()
        damaged = bytearray(good)
        damaged[29] ^= 1  # IHDR CRC; actual bytes are fetched and hash-verified elsewhere.
        path.write_bytes(damaged)
        with self.assertRaises(imp.InvalidImageError):
            imp.NativeRuntime.validate_media(self.runtime, {"path": "bad.png"}, path)
        with self.assertRaises(FileNotFoundError):
            imp.NativeRuntime.validate_media(self.runtime, {"path": "missing.png"}, self.root / "missing.png")
        path.write_bytes(good)
        imp.NativeRuntime.validate_media(self.runtime, {"path": "good.png"}, path)

    def test_native_source_slots_visible_local_video_and_screenshot_dedup(self):
        plan = self.make_plan()
        entry = plan["entries"][0]
        self.assertEqual(entry["patch"]["slug"], "source-proven-game")
        self.assertIn("video_path", entry["patch"]["gamelist_metadata"])
        self.assertIn("box2d_back_path", entry["patch"]["launchbox_metadata"])
        self.assertIn("fanart_path", entry["patch"]["ss_metadata"])
        fanart = [f for f in entry["files"] if f["field"] == "fanart_path"]
        self.assertEqual([f["actual_source"] for f in fanart], ["screenscraper"])
        self.assertEqual(len(entry["patch"]["path_screenshots"]), 2)
        self.assertNotIn("summary", entry["patch"])
        self.assertNotIn("manual_metadata", entry["patch"])
        self.assertNotIn("path_cover_l", entry["patch"])
        self.assertGreater(plan["resource_bytes"], plan["unique_download_bytes"])
    def test_coverage_skips_are_reported_and_never_bound_to_retail(self):
        self.api.value["coverage"] = {"considered": 4, "included": 2, "skipped": [
            {"rom": "translations/en/Fan Title.zip", "status": "unmatched", "reason": "no exact fingerprint"},
            {"rom": "aftermarket/Unknown.7z", "status": "unsupported", "reason": "unsupported archive"}]}
        plan = self.make_plan()
        self.assertEqual(plan["coverage"], self.api.value["coverage"])
        self.assertEqual(len(plan["entries"]), 2)
        plan["coverage"]["skipped"][0]["reason"] = "edited"
        with self.assertRaisesRegex(ValueError, "snapshot changed"):
            imp.apply_plan(self.runtime, self.api, plan, self.root / "coverage.json", self.root)
        self.assertEqual((self.runtime.updates, self.runtime.publications, self.api.downloads), (0, 0, 0))

    def test_conflicting_owner_provider_keeps_its_id_blob_and_artwork(self):
        row = self.runtime.snapshot(1)
        row["columns"].update({"ss_id": 999, "ss_metadata": {"owner_extra": "preserve"}, "launchbox_id": 998})
        proposed = projection(self.api.value, [(self.api.value["inputs"][0], row)])["Game 1.gg"]
        entry = imp.plan_entry(self.runtime, self.api.value, self.api.value["inputs"][0], row, proposed)
        for key in ("ss_id", "ss_metadata", "launchbox_id", "launchbox_metadata"):
            self.assertNotIn(key, entry["patch"])
        self.assertTrue(any("provider identity" in skipped["reason"] for skipped in entry["omissions"]))
        self.assertTrue(all(f["column"] not in ("ss_metadata", "launchbox_metadata") for f in entry["files"]))
        self.assertIn("path_screenshots", entry["patch"])
        self.assertIn("video_path", entry["patch"]["gamelist_metadata"])

    def test_actual_empty_owner_locks_win_over_projection_form(self):
        row = self.runtime.snapshot(1)
        row["columns"].update({"summary": "", "igdb_id": None, "igdb_metadata": {}, "manual_metadata": {}, "locked_fields": ["summary", "igdb_id", "manual_metadata", "ss_metadata", "gamelist_metadata", "path_screenshots"]})
        item = self.api.value["inputs"][0]
        proposed = {"providers": {"slots": {"igdb": {"id": 123, "source_slug": "source-proven"}}}, "form": {"summary": "do not fill", "igdb_id": "123", "raw_igdb_metadata": "{\"genres\":[\"Action\"]}", "raw_manual_metadata": "{\"genres\":[\"Action\"]}"}}
        entry = imp.plan_entry(self.runtime, self.api.value, item, row, proposed)
        for key in ("summary", "igdb_id", "igdb_metadata", "manual_metadata", "ss_metadata", "gamelist_metadata", "path_screenshots", "slug"):
            self.assertNotIn(key, entry["patch"])
    def test_id_only_slug_and_occupied_video_are_preserved(self):
        row = self.runtime.snapshot(1)
        row["columns"]["ss_metadata"] = {"video_path": "owner/video.mp4"}
        proposed = {"providers": {"slots": {"igdb": {"id": 123}}}, "form": {}}
        entry = imp.plan_entry(self.runtime, self.api.value, self.api.value["inputs"][0], row, proposed)
        self.assertNotIn("slug", entry["patch"])
        # Logical video occupancy must preserve any higher-priority native carrier.
        self.assertFalse(any(f["field"] == "video_path" for f in entry["files"]))
        self.assertTrue(any(o.get("role") == "igdb slug" for o in entry["omissions"]))
    def test_apply_repeat_preserves_original_resources_and_uses_regular_copies(self):
        plan = self.make_plan()
        original = [self.runtime.snapshot(i)["resources"] for i in (1, 2)]
        receipt = imp.apply_plan(self.runtime, self.api, plan, self.root / "receipt.json", self.root)
        self.assertEqual([r["status"] for r in receipt["results"]], ["applied", "applied"])
        self.assertEqual(self.runtime.updates, 2)
        self.assertIn("Download 1/", self.output.getvalue())
        self.assertIn("RomM 1/2 | id 1", self.output.getvalue())
        self.assertIn("Confirmed id 2", self.output.getvalue())
        for rom_id, before in zip((1, 2), original):
            after = self.runtime.snapshot(rom_id)
            for name, value in before.items():
                self.assertEqual(after["resources"][name], value)
            self.assertEqual(after["columns"]["manual_metadata"], {"first_release_date": 741571200000})
        first = plan["entries"][0]["files"][0]["path"]
        second = plan["entries"][1]["files"][0]["path"]
        self.assertNotEqual((self.runtime.resources / first).stat().st_ino, (self.runtime.resources / second).stat().st_ino)
        repeat = self.make_plan()
        self.assertEqual(repeat["resource_bytes"], 0)
        self.assertTrue(all(not e["patch"] and not e["files"] for e in repeat["entries"]))
    def test_changed_last_rom_or_owner_aborts_before_first_publication(self):
        plan = self.make_plan()
        (self.runtime.library / "roms/Test/retail/Game 2.gg").write_bytes(b"different")
        with self.assertRaises(ValueError):
            imp.apply_plan(self.runtime, self.api, plan, self.root / "receipt.json", self.root)
        self.assertEqual((self.runtime.updates, self.runtime.publications, self.api.downloads), (0, 0, 0))
    def test_plan_patch_tampering_is_not_a_native_write_interface(self):
        plan = self.make_plan()
        plan["entries"][0]["patch"]["summary"] = "forged overwrite"
        with self.assertRaises(ValueError):
            imp.apply_plan(self.runtime, self.api, plan, self.root / "receipt.json", self.root)
        self.assertEqual((self.runtime.updates, self.runtime.publications), (0, 0))
    def test_changed_asset_stops_before_resources_or_db_write(self):
        plan = self.make_plan()
        self.api.corrupt = True
        with self.assertRaises(ValueError):
            imp.apply_plan(self.runtime, self.api, plan, self.root / "receipt.json", self.root)
        self.assertEqual((self.runtime.updates, self.runtime.publications), (0, 0))
    def test_partial_failure_receipt_and_fresh_resume(self):
        plan = self.make_plan()
        self.runtime.fail_id = 2
        with patch.object(imp, "save_receipt", wraps=imp.save_receipt) as checkpoints:
            with self.assertRaises(ValueError):
                imp.apply_plan(self.runtime, self.api, plan, self.root / "receipt.json", self.root)
        self.assertLessEqual(checkpoints.call_count, 2 * len(plan["entries"]) + 2)
        receipt = json.loads((self.root / "receipt.json").read_text(encoding="utf-8"))
        self.assertEqual([r["status"] for r in receipt["results"]], ["applied", "not-confirmed"])
        self.assertEqual(len(receipt["results"][1]["published_files"]), len(plan["entries"][1]["files"]))
        self.runtime.fail_id = None
        fresh = self.make_plan()
        self.assertEqual([bool(e["patch"]) for e in fresh["entries"]], [False, True])
        imp.apply_plan(self.runtime, self.api, fresh, self.root / "resume.json", self.root)
        self.assertTrue(all(not e["patch"] for e in self.make_plan()["entries"]))
    def test_destination_conflict_fails_before_download_or_other_write(self):
        plan = self.make_plan()
        target = self.runtime.resource_path(plan["entries"][1]["files"][0]["path"])
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(b"owner data")
        with self.assertRaises(ValueError):
            imp.apply_plan(self.runtime, self.api, plan, self.root / "receipt.json", self.root)
        self.assertEqual((self.runtime.updates, self.runtime.publications, self.api.downloads), (0, 0, 0))
        self.assertEqual(target.read_bytes(), b"owner data")
    def test_native_cover_decodes_mislabeled_alpha_jpeg_without_source_mutation(self):
        from PIL import Image
        source = self.root / "actual-alpha.jpg"
        Image.new("RGBA", (3, 2), (10, 20, 30, 87)).save(source, format="PNG")
        before = imp.hash_file(source)
        normalized = imp.normalized_cover(source)
        with Image.open(normalized) as image:
            self.assertEqual((image.format, image.mode, image.size, image.getpixel((0, 0))), ("PNG", "RGBA", (3, 2), (10, 20, 30, 87)))
        self.assertEqual(imp.hash_file(source), before)
        self.assertEqual(self.runtime.cover_paths(49, 1, {"path": "cover.jpg"}), ("roms/49/1/cover/big.png", "roms/49/1/cover/small.png"))
        cmyk = self.root / "cmyk.jpg"
        Image.new("CMYK", (2, 3), (0, 30, 60, 0)).save(cmyk, format="JPEG")
        with Image.open(imp.normalized_cover(cmyk)) as image:
            self.assertEqual((image.format, image.mode, image.size), ("PNG", "RGB", (2, 3)))

    def test_blank_cover_and_manual_share_the_ordinary_job_and_repeat(self):
        card = self.api.value["cards"][self.card_id]
        for kind, suffix, data, role in (("box-front", ".png", b"source cover", "cover"), ("manual", ".pdf", b"%PDF synthetic manual", "manual")):
            name = f"{self.card_id}/media/{kind}.usa.local{suffix}"
            asset_id = sha(name.encode())
            asset = {"id": asset_id, "uri": "/v1/assets/" + asset_id, "path": name, "game": self.card_id, "kind": kind, "source": "local", "size": len(data), "sha256": sha(data)}
            card["assets"].append(asset)
            self.content[asset_id] = data
            for item in self.api.value["inputs"]:
                item["record"].setdefault("media", {})[role] = {"source_path": name}
        # A genuinely fresh destination; existing owner derivatives are never replaced.
        for row in self.runtime.rows.values():
            for field in ("path_cover_l", "path_cover_s"):
                self.runtime.resource_path(row["columns"][field]).unlink()
                row["columns"][field] = ""
            row["columns"]["locked_fields"] = []
        plan = self.make_plan()
        for entry in plan["entries"]:
            self.assertEqual(sum(f.get("role") == "cover" for f in entry["files"]), 1)
            self.assertEqual(sum(f.get("role") == "manual" for f in entry["files"]), 1)
        receipt = imp.apply_plan(self.runtime, self.api, plan, self.root / "basic.json", self.root)
        self.assertTrue(all(row["status"] == "applied" for row in receipt["results"]))
        self.assertTrue(all(len(row["published_files"]) > len(entry["files"]) for row, entry in zip(receipt["results"], plan["entries"])))
        repeat = self.make_plan()
        self.assertTrue(all(not entry["patch"] and not entry["files"] for entry in repeat["entries"]))
    def test_planned_identity_allows_source_slug_on_a_fresh_rom(self):
        row = self.runtime.snapshot(1)
        row["columns"]["igdb_id"] = None
        proposed = {"providers": {"slots": {"igdb": {"id": 123, "source_slug": "source-slug"}}}, "form": {"igdb_id": "123"}}
        entry = imp.plan_entry(self.runtime, self.api.value, self.api.value["inputs"][0], row, proposed)
        self.assertEqual(entry["patch"]["slug"], "source-slug")
    def test_duplicate_or_retargeted_manifest_aborts_before_any_write(self):
        plan = self.make_plan()
        plan["entries"][1] = copy.deepcopy(plan["entries"][0])
        with self.assertRaises(ValueError):
            imp.apply_plan(self.runtime, self.api, plan, self.root / "duplicate.json", self.root)
        plan = self.make_plan()
        self.runtime.rows[1]["columns"]["fs_path"] = "roms/Other/retail"
        entry = plan["entries"][0]
        entry["before"] = self.runtime.snapshot(1)
        entry["before_hash"] = imp.digest(entry["before"])
        with self.assertRaises(ValueError):
            imp.apply_plan(self.runtime, self.api, plan, self.root / "retarget.json", self.root)
        self.assertEqual((self.runtime.updates, self.runtime.publications, self.api.downloads), (0, 0, 0))
    def test_reviewed_identity_repairs_are_opt_in_exact_and_repeatable(self):
        def reviewed_projection(selection, rows):
            result = projection(selection, rows)
            for index, (item, _) in enumerate(rows):
                value = result[item["record"]["rom"]]
                review = {"source": "hasheous", "from_id": 123, "action": "replace" if index == 0 else "suppress", "reason": "Reviewed wrong foreign port", "evidence": ["https://www.igdb.com/games/actual-port"]}
                if index == 0:
                    review["to_id"] = 124
                    value["providers"]["slots"]["igdb"] = {"id": 124, "source": "igdb", "source_slug": "actual-port"}
                else:
                    del value["providers"]["slots"]["igdb"]
                value["providers"]["reference_reviews"] = [review]
            return result
        self.api.value["cards"][self.card_id]["assets"] = []
        for row in self.runtime.rows.values():
            row["columns"]["igdb_metadata"] = {}
        regular = imp.build_plan(self.runtime, self.api, 49, "roms/Test/retail", projector=reviewed_projection)
        self.assertTrue(all(not e["patch"] for e in regular["entries"]))
        plan = imp.build_plan(self.runtime, self.api, 49, "roms/Test/retail", projector=reviewed_projection, repair_igdb_references=True)
        self.assertEqual(plan["entries"][0]["patch"], {"igdb_id": 124, "slug": "actual-port"})
        self.assertEqual(plan["entries"][1]["patch"], {"igdb_id": None})
        self.assertTrue(all(e["igdb_reference_repair"] and not e["files"] for e in plan["entries"]))
        with patch.object(imp, "project", reviewed_projection):
            imp.apply_plan(self.runtime, self.api, plan, self.root / "reviewed-repair.json", self.root)
            repeat = imp.build_plan(self.runtime, self.api, 49, "roms/Test/retail", projector=reviewed_projection, repair_igdb_references=True)
        self.assertTrue(all(not e["patch"] and not e["igdb_reference_repair"] and not e["files"] for e in repeat["entries"]))
        self.assertEqual((self.api.downloads, self.runtime.publications), (0, 0))
    def test_reviewed_igdb_repair_rejects_owner_data_and_bad_targets(self):
        before = self.runtime.snapshot(1)["columns"]
        before["igdb_metadata"] = {}
        providers = {"slots": {"igdb": {"id": 124, "source": "igdb", "source_slug": "actual-port"}}, "reference_reviews": [{"source": "hasheous", "from_id": 123, "action": "replace", "to_id": 124, "reason": "Verified port", "evidence": ["https://www.igdb.com/games/actual-port"]}]}
        self.assertEqual(imp.igdb_reference_repair(before, providers, True)["after_id"], 124)
        for key, value in (("igdb_id", 999), ("slug", "owner-slug"), ("igdb_metadata", {"total_rating": "50"}), ("locked_fields", ["igdb_id"]), ("locked_fields", ["slug"]), ("locked_fields", ["igdb_metadata"])):
            candidate = copy.deepcopy(before)
            candidate[key] = value
            with self.assertRaises(ValueError):
                imp.igdb_reference_repair(candidate, providers, True)
        for key, value in (("source", "hasheous-reference"), ("id", 999), ("source_slug", None)):
            proposed = copy.deepcopy(providers)
            proposed["slots"]["igdb"][key] = value
            with self.assertRaises(ValueError):
                imp.igdb_reference_repair(before, proposed, True)

    def test_cover_baseline_export_refresh_preservation_and_same_review_noop(self):
        old = {"id": "1" * 64, "uri": "/v1/assets/" + "1" * 64, "path": self.card_id + "/media/box-front.usa.emumovies.png", "kind": "box-front", "source": "emumovies", "style": "unknown", "size": 3, "sha256": sha(b"OLD")}
        new = {"id": "2" * 64, "uri": "/v1/assets/" + "2" * 64, "path": self.card_id + "/media/box-front.usa.screenscraper.png", "kind": "box-front", "source": "screenscraper", "style": "flat-front", "style_evidence": "provider-category:screenscraper:box-2D", "size": 3, "sha256": sha(b"NEW")}
        self.content.update({old["id"]: b"OLD", new["id"]: b"NEW"})
        self.api.value["cards"][self.card_id]["assets"] = [old, new]
        for item in self.api.value["inputs"]:
            item["record"]["media"] = {"cover": {"source_path": old["path"]}}
        for row in self.runtime.rows.values():
            row["columns"]["slug"] = "source-proven-game"
        baseline = self.make_plan()
        before_plan = self.root / "before-plan.json"
        imp.write_new(before_plan, baseline)
        review = imp.export_cover_review(baseline, self.api.selection(), before_plan)
        self.assertEqual(len(review["entries"]), 2)
        self.assertTrue(all(e["source_sha256"] == old["sha256"] for e in review["entries"]))
        self.assertTrue(all("NOT" not in e["reason"] and "review ownership" in e["reason"] for e in review["entries"]))
        same = imp.build_plan(self.runtime, self.api, 49, "roms/Test/retail", projector=projection, cover_review=review)
        self.assertTrue(all(not e["patch"] and not e["files"] for e in same["entries"]))
        old_resources = {path: imp.hash_file(self.runtime.resource_path(path)) for e in review["entries"] for path in e["resources"]}
        self.api.value["digest"] = "b" * 64
        for item in self.api.value["inputs"]:
            item["record"]["media"]["cover"]["source_path"] = new["path"]
        regular = self.make_plan()
        self.assertTrue(all(not e["patch"] for e in regular["entries"]))
        plan = imp.build_plan(self.runtime, self.api, 49, "roms/Test/retail", projector=projection, cover_review=review)
        self.assertTrue(all(set(e["patch"]) == {"path_cover_l", "path_cover_s"} and e["cover_refresh"]["style"] == "flat-front" for e in plan["entries"]))
        receipt = imp.apply_plan(self.runtime, self.api, plan, self.root / "cover-receipt.json", self.root)
        self.assertEqual(self.api.downloads, 1)
        self.assertTrue(all(r["cover_refresh"] and r["status"] == "applied" for r in receipt["results"]))
        self.assertEqual(old_resources, {path: imp.hash_file(self.runtime.resource_path(path)) for path in old_resources})
        repeat = imp.build_plan(self.runtime, self.api, 49, "roms/Test/retail", projector=projection, cover_review=review)
        self.assertTrue(all(not e["patch"] and not e["files"] and not e["cover_refresh"] for e in repeat["entries"]))
        self.assertTrue(all(self.runtime.rows[r]["columns"]["locked_fields"] == ["url_cover"] for r in self.runtime.rows))
        bad = copy.deepcopy(review)
        bad["entries"][0]["resources"][next(iter(bad["entries"][0]["resources"]))]["sha256"] = "0" * 64
        with self.assertRaises(ValueError):
            imp.build_plan(self.runtime, self.api, 49, "roms/Test/retail", projector=projection, cover_review=bad)
        self.runtime.rows[1]["columns"]["path_cover_l"] = "owner/choice.png"
        with self.assertRaises(ValueError):
            imp.build_plan(self.runtime, self.api, 49, "roms/Test/retail", projector=projection, cover_review=review)
    def test_cover_review_rejects_owner_url_path_locks_and_unselected_ids(self):
        snapshot = self.runtime.snapshot(1)
        before = snapshot["columns"]
        review = {"id": 1, "source_sha256": "1" * 64, "before": {key: before.get(key) for key in ("path_cover_l", "path_cover_s", "url_cover")}, "resources": {p: snapshot["resources"][p] for p in (before["path_cover_l"], before["path_cover_s"])}, "reason": "Explicit accepted baseline", "evidence": ["private accepted receipt"]}
        book = {"schema_version": 1, "platform_id": 49, "entries": [review]}
        self.assertEqual(imp.cover_review_entries(book, 49)[1], review)
        for key, value in (("path_cover_l", "owner/other.png"), ("locked_fields", ["path_cover_l"]), ("url_cover", "https://owner.test/cover.png")):
            changed = copy.deepcopy(snapshot)
            changed["columns"][key] = value
            with self.assertRaises(ValueError):
                imp.cover_refresh_intent(self.runtime, changed, {"sha256": "2" * 64}, review)
        book["entries"][0]["id"] = 999
        with self.assertRaises(ValueError):
            imp.build_plan(self.runtime, self.api, 49, "roms/Test/retail", projector=projection, cover_review=book)
    def test_cover_normalization_preserves_square_and_rectangular_aspects(self):
        from PIL import Image
        for size in ((577, 680), (685, 680), (869, 1000), (496, 680)):
            path = self.root / (str(size[0]) + ".jpg")
            Image.new("RGBA", size, (10, 20, 30, 128)).save(path, format="PNG")
            with Image.open(imp.normalized_cover(path)) as normalized:
                self.assertEqual(normalized.size, size)
                self.assertEqual(normalized.mode, "RGBA")

    def test_platforms_cli_reads_romm_without_api_token_or_file_reads(self):
        out = io.StringIO()
        with patch.object(imp, "NativeRuntime", return_value=self.runtime), patch.object(imp, "API") as api, patch.object(self.runtime, "inventory") as inventory, patch.object(self.runtime, "snapshot") as snapshot, patch.dict(imp.os.environ, {"ROMMETA_API_TOKEN": ""}), patch("sys.stdout", out):
            imp.main(["platforms"])
            api.assert_not_called()
            inventory.assert_not_called()
            snapshot.assert_not_called()
        self.assertIn("My Game Gear | Sega Game Gear; Sega - Game Gear; gamegear", out.getvalue())
        self.assertIn("1 platforms from RomM", out.getvalue())
        self.assertEqual((self.runtime.updates, self.runtime.publications), (0, 0))

    def test_platforms_cli_sorts_names_deduplicates_aliases_and_handles_empty(self):
        data = [{"id": 2, "name": "Zoo", "custom_name": "", "fs_slug": "Zoo", "slug": "zoo"}, {"id": 1, "name": "Alpha", "custom_name": "", "fs_slug": "Alpha", "slug": "alpha"}]
        out = io.StringIO()
        with patch.object(self.runtime, "platforms", return_value=data), patch("sys.stdout", out):
            imp.print_platforms(self.runtime)
        self.assertEqual(out.getvalue().splitlines()[1:3], ["Alpha | ", "Zoo | "])
        out = io.StringIO()
        with patch.object(self.runtime, "platforms", return_value=[]), patch("sys.stdout", out):
            imp.print_platforms(self.runtime)
        self.assertEqual(out.getvalue().strip(), "No platforms found in RomM. Scan your library first.")

    def test_named_platform_detects_id_and_category_root(self):
        for name in ("Sega Game Gear", "  sega   game GEAR ", "Sega - Game Gear", "My Game Gear", "gamegear"):
            plan = imp.build_plan(self.runtime, self.api, platform=name, projector=projection)
            self.assertEqual((plan["platform_id"], plan["prefix"]), (49, "roms/Test/retail"))
            self.assertEqual(plan["entries"], self.make_plan()["entries"])
        # A whole-system source preserves categories; derive their common parent.
        for item in self.api.value["inputs"]:
            item["record"]["rom"] = "retail/" + item["record"]["rom"]
        plan = imp.build_plan(self.runtime, self.api, platform="gamegear", projector=projection)
        self.assertEqual(plan["prefix"], "roms/Test")
        imp.preflight(self.runtime, self.api, plan)
        self.assertEqual(self.runtime.updates, 0)

    def test_named_mapping_preserves_nested_category_paths_and_checks_bytes(self):
        for item in self.api.value["inputs"]:
            name = item["record"]["rom"]
            category = "translations/en" if name == "Game 1.gg" else "retail"
            item["record"]["rom"] = category + "/" + name
            row = self.runtime.rows[1 if name == "Game 1.gg" else 2]["columns"]
            old = self.runtime.library / row["fs_path"] / name
            row["fs_path"] = "roms/Test/" + category
            target = self.runtime.library / row["fs_path"] / name
            target.parent.mkdir(parents=True, exist_ok=True)
            old.rename(target)
        plan = imp.build_plan(self.runtime, self.api, platform="Sega Game Gear", projector=projection)
        self.assertEqual(plan["prefix"], "roms/Test")
        self.assertEqual(plan["entries"][0]["record"]["rom"], "translations/en/Game 1.gg")
        # The same name/size is only a discovery hint, not permission to import.
        target.write_bytes(b"FAIL")
        with self.assertRaisesRegex(ValueError, "native ROM bytes differ"):
            imp.build_plan(self.runtime, self.api, platform="Sega Game Gear", projector=projection)

    def test_named_platform_rejects_missing_ambiguous_or_other_source(self):
        with self.assertRaisesRegex(ValueError, "not found"):
            imp.build_plan(self.runtime, self.api, platform="Gear", projector=projection)
        original = self.runtime.platforms()
        duplicate = copy.deepcopy(original[0])
        duplicate["id"] = 50
        with patch.object(self.runtime, "platforms", return_value=original + [duplicate]):
            with self.assertRaisesRegex(ValueError, "ambiguous"):
                imp.build_plan(self.runtime, self.api, platform="Sega Game Gear", projector=projection)
        self.api.value["platform"] = "Nintendo - Game Boy"
        with self.assertRaisesRegex(ValueError, "same system"):
            imp.build_plan(self.runtime, self.api, platform="Sega Game Gear", projector=projection)

    def test_path_detection_rejects_missing_nested_or_inconsistent_inventory(self):
        self.api.value["inputs"][0]["record"]["rom"] = "translations/en/Game 1.gg"
        with self.assertRaisesRegex(ValueError, "No common scanned"):
            imp.build_plan(self.runtime, self.api, platform="gamegear", projector=projection)
        self.api.value["inputs"][0]["record"]["rom"] = "Game 1.gg"
        self.runtime.rows[2]["columns"]["fs_path"] = "another/library"
        with self.assertRaisesRegex(ValueError, "No common scanned"):
            imp.build_plan(self.runtime, self.api, platform="gamegear", projector=projection)

    def test_path_detection_rejects_two_complete_copies_and_duplicate_rows(self):
        for row in list(self.runtime.rows.values()):
            duplicate = copy.deepcopy(row)
            duplicate["columns"]["id"] += 100
            duplicate["columns"]["fs_path"] = "roms/Second/retail"
            self.runtime.rows[duplicate["columns"]["id"]] = duplicate
        with self.assertRaisesRegex(ValueError, "More than one RomM folder"):
            imp.build_plan(self.runtime, self.api, platform="gamegear", projector=projection)
        for rom_id in (101, 102):
            self.runtime.rows[rom_id]["columns"]["fs_path"] = "roms/Test/retail"
        with self.assertRaisesRegex(ValueError, "no unique exact path"):
            imp.build_plan(self.runtime, self.api, platform="gamegear", projector=projection)

    def test_named_cli_generates_plan_without_native_id_or_path(self):
        out = self.root / "automatic.json"
        with patch.object(imp, "NativeRuntime", return_value=self.runtime), patch.object(imp, "API", return_value=self.api), patch("sys.stdout", io.StringIO()):
            imp.main(["plan", "--api", self.api.origin, "--platform", "Sega Game Gear", "--out", str(out)])
        plan = json.loads(out.read_text(encoding="utf-8"))
        self.assertEqual(plan["prefix"], "roms/Test/retail")
        self.assertEqual(plan["platform_id"], 49)
        for extra in (["--platform", "gamegear", "--romm-rom-root", "manual/path"], ["--platform-id", "49"]):
            with patch.object(imp, "NativeRuntime") as native:
                with self.assertRaises(SystemExit):
                    imp.main(["plan", "--api", self.api.origin, "--out", str(out), *extra])
                native.assert_not_called()

    def test_full_platform_plan_and_preflight_above_one_thousand(self):
        runtime = FakeRuntime(self.root / "large", 1005)
        value = copy.deepcopy(self.api.value)
        value["cards"][self.card_id]["assets"] = []
        value["inputs"] = [{"record": {"rom": row["columns"]["fs_name"], "game": self.card_id, "release": "Game (USA)"}, "outer": imp.hash_file(runtime.library / row["columns"]["fs_path"] / row["columns"]["fs_name"])} for row in runtime.rows.values()]
        api = FakeAPI(value, {})
        plan = imp.build_plan(runtime, api, platform="gamegear", expected_ids=imp.expected_ids("1:1005"), projector=projection)
        self.assertEqual(len(plan["entries"]), 1005)
        self.assertEqual(plan["coverage"]["considered"], 1005)
        imp.write_new(self.root / "large-plan.json", plan)
        loaded = json.loads((self.root / "large-plan.json").read_text(encoding="utf-8"))
        imp.preflight(runtime, api, loaded)
        self.assertEqual((runtime.updates, runtime.publications), (0, 0))
        loaded["entries"][-1]["patch"]["name"] = "tampered last entry"
        with self.assertRaisesRegex(ValueError, "plan actions differ"):
            imp.preflight(runtime, api, loaded)

    def test_http_selection_above_old_response_and_count_bounds(self):
        value = copy.deepcopy(self.api.value)
        value["inputs"] = [value["inputs"][0]] * 1005
        value["transport_test_padding"] = "x" * ((64 << 20) + 1)
        content = json.dumps(value).encode()
        api = imp.API("http://catalog.test", "s" * 32)
        with patch.object(api, "open", return_value=io.BytesIO(content)):
            loaded = api.selection()
        self.assertEqual(len(loaded["inputs"]), 1005)
        self.assertEqual(loaded["transport_test_padding"], value["transport_test_padding"])

    def test_url_path_and_expected_id_guards(self):
        for value in ("http://user:pass@test", "https://test/path", "file:///tmp", "https://test/?x=1"):
            with self.assertRaises(ValueError):
                imp.API(value, "s" * 32)
        api = imp.API("https://test", "s" * 32)
        for uri in ("https://evil/asset", "/v1/assets/../secret", "/config/secrets", "/v1/selection?token=x"):
            with self.assertRaises(ValueError):
                api.open(uri)
        with self.assertRaises(ValueError):
            imp.build_plan(self.runtime, self.api, 49, "roms/Test/retail", {1, 99}, projector=projection)
        for value in ("../escape", "/absolute", "x\\escape", "C:/secret", "x//secret"):
            with self.assertRaises(ValueError):
                imp.relative(value)


if __name__ == "__main__":
    unittest.main()
