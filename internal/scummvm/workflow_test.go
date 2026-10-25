package scummvm

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rommeta/internal/catalog"
	"rommeta/internal/emumovies"
	"rommeta/internal/launchbox"
	"rommeta/internal/platformnames"
	"rommeta/internal/screenscraper"
	"rommeta/internal/sourceassets"
	"rommeta/internal/store"
)

// Exercise generated cards through the existing provider and download paths.
// All network/FTP data is synthetic; this is not a provider coverage estimate.
func TestScummVMMetadataArtworkVideoWorkflow(t *testing.T) {
	options := savedOptions(t, "scumm:tentacle Day of the Tentacle\n")
	if _, err := store.Init(options.DataRoot, store.InitOptions{}); err != nil {
		t.Fatal(err)
	}
	options.Apply = true
	if _, err := Import(options); err != nil {
		t.Fatal(err)
	}
	registry := platformnames.DefaultRegistry()
	if got := registry.ResolveCLI("scummvm"); got != Platform {
		t.Fatalf("CLI alias resolved to %s", got)
	}
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/systemesListe.php":
			fmt.Fprint(w, `{"response":{"systemes":[{"id":123,"nom":"ScummVM"},{"id":135,"nom":"PC Dos"}]}}`)
		case "/jeuInfos.php":
			if r.URL.Query().Get("systemeid") != "123" {
				t.Errorf("wrong ScreenScraper system: %s", r.URL.Query().Get("systemeid"))
			}
			fmt.Fprintf(w, `{"response":{"jeu":{"id":42,"noms":[{"region":"us","text":"Day of the Tentacle"}],"synopsis":[{"langue":"en","text":"Synthetic game description."}],"medias":[{"type":"box-2D","region":"us","url":%q}]}}}`, server.URL+"/cover.png")
		case "/cover.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write(pngData.Bytes())
		default:
			t.Errorf("unexpected request path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	config := fmt.Sprintf(`sources:
  launchbox:
    enabled: true
    levels:
      metadata: true
      artwork: true
  screenscraper:
    enabled: true
    api:
      base_url: %s
      rate_limit_per_second: 1000
    levels:
      metadata: true
      artwork: true
  emumovies:
    enabled: true
    api:
      base_url: ftps://example.test
    levels:
      video: true
`, server.URL)
	if err := os.WriteFile(filepath.Join(options.DataRoot, "config", "sources.yaml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	secrets := `sources:
  screenscraper:
    username: test
    password: test
    dev_id: test
    dev_password: test
  emumovies:
    username: test
    password: test
`
	if err := os.WriteFile(filepath.Join(options.DataRoot, "config", "secrets.yaml"), []byte(secrets), 0o600); err != nil {
		t.Fatal(err)
	}
	var snapshot bytes.Buffer
	archive := zip.NewWriter(&snapshot)
	member, err := archive.Create("Metadata.xml")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprint(member, `<LaunchBox>
<Game><DatabaseID>1</DatabaseID><Name>Day of the Tentacle</Name><Platform>MS-DOS</Platform><Overview>Wrong source platform</Overview></Game>
<Game><DatabaseID>2</DatabaseID><Name>Day of the Tentacle</Name><Platform>ScummVM</Platform><Overview>Correct source platform</Overview></Game>
</LaunchBox>`)
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(t.TempDir(), "Metadata.zip")
	if err := os.WriteFile(zipPath, snapshot.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	lb, err := launchbox.Import(launchbox.ImportOptions{DataRoot: options.DataRoot, Platform: Platform, PlatformRegistry: &registry, ZipPath: zipPath, Apply: true})
	if err != nil || lb.MatchedReleaseCount != 1 {
		t.Fatalf("LaunchBox workflow: %+v %v", lb, err)
	}
	ss, err := screenscraper.Import(screenscraper.ImportOptions{DataRoot: options.DataRoot, Platform: Platform, PlatformRegistry: &registry, Client: server.Client(), Apply: true})
	if err != nil || ss.MatchedGameCount != 1 || ss.SkippedUnmappedCount != 0 {
		t.Fatalf("ScreenScraper workflow: %+v %v", ss, err)
	}
	art, err := sourceassets.Fetch(sourceassets.Options{DataRoot: options.DataRoot, Platform: Platform, Source: "screenscraper", Apply: true})
	if err != nil || art.DownloadedFileCount != 1 {
		t.Fatalf("artwork workflow: %+v %v", art, err)
	}
	again, err := sourceassets.Fetch(sourceassets.Options{DataRoot: options.DataRoot, Platform: Platform, Source: "screenscraper", Apply: true})
	if err != nil || again.DownloadedFileCount != 0 || again.ExistingFileCount != 1 {
		t.Fatalf("artwork resume: %+v %v", again, err)
	}
	ftp := &scummVMFTP{}
	video, err := emumovies.FetchVideo(emumovies.VideoOptions{DataRoot: options.DataRoot, Platform: Platform, PlatformRegistry: &registry, FTPClient: ftp, Apply: true})
	if err != nil || video.DownloadedFileCount != 1 {
		t.Fatalf("video workflow: %+v %v", video, err)
	}
	repeat, err := emumovies.FetchVideo(emumovies.VideoOptions{DataRoot: options.DataRoot, Platform: Platform, PlatformRegistry: &registry, FTPClient: ftp, Apply: true})
	if err != nil || repeat.DownloadedFileCount != 0 || repeat.ExistingFileCount != 1 || ftp.downloads != 1 {
		t.Fatalf("video resume: %+v %v", repeat, err)
	}
	cards, err := readExisting(filepath.Join(options.DataRoot, "platforms", Platform))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cards["scumm:tentacle"].contents), "Correct source platform") || strings.Contains(string(cards["scumm:tentacle"].contents), "Wrong source platform") {
		t.Fatal("LaunchBox borrowed another platform")
	}
	if !strings.Contains(string(cards["scumm:tentacle"].contents), "Synthetic game description") {
		t.Fatal("description missing")
	}
	replay, err := Import(options)
	if err != nil || replay.WrittenFiles != 0 {
		t.Fatalf("import overwrote enriched card: %+v %v", replay, err)
	}
	issues, err := catalog.Validate(options.DataRoot)
	if err != nil || len(issues) != 0 {
		t.Fatalf("enriched catalog invalid: %+v %v", issues, err)
	}
}

type scummVMFTP struct{ downloads int }

const videoDirectory = "Official/Video Snaps (HQ)/ScummVM (Video Snaps)(HQ)(EM 1.0)"

func (ftp *scummVMFTP) List(path string) ([]emumovies.FTPEntry, error) {
	switch path {
	case "Official/Video Snaps (HQ)":
		return []emumovies.FTPEntry{{Name: "ScummVM (Video Snaps)(HQ)(EM 1.0)", IsDir: true}}, nil
	case videoDirectory:
		return []emumovies.FTPEntry{{Name: "Day of the Tentacle.mp4", Size: 5}}, nil
	}
	return nil, nil
}
func (ftp *scummVMFTP) Download(path, target string) error {
	ftp.downloads++
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(target, []byte("video"), 0o600)
}
