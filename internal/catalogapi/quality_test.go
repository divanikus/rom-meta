package catalogapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoverOnlySnapshotNeverReadsUnrelatedVideo(t *testing.T) {
	s, q, game, _ := lookupFixture(t)
	video, err := os.Create(filepath.Join(game, "media", "gameplay-video.usa.emumovies.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	if err = video.Truncate(MaxFile + 1); err != nil {
		t.Fatal(err)
	}
	video.Close()
	q.Media = "covers"
	w := lookupRequest(s, "POST", "/v1/lookup", strings.Repeat("s", 32), q)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var v Selection
	if err = json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, card := range v.Cards {
		for _, asset := range card.Assets {
			count++
			if asset.Kind != "box-front" {
				t.Fatal(asset)
			}
		}
	}
	if count != 1 || v.MediaMode != "covers" {
		t.Fatal(v)
	}
	q.Media = "none"
	w = lookupRequest(s, "POST", "/v1/lookup", strings.Repeat("s", 32), q)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	q.Media = ""
	w = lookupRequest(s, "POST", "/v1/lookup", strings.Repeat("s", 32), q)
	if w.Code != 409 {
		t.Fatal("default full inventory silently changed", w.Code)
	}
}
