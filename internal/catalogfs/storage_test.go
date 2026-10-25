package catalogfs_test

import (
	"archive/zip"
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogfstest"
	"rommeta/internal/sourceyaml"
)

func put(t *testing.T, n, s string) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(n), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(n, []byte(s), 0644); e != nil {
		t.Fatal(e)
	}
}
func fixture(t *testing.T) (string, string, map[string]string) {
	root := t.TempDir()
	p := filepath.Join(root, "platforms", "Example")
	game := filepath.Join(p, "_Leading game")
	files := map[string]string{"game.yaml": "# edit me\r\ntitle: Example\r\nmanual: {note: keep}\r\n", "fingerprints.yaml": "items: []\n", "notes.txt": "private note", "releases/USA/game.yaml": "historical YAML\n", "media/USA/box-front.usa.launchbox.png": "art", "media/video.emumovies.mp4": "0123456789", "media/_fold/old/screenshot.emumovies.png": "folded"}
	for n, b := range files {
		put(t, filepath.Join(game, filepath.FromSlash(n)), b)
	}
	put(t, filepath.Join(p, "_platform.yaml"), "name: Example\n")
	return root, game, files
}
func TestLogicalInventoryMixedPlatformsAndOriginalBytes(t *testing.T) {
	root, game, want := fixture(t)
	put(t, filepath.Join(root, "platforms", "Loose", "Other", "game.yaml"), "title: Loose")
	put(t, filepath.Join(root, "platforms", "Example.old", "Old", "game.yaml"), "title: old")
	catalogfstest.Pack(t, filepath.Dir(game))
	got := map[string]string{}
	err := catalogfs.WalkDir(game, func(n string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		r, _ := filepath.Rel(game, n)
		f, e := catalogfs.Open(n)
		if e != nil {
			return e
		}
		var b bytes.Buffer
		_, e = io.Copy(&b, f)
		f.Close()
		got[filepath.ToSlash(r)] = b.String()
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payloads: %#v", got)
	}
	entries, e := catalogfs.ReadDir(filepath.Join(root, "platforms"))
	if e != nil || len(entries) != 2 {
		t.Fatal(entries, e)
	}
	physical, e := os.ReadDir(game)
	if e != nil {
		t.Fatal(e)
	}
	for _, d := range physical {
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".zip") {
			t.Fatal("unexpected loose member", d.Name())
		}
	}
	for n := range want {
		if _, e := catalogfs.EvalSymlinks(filepath.Join(game, filepath.FromSlash(n))); e != nil {
			t.Fatal(e)
		}
	}
}
func TestUpdatesCacheStampsAndRawUnchangedMedia(t *testing.T) {
	_, game, _ := fixture(t)
	catalogfstest.Pack(t, filepath.Dir(game))
	art := filepath.Join(game, "artwork.launchbox.zip")
	before, _ := os.ReadFile(art)
	name := filepath.Join(game, "game.yaml")
	first, e := catalogfs.Stat(name)
	if e != nil {
		t.Fatal(e)
	}
	if e = sourceyaml.WriteSourceMetadata(name, "igdb", "    title: Updated\n"); e != nil {
		t.Fatal(e)
	}
	b, e := catalogfs.ReadFile(name)
	if e != nil || !strings.Contains(string(b), "manual: {note: keep}") || !strings.Contains(string(b), "Updated") {
		t.Fatal(string(b), e)
	}
	second, e := catalogfs.Stat(name)
	if e != nil || catalogfs.CacheStamp(first) == catalogfs.CacheStamp(second) || catalogfs.SameFile(first, second) {
		t.Fatal("stale logical stamp", e)
	}
	after, _ := os.ReadFile(art)
	if !bytes.Equal(before, after) {
		t.Fatal("metadata update rewrote media")
	}
	target := filepath.Join(game, "media", "new.launchbox.png")
	if e = catalogfs.MkdirAll(filepath.Dir(target), 0755); e != nil {
		t.Fatal(e)
	}
	put(t, target+".tmp", "downloaded")
	if e = catalogfs.Rename(target+".tmp", target); e != nil {
		t.Fatal(e)
	}
	b, e = catalogfs.ReadFile(target)
	if e != nil || string(b) != "downloaded" {
		t.Fatal(string(b), e)
	}
	if e = catalogfs.Rename(target, filepath.Join(game, "media", "renamed.launchbox.png")); e != nil {
		t.Fatal(e)
	}
	if e = catalogfs.Remove(filepath.Join(game, "media", "renamed.launchbox.png")); e != nil {
		t.Fatal(e)
	}
	out, e := catalogfs.OpenFile(name, os.O_WRONLY|os.O_TRUNC, 0644)
	if e != nil {
		t.Fatal(e)
	}
	out.Write([]byte("title: Changed\n"))
	if e = out.Close(); e != nil {
		t.Fatal(e)
	}
	b, e = catalogfs.ReadFile(name)
	if e != nil || string(b) != "title: Changed\n" {
		t.Fatal(string(b), e)
	}
	if e = catalogfs.RemoveAll(filepath.Join(game, "media", "_fold")); e != nil {
		t.Fatal(e)
	}
	if _, e = catalogfs.Stat(filepath.Join(game, "media", "_fold", "old", "screenshot.emumovies.png")); !os.IsNotExist(e) {
		t.Fatal(e)
	}
}
func TestStoredAndManuallyDeflatedVideoSeeking(t *testing.T) {
	for _, method := range []uint16{zip.Store, zip.Deflate} {
		t.Run(map[uint16]string{zip.Store: "stored", zip.Deflate: "manual-deflate"}[method], func(t *testing.T) {
			_, game, _ := fixture(t)
			catalogfstest.Pack(t, filepath.Dir(game))
			n := filepath.Join(game, "video.emumovies.zip")
			f, e := os.Create(n)
			if e != nil {
				t.Fatal(e)
			}
			z := zip.NewWriter(f)
			w, e := z.CreateHeader(&zip.FileHeader{Name: "media/video.emumovies.mp4", Method: method})
			if e != nil {
				t.Fatal(e)
			}
			w.Write([]byte("0123456789"))
			z.Close()
			f.Close()
			in, e := catalogfs.Open(filepath.Join(game, "media", "video.emumovies.mp4"))
			if e != nil {
				t.Fatal(e)
			}
			defer in.Close()
			if _, e = in.Seek(3, io.SeekStart); e != nil {
				t.Fatal(e)
			}
			b := make([]byte, 4)
			if _, e = io.ReadFull(in, b); e != nil || string(b) != "3456" {
				t.Fatal(string(b), e)
			}
			in.Seek(0, io.SeekStart)
			var out bytes.Buffer
			io.Copy(&out, in)
			if out.String() != "0123456789" {
				t.Fatal(out.String())
			}
		})
	}
}
func TestBadArchivesAndLooseDuplicatesFail(t *testing.T) {
	for _, name := range []string{"../escape.yaml", "media\\escape.png", "media/box-front.launchbox.png", "game.yaml"} {
		t.Run(name, func(t *testing.T) {
			_, game, _ := fixture(t)
			catalogfstest.Pack(t, filepath.Dir(game))
			f, e := os.Create(filepath.Join(game, "metadata.zip"))
			if e != nil {
				t.Fatal(e)
			}
			z := zip.NewWriter(f)
			for i := 0; i < 2; i++ {
				w, e := z.Create(name)
				if e != nil {
					t.Fatal(e)
				}
				w.Write([]byte("x"))
			}
			z.Close()
			f.Close()
			if _, e = catalogfs.ReadDir(game); e == nil {
				t.Fatal("accepted malformed or duplicate member")
			}
		})
	}
	_, game, _ := fixture(t)
	catalogfstest.Pack(t, filepath.Dir(game))
	put(t, filepath.Join(game, "game.yaml"), "loose duplicate")
	if _, e := catalogfs.ReadFile(filepath.Join(game, "game.yaml")); e == nil {
		t.Fatal("accepted loose duplicate")
	}
}
func TestConcurrentWritersAndConflictPreserveOldData(t *testing.T) {
	_, game, _ := fixture(t)
	catalogfstest.Pack(t, filepath.Dir(game))
	name := filepath.Join(game, "game.yaml")
	original, _ := catalogfs.ReadFile(name)
	if e := os.Mkdir(filepath.Join(game, ".rommeta-write-lock"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := catalogfs.WriteFile(name, []byte("rejected"), 0644); e == nil {
		t.Fatal("ignored writer lock")
	}
	os.Remove(filepath.Join(game, ".rommeta-write-lock"))
	now, _ := catalogfs.ReadFile(name)
	if !bytes.Equal(original, now) {
		t.Fatal("changed on rejected write")
	}
	f, e := catalogfs.OpenFile(name, os.O_WRONLY|os.O_TRUNC, 0644)
	if e != nil {
		t.Fatal(e)
	}
	f.Write([]byte("stale"))
	if e = catalogfs.WriteFile(name, []byte("title: concurrent\n"), 0644); e != nil {
		t.Fatal(e)
	}
	if e = f.Close(); e == nil {
		t.Fatal("stale close overwrote concurrent change")
	}
	now, _ = catalogfs.ReadFile(name)
	if string(now) != "title: concurrent\n" {
		t.Fatal(string(now))
	}
}

// Golden bytes were produced independently with Python/system-zlib using the
// upstream TorrentZip reference.py, not this application's writer.
func TestTorrentZipReferenceGoldenAndNormalization(t *testing.T) {
	game := filepath.Join(t.TempDir(), "platforms", "Example", "Game")
	if e := os.MkdirAll(game, 0755); e != nil {
		t.Fatal(e)
	}
	stage := t.TempDir()
	put(t, filepath.Join(stage, "game.yaml"), "title: Example\n")
	put(t, filepath.Join(stage, "notes.txt"), "hand edited\n")
	files := map[string]string{"game.yaml": filepath.Join(stage, "game.yaml"), "notes/cafe.txt": filepath.Join(stage, "notes.txt")}
	if e := catalogfs.PutFiles(game, files, nil); e != nil {
		t.Fatal(e)
	}
	got, e := os.ReadFile(filepath.Join(game, "metadata.zip"))
	if e != nil {
		t.Fatal(e)
	}
	want, e := hex.DecodeString("504b030414000200080000bc98211088933c110000000f0000000900000067616d652e79616d6c2bc92cc949b55270ad48cc2dc849e50200504b030414000200080000bc9821d351ca660e0000000c0000000e0000006e6f7465732f636166652e747874cb48cc4b51484dc92c494de10200504b0102000014000200080000bc98211088933c110000000f00000009000000000000000000000000000000000067616d652e79616d6c504b0102000014000200080000bc9821d351ca660e0000000c0000000e00000000000000000000000000380000006e6f7465732f636166652e747874504b0506000000000200020073000000720000001600544f5252454e545a49505045442d3546353744353936")
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("TorrentZip differs from independent zlib reference")
	}
	if e = catalogfs.PutFiles(game, files, nil); e != nil {
		t.Fatal(e)
	}
	second, e := os.ReadFile(filepath.Join(game, "metadata.zip"))
	if e != nil || !bytes.Equal(got, second) {
		t.Fatal("identical logical content did not produce identical TorrentZip", e)
	}
}
func TestTorrentZipUnicodeNamesRoundTrip(t *testing.T) {
	game := filepath.Join(t.TempDir(), "platforms", "Example", "Game")
	stage := t.TempDir()
	put(t, filepath.Join(stage, "input"), "preserved")
	names := []string{"notes/café.txt", "releases/日本/game.yaml"}
	files := map[string]string{}
	for _, n := range names {
		files[n] = filepath.Join(stage, "input")
	}
	if e := os.MkdirAll(game, 0755); e != nil {
		t.Fatal(e)
	}
	if e := catalogfs.PutFiles(game, files, nil); e != nil {
		t.Fatal(e)
	}
	put(t, filepath.Join(filepath.Dir(game), catalogfs.Marker), catalogfs.MarkerContent)
	for _, n := range names {
		b, e := catalogfs.ReadFile(filepath.Join(game, filepath.FromSlash(n)))
		if e != nil || string(b) != "preserved" {
			t.Fatal(n, string(b), e)
		}
	}
}

func TestCaseCollidingUpdateLeavesArchiveUntouched(t *testing.T) {
	_, game, _ := fixture(t)
	catalogfstest.Pack(t, filepath.Dir(game))
	name := filepath.Join(game, "metadata.zip")
	before, e := os.ReadFile(name)
	if e != nil {
		t.Fatal(e)
	}
	if e = catalogfs.WriteFile(filepath.Join(game, "GAME.yaml"), []byte("bad"), 0644); e == nil {
		t.Fatal("accepted case-colliding write")
	}
	after, e := os.ReadFile(name)
	if e != nil || !bytes.Equal(before, after) {
		t.Fatal("rejected change altered old archive", e)
	}
}

func TestUnsafeUpdateFailsBeforeCreatingGame(t *testing.T) {
	game := filepath.Join(t.TempDir(), "platforms", "Example", "Game")
	for _, member := range []string{"../escape", "/root", "media/../escape"} {
		if e := catalogfs.PutFiles(game, map[string]string{member: "unused"}, nil); e == nil {
			t.Fatal(member)
		}
	}
	if _, e := os.Stat(game); !os.IsNotExist(e) {
		t.Fatal("unsafe update created output", e)
	}
}

func TestSameMemberRenameAndSelectedMediaCache(t *testing.T) {
	_, game, _ := fixture(t)
	catalogfstest.Pack(t, filepath.Dir(game))
	name := filepath.Join(game, "game.yaml")
	before, e := catalogfs.ReadFile(name)
	if e != nil {
		t.Fatal(e)
	}
	if e = catalogfs.Rename(name, name); e != nil {
		t.Fatal(e)
	}
	after, e := catalogfs.ReadFile(name)
	if e != nil || !bytes.Equal(before, after) {
		t.Fatal("self-rename changed data", e)
	}
	video := filepath.Join(game, "media", "large.emumovies.mp4")
	payload := bytes.Repeat([]byte("video"), 300000)
	if e = catalogfs.WriteFile(video, payload, 0644); e != nil {
		t.Fatal(e)
	}
	first, e := catalogfs.Open(video)
	if e != nil {
		t.Fatal(e)
	}
	cached := first.File.Name()
	first.Close()
	second, e := catalogfs.Open(video)
	if e != nil {
		t.Fatal(e)
	}
	if second.File.Name() != cached {
		t.Fatal("selected media cache was not reused")
	}
	second.Seek(100, io.SeekStart)
	b := make([]byte, 5)
	if _, e = io.ReadFull(second, b); e != nil || !bytes.Equal(b, payload[100:105]) {
		t.Fatal(string(b), e)
	}
	second.Close()
	catalogfs.Cleanup()
	if _, e = os.Stat(cached); !os.IsNotExist(e) {
		t.Fatal("idle member cache not cleaned", e)
	}
}

func TestIncompletePlatformIsOmittedButDirectAccessFails(t *testing.T) {
	root, game, _ := fixture(t)
	catalogfstest.Pack(t, filepath.Dir(game))
	busy := filepath.Join(root, "platforms", "Busy")
	put(t, filepath.Join(busy, "catalog-conversion.pending"), "incomplete")
	put(t, filepath.Join(busy, "Game", "game.yaml"), "title: Busy\n")
	entries, e := catalogfs.ReadDir(filepath.Join(root, "platforms"))
	if e != nil || len(entries) != 1 {
		t.Fatal(entries, e)
	}
	if _, e = catalogfs.ReadFile(filepath.Join(busy, "Game", "game.yaml")); e == nil {
		t.Fatal("direct access accepted incomplete platform")
	}
}

func TestMetadataReadsDoNotOpenMediaPacks(t *testing.T) {
	_, game, _ := fixture(t)
	catalogfstest.Pack(t, filepath.Dir(game))
	if err := os.WriteFile(filepath.Join(game, "video.emumovies.zip"), []byte("invalid ZIP"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := catalogfs.ReadFile(filepath.Join(game, "game.yaml")); err != nil {
		t.Fatal("metadata inspected unrelated media:", err)
	}
	if _, err := catalogfs.ReadDir(game); err == nil {
		t.Fatal("full inventory stopped validating media")
	}
}

func TestIndexCacheCannotBeHardlinkedIntoSnapshot(t *testing.T) {
	root := t.TempDir()
	from, to := filepath.Join(root, "_index.sqlite"), filepath.Join(root, "copy.sqlite")
	put(t, from, "cache bytes")
	if err := catalogfs.Link(from, to); err == nil {
		t.Fatal("mutable index was hardlinked")
	}
	if _, err := os.Stat(to); !os.IsNotExist(err) {
		t.Fatal("link target created", err)
	}
}

// Listing needs the central directory, not a local-header seek for every media
// member. Damage in an unselected member is detected when that member is opened.
func TestMediaInventoryDefersLocalHeadersUntilSelectedOpen(t *testing.T) {
	for _, method := range []uint16{zip.Store, zip.Deflate} {
		t.Run(fmt.Sprint(method), func(t *testing.T) {
			_, game, _ := fixture(t)
			catalogfstest.Pack(t, filepath.Dir(game))
			pack := filepath.Join(game, "video.emumovies.zip")
			f, err := os.Create(pack)
			if err != nil {
				t.Fatal(err)
			}
			zw := zip.NewWriter(f)
			for _, name := range []string{"bad", "good"} {
				w, err := zw.CreateHeader(&zip.FileHeader{Name: "media/video." + name + ".emumovies.mp4", Method: method})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = w.Write([]byte(name)); err != nil {
					t.Fatal(err)
				}
			}
			if err = zw.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err = f.WriteAt([]byte("BAD!"), 0); err != nil {
				t.Fatal(err)
			}
			if err = f.Close(); err != nil {
				t.Fatal(err)
			}
			media := filepath.Join(game, "media")
			if _, err = catalogfs.ReadDir(media); err != nil {
				t.Fatalf("listing parsed unselected local header: %v", err)
			}
			good := filepath.Join(media, "video.good.emumovies.mp4")
			if b, err := catalogfs.ReadFile(good); err != nil || string(b) != "good" {
				t.Fatalf("selected good member: %q %v", b, err)
			}
			bad := filepath.Join(media, "video.bad.emumovies.mp4")
			if _, err = catalogfs.Open(bad); err == nil {
				t.Fatal("selected corrupt local header accepted")
			}
		})
	}
}
