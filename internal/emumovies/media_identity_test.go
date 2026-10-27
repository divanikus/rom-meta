package emumovies

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"rommeta/internal/catalogyaml"
)

func identityGame(name, release string) catalogGame {
	return catalogGame{Folder: name, Path: "/games/" + name, Title: "Example", Aliases: []string{"Example"}, DefaultRelease: release, Releases: []releaseInfo{{Name: release, Title: "Example", Regions: regionTags(release)}}}
}

func TestMediaMatchingPreservesEditionAndHardwareIdentity(t *testing.T) {
	for _, tc := range []struct {
		source, release string
		want            bool
	}{
		{"Example (USA)", "Example (USA) (Game of the Year Edition)", false},
		{"Example (USA) (Game of the Year Edition)", "Example (USA)", false},
		{"Example (USA)", "Example - Complete Edition (USA)", false},
		{"Example - Complete Edition (USA)", "Example (USA)", false},
		{"Example (USA) (32X)", "Example (USA) (Disc 1)", false},
		{"Example (USA) (Disc 2) (32X)", "Example (USA) (Disc 1) (32X)", true},
		{"Example (USA) (Demo)", "Example (USA)", false},
		{"Example (USA)", "Example (USA) (Demo)", false},
		{"Example (USA) (Unknown Edition)", "Example (USA)", false},
		{"Example (USA) (Disc Special)", "Example (USA)", false},
		{"Example (USA) (En,Fr) (Rev 1)", "Example (USA) (Disc 1)", true},
		{"Example (Europe)", "Example (Australia)", false},
		{"Example (Denmark)", "Example (Europe)", false},
		{"Example (World)", "Example (USA)", true},
	} {
		t.Run(tc.source+" -> "+tc.release, func(t *testing.T) {
			games := []catalogGame{identityGame("target", tc.release)}
			entry := FTPEntry{Name: tc.source + ".mp4", Size: 123}
			_, ok := matchVideoEntry("Platform", "videos", "HQ", "ftps", "example.test", entry, games)
			if ok != tc.want {
				t.Fatalf("match=%v want=%v", ok, tc.want)
			}
			index := newMediaGameIndex(games)
			_, indexed := matchVideoEntry("Platform", "videos", "HQ", "ftps", "example.test", entry, index.candidates(entry.Name))
			if indexed != ok {
				t.Fatal("index changed identity decision")
			}
			_, art := matchArtworkFile(artworkPackage{}, "art", filepath.Join("art", tc.source+".png"), 123, games)
			if art != tc.want {
				t.Fatalf("artwork match=%v want=%v", art, tc.want)
			}
		})
	}
}

func TestMediaMatchingKeepsSelectedEditionWhenRegionsOverlap(t *testing.T) {
	game := identityGame("folded", "Example (USA)")
	edition := "Example (USA) (Complete Edition)"
	game.Releases = append(game.Releases, releaseInfo{Name: edition, Title: "Example", Regions: []string{"USA"}})
	for _, stem := range []string{"Example (USA) (Complete Edition)", "Example (World) (Complete Edition)"} {
		release, score, _ := scoreVideoAgainstGame(stem, game)
		if release != edition || score < minimumMatchScore {
			t.Fatalf("%q selected %q/%d", stem, release, score)
		}
	}
}

func TestMediaMatchingSelectsCorrectOwnerOverLossyAliases(t *testing.T) {
	base := identityGame("base", "Example (USA)")
	edition := identityGame("complete", "Example (USA) (Complete Edition)")
	for _, tc := range []struct{ source, want string }{{"Example (USA)", "base"}, {"Example (USA) (Complete Edition)", "complete"}} {
		c, ok := matchVideoEntry("Platform", "videos", "HQ", "ftps", "example.test", FTPEntry{Name: tc.source + ".mp4"}, []catalogGame{edition, base})
		if !ok || c.Game != tc.want {
			t.Fatalf("%s: %#v/%v", tc.source, c, ok)
		}
	}
}

type failingVideoFTP struct {
	*fakeFTP
	failure string
}

func (f *failingVideoFTP) Download(path, target string) error {
	if path == f.failure {
		return errors.New("fixture transfer failure")
	}
	return f.fakeFTP.Download(path, target)
}

func TestFetchVideoResumePreservesHistoryAndOmitsFailedDownloads(t *testing.T) {
	root := makeEmuMoviesDataRoot(t)
	ftp := newFakeFTP()
	directory := "Official/Video Snaps (HQ)/Nintendo 64 (Video Snaps)(HQ)(EM 1.7)"
	game := filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "GoldenEye 007")
	metadata := filepath.Join(game, "game.yaml")
	first, err := FetchVideo(VideoOptions{DataRoot: root, FTPClient: ftp, Apply: true})
	if err != nil || first.DownloadedFileCount != 1 {
		t.Fatalf("first %#v %v", first, err)
	}
	data, err := os.ReadFile(metadata)
	if err != nil {
		t.Fatal(err)
	}
	// Recovery evidence and artwork fields must survive a later partial batch.
	text := strings.Replace(string(data), "  emumovies:\n    matches:\n", "  emumovies:\n    note: preserve recovery\n    matches:\n      - source_package: artwork.zip\n        media: {box_front: [{source_file: cover.png}]}\n", 1)
	text = strings.Replace(text, "        matched_file:", "        provenance_recovery: {method: full-size-and-sha256-suffix, files: [{path: media/video.mp4, local_sha256: fixture-sha}]}\n        matched_file:", 1)
	mustWrite(t, metadata, text)
	before, err := os.ReadFile(metadata)
	if err != nil {
		t.Fatal(err)
	}
	secondPath := directory + "/007 - GoldenEye (Europe).mp4"
	ftp.lists[directory] = append(ftp.lists[directory], FTPEntry{Name: "007 - GoldenEye (Europe).mp4", Size: 123})
	failing := &failingVideoFTP{fakeFTP: ftp, failure: secondPath}
	failed, err := FetchVideo(VideoOptions{DataRoot: root, FTPClient: failing, Apply: true})
	if err != nil || failed.FailedDownloadCount != 1 {
		t.Fatalf("failed %#v %v", failed, err)
	}
	after, err := os.ReadFile(metadata)
	if err != nil || string(after) != string(before) {
		t.Fatal("failed batch replaced source history")
	}
	resumed, err := FetchVideo(VideoOptions{DataRoot: root, FTPClient: ftp, Apply: true})
	if err != nil || resumed.DownloadedFileCount != 1 {
		t.Fatalf("resume %#v %v", resumed, err)
	}
	after, err = os.ReadFile(metadata)
	if err != nil {
		t.Fatal(err)
	}
	document, err := catalogyaml.Parse(after)
	if err != nil {
		t.Fatal(err)
	}
	source := catalogyaml.Value(catalogyaml.Value(document.Root(), "sources"), "emumovies")
	matches := catalogyaml.Value(source, "matches")
	if len(matches.Content) != 3 || catalogyaml.Scalar(catalogyaml.Value(source, "note")) != "preserve recovery" {
		t.Fatalf("lost prior source data: %s", after)
	}
	original, err := catalogyaml.Parse(before)
	if err != nil {
		t.Fatal(err)
	}
	originalMatches := catalogyaml.Value(catalogyaml.Value(catalogyaml.Value(original.Root(), "sources"), "emumovies"), "matches")
	var priorValues, afterValues []any
	if err = originalMatches.Decode(&priorValues); err != nil {
		t.Fatal(err)
	}
	if err = matches.Decode(&afterValues); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(priorValues, afterValues[:len(priorValues)]) {
		t.Fatal("existing video/artwork/recovery matches changed")
	}
	repeated, err := FetchVideo(VideoOptions{DataRoot: root, FTPClient: ftp, Apply: true})
	if err != nil || repeated.PlannedDownloadCount != 0 {
		t.Fatalf("repeat %#v %v", repeated, err)
	}
	final, _ := os.ReadFile(metadata)
	if string(final) != string(after) {
		t.Fatal("zero-download repeat changed metadata")
	}
}

func TestPSPMinisCatalogLabelMatchesUntaggedMedia(t *testing.T) {
	const platform = "Sony - PlayStation Portable"
	for _, tc := range []struct {
		source, release, system string
		want                    bool
	}{
		{"Example (Europe)", "Example (Europe) (Minis)", platform, true},
		{"Example (USA)", "Example (Europe) (Minis)", platform, false},
		{"Example (Japan)", "Example (USA) (Minis)", platform, false},
		{"Example (Europe) (Minis)", "Example (Europe) (Minis)", platform, true},
		{"Example (Europe) (Minis)", "Example (Europe)", platform, false},
		{"Example (Europe)", "Example (Europe) (Minis) (Demo)", platform, false},
		{"Example (Europe)", "Example (Europe) (Minis) (Unknown Edition)", platform, false},
		{"Example (Europe)", "Example (Europe) [Minis]", platform, false},
		{"Example (Europe)", "Example (Europe) (Minis)", "Nintendo - Nintendo DS", false},
		{"Example (Europe)", "Example (Europe) (Minis)", "Sony - PlayStation Portable (PSN) (Minis) (Encrypted)", true},
	} {
		t.Run(tc.system+"/"+tc.source+"/"+tc.release, func(t *testing.T) {
			game := identityGame("minis", tc.release)
			game.Platform = tc.system
			entry := FTPEntry{Name: tc.source + ".mp4", Size: 123}
			full, ok := matchVideoEntry(tc.system, "videos", "HQ", "ftps", "example.test", entry, []catalogGame{game})
			index := newMediaGameIndex([]catalogGame{game})
			indexed, indexedOK := matchVideoEntry(tc.system, "videos", "HQ", "ftps", "example.test", entry, index.candidates(entry.Name))
			if ok != tc.want || indexedOK != ok || !reflect.DeepEqual(full, indexed) {
				t.Fatalf("video full=%+v/%v indexed=%+v/%v want=%v", full, ok, indexed, indexedOK, tc.want)
			}
			art, artOK := matchArtworkFile(artworkPackage{Platform: tc.system}, "art", filepath.Join("art", tc.source+".png"), 123, index.candidates(entry.Name))
			if artOK != tc.want {
				t.Fatalf("art=%+v/%v want=%v", art, artOK, tc.want)
			}
			if ok && (full.Release != tc.release || art.Release != tc.release) {
				t.Fatal("catalog release key changed", full, art)
			}
		})
	}
	// A source name without the collection tag cannot choose between two owners.
	base := identityGame("retail", "Example (Europe)")
	base.Platform = platform
	mini := identityGame("mini", "Example (Europe) (Minis)")
	mini.Platform = platform
	games := []catalogGame{base, mini}
	entry := FTPEntry{Name: "Example (Europe).mp4", Size: 123}
	if c, ok := matchVideoEntry(platform, "videos", "HQ", "ftps", "example.test", entry, newMediaGameIndex(games).candidates(entry.Name)); ok {
		t.Fatal("ambiguous retail/digital owner chosen", c)
	}
	entry.Name = "Example (Europe) (Minis).mp4"
	if c, ok := matchVideoEntry(platform, "videos", "HQ", "ftps", "example.test", entry, newMediaGameIndex(games).candidates(entry.Name)); !ok || c.Game != "mini" {
		t.Fatal("explicit source label lost its owner", c, ok)
	}
}
