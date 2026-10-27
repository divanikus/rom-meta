package exporter

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogfstest"
)

func indexFixture(t *testing.T, packed bool) (*LookupCatalog, string) {
	t.Helper()
	root := t.TempDir()
	platform := filepath.Join(root, "platforms", "Example")
	for i := 0; i < 3; i++ {
		indexWriteGame(t, platform, fmt.Sprintf("Game%d", i), fmt.Sprintf("Example %d", i), i)
	}
	if packed {
		catalogfstest.Pack(t, platform)
	}
	return &LookupCatalog{Root: root, Platforms: []LookupPlatform{{Platform: "Example", System: "gamegear", Supported: true}}}, platform
}
func indexWriteGame(t *testing.T, platform, folder, title string, id int) {
	t.Helper()
	path := filepath.Join(platform, folder)
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	docs := map[string]string{
		"game.yaml":         fmt.Sprintf("title: %s\naliases: [Alias %d]\nreleases:\n  Release %d:\n    authority: {entry_name: Authority %d}\n", title, id, id, id),
		"fingerprints.yaml": fmt.Sprintf("items:\n  - release: Release %d\n    file: {name: Game%d.gg, size: %d, sha1: '%040x'}\n", id, id, id+1, id+1),
	}
	for name, data := range docs {
		if err := catalogfs.WriteFile(filepath.Join(path, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
func openIndexTest(t *testing.T, c *LookupCatalog, cached, rebuilt int) *LookupResolver {
	t.Helper()
	resolver, err := c.Open("Example")
	if err != nil {
		t.Fatal(err)
	}
	if resolver.IndexStats.Cached != cached || resolver.IndexStats.Rebuilt != rebuilt {
		t.Fatalf("stats %+v; want %d cached, %d rebuilt", resolver.IndexStats, cached, rebuilt)
	}
	return resolver
}
func indexShape(index *catalogIndex) map[string][]string {
	shape := map[string][]string{}
	for key, gs := range index.GamesByName {
		for _, g := range gs {
			shape["game:"+key] = append(shape["game:"+key], g.path)
		}
	}
	for key, fps := range index.ByName {
		for _, fp := range fps {
			shape["name:"+key] = append(shape["name:"+key], fp.game.path+"/"+fp.release)
		}
	}
	for size, fps := range index.BySize {
		for _, fp := range fps {
			key := fmt.Sprintf("hash:%d", size)
			shape[key] = append(shape[key], fmt.Sprintf("%s/%s:%t:%s:%s:%s:%s", fp.game.path, fp.release, fp.chd, fp.sha1, fp.sha256, fp.md5, fp.crc))
		}
	}
	return shape
}
func TestLookupIndexReuseRefreshAndPrune(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(fmt.Sprint(packed), func(t *testing.T) {
			c, platform := indexFixture(t, packed)
			first := openIndexTest(t, c, 0, 3)
			original, err := buildIndex(c.Root, platform, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(indexShape(original), indexShape(first.index)) {
				t.Fatal("cold index changed recognition")
			}
			again := openIndexTest(t, c, 3, 0)
			if !reflect.DeepEqual(first.DocumentDigests, again.DocumentDigests) || !reflect.DeepEqual(indexShape(first.index), indexShape(again.index)) {
				t.Fatal("persisted index changed recognition/digests")
			}
			for _, fps := range again.index.BySize {
				if fps[0].game.root != nil {
					t.Fatal("unselected YAML was eagerly loaded")
				}
			}
			// Unselected media is irrelevant to both discovery and freshness.
			if packed {
				if err := os.WriteFile(filepath.Join(platform, "Game0", "video.emumovies.zip"), []byte("not a ZIP"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			openIndexTest(t, c, 3, 0)
			if packed {
				c.CacheDir = "-"
				openIndexTest(t, c, 0, 3)
				c.CacheDir = ""
			}
			// Change only fingerprints. A cached negative/unique result must not hide ambiguity.
			fp, err := catalogfs.ReadFile(filepath.Join(platform, "Game0", "fingerprints.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			if err = catalogfs.WriteFile(filepath.Join(platform, "Game1", "fingerprints.yaml"), []byte(strings.ReplaceAll(string(fp), "Release 0", "Release 1")), 0600); err != nil {
				t.Fatal(err)
			}
			changed := openIndexTest(t, c, 2, 1)
			if len(changed.index.BySize[1]) != 2 {
				t.Fatal("new duplicate fingerprint hidden")
			}
			indexWriteGame(t, platform, "Added", "New game", 9)
			openIndexTest(t, c, 3, 1)
			if err = os.RemoveAll(filepath.Join(platform, "Added")); err != nil {
				t.Fatal(err)
			}
			openIndexTest(t, c, 3, 0)
			// Removing the index restores the same result from source.
			files, _ := filepath.Glob(filepath.Join(c.Root, "platforms", "Example", "_index.sqlite"))
			if len(files) != 1 {
				t.Fatal(files)
			}
			if err = os.Remove(files[0]); err != nil {
				t.Fatal(err)
			}
			openIndexTest(t, c, 0, 3)
		})
	}
}
func TestLookupIndexCorruptionUnavailableAndDisabled(t *testing.T) {
	c, _ := indexFixture(t, true)
	openIndexTest(t, c, 0, 3)
	files, _ := filepath.Glob(filepath.Join(c.Root, "platforms", "Example", "_index.sqlite"))
	db, err := sql.Open("sqlite", files[0])
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("UPDATE lookup_bundles_v1 SET payload='broken' WHERE folder='Game0'")
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	openIndexTest(t, c, 2, 1)
	// A completely unreadable database also falls back to the source.
	if err = os.WriteFile(files[0], []byte("broken database"), 0600); err != nil {
		t.Fatal(err)
	}
	openIndexTest(t, c, 0, 3)
	blocker := filepath.Join(t.TempDir(), "file")
	if err = os.WriteFile(blocker, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	c.CacheDir = blocker
	openIndexTest(t, c, 0, 3)
	c.CacheDir = "-"
	openIndexTest(t, c, 0, 3)
}
func TestLookupIndexLayoutChangeAndSelectedDigestGuard(t *testing.T) {
	c, platform := indexFixture(t, false)
	openIndexTest(t, c, 0, 3)
	catalogfstest.Pack(t, platform)
	openIndexTest(t, c, 0, 3)
	cached := openIndexTest(t, c, 3, 0)
	g := cached.index.BySize[1][0].game
	data, err := catalogfs.ReadFile(filepath.Join(g.path, "game.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := catalogfs.WriteFile(filepath.Join(g.path, "game.yaml"), []byte(strings.Replace(string(data), "Example 0", "Changed game", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := attachRecord(c.Root, g, "", new(Record), c.Profile, "gamegear"); err == nil {
		t.Fatal("stale cached card accepted")
	}
	openIndexTest(t, c, 2, 1)
}

func TestLookupIndexTravelsWithPlatform(t *testing.T) {
	c, platform := indexFixture(t, true)
	first := openIndexTest(t, c, 0, 3)
	root := t.TempDir()
	dest := filepath.Join(root, "platforms", "Example")
	err := filepath.WalkDir(platform, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(platform, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err = os.WriteFile(target, data, 0600); err != nil {
			return err
		}
		return os.Chtimes(target, info.ModTime(), info.ModTime())
	})
	if err != nil {
		t.Fatal(err)
	}
	moved := &LookupCatalog{Root: root, Platforms: c.Platforms}
	again := openIndexTest(t, moved, 3, 0)
	if !reflect.DeepEqual(first.DocumentDigests, again.DocumentDigests) {
		t.Fatal("portable digests changed")
	}
	for _, fps := range again.index.BySize {
		if !strings.HasPrefix(fps[0].game.path, dest+string(os.PathSeparator)) {
			t.Fatal("old absolute path retained")
		}
	}
}
func TestLookupIndexSameStampSelectedChangeFailsClosed(t *testing.T) {
	c, platform := indexFixture(t, false)
	openIndexTest(t, c, 0, 3)
	path := filepath.Join(platform, "Game0", "game.yaml")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "Example 0", "ExAmple 0", 1))
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	cached := openIndexTest(t, c, 3, 0)
	g := cached.index.BySize[1][0].game
	if err = attachRecord(c.Root, g, "", new(Record), c.Profile, "gamegear"); err == nil {
		t.Fatal("same-stamp stale card accepted")
	}
}

func TestRefreshPlatformIndexReportsUnavailableStorage(t *testing.T) {
	c, platform := indexFixture(t, false)
	if err := os.Mkdir(filepath.Join(platform, "_index.sqlite"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := RefreshPlatformIndex(c.Root, "Example"); err == nil {
		t.Fatal("writer hid index refresh failure")
	}
	// The read-only consumer still falls back to authoritative source files.
	openIndexTest(t, c, 0, 3)
}

func TestLookupIndexAcceptsLegacyNanosecondStampsAtSMBPrecision(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(fmt.Sprint(packed), func(t *testing.T) {
			c, platform := indexFixture(t, packed)
			first := openIndexTest(t, c, 0, 3)
			db, err := sql.Open("sqlite", filepath.Join(platform, "_index.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			rows, err := db.Query("SELECT folder,stamp FROM lookup_bundles_v1")
			if err != nil {
				t.Fatal(err)
			}
			stamps := map[string]string{}
			for rows.Next() {
				var folder, stamp string
				if err := rows.Scan(&folder, &stamp); err != nil {
					t.Fatal(err)
				}
				parts := strings.Split(stamp, "|")
				for i, part := range parts {
					f := strings.Split(part, ":")
					if len(f) == 3 && (f[0] == "metadata.zip" || f[0] == "game.yaml" || f[0] == "fingerprints.yaml") {
						ns, e := strconv.ParseInt(f[2], 10, 64)
						if e != nil {
							t.Fatal(e)
						}
						f[2] = strconv.FormatInt(ns-ns%100+37, 10)
						parts[i] = strings.Join(f, ":")
					}
				}
				stamps[folder] = strings.Join(parts, "|")
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			rows.Close()
			for folder, stamp := range stamps {
				if _, err := db.Exec("UPDATE lookup_bundles_v1 SET stamp=? WHERE folder=?", stamp, folder); err != nil {
					t.Fatal(err)
				}
			}
			db.Close()
			second := openIndexTest(t, c, 3, 0)
			if !reflect.DeepEqual(indexShape(first.index), indexShape(second.index)) {
				t.Fatal("portable reuse changed recognition")
			}
		})
	}
}
func TestPortableLookupStampKeepsMaterialDifferences(t *testing.T) {
	base := "Game:true|metadata.zip:2787:1792796379670315621|game.yaml:missing|fingerprints.yaml:missing"
	same := strings.ReplaceAll(base, "1792796379670315621", "1792796379670315600")
	if portableLookupStamp(base) != portableLookupStamp(same) {
		t.Fatal("SMB rounding caused rebuild")
	}
	for _, other := range []string{strings.ReplaceAll(base, "1792796379670315621", "1792796379670315700"), strings.ReplaceAll(base, "2787:", "2788:"), strings.ReplaceAll(base, "Game:true", "Game:false"), strings.ReplaceAll(base, "game.yaml:missing", "game.yaml:1:100")} {
		if portableLookupStamp(base) == portableLookupStamp(other) {
			t.Fatal("lost change", other)
		}
	}
}

func TestLookupReleasesParsedCardsBetweenRequests(t *testing.T) {
	for _, packed := range []bool{false, true} {
		t.Run(fmt.Sprint(packed), func(t *testing.T) {
			catalog, _ := indexFixture(t, packed)
			resolver := openIndexTest(t, catalog, 0, 3)
			assertReleased := func() {
				t.Helper()
				for _, games := range resolver.index.GamesByName {
					for _, game := range games {
						if game.root != nil || game.mediaStyles != nil {
							t.Fatal("lookup retains a full parsed card after its request", game.path)
						}
					}
				}
			}
			observe := func(id int) ObservedInput {
				name := fmt.Sprintf("Authority %d.zip", id)
				return ObservedInput{ROM: name, Outer: FileDigest{Size: 4}, Catalog: &CatalogObservation{Kind: "name-stat-v1", Files: []CatalogFile{{Name: name, Size: 4, MtimeNS: 1}}}}
			}
			for round := 0; round < 2; round++ {
				for id := 0; id < 3; id++ {
					plan, err := resolver.ResolveBindings([]ObservedInput{observe(id)}, true)
					if err != nil || len(plan.Records) != 1 || plan.Records[0].Status != "matched" || plan.Records[0].Fields["title"].Value == "" {
						t.Fatal("metadata changed while reusing the compact index", plan, err)
					}
					assertReleased()
				}
			}
			bad := observe(1)
			bad.ROM = "../unsafe.zip"
			if _, err := resolver.ResolveBindings([]ObservedInput{observe(0), bad}, true); err == nil {
				t.Fatal("invalid observation accepted")
			}
			assertReleased()
		})
	}
}
