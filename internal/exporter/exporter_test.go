package exporter

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func put(t *testing.T, path string, contents []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatal(err)
	}
}
func putZip(t *testing.T, path string, members map[string][]byte) {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name, payload := range members {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(payload); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	put(t, path, b.Bytes())
}
func fixture(t *testing.T) (Options, string, []byte) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "catalog")
	roms := filepath.Join(base, "roms")
	if err := os.MkdirAll(roms, 0o755); err != nil {
		t.Fatal(err)
	}
	gamePath := filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Example")
	put(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "_platform.yaml"), []byte("name: Nintendo - Nintendo 64\nrom_layout: BigEndian\n"))
	put(t, filepath.Join(gamePath, "game.yaml"), []byte(`title: "Example & Friends"
default_release: "Example (USA)"
releases:
  "Example (USA)":
    title: Example
    regions: [USA]
    authority:
      entry_name: "Example (USA)"
  "Example (Europe)":
    title: Example
    regions: [Europe]
sources:
  launchbox:
    matches:
      - releases: ["Example (USA)", "Example (Europe)"]
        developer: First developer
        publisher: First publisher
        release_date: '2000-10-17'
        max_players: '4'
        genres: [Action, Adventure]
        descriptions:
          en: "English & <safe> description"
  screenscraper:
    matches:
      - release: "Example (USA)"
        publisher: Second publisher
        descriptions:
          ru: "Русское описание"
          fr: Description française
  igdb:
    matches:
      - release: "Example (Europe)"
        descriptions:
          ru: "Wrong edition must not win"
`))
	payload := []byte("synthetic N64 fixture bytes, not a copyrighted ROM")
	fingerprint := fmt.Sprintf("items:\n  - release: Example (USA)\n    source:\n      entry_name: Example (USA)\n    file:\n      name: Example (USA).z64\n      size: %d\n      sha1: %x\n", len(payload), sha1.Sum(payload))
	put(t, filepath.Join(gamePath, "fingerprints.yaml"), []byte(fingerprint))
	var bitmap bytes.Buffer
	if err := png.Encode(&bitmap, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"box-front.usa.launchbox.png", "box-front.europe.launchbox.png", "box-front.world.screenscraper.png", "screenshot.world.launchbox.png", "logo.usa.launchbox.png"} {
		put(t, filepath.Join(gamePath, "media", name), bitmap.Bytes())
	}
	put(t, filepath.Join(gamePath, "media", "gameplay-video.usa.emumovies.mp4"), []byte("synthetic media stream"))
	// Credentials and disposable caches are deliberately unusable.
	put(t, filepath.Join(root, "config", "secrets.yaml"), []byte("[ invalid credentials YAML"))
	return Options{DataRoot: root, ROMRoot: roms, Platform: "n64", Out: filepath.Join(base, "output")}, gamePath, payload
}
func profile(t *testing.T, options *Options, body string) {
	t.Helper()
	options.ProfilePath = filepath.Join(filepath.Dir(options.DataRoot), "profile.yaml")
	put(t, options.ProfilePath, []byte(body))
}
func treeHashes(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			result[rel] = fmt.Sprintf("%x", sha256.Sum256(data))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestOfflineZIPExportPrioritiesPreservationAndDeterminism(t *testing.T) {
	options, gamePath, payload := fixture(t)
	putZip(t, filepath.Join(options.ROMRoot, "nested", "Renamed & File.zip"), map[string][]byte{"unrelated-name.z64": payload, "readme.txt": []byte("sidecar")})
	profile(t, &options, `schema_version: 1
target: es-de
selection:
  description:
    languages: [ru, fr, en]
    sources: [launchbox, screenscraper, igdb]
  fields:
    publisher:
      sources: [screenscraper, launchbox]
  media:
    cover:
      sources: [launchbox, screenscraper]
    screenshot:
      sources: [launchbox]
`)
	beforeCatalog, beforeROM := treeHashes(t, options.DataRoot), treeHashes(t, options.ROMRoot)
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if plan.HashMatches != 1 || plan.Matched != 1 || plan.Records[0].Member != "unrelated-name.z64" {
		t.Fatalf("binding: %+v", plan)
	}
	record := plan.Records[0]
	if record.Fields["description"].Value != "Русское описание" || record.Fields["description"].Source != "screenscraper" {
		t.Fatalf("language-first: %+v", record.Fields)
	}
	if record.Fields["publisher"].Value != "Second publisher" {
		t.Fatal("per-field source list ignored")
	}
	if !strings.HasSuffix(record.Media["cover"].SourcePath, "box-front.usa.launchbox.png") {
		t.Fatalf("region selection: %+v", record.Media)
	}
	if record.Media["cover"].OutputPath != "downloaded_media/n64/covers/nested/Renamed & File.png" {
		t.Fatal(record.Media["cover"].OutputPath)
	}
	if _, err := os.Stat(options.Out); !os.IsNotExist(err) {
		t.Fatal("planning wrote output")
	}
	if err := Apply(plan, nil); err != nil {
		t.Fatal(err)
	}
	xmlBytes, err := os.ReadFile(filepath.Join(options.Out, "gamelists", "n64", "gamelist.xml"))
	if err != nil {
		t.Fatal(err)
	}
	var list xmlList
	if err := xml.Unmarshal(xmlBytes, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Games) != 1 || list.Games[0].Path != "./nested/Renamed & File.zip" || list.Games[0].Date != "20001017T000000" {
		t.Fatalf("XML: %+v", list)
	}
	if list.Games[0].Description != "Русское описание" {
		t.Fatal(list.Games[0].Description)
	}
	manifestBytes, _ := os.ReadFile(filepath.Join(options.Out, "manifest.json"))
	var manifest Plan
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Records[0].Media["cover"].SHA256 == "" {
		t.Fatal("missing copied-asset digest")
	}
	preview, _ := os.ReadFile(filepath.Join(options.Out, "preview.html"))
	if !bytes.Contains(preview, []byte("Example &amp; Friends")) {
		t.Fatal("HTML title not escaped")
	}
	firstOutput := treeHashes(t, options.Out)
	options.Out = filepath.Join(filepath.Dir(options.Out), "second-output")
	again, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(again, nil); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(firstOutput) != fmt.Sprint(treeHashes(t, options.Out)) {
		t.Fatal("repeat output differs")
	}
	if fmt.Sprint(beforeCatalog) != fmt.Sprint(treeHashes(t, options.DataRoot)) || fmt.Sprint(beforeROM) != fmt.Sprint(treeHashes(t, options.ROMRoot)) {
		t.Fatal("input trees changed")
	}
	if _, err := os.Stat(filepath.Join(options.DataRoot, ".rommeta")); !os.IsNotExist(err) {
		t.Fatal("runtime cache was created")
	}
	if _, err := os.Stat(gamePath); err != nil {
		t.Fatal(err)
	}
}

func TestDescriptionSourceFirstAndStrictLanguageLists(t *testing.T) {
	options, _, payload := fixture(t)
	put(t, filepath.Join(options.ROMRoot, "renamed.z64"), payload)
	profile(t, &options, "selection:\n  description:\n    order: [source, language]\n    languages: [ru, fr, en]\n    sources: [launchbox, screenscraper]\n  media: {}\n")
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Records[0].Fields["description"].Language != "en" {
		t.Fatal("source-first ignored")
	}
	profile(t, &options, "selection:\n  description:\n    languages: [de]\n  media: {}\n")
	plan, err = Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := plan.Records[0].Fields["description"]; exists {
		t.Fatal("unlisted language selected")
	}
	if plan.MediaFiles != 0 {
		t.Fatal("empty media mapping did not disable assets")
	}
}

func TestNameFallbackDoesNotInventGameOnlyRelease(t *testing.T) {
	options, _, _ := fixture(t)
	put(t, filepath.Join(options.ROMRoot, "Example (USA).z64"), []byte("different payload"))
	put(t, filepath.Join(options.ROMRoot, "Example & Friends.z64"), []byte("another payload"))
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if plan.NameMatches != 1 || plan.GameOnly != 1 || plan.HashMatches != 0 {
		t.Fatalf("fallback counters: %+v", plan)
	}
	for _, record := range plan.Records {
		if record.MatchedBy == "game-name" {
			if record.Release != "" || record.MetadataRelease != "Example (USA)" {
				t.Fatalf("invented release: %+v", record)
			}
		} else if record.MatchedBy != "name" || record.Release != "Example (USA)" {
			t.Fatalf("name binding: %+v", record)
		}
	}
}

func TestHashAmbiguityIsNotOverriddenByFilename(t *testing.T) {
	options, gamePath, payload := fixture(t)
	put(t, filepath.Join(options.ROMRoot, "Example (USA).z64"), payload)
	fp := fmt.Sprintf("items:\n  - release: Example (USA)\n    file: {name: Example (USA).z64, size: %d, sha1: '%x'}\n  - release: Example (Europe)\n    file: {name: Example (Europe).z64, size: %d, sha1: '%x'}\n", len(payload), sha1.Sum(payload), len(payload), sha1.Sum(payload))
	put(t, filepath.Join(gamePath, "fingerprints.yaml"), []byte(fp))
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Ambiguous != 1 || plan.Matched != 0 || plan.Records[0].Release != "" {
		t.Fatal("hash ambiguity auto-selected by name")
	}
}

func TestHashlessCatalogNameAndConflictingZIPNames(t *testing.T) {
	options, gamePath, _ := fixture(t)
	if err := os.Remove(filepath.Join(gamePath, "fingerprints.yaml")); err != nil {
		t.Fatal(err)
	}
	putZip(t, filepath.Join(options.ROMRoot, "Example (USA).zip"), map[string][]byte{"Example (Europe).z64": []byte("synthetic")})
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Ambiguous != 1 || len(plan.Records[0].Candidates) != 2 {
		t.Fatal("conflicting archive/member names not held")
	}
}

func TestIncompleteZIPAndUnknownFilesAreReported(t *testing.T) {
	options, _, payload := fixture(t)
	putZip(t, filepath.Join(options.ROMRoot, "multiple.zip"), map[string][]byte{"one.z64": payload, "two.z64": payload})
	put(t, filepath.Join(options.ROMRoot, "unknown.z64"), []byte("unknown"))
	put(t, filepath.Join(options.ROMRoot, "notes.txt"), []byte("not a ROM"))
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Unsupported != 1 || plan.Unmatched != 1 || len(plan.Records) != 2 {
		t.Fatalf("counts: %+v", plan)
	}
	body, err := RenderGamelist(plan)
	if err != nil {
		t.Fatal(err)
	}
	var list xmlList
	if err := xml.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Games) != 1 || list.Games[0].Name != "unknown" {
		t.Fatal("unknown not retained as filename-only entry")
	}
}

func TestChangedROMAndExistingOutputArePreserved(t *testing.T) {
	options, _, payload := fixture(t)
	rom := filepath.Join(options.ROMRoot, "renamed.z64")
	put(t, rom, payload)
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	changed := append([]byte(nil), payload...)
	changed[0] = 'X'
	put(t, rom, changed)
	if err := Apply(plan, nil); err == nil || !strings.Contains(err.Error(), "changed since planning") {
		t.Fatalf("changed input accepted: %v", err)
	}
	if _, err := os.Stat(options.Out); !os.IsNotExist(err) {
		t.Fatal("failed export published")
	}
	put(t, rom, payload)
	plan, err = Build(options)
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(options.Out, "keep.txt"), []byte("existing user file"))
	if err := Apply(plan, nil); err == nil {
		t.Fatal("existing output replaced")
	}
	contents, _ := os.ReadFile(filepath.Join(options.Out, "keep.txt"))
	if string(contents) != "existing user file" {
		t.Fatal("existing file changed")
	}
}

func TestOutputOverlapAndMediaCollisions(t *testing.T) {
	options, _, payload := fixture(t)
	put(t, filepath.Join(options.ROMRoot, "same.z64"), payload)
	options.Out = filepath.Join(options.DataRoot, "platforms", "new")
	if _, err := Build(options); err == nil {
		t.Fatal("catalog output overlap accepted")
	}
	options.Out = filepath.Join(filepath.Dir(options.DataRoot), "output")
	putZip(t, filepath.Join(options.ROMRoot, "same.zip"), map[string][]byte{"one.z64": payload})
	if _, err := Build(options); err == nil || !strings.Contains(err.Error(), "collision") {
		t.Fatalf("media collision accepted: %v", err)
	}
}

func TestMissingAssetsFallBackAndExplicitMetadataWins(t *testing.T) {
	options, gamePath, payload := fixture(t)
	put(t, filepath.Join(options.ROMRoot, "renamed.z64"), payload)
	if err := os.Remove(filepath.Join(gamePath, "media", "box-front.usa.launchbox.png")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(gamePath, "game.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(gamePath, "game.yaml"), append([]byte("publisher: Local publisher\n"), raw...))
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Records[0].Media["cover"].Source != "screenscraper" {
		t.Fatal("missing or wrong-region image blocked fallback")
	}
	if plan.Records[0].Fields["publisher"].Value != "Local publisher" {
		t.Fatal("explicit override ignored")
	}
}

func TestProfileUnknownCriteriaAreRejected(t *testing.T) {
	options, _, payload := fixture(t)
	put(t, filepath.Join(options.ROMRoot, "one.z64"), payload)
	profile(t, &options, "selection:\n  description:\n    order: [random]\n")
	if _, err := Build(options); err == nil {
		t.Fatal("unknown criterion accepted")
	}
	profile(t, &options, "selection:\n  description:\n    langauges: [en]\n")
	if _, err := Build(options); err == nil {
		t.Fatal("unknown profile key accepted")
	}
}

func TestExportIncludesCanonicalGameplayVideo(t *testing.T) {
	options, _, payload := fixture(t)
	put(t, filepath.Join(options.ROMRoot, "video-test.z64"), payload)
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	video, ok := plan.Records[0].Media["video"]
	if !ok || !strings.Contains(video.SourcePath, "gameplay-video.") {
		t.Fatalf("canonical video missing: %+v", plan.Records[0].Media)
	}
	if !strings.HasSuffix(video.OutputPath, "videos/video-test.mp4") {
		t.Fatal(video.OutputPath)
	}
}

func TestIrrelevantProfileSettingsAreRejected(t *testing.T) {
	options, _, payload := fixture(t)
	put(t, filepath.Join(options.ROMRoot, "one.z64"), payload)
	for _, body := range []string{
		"selection:\n  fields:\n    publisher:\n      formats: [png]\n",
		"selection:\n  fields:\n    publisher:\n      languages: [ru]\n",
		"selection:\n  description:\n    regions: [world]\n",
		"selection:\n  media:\n    cover:\n      order: [language, source]\n",
	} {
		profile(t, &options, body)
		if _, err := Build(options); err == nil {
			t.Fatalf("ignored profile settings: %s", body)
		}
	}
}

func TestSourceMediaSymlinkIsNotCopied(t *testing.T) {
	options, gamePath, payload := fixture(t)
	put(t, filepath.Join(options.ROMRoot, "one.z64"), payload)
	outside := filepath.Join(filepath.Dir(options.DataRoot), "private-outside.png")
	put(t, outside, []byte("outside bytes"))
	link := filepath.Join(gamePath, "media", "box-front.usa.emumovies.png")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plan.Records[0].Media["cover"].SourcePath, "emumovies") {
		t.Fatal("outside source symlink selected")
	}
}

func TestZIPReleaseNameConflictsWithDifferentGameTitle(t *testing.T) {
	options, _, _ := fixture(t)
	other := filepath.Join(options.DataRoot, "platforms", "Nintendo - Nintendo 64", "Other")
	put(t, filepath.Join(other, "game.yaml"), []byte("title: Other Game\nreleases:\n  Other (USA): {regions: [USA]}\n"))
	putZip(t, filepath.Join(options.ROMRoot, "Other Game.zip"), map[string][]byte{"Example (USA).z64": []byte("unknown payload")})
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Ambiguous != 1 || plan.Matched != 0 || len(plan.Records[0].Candidates) != 2 {
		t.Fatalf("cross-level name conflict selected: %+v", plan)
	}
}

func TestGameGearSMSModePayloadUsesOnlyItsOwnExactCatalog(t *testing.T) {
	options, oldGame, payload := fixture(t)
	oldPlatform := filepath.Dir(oldGame)
	platform := filepath.Join(options.DataRoot, "platforms", "Sega - Game Gear")
	if err := os.Rename(oldPlatform, platform); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(platform, "_platform.yaml"), []byte("name: Sega - Game Gear\n"))
	options.Platform = "gamegear"
	putZip(t, filepath.Join(options.ROMRoot, "Wrapped.zip"), map[string][]byte{"unrelated-name.SMS": payload})
	put(t, filepath.Join(options.ROMRoot, "Raw.sms"), payload)
	putZip(t, filepath.Join(options.ROMRoot, "Multiple.zip"), map[string][]byte{"one.gg": payload, "two.sms": payload})
	put(t, filepath.Join(options.ROMRoot, "Unknown.sms"), []byte("unregistered SMS-mode bytes"))
	// An equal payload in another system's catalog is not an ambiguity or fallback.
	other := filepath.Join(options.DataRoot, "platforms", "Sega - Master System - Mark III", "Other")
	put(t, filepath.Join(other, "game.yaml"), []byte("title: Other\nreleases: {Other: {}}\n"))
	put(t, filepath.Join(other, "fingerprints.yaml"), []byte(fmt.Sprintf("items:\n  - release: Other\n    file: {size: %d, sha1: '%x'}\n", len(payload), sha1.Sum(payload))))
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if plan.HashMatches != 2 || plan.Unsupported != 1 || plan.Unmatched != 1 || len(plan.Records) != 4 {
		t.Fatalf("SMS-mode coverage: %+v", plan)
	}
	for _, record := range plan.Records {
		if record.Status == "matched" && (record.MatchedBy != "hash" || !strings.HasPrefix(record.Game, "platforms/Sega - Game Gear/")) {
			t.Fatal("cross-platform/name-only match", record)
		}
		if record.ROM == "Wrapped.zip" && (record.Member != "unrelated-name.SMS" || record.Payload == nil || record.Payload.SHA1 == "") {
			t.Fatal("ZIP payload proof absent", record)
		}
		if record.ROM == "Multiple.zip" && !strings.Contains(record.Reason, "multiple platform payloads") {
			t.Fatal("mixed GG/SMS archive accepted", record)
		}
	}
}
