package romm

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func legacyDateFixture(t *testing.T, count int) (*fakeAPI, *Client, *Plan, *Receipt) {
	t.Helper()
	source := sourceFixture(t, count)
	var rows []map[string]json.RawMessage
	for i := range source.Records {
		source.Records[i].Fields["release_date"] = source.Records[i].Fields["description"]
		value := source.Records[i].Fields["release_date"]
		value.Value = "1993-07-02"
		value.Source = "local"
		source.Records[i].Fields["release_date"] = value
		rows = append(rows, beforeRow(i+1, source.Records[i].ROM, source.Records[i].Size))
	}
	api := newFake(t, rows...)
	t.Cleanup(api.close)
	client, e := NewClient(api.server.URL, "secret-never-print")
	if e != nil {
		t.Fatal(e)
	}
	original, e := Build(context.Background(), client, source, api.platform.FSSlug, "roms/Sega - Game Gear/retail")
	if e != nil {
		t.Fatal(e)
	}
	receipt := &Receipt{SchemaVersion: 1, Target: "romm", Results: []Result{}}
	for i := range original.Entries {
		entry := &original.Entries[i]
		var sent map[string]json.RawMessage
		if e = json.Unmarshal([]byte(entry.Form["raw_manual_metadata"]), &sent); e != nil {
			t.Fatal(e)
		}
		date, _ := time.Parse("2006-01-02", entry.Source.Fields["release_date"].Value)
		sent["first_release_date"] = raw(date.Unix())
		payload, _ := json.Marshal(sent)
		entry.Form["raw_manual_metadata"] = string(payload)
		after, e := client.update(context.Background(), *entry)
		if e != nil {
			t.Fatal(e)
		}
		receipt.Results = append(receipt.Results, Result{ID: entry.ROM.ID, Status: "applied", After: after})
	}
	api.put = 0
	return api, client, original, receipt
}
func TestManualReleaseDateUsesRomMJavaScriptMilliseconds(t *testing.T) {
	record := sourceFixture(t, 1).Records[0]
	value := record.Fields["release_date"]
	value.Value = "1993-07-02"
	record.Fields["release_date"] = value
	form := proposedForm(record, beforeRow(1, record.ROM, record.Size))
	var metadata map[string]json.RawMessage
	if e := json.Unmarshal([]byte(form["raw_manual_metadata"]), &metadata); e != nil {
		t.Fatal(e)
	}
	valueMS, ok := metadataDate(metadata)
	if !ok || valueMS != 741571200000 || time.UnixMilli(valueMS).UTC().Format("2006-01-02") != "1993-07-02" {
		t.Fatalf("RomM new Date(value) would render the wrong date: %d", valueMS)
	}
}
func TestDateRepairOnlyChangesProvenDateAndPreservesOwnerMetadata(t *testing.T) {
	api, client, original, receipt := legacyDateFixture(t, 1)
	var current map[string]json.RawMessage
	_ = json.Unmarshal(api.rows[1]["manual_metadata"], &current)
	current["publishers"] = raw([]string{"Owner's later publisher"})
	api.rows[1]["manual_metadata"] = raw(current)
	api.rows[1]["summary"] = raw("Owner's later prose")
	plan, e := BuildDateRepair(context.Background(), client, original, receipt)
	if e != nil {
		t.Fatal(e)
	}
	if api.put != 0 || len(plan.Entries) != 1 || plan.CoverBytes != 0 {
		t.Fatal("repair planning mutated or selected wrong scope")
	}
	entry := plan.Entries[0]
	if entry.Cover != nil || len(entry.Form) != 1 || entry.Form["raw_manual_metadata"] == "" {
		t.Fatal("repair contains non-date API fields/media")
	}
	var proposed map[string]json.RawMessage
	_ = json.Unmarshal([]byte(entry.Form["raw_manual_metadata"]), &proposed)
	delete(proposed, "first_release_date")
	delete(current, "first_release_date")
	if !reflect.DeepEqual(proposed, current) {
		t.Fatal("repair changed another manual field")
	}
	if e = Apply(context.Background(), client, plan, filepath.Join(t.TempDir(), "repair.json"), nil, false); e != nil {
		t.Fatal(e)
	}
	if api.put != 1 || rawString(api.rows[1], "summary") != "Owner's later prose" {
		t.Fatal("repair changed wrong scope")
	}
	metadata, _ := metadataObject(api.rows[1])
	date, _ := metadataDate(metadata)
	if date != 741571200000 {
		t.Fatal("date still seconds")
	}
	again, e := BuildDateRepair(context.Background(), client, original, receipt)
	if e != nil || len(again.Entries) != 0 {
		t.Fatalf("repair not repeatable: %v", e)
	}
	name := filepath.Join(t.TempDir(), "no-op.json")
	if e = WriteNew(name, again); e != nil {
		t.Fatal(e)
	}
	copy, e := ReadPlan(name)
	if e != nil || len(copy.Entries) != 0 {
		t.Fatalf("no-op repair plan not readable: %v", e)
	}
}
func TestDateRepairPreservesAChangedDate(t *testing.T) {
	api, client, original, receipt := legacyDateFixture(t, 1)
	metadata, _ := metadataObject(api.rows[1])
	metadata["first_release_date"] = raw(int64(777600000000))
	api.rows[1]["manual_metadata"] = raw(metadata)
	plan, e := BuildDateRepair(context.Background(), client, original, receipt)
	if e != nil || len(plan.Entries) != 0 || api.put != 0 {
		t.Fatalf("owner date not preserved: %v", e)
	}
}
func TestDateRepairRequiresOriginalConfirmedCreation(t *testing.T) {
	for _, kind := range []string{"missing-receipt", "unconfirmed", "different-after", "preexisting-date", "changed-inventory"} {
		t.Run(kind, func(t *testing.T) {
			api, client, original, receipt := legacyDateFixture(t, 1)
			switch kind {
			case "missing-receipt":
				receipt.Results = nil
			case "unconfirmed":
				receipt.Results[0].Status = "response-not-confirmed"
			case "different-after":
				metadata, _ := metadataObject(receipt.Results[0].After)
				metadata["first_release_date"] = raw(1234)
				receipt.Results[0].After["manual_metadata"] = raw(metadata)
			case "preexisting-date":
				original.Entries[0].Before["manual_metadata"] = raw(map[string]any{"first_release_date": 741571200})
			case "changed-inventory":
				api.rows[1]["fs_name"] = raw("owner-renamed.gg")
			}
			if _, e := BuildDateRepair(context.Background(), client, original, receipt); e == nil || api.put != 0 {
				t.Fatalf("unsafe repair accepted: %v", e)
			}
		})
	}
}
func TestDateRepairPreflightRejectsOtherFieldsAndConcurrentEdits(t *testing.T) {
	for _, kind := range []string{"extra-form", "extra-manual", "stale-second"} {
		t.Run(kind, func(t *testing.T) {
			api, client, original, receipt := legacyDateFixture(t, 2)
			plan, e := BuildDateRepair(context.Background(), client, original, receipt)
			if e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "extra-form":
				plan.Entries[0].Form["summary"] = "unexpected overwrite"
			case "extra-manual":
				var metadata map[string]json.RawMessage
				_ = json.Unmarshal([]byte(plan.Entries[0].Form["raw_manual_metadata"]), &metadata)
				metadata["genres"] = raw([]string{"Unexpected genre"})
				payload, _ := json.Marshal(metadata)
				plan.Entries[0].Form["raw_manual_metadata"] = string(payload)
			case "stale-second":
				api.rows[2]["summary"] = raw("Concurrent owner prose")
			}
			if e = Apply(context.Background(), client, plan, filepath.Join(t.TempDir(), "receipt.json"), nil, false); e == nil || api.put != 0 {
				t.Fatalf("unsafe partial repair allowed: %v", e)
			}
		})
	}
}
func TestOldSecondsManifestCannotBeAppliedWithFixedMapper(t *testing.T) {
	api, client, original, _ := legacyDateFixture(t, 1)
	if e := Preflight(context.Background(), client, original); e == nil || api.put != 0 {
		t.Fatal("old seconds plan can still be submitted")
	}
}
