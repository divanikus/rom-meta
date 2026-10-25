package romm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"rommeta/internal/exporter"
)

func manualFixture(t *testing.T, count int, ext string) (*fakeAPI, *Client, *Plan, *exporter.Plan, []byte) {
	t.Helper()
	source := sourceFixture(t, count)
	source.Profile.Selection.Media = map[string]exporter.Rule{"manual": {}}
	data := []byte("%PDF-1.7\nSynthetic manual bytes\n")
	if ext != ".pdf" {
		data = []byte("Synthetic source manual text")
	}
	name := "manual.world.local" + ext
	if e := os.WriteFile(filepath.Join(source.DataRoot, name), data, 0600); e != nil {
		t.Fatal(e)
	}
	var rows []map[string]json.RawMessage
	for i := range source.Records {
		source.Records[i].Fields = map[string]exporter.Value{}
		source.Records[i].Media = map[string]exporter.Media{"manual": {SourcePath: name, Size: int64(len(data)), Source: "local"}}
		rows = append(rows, beforeRow(i+1, source.Records[i].ROM, int64(len([]byte(fmt.Sprintf("synthetic ROM %d", i))))))
	}
	api := newFake(t, rows...)
	t.Cleanup(api.close)
	client, e := NewClient(api.server.URL, "secret-never-print")
	if e != nil {
		t.Fatal(e)
	}
	p, e := Build(context.Background(), client, source, api.platform.FSSlug, "roms/Sega - Game Gear/retail")
	if e != nil {
		t.Fatal(e)
	}
	return api, client, p, source, data
}
func TestStockManualMultipartConfirmationAndFreshRepeat(t *testing.T) {
	for _, ext := range []string{".pdf", ".md", ".txt"} {
		t.Run(ext, func(t *testing.T) {
			api, client, p, source, data := manualFixture(t, 1, ext)
			if p.ManualBytes != int64(len(data)) || p.Entries[0].Manual == nil {
				t.Fatal("manual action/budget missing")
			}
			if e := Apply(context.Background(), client, p, filepath.Join(t.TempDir(), "receipt.json"), nil, false); e != nil {
				t.Fatal(e)
			}
			if api.put != 0 || api.manualPost != 1 || !bytes.Equal(api.manualData, data) {
				t.Fatal("wrong stock manual wire", api.put, api.manualPost)
			}
			again, e := Build(context.Background(), client, source, api.platform.FSSlug, p.Prefix)
			if e != nil {
				t.Fatal(e)
			}
			if again.ManualBytes != 0 || again.Entries[0].Manual != nil {
				t.Fatal("repeat would overwrite imported manual")
			}
			if e = Preflight(context.Background(), client, again); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestManualPreflightPreservesExistingAndRejectsChangedLastSource(t *testing.T) {
	api, client, p, source, original := manualFixture(t, 2, ".pdf")
	file := p.Entries[1].Manual.Path
	if e := os.WriteFile(file, []byte("changed"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := Apply(context.Background(), client, p, filepath.Join(t.TempDir(), "receipt.json"), nil, false); e == nil {
		t.Fatal("changed source allowed")
	}
	if api.put != 0 || api.manualPost != 0 {
		t.Fatal("wrote before whole batch preflight")
	}
	if e := os.WriteFile(file, original, 0600); e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{"path_manual", "url_manual"} {
		api.rows[1][key] = raw("owner manual")
		again, e := Build(context.Background(), client, source, api.platform.FSSlug, p.Prefix)
		if e != nil {
			t.Fatal(e)
		}
		if again.Entries[0].Manual != nil {
			t.Fatal("owner manual replaced", key)
		}
		delete(api.rows[1], key)
	}
}
func TestUnsupportedMediaProfileFailsBeforeAPIAndProvenanceDoesNotBecomeFields(t *testing.T) {
	api, client, p, _, _ := manualFixture(t, 1, ".pdf")
	before := api.requests
	source := sourceFixture(t, 1)
	source.Profile.Selection.Media = map[string]exporter.Rule{"video": {}}
	if _, e := Build(context.Background(), client, source, p.Platform.FSSlug, p.Prefix); e == nil {
		t.Fatal("unsupported media profile accepted")
	}
	if api.requests != before {
		t.Fatal("unsupported profile did not fail before network/write")
	}
	_, _, providers := providerFixture(t, 1)
	e := providers.Entries[0]
	if len(e.Providers.SourceTexts) != 1 || e.Providers.SourceTexts[0].Descriptions["de"] == "" || e.Providers.Slots["igdb"].SourceSlug != "synthetic-game" {
		t.Fatal("source alternatives lost", e.Providers)
	}
	if _, exists := e.Providers.Slots["igdb"].Metadata["descriptions"]; exists {
		t.Fatal("unsupported native description fabricated")
	}
}

func TestAvailabilityInventoryIsDistinctAndExcludesUnregisteredTrees(t *testing.T) {
	source := sourceFixture(t, 2)
	p := &Plan{DataRoot: source.DataRoot, Entries: []Entry{{Source: source.Records[0]}, {Source: source.Records[1]}}}
	game := filepath.Join(p.DataRoot, filepath.FromSlash(source.Records[0].Game))
	if e := os.MkdirAll(filepath.Join(game, "media", "_fold"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(game, "game.yaml"), []byte("releases:\n  Example (USA): {}\n"), 0600); e != nil {
		t.Fatal(e)
	}
	for name, data := range map[string][]byte{"box-front.usa.local.jpg": []byte("one"), "screenshot-01.igdb.png": []byte("two"), "_fold/ignored.local.png": []byte("excluded")} {
		if e := os.WriteFile(filepath.Join(game, "media", filepath.FromSlash(name)), data, 0600); e != nil {
			t.Fatal(e)
		}
	}
	if e := InventoryAvailableMedia(p); e != nil {
		t.Fatal(e)
	}
	if len(p.AvailableMedia) != 2 || p.AvailableMedia[1].Kind != "screenshot" || p.AvailableMedia[1].Source != "igdb" {
		t.Fatal(p.AvailableMedia)
	}
}

func TestProviderProfileCombinesBlankTextCoverAndRealManual(t *testing.T) {
	api, client, existing := providerFixture(t, 1)
	entry := existing.Entries[0]
	api.rows[entry.ROM.ID] = beforeRow(entry.ROM.ID, entry.ROM.FSName, entry.ROM.FSSize)
	source := &exporter.Plan{DataRoot: existing.DataRoot, ROMRoot: existing.ROMRoot, Profile: existing.Profile, Records: []exporter.Record{entry.Source}}
	source.Profile.Selection.Media["manual"] = exporter.Rule{}
	data := []byte("%PDF-1.7\nSynthetic combined manual\n")
	manualName := "combined-manual.pdf"
	if err := os.WriteFile(filepath.Join(source.DataRoot, manualName), data, 0600); err != nil {
		t.Fatal(err)
	}
	source.Records[0].Media["manual"] = exporter.Media{SourcePath: manualName, Size: int64(len(data)), Source: "local"}
	plan, err := Build(context.Background(), client, source, existing.Platform.FSSlug, existing.Prefix)
	if err != nil {
		t.Fatal(err)
	}
	if err = AddProviders(context.Background(), client, plan); err != nil {
		t.Fatal(err)
	}
	if plan.Entries[0].Form["summary"] != "Offline prose" || plan.Entries[0].Cover == nil || plan.Entries[0].Manual == nil || plan.Entries[0].Form["igdb_id"] != "123" {
		t.Fatal("combined actions missing")
	}
	if err = Apply(context.Background(), client, plan, filepath.Join(t.TempDir(), "receipt.json"), nil, false); err != nil {
		t.Fatal(err)
	}
	if api.put != 1 || api.manualPost != 1 || rawString(api.rows[entry.ROM.ID], "summary") != "Offline prose" {
		t.Fatal("combined native flow not retained")
	}
	repeat, err := Build(context.Background(), client, source, existing.Platform.FSSlug, existing.Prefix)
	if err != nil {
		t.Fatal(err)
	}
	if err = AddProviders(context.Background(), client, repeat); err != nil {
		t.Fatal(err)
	}
	if len(repeat.Entries[0].Form) != 0 || repeat.Entries[0].Cover != nil || repeat.Entries[0].Manual != nil {
		t.Fatal("combined repeat is not empty")
	}
}
