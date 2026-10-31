package exportclient

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"rommeta/internal/catalogapi"
	"rommeta/internal/exporter"
)

func exportCHDBytes() []byte {
	data := make([]byte, 512)
	copy(data, "MComprHD")
	binary.BigEndian.PutUint32(data[8:12], 124)
	binary.BigEndian.PutUint32(data[12:16], 5)
	binary.BigEndian.PutUint64(data[32:40], 2448*100)
	binary.BigEndian.PutUint64(data[40:48], 124)
	binary.BigEndian.PutUint32(data[56:60], 2448*8)
	binary.BigEndian.PutUint32(data[60:64], 2448)
	data[64], data[84] = 1, 5
	return data
}

func TestNetworkCHDHeadersFallbackAndLegacyCapabilities(t *testing.T) {
	for _, mode := range []string{"headers", "container", "force-hash", "legacy-server", "conflicting-variant"} {
		t.Run(mode, func(t *testing.T) {
			root, roms := t.TempDir(), t.TempDir()
			data := exportCHDBytes()
			card(t, root, "saturn", "Game", "Game", "Game (USA)", "Game (USA).chd", data)
			game := filepath.Join(root, "platforms", "saturn", "Game")
			if mode == "headers" || mode == "force-hash" || mode == "legacy-server" {
				put(t, filepath.Join(game, "fingerprints.yaml"), []byte("items: []\n"))
			}
			name := "Game (USA).chd"
			if mode == "container" {
				name = "Renamed.chd"
			}
			if mode == "conflicting-variant" {
				put(t, filepath.Join(game, "game.yaml"), []byte("title: Game\nreleases:\n  Game (USA): {}\nvariants:\n  English: {kind: translation, based_on: 'Game (USA)'}\n"))
				fps, err := os.ReadFile(filepath.Join(game, "fingerprints.yaml"))
				if err != nil {
					t.Fatal(err)
				}
				fps = append(fps, []byte(fmt.Sprintf("  - variant: English\n    file: {kind: chd-combined-sha1, sha1: '%x'}\n", data[84:104]))...)
				put(t, filepath.Join(game, "fingerprints.yaml"), fps)
			}
			put(t, filepath.Join(roms, name), data)
			var headerRequests, payloadRequests atomic.Int32
			s := server(t, root, func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/v1/platforms" && mode == "legacy-server" {
						reply := httptest.NewRecorder()
						next.ServeHTTP(reply, r)
						var doc map[string]any
						json.Unmarshal(reply.Body.Bytes(), &doc)
						delete(doc, "export_features")
						w.Header().Set("Content-Type", "application/json")
						json.NewEncoder(w).Encode(doc)
						return
					}
					if r.URL.Path == "/v1/export" {
						body, err := io.ReadAll(r.Body)
						if err != nil {
							t.Error(err)
							return
						}
						r.Body = io.NopCloser(bytes.NewReader(body))
						var q catalogapi.ExportRequest
						if err = json.Unmarshal(body, &q); err != nil {
							t.Error(err)
							return
						}
						for _, input := range q.Inputs {
							if input.Kind == "disc" {
								headerRequests.Add(1)
							}
							if input.Kind == "payload" {
								payloadRequests.Add(1)
							}
						}
					}
					next.ServeHTTP(w, r)
				})
			})
			options := exporter.Options{ROMRoot: roms, Platform: "saturn", Out: filepath.Join(t.TempDir(), "output"), HashROMs: mode == "force-hash"}
			job, err := Build(context.Background(), Options{API: s.URL, BatchOptions: exporter.BatchOptions{Options: options}})
			if err != nil {
				t.Fatal(err)
			}
			record := job.Batch.Plans[0].Records[0]
			if len(record.HashRequired) != 0 {
				t.Fatal("unresolved hash request")
			}
			switch mode {
			case "headers":
				if record.Payload != nil || record.SHA1 != "" || headerRequests.Load() != 1 || payloadRequests.Load() != 0 {
					t.Fatalf("not bounded: %+v", record)
				}
			case "container", "conflicting-variant":
				if record.Payload == nil || headerRequests.Load() != 1 || payloadRequests.Load() != 1 {
					t.Fatalf("missing selective retry: %+v", record)
				}
			default:
				if record.Payload == nil || headerRequests.Load() != 0 || payloadRequests.Load() != 1 {
					t.Fatalf("wrong compatibility/explicit mode: %+v", record)
				}
			}
			if mode == "container" && record.MatchedBy != "hash" {
				t.Fatal("rename lost exact binding", record)
			}
			if mode == "conflicting-variant" && record.Status != "ambiguous" {
				t.Fatal("conflicting evidence was hidden", record)
			}
			if err = job.Apply(context.Background()); err != nil {
				t.Fatal(err)
			}
			if mode == "headers" || mode == "container" {
				assertLocalParity(t, root, roms, "saturn", s.URL)
			}
		})
	}
}

func TestNetworkCHDPlaylistParityAndHeaderDrift(t *testing.T) {
	root, roms := t.TempDir(), t.TempDir()
	data := exportCHDBytes()
	card(t, root, "saturn", "Game", "Game", "Game (USA) (Disc 1)", "Game (USA) (Disc 1).chd", data)
	game := filepath.Join(root, "platforms", "saturn", "Game")
	put(t, filepath.Join(game, "game.yaml"), []byte("title: Game\nreleases:\n  Game (USA) (Disc 1): {}\n  Game (USA) (Disc 2): {}\n"))
	put(t, filepath.Join(game, "fingerprints.yaml"), []byte("items: []\n"))
	dir := filepath.Join(roms, "Game.m3u")
	put(t, filepath.Join(dir, "Game.m3u"), []byte("Game (USA) (Disc 1).chd\nGame (USA) (Disc 2).chd\n"))
	for i := 1; i <= 2; i++ {
		put(t, filepath.Join(dir, fmt.Sprintf("Game (USA) (Disc %d).chd", i)), data)
	}
	s := server(t, root, nil)
	assertLocalParity(t, root, roms, "saturn", s.URL)
	options := exporter.Options{ROMRoot: roms, Platform: "saturn", Out: filepath.Join(t.TempDir(), "changed")}
	job, err := Build(context.Background(), Options{API: s.URL, BatchOptions: exporter.BatchOptions{Options: options}})
	if err != nil {
		t.Fatal(err)
	}
	record := job.Batch.Plans[0].Records[0]
	if record.Status != "matched" || len(record.Playlist) != 2 || record.Playlist[0].Disc == nil || record.Playlist[0].SHA1 != "" {
		t.Fatalf("unexpected playlist proof: %+v", record)
	}
	path := filepath.Join(dir, "Game (USA) (Disc 2).chd")
	info, _ := os.Stat(path)
	data[84] = 44
	put(t, path, data)
	os.Chtimes(path, info.ModTime(), info.ModTime())
	if err = job.Apply(context.Background()); err == nil || !strings.Contains(err.Error(), "CHD changed") {
		t.Fatal("header drift accepted", err)
	}
	if _, err = os.Stat(job.Batch.Out); !os.IsNotExist(err) {
		t.Fatal("changed output published")
	}
}

func TestNetworkRejectsCHDProofPromotionAndUnsafeHashRequest(t *testing.T) {
	for _, mode := range []string{"payload", "header", "matched-by", "hash-path"} {
		t.Run(mode, func(t *testing.T) {
			root, roms := t.TempDir(), t.TempDir()
			data := exportCHDBytes()
			card(t, root, "saturn", "Game", "Game", "Game (USA)", "Game (USA).chd", data)
			put(t, filepath.Join(root, "platforms", "saturn", "Game", "fingerprints.yaml"), []byte("items: []\n"))
			put(t, filepath.Join(roms, "Game (USA).chd"), data)
			s := server(t, root, func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/v1/export" {
						next.ServeHTTP(w, r)
						return
					}
					reply := httptest.NewRecorder()
					next.ServeHTTP(reply, r)
					var response catalogapi.ExportResponse
					if err := json.Unmarshal(reply.Body.Bytes(), &response); err != nil {
						t.Error(err)
						return
					}
					record := &response.Plan.Records[0]
					switch mode {
					case "payload":
						record.Payload = &exporter.PayloadProof{}
					case "header":
						record.Disc.Files[0].Header = strings.Repeat("a", 248)
					case "matched-by":
						record.MatchedBy = "hash"
					case "hash-path":
						record.HashRequired = []string{"../outside.chd"}
					}
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(response)
				})
			})
			options := exporter.Options{ROMRoot: roms, Platform: "saturn", Out: filepath.Join(t.TempDir(), "output")}
			if _, err := Build(context.Background(), Options{API: s.URL, BatchOptions: exporter.BatchOptions{Options: options}}); err == nil {
				t.Fatal("altered CHD evidence accepted", mode)
			}
		})
	}
}

func TestNetworkCHDHashFallbackPinsRevisionAndProfile(t *testing.T) {
	for _, mode := range []string{"revision", "profile"} {
		t.Run(mode, func(t *testing.T) {
			root, roms := t.TempDir(), t.TempDir()
			data := exportCHDBytes()
			card(t, root, "saturn", "Game", "Game", "Game (USA)", "Game (USA).chd", data)
			put(t, filepath.Join(roms, "Game (USA).chd"), data)
			var requests atomic.Int32
			s := server(t, root, func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/v1/export" {
						next.ServeHTTP(w, r)
						return
					}
					reply := httptest.NewRecorder()
					next.ServeHTTP(reply, r)
					var response catalogapi.ExportResponse
					if err := json.Unmarshal(reply.Body.Bytes(), &response); err != nil {
						t.Error(err)
						return
					}
					if requests.Add(1) == 2 {
						if mode == "revision" {
							response.Revision += "-changed"
						} else {
							response.Plan.Profile.SchemaVersion++
						}
					}
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(response)
				})
			})
			options := exporter.Options{ROMRoot: roms, Platform: "saturn", Out: filepath.Join(t.TempDir(), "output")}
			if _, err := Build(context.Background(), Options{API: s.URL, BatchOptions: exporter.BatchOptions{Options: options}}); err == nil || !strings.Contains(err.Error(), "changed") {
				t.Fatal("fallback drift accepted", mode, err)
			}
			if requests.Load() != 2 {
				t.Fatal("fallback was not exercised")
			}
		})
	}
}
