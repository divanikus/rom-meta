package frontend

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type countedStream struct {
	input             io.Reader
	bytes, closes     *int
	readErr, closeErr error
}

func (r *countedStream) Read(p []byte) (int, error) {
	n, e := r.input.Read(p)
	*r.bytes += n
	if r.readErr != nil && n > 0 {
		return n, r.readErr
	}
	return n, e
}
func (r *countedStream) Close() error { *r.closes++; return r.closeErr }

func directFixture(t *testing.T, payload []byte, sha string) (Options, string, *Source) {
	t.Helper()
	o := setup(t, game(a, "Owner", "ROMs/one.zip", "<Favorite>true</Favorite>"), game(c, "Catalog", "ROMs/one.zip", "<Notes>New description</Notes>"))
	relative := "Images/" + platform + "/Box - Front/" + c + "-01.png"
	source := virtualSource(t, o.PackageRoot)
	source.Check = func() error { return nil }
	source.Files[relative] = SourceFile{Size: int64(len(payload)), SHA256: sha}
	o.Source = source
	o.PackageRoot = filepath.Join(t.TempDir(), "virtual-source")
	return o, relative, source
}

func TestDirectNativeStreamsNeededAssetOnceWithoutPackage(t *testing.T) {
	for _, known := range []bool{false, true} {
		t.Run(fmt.Sprint(known), func(t *testing.T) {
			payload := []byte("one complete asset stream")
			sha := ""
			if known {
				sha = hashBytes(payload)
			}
			o, relative, source := directFixture(t, payload, sha)
			opens, reads, closes, checks := 0, 0, 0, 0
			file := source.Files[relative]
			file.Open = func() (io.ReadCloser, error) {
				opens++
				if _, err := os.Stat(o.PackageRoot); !os.IsNotExist(err) {
					t.Fatal("direct source package was materialized")
				}
				return &countedStream{input: bytes.NewReader(payload), bytes: &reads, closes: &closes}, nil
			}
			source.Files[relative] = file
			source.Files["VersionMedia/unused.png"] = SourceFile{Size: 99, Open: func() (io.ReadCloser, error) { t.Fatal("unused alternate media was opened"); return nil, nil }}
			source.Check = func() error {
				checks++
				if opens != 0 {
					t.Fatal("asset opened before input validation")
				}
				return nil
			}
			p := mustBuild(t, o)
			if opens != 0 {
				t.Fatal("preview opened media")
			}
			counts := []int{}
			p.Progress = func(stage, name string, current, total int) {
				if stage == "write" {
					counts = append(counts, current)
					if total != len(p.Files) {
						t.Fatal("write total reset")
					}
				}
			}
			backup, err := p.Apply()
			if err != nil {
				t.Fatal(err)
			}
			if opens != 1 || reads != len(payload) || closes != 1 || checks != 1 {
				t.Fatalf("extra source pass: opens=%d bytes=%d closes=%d checks=%d", opens, reads, closes, checks)
			}
			if len(counts) != len(p.Files)+1 || counts[len(counts)-1] != len(p.Files) {
				t.Fatal("write did not complete once", counts)
			}
			installed := "Images/" + platform + "/Box - Front/" + a + "-01.png"
			if !bytes.Equal(get(t, o.Destination, installed), payload) {
				t.Fatal("wrong asset")
			}
			var receipt []struct{ Path, SHA256 string }
			if err = json.Unmarshal(get(t, backup, "changes.json"), &receipt); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, entry := range receipt {
				if entry.Path == installed {
					found = true
					if entry.SHA256 != hashBytes(payload) {
						t.Fatal("receipt has a deferred hash")
					}
				}
			}
			if !found {
				t.Fatal("missing asset receipt")
			}
			if repeat := mustBuild(t, o); len(repeat.Files) != 0 {
				t.Fatal("direct repeat changed files", repeat.Files)
			}
			if opens != 1 {
				t.Fatal("repeat reopened source media")
			}
		})
	}
}

func TestDirectStreamFailurePreservesMetadataAndRemovesPartialAsset(t *testing.T) {
	for _, mode := range []string{"short", "extra", "checksum", "read", "close", "input"} {
		t.Run(mode, func(t *testing.T) {
			payload := []byte("asset bytes")
			o, relative, source := directFixture(t, payload, hashBytes(payload))
			actual := payload
			if mode == "short" {
				actual = payload[:len(payload)-1]
			}
			if mode == "extra" {
				actual = append(append([]byte{}, payload...), 1)
			}
			if mode == "checksum" {
				actual = bytes.Repeat([]byte("x"), len(payload))
			}
			opened, read, closed := 0, 0, 0
			file := source.Files[relative]
			file.Open = func() (io.ReadCloser, error) {
				opened++
				r := &countedStream{input: bytes.NewReader(actual), bytes: &read, closes: &closed}
				if mode == "read" {
					r.readErr = errors.New("source read failed")
				}
				if mode == "close" {
					r.closeErr = errors.New("source close failed")
				}
				return r, nil
			}
			source.Files[relative] = file
			if mode == "input" {
				source.Check = func() error { return errors.New("ROM changed") }
			}
			before := get(t, o.Destination, platformFile)
			p := mustBuild(t, o)
			if _, err := p.Apply(); err == nil {
				t.Fatal("failed source was accepted")
			}
			if !bytes.Equal(before, get(t, o.Destination, platformFile)) {
				t.Fatal("metadata published after source failure")
			}
			installed := "Images/" + platform + "/Box - Front/" + a + "-01.png"
			if _, err := os.Stat(filepath.Join(o.Destination, filepath.FromSlash(installed))); !os.IsNotExist(err) {
				t.Fatal("partial asset remains")
			}
			if mode == "input" && opened != 0 {
				t.Fatal("input failure downloaded media")
			}
		})
	}
}
