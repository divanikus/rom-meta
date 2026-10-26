#!/usr/bin/env python3
"""One-shot native RomM consumer of the independent RomMeta API.

Metadata/media inputs come only from HTTP. Native runtime imports are lazy so the
portable planner can be meaningfully tested without a live RomM database.
"""
from __future__ import annotations

import argparse
import copy
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import subprocess
import sys
import tempfile
from urllib.error import HTTPError, URLError
from urllib.parse import urlparse
from urllib.request import HTTPRedirectHandler, Request, build_opener

class InvalidImageError(ValueError):
    """Verified source bytes cannot be decoded as an image."""


VERSION = "5.4.1"
MAX_ASSET = 512 << 20
FIELDS = {"name", "summary", "manual_metadata", "igdb_id", "ss_id", "launchbox_id", "hasheous_id", "tgdb_id", "igdb_metadata", "ss_metadata", "launchbox_metadata", "hasheous_metadata"}
ROLES = {
    "box-3d": "box3d", "box-back": "box2d_back", "box-spine": "box2d_side",
    "cart-front": "physical", "cart-3d": "physical", "fanart": "fanart",
    "logo": "logo", "title-screen": "title_screen", "gameplay-video": "video",
}
GML = {"box3d", "physical", "marquee", "video", "miximage", "miximage_v2"}
LB = {"box2d", "box2d_back", "box2d_side", "box3d", "video"}
SS = {"bezel", "box2d", "box2d_back", "box2d_side", "box3d", "miximage", "miximage_v2", "physical", "title_screen", "marquee", "logo", "fanart", "video", "video_normalized"}


def progress(message):
    print(message, file=sys.stderr, flush=True)


def media_map(action, items):
    # Native publication stays serial. Only independent job-local media work runs
    # concurrently, and all active tasks stop before staging can be discarded.
    pool = ThreadPoolExecutor(max_workers=4)
    try:
        yield from pool.map(action, items)
    finally:
        pool.shutdown(wait=True, cancel_futures=True)


def canonical(value):
    return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")


def digest(value):
    return hashlib.sha256(canonical(value)).hexdigest()


def hash_file(path):
    sha = hashlib.sha256()
    size = 0
    with Path(path).open("rb") as stream:
        for block in iter(lambda: stream.read(1 << 20), b""):
            sha.update(block)
            size += len(block)
    return {"size": size, "sha256": sha.hexdigest()}


def normalized_cover(source):
    from io import BytesIO
    from PIL import Image, ImageOps
    # Decode real bytes, not the source suffix: a .jpg can contain RGBA PNG data.
    # Native derivative paths/encoding are deterministic; source bytes stay intact.
    with Image.open(source) as image:
        alpha = image.mode in ("RGBA", "LA") or "transparency" in image.info
        oriented = ImageOps.exif_transpose(image)
        normalized = oriented.convert("RGBA" if alpha else "RGB")
        # A JPEG ICC profile can exceed the native PNG decoder limit. These
        # derivatives contain decoded pixels; leave the original asset intact.
        normalized.info.pop("icc_profile", None)
        result = BytesIO()
        normalized.save(result, format="PNG")
        result.seek(0)
        return result


def relative(value):
    if not isinstance(value, str) or not value or "\\" in value or ":" in value:
        raise ValueError("invalid relative path")
    if value.startswith("/") or any(p in ("", ".", "..") for p in value.split("/")):
        raise ValueError("invalid path component")
    return value


def write_new(path, value):
    with Path(path).open("x", encoding="utf-8") as stream:
        os.chmod(path, 0o600)
        json.dump(value, stream, ensure_ascii=False, indent=2)
        stream.write("\n")
        stream.flush()
        os.fsync(stream.fileno())


def save_receipt(path, value):
    path = Path(path)
    with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8", dir=path.parent, delete=False) as stream:
        temporary = Path(stream.name)
        os.chmod(temporary, 0o600)
        json.dump(value, stream, ensure_ascii=False, indent=2)
        stream.write("\n")
        stream.flush()
        os.fsync(stream.fileno())
    temporary.replace(path)


def load_invalid_images(path):
    if not path.exists():
        return {}
    if path.stat().st_size > 1 << 20:
        raise ValueError("invalid-image cache is too large")
    value = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(value, dict) or any(not re.fullmatch(r"[a-f0-9]{64}", key) or not isinstance(reason, str) or not re.fullmatch(r"[A-Za-z]+Error", reason) for key, reason in value.items()):
        raise ValueError("invalid-image cache is malformed")
    return value


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


def check_native_version(origin=None):
    origin = os.environ.get("ROMM_SERVER_URL", "http://romm:8080") if origin is None else origin
    parsed = urlparse(origin)
    if parsed.scheme not in ("http", "https") or not parsed.netloc or parsed.username or parsed.password or parsed.path not in ("", "/") or parsed.query or parsed.fragment:
        raise ValueError("ROMM_SERVER_URL must be an HTTP(S) origin without credentials")
    try:
        with build_opener(NoRedirect()).open(Request(origin.rstrip("/") + "/api/heartbeat"), timeout=10) as response:
            data = response.read((1 << 20) + 1)
        if len(data) > 1 << 20:
            raise ValueError("RomM heartbeat exceeds 1 MiB")
        value = json.loads(data)
    except (URLError, OSError, json.JSONDecodeError, UnicodeError):
        raise ValueError("Cannot read RomM server version; check ROMM_SERVER_URL and connectivity") from None
    system = value.get("SYSTEM") if isinstance(value, dict) else None
    version = system.get("VERSION") if isinstance(system, dict) else None
    if not isinstance(version, str) or not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?", version):
        raise ValueError("RomM heartbeat has no valid SYSTEM.VERSION")
    if version != VERSION:
        raise ValueError(f"RomM server reports {version}; importer requires {VERSION}. Rebuild the matching importer before continuing")
    return version


class API:
    def __init__(self, origin, token):
        parsed = urlparse(origin)
        if parsed.scheme not in ("http", "https") or not parsed.netloc or parsed.username or parsed.password or parsed.path not in ("", "/") or parsed.query or parsed.fragment:
            raise ValueError("API must be an HTTP(S) origin; use HTTP only on a trusted private network")
        if len(token) < 32:
            raise ValueError("ROMMETA_API_TOKEN is required (32+ characters)")
        self.origin = origin.rstrip("/")
        self.token = token
        self.opener = build_opener(NoRedirect())

    def open(self, uri, data=None):
        if not re.fullmatch(r"/v1/(selection|platforms|lookup|assets/[a-f0-9]{64})", uri):
            raise ValueError("unapproved API route")
        return self.opener.open(Request(self.origin + uri, data=canonical(data) if data is not None else None, headers={"Authorization": "Bearer " + self.token, "Content-Type": "application/json"}), timeout=None if uri == "/v1/lookup" else 120)

    def selection(self):
        with self.open("/v1/selection") as response:
            value = json.load(response)
        if value.get("schema_version") != 1 or not isinstance(value.get("inputs"), list) or not value["inputs"] or not re.fullmatch(r"[a-f0-9]{64}", value.get("digest", "")):
            raise ValueError("unsupported API snapshot")
        return value

    def capabilities(self):
        try:
            with self.open("/v1/platforms") as response:
                value = json.load(response)
        except HTTPError as error:
            if error.code == 404:
                return None  # Compatibility with the original fixed-selection API.
            raise
        if value.get("schema_version") != 1 or value.get("mode") != "catalog" or not isinstance(value.get("platforms"), list):
            raise ValueError("unsupported catalog capabilities")
        names = [p.get("platform") for p in value["platforms"]]
        if not all(isinstance(n, str) and n for n in names) or len(names) != len(set(names)):
            raise ValueError("invalid catalog platform list")
        return value

    def lookup(self, query):
        try:
            with self.open("/v1/lookup", query) as response:
                value = json.load(response)
        except HTTPError as error:
            detail = error.read(1500).decode("utf-8", errors="replace").strip().replace(self.token, "<redacted>")
            raise ValueError(f"Catalog lookup failed (HTTP {error.code}): {detail}") from None
        if value.get("schema_version") != 1 or not isinstance(value.get("inputs"), list) or not re.fullmatch(r"[a-f0-9]{64}", value.get("digest", "")):
            raise ValueError("unsupported lookup snapshot")
        validate_lookup(query, value)
        return value

    def download(self, asset, destination):
        if not 0 < asset["size"] <= MAX_ASSET:
            raise ValueError("asset size outside supported bound")
        with self.open(asset["uri"]) as response, Path(destination).open("xb") as stream:
            total = 0
            sha = hashlib.sha256()
            while block := response.read(1 << 20):
                total += len(block)
                if total > asset["size"]:
                    raise ValueError("asset exceeded planned size")
                sha.update(block)
                stream.write(block)
            if total != asset["size"] or sha.hexdigest() != asset["sha256"]:
                raise ValueError("asset differs from reviewed digest")


class LookupAPI:
    def __init__(self, api, query, selection=None):
        self.api, self.query, self.value = api, copy.deepcopy(query), selection
        self.origin = api.origin

    def selection(self):
        value = copy.deepcopy(self.value) if self.value is not None else self.api.lookup(self.query)
        validate_lookup(self.query, value)
        return value

    def download(self, asset, destination):
        return self.api.download(asset, destination)


def validate_lookup(query, selection):
    if selection.get("media_mode", "") != query.get("media", "") or selection.get("game_binding", False) != query.get("game_binding", False):
        raise ValueError("lookup changed requested media/identity scope")
    if selection.get("platform") != query["platform"]:
        raise ValueError("lookup returned another platform")
    submitted = {item["rom"]: item for item in query["inputs"]}
    included = selection["inputs"]
    skipped = coverage(selection)["skipped"]
    names = [item["record"]["rom"] for item in included] + [item["rom"] for item in skipped]
    if len(submitted) != len(query["inputs"]) or len(names) != len(set(names)) or set(names) != set(submitted):
        raise ValueError("lookup changed submitted input membership")
    for item in included:
        observed = submitted[item["record"]["rom"]]
        if item["outer"] != observed["outer"] or item["payload"] != observed["payload"] or item.get("disc") != observed.get("disc") or item.get("catalog") != observed.get("catalog"):
            raise ValueError("lookup changed submitted payload/outer proof")


def inspect_inputs(runtime, system, names):
    request = {"root": str(runtime.library), "system": system, "roms": names, "name_binding": True}
    result = subprocess.run([os.environ.get("ROMMETA_BIN", "/usr/local/bin/rom-meta"), "romm", "inspect-inputs"], input=canonical(request), stdout=subprocess.PIPE, check=False)
    if result.returncode:
        raise ValueError("native payload inspection failed")
    value = json.loads(result.stdout)
    got = [item["rom"] for item in value["inputs"]] + [item["rom"] for item in value["skipped"]]
    if len(got) != len(set(got)) or set(got) != set(names):
        raise ValueError("native observation membership differs")
    return value


def input_identity(item, system):
    if item.get("catalog"):
        return {"size": item["outer"]["size"], "catalog": copy.deepcopy(item["catalog"]), "system": system}
    if item.get("disc"):
        if not system:
            raise ValueError("CHD observation lacks its reader system")
        return {"size": item["outer"]["size"], "disc": copy.deepcopy(item["disc"]), "system": system}
    return item["outer"]


def native_disc_files(snapshot, full, disc):
    expected = {}
    for item in disc["files"]:
        name = PurePosixPath(full) / item["name"] if disc["directory"] else PurePosixPath(full)
        expected[str(name)] = item["size"]
    actual = {}
    for item in snapshot["files"]:
        if not all(key in item for key in ("file_path", "file_name", "file_size_bytes")):
            raise ValueError("native disc file inventory is incomplete")
        name = str(PurePosixPath(relative(item["file_path"])) / relative(item["file_name"]))
        if name in actual:
            raise ValueError("native disc file inventory contains duplicates")
        actual[name] = item["file_size_bytes"]
    if actual != expected:
        raise ValueError("native disc members differ from observed CHDs")


def coverage(selection):
    value = selection.get("coverage", {"considered": len(selection["inputs"]), "included": len(selection["inputs"]), "skipped": []})
    skipped = value.get("skipped", [])
    if value.get("included") != len(selection["inputs"]) or value.get("considered") != len(selection["inputs"]) + len(skipped):
        raise ValueError("API coverage count differs from its inputs/skips")
    return copy.deepcopy(value)


class ProjectionConflicts(ValueError):
    def __init__(self, conflicts):
        self.conflicts = conflicts
        super().__init__("Catalog provider identity conflict: " + "; ".join(item["rom"] + ": " + item["reason"] for item in conflicts))


def project(selection, rows):
    before = {}
    for record, row in rows:
        values = copy.deepcopy(row["columns"])
        values["path_cover_large"] = values.get("path_cover_l")
        values["path_cover_small"] = values.get("path_cover_s")
        before[record["record"]["rom"]] = values
    result = subprocess.run([os.environ.get("ROMMETA_BIN", "/usr/local/bin/rom-meta"), "romm", "project-snapshot", "--report-conflicts"], input=canonical({"selection": selection, "before": before}), stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False)
    if result.returncode:
        detail = result.stderr.decode("utf-8", errors="replace").strip()
        raise ValueError("native consumer projection rejected API snapshot" + (": " + detail[:2000] if detail else ""))
    output = json.loads(result.stdout)
    if len(output) != len(rows) or len({entry["rom"] for entry in output}) != len(rows) or {entry["rom"] for entry in output} != {item["record"]["rom"] for item, _ in rows}:
        raise ValueError("projection count or membership differs")
    conflicts = [{"rom": entry["rom"], "status": "metadata-conflict", "reason": entry["conflict"]} for entry in output if entry.get("conflict")]
    if conflicts:
        raise ProjectionConflicts(conflicts)
    return {entry["rom"]: entry for entry in output}


def carrier(asset, role):
    source = asset["source"]
    if source == "screenscraper" and role in SS:
        return "ss_metadata", role
    if source == "launchbox" and role in LB:
        return "launchbox_metadata", role
    # A local gamelist projection is a real native generic-media schema. It is not
    # a claim that SS supplied EmuMovies/LB assets; actual provenance stays explicit.
    local = "marquee" if role == "logo" else role
    if local in GML:
        return "gamelist_metadata", local
    return None


def asset_rank(asset, record, card, policy):
    rule = policy.get("media", {}).get("cover", {})
    sources = rule.get("sources", ["emumovies", "launchbox", "screenscraper", "igdb", "openvgdb"])
    variants = rule.get("variants", ["original", "unknown", "reconstructed", "fanart"])
    release = record.get("release", "")
    basis = release or record.get("metadata_release", "")
    regions = [str(v).lower() for v in card["game"]["data"].get("releases", {}).get(basis, {}).get("regions", [])]
    region = asset.get("region", "").lower()
    region_score = 0 if region in regions else (1 if region in ("", "world", "unspecified") else 2)
    source_score = sources.index(asset["source"]) if asset["source"] in sources else len(sources)
    variant = asset.get("variant", "unknown")
    variant_score = variants.index(variant) if variant in variants else len(variants)
    # Prefer the exact release folder, then ordinary card art, then same-card donor.
    parent = PurePosixPath(asset["path"]).parent.name
    scope_score = 0 if release and parent == release else (1 if parent == "media" else 2)
    return (variant_score, region_score, source_score, scope_score, asset["path"])


def cover_review_entries(review, platform_id):
    if review is None:
        return {}
    if not isinstance(review, dict) or review.get("schema_version") != 1 or review.get("platform_id") != platform_id or not review.get("entries") or set(review) != {"schema_version", "platform_id", "entries"}:
        raise ValueError("invalid cover refresh review")
    result = {}
    for entry in review["entries"]:
        before = entry.get("before", {})
        if set(entry) != {"id", "source_sha256", "before", "resources", "reason", "evidence"} or not isinstance(entry.get("id"), int) or entry["id"] < 1 or entry["id"] in result or not re.fullmatch(r"[a-f0-9]{64}", entry.get("source_sha256", "")) or not entry.get("reason") or not isinstance(entry.get("evidence"), list) or not entry["evidence"] or not all(isinstance(e, str) and e.strip() for e in entry["evidence"]):
            raise ValueError("invalid/duplicate cover review entry")
        if set(before) != {"path_cover_l", "path_cover_s", "url_cover"} or before["url_cover"] not in (None, ""):
            raise ValueError("reviewed custom cover requires exact paths and no remote owner URL")
        paths = {relative(before["path_cover_l"]), relative(before["path_cover_s"])}
        if len(paths) != 2 or set(entry["resources"]) != paths:
            raise ValueError("review requires both distinct old cover resources")
        for state in entry["resources"].values():
            if set(state) != {"size", "sha256"} or not isinstance(state["size"], int) or state["size"] < 1 or not re.fullmatch(r"[a-f0-9]{64}", state["sha256"]):
                raise ValueError("invalid reviewed cover resource digest")
        result[entry["id"]] = entry
    return result


def selected_cover(selection, item):
    selected = item["record"].get("media", {}).get("cover", {}).get("source_path")
    card = selection["cards"][item["record"]["game"]]
    return next((asset for asset in card["assets"] if asset["path"] == selected and asset["kind"] == "box-front"), None)


def refreshed_cover_paths(before, asset):
    folder = f'roms/{before["platform_id"]}/{before["id"]}/cover/rommeta-{asset["sha256"]}'
    return folder + "/big.png", folder + "/small.png"


def cover_refresh_intent(runtime, snapshot, asset, review):
    if review is None or asset is None:
        return None
    before = snapshot["columns"]
    if set(before.get("locked_fields") or []).intersection({"path_cover_l", "path_cover_s"}):
        raise ValueError("explicit owner cover path lock prevents refresh")
    # The old bytes remain immutable even after an accepted additive refresh.
    for path, state in review["resources"].items():
        if snapshot.get("resources", {}).get(path, None) != state and hash_file(runtime.resource_path(path)) != state:
            raise ValueError("reviewed old cover bytes changed")
    paths = refreshed_cover_paths(before, asset)
    if before.get("path_cover_l") == paths[0] and before.get("path_cover_s") == paths[1] and before.get("url_cover") == review["before"]["url_cover"]:
        # No overwrite is authorized here. A later owner byte change at these
        # already selected paths remains untouched, not silently repaired.
        return None
    if any(before.get(key) != value for key, value in review["before"].items()):
        raise ValueError("current cover differs from reviewed baseline; preserve owner choice")
    if asset["sha256"] == review["source_sha256"]:
        return None
    return {"review": copy.deepcopy(review), "new_source_sha256": asset["sha256"], "source": asset["source"], "style": asset.get("style", "unknown"), "style_evidence": asset.get("style_evidence", "no-bound-provider-category"), "paths": list(paths)}


def export_cover_review(plan, selection, plan_path):
    if selection["digest"] != plan["source_digest"]:
        raise ValueError("API changed before exporting cover baseline")
    inputs = {item["record"]["rom"]: item for item in selection["inputs"]}
    evidence = str(plan_path) + ":sha256:" + hash_file(plan_path)["sha256"]
    entries = []
    for entry in plan["entries"]:
        before = entry["before"]["columns"]
        asset = selected_cover(selection, inputs[entry["record"]["rom"]])
        paths = {before.get("path_cover_l"), before.get("path_cover_s")}
        if not asset or None in paths or "" in paths or len(paths) != 2 or before.get("url_cover") or set(before.get("locked_fields") or []).intersection({"path_cover_l", "path_cover_s"}):
            continue
        resources = {path: entry["before"]["resources"].get(path) for path in paths}
        if any(not state or "missing" in state for state in resources.values()):
            continue
        entries.append({"id": entry["id"], "source_sha256": asset["sha256"], "before": {key: before.get(key) for key in ("path_cover_l", "path_cover_s", "url_cover")}, "resources": resources, "reason": "Explicit current cover baseline; review ownership before refresh", "evidence": [evidence]})
    review = {"schema_version": 1, "platform_id": plan["platform_id"], "entries": entries}
    cover_review_entries(review, plan["platform_id"])
    return review


def igdb_reference_repair(before, providers, enabled):
    if not enabled:
        return None
    reviews = providers.get("reference_reviews", [])
    if len(reviews) > 1:
        raise ValueError("multiple reviewed IGDB decisions in one card")
    if not reviews:
        return None
    review = reviews[0]
    if review.get("source") != "hasheous" or review.get("action") not in ("replace", "suppress") or not isinstance(review.get("from_id"), int) or review["from_id"] < 1 or not review.get("reason") or not review.get("evidence"):
        raise ValueError("invalid reviewed IGDB identity repair")
    slot = providers.get("slots", {}).get("igdb", {})
    target = review.get("to_id") if review["action"] == "replace" else None
    if review["action"] == "replace":
        if not isinstance(target, int) or target < 1 or target == review["from_id"] or slot.get("id") != target or slot.get("source") != "igdb" or not slot.get("source_slug"):
            raise ValueError("IGDB repair lacks verified direct replacement")
    elif slot or review.get("to_id"):
        raise ValueError("suppressed IGDB reference still has an active slot")
    old = before.get("igdb_id")
    if old == target:
        return None
    if old != review["from_id"] or before.get("slug") or before.get("igdb_metadata") or set(before.get("locked_fields") or []).intersection({"igdb_id", "igdb_metadata", "slug"}):
        raise ValueError("IGDB repair requires exact old ID, blank slug/own metadata and unlocked fields")
    return {"before_id": old, "after_id": target, "review": copy.deepcopy(review)}


def plan_entry(runtime, selection, item, snapshot, projection, repair_igdb_references=False, cover_review=None, invalid_images=None):
    before = snapshot["columns"]
    record = item["record"]
    card = selection["cards"][record["game"]]
    locks = set(before.get("locked_fields") or [])
    patch = {}
    for key, value in projection["form"].items():
        native = key[4:] if key.startswith("raw_") else key
        if native not in FIELDS:
            raise ValueError("projector proposed unsupported native field")
        value = json.loads(value) if key.startswith("raw_") else (int(value) if key.endswith("_id") else value)
        protected = {native}
        for identity, metadata in (("igdb_id", "igdb_metadata"), ("ss_id", "ss_metadata"), ("launchbox_id", "launchbox_metadata"), ("hasheous_id", "hasheous_metadata")):
            if native in (identity, metadata):
                protected.update((identity, metadata))
        if protected & locks:
            continue
        if before.get(native) != value:
            patch[native] = value
    providers = projection["providers"]
    repair = igdb_reference_repair(before, providers, repair_igdb_references)
    if repair:
        if "igdb_id" in patch and patch["igdb_id"] != repair["after_id"]:
            raise ValueError("IGDB projection differs from reviewed repair")
        patch["igdb_id"] = repair["after_id"]
    igdb = providers.get("slots", {}).get("igdb", {})
    slug = igdb.get("source_slug")
    if not before.get("slug") and not locks.intersection({"slug", "igdb_id"}) and slug and patch.get("igdb_id", before.get("igdb_id")) == igdb["id"]:
        if not re.fullmatch(r"[a-z0-9][a-z0-9$-]{0,399}", slug):
            raise ValueError("invalid source IGDB slug")
        patch["slug"] = slug
    files = []
    omissions = []
    assets = card["assets"]
    invalid = getattr(runtime, "invalid_images", {}) if invalid_images is None else invalid_images

    def usable(asset):
        reason = invalid.get(asset["sha256"])
        if reason is None:
            return True
        omission = {"path": asset["path"], "kind": asset["kind"], "source": asset["source"], "sha256": asset["sha256"], "reason": "invalid image: " + reason}
        omissions.append(omission)
        progress(f'WARNING | Skip invalid image | {asset["source"]} | {asset["path"]} | {reason}')
        return False

    by_path = {asset["path"]: asset for asset in assets}
    selected = record.get("media", {})
    cover = by_path.get(selected.get("cover", {}).get("source_path"))
    if cover and not usable(cover):
        cover = None
    cover_refresh = cover_refresh_intent(runtime, snapshot, cover, cover_review)
    if cover_refresh:
        large, small = cover_refresh["paths"]
        patch.update({"path_cover_l": large, "path_cover_s": small})
        files.append({"asset": cover, "path": large, "paths": [large, small], "column": "cover", "field": "reviewed native cover refresh", "role": "cover", "id": before["id"], "actual_source": cover["source"], "projection": "native custom cover", "refresh": True})
    elif cover and not before.get("path_cover_l") and not before.get("path_cover_s") and not before.get("url_cover") and not locks.intersection({"url_cover", "path_cover_l", "path_cover_s"}):
        large, small = refreshed_cover_paths(before, cover)
        patch.update({"path_cover_l": large, "path_cover_s": small})
        patch["locked_fields"] = sorted(locks | {"url_cover"})
        files.append({"asset": cover, "path": large, "paths": [large, small], "column": "cover", "field": "native cover derivatives", "role": "cover", "id": before["id"], "actual_source": cover["source"], "projection": "native custom cover", "additive": True})
    manual = by_path.get(selected.get("manual", {}).get("source_path"))
    if manual and manual["size"] > 64 << 20:
        raise ValueError("native manual exceeds 64 MiB limit")
    if manual and not before.get("path_manual") and not before.get("url_manual") and not locks.intersection({"url_manual", "path_manual"}):
        path = runtime.media_path(before["platform_id"], before["id"], "manual", manual)
        patch["path_manual"] = path
        patch["locked_fields"] = sorted(set(patch.get("locked_fields", locks)) | {"url_manual"})
        files.append({"asset": manual, "path": path, "column": "path_manual", "field": "path_manual", "role": "manual", "actual_source": manual["source"], "projection": "native manual"})
    for role in sorted(set(ROLES.values())):
        candidates = [a for a in assets if ROLES.get(a["kind"]) == role and carrier(a, role)]
        if not candidates:
            continue
        # Preserve occupied logical artwork across all native carriers, including URLs.
        possible = {carrier(a, role) for a in candidates}
        if role in SS:
            possible.add(("ss_metadata", role))
        if role in LB:
            possible.add(("launchbox_metadata", role))
        local_role = "marquee" if role == "logo" else role
        if local_role in GML:
            possible.add(("gamelist_metadata", local_role))
        occupied = any((before.get(column) or {}).get(key + suffix) for column, key in possible for suffix in ("_path", "_url"))
        if occupied:
            continue
        candidates.sort(key=lambda a: asset_rank(a, record, card, selection.get("policy", {})))
        asset = candidates[0]
        if not usable(asset):
            continue
        column, native_role = carrier(asset, role)
        provider = {"ss_metadata": ("screenscraper", "ss_id"), "launchbox_metadata": ("launchbox", "launchbox_id")}.get(column)
        if provider:
            source_name, id_field = provider
            source_id = providers.get("slots", {}).get(source_name, {}).get("id")
            native_id = patch.get(id_field, before.get(id_field))
            if native_id and native_id != source_id:
                omissions.append({"role": role, "reason": "existing different or unbound provider identity; provider artwork preserved", "provider": source_name})
                continue
        if locks.intersection({column, native_role + "_path", native_role + "_url"}):
            omissions.append({"role": role, "reason": "owner lock"})
            continue
        native = copy.deepcopy(patch.get(column, before.get(column) or {}))
        path = runtime.media_path(before["platform_id"], before["id"], native_role, asset)
        native[native_role + "_path"] = path
        patch[column] = native
        files.append({"asset": asset, "path": path, "column": column, "field": native_role + "_path", "actual_source": asset["source"], "projection": "native provider" if column != "gamelist_metadata" else "local gamelist media"})
    if "url_screenshots" not in locks and "path_screenshots" not in locks:
        screens = list(before.get("path_screenshots") or [])
        seen = set()
        for asset in sorted((a for a in assets if a["kind"] == "screenshot"), key=lambda a: asset_rank(a, record, card, selection.get("policy", {}))):
            if asset["sha256"] in seen:
                continue
            seen.add(asset["sha256"])
            if not usable(asset):
                continue
            path = runtime.media_path(before["platform_id"], before["id"], "screenshots", asset)
            if path not in screens:
                screens.append(path)
                files.append({"asset": asset, "path": path, "column": "path_screenshots", "field": "append", "actual_source": asset["source"], "projection": "shared screenshots"})
        if screens != (before.get("path_screenshots") or []):
            patch["path_screenshots"] = screens
    for asset in assets:
        if asset["kind"] not in ROLES and asset["kind"] not in ("screenshot", "box-front", "manual"):
            omissions.append({"path": asset["path"], "kind": asset["kind"], "reason": "no supported native role in this profile"})
    if not slug and igdb.get("id") and not before.get("slug"):
        omissions.append({"role": "igdb slug", "reason": "source is ID-only; no title guess"})
    runtime.validate_patch(patch)
    return {"id": before["id"], "record": record, "input": input_identity(item, selection.get("system", "")), "before": snapshot, "before_hash": digest(snapshot), "patch": patch, "files": files, "providers": providers, "omissions": omissions, "igdb_reference_repair": repair, "cover_refresh": cover_refresh}


def resolve_target(runtime, selection, name):
    # Match complete native names (including owner display names), never fuzzy titles.
    key = " ".join(name.split()).casefold()
    platforms = runtime.platforms()
    matches = [p for p in platforms if key and any(key == " ".join(str(p.get(field) or "").split()).casefold() for field in ("name", "custom_name", "fs_slug", "slug"))]
    if len(matches) != 1:
        available = ", ".join(sorted({p.get("custom_name") or p["name"] for p in platforms}))
        raise ValueError(f'RomM platform "{name}" is {"ambiguous" if matches else "not found"}; use its complete unique name. Available: {available}')
    platform = matches[0]
    if platform["fs_slug"] != selection["platform"]:
        raise ValueError(f'API serves "{selection["platform"]}", but selected RomM platform uses "{platform["fs_slug"]}"; select the same system on both utilities')
    # Infer a common prefix from real scanned paths, including arbitrary category
    # depth. No assumption about "roms/<platform>/retail" or the server mount.
    by_name_size = {}
    for row in runtime.inventory(platform["id"]):
        full = PurePosixPath(relative(row["fs_path"])) / relative(row["fs_name"])
        by_name_size.setdefault((full.name, row["fs_size_bytes"]), []).append(full)
    prefixes = None
    for item in selection["inputs"]:
        source = PurePosixPath(relative(item["record"]["rom"]))
        candidates = set()
        for full in by_name_size.get((source.name, item["outer"]["size"]), []):
            if len(full.parts) > len(source.parts) and full.parts[-len(source.parts):] == source.parts:
                candidates.add(str(PurePosixPath(*full.parts[:-len(source.parts)])))
        prefixes = candidates if prefixes is None else prefixes & candidates
        if not prefixes:
            raise ValueError(f'No common scanned RomM folder matches API input "{source}"; scan the games and select the same ROM folder on rom-meta')
    if len(prefixes or ()) != 1:
        raise ValueError("More than one RomM folder matches the complete API selection; select a narrower source ROM folder")
    prefix = prefixes.pop()
    progress(f'Platform: {platform.get("custom_name") or platform["name"]} | ID {platform["id"]} | ROM root: {prefix}')
    return platform["id"], prefix


IDENTITY_FIELDS = {"igdb": "igdb_id", "screenscraper": "ss_id", "launchbox": "launchbox_id", "hasheous": "hasheous_id"}


def reconcile_identities(entries):
    """Complete only conflict-free source identity components, never name unions."""
    # Recompute derived actions from original projection, including older drafts.
    for entry in entries:
        fills = entry.pop("identity_fills", [])
        for fill in fills:
            entry["patch"].pop(fill["field"], None)
        if any(f["field"] == "igdb_id" for f in fills) and not entry["before"]["columns"].get("slug") and not entry["providers"].get("slots", {}).get("igdb"):
            entry["patch"].pop("slug", None)
        for ref in entry.pop("identity_reference_only", []):
            field = ref["field"]
            if entry["patch"].get(field) is None and entry["before"]["columns"].get(field) == ref["value"]:
                entry["patch"].pop(field, None)
    parent = list(range(len(entries)))
    def find(i):
        while parent[i] != i:
            parent[i] = parent[parent[i]]; i = parent[i]
        return i
    seen, values = {}, []
    for i, entry in enumerate(entries):
        slots = entry["providers"].get("slots", {})
        ids = {field: slots[source]["id"] for source, field in IDENTITY_FIELDS.items()
               if slots.get(source, {}).get("id")}
        values.append(ids)
        if entry["providers"].get("reference_reviews"):
            continue  # Curated foreign-reference decisions must not be undone by inheritance.
        for field, value in ids.items():
            key = field, value
            if key in seen:
                parent[find(i)] = find(seen[key])
            seen[key] = i
    groups = {}
    for i in range(len(entries)):
        groups.setdefault(find(i), []).append(i)
    conflicts = []
    for indexes in groups.values():
        donors = {}
        for i in indexes:
            for field, value in values[i].items():
                donors.setdefault(field, {}).setdefault(value, i)
        bad = [f for f, ids in donors.items() if len(ids) > 1]
        if bad:
            conflicts.append({"ids": [entries[i]["id"] for i in indexes], "fields": sorted(set(bad)),
                              "reason": "multiple provider identities; ambiguous fields are not completed"})
        for i in indexes:
            entry = entries[i]; before = entry["before"]["columns"]
            owner_conflicts = [f for f, ids in donors.items() if before.get(f) and before[f] not in ids]
            if owner_conflicts:
                conflicts.append({"ids": [entry["id"]], "fields": owner_conflicts,
                                  "reason": "existing owner identity differs from source; row held"})
                entry.update(patch={}, files=[], cover_refresh=None, quality_hold="conflicting owner identity")
                continue
            locks = set(before.get("locked_fields") or [])
            for field, ids in donors.items():
                if len(ids) != 1:
                    continue
                value, donor = next(iter(ids.items()))
                metadata = {"igdb_id": "igdb_metadata", "ss_id": "ss_metadata", "launchbox_id": "launchbox_metadata", "hasheous_id": "hasheous_metadata"}[field]
                if field in values[i] or before.get(field) or entry["patch"].get(field) or before.get(metadata) or locks.intersection({field, metadata}):
                    continue
                entry["patch"][field] = value
                entry.setdefault("identity_fills", []).append({"field": field, "value": value,
                    "source_game": entries[donor]["record"]["game"], "reason": "unique identity consensus through shared provider IDs"})
                if field == "igdb_id" and not before.get("slug") and "slug" not in locks:
                    slug = entries[donor]["providers"].get("slots", {}).get("igdb", {}).get("source_slug")
                    if slug and re.fullmatch(r"[a-z0-9][a-z0-9$-]{0,399}", slug):
                        entry["patch"]["slug"] = slug
            igdb = donors.get("igdb_id", {})
            slugs = {entries[j]["providers"].get("slots", {}).get("igdb", {}).get("source_slug") for j in indexes}
            slugs.discard(None); slugs.discard("")
            if len(igdb) == len(slugs) == 1 and not before.get("slug") and not locks.intersection({"slug", "igdb_id"}) and entry["patch"].get("igdb_id", before.get("igdb_id")) == next(iter(igdb)):
                slug = next(iter(slugs))
                if re.fullmatch(r"[a-z0-9][a-z0-9$-]{0,399}", slug):
                    entry["patch"]["slug"] = slug
    # Foreign TGDB references can connect unrelated games. Quarantine only when
    # three independent primary identities disagree and no strong component joins them.
    refs = {}
    for i, entry in enumerate(entries):
        slot = entry["providers"].get("slots", {}).get("tgdb", {})
        if slot.get("source") == "hasheous-reference":
            refs.setdefault(slot["id"], []).append(i)
    quarantined = []
    for value, indexes in refs.items():
        if len({find(i) for i in indexes}) < 2 or not all(len({values[i][f] for i in indexes if values[i].get(f)}) > 1 for f in ("igdb_id", "ss_id", "hasheous_id")):
            continue
        for i in indexes:
            entry=entries[i]; before=entry["before"]["columns"]
            if entry.get("quality_hold"):
                continue
            if "tgdb_id" in (before.get("locked_fields") or []):
                continue
            entry["patch"].pop("tgdb_id", None)
            if before.get("tgdb_id") == value:
                entry["patch"]["tgdb_id"] = None
            entry.setdefault("identity_reference_only", []).append({"field": "tgdb_id", "value": value,
                "reason": "indirect reference crosses conflicting game identities; retained in provenance"})
        quarantined.append({"provider": "tgdb", "value": value, "ids": [entries[i]["id"] for i in indexes]})
    return {"identity_conflicts": conflicts, "reference_only": quarantined,
            "held_ids": [e["id"] for e in entries if e.get("quality_hold")],
            "identity_fills": sum(len(e.get("identity_fills", [])) for e in entries),
            "unverified_game_bindings": [e["id"] for e in entries if e["record"].get("matched_by") in ("game-name", "catalog-name")]}


def update_reviews(runtime, selection, rows, baseline):
    reviews = []
    for item, snapshot in rows:
        before = snapshot["columns"]; old = baseline.get(str(before["id"]))
        asset = selected_cover(selection, item)
        if not old or not asset or old["source_sha256"] == asset["sha256"]:
            continue
        if any(before.get(k) != v for k, v in old["before"].items()) or before.get("url_cover") or set(before.get("locked_fields") or []).intersection({"path_cover_l", "path_cover_s"}):
            continue
        paths = [before.get("path_cover_l"), before.get("path_cover_s")]
        if not all(paths) or len(set(paths)) != 2:
            continue
        states = {p: hash_file(runtime.resource_path(p)) for p in paths}
        runtime.resource_scope[str(before["id"])] = sorted(paths)
        snapshot["resources"] = states
        reviews.append({"id": before["id"], "source_sha256": old["source_sha256"], "before": old["before"],
                        "resources": states, "reason": "Explicit unlocked cover update after profile change",
                        "evidence": ["generated baseline: " + baseline["_digest"]]})
    return {"schema_version": 1, "platform_id": selection.get("_platform_id"), "entries": reviews} if reviews else None


def set_resource_scope(runtime, plan):
    if plan.get("update_mode") != "metadata-covers":
        runtime.resource_scope = None
        return
    expected = {str(e["id"]): sorted(e["resources"]) for e in (plan.get("cover_refresh_review") or {}).get("entries", [])}
    if plan.get("resource_scope") != expected:
        raise ValueError("update resource scope differs from reviewed covers")
    runtime.resource_scope = expected


def build_plan(runtime, api, platform_id=None, prefix=None, expected_ids=None, projector=None, repair_igdb_references=False, cover_review=None, platform=None, update_mode=False, cover_baseline=None, native_fs_slug=None):
    selection = api.selection()
    source_coverage = coverage(selection)
    if platform is not None:
        if platform_id is not None or prefix is not None:
            raise ValueError("use --platform alone; native ID and path are automatic")
        platform_id, prefix = resolve_target(runtime, selection, platform)
    if platform_id is None or prefix is None:
        raise ValueError("select --platform by name (legacy mode requires both --platform-id and --romm-rom-root)")
    reviews=cover_review_entries(cover_review,platform_id)
    if update_mode:
        if selection.get("media_mode") != "covers":
            raise ValueError("metadata/cover update requires a cover-only API snapshot")
        runtime.resource_scope = {}
    relative(prefix)
    rows = []
    for index, item in enumerate(selection["inputs"], 1):
        if index == 1 or index % 100 == 0 or index == len(selection["inputs"]):
            progress(f'Matching native ROMs {index}/{len(selection["inputs"])}')
        name = relative(item["record"]["rom"])
        full = PurePosixPath(prefix) / name
        snapshot = runtime.find(platform_id, str(full.parent), full.name)
        before = snapshot["columns"]
        if snapshot["platform_fs_slug"] != (native_fs_slug or selection["platform"]) or before["fs_size_bytes"] != item["outer"]["size"]:
            raise ValueError("native inventory differs from exact API selection")
        # Update lookup already observed the bytes; repeat only changed inputs
        # in apply's final preflight rather than hashing the collection again here.
        if not update_mode:
            runtime.verify_input(snapshot, input_identity(item, selection.get("system", "")))
        rows.append((item, snapshot))
    ids = [row["columns"]["id"] for _, row in rows]
    if not set(reviews).issubset(ids):
        raise ValueError("cover review contains IDs outside exact API/native selection")
    if len(set(ids)) != len(ids) or expected_ids is not None and (len(ids) != len(expected_ids) or set(ids) != set(expected_ids)):
        raise ValueError("native IDs differ from explicit bounded selection")
    progress(f'Projecting metadata for {len(rows)} exact native ROMs')
    projected = (projector or project)(selection, rows)
    if update_mode:
        cover_review = update_reviews(runtime, dict(selection, _platform_id=platform_id), rows, cover_baseline or {})
        reviews = cover_review_entries(cover_review, platform_id)
    entries = [plan_entry(runtime, selection, item, row, projected[item["record"]["rom"]], repair_igdb_references, reviews.get(row["columns"]["id"])) for item, row in rows]
    quality = reconcile_identities(entries) if update_mode else None
    if update_mode:
        for entry in entries:
            runtime.validate_patch(entry["patch"])
    result = {"schema_version": 1, "repair_igdb_references": repair_igdb_references, "cover_refresh_review": cover_review, "runtime": VERSION, "api": api.origin, "source_digest": selection["digest"], "invalid_images": dict(getattr(runtime, "invalid_images", {})), "coverage": source_coverage, "platform_id": platform_id, "platform_fs_slug": native_fs_slug or selection["platform"], "prefix": prefix, "entries": entries, "resource_bytes": sum(f["asset"]["size"] for e in entries for f in e["files"]), "unique_download_bytes": sum(a["size"] for a in {f["asset"]["id"]: f["asset"] for e in entries for f in e["files"]}.values())}
    if update_mode:
        result.update(update_mode="metadata-covers", resource_scope=copy.deepcopy(runtime.resource_scope), quality=quality)
    return result


def preflight(runtime, api, plan):
    set_resource_scope(runtime, plan)
    # Conflict annotations are a set; earlier generated drafts may preserve map order.
    for entry in plan.get("entries", []):
        if entry.get("providers", {}).get("conflicts"):
            entry["providers"]["conflicts"].sort(key=lambda c: c["provider"])
    if plan.get("kind") == "check":
        raise ValueError("read-only check report cannot be applied; create a fresh plan")
    if not isinstance(plan.get("repair_igdb_references", False), bool) or plan.get("schema_version") != 1 or plan.get("runtime") != VERSION or plan.get("api") != api.origin or not plan.get("entries"):
        raise ValueError("invalid native import plan")
    if "lookup" in plan:
        if not isinstance(api, LookupAPI) or api.query != plan["lookup"]:
            raise ValueError("lookup query differs from reviewed plan")
        caps = api.api.capabilities()
        capability = next((p for p in (caps or {}).get("platforms", []) if p["platform"] == plan["lookup"]["platform"]), None)
        if not capability or not capability.get("supported") or capability["system"] != plan["lookup_system"]:
            raise ValueError("catalog reader changed")
        native = next((p for p in runtime.platforms() if p["id"] == plan["platform_id"]), None)
        if native is None or lookup_capability(native, caps["platforms"]) != capability:
            raise ValueError("native/catalog system mapping changed")
        observations = plan["lookup"]["inputs"]
        if plan.get("update_mode") == "metadata-covers":
            changed = {e["record"]["rom"] for e in plan["entries"] if e["patch"]}
            observations = [i for i in observations if i["rom"] in changed]
        names = [str(PurePosixPath(relative(plan["prefix"])) / relative(i["rom"])) for i in observations]
        observed = inspect_inputs(runtime, plan["lookup_system"], names) if names else {"inputs": [], "skipped": []}
        for item in observed["inputs"]:
            item["rom"] = str(PurePosixPath(item["rom"]).relative_to(plan["prefix"]))
        if observed["skipped"] or canonical(observed["inputs"]) != canonical(observations):
            raise ValueError("native payload/outer observations changed")
    selection = api.selection()
    reviews=cover_review_entries(plan.get("cover_refresh_review"),plan["platform_id"])
    if not set(reviews).issubset({entry["id"] for entry in plan["entries"]}):
        raise ValueError("cover review ID escaped plan selection")
    if selection["digest"] != plan["source_digest"] or plan.get("coverage") != coverage(selection):
        raise ValueError("API source snapshot changed")
    source_inputs = {item["record"]["rom"]: item for item in selection["inputs"]}
    source_names = set(source_inputs)
    entries = plan["entries"]
    if len(source_inputs) != len(selection["inputs"]) or len(entries) != len(selection["inputs"]) or len({entry["id"] for entry in entries}) != len(entries) or {entry["record"]["rom"] for entry in entries} != source_names:
        raise ValueError("plan selection count, unique IDs or source membership differs")
    relative(plan["prefix"])
    rows = []
    for entry in plan["entries"]:
        current = runtime.snapshot(entry["id"])
        columns = current["columns"]
        wanted = PurePosixPath(plan["prefix"]) / relative(entry["record"]["rom"])
        if columns["id"] != entry["id"] or columns["platform_id"] != plan["platform_id"] or columns["fs_path"] != str(wanted.parent) or columns["fs_name"] != wanted.name or current["platform_fs_slug"] != plan.get("platform_fs_slug", selection["platform"]):
            raise ValueError("native selection platform/path differs from reviewed source mapping")
        if digest(current) != entry["before_hash"] or digest(entry["before"]) != entry["before_hash"]:
            raise ValueError("native selection changed; prepare a fresh plan")
        if plan.get("update_mode") != "metadata-covers" or "lookup" not in plan:
            runtime.verify_input(current, entry["input"])
        for file in entry["files"]:
            runtime.check_destination(file)
        rows.append((source_inputs[entry["record"]["rom"]], current))
    # Recompute every action from fresh API source and the native baselines before
    # any download/publication/update, rather than trusting edited JSON patches.
    rejected = plan.get("invalid_images", {})
    known = getattr(runtime, "invalid_images", {})
    if not isinstance(rejected, dict) or any(known.get(sha) != reason for sha, reason in rejected.items()):
        raise ValueError("reviewed invalid-image evidence changed; prepare a fresh plan")
    projected = project(selection, rows)
    expected = [plan_entry(runtime, selection, item, row, projected[item["record"]["rom"]], plan.get("repair_igdb_references", False), reviews.get(row["columns"]["id"]), invalid_images=rejected) for item, row in rows]
    if plan.get("update_mode") == "metadata-covers":
        if selection.get("media_mode") != "covers" or any(f.get("role") != "cover" for e in expected for f in e["files"]):
            raise ValueError("update escaped metadata/cover scope")
        if reconcile_identities(expected) != plan["quality"]:
            raise ValueError("identity consensus differs from reviewed selection")
    if canonical(expected) != canonical(plan["entries"]):
        raise ValueError("plan actions differ from source/owner baselines")
    return selection


def verify_after(entry, after):
    before = entry["before"]
    changed = entry["patch"]
    for key, value in changed.items():
        if after["columns"].get(key) != value:
            raise ValueError("native update not confirmed")
    for key, value in before["columns"].items():
        if key in changed or key == "updated_at" or key.startswith("generated_") or key in ("search_titles", "min_age"):
            continue
        if key == "name_sort_key" and "name" in changed:
            continue
        if after["columns"].get(key) != value:
            raise ValueError("native update changed protected field: " + key)
    for key in ("files", "users", "resources", "platform_fs_slug"):
        if before.get(key) != after.get(key):
            raise ValueError("native update changed protected bindings/resources")


def apply_plan(runtime, api, plan, receipt_path, work_root):
    if plan.get("update_mode") == "metadata-covers":
        # Staging is disposable. One complete source/native preflight immediately
        # before publication avoids reading the same collection twice for a delta.
        set_resource_scope(runtime, plan)
    else:
        preflight(runtime, api, plan)
    receipt = {"schema_version": 1, "source_digest": plan["source_digest"], "results": [], "skipped_media": [], "error": None}
    write_new(receipt_path, receipt)
    # Use ordinary files, never hardlinks, in the job's own work volume. All source
    # assets are fetched/verified before the first Resources/DB publication.
    try:
        with tempfile.TemporaryDirectory(prefix="rommeta-stage-", dir=work_root) as staging:
            staged = {}
            download_assets = {f["asset"]["id"]: f["asset"] for e in plan["entries"] for f in e["files"]}
            downloaded_bytes = 0
            invalid = {a["sha256"]: reason for a in download_assets.values() if (reason := getattr(runtime, "invalid_images", {}).get(a["sha256"]))}
            def stage(asset):
                target = Path(staging) / asset["id"]
                api.download(asset, target)
                reason = None
                try:
                    runtime.validate_media(asset, target)
                except InvalidImageError as error:
                    reason = str(error)
                return asset, target, reason

            progress(f"Staging {len(download_assets)} unique files with up to 4 media workers")
            for index, (asset, target, reason) in enumerate(media_map(stage, download_assets.values()), 1):
                if reason:
                    invalid[asset["sha256"]] = reason
                    progress(f'WARNING | Invalid image | {asset["source"]} | {asset["path"]} | {reason}')
                staged[asset["id"]] = target
                downloaded_bytes += asset["size"]
                progress(f'Download {index}/{len(download_assets)} | {asset["kind"]} from {asset["source"]} | {asset["size"]} bytes | {downloaded_bytes}/{plan["unique_download_bytes"]} bytes verified')
            progress(f"Staging complete: {len(staged)} files, {downloaded_bytes} bytes verified")
            covers = [f for e in plan["entries"] for f in e["files"] if f.get("role") == "cover" and f["asset"]["sha256"] not in invalid]
            def prepare(file):
                return (file["id"], file["path"]), runtime.prepare_cover(file, staged[file["asset"]["id"]], staging)
            prepared = dict(media_map(prepare, covers))
            selection = preflight(runtime, api, plan)
            entries = plan["entries"]
            if invalid:
                runtime.invalid_images = dict(getattr(runtime, "invalid_images", {}), **invalid)
                cache = getattr(runtime, "invalid_images_path", None)
                if cache is not None:
                    save_receipt(cache, runtime.invalid_images)
                inputs = {i["record"]["rom"]: i for i in selection["inputs"]}
                rows = [(inputs[e["record"]["rom"]], e["before"]) for e in entries]
                projected = project(selection, rows)
                reviews = cover_review_entries(plan.get("cover_refresh_review"), plan["platform_id"])
                effective = [plan_entry(runtime, selection, item, row, projected[item["record"]["rom"]], plan.get("repair_igdb_references", False), reviews.get(row["columns"]["id"])) for item, row in rows]
                if plan.get("update_mode") == "metadata-covers":
                    reconcile_identities(effective)
                # Decoding failures can only remove media; no replacement or new action
                # may escape the already staged, source/owner-checked original plan.
                if any(any(f not in old["files"] for f in new["files"]) for old, new in zip(entries, effective)):
                    raise ValueError("invalid-media handling added an unstaged action")
                entries = effective
            receipt["skipped_media"] = [dict(o, rom_id=e["id"]) for e in entries for o in e["omissions"] if o["reason"].startswith("invalid image:")]
            save_receipt(receipt_path, receipt)
            progress("Source/native preflight passed; starting reviewed native updates")
            try:
                for row_index, entry in enumerate(entries, 1):
                    progress(f'RomM {row_index}/{len(plan["entries"])} | id {entry["id"]}')
                    result = {"id": entry["id"], "status": "unchanged", "published_files": [], "cover_refresh": entry.get("cover_refresh")}
                    receipt["results"].append(result)
                    if not entry["patch"]:
                        continue
                    if digest(runtime.snapshot(entry["id"])) != entry["before_hash"]:
                        raise ValueError("native row changed before its update")
                    result["status"] = "not-confirmed"
                    save_receipt(receipt_path, receipt)
                    for file in entry["files"]:
                        outputs = prepared[(entry["id"], file["path"])] if file.get("role") == "cover" else [(file, staged[file["asset"]["id"]])]
                        for output, staged_file in outputs:
                            runtime.publish(output, staged_file)
                            result["published_files"].append({"path": output["path"], "sha256": output["asset"]["sha256"], "size": output["asset"]["size"], "actual_source": file["actual_source"]})
                            # Full receipts checkpoint at row boundaries and caught errors,
                            # not once per image in a platform-sized gallery.
                    runtime.update(entry["id"], entry["patch"], entry["before_hash"])
                    after = runtime.snapshot(entry["id"])
                    # Newly referenced Resources are additive; the original resource set must
                    # remain hash-identical, while new files are verified independently below.
                    original_resources = entry["before"].get("resources", {})
                    if any((hash_file(runtime.resource_path(path)) if entry.get("cover_refresh") and path in entry["cover_refresh"]["review"]["resources"] else after.get("resources", {}).get(path)) != value for path, value in original_resources.items()):
                        raise ValueError("original Resources changed")
                    comparable = copy.deepcopy(after)
                    comparable["resources"] = original_resources
                    verify_after(entry, comparable)
                    for file in entry["files"]:
                        outputs = prepared[(entry["id"], file["path"])] if file.get("role") == "cover" else [(file, None)]
                        for output, _ in outputs:
                            runtime.check_published(output)
                    result["status"] = "applied"
                    progress(f'Confirmed id {entry["id"]}: {len(result["published_files"])} resource files')
                    result["after"] = after
                    save_receipt(receipt_path, receipt)
                receipt["cache_refresh"] = runtime.refresh([e["id"] for e in entries if e["patch"]])
            except Exception as error:
                receipt["error"] = str(error) if isinstance(error, ValueError) else type(error).__name__
                save_receipt(receipt_path, receipt)
                raise
        save_receipt(receipt_path, receipt)
    except Exception as error:
        if receipt["error"] is None:
            receipt["error"] = str(error) if isinstance(error, ValueError) else type(error).__name__
            save_receipt(receipt_path, receipt)
        raise
    return receipt


def platform_rows(runtime):
    return sorted(runtime.platforms(), key=lambda p: p["id"])


def inventory_rows(runtime, platform_id):
    fields = ("id", "fs_path", "fs_name", "fs_size_bytes")
    rows = [{f: r[f] for f in fields} for r in runtime.inventory(platform_id)]
    rows.sort(key=lambda r: r["id"])
    if len({r["id"] for r in rows}) != len(rows):
        raise ValueError("duplicate native inventory IDs")
    return rows


def inventory_prefix(rows):
    parts = [PurePosixPath(relative(r["fs_path"])).parts for r in rows]
    prefix = []
    for values in zip(*parts):
        if len(set(values)) != 1:
            break
        prefix.append(values[0])
    if not prefix:
        raise ValueError("native platform has no common ROM directory")
    return str(PurePosixPath(*prefix))


def select_platform(platforms, name):
    key = " ".join(name.split()).casefold()
    matches = [p for p in platforms if any(key == " ".join(str(p.get(f) or "").split()).casefold() for f in ("name", "custom_name", "fs_slug", "slug"))]
    if not key or len(matches) != 1:
        raise ValueError(f'RomM platform "{name}" is ambiguous or not found; run platforms and use its complete unique alias')
    return matches


def cached_observations(paths):
    """Reuse draft input facts from generated plans, never as apply verification."""
    result = {}
    for name in paths or []:
        path = Path(name).resolve()
        parent = json.loads(path.read_text(encoding="utf-8"))
        children = []
        if parent.get("kind") in ("batch", "check", "cover-baseline") and parent.get("status") == "ready":
            for part in parent["parts"]:
                child = (path.parent / relative(part["path"])).resolve()
                if not child.is_relative_to(path.parent) or hash_file(child)["sha256"] != part["sha256"]:
                    raise ValueError("previous plan part changed")
                children.append(json.loads(child.read_text(encoding="utf-8")))
        elif "lookup" in parent:
            children.append(parent)
        else:
            raise ValueError("--reuse-plan requires a completed generated plan/check")
        for child in children:
            observations = {v["rom"]: v for v in child.get("lookup", {}).get("inputs", [])}
            for entry in child["entries"]:
                fact = observations.get(entry["record"]["rom"])
                if fact:
                    old = entry["before"]["columns"]
                    result[entry["id"]] = {"platform": child["platform_id"], "name": old["fs_name"], "size": old["fs_size_bytes"], "fact": fact}
    return result


def lookup_capability(native, capabilities):
    def key(value):
        return " ".join(str(value or "").split()).casefold()
    folder = key(native.get("fs_slug"))
    exact = [p for p in capabilities if key(p["platform"]) == folder]
    # Native provider slugs may be shared (for example NES and Family BASIC).
    # A distinct physical platform needs its own explicit catalog alias.
    aliases = {folder} if folder else {key(native.get(field)) for field in ("slug", "name", "custom_name")} - {""}
    matches = exact or [p for p in capabilities if key(p["platform"]) in aliases or any(key(a) in aliases for a in p.get("aliases", []))]
    if len(matches) > 1:
        raise ValueError("multiple catalog platforms match native system aliases")
    return matches[0] if matches else None


def build_batch(runtime, api, path, platform=None, limit=0, batch_size=100, *, check_only=False, update_mode=False, baseline_only=False, cover_baseline=None, observation_cache=None, match=None):
    if limit < 0 or batch_size < 1:
        raise ValueError("--limit must be non-negative and --batch-size positive")
    capabilities = api.capabilities()
    if capabilities is None:
        raise ValueError("--all requires rom-meta serve (configure it once with the complete metadata catalog)")
    platforms = platform_rows(runtime)
    if platform:
        names = [platform] if isinstance(platform, str) else platform
        selected = []
        seen = set()
        for name in names:
            native = select_platform(platforms, name)[0]
            if native["id"] in seen:
                raise ValueError("same native platform selected more than once")
            seen.add(native["id"])
            selected.append(native)
    else:
        selected = platforms
    matches = [value.casefold() for value in (match or [])]
    if any(not value.strip() for value in matches):
        raise ValueError("--match must contain a nonempty filename substring")
    path = Path(path).resolve()
    parts = path.with_name(path.stem + "-parts")
    if path.exists() or parts.exists():
        raise ValueError("plan/parts already exist; choose a new --out filename")
    result = {"schema_version": 1, "kind": "cover-baseline" if baseline_only else ("check" if check_only else "batch"), "runtime": VERSION, "api": api.origin, "status": "building", "native_platforms_hash": digest(platforms), "capabilities_hash": digest(capabilities), "platforms": [], "parts": [], "entries": 0, "changed": 0, "resource_bytes": 0, "unique_download_bytes": 0, "limit_per_platform": limit, "matches": match or []}
    if baseline_only:
        result["covers"] = {}
    if check_only:
        result["media_content_audited"] = False
    write_new(path, result)
    parts.mkdir(mode=0o700)
    try:
        for number, native in enumerate(selected, 1):
            rows = inventory_rows(runtime, native["id"])
            report = {"id": native["id"], "name": native.get("custom_name") or native["name"], "platform": native["fs_slug"], "inventory_hash": digest(rows), "total": len(rows), "considered": 0, "included": 0, "exact": 0, "game_only": 0, "skipped": [], "status": "skipped", "reason": ""}
            result["platforms"].append(report)
            cap = lookup_capability(native, capabilities["platforms"])
            progress(f'Platform {number}/{len(selected)} | {report["name"]} | {len(rows)} native games')
            if not rows:
                report["reason"] = "no scanned games in RomM"
            elif not cap:
                report["reason"] = "no matching platform in the metadata catalog"
            elif not cap.get("supported"):
                report["reason"] = cap.get("reason") or "no exact lookup reader"
            else:
                prefix = inventory_prefix(rows)
                filtered = [r for r in rows if not matches or any(value in r["fs_name"].casefold() for value in matches)]
                report["filtered_out"] = len(rows) - len(filtered)
                if matches:
                    progress(f'Filename filter | {len(filtered)}/{len(rows)} native games selected; {report["filtered_out"]} deliberately excluded')
                chosen = filtered[:limit] if limit else filtered
                report.update(status="sampled" if len(chosen) < len(rows) else "processed", considered=len(chosen), prefix=prefix)
                part_size = max(1, len(chosen)) if update_mode or baseline_only else batch_size
                for start in range(0, len(chosen), part_size):
                    batch = chosen[start:start + part_size]
                    names = [str(PurePosixPath(relative(r["fs_path"])) / relative(r["fs_name"])) for r in batch]
                    reused = {}
                    for row, name in zip(batch, names):
                        old = (observation_cache or {}).get(row["id"])
                        if old and old["platform"] == native["id"] and old["name"] == row["fs_name"] and old["size"] == row["fs_size_bytes"]:
                            reused[name] = dict(copy.deepcopy(old["fact"]), rom=name)
                    fresh = [name for name in names if name not in reused]
                    if reused:
                        progress(f'Reusing {len(reused)} previous input observations; reading {len(fresh)} others. Changed ROMs are verified again before writes.')
                    observations = inspect_inputs(runtime, cap["system"], fresh) if fresh else {"inputs": [], "skipped": []}
                    by_name = {**reused, **{v["rom"]: v for v in observations["inputs"]}}
                    observations["inputs"] = [by_name[name] for name in names if name in by_name]
                    for item in observations["skipped"]:
                        item["rom"] = str(PurePosixPath(item["rom"]).relative_to(prefix))
                    report["skipped"].extend(observations["skipped"])
                    for item in observations["inputs"]:
                        item["rom"] = str(PurePosixPath(item["rom"]).relative_to(prefix))
                    if not observations["inputs"]:
                        continue
                    query = {"platform": cap["platform"], "inputs": observations["inputs"]}
                    if update_mode or baseline_only:
                        query["media"] = "covers"
                    if update_mode or cap.get("game_binding") or cap.get("name_binding"):
                        query["game_binding"] = True
                    progress(f'Lookup metadata | {cap["system"]} | native games {start + 1}-{start + len(batch)}/{len(chosen)} | {len(query["inputs"])} observed inputs')
                    selection = api.lookup(query)
                    validate_lookup(query, selection)
                    report["skipped"].extend(coverage(selection)["skipped"])
                    if not selection["inputs"]:
                        continue
                    scoped = LookupAPI(api, query, selection)
                    try:
                        child = build_plan(runtime, scoped, native["id"], prefix, update_mode=update_mode, cover_baseline=cover_baseline, native_fs_slug=native["fs_slug"])
                    except ProjectionConflicts as error:
                        # Ambiguous identities never receive guessed metadata or proof.
                        # Re-query the exact reduced scope; all transport/payload errors fail.
                        conflicts = {item["rom"] for item in error.conflicts}
                        included = {item["record"]["rom"] for item in selection["inputs"]}
                        if not conflicts or not conflicts.issubset(included):
                            raise ValueError("projection conflict escaped lookup selection")
                        report["skipped"].extend(error.conflicts)
                        if conflicts == included:
                            continue
                        query = {**query, "inputs": [item for item in query["inputs"] if item["rom"] not in conflicts]}
                        selection = api.lookup(query)
                        validate_lookup(query, selection)
                        child = build_plan(runtime, LookupAPI(api, query, selection), native["id"], prefix, update_mode=update_mode, cover_baseline=cover_baseline, native_fs_slug=native["fs_slug"])
                    if baseline_only:
                        inputs = {item["record"]["rom"]: item for item in selection["inputs"]}
                        for entry in child["entries"]:
                            before = entry["before"]["columns"]
                            asset = selected_cover(selection, inputs[entry["record"]["rom"]])
                            if asset and before.get("path_cover_l") and before.get("path_cover_s") and not before.get("url_cover"):
                                result["covers"][str(entry["id"])] = {"source_sha256": asset["sha256"], "before": {key: before.get(key) for key in ("path_cover_l", "path_cover_s", "url_cover")}}
                        child.update(kind="cover-baseline")
                    if update_mode:
                        report["quality"] = child["quality"]
                        report["provider_conflicts"] = [{"id": e["id"], **c} for e in child["entries"] for c in e["providers"].get("conflicts", [])]
                        progress(f'QUALITY | {report["name"]} | identity fills {child["quality"]["identity_fills"]} | unverified game bindings {len(child["quality"]["unverified_game_bindings"])} | conflicting groups {len(child["quality"]["identity_conflicts"])} | reference-only groups {len(child["quality"]["reference_only"])}')
                        for c in report["provider_conflicts"]:
                            progress(f'PROVIDER SKIP | id {c["id"]} | {c["provider"]} | {c["reason"]}; other metadata retained')
                    child.update(lookup=query, lookup_system=cap["system"])
                    if check_only:
                        child.update(kind="check", media_content_audited=False)
                    child_path = parts / f'{len(result["parts"]) + 1:05d}.json'
                    write_new(child_path, child)
                    result["parts"].append({"path": child_path.relative_to(path.parent).as_posix(), "sha256": hash_file(child_path)["sha256"], "platform_id": native["id"], "ids": [e["id"] for e in child["entries"]]})
                    report["included"] += len(child["entries"])
                    report["exact"] += sum(e["record"].get("matched_by", "hash") == "hash" for e in child["entries"])
                    report["game_only"] += sum(e["record"].get("matched_by") in ("game-name", "catalog-name") for e in child["entries"])
                    result["entries"] += len(child["entries"])
                    result["changed"] += sum(bool(e["patch"]) for e in child["entries"])
                    for key in ("resource_bytes", "unique_download_bytes"):
                        result[key] += child[key]
                    save_receipt(path, result)
            if not report["reason"] and report["considered"] != report["included"] + len(report["skipped"]):
                raise ValueError("batch coverage does not account for every considered native ROM")
            print(f'{report["status"].upper()} | {report["name"]} [{native["fs_slug"]}] | {report["included"]}/{report["total"]} matched (hash {report["exact"]}, game-only {report["game_only"]}) | {len(report["skipped"])} skipped | {report["reason"]}', flush=True)
            for item in report["skipped"]:
                print(f'  SKIP | {item["rom"]} | {item["reason"]}', flush=True)
            save_receipt(path, result)
        result["status"] = "ready"
        save_receipt(path, result)
    except Exception as error:
        result.update(status="failed", error=str(error) if isinstance(error, ValueError) else type(error).__name__)
        save_receipt(path, result)
        raise
    label = "Check" if check_only else "Plan"
    print(f'{label}: {result["entries"]} ROMs; {result["changed"]} changed; {len(result["parts"])} automatic parts; Resources +{result["resource_bytes"]} bytes; download {result["unique_download_bytes"]} bytes. No native writes.', flush=True)
    if check_only:
        print("Read-only metadata/reference check; existing media bytes/existence not audited. Create a fresh plan before apply.", flush=True)
    return result


def check_batch(runtime, api, path, platform=None, limit=0, batch_size=100, match=None):
    # This scope must never weaken a later plan/apply using the same runtime.
    previous = getattr(runtime, "metadata_only", False)
    runtime.metadata_only = True
    try:
        return build_batch(runtime, api, path, platform, limit, batch_size, check_only=True, match=match)
    finally:
        runtime.metadata_only = previous


def update_batch(runtime, api, path, platform=None, baseline_path=None, reuse_plans=None, match=None):
    baseline = None
    if baseline_path:
        saved = json.loads(Path(baseline_path).read_text(encoding="utf-8"))
        if saved.get("kind") != "cover-baseline" or saved.get("status") != "ready" or saved.get("api") != api.origin or saved.get("native_platforms_hash") != digest(platform_rows(runtime)):
            raise ValueError("invalid cover baseline; generate it before changing the profile")
        baseline = dict(saved["covers"], _digest=hash_file(baseline_path)["sha256"])
    previous = getattr(runtime, "resource_scope", None)
    runtime.resource_scope = {}
    try:
        return build_batch(runtime, api, path, platform, update_mode=bool(baseline_path), baseline_only=not baseline_path, cover_baseline=baseline, observation_cache=cached_observations(reuse_plans), match=match)
    finally:
        runtime.resource_scope = previous


def batch_parts(runtime, api, plan, path):
    if plan.get("kind") != "batch" or plan.get("status") != "ready" or plan.get("schema_version") != 1 or plan.get("runtime") != VERSION or plan.get("api") != api.origin:
        raise ValueError("invalid or incomplete batch plan; prepare a fresh plan")
    if digest(platform_rows(runtime)) != plan["native_platforms_hash"] or digest(api.capabilities()) != plan["capabilities_hash"]:
        raise ValueError("native/catalog platforms changed; prepare a fresh plan")
    reports = {r["id"]: r for r in plan["platforms"]}
    if len(reports) != len(plan["platforms"]):
        raise ValueError("duplicate batch platform")
    for platform_id, report in reports.items():
        if digest(inventory_rows(runtime, platform_id)) != report["inventory_hash"]:
            raise ValueError("native inventory changed; prepare a fresh plan")
    base = Path(path).resolve().parent
    children, seen, seen_paths = [], set(), set()
    for part in plan["parts"]:
        name = relative(part["path"])
        target = (base / name).resolve()
        if not target.is_relative_to(base) or target == Path(path).resolve() or target in seen_paths:
            raise ValueError("batch part path/digest differs")
        seen_paths.add(target)
        ids = part["ids"]
        if part["platform_id"] not in reports or not ids or len(set(ids)) != len(ids) or set(ids) & seen:
            raise ValueError("batch part native membership differs")
        seen.update(ids)
        children.append((part, target))
    if len(seen) != plan["entries"]:
        raise ValueError("batch entry total differs")
    return children


def run_batch(runtime, api, plan, path, receipt_path=None):
    children = batch_parts(runtime, api, plan, path)
    receipt = {"schema_version": 1, "kind": "batch", "plan_sha256": hash_file(path)["sha256"], "results": [], "error": None}
    if receipt_path:
        write_new(receipt_path, receipt)
    try:
        for index, (part, target) in enumerate(children, 1):
            child_receipt = Path(receipt_path).with_name(Path(receipt_path).stem + f'-part-{index:05d}.json') if receipt_path else None
            # Materialize one part at a time, including during apply/preflight.
            raw = target.read_bytes()
            if hashlib.sha256(raw).hexdigest() != part["sha256"]:
                raise ValueError("batch part path/digest differs")
            child = json.loads(raw)
            if child["platform_id"] != part["platform_id"] or [e["id"] for e in child["entries"]] != part["ids"] or not child.get("lookup"):
                raise ValueError("batch part native membership differs")
            progress(f'Native batch {index}/{len(children)} | platform {child["platform_id"]} | {len(child["entries"])} ROMs')
            scoped = LookupAPI(api, child["lookup"])
            if receipt_path:
                apply_plan(runtime, scoped, child, child_receipt, Path(receipt_path).resolve().parent)
                receipt["results"].append({"part": part["path"], "receipt": child_receipt.name, "status": "completed"})
                save_receipt(receipt_path, receipt)
            else:
                preflight(runtime, scoped, child)
    except Exception as error:
        if receipt_path:
            receipt["error"] = str(error) if isinstance(error, ValueError) else type(error).__name__
            receipt["failed_part_receipt"] = child_receipt.name
            save_receipt(receipt_path, receipt)
        raise
    return receipt


class NativeRuntime:
    def __init__(self):
        sys.path.insert(0, "/backend")
        from utils import get_version
        if get_version() != VERSION:
            raise ValueError("importer image requires RomM " + VERSION)
        check_native_version()
        from config import LIBRARY_BASE_PATH, RESOURCES_BASE_PATH
        from config.config_manager import MetadataMediaType
        from handler.database import db_rom_handler
        from handler.database.base_handler import sync_session
        from handler.filesystem import fs_resource_handler
        from models.rom import Rom
        from models.platform import Platform
        from sqlalchemy import inspect, select
        from endpoints.responses import rom as schemas
        from pydantic import TypeAdapter
        self.invalid_images_path = Path("/work/rommeta-invalid-images.json")
        self.invalid_images = load_invalid_images(self.invalid_images_path)
        self.roms = db_rom_handler
        self.sessions = sync_session
        self.Rom = Rom
        self.Platform = Platform
        self.inspect = inspect
        self.select = select
        self.library = Path(LIBRARY_BASE_PATH).resolve()
        self.resources = Path(RESOURCES_BASE_PATH).resolve()
        self.fs = fs_resource_handler
        self.media_type = MetadataMediaType
        self.adapters = {"igdb_metadata": TypeAdapter(schemas.RomIGDBMetadata), "ss_metadata": TypeAdapter(schemas.RomSSMetadata), "launchbox_metadata": TypeAdapter(schemas.RomLaunchboxMetadata), "hasheous_metadata": TypeAdapter(schemas.RomHasheousMetadata), "gamelist_metadata": TypeAdapter(schemas.RomGamelistMetadata)}
        required = FIELDS | {"slug", "locked_fields", "path_screenshots", "gamelist_metadata", "path_cover_l", "path_cover_s"}
        if not required <= set(Rom.__table__.columns.keys()):
            raise ValueError("native model schema differs")

    def row(self, value):
        def safe(v):
            if hasattr(v, "isoformat"):
                return v.isoformat()
            return v
        return {column.key: safe(getattr(value, column.key)) for column in self.inspect(type(value)).column_attrs}

    def platforms(self):
        with self.sessions() as session:
            fields = ("id", "name", "custom_name", "slug", "fs_slug")
            return [dict(zip(fields, row)) for row in session.execute(self.select(*(getattr(self.Platform, f) for f in fields))).all()]

    def inventory(self, platform_id):
        with self.sessions() as session:
            fields = ("id", "fs_path", "fs_name", "fs_size_bytes")
            return [dict(zip(fields, row)) for row in session.execute(self.select(*(getattr(self.Rom, f) for f in fields)).where(self.Rom.platform_id == platform_id)).all()]

    def find(self, platform_id, fs_path, fs_name):
        with self.sessions() as session:
            ids = session.scalars(self.select(self.Rom.id).where(self.Rom.platform_id == platform_id, self.Rom.fs_path == fs_path, self.Rom.fs_name == fs_name)).all()
            if len(ids) != 1:
                raise ValueError("native inventory has no unique exact path")
            return self.snapshot(ids[0])

    def snapshot(self, rom_id):
        with self.sessions() as session:
            value = self.roms.get_rom(rom_id, session=session)
            if value is None:
                raise ValueError("native ROM missing")
            columns = self.row(value)
            result = {"columns": columns, "files": sorted((self.row(f) for f in value.files), key=lambda f: f["id"]), "users": sorted((self.row(u) for u in value.rom_users), key=lambda u: u["id"]), "platform_fs_slug": value.platform.fs_slug}
            result["resources"] = self.resource_snapshot(columns)
            return result

    def resource_snapshot(self, columns):
        if getattr(self, "metadata_only", False):
            return {}
        scope = getattr(self, "resource_scope", None)
        if scope is not None:
            return {path: hash_file(self.resource_path(path)) for path in scope.get(str(columns["id"]), [])}
        paths = {columns.get("path_cover_l"), columns.get("path_cover_s"), columns.get("path_manual")}
        paths.update(columns.get("path_screenshots") or [])
        for key in ("ss_metadata", "launchbox_metadata", "gamelist_metadata"):
            paths.update(v for k, v in (columns.get(key) or {}).items() if k.endswith("_path") and isinstance(v, str))
        result = {}
        for path in paths:
            if path:
                target = self.resource_path(path)
                result[path] = hash_file(target) if target.is_file() else {"missing": True}
        return result

    def resource_path(self, path):
        relative(path)
        target = (self.resources / path).resolve()
        if not target.is_relative_to(self.resources):
            raise ValueError("Resources path escaped native root")
        return target

    def verify_input(self, snapshot, expected):
        columns = snapshot["columns"]
        target = (self.library / relative(columns["fs_path"]) / relative(columns["fs_name"])).resolve()
        if not target.is_relative_to(self.library):
            raise ValueError("native ROM escaped library")
        if expected.get("catalog"):
            name = str(PurePosixPath(relative(columns["fs_path"])) / relative(columns["fs_name"]))
            observed = inspect_inputs(self, "catalog", [name])
            if observed["skipped"] or len(observed["inputs"]) != 1:
                raise ValueError("native catalog observation is no longer available")
            item = observed["inputs"][0]
            if item.get("catalog") != expected["catalog"] or item["outer"]["size"] != expected["size"]:
                raise ValueError("native file stats or folder membership changed")
        elif expected.get("disc"):
            name = str(PurePosixPath(relative(columns["fs_path"])) / relative(columns["fs_name"]))
            observed = inspect_inputs(self, expected["system"], [name])
            if observed["skipped"] or len(observed["inputs"]) != 1:
                raise ValueError("native CHD observation is no longer available")
            item = observed["inputs"][0]
            if item.get("disc") != expected["disc"] or item["outer"]["size"] != expected["size"]:
                raise ValueError("native CHD headers, size, mtime or membership changed")
            native_disc_files(snapshot, name, expected["disc"])
        elif not target.is_file() or hash_file(target) != expected:
            raise ValueError("native ROM bytes differ from reviewed API input")

    def media_path(self, platform, rom, role, asset):
        suffix = Path(asset["path"]).suffix.lower()
        if role == "manual":
            if suffix not in (".pdf", ".md", ".txt"):
                raise ValueError("native manual requires PDF/MD/TXT")
            return str(PurePosixPath(self.fs.get_media_resources_path(platform, rom, self.media_type("manual"))) / (str(rom) + suffix))
        if suffix not in (".png", ".jpg", ".jpeg", ".webp", ".gif", ".mp4", ".webm", ".mkv", ".avi", ".mov", ".wmv", ".m4v"):
            raise ValueError("unsupported native media extension")
        folder = f"roms/{platform}/{rom}/screenshots" if role == "screenshots" else self.fs.get_media_resources_path(platform, rom, self.media_type(role))
        return str(PurePosixPath(folder) / ("rommeta-" + asset["sha256"] + suffix))

    def validate_patch(self, patch):
        allowed = FIELDS | {"slug", "path_screenshots", "gamelist_metadata", "path_manual", "path_cover_l", "path_cover_s", "locked_fields"}
        if not set(patch) <= allowed:
            raise ValueError("unreviewed native update field")
        for key, adapter in self.adapters.items():
            if key in patch and adapter.validate_python(patch[key], strict=True) != patch[key]:
                raise ValueError("native partial schema dropped or changed metadata fields")

    def cover_paths(self, platform, rom, asset):
        suffix = Path(asset["path"]).suffix.lower()
        if suffix not in (".png", ".jpg", ".jpeg", ".webp"):
            raise ValueError("native cover requires PNG/JPEG/WebP")
        folder = f"roms/{platform}/{rom}/cover"
        return f"{folder}/big.png", f"{folder}/small.png"

    def prepare_cover(self, file, source, staging):
        import asyncio
        from handler.filesystem.resources_handler import FSResourcesHandler
        with self.sessions() as session:
            rom = self.roms.get_rom(file["id"], session=session)
            native = FSResourcesHandler()
            private = Path(staging) / ("cover-" + str(file["id"]))
            private.mkdir()
            # A fresh job-local handler instance uses its own staging root. No global
            # application setting or source file is changed.
            native.base_path = private.resolve()
            large, small = asyncio.run(native.store_artwork(rom, normalized_cover(source), "png"))
        expected = list(self.cover_paths(rom.platform_id, rom.id, file["asset"]))
        if [large, small] != expected:
            raise ValueError("native cover derivative paths differ from reviewed layout")
        destinations = dict(zip(expected, file["paths"]))
        if (file.get("refresh") or file.get("additive")) and file["paths"] != list(refreshed_cover_paths({"platform_id": rom.platform_id, "id": rom.id}, file["asset"])):
            raise ValueError("unreviewed additive cover layout")
        outputs = []
        for path in sorted(private.rglob("*")):
            if path.is_file():
                output = copy.deepcopy(file)
                output.pop("role", None)
                native_path=path.relative_to(private).as_posix()
                output["path"] = destinations.get(native_path, str(PurePosixPath(file["path"]).parent / Path(native_path).name))
                output["asset"].update(hash_file(path))
                self.validate_media(output["asset"], path)
                self.check_destination(output)
                outputs.append((output, path))
        if not set(file["paths"]) <= {output["path"] for output, _ in outputs}:
            raise ValueError("native cover derivatives missing")
        return outputs

    def check_destination(self, file):
        target = self.resource_path(file["path"])
        if file.get("role") == "cover":
            if file.get("additive") or file.get("refresh"):
                if target.parent.name != "rommeta-" + file["asset"]["sha256"]:
                    raise ValueError("unreviewed additive cover directory")
                # Generated PNG hashes are checked in private staging before publication.
                return
            if target.parent.exists() and any(target.parent.iterdir()):
                raise ValueError("unbound cover directory is not empty; review prior receipt")
            return
        if file.get("role") == "manual" and target.parent.exists():
            if any(path != target for path in target.parent.iterdir()):
                raise ValueError("unbound manual directory contains owner files")
        if target.exists() and (not target.is_file() or hash_file(target) != {"size": file["asset"]["size"], "sha256": file["asset"]["sha256"]}):
            raise ValueError("destination contains different owner data")

    def validate_media(self, asset, staged):
        if Path(asset["path"]).suffix.lower() in (".png", ".jpg", ".jpeg", ".webp", ".gif"):
            from PIL import Image
            try:
                with Image.open(staged) as image:
                    image.verify()
                with Image.open(staged) as image:
                    image.load()
            except (OSError, SyntaxError, ValueError, Image.DecompressionBombError) as error:
                if isinstance(error, OSError) and error.errno is not None:
                    raise  # Filesystem/access failures are not corrupt source images.
                raise InvalidImageError(type(error).__name__) from error

    def publish(self, file, staged):
        self.check_destination(file)
        target = self.resource_path(file["path"])
        if target.exists():
            return
        target.parent.mkdir(parents=True, exist_ok=True)
        descriptor, temporary_name = tempfile.mkstemp(prefix=".rommeta-", dir=target.parent)
        temporary = Path(temporary_name)
        try:
            with os.fdopen(descriptor, "wb") as stream, Path(staged).open("rb") as source:
                shutil.copyfileobj(source, stream, 1 << 20)
                stream.flush()
                os.fsync(stream.fileno())
            if os.name == "nt":
                # Windows rename refuses an existing target; production is the pinned Linux image.
                temporary.rename(target)
            else:
                import ctypes
                import errno
                libc = ctypes.CDLL(None, use_errno=True)
                rename = libc.renameat2
                rename.argtypes = [ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_uint]
                rename.restype = ctypes.c_int
                if rename(-100, os.fsencode(temporary), -100, os.fsencode(target), 1) != 0:
                    code = ctypes.get_errno()
                    if code != errno.EEXIST:
                        raise OSError(code, "atomic no-replace media publication failed")
                    self.check_destination(file)
        finally:
            temporary.unlink(missing_ok=True)
        self.check_published(file)

    def check_published(self, file):
        target = self.resource_path(file["path"])
        if hash_file(target) != {"size": file["asset"]["size"], "sha256": file["asset"]["sha256"]}:
            raise ValueError("published media differs")

    def update(self, rom_id, patch, expected_hash):
        self.validate_patch(patch)
        with self.sessions.begin() as session:
            session.execute(self.select(self.Rom.id).where(self.Rom.id == rom_id).with_for_update())
            # The whole snapshot was checked just before filesystem publication. Recheck
            # raw fields under the native row lock; files are never edited by this job.
            value = self.roms.get_rom(rom_id, session=session)
            current = {"columns": self.row(value), "files": sorted((self.row(f) for f in value.files), key=lambda f: f["id"]), "users": sorted((self.row(u) for u in value.rom_users), key=lambda u: u["id"]), "platform_fs_slug": value.platform.fs_slug}
            current["resources"] = self.resource_snapshot(current["columns"])
            if digest(current) != expected_hash:
                raise ValueError("native row changed under update lock")
            self.roms.update_rom(rom_id, copy.deepcopy(patch), session=session)

    def refresh(self, ids):
        if not ids:
            return "unchanged"
        from handler.database import db_collection_handler
        try:
            self.roms.invalidate_filter_values_cache()
            db_collection_handler.refresh_smart_collections_for_roms(ids)
            return "confirmed"
        except Exception:
            # The writes already committed. A cache failure is not a database rollback.
            return "warning: native cache/smart-collection refresh failed; writes committed"


def expected_ids(value):
    if value is None:
        return None
    match = re.fullmatch(r"([1-9][0-9]*):([1-9][0-9]*)", value)
    if not match or int(match[2]) < int(match[1]):
        raise ValueError("expect-ids requires an inclusive start:end range")
    return range(int(match[1]), int(match[2]) + 1)


def print_platforms(runtime):
    platforms = runtime.platforms()
    if not platforms:
        print("No platforms found in RomM. Scan your library first.")
        return
    print("Platform name | Other accepted names")
    for platform in sorted(platforms, key=lambda p: ((p.get("custom_name") or p["name"]).casefold(), p["fs_slug"], p["id"])):
        name = platform.get("custom_name") or platform["name"]
        aliases = list(dict.fromkeys(value for value in (platform["name"], platform["fs_slug"], platform["slug"]) if value and value.casefold() != name.casefold()))
        print(f'{name} | {"; ".join(aliases)}')
    print(f"{len(platforms)} platforms from RomM. Use a complete name or alias with --platform.")
    print("Metadata availability is checked separately during planning.")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    sub.add_parser("platforms", help="List actual RomM platform names and aliases; no RomMeta API required")
    plan = sub.add_parser("plan")
    plan.add_argument("--api", required=True)
    target = plan.add_mutually_exclusive_group(required=True)
    target.add_argument("--all", action="store_true", help="Discover all native RomM platforms/games; skip unsupported with a report")
    target.add_argument("--platform", action="append", help="Complete RomM platform name; repeat to select several systems (native IDs/paths are automatic)")
    target.add_argument("--platform-id", type=int, help="Legacy explicit native platform ID; requires --romm-rom-root")
    plan.add_argument("--romm-rom-root", help="Legacy explicit native ROM folder; use --platform for automatic detection")
    plan.add_argument("--match", action="append", help="Catalog mode: filename substring; repeat for OR, before --limit")
    plan.add_argument("--limit", type=int, default=0, help="Catalog mode: sample at most this many native games per platform (0: all)")
    plan.add_argument("--batch-size", type=int, default=100, help="Native games per automatic lookup part; no total collection ceiling")
    plan.add_argument("--expect-ids")
    plan.add_argument("--repair-igdb-references", action="store_true", help="Correct only source-reviewed foreign IGDB IDs with exact old ID and blank unlocked own fields")
    plan.add_argument("--refresh-covers", help="Explicit old source/path/SHA cover review; ordinary fill-only remains")
    plan.add_argument("--export-cover-review", help="Read-only current cover baseline for human ownership review before a policy change")
    plan.add_argument("--out", required=True)
    check = sub.add_parser("check", help="Read-only repeat check of metadata/references; does not read existing media bytes")
    check.add_argument("--api", required=True)
    check_target = check.add_mutually_exclusive_group(required=True)
    check_target.add_argument("--all", action="store_true")
    check_target.add_argument("--platform", action="append", help="Complete native platform name; repeat to select several")
    check.add_argument("--match", action="append", help="Filename substring; repeat for OR, before --limit")
    check.add_argument("--limit", type=int, default=0, help="Sample per platform (0: all)")
    check.add_argument("--batch-size", type=int, default=100)
    check.add_argument("--out", required=True, help="New report filename; cannot be used for apply")
    for command in ("cover-baseline", "refresh"):
        update = sub.add_parser(command, help="Metadata/cover update only; existing other media is not read or imported")
        update.add_argument("--api", required=True)
        update_target = update.add_mutually_exclusive_group(required=True)
        update_target.add_argument("--all", action="store_true")
        update_target.add_argument("--platform", action="append")
        if command == "refresh":
            update.add_argument("--cover-baseline", required=True, help="Generated baseline from before a profile change")
            update.add_argument("--reuse-plan", action="append", help="Reuse prior generated input observations for planning; changed files are verified before writes")
        update.add_argument("--match", action="append", help="Filename substring; repeat for OR")
        update.add_argument("--out", required=True)
    apply = sub.add_parser("apply")
    apply.add_argument("--plan", required=True)
    apply.add_argument("--apply", action="store_true")
    apply.add_argument("--out")
    args = parser.parse_args(argv)
    if args.command == "plan" and (((args.platform is not None or args.all) and args.romm_rom_root is not None) or (args.platform_id is not None and args.romm_rom_root is None)):
        parser.error("use --platform by name, or the legacy --platform-id / --romm-rom-root pair")
    runtime = NativeRuntime()
    if args.command == "platforms":
        print_platforms(runtime)
        return
    if args.command == "check":
        api = API(args.api, os.environ.get("ROMMETA_API_TOKEN", ""))
        check_batch(runtime, api, args.out, args.platform, args.limit, args.batch_size, match=args.match)
        return
    if args.command in ("cover-baseline", "refresh"):
        api = API(args.api, os.environ.get("ROMMETA_API_TOKEN", ""))
        update_batch(runtime, api, args.out, args.platform, getattr(args, "cover_baseline", None), getattr(args, "reuse_plan", None), match=args.match)
        return
    if args.command == "plan":
        api = API(args.api, os.environ.get("ROMMETA_API_TOKEN", ""))
        caps = api.capabilities() if hasattr(api, "capabilities") else None
        if args.all or caps is not None and args.platform is not None:
            if args.expect_ids or args.repair_igdb_references or args.refresh_covers or args.export_cover_review:
                parser.error("batch lookup uses ordinary fill-only imports; scoped repair/review flags require fixed-selection mode")
            build_batch(runtime, api, args.out, args.platform, args.limit, args.batch_size, match=args.match)
            return
        if args.platform is not None and len(args.platform) != 1:
            parser.error("multiple --platform values require rom-meta serve")
        if args.limit or args.batch_size != 100 or args.match:
            parser.error("--limit/--batch-size/--match require catalog API mode")
        if args.export_cover_review and args.refresh_covers:
            parser.error("export current baseline before using --refresh-covers")
        cover_review = json.loads(Path(args.refresh_covers).read_text(encoding="utf-8")) if args.refresh_covers else None
        value = build_plan(runtime, api, args.platform_id, args.romm_rom_root, expected_ids(args.expect_ids), repair_igdb_references=args.repair_igdb_references, cover_review=cover_review, platform=args.platform[0] if args.platform else None)
        write_new(args.out, value)
        if args.export_cover_review:
            review = export_cover_review(value, api.selection(), args.out)
            write_new(args.export_cover_review, review)
            print(f'Cover baseline: {len(review["entries"])} entries; this is NOT proof of adapter ownership. Review covers/evidence before authorizing refresh.')
        print(f'Coverage: {value["coverage"]["considered"]} considered; {value["coverage"]["included"]} exact inputs; {len(value["coverage"]["skipped"])} skipped')
        for skipped in value["coverage"]["skipped"]:
            print(f'SKIP | {skipped["rom"]} | {skipped["status"]} | {skipped["reason"]}')
        for entry in value["entries"]:
            print(f'{entry["id"]} | {Path(entry["record"]["game"]).name} | fields: {", ".join(entry["patch"])} | {len(entry["files"])} media | {sum(f["asset"]["size"] for f in entry["files"])} bytes')
        print(f'Plan: {len(value["entries"])} ROMs; {sum(bool(e["patch"]) for e in value["entries"])} changed; Resources +{value["resource_bytes"]} bytes; unique download {value["unique_download_bytes"]} bytes')
        for entry in value["entries"]:
            if entry.get("cover_refresh"):
                r=entry["cover_refresh"]
                print(f'REVIEWED COVER | {entry["id"]} | {r["review"]["source_sha256"]} -> {r["new_source_sha256"]} | {r["source"]} | {r["style"]} | {r["style_evidence"]}')
        repairs = [e for e in value["entries"] if e.get("igdb_reference_repair")]
        for entry in repairs:
            r = entry["igdb_reference_repair"]
            print(f'REVIEWED IGDB | {entry["id"]} | {r["before_id"]} -> {r["after_id"]} | {r["review"]["reason"]}')
        print("Existing owner data/covers/locks preserved. Source IDs are game-level; verification remains payload-specific. Unsupported prose/roles stay in provenance.")
    else:
        value = json.loads(Path(args.plan).read_text(encoding="utf-8"))
        if value.get("kind") in ("check", "cover-baseline"):
            parser.error("read-only report cannot be applied; create a fresh plan or refresh")
        api = API(value["api"], os.environ.get("ROMMETA_API_TOKEN", ""))
        if value.get("kind") == "batch":
            if args.apply and not args.out:
                parser.error("--apply requires a new --out receipt")
            run_batch(runtime, api, value, args.plan, args.out if args.apply else None)
            print("Batch apply complete; inspect receipts. Create a fresh plan to continue after a partial failure." if args.apply else "Batch native/API/source preflight passed; no native writes.")
            return
        if not args.apply:
            preflight(runtime, api, value)
            print("Whole-selection native/API/source preflight passed; no writes.")
        else:
            if not args.out:
                parser.error("--apply requires a new --out receipt")
            apply_plan(runtime, api, value, args.out, Path(args.out).resolve().parent)
            print("Apply complete; review receipt and create a fresh same-scope no-op plan.")


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        # Never render an authenticated URL, environment or native connection exception.
        print("RomMeta native import failed: " + (str(error) if isinstance(error, ValueError) else type(error).__name__), file=sys.stderr)
        sys.exit(2)
