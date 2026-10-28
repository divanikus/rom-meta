package emumovies

import (
	"errors"
	"os"
	"rommeta/internal/fetchstate"
	"strings"
	"testing"
)

type failingArtworkFTP struct {
	*fakeArtworkFTP
	fail bool
}

func (f *failingArtworkFTP) Download(path, target string) error {
	if f.fail && strings.Contains(path, "2D Boxes") {
		return errors.New("fixture transfer failure")
	}
	return f.fakeArtworkFTP.Download(path, target)
}
func TestPartialArtworkUsesCompletePackagesAndResumesOnlyMissing(t *testing.T) {
	root := makeEmuMoviesDataRoot(t)
	ftp := &failingArtworkFTP{fakeArtworkFTP: newFakeArtworkFTP(), fail: true}
	result, err := FetchArtwork(ArtworkOptions{DataRoot: root, Apply: true, FTPClient: ftp})
	var partial *fetchstate.PartialError
	if !errors.As(err, &partial) || result.DownloadedPackageCount != 1 || result.FailedDownloadCount != 1 || result.ExtractedPackageCount != 1 || result.WrittenArtworkFileCount != 1 {
		t.Fatalf("partial=%#v err=%v", result, err)
	}
	report, err := os.ReadFile(result.ManifestPath)
	if err != nil || !strings.Contains(string(report), `"status":"failed"`) {
		t.Fatalf("lost failed archive: %s %v", report, err)
	}
	calls := len(ftp.downloads)
	ftp.fail = false
	resumed, err := FetchArtwork(ArtworkOptions{DataRoot: root, Apply: true, FTPClient: ftp})
	if err != nil || resumed.FailedDownloadCount != 0 || resumed.ExistingPackageCount != 1 || resumed.DownloadedPackageCount != 1 || len(ftp.downloads) != calls+1 || !strings.Contains(ftp.downloads[len(ftp.downloads)-1], "2D Boxes") {
		t.Fatalf("resume=%#v err=%v", resumed, err)
	}
	if resumed.ManifestPath == result.ManifestPath {
		t.Fatal("old report overwritten")
	}
}
