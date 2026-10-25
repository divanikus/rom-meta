package emumovies

import (
	"errors"
	"path/filepath"
	"testing"
)

func addUnmappedPlatform(t *testing.T, root string) {
	mustWrite(t, filepath.Join(root, "platforms", "Example - Missing", "Example Game", "game.yaml"), `title: Example Game
default_release: Example Game (World)
releases:
  Example Game (World):
    title: Example Game
`)
}

func TestMediaFetchSkipsMissingPlatformDirectory(t *testing.T) {
	for _, apply := range []bool{false, true} {
		t.Run(map[bool]string{false: "plan", true: "apply"}[apply], func(t *testing.T) {
			t.Run("artwork", func(t *testing.T) {
				root := makeEmuMoviesDataRoot(t)
				addUnmappedPlatform(t, root)
				fake := newFakeArtworkFTP()
				fake.lists[artworkParentDirectory] = []FTPEntry{{Name: "Nintendo 64", IsDir: true}}
				result, err := FetchArtwork(ArtworkOptions{DataRoot: root, FTPClient: fake, Apply: apply, Limit: 1})
				if err != nil {
					t.Fatal(err)
				}
				if len(result.SkippedPlatforms) != 1 || result.SkippedPlatforms["Example - Missing"] == "" || result.PlannedPackageCount != 1 {
					t.Fatalf("unexpected result: %#v", result)
				}
				if apply && result.DownloadedPackageCount != 1 {
					t.Fatal("supported platform did not download")
				}
				if !apply && len(fake.downloads) > 0 {
					t.Fatal("dry run downloaded files")
				}
			})
			t.Run("video", func(t *testing.T) {
				root := makeEmuMoviesDataRoot(t)
				addUnmappedPlatform(t, root)
				fake := newFakeFTP()
				fake.lists["Official/Video Snaps (SQ)"] = []FTPEntry{}
				result, err := FetchVideo(VideoOptions{DataRoot: root, FTPClient: fake, Apply: apply})
				if err != nil {
					t.Fatal(err)
				}
				if len(result.SkippedPlatforms) != 1 || result.SkippedPlatforms["Example - Missing"] == "" || result.PlannedDownloadCount != 1 {
					t.Fatalf("unexpected result: %#v", result)
				}
				if apply && result.DownloadedFileCount != 1 {
					t.Fatal("supported platform did not download")
				}
				if !apply && len(fake.downloads) > 0 {
					t.Fatal("dry run downloaded files")
				}
			})
		})
	}
}

type failingListFTP struct {
	FTPClient
	path    string
	failure error
}

func (f failingListFTP) List(path string) ([]FTPEntry, error) {
	if path == f.path {
		return nil, f.failure
	}
	return f.FTPClient.List(path)
}

func TestFailedMediaListingIsNotMissingPlatform(t *testing.T) {
	failure := errors.New("test FTP connection failure")
	t.Run("artwork parent", func(t *testing.T) {
		fake := newFakeArtworkFTP()
		_, _, err := findArtworkDirectory(failingListFTP{fake, artworkParentDirectory, failure}, "Example - Missing", nil)
		var missing missingPlatformDirectoryError
		if !errors.Is(err, failure) || errors.As(err, &missing) {
			t.Fatalf("masked listing error: %v", err)
		}
	})
	t.Run("artwork child", func(t *testing.T) {
		fake := newFakeArtworkFTP()
		fake.lists[artworkParentDirectory] = []FTPEntry{{Name: "Nintendo 64", IsDir: true}}
		_, _, err := findArtworkDirectory(failingListFTP{fake, "Official/Artwork/Nintendo 64", failure}, "Nintendo - Nintendo 64", nil)
		var missing missingPlatformDirectoryError
		if !errors.Is(err, failure) || errors.As(err, &missing) {
			t.Fatalf("masked child error: %v", err)
		}
	})
	t.Run("video parent", func(t *testing.T) {
		fake := newFakeFTP()
		fake.lists["Official/Video Snaps (SQ)"] = []FTPEntry{}
		_, _, err := findVideoDirectory(failingListFTP{fake, "Official/Video Snaps (HQ)", failure}, "Example - Missing", nil)
		var missing missingPlatformDirectoryError
		if err == nil || errors.As(err, &missing) {
			t.Fatalf("masked listing error: %v", err)
		}
	})
	t.Run("video child", func(t *testing.T) {
		root := makeEmuMoviesDataRoot(t)
		fake := newFakeFTP()
		_, err := FetchVideo(VideoOptions{DataRoot: root, FTPClient: failingListFTP{fake, "Official/Video Snaps (HQ)/Nintendo 64 (Video Snaps)(HQ)(EM 1.7)", failure}})
		if !errors.Is(err, failure) {
			t.Fatalf("masked child error: %v", err)
		}
	})
}

func TestVideoDirectoryResolutionLabelsAndVendorName(t *testing.T) {
	for _, tc := range []struct{ platform, quality, directory string }{
		{"Atari - Atari 2600", "HQ", "Atari 2600 (Video Snaps)(480p)(No-Intro)(EM 2.1)"},
		{"Atari - Atari 5200", "SQ", "Atari 5200 (Video Snaps)(240p)(No-Intro)(EM 2.2)"},
		{"3DO Interactive Multiplayer", "HQ", "Panasonic 3DO (Video Snaps)(HQ)(EM 1.4)"},
		{"Arcade", "HD", "MAME Arcade (Video Snaps)(HD)(MAME)(EM .278)"},
		{"Nintendo - Super Nintendo Entertainment System", "HQ", "Nintendo Super Nintendo (Video Snaps)(HQ)(HyperList)(EM 2.2)"},
		{"Sega - Mega Drive - Genesis", "HQ", "Sega Genesis - USA (Video Snaps)(HQ)(No-Intro)(EM 2.6)"},
		{"Sony - PlayStation 3", "HD", "Sony Playstation 3 - Retail (Video Snaps)(HD)(ReDump)(EM 0.9)"},
	} {
		t.Run(tc.platform, func(t *testing.T) {
			fake := &fakeFTP{lists: map[string][]FTPEntry{}}
			for _, q := range videoQualityPreference {
				fake.lists[q.Parent] = []FTPEntry{}
			}
			parent := "Official/Video Snaps (" + tc.quality + ")"
			fake.lists[parent] = []FTPEntry{{Name: tc.directory, IsDir: true}}
			directory, quality, err := findVideoDirectory(fake, tc.platform, nil)
			if err != nil || directory != parent+"/"+tc.directory || quality != tc.quality {
				t.Fatalf("directory=%q quality=%q err=%v", directory, quality, err)
			}
		})
	}
	if videoDirectoryMatchScore("Atari 2600 (Video Snaps)(240p)", []string{"Atari 2600"}, " hq") != 0 {
		t.Fatal("low-resolution folder accepted as HQ")
	}
}

func TestVideoDirectoryRequiresExactPlatformBeforeQuality(t *testing.T) {
	for _, tc := range []struct {
		platform string
		correct  string
		wrong    string
	}{
		{"Microsoft - Xbox", "Microsoft Xbox", "Microsoft XBOX 360"},
		{"Sony - PlayStation 3", "Sony Playstation 3 - Retail", "Sony Playstation 3 - PSN"},
		{"Nintendo - Super Nintendo Entertainment System", "Nintendo Super Nintendo", "Nintendo Super Nintendo (Hacks & Homebrew)"},
		{"Sony - PlayStation", "Sony PlayStation", "Sony PlayStation 2"},
		{"Sony - PlayStation", "Sony PlayStation", "Sony PlayStation 3"},
		{"Nintendo - Game Boy", "Nintendo Game Boy", "Nintendo Game Boy Color"},
		{"Nintendo - Game Boy", "Nintendo Game Boy", "Nintendo Game Boy Advance"},
		{"Nintendo - Nintendo 64", "Nintendo 64", "Nintendo 64 DD"},
	} {
		t.Run(tc.wrong, func(t *testing.T) {
			for _, exactAvailable := range []bool{true, false} {
				t.Run(map[bool]string{true: "exact-lower-quality", false: "wrong-only"}[exactAvailable], func(t *testing.T) {
					fake := &fakeFTP{lists: map[string][]FTPEntry{}}
					for _, q := range videoQualityPreference {
						fake.lists[q.Parent] = []FTPEntry{}
					}
					fake.lists[videoQualityPreference[0].Parent] = []FTPEntry{
						{Name: tc.wrong + " (Video Snaps)(HD)(Redump)(EM 1.1)", IsDir: true},
					}
					correct := tc.correct + " (Video Snaps)(HQ)(EM 1.0)"
					if exactAvailable {
						fake.lists[videoQualityPreference[1].Parent] = []FTPEntry{{Name: correct, IsDir: true}}
					}
					directory, quality, err := findVideoDirectory(fake, tc.platform, nil)
					if exactAvailable {
						if err != nil || quality != "HQ" || directory != videoQualityPreference[1].Parent+"/"+correct {
							t.Fatalf("selected another platform: directory=%q quality=%q err=%v", directory, quality, err)
						}
					} else {
						var missing missingPlatformDirectoryError
						if !errors.As(err, &missing) || directory != "" || quality != "" {
							t.Fatalf("accepted another platform as fallback: directory=%q quality=%q err=%v", directory, quality, err)
						}
					}
				})
			}
		})
	}
}

func TestPlatformDirectoryIdentityRejectsExtraWords(t *testing.T) {
	for _, other := range []string{"Microsoft Xbox 360", "Super Microsoft Xbox", "Microsoft Xbox One"} {
		if platformDirectoryNameScore(other, "Microsoft Xbox") != 0 || platformDirectoryNameEqual(other, "Microsoft Xbox") {
			t.Fatalf("accepted a different platform: %q", other)
		}
	}
	for _, same := range []string{"MICROSOFT XBOX", "Microsoft-Xbox", "MicrosoftXbox"} {
		if !platformDirectoryNameEqual(same, "Microsoft Xbox") {
			t.Fatalf("rejected normalized platform identity: %q", same)
		}
	}
}
