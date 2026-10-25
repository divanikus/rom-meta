package emumovies

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseFTPLineReadsWindowsListing(t *testing.T) {
	entry, ok := parseFTPLine("06-05-18  01:56PM              5095614 007 - GoldenEye (USA).mp4")
	if !ok {
		t.Fatalf("expected FTP line to parse")
	}
	if entry.Name != "007 - GoldenEye (USA).mp4" || entry.IsDir || entry.Size != 5095614 {
		t.Fatalf("unexpected entry: %#v", entry)
	}

	entry, ok = parseFTPLine("02-10-25  07:40PM       <DIR>          Nintendo 64 (Video Snaps)(HQ)(EM 1.7)")
	if !ok {
		t.Fatalf("expected directory line to parse")
	}
	if entry.Name != "Nintendo 64 (Video Snaps)(HQ)(EM 1.7)" || !entry.IsDir {
		t.Fatalf("unexpected directory entry: %#v", entry)
	}
}

func TestFetchVideoPlansRelativeTargets(t *testing.T) {
	root := makeEmuMoviesDataRoot(t)
	fake := newFakeFTP()
	planPath := filepath.Join(root, ".rommeta", "runs", "emumovies-video.jsonl")

	result, err := FetchVideo(VideoOptions{
		DataRoot:   root,
		FTPClient:  fake,
		PlanPath:   planPath,
		PlanFormat: "jsonl",
	})
	if err != nil {
		t.Fatalf("FetchVideo returned error: %v", err)
	}
	if result.VideoQuality != "HQ" || result.VideoFileCount != 2 || result.MatchedVideoCount != 1 || result.PlannedDownloadCount != 1 {
		t.Fatalf("unexpected result counts: %#v", result)
	}

	contents, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("ReadFile plan failed: %v", err)
	}
	text := string(contents)
	if !strings.Contains(text, `"region":"USA"`) {
		t.Fatalf("plan did not include region:\n%s", text)
	}
	if !strings.Contains(text, `"target":"platforms/Nintendo - Nintendo 64/GoldenEye 007/media/gameplay-video.usa.emumovies.mp4"`) {
		t.Fatalf("plan target was not relative POSIX path:\n%s", text)
	}
	if strings.Contains(text, `D:\`) || strings.Contains(text, `\\`) {
		t.Fatalf("plan should not contain absolute Windows paths:\n%s", text)
	}
	if !strings.Contains(text, `ftps://files.emumovies.com/Official/Video%20Snaps%20%28HQ%29/Nintendo%2064%20%28Video%20Snaps%29%28HQ%29%28EM%201.7%29/007%20-%20GoldenEye%20%28USA%29.mp4`) {
		t.Fatalf("plan did not contain escaped FTP URL:\n%s", text)
	}
	if len(fake.downloads) != 0 {
		t.Fatalf("dry run should not download: %#v", fake.downloads)
	}
}

func TestFetchVideoPrefersHDWhenAvailable(t *testing.T) {
	root := makeEmuMoviesDataRoot(t)
	fake := newFakeFTP()
	directory := "Official/Video Snaps (HD)/Nintendo 64 (Video Snaps)(HD)(EM 1.0)"
	fake.lists["Official/Video Snaps (HD)"] = []FTPEntry{
		{Name: "Nintendo 64 (Video Snaps)(HD)(EM 1.0)", IsDir: true},
	}
	fake.lists[directory] = []FTPEntry{
		{Name: "007 - GoldenEye (USA).mp4", Size: 9900000},
	}

	result, err := FetchVideo(VideoOptions{
		DataRoot:  root,
		FTPClient: fake,
	})
	if err != nil {
		t.Fatalf("FetchVideo returned error: %v", err)
	}
	if result.VideoQuality != "HD" || result.FTPDirectory != directory || result.MatchedVideoCount != 1 {
		t.Fatalf("expected HD directory to win, got %#v", result)
	}
}

func TestFindVideoDirectoryUsesPlatformCandidates(t *testing.T) {
	fake := &fakeFTP{lists: map[string][]FTPEntry{
		"Official/Video Snaps (HD)": {},
		"Official/Video Snaps (HQ)": {
			{Name: "Nintendo GBA (Video Snaps)(HQ)(EM 2.2)", IsDir: true},
		},
	}}

	directory, quality, err := findVideoDirectory(fake, "Nintendo - Game Boy Advance", nil)
	if err != nil {
		t.Fatalf("findVideoDirectory returned error: %v", err)
	}
	if directory != "Official/Video Snaps (HQ)/Nintendo GBA (Video Snaps)(HQ)(EM 2.2)" || quality != "HQ" {
		t.Fatalf("unexpected directory match: directory=%q quality=%q", directory, quality)
	}
}

func TestFetchVideoApplyDownloadsAndWritesSourceStanza(t *testing.T) {
	root := makeEmuMoviesDataRoot(t)
	fake := newFakeFTP()

	result, err := FetchVideo(VideoOptions{
		DataRoot:  root,
		FTPClient: fake,
		Apply:     true,
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("FetchVideo returned error: %v", err)
	}
	if result.DownloadedFileCount != 1 || len(fake.downloads) != 1 {
		t.Fatalf("expected one download: result=%#v downloads=%#v", result, fake.downloads)
	}

	target := filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "GoldenEye 007", "media", "gameplay-video.usa.emumovies.mp4")
	if data, err := os.ReadFile(target); err != nil || string(data) != "video" {
		t.Fatalf("unexpected downloaded file: data=%q err=%v", data, err)
	}

	gameYAML := filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "GoldenEye 007", "game.yaml")
	contents, err := os.ReadFile(gameYAML)
	if err != nil {
		t.Fatalf("ReadFile game.yaml failed: %v", err)
	}
	text := string(contents)
	for _, expected := range []string{
		"  emumovies:",
		`release: "GoldenEye 007 (USA)"`,
		`matched_file: "007 - GoldenEye (USA).mp4"`,
		`match_keys: ["alias", "region"]`,
		`gameplay_video:`,
		`- url: "ftps://files.emumovies.com/Official/Video%20Snaps%20%28HQ%29/Nintendo%2064%20%28Video%20Snaps%29%28HQ%29%28EM%201.7%29/007%20-%20GoldenEye%20%28USA%29.mp4"`,
		`region: "USA"`,
		`quality: "HQ"`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("game.yaml missing %q:\n%s", expected, text)
		}
	}
}

func TestEnsureCredentialsReportsMissingEmuMoviesSecrets(t *testing.T) {
	err := ensureCredentials(sourceSettings{Username: "user"})
	if err == nil {
		t.Fatalf("expected missing password error")
	}
	if !strings.Contains(err.Error(), "EmuMovies credentials are missing") || !strings.Contains(err.Error(), "sources.emumovies.password") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCopyWithIdleDeadlineAllowsLongActiveTransfers(t *testing.T) {
	reader, writer := net.Pipe()
	defer reader.Close()

	done := make(chan error, 1)
	go func() {
		defer writer.Close()
		for _, chunk := range []string{"lo", "ng", "-", "do", "wnload"} {
			time.Sleep(30 * time.Millisecond)
			if _, err := writer.Write([]byte(chunk)); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()

	var out bytes.Buffer
	written, err := copyWithIdleDeadline(&out, reader, 120*time.Millisecond, nil, "", 0, "", 0, 0)
	if err != nil {
		t.Fatalf("copyWithIdleDeadline returned error: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("writer returned error: %v", err)
	}
	if written != int64(len("long-download")) || out.String() != "long-download" {
		t.Fatalf("unexpected copied data: written=%d data=%q", written, out.String())
	}
}

func makeEmuMoviesDataRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "config", "sources.yaml"), `sources:
  emumovies:
    enabled: true
    kind: media
    credentials:
      type: secrets
      username: username
      password: password
    api:
      base_url: ftps://files.emumovies.com
    tools:
      archive_extractor: none
    levels:
      video: true
`)
	mustWrite(t, filepath.Join(root, "config", "secrets.yaml"), `sources:
  emumovies:
    username: "user"
    password: "pass"
`)
	mustWrite(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "GoldenEye 007", "game.yaml"), `title: "GoldenEye 007"
default_release: "GoldenEye 007 (USA)"
alternate_titles: ["007: GoldenEye"]

releases:
  "GoldenEye 007 (Europe)":
    title: "GoldenEye 007"
    regions: ["Europe"]
  "GoldenEye 007 (USA)":
    title: "GoldenEye 007"
    regions: ["USA"]
sources:
  launchbox:
    matches:
      - release: "GoldenEye 007 (USA)"
        alternate_titles: ["stale source alias"]
`)
	mustWrite(t, filepath.Join(root, "platforms", "Nintendo - Nintendo 64", "Banjo-Kazooie", "game.yaml"), `title: "Banjo-Kazooie"
default_release: "Banjo-Kazooie (USA)"

releases:
  "Banjo-Kazooie (USA)":
    title: "Banjo-Kazooie"
    regions: ["USA"]
`)
	return root
}

func mustWrite(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}

type fakeFTP struct {
	lists     map[string][]FTPEntry
	downloads []string
}

func newFakeFTP() *fakeFTP {
	directory := "Official/Video Snaps (HQ)/Nintendo 64 (Video Snaps)(HQ)(EM 1.7)"
	return &fakeFTP{
		lists: map[string][]FTPEntry{
			"Official/Video Snaps (HD)": {
				{Name: "Nintendo GameCube (Video Snaps)(HD)(EM WIP)", IsDir: true},
			},
			"Official/Video Snaps (HQ)": {
				{Name: "Nintendo 64 (Video Snaps)(HQ)(EM 1.7)", IsDir: true},
				{Name: "Nintendo 64DD (Video Snaps)(HQ)(EM 1.0)", IsDir: true},
			},
			directory: {
				{Name: "007 - GoldenEye (USA).mp4", Size: 5095614},
				{Name: "Unknown Game (USA).mp4", Size: 1234},
			},
		},
	}
}

func (fake *fakeFTP) List(path string) ([]FTPEntry, error) {
	entries, ok := fake.lists[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return entries, nil
}

func (fake *fakeFTP) Download(path, target string) error {
	fake.downloads = append(fake.downloads, path)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, []byte("video"), 0o644)
}
