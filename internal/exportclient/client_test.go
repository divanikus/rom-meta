package exportclient

import (
	"archive/zip"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"rommeta/internal/catalogapi"
	"rommeta/internal/catalogfstest"
	"rommeta/internal/exporter"
)

func put(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}
func platform(t *testing.T, root, system string) {
	t.Helper()
	name, ok := exporter.SystemPlatform(system)
	if !ok {
		t.Fatal(system)
	}
	put(t, filepath.Join(root, "platforms", system, "_platform.yaml"), []byte(fmt.Sprintf("name: %q\n", name)))
}
func card(t *testing.T, root, system, folder, title, release, filename string, data []byte) {
	t.Helper()
	platform(t, root, system)
	path := filepath.Join(root, "platforms", system, folder)
	put(t, filepath.Join(path, "game.yaml"), []byte(fmt.Sprintf("title: %q\ndefault_release: %q\ndescription: Offline description\nreleases:\n  %q: {regions: [USA]}\n", title, release, release)))
	fp := fmt.Sprintf("items:\n  - release: %q\n    file: {name: %q, size: %d, sha1: %q, sha256: %q, md5: %q, crc32: %q}\n", release, filename, len(data), fmt.Sprintf("%x", sha1.Sum(data)), fmt.Sprintf("%x", sha256.Sum256(data)), fmt.Sprintf("%x", md5.Sum(data)), fmt.Sprintf("%08x", crc32.ChecksumIEEE(data)))
	put(t, filepath.Join(path, "fingerprints.yaml"), []byte(fp))
	put(t, filepath.Join(path, "media", release, "box-front.usa.launchbox.png"), []byte("synthetic cover bytes"))
}
func arcade(t *testing.T, root string) {
	t.Helper()
	platform(t, root, "arcade")
	path := filepath.Join(root, "platforms", "arcade", "Game")
	// Real MAME bindings deliberately use the shared physical Arcade folder.
	if err := os.Rename(filepath.Join(root, "platforms", "arcade"), filepath.Join(root, "platforms", "Arcade")); err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(root, "platforms", "Arcade", "Game")
	put(t, filepath.Join(path, "game.yaml"), []byte("title: Arcade Game\ndescription: Arcade description\nreleases:\n  game1:\n    regions: [USA]\n    authority: {source: mame, entry_name: game1}\n    mame: {snapshot: test-snapshot}\n"))
	put(t, filepath.Join(path, "fingerprints.yaml"), []byte("items: []\n"))
	put(t, filepath.Join(path, "media", "game1", "box-front.usa.launchbox.png"), []byte("arcade cover"))
	put(t, filepath.Join(root, "bindings", "romsets", "mame", "test-snapshot", "catalog.yaml"), []byte("sets:\n  game1: {platform: Arcade, game: Game, release: game1}\n"))
}
func server(t *testing.T, root string, wrap func(http.Handler) http.Handler) *httptest.Server {
	t.Helper()
	api, err := catalogapi.NewLookup(catalogapi.Options{Export: exporter.Options{DataRoot: root}, LookupCacheDir: "-"})
	if err != nil {
		t.Fatal(err)
	}
	var handler http.Handler = api
	if wrap != nil {
		handler = wrap(handler)
	}
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	return s
}
func selectedBytes(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			rel, _ := filepath.Rel(root, path)
			if strings.HasPrefix(filepath.ToSlash(rel), "gamelists/") || strings.HasPrefix(filepath.ToSlash(rel), "downloaded_media/") {
				b, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				result[filepath.ToSlash(rel)] = fmt.Sprintf("%x", sha256.Sum256(b))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func assertLocalParity(t *testing.T, root, roms, system, api string, profile ...string) {
	t.Helper()
	base := t.TempDir()
	options := exporter.Options{DataRoot: root, ROMRoot: roms, Platform: system, Out: filepath.Join(base, "local")}
	if len(profile) > 0 {
		options.ProfilePath = profile[0]
	}
	local, err := exporter.Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if err = exporter.Apply(local, nil); err != nil {
		t.Fatal(err)
	}
	job, err := Build(context.Background(), Options{API: api, BatchOptions: exporter.BatchOptions{Options: exporter.Options{ROMRoot: roms, Platform: system, ProfilePath: options.ProfilePath, Out: filepath.Join(base, "remote")}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(job.Batch.Out); !os.IsNotExist(err) {
		t.Fatal("preview published output")
	}
	if err = job.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(selectedBytes(t, local.Out), selectedBytes(t, job.Batch.Out)) {
		t.Fatal("remote gamelist/media differs from local export")
	}
	localJSON, _ := json.Marshal(local.Records)
	remoteJSON, _ := json.Marshal(job.Batch.Plans[0].Records)
	if string(localJSON) != string(remoteJSON) {
		t.Fatalf("records differ: local %s remote %s", localJSON, remoteJSON)
	}
	if strings.Contains(string(remoteJSON), root) {
		t.Fatal("server filesystem root leaked")
	}
}

func TestRemoteParityForEveryLocalReader(t *testing.T) {
	cases := map[string]string{"n64": ".z64", "nes": ".nes", "snes": ".sfc", "gb": ".gb", "gbc": ".gbc", "gba": ".gba", "gamegear": ".gg", "mastersystem": ".sms", "megadrive": ".md", "genesis": ".md", "3do": ".chd", "psx": ".chd", "ps2": ".iso", "dreamcast": ".iso", "saturn": ".chd", "segacd": ".iso", "pcenginecd": ".chd", "ps3": ".iso", "xbox": ".xiso", "xbox360": ".iso"}
	for system, ext := range cases {
		t.Run(system, func(t *testing.T) {
			root, roms := t.TempDir(), t.TempDir()
			data := []byte("synthetic local payload " + system)
			name := "Game (USA)" + ext
			card(t, root, system, "Game", "Game", "Game (USA)", name, data)
			put(t, filepath.Join(roms, name), data)
			s := server(t, root, nil)
			assertLocalParity(t, root, roms, system, s.URL)
		})
	}
	for _, ext := range []string{".zip", ".7z"} {
		t.Run("MAME"+ext, func(t *testing.T) {
			root, roms := t.TempDir(), t.TempDir()
			arcade(t, root)
			put(t, filepath.Join(roms, "game1"+ext), []byte("observed container bytes"))
			s := server(t, root, nil)
			assertLocalParity(t, root, roms, "arcade", s.URL)
		})
	}
}

func TestRemotePlaylistAndPS3DescriptorParity(t *testing.T) {
	for _, system := range []string{"psx", "saturn", "dreamcast"} {
		t.Run(system, func(t *testing.T) {
			root, roms := t.TempDir(), t.TempDir()
			data := []byte("disc one")
			card(t, root, system, "Game", "Game", "Game (USA)", "Game (USA).chd", data)
			name := "Game.m3u"
			put(t, filepath.Join(roms, name, name), []byte("Game (USA).chd\n"))
			put(t, filepath.Join(roms, name, "Game (USA).chd"), data)
			s := server(t, root, nil)
			assertLocalParity(t, root, roms, system, s.URL)
		})
	}
	root, roms := t.TempDir(), t.TempDir()
	card(t, root, "ps3", "Game", "Game", "Game (USA)", "Game.iso", []byte("disc"))
	put(t, filepath.Join(roms, "Game.ps3"), []byte("NPUB00001\n"))
	s := server(t, root, nil)
	assertLocalParity(t, root, roms, "ps3", s.URL)
}

func TestRemoteAllBatchAndPreviewDoesNotDownload(t *testing.T) {
	root, roms := t.TempDir(), t.TempDir()
	data := []byte("Game Gear data")
	card(t, root, "gamegear", "Game", "Game", "Game (USA)", "Game.gg", data)
	put(t, filepath.Join(roms, "gamegear", "Game.gg"), data)
	arcade(t, root)
	put(t, filepath.Join(roms, "arcade", "game1.zip"), []byte("set"))
	put(t, filepath.Join(roms, "unknown", "one.bin"), []byte("unknown"))
	var gets atomic.Int32
	s := server(t, root, func(api http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/v1/assets/") {
				gets.Add(1)
			}
			api.ServeHTTP(w, r)
		})
	})
	job, err := Build(context.Background(), Options{API: s.URL, BatchOptions: exporter.BatchOptions{All: true, Options: exporter.Options{ROMRoot: roms, Out: filepath.Join(t.TempDir(), "out")}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(job.Batch.Plans) != 2 || len(job.Batch.Skipped) != 1 || gets.Load() != 0 {
		t.Fatalf("batch preview: %+v / downloads %d", job.Batch, gets.Load())
	}
	if err = job.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gets.Load() != 2 {
		t.Fatal("selected asset download count", gets.Load())
	}
}

func TestRemoteMoreThanOneThousandInputsAndAssetDedup(t *testing.T) {
	root, roms := t.TempDir(), t.TempDir()
	data := []byte("exact payload")
	card(t, root, "gamegear", "Game", "Game", "Game (USA)", "Original.gg", data)
	for i := 0; i < 1005; i++ {
		put(t, filepath.Join(roms, fmt.Sprintf("%04d.gg", i)), data)
	}
	var posts, gets atomic.Int32
	s := server(t, root, func(api http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/export" {
				posts.Add(1)
			}
			if strings.HasPrefix(r.URL.Path, "/v1/assets/") {
				gets.Add(1)
			}
			api.ServeHTTP(w, r)
		})
	})
	job, err := Build(context.Background(), Options{API: s.URL, BatchOptions: exporter.BatchOptions{Options: exporter.Options{Platform: "gamegear", ROMRoot: roms, Out: filepath.Join(t.TempDir(), "out")}}})
	if err != nil {
		t.Fatal(err)
	}
	if posts.Load() != 11 || job.Batch.Plans[0].Matched != 1005 {
		t.Fatal("collection ceiling or missing batches", posts.Load())
	}
	if err = job.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gets.Load() != 1 {
		t.Fatal("asset not deduplicated", gets.Load())
	}
}

func TestRemoteFailuresDoNotPublish(t *testing.T) {
	for _, failure := range []string{"asset", "ROM", "revision"} {
		t.Run(failure, func(t *testing.T) {
			root, roms := t.TempDir(), t.TempDir()
			data := []byte("payload")
			card(t, root, "gamegear", "Game", "Game", "Game (USA)", "Game.gg", data)
			path := filepath.Join(roms, "Game.gg")
			put(t, path, data)
			if failure == "revision" {
				for i := 0; i < 101; i++ {
					put(t, filepath.Join(roms, fmt.Sprintf("%03d.gg", i)), data)
				}
			}
			var posted atomic.Int32
			s := server(t, root, func(api http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if failure == "asset" && strings.HasPrefix(r.URL.Path, "/v1/assets/") {
						w.Write([]byte("corrupt"))
						return
					}
					api.ServeHTTP(w, r)
					if failure == "revision" && r.URL.Path == "/v1/export" && posted.Add(1) == 1 {
						put(t, filepath.Join(root, "_catalog-revision"), []byte("new revision"))
					}
				})
			})
			out := filepath.Join(t.TempDir(), "out")
			job, err := Build(context.Background(), Options{API: s.URL, BatchOptions: exporter.BatchOptions{Options: exporter.Options{Platform: "gamegear", ROMRoot: roms, Out: out}}})
			if failure == "revision" {
				if err == nil {
					t.Fatal("source change accepted across batches")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if failure == "ROM" {
				put(t, path, []byte("modified payload"))
			}
			if err = job.Apply(context.Background()); err == nil {
				t.Fatal("drift/corruption accepted")
			}
			if _, err = os.Stat(out); !os.IsNotExist(err) {
				t.Fatal("failed export published output")
			}
		})
	}
}

func TestAPIOriginAndRedirectDoNotExposeToken(t *testing.T) {
	for _, origin := range []string{"http://user:secret@localhost", "http://localhost/path", "http://localhost?token=secret", "file:///tmp"} {
		if _, err := newClient(origin, "", nil); err == nil {
			t.Fatal("unsafe API origin accepted")
		}
	}
	var leaked atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1) }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer origin.Close()
	c, err := newClient(origin.URL, strings.Repeat("t", 32), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.request(context.Background(), "GET", "/v1/platforms", nil); err == nil {
		t.Fatal("redirect followed")
	}
	if leaked.Load() != 0 {
		t.Fatal("token forwarded")
	}
}

func TestRemoteZIPProfileAndPackedParity(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(fmt.Sprint(packed), func(t *testing.T) {
			root, roms := t.TempDir(), t.TempDir()
			data := []byte("single cartridge payload")
			card(t, root, "gamegear", "Game", "Catalog Title", "Game (USA)", "Game.gg", data)
			path := filepath.Join(roms, "Game (USA).zip")
			f, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			z := zip.NewWriter(f)
			w, err := z.Create("Game.gg")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = w.Write(data); err != nil {
				t.Fatal(err)
			}
			if err = z.Close(); err != nil {
				t.Fatal(err)
			}
			if err = f.Close(); err != nil {
				t.Fatal(err)
			}
			if packed {
				catalogfstest.Pack(t, filepath.Join(root, "platforms", "gamegear"))
			}
			profile := filepath.Join(t.TempDir(), "policy.yaml")
			put(t, profile, []byte("schema_version: 1\ntarget: es-de\ntitle_mode: release\n"))
			s := server(t, root, nil)
			assertLocalParity(t, root, roms, "gamegear", s.URL, profile)
		})
	}
}
func TestRemoteCancellationDoesNotPublish(t *testing.T) {
	root, roms := t.TempDir(), t.TempDir()
	data := []byte("payload")
	card(t, root, "gamegear", "Game", "Game", "Game (USA)", "Game.gg", data)
	put(t, filepath.Join(roms, "Game.gg"), data)
	s := server(t, root, nil)
	options := Options{API: s.URL, BatchOptions: exporter.BatchOptions{Options: exporter.Options{Platform: "gamegear", ROMRoot: roms, Out: filepath.Join(t.TempDir(), "out")}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Build(ctx, options); err == nil {
		t.Fatal("cancelled build succeeded")
	}
	job, err := Build(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if err = job.Apply(ctx); err == nil {
		t.Fatal("cancelled apply succeeded")
	}
	if _, err = os.Stat(options.Out); !os.IsNotExist(err) {
		t.Fatal("cancelled apply published output")
	}
}
func TestRemoteMAMEMissingSnapshotSkipsOnlyBatch(t *testing.T) {
	root, roms := t.TempDir(), t.TempDir()
	arcade(t, root)
	if err := os.RemoveAll(filepath.Join(root, "bindings")); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(roms, "mame", "game1.zip"), []byte("set"))
	s := server(t, root, nil)
	options := Options{API: s.URL, BatchOptions: exporter.BatchOptions{All: true, Options: exporter.Options{ROMRoot: roms, Out: filepath.Join(t.TempDir(), "out")}}}
	job, err := Build(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if len(job.Batch.Plans) != 0 || len(job.Batch.Skipped) != 1 || !strings.Contains(job.Batch.Skipped[0].Reason, "snapshot") {
		t.Fatal(job.Batch)
	}
	options.All = false
	options.Platform = "mame"
	options.ROMRoot = filepath.Join(roms, "mame")
	if _, err = Build(context.Background(), options); err == nil {
		t.Fatal("single MAME silently skipped")
	}
}

func TestRemoteInvalidProfileFailsEvenWhenEverySystemIsSkipped(t *testing.T) {
	root, roms := t.TempDir(), t.TempDir()
	platform(t, root, "gamegear")
	put(t, filepath.Join(roms, "unknown", "file.txt"), []byte("unhandled"))
	s := server(t, root, nil)
	profile := filepath.Join(t.TempDir(), "profile.yaml")
	for _, contents := range []string{"", "unknown: true\n", "title_mode: filename\n"} {
		put(t, profile, []byte(contents))
		_, err := Build(context.Background(), Options{API: s.URL, BatchOptions: exporter.BatchOptions{All: true, Options: exporter.Options{ROMRoot: roms, Out: filepath.Join(t.TempDir(), "out"), ProfilePath: profile}}})
		if err == nil {
			t.Fatal("invalid profile ignored with empty input selection")
		}
	}
}
