package exporter

import (
	"archive/zip"
	"crypto/sha1"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func futureCard(t *testing.T, options Options, title, release string, payload []byte) string {
	t.Helper()
	name := "Acme - Future Console"
	card := filepath.Join(options.DataRoot, "platforms", "private-store", title)
	put(t, filepath.Join(filepath.Dir(card), "_platform.yaml"), []byte("name: "+name+"\n"))
	put(t, filepath.Join(card, "game.yaml"), []byte(fmt.Sprintf("title: %q\ndefault_release: %q\nreleases:\n  %q: {regions: [USA]}\n", title, release, release)))
	put(t, filepath.Join(card, "fingerprints.yaml"), []byte(fmt.Sprintf("items:\n  - release: %q\n    file: {name: %q, size: %d, sha1: '%x'}\n", release, release+".future", len(payload), sha1.Sum(payload))))
	return card
}
func TestGenericFuturePlatformAndFormatsWithoutRegistration(t *testing.T) {
	options, _, _ := fixture(t)
	payload := []byte("synthetic future ROM")
	futureCard(t, options, "Future", "Future (USA)", payload)
	options.Platform = "Acme - Future Console"
	options.System = ""
	put(t, filepath.Join(options.ROMRoot, "Renamed.never-seen-before"), payload)
	putZip(t, filepath.Join(options.ROMRoot, "Wrapped.zip"), map[string][]byte{"unusual.format": payload, "readme.txt": []byte("documentation")})
	for _, target := range []string{"es-de", "launchbox"} {
		options.Target = target
		plan, err := Build(options)
		if err != nil {
			t.Fatal(err)
		}
		if plan.System != "acme-future-console" || plan.Platform != "private-store" || plan.HashMatches != 2 || plan.Matched != 2 || plan.Unsupported != 0 {
			t.Fatalf("generic platform/format not automatic: %+v", plan)
		}
		if err := Apply(plan, nil); err != nil {
			t.Fatal(err)
		}
		if target == "launchbox" {
			list := readLaunchBox(t, plan)
			if len(list.Games) != 1 || len(list.Versions) != 2 || list.Games[0].Platform != "Acme - Future Console" {
				t.Fatal(list)
			}
		}
		options.Out = filepath.Join(t.TempDir(), "next")
	}
}
func TestGenericContainersAndInstalledFolderKeepStatIdentity(t *testing.T) {
	options, _, _ := fixture(t)
	options.Platform = "Acme - Future Console"
	options.Target = "launchbox"
	data := []byte("container bytes deliberately equal a catalog raw fingerprint")
	for _, title := range []string{"Image", "Set", "Installed"} {
		futureCard(t, options, title, title+" (USA)", data)
	}
	put(t, filepath.Join(options.ROMRoot, "retail", "Image (USA).rvz"), data)
	putZip(t, filepath.Join(options.ROMRoot, "retail", "Set (USA).zip"), map[string][]byte{"disc.cue": []byte("FILE track.bin BINARY"), "track.bin": data})
	folder := filepath.Join(options.ROMRoot, "retail", "Installed (USA)")
	put(t, filepath.Join(folder, "code", "main.rpx"), data)
	put(t, filepath.Join(folder, "content", "data.app"), data)
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Records) != 3 || plan.Matched != 3 || plan.HashMatches != 0 || plan.NameMatches != 3 {
		t.Fatalf("container identity overstated or folder split: %+v", plan)
	}
	for _, record := range plan.Records {
		if record.Catalog == nil || record.Payload != nil || record.SHA1 != "" || record.MatchedBy != "catalog-name" {
			t.Fatal("stat became payload proof", record)
		}
	}
	if err := Apply(plan, nil); err != nil {
		t.Fatal(err)
	}
	list := readLaunchBox(t, plan)
	if len(list.Games) != 3 || len(list.Versions) != 3 {
		t.Fatal("missing source set cards", list)
	}
	for _, app := range list.Versions {
		if strings.HasSuffix(app.ApplicationPath, ".zip") || app.ApplicationPath == folder {
			if app.UseEmulator || app.Installed || !strings.Contains(app.Name, "source set") {
				t.Fatal("source package advertised as installed emulator launch", app)
			}
		}
	}
	xmlBody, err := RenderGamelist(plan)
	if err != nil {
		t.Fatal(err)
	}
	var esde xmlList
	if err := xml.Unmarshal(xmlBody, &esde); err != nil || len(esde.Folders) != 1 || len(esde.Games) != 2 {
		t.Fatalf("installed directory must remain native folder: %s %v", xmlBody, err)
	}
	options.Out = filepath.Join(t.TempDir(), "changed")
	plan, err = Build(options)
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(folder, "content", "added.bin"), data)
	if err := Apply(plan, nil); err == nil {
		t.Fatal("changed installed-folder membership accepted")
	}
	if _, err := os.Stat(options.Out); !os.IsNotExist(err) {
		t.Fatal("stale set published")
	}
}
func TestGenericLargeFilesUseBoundedStatAndUnsafeArchivesStayHeld(t *testing.T) {
	options, _, _ := fixture(t)
	options.Platform = "Acme - Future Console"
	futureCard(t, options, "Large", "Large (USA)", []byte("raw"))
	path := filepath.Join(options.ROMRoot, "Large (USA).future")
	put(t, path, []byte(""))
	f, err := os.OpenFile(path, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxPayload + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	putZip(t, filepath.Join(options.ROMRoot, "unsafe.zip"), map[string][]byte{"../escape.bin": []byte("no")})
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Records) != 2 || plan.Matched != 1 || plan.Unsupported != 1 || plan.HashMatches != 0 {
		t.Fatal("unbounded or unsafe input", plan)
	}
}
func TestGenericBatchCoversNewHeadersAndExcludesPCAndScummVM(t *testing.T) {
	options, _, _ := fixture(t)
	options.Platform = ""
	futureCard(t, options, "Future", "Future (USA)", []byte("ROM"))
	put(t, filepath.Join(options.ROMRoot, "Acme - Future Console", "Future (USA).future"), []byte("ROM"))
	for _, name := range []string{"IBM - PC and Compatibles", "ScummVM"} {
		put(t, filepath.Join(options.DataRoot, "platforms", name, "_platform.yaml"), []byte("name: "+name+"\n"))
		put(t, filepath.Join(options.ROMRoot, name, "Game.bin"), []byte("ROM"))
	}
	batch, err := BuildBatch(BatchOptions{Options: options, All: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Plans) != 1 || len(batch.Skipped) != 2 {
		t.Fatalf("coverage/exclusion: %+v", batch)
	}
	for _, skip := range batch.Skipped {
		if !strings.Contains(skip.Reason, "excluded") {
			t.Fatal(skip)
		}
	}
}

func TestGenericNamedDirectoryWithUnfamiliarComponentFormats(t *testing.T) {
	options, _, _ := fixture(t)
	options.Platform = "Acme - Future Console"
	futureCard(t, options, "Set", "Set (USA)", []byte("different raw fingerprint"))
	put(t, filepath.Join(options.ROMRoot, "retail", "Set (USA)", "part.one"), []byte("first"))
	put(t, filepath.Join(options.ROMRoot, "retail", "Set (USA)", "part.two"), []byte("second"))
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Records) != 1 || plan.Matched != 1 || !plan.Records[0].SourceOnly || plan.Records[0].Catalog == nil || len(plan.Records[0].Catalog.Files) != 2 {
		t.Fatal("new component formats require no platform registration", plan)
	}
}

func TestGenericNameExtensionPreservesDottedVersions(t *testing.T) {
	for _, name := range []string{"Example (USA) (v1.3)", "Example v1.2"} {
		if completeNameKey(name) != strings.ToLower(name) {
			t.Fatal("version treated as unknown extension", name, completeNameKey(name))
		}
	}
}

func TestGenericFuturePlatformUsesTypedPlaylistHandler(t *testing.T) {
	options, _, _ := fixture(t)
	options.Platform = "Acme - Future Console"
	options.Target = "launchbox"
	card := futureCard(t, options, "Discs", "Disc Game (USA) (Disc 1)", []byte("one"))
	put(t, filepath.Join(card, "game.yaml"), []byte("title: Discs\nreleases:\n  Disc Game (USA) (Disc 1): {regions: [USA]}\n  Disc Game (USA) (Disc 2): {regions: [USA]}\n"))
	put(t, filepath.Join(card, "fingerprints.yaml"), []byte(fmt.Sprintf("items:\n - release: Disc Game (USA) (Disc 1)\n   file: {name: first.iso, size: 3, sha1: '%x'}\n - release: Disc Game (USA) (Disc 2)\n   file: {name: second.iso, size: 3, sha1: '%x'}\n", sha1.Sum([]byte("one")), sha1.Sum([]byte("two")))))
	dir := filepath.Join(options.ROMRoot, "Disc Game (USA).m3u")
	put(t, filepath.Join(dir, "Disc Game (USA).m3u"), []byte("first.iso\nsecond.iso\n"))
	put(t, filepath.Join(dir, "first.iso"), []byte("one"))
	put(t, filepath.Join(dir, "second.iso"), []byte("two"))
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Records) != 1 || plan.Records[0].MatchedBy != "playlist-hash" || len(plan.Records[0].Playlist) != 2 || plan.Records[0].Catalog != nil {
		t.Fatal("generic folder overrode typed playlist", plan)
	}
}

func TestGenericCatalogDatasetsShareOneNativePlatformOutput(t *testing.T) {
	options, _, _ := fixture(t)
	options.Platform = ""
	options.Target = "launchbox"
	for i, name := range []string{"Nintendo - Nintendo 3DS", "Nintendo - New Nintendo 3DS"} {
		title := fmt.Sprintf("Game %d", i)
		familyCard(t, options.DataRoot, name, title, title+" (USA)")
		put(t, filepath.Join(options.ROMRoot, name, title+" (USA).3ds"), []byte(title))
	}
	batch, err := BuildBatch(BatchOptions{Options: options, All: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Plans) != 2 || len(batch.Skipped) != 0 {
		t.Fatal(batch)
	}
	if outputXMLPath(batch.Plans[0]) != outputXMLPath(batch.Plans[1]) {
		t.Fatal("native taxonomy was split to avoid merging datasets")
	}
	files, err := PreviewFiles(batch.Plans, false)
	if err != nil {
		t.Fatal(err)
	}
	path := outputXMLPath(batch.Plans[0])
	var list launchBoxList
	if err := xml.Unmarshal(files[path].Data, &list); err != nil || len(list.Games) != 2 || len(list.Versions) != 2 {
		t.Fatalf("lost dataset in preview: %+v %v", list, err)
	}
	if err := ApplyBatch(batch, nil); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(batch.Out, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(files[path].Data) {
		t.Fatal("package differs from coalesced native preview")
	}
}

func TestGenericLargeSingleZIPDoesNotBecomeInstallPackage(t *testing.T) {
	options, _, _ := fixture(t)
	options.Platform = "Acme - Future Console"
	futureCard(t, options, "Large", "Large (USA)", []byte("raw"))
	file, err := os.Create(filepath.Join(options.ROMRoot, "Large (USA).zip"))
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	// Synthetic directory declarations exercise the bounded path; no large payload
	// is allocated/decompressed, and this fixture never claims archive integrity.
	member, err := archive.CreateRaw(&zip.FileHeader{Name: "Large.newrom", Method: zip.Store, CompressedSize64: 1, UncompressedSize64: maxPayload + 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := member.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	file.Close()
	plan, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Matched != 1 || plan.HashMatches != 0 || plan.Records[0].Catalog == nil || plan.Records[0].SourceOnly {
		t.Fatal("hash confidence changed launch representation", plan)
	}
}
