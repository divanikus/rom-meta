package frontend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const a = "10000000-0000-0000-0000-000000000001"
const b = "10000000-0000-0000-0000-000000000002"
const c = "10000000-0000-0000-0000-000000000003"
const v1 = "20000000-0000-0000-0000-000000000001"
const v2 = "20000000-0000-0000-0000-000000000002"
const emulator = "30000000-0000-0000-0000-000000000001"
const platform = "Test Console"
const platformFile = "Data/Platforms/Test Console.xml"

func put(t *testing.T, root, relative, body string) {
	t.Helper()
	name := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
func get(t *testing.T, root, relative string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	return body
}
func wrap(body string) string {
	return "<?xml version=\"1.0\"?>\n<LaunchBox>\n" + body + "\n</LaunchBox>\n"
}
func game(guid, title, launch, extra string) string {
	return "<Game>" + string(leaf("ID", guid)) + string(leaf("Title", title)) + string(leaf("Platform", platform)) + string(leaf("ApplicationPath", launch)) + extra + "</Game>"
}
func version(guid, parent, launch, extra string) string {
	return "<AdditionalApplication>" + string(leaf("Id", guid)) + string(leaf("GameID", parent)) + string(leaf("ApplicationPath", launch)) + "<Section>Version</Section><Name>Original</Name><UseEmulator>true</UseEmulator><EmulatorId>" + zeroGUID + "</EmulatorId>" + extra + "</AdditionalApplication>"
}
func setup(t *testing.T, existing, incoming string) Options {
	t.Helper()
	root := t.TempDir()
	pkg := t.TempDir()
	put(t, root, platformFile, wrap(existing))
	put(t, pkg, "manifest.json", `{"schema_version":1,"target":"launchbox"}`)
	put(t, pkg, platformFile, wrap(incoming))
	return Options{PackageRoot: pkg, Destination: root, Target: "launchbox"}
}
func mustBuild(t *testing.T, o Options) *Plan {
	t.Helper()
	p, err := Build(o)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func nativeData(t *testing.T, root string) (map[string]native, []native) {
	t.Helper()
	doc, err := parse(get(t, root, platformFile), "LaunchBox")
	if err != nil {
		t.Fatal(err)
	}
	g, v, err := records(doc)
	if err != nil {
		t.Fatal(err)
	}
	return g, v
}
func TestAdditiveMergePreservesNativeDataAndRepeats(t *testing.T) {
	original := game(a, "My title", "ROMs/one.zip", "<Notes></Notes><ReleaseDate>0001-01-01T00:00:00</ReleaseDate><Favorite>true</Favorite><PlayCount>42</PlayCount><Emulator>"+emulator+"</Emulator><CommandLine>--custom</CommandLine><Future attr=\"keep\"><Nested>owner &amp; value</Nested></Future>")
	unrelated := game(b, "Unrelated", "ROMs/other.zip", "<PlayTime>123456</PlayTime>")
	incoming := game(c, "Catalog title", "ROMs/one.zip", "<Notes>Prepared description</Notes><ReleaseDate>1995-01-02</ReleaseDate><Developer>Studio</Developer><Emulator>"+zeroGUID+"</Emulator><VideoPath>Videos/Test Console/"+c+"-01.mp4</VideoPath>") + version(v1, c, "ROMs/one.zip", "") + version(v2, c, "ROMs/two.zip", "")
	o := setup(t, original+"\n<!-- owner comment -->\n"+unrelated, incoming)
	put(t, o.PackageRoot, "Images/Test Console/Box - Front/"+c+"-01.png", "catalog cover")
	put(t, o.Destination, "Images/Test Console/Box - Front/My title-01.png", "owner cover")
	put(t, o.PackageRoot, "Images/Test Console/Screenshot - Gameplay/"+c+"-01.png", "screenshot")
	put(t, o.PackageRoot, "Videos/Test Console/"+c+"-01.mp4", "video")
	before := get(t, o.Destination, platformFile)
	p := mustBuild(t, o)
	if len(p.Conflicts) != 0 || len(p.Changes) != 1 || p.Changes[0].Action != "enrich" || p.Changes[0].Versions != 2 || p.Changes[0].Media != 2 {
		t.Fatalf("%+v", p)
	}
	if !bytes.Equal(before, get(t, o.Destination, platformFile)) {
		t.Fatal("preview changed library")
	}
	backup, err := p.Apply()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, get(t, backup, platformFile)) {
		t.Fatal("backup did not preserve original XML")
	}
	after := get(t, o.Destination, platformFile)
	for _, raw := range []string{unrelated, "<!-- owner comment -->", "<Favorite>true</Favorite>", "<PlayCount>42</PlayCount>", "<CommandLine>--custom</CommandLine>", "<Future attr=\"keep\"><Nested>owner &amp; value</Nested></Future>"} {
		if !bytes.Contains(after, []byte(raw)) {
			t.Fatalf("lost native data: %s", raw)
		}
	}
	games, apps := nativeData(t, o.Destination)
	if len(games) != 2 || len(apps) != 2 || games[a].fields["Title"] != "My title" || games[a].fields["Notes"] != "Prepared description" || games[a].fields["ReleaseDate"] != "1995-01-02" {
		t.Fatalf("%+v / %+v", games, apps)
	}
	if games[a].fields["VideoPath"] != "Videos/Test Console/"+a+"-01.mp4" {
		t.Fatal("media ID was not remapped")
	}
	for _, app := range apps {
		if app.fields["GameID"] != a || app.fields["EmulatorId"] != emulator {
			t.Fatal("parent/emulator lost")
		}
	}
	if string(get(t, o.Destination, "Images/Test Console/Box - Front/My title-01.png")) != "owner cover" {
		t.Fatal("owner media overwritten")
	}
	if string(get(t, o.Destination, "Images/Test Console/Screenshot - Gameplay/"+a+"-01.png")) != "screenshot" {
		t.Fatal("missing media not installed")
	}
	repeat := mustBuild(t, o)
	if len(repeat.Files) != 0 || repeat.Changes[0].Action != "unchanged" {
		t.Fatalf("repeat changes: %+v", repeat)
	}
	second, err := repeat.Apply()
	if err != nil || second != "" {
		t.Fatalf("no-op wrote backup: %s / %v", second, err)
	}
	if !bytes.Equal(after, get(t, o.Destination, platformFile)) {
		t.Fatal("repeat rewrote native XML")
	}
}
func TestNewCardAndGeneratedPlaylist(t *testing.T) {
	playlist := "Playlists/Test Console/" + v1 + ".m3u"
	incoming := game(c, "New game", playlist, "<Emulator>"+zeroGUID+"</Emulator><ManualPath>Manuals/Test Console/"+c+"-01.pdf</ManualPath>") + version(v1, c, playlist, "")
	o := setup(t, game(a, "Existing", "ROMs/old.zip", ""), incoming)
	put(t, o.PackageRoot, playlist, "X:/games/disc1.chd\nX:/games/disc2.chd\n")
	put(t, o.PackageRoot, "Manuals/Test Console/"+c+"-01.pdf", "manual")
	put(t, o.PackageRoot, "Images/Test Console/Box - Front/"+c+"-01.png", "cover")
	p := mustBuild(t, o)
	if p.Changes[0].Action != "add" || p.Changes[0].Media != 2 {
		t.Fatalf("%+v", p)
	}
	if _, err := p.Apply(); err != nil {
		t.Fatal(err)
	}
	games, apps := nativeData(t, o.Destination)
	if len(games) != 2 || len(apps) != 1 || games[c].fields["ApplicationPath"] != playlist || apps[0].fields["EmulatorId"] != zeroGUID {
		t.Fatal("new game or playlist binding lost")
	}
	if !bytes.Equal(get(t, o.PackageRoot, playlist), get(t, o.Destination, playlist)) {
		t.Fatal("playlist order/content changed")
	}
	_ = get(t, o.Destination, "Manuals/Test Console/"+c+"-01.pdf")
	if len(mustBuild(t, o).Files) != 0 {
		t.Fatal("repeat changed new card")
	}
}
func TestNoTitleGuessAndVersionPathAdoption(t *testing.T) {
	t.Run("homonym", func(t *testing.T) {
		o := setup(t, game(a, "Same title", "ROMs/old.zip", ""), game(c, "Same title", "ROMs/new.zip", "")+version(v1, c, "ROMs/new.zip", ""))
		p := mustBuild(t, o)
		if p.Changes[0].Action != "add" {
			t.Fatal(p.Changes)
		}
	})
	t.Run("version path", func(t *testing.T) {
		o := setup(t, game(a, "Owner title", `C:\Games\primary.zip`, "")+version(v1, a, `C:\Games\EUROPE.zip`, "<CommandLine>--keep</CommandLine>"), game(c, "Catalog", `c:/games/europe.zip`, "")+version(v2, c, `c:/games/europe.zip`, "<Region>Europe</Region>"))
		p := mustBuild(t, o)
		if p.Changes[0].ID != a || p.Changes[0].Versions != 0 {
			t.Fatal(p.Changes)
		}
		if _, err := p.Apply(); err != nil {
			t.Fatal(err)
		}
		_, apps := nativeData(t, o.Destination)
		if len(apps) != 1 || apps[0].fields["Id"] != v1 || apps[0].fields["CommandLine"] != "--keep" || apps[0].fields["Region"] != "Europe" {
			t.Fatal(apps)
		}
	})
}
func TestConflictsBlockAllMutations(t *testing.T) {
	tests := []struct{ name, existing, incoming string }{
		{"multiple cards", game(a, "US", "ROMs/us.zip", "") + game(b, "EU", "ROMs/eu.zip", ""), game(c, "Catalog", "ROMs/us.zip", "") + version(v1, c, "ROMs/us.zip", "") + version(v2, c, "ROMs/eu.zip", "")},
		{"app ID", game(a, "Owner", "ROMs/us.zip", "") + version(v1, a, "ROMs/other.zip", ""), game(c, "Catalog", "ROMs/us.zip", "") + version(v1, c, "ROMs/us.zip", "")},
		{"shared target", game(a, "Owner", "ROMs/us.zip", "") + version(v1, a, "ROMs/eu.zip", ""), game(b, "First", "ROMs/us.zip", "") + game(c, "Second", "ROMs/eu.zip", "")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			o := setup(t, test.existing, test.incoming)
			before := get(t, o.Destination, platformFile)
			p := mustBuild(t, o)
			if len(p.Conflicts) == 0 {
				t.Fatal("missing conflict")
			}
			if _, err := p.Apply(); err == nil {
				t.Fatal("applied conflict")
			}
			if !bytes.Equal(before, get(t, o.Destination, platformFile)) {
				t.Fatal("conflict changed library")
			}
			if _, err := os.Stat(filepath.Join(o.Destination, statePath)); !os.IsNotExist(err) {
				t.Fatal("conflict wrote state")
			}
		})
	}
}
func TestAdoptedIdentitySurvivesMovedPathsAndDeletedCard(t *testing.T) {
	o := setup(t, game(a, "Owner", "ROMs/old.zip", ""), game(c, "Catalog", "ROMs/old.zip", "")+version(v1, c, "ROMs/old.zip", ""))
	p := mustBuild(t, o)
	if _, err := p.Apply(); err != nil {
		t.Fatal(err)
	}
	put(t, o.PackageRoot, platformFile, wrap(game(c, "Catalog", "ROMs/moved.zip", "")+version(v2, c, "ROMs/moved.zip", "")))
	p = mustBuild(t, o)
	if p.Changes[0].ID != a || p.Changes[0].Action != "enrich" {
		t.Fatal(p.Changes)
	}
	if _, err := p.Apply(); err != nil {
		t.Fatal(err)
	}
	games, apps := nativeData(t, o.Destination)
	if len(games) != 1 || len(apps) != 2 || games[a].fields["ApplicationPath"] != "ROMs/old.zip" {
		t.Fatal("changed primary or duplicated card")
	}
	put(t, o.Destination, platformFile, wrap(""))
	if len(mustBuild(t, o).Conflicts) == 0 {
		t.Fatal("silently recreated a deleted adopted card")
	}
}
func TestDriftAndExistingPlaylistRejectBeforeMutation(t *testing.T) {
	for _, which := range []string{"library", "package", "asset", "destination"} {
		t.Run(which, func(t *testing.T) {
			o := setup(t, game(a, "Owner", "ROMs/old.zip", ""), game(c, "Catalog", "ROMs/new.zip", "")+version(v1, c, "ROMs/new.zip", ""))
			asset := "Images/Test Console/Box - Front/" + c + "-01.png"
			put(t, o.PackageRoot, asset, "cover")
			p := mustBuild(t, o)
			switch which {
			case "library":
				put(t, o.Destination, platformFile, wrap(game(a, "Changed by owner", "ROMs/old.zip", "")))
			case "package":
				put(t, o.PackageRoot, platformFile, wrap(""))
			case "asset":
				put(t, o.PackageRoot, asset, "changed")
			case "destination":
				put(t, o.Destination, asset, "owner")
			}
			before := get(t, o.Destination, platformFile)
			if _, err := p.Apply(); err == nil || !strings.Contains(err.Error(), "changed") {
				t.Fatal(err)
			}
			if !bytes.Equal(before, get(t, o.Destination, platformFile)) {
				t.Fatal("drift caused mutation")
			}
		})
	}
	t.Run("playlist collision", func(t *testing.T) {
		playlist := "Playlists/Test Console/" + v1 + ".m3u"
		o := setup(t, "", game(c, "New", playlist, "")+version(v1, c, playlist, ""))
		put(t, o.PackageRoot, playlist, "one\ntwo\n")
		put(t, o.Destination, playlist, "owner playlist\n")
		if _, err := Build(o); err == nil || !strings.Contains(err.Error(), "refusing overwrite") {
			t.Fatal(err)
		}
	})
}
func TestCustomFoldersAndUnsafeInputs(t *testing.T) {
	t.Run("custom media folder", func(t *testing.T) {
		o := setup(t, "", game(c, "New", "ROMs/new.zip", ""))
		put(t, o.Destination, "Data/Platforms.xml", wrap("<PlatformFolder><Platform>Test Console</Platform><MediaType>Box - Front</MediaType><FolderPath>D:/Custom</FolderPath></PlatformFolder>"))
		if _, err := Build(o); err == nil || !strings.Contains(err.Error(), "custom") {
			t.Fatal(err)
		}
	})
	for _, name := range []string{"../escape", "Images/../../escape", "C:/escape", "Images/link/../escape", "Images/x:stream", "Images/trailing. "} {
		if _, err := safeJoin(t.TempDir(), name); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	for _, body := range []string{"<LaunchBox><Game></LaunchBox>", "<!DOCTYPE LaunchBox><LaunchBox/>", "<Other/>", "<LaunchBox></LaunchBox><LaunchBox/>"} {
		if _, err := parse([]byte(body), "LaunchBox"); err == nil {
			t.Fatalf("accepted invalid XML: %s", body)
		}
	}
	t.Run("symlink", func(t *testing.T) {
		o := setup(t, "", game(c, "New", "ROMs/new.zip", ""))
		if err := os.Symlink(t.TempDir(), filepath.Join(o.Destination, "Images")); err != nil {
			t.Skip(err)
		}
		if _, err := Build(o); err == nil {
			t.Fatal("accepted linked library subtree")
		}
	})
	t.Run("wrong target", func(t *testing.T) {
		o := setup(t, "", game(c, "New", "ROMs/new.zip", ""))
		put(t, o.PackageRoot, "manifest.json", `{"schema_version":1,"target":"es-de"}`)
		if _, err := Build(o); err == nil {
			t.Fatal("accepted ES-DE package")
		}
	})
}
func TestExistingMediaAndNativeVersionsArePreserved(t *testing.T) {
	incoming := game(c, "Catalog", "ROMs/one.zip", "<VideoPath>Videos/Test Console/"+c+"-01.mp4</VideoPath>") + version(v2, c, "ROMs/one.zip", "<Region>Europe</Region>")
	oldVersion := version(v1, a, "ROMs/one.zip", "<Region>Owner region</Region><Priority>7</Priority><Disc>9</Disc>")
	o := setup(t, game(a, "Owner", "ROMs/one.zip", "<VideoPath>MyVideo.mp4</VideoPath><ManualPath>MyManual.pdf</ManualPath>")+oldVersion, incoming)
	p := mustBuild(t, o)
	if _, err := p.Apply(); err != nil {
		t.Fatal(err)
	}
	games, apps := nativeData(t, o.Destination)
	if games[a].fields["VideoPath"] != "MyVideo.mp4" || len(apps) != 1 || apps[0].fields["Region"] != "Owner region" || apps[0].fields["Disc"] != "9" || apps[0].fields["Priority"] != "7" {
		t.Fatal("owner media/version was overwritten")
	}
	if !bytes.Contains(get(t, o.Destination, platformFile), []byte(oldVersion)) {
		t.Fatal("existing version bytes changed")
	}
	state := identityMap{}
	if err := json.Unmarshal(get(t, o.Destination, statePath), &state); err != nil {
		t.Fatal(err)
	}
	if state.Cards[platform+"/"+c] != a {
		t.Fatal(fmt.Sprint(state))
	}
}

func TestEmptyNativePlatformRoot(t *testing.T) {
	o := setup(t, "", game(c, "New", "ROMs/new.zip", "")+version(v1, c, "ROMs/new.zip", ""))
	put(t, o.Destination, platformFile, "<?xml version=\"1.0\"?>\n<LaunchBox />\n")
	p := mustBuild(t, o)
	if _, err := p.Apply(); err != nil {
		t.Fatal(err)
	}
	games, apps := nativeData(t, o.Destination)
	if len(games) != 1 || len(apps) != 1 {
		t.Fatal("self-closing root was not expanded")
	}
}

func TestOrderedDiscPathsAdoptExistingM3UAndPreserveLaunch(t *testing.T) {
	playlist := "Playlists/Test Console/" + v1 + ".m3u"
	o := setup(t, "", game(c, "Catalog", playlist, "")+version(v1, c, playlist, ""))
	old := filepath.Join(t.TempDir(), "Existing.m3u")
	put(t, filepath.Dir(old), filepath.Base(old), "#EXTM3U\nC:/ROMs/Disc1.chd\nC:/ROMs/Disc2.chd\n")
	put(t, o.Destination, platformFile, wrap(game(a, "Owner", old, "<PlayCount>9</PlayCount>")))
	put(t, o.PackageRoot, playlist, "c:\\roms\\disc1.chd\nc:\\roms\\disc2.chd\n")
	p := mustBuild(t, o)
	if p.Changes[0].ID != a || p.Changes[0].Versions != 1 {
		t.Fatal(p.Changes)
	}
	if _, err := p.Apply(); err != nil {
		t.Fatal(err)
	}
	games, apps := nativeData(t, o.Destination)
	if len(games) != 1 || len(apps) != 1 || games[a].fields["ApplicationPath"] != old || apps[0].fields["ApplicationPath"] != old {
		t.Fatal("existing M3U launch path changed")
	}
	if _, err := os.Stat(filepath.Join(o.Destination, filepath.FromSlash(playlist))); !os.IsNotExist(err) {
		t.Fatal("installed unused generated playlist")
	}
	if len(mustBuild(t, o).Files) != 0 {
		t.Fatal("M3U merge is not idempotent")
	}
}
func TestPlaylistMemberOrderAndDrift(t *testing.T) {
	playlist := "Playlists/Test Console/" + v1 + ".m3u"
	o := setup(t, "", game(c, "Catalog", playlist, "")+version(v1, c, playlist, ""))
	old := filepath.Join(t.TempDir(), "Existing.m3u")
	put(t, filepath.Dir(old), filepath.Base(old), "C:/ROMs/Disc1.chd\nC:/ROMs/Disc2.chd\n")
	put(t, o.Destination, platformFile, wrap(game(a, "Owner", old, "")))
	put(t, o.PackageRoot, playlist, "C:/ROMs/Disc2.chd\nC:/ROMs/Disc1.chd\n")
	if mustBuild(t, o).Changes[0].Action != "add" {
		t.Fatal("ignored disc order")
	}
	put(t, o.PackageRoot, playlist, "C:/ROMs/Disc1.chd\nC:/ROMs/Disc2.chd\n")
	p := mustBuild(t, o)
	put(t, filepath.Dir(old), filepath.Base(old), "C:/changed.chd\nC:/other.chd\n")
	if _, err := p.Apply(); err == nil || !strings.Contains(err.Error(), "playlist changed") {
		t.Fatal(err)
	}
}

func TestMissingGeneratedPlaylistIsRestoredWithoutDuplicateVersion(t *testing.T) {
	playlist := "Playlists/Test Console/" + v1 + ".m3u"
	incoming := game(c, "Catalog", playlist, "") + version(v1, c, playlist, "")
	o := setup(t, incoming, incoming)
	put(t, o.PackageRoot, playlist, "C:/ROMs/Disc1.chd\nC:/ROMs/Disc2.chd\n")
	p := mustBuild(t, o)
	if len(p.Conflicts) != 0 || p.Changes[0].Versions != 0 {
		t.Fatal(p.Changes, p.Conflicts)
	}
	if _, err := p.Apply(); err != nil {
		t.Fatal(err)
	}
	_, apps := nativeData(t, o.Destination)
	if len(apps) != 1 {
		t.Fatal("duplicated version")
	}
	_ = get(t, o.Destination, playlist)
}
func TestUNCAndDrivePathsRemainDistinct(t *testing.T) {
	if keyPath("/library", `\\server\share\Game.zip`) != keyPath("/library", "//SERVER/share/Game.zip") {
		t.Fatal("UNC separators/case")
	}
	if keyPath("/library", `\\server\share\Game.zip`) == keyPath("/library", "/server/share/Game.zip") {
		t.Fatal("UNC collapsed into local absolute path")
	}
}

func TestSelfClosingUnrelatedNodeRemainsValid(t *testing.T) {
	o := setup(t, "<FutureSetting attr=\"keep\" />", game(c, "New", "ROMs/new.zip", "")+version(v1, c, "ROMs/new.zip", ""))
	p := mustBuild(t, o)
	if _, err := p.Apply(); err != nil {
		t.Fatal(err)
	}
	games, _ := nativeData(t, o.Destination)
	if len(games) != 1 || !bytes.Contains(get(t, o.Destination, platformFile), []byte("<FutureSetting attr=\"keep\" />")) {
		t.Fatal("self-closing sibling/root confused")
	}
}
