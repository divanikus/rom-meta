package frontend

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func consolidationFixture(t *testing.T, ownerExtra string, hasMain bool) (Options, string, []string) {
	t.Helper()
	incomingID := generatedLaunchBoxID(platform + "\ncard:platforms/Test/Game")
	sources := []string{"X:/games/retail/Game (USA) (Disc I).chd", "X:/games/retail/Game (USA) (Disc II).chd"}
	oldIDs := []string{}
	existing := ""
	for i, launch := range sources {
		oldID := generatedLaunchBoxID(platform + "\ninput:" + strings.ToLower(launch))
		oldIDs = append(oldIDs, oldID)
		extra := "<Emulator>" + zeroGUID + "</Emulator><Installed>true</Installed><InstalledOverride>true</InstalledOverride><InstalledStateVersion>1</InstalledStateVersion><InstallationState>Unknown</InstallationState><MissingBoxFrontImage>true</MissingBoxFrontImage><DateAdded>0001-01-01T00:00:00+03:00</DateAdded>"
		if i == 0 {
			extra += ownerExtra
		}
		existing += game(oldID, placeholderTitle(launch), launch, extra)
		appID := generatedLaunchBoxID(platform + "\nversion:" + strings.ToLower(launch))
		existing += strings.Replace(version(appID, oldID, launch, "<Version>"+placeholderTitle(launch)+"</Version><PlayCount>0</PlayCount><Installed>true</Installed>"), "<Name>Original</Name>", "<Name>"+placeholderTitle(launch)+"</Name>", 1)
	}
	playlist := "Playlists/" + platform + "/" + v1 + ".m3u"
	if hasMain {
		existing += game(a, "Owner title", sources[0], "<Favorite>true</Favorite><PlayCount>8</PlayCount><Emulator>"+emulator+"</Emulator>")
	}
	o := setup(t, existing, game(incomingID, "Catalog title", playlist, "<Notes>Description</Notes>")+version(v1, incomingID, playlist, "<Version>USA [2 discs]</Version>"))
	put(t, o.PackageRoot, playlist, strings.Join(sources, "\n")+"\n")
	manifest := fmt.Sprintf(`{"schema_version":1,"target":"launchbox","rom_root":"X:/games","records":[{"rom":"retail/Game (USA) (Disc I).chd","status":"matched","game":"platforms/Test/Game"},{"rom":"retail/Game (USA) (Disc II).chd","status":"matched","game":"platforms/Test/Game"}],"launchbox":{"groups":[{"id":%q,"game":"platforms/Test/Game","versions":[{"records":[0,1]}]}]}}`, incomingID)
	put(t, o.PackageRoot, "manifest.json", manifest)
	return o, incomingID, oldIDs
}
func TestConsolidateGeneratedPlaceholdersPreservesNativeCardAndPlaylists(t *testing.T) {
	for _, hasMain := range []bool{false, true} {
		t.Run(fmt.Sprint(hasMain), func(t *testing.T) {
			o, incoming, oldIDs := consolidationFixture(t, "", hasMain)
			put(t, o.Destination, "Data/Playlists/Favorites.xml", wrap("<!-- owner comment --><Playlist><Name>Favorites</Name></Playlist><PlaylistGame><GameId>"+oldIDs[0]+"</GameId><Future>keep</Future></PlaylistGame>"))
			before := get(t, o.Destination, platformFile)
			p := mustBuild(t, o)
			if len(p.Conflicts) != 0 || len(p.Changes) != 1 || p.Changes[0].Consolidated != 2 {
				t.Fatalf("repair not planned: %+v", p)
			}
			if !bytes.Equal(before, get(t, o.Destination, platformFile)) {
				t.Fatal("preview mutated library")
			}
			backup, err := p.Apply()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, get(t, backup, platformFile)) {
				t.Fatal("repair backup lost")
			}
			games, apps := nativeData(t, o.Destination)
			target := incoming
			if hasMain {
				target = a
			}
			if len(games) != 1 || len(apps) != 1 || id(apps[0].fields["GameID"]) != target {
				t.Fatalf("orphan cards/disc apps remain: %+v %+v", games, apps)
			}
			if hasMain && (games[target].fields["Title"] != "Owner title" || games[target].fields["Favorite"] != "true" || games[target].fields["PlayCount"] != "8") {
				t.Fatal("owner curation lost")
			}
			playlist := string(get(t, o.Destination, "Data/Playlists/Favorites.xml"))
			if !strings.Contains(playlist, "<GameId>"+target+"</GameId>") || !strings.Contains(playlist, "<!-- owner comment -->") || !strings.Contains(playlist, "<Future>keep</Future>") {
				t.Fatal("playlist membership/raw data lost")
			}
			if _, err := os.Stat(filepath.Join(o.Destination, legacyStatePath)); !os.IsNotExist(err) {
				t.Fatal("repair created a binding file")
			}
			if repeat := mustBuild(t, o); len(repeat.Files) != 0 || repeat.Changes[0].Consolidated != 0 {
				t.Fatalf("repeat changed repaired library: %+v", repeat)
			}
			_ = get(t, o.Destination, filepath.ToSlash(filepath.Join("Playlists", platform, v1+".m3u")))
		})
	}
}
func TestConsolidationHoldsOwnerChangesAndUnknownData(t *testing.T) {
	for _, extra := range []string{"<Favorite>true</Favorite>", "<Notes>Owner notes</Notes>", "<Future><Nested>owner</Nested></Future>", "<Future flag=\"owner\" />", "<CommandLine>--custom</CommandLine>", "<PlayCount>1</PlayCount>"} {
		t.Run(extra, func(t *testing.T) {
			o, _, _ := consolidationFixture(t, extra, true)
			before := get(t, o.Destination, platformFile)
			p := mustBuild(t, o)
			if len(p.Conflicts) != 1 || p.Changes[0].Consolidated != 0 {
				t.Fatalf("owner data retirement allowed: %+v", p)
			}
			if _, err := p.Apply(); err == nil {
				t.Fatal("conflicting repair applied")
			}
			if !bytes.Equal(before, get(t, o.Destination, platformFile)) {
				t.Fatal("owner data changed")
			}
		})
	}
}
func TestConsolidationRequiresCurrentManifestAndIgnoresLegacyState(t *testing.T) {
	for _, manifest := range []bool{false, true} {
		t.Run(fmt.Sprint(manifest), func(t *testing.T) {
			o, _, _ := consolidationFixture(t, "", false)
			put(t, o.Destination, legacyStatePath, "not JSON")
			if !manifest {
				put(t, o.PackageRoot, "manifest.json", `{"schema_version":1,"target":"launchbox"}`)
			}
			p := mustBuild(t, o)
			if (p.Changes[0].Consolidated == 2) != manifest {
				t.Fatal("consolidation ignored current evidence", p.Changes)
			}
			if _, err := p.Apply(); err != nil {
				t.Fatal(err)
			}
			if string(get(t, o.Destination, legacyStatePath)) != "not JSON" {
				t.Fatal("legacy state was read/written")
			}
		})
	}
}

func TestSingleCuratedLegacyCardIsAdoptedWithoutStoredLinks(t *testing.T) {
	o, _, oldIDs := consolidationFixture(t, "<Favorite>true</Favorite><Future>owner</Future>", false)
	p := mustBuild(t, o)
	if len(p.Conflicts) != 0 || p.Changes[0].ID != oldIDs[0] || p.Changes[0].Consolidated != 1 {
		t.Fatalf("current-path adoption failed: %+v", p)
	}
	if _, err := p.Apply(); err != nil {
		t.Fatal(err)
	}
	games, _ := nativeData(t, o.Destination)
	if len(games) != 1 || games[oldIDs[0]].fields["Favorite"] != "true" || !bytes.Contains(get(t, o.Destination, platformFile), []byte("<Future>owner</Future>")) {
		t.Fatal("curated native data lost")
	}
	if repeat := mustBuild(t, o); len(repeat.Files) != 0 {
		t.Fatal("stateless adopted repeat changed", repeat.Files)
	}
}
