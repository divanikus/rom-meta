package exporter

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
)

func discSize(disc *DiscObservation) int64 {
	var size int64
	if disc != nil {
		for _, file := range disc.Files {
			size += file.Size
		}
	}
	return size
}

// observeExportPayload uses bounded CHD evidence unless full hashing is requested.
// Older/unsupported headers retain the existing whole-container reader.
func observeExportPayload(options Options, root, path, rom string) (ExportInput, error) {
	input := ExportInput{ROM: rom, Kind: "payload"}
	if strings.EqualFold(filepath.Ext(path), ".chd") && DiscLookupSystem(options.System) {
		observed, err := observeDisc(root, options.System, rom)
		if err == nil {
			input.Disc = observed.Disc
			if !options.HashROMs {
				input.Kind = "disc"
				return input, nil
			}
		}
	}
	proof, err := inspectROMContext(options.Context, path, options.System)
	input.Proof = proof
	return input, err
}

func (r *ExportResolver) resolveCHD(input ExportInput, profile Profile) (Record, error) {
	record := exportRecord(input)
	record.Representation, record.Size, record.Disc = "chd-container", discSize(input.Disc), input.Disc
	// A possible whole-container fingerprint may disagree with either header or
	// filename identity. Ask the local reader for bytes before choosing any target.
	for _, fp := range r.index.BySize[record.Size] {
		if fp.chd {
			record.HashRequired = []string{input.ROM}
			record.Reason = "CHD container fingerprints require a full-file hash"
			return record, nil
		}
	}
	matched, err := bindVariantCHD(r.Root, r.index, &record, profile, r.System)
	if err != nil {
		return record, err
	}
	if !matched {
		if err := nameFallback(r.Root, r.index, &record, profile, r.System); err != nil {
			return record, err
		}
		if record.MatchedBy == "name" {
			record.Reason = "unique catalog release/file name; CHD payload remains unverified"
		}
	}
	record.Warnings = append(record.Warnings, "Only CHD header, size and modification time observed; no full-file hash, decompression or integrity verification")
	return record, nil
}

// CheckDiscObservation rechecks bounded header/stat facts, not CHD payload bytes.
func CheckDiscObservation(root, system, rom string, expected *DiscObservation) error {
	if expected == nil {
		return fmt.Errorf("missing CHD observation")
	}
	observed, err := observeDisc(root, system, rom)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(expected, observed.Disc) {
		return fmt.Errorf("CHD changed since planning: %s", rom)
	}
	return nil
}

// HashRequiredInputs upgrades only observed CHDs requested by the resolver.
// A server cannot use this response to open arbitrary paths or other inputs.
func HashRequiredInputs(options Options, inputs []ExportInput, records []Record) ([]ExportInput, bool, error) {
	if len(inputs) != len(records) {
		return nil, false, fmt.Errorf("hash request does not match inputs")
	}
	type hashRequest struct {
		index int
		input ExportInput
	}
	requests := []hashRequest{}
	result := append([]ExportInput(nil), inputs...)
	// Validate every requested path before any expensive reads.
	for i, record := range records {
		if record.ROM != inputs[i].ROM {
			return nil, false, fmt.Errorf("hash request changed ROM identity")
		}
		if len(record.HashRequired) == 0 {
			continue
		}
		if record.Status != "unmatched" || record.MatchedBy != "" || record.Game != "" || record.Release != "" || record.Variant != "" || len(record.Media) > 0 {
			return nil, false, fmt.Errorf("invalid provisional CHD hash response")
		}
		allowed := map[string]ExportInput{}
		if inputs[i].Kind == "disc" {
			allowed[inputs[i].ROM] = inputs[i]
		}
		if inputs[i].Kind == "playlist" {
			for _, part := range inputs[i].Parts {
				if part.Kind == "disc" {
					allowed[part.ROM] = part
				}
			}
		}
		seen := map[string]bool{}
		for _, rom := range record.HashRequired {
			input, ok := allowed[rom]
			if !ok || seen[rom] {
				return nil, false, fmt.Errorf("invalid CHD hash request")
			}
			seen[rom] = true
			requests = append(requests, hashRequest{i, input})
		}
		if inputs[i].Kind == "playlist" {
			result[i].Parts = append([]ExportInput(nil), inputs[i].Parts...)
		}
	}
	for j, request := range requests {
		if options.Context != nil {
			if err := options.Context.Err(); err != nil {
				return nil, false, err
			}
		}
		input, i := request.input, request.index
		if options.Progress != nil {
			options.Progress("hash ROMs", input.ROM, j+1, len(requests))
		}
		if err := CheckDiscObservation(options.ROMRoot, options.System, input.ROM, input.Disc); err != nil {
			return nil, false, err
		}
		proof, err := inspectROMContext(options.Context, filepath.Join(options.ROMRoot, filepath.FromSlash(input.ROM)), options.System)
		if err != nil {
			return nil, false, err
		}
		if err := CheckDiscObservation(options.ROMRoot, options.System, input.ROM, input.Disc); err != nil {
			return nil, false, err
		}
		input.Kind, input.Proof = "payload", proof
		if inputs[i].Kind == "disc" {
			result[i] = input
		} else {
			for k := range result[i].Parts {
				if result[i].Parts[k].ROM == input.ROM {
					result[i].Parts[k] = input
				}
			}
		}
	}
	return result, len(requests) > 0, nil
}
