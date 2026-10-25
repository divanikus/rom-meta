package romm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"time"

	"rommeta/internal/catalogfs"
)

const repairDateUnits = "repair-date-units"

// DateRepair proves the narrowly intended correction; it is not a general overwrite.
type DateRepair struct {
	SourceDate          string `json:"source_date"`
	AppliedSeconds      int64  `json:"applied_seconds"`
	CorrectMilliseconds int64  `json:"correct_milliseconds"`
	AppliedSnapshotHash string `json:"applied_snapshot_hash"`
}

func metadataObject(before map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	metadata := map[string]json.RawMessage{}
	if value := before["manual_metadata"]; nonemptyRaw(value) {
		if e := json.Unmarshal(value, &metadata); e != nil {
			return nil, fmt.Errorf("invalid manual metadata object")
		}
	}
	if metadata == nil {
		metadata = map[string]json.RawMessage{}
	}
	return metadata, nil
}
func metadataDate(metadata map[string]json.RawMessage) (int64, bool) {
	var value int64
	e := json.Unmarshal(metadata["first_release_date"], &value)
	return value, e == nil && nonemptyRaw(metadata["first_release_date"])
}
func dateRepairForm(entry Entry) (map[string]string, error) {
	proof := entry.DateRepair
	if proof == nil || entry.Cover != nil || entry.Manual != nil || proof.AppliedSnapshotHash == "" {
		return nil, fmt.Errorf("invalid date-repair evidence or media action")
	}
	date, e := time.Parse("2006-01-02", entry.Source.Fields["release_date"].Value)
	if e != nil || proof.SourceDate != entry.Source.Fields["release_date"].Value || proof.AppliedSeconds != date.Unix() || proof.CorrectMilliseconds != date.UnixMilli() || proof.AppliedSeconds == proof.CorrectMilliseconds {
		return nil, fmt.Errorf("date-repair source/units do not agree")
	}
	metadata, e := metadataObject(entry.Before)
	if e != nil {
		return nil, e
	}
	current, ok := metadataDate(metadata)
	if !ok || current != proof.AppliedSeconds || !knownManualFields(metadata) {
		return nil, fmt.Errorf("date repair would replace unproved or unknown metadata")
	}
	metadata["first_release_date"], _ = json.Marshal(proof.CorrectMilliseconds)
	payload, _ := json.Marshal(metadata)
	return map[string]string{"raw_manual_metadata": string(payload)}, nil
}
func expectedForm(p *Plan, entry Entry) (map[string]string, error) {
	switch p.Operation {
	case "":
		if entry.DateRepair != nil {
			return nil, fmt.Errorf("date-repair evidence in ordinary import")
		}
		return proposedForm(entry.Source, entry.Before), nil
	case providerImport:
		return expectedProviderForm(p, entry)
	case repairDateUnits:
		return dateRepairForm(entry)
	default:
		return nil, fmt.Errorf("unsupported RomM plan operation")
	}
}
func ReadReceipt(name string) (*Receipt, error) {
	f, e := catalogfs.Open(name)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	decoder := json.NewDecoder(io.LimitReader(f, 64<<20))
	decoder.DisallowUnknownFields()
	var receipt Receipt
	if e = decoder.Decode(&receipt); e != nil {
		return nil, e
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, fmt.Errorf("receipt must contain one JSON document")
	}
	if receipt.SchemaVersion != 1 || receipt.Target != "romm" {
		return nil, fmt.Errorf("unsupported receipt")
	}
	return &receipt, nil
}

// BuildDateRepair requires the original import's plan/confirmed responses and
// fresh API records. Only a date still equal to that exact bad value is eligible.
func BuildDateRepair(ctx context.Context, c *Client, original *Plan, receipt *Receipt) (*Plan, error) {
	if original.Operation != "" || original.Server != c.base || original.SchemaVersion != 1 || original.Target != "romm" || receipt.SchemaVersion != 1 || receipt.Target != "romm" {
		return nil, fmt.Errorf("repair requires an original import plan and matching receipt")
	}
	results := map[int]Result{}
	for _, result := range receipt.Results {
		if _, exists := results[result.ID]; exists {
			return nil, fmt.Errorf("duplicate receipt ROM id")
		}
		results[result.ID] = result
	}
	plan := &Plan{SchemaVersion: 1, Target: "romm", Operation: repairDateUnits, Server: original.Server, DataRoot: original.DataRoot, ROMRoot: original.ROMRoot, Platform: original.Platform, Prefix: original.Prefix, Profile: original.Profile, Entries: []Entry{}, Skipped: []Skip{}}
	proven := 0
	seen := map[int]bool{}
	for _, old := range original.Entries {
		if seen[old.ROM.ID] {
			return nil, fmt.Errorf("duplicate original ROM id")
		}
		seen[old.ROM.ID] = true
		skip := func(reason string) { plan.Skipped = append(plan.Skipped, Skip{ROM: old.Source.ROM, Reason: reason}) }
		date, e := time.Parse("2006-01-02", old.Source.Fields["release_date"].Value)
		result, hasResult := results[old.ROM.ID]
		sent := map[string]json.RawMessage{}
		sentError := json.Unmarshal([]byte(old.Form["raw_manual_metadata"]), &sent)
		sentDate, hasSentDate := metadataDate(sent)
		originalBefore, beforeError := metadataObject(old.Before)
		appliedMetadata, appliedError := metadataObject(result.After)
		appliedDate, hasAppliedDate := metadataDate(appliedMetadata)
		appliedROM, romError := decodeROM(result.After)
		if e != nil || sentError != nil || beforeError != nil || appliedError != nil || romError != nil || !hasResult || result.Status != "applied" || !hasSentDate || !hasAppliedDate || nonemptyRaw(originalBefore["first_release_date"]) || sentDate != date.Unix() || appliedDate != sentDate || date.Unix() == date.UnixMilli() || appliedROM != old.ROM || old.Source.Status != "matched" || old.Source.MatchedBy != "hash" {
			skip("no confirmed importer-created seconds value for a complete source date")
			continue
		}
		proven++
		current, e := c.Detail(ctx, old.ROM.ID)
		if e != nil {
			return nil, e
		}
		currentROM, e := decodeROM(current)
		if e != nil || currentROM != old.ROM {
			return nil, fmt.Errorf("repair ROM %d inventory changed", old.ROM.ID)
		}
		metadata, e := metadataObject(current)
		if e != nil {
			return nil, e
		}
		currentDate, hasDate := metadataDate(metadata)
		if hasDate && currentDate == date.UnixMilli() {
			skip("date already corrected")
			continue
		}
		if !hasDate || currentDate != sentDate {
			skip("current date differs from the confirmed bad import; preserved")
			continue
		}
		if !knownManualFields(metadata) {
			return nil, fmt.Errorf("repair ROM %d has unknown manual fields; preserved", old.ROM.ID)
		}
		if e = verifyFile(original.ROMRoot, old.Input, 512<<20); e != nil {
			return nil, fmt.Errorf("repair input %d: %w", old.ROM.ID, e)
		}
		entry := Entry{ROM: old.ROM, Source: old.Source, Input: old.Input, Before: current, BeforeHash: snapshotHash(current), DateRepair: &DateRepair{SourceDate: old.Source.Fields["release_date"].Value, AppliedSeconds: sentDate, CorrectMilliseconds: date.UnixMilli(), AppliedSnapshotHash: snapshotHash(result.After)}}
		entry.Form, e = dateRepairForm(entry)
		if e != nil {
			return nil, e
		}
		plan.Entries = append(plan.Entries, entry)
	}
	if proven == 0 {
		return nil, fmt.Errorf("plan/receipt contain no confirmed importer-created date-unit error")
	}
	return plan, nil
}
func sameJSON(a, b []byte) bool {
	var av, bv any
	return json.Unmarshal(a, &av) == nil && json.Unmarshal(b, &bv) == nil && reflect.DeepEqual(av, bv)
}
