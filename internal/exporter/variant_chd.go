package exporter

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"path"
	"sort"
)

// bindVariantCHD identifies declared CHD content. It never promotes
// an embedded hash into full-file, decompressed payload or Redump track proof.
func bindVariantCHD(root string, index *catalogIndex, record *Record, profile Profile, system string) (bool, error) {
	if record.Disc == nil {
		return false, nil
	}
	if len(record.Disc.Files) > 1 {
		return bindVariantCHDGroup(root, index, record, profile, system)
	}
	if len(record.Disc.Files) != 1 {
		return false, nil
	}
	file := record.Disc.Files[0]
	header, err := hex.DecodeString(file.Header)
	if err != nil || len(header) != 124 {
		return false, fmt.Errorf("invalid variant CHD header")
	}
	sha1 := hex.EncodeToString(header[84:104])
	logical := int64(binary.BigEndian.Uint64(header[32:40]))
	var matches []fingerprint
	seen := map[string]bool{}
	for _, fp := range index.ByCHD[sha1] {
		if fp.variant == "" || fp.kind != "chd-combined-sha1" || fp.size > 0 && fp.size != logical || seen[fp.targetKey()] {
			continue
		}
		seen[fp.targetKey()] = true
		matches = append(matches, fp)
	}
	if len(matches) > 0 && record.Payload != nil {
		p := record.Payload
		for _, fp := range index.BySize[p.Size] {
			if fp.chd && fingerprintMatches(fp, hashes{p.Size, p.SHA1, p.SHA256, p.MD5, p.CRC32}) && fp.targetKey() != matches[0].targetKey() && !seen[fp.targetKey()] {
				matches = append(matches, fp)
				seen[fp.targetKey()] = true
			}
		}
	}
	if len(matches) == 0 {
		return false, nil
	}
	if len(matches) > 1 {
		record.Status, record.Reason = "ambiguous", "CHD identity evidence identifies conflicting catalog targets"
		for _, fp := range matches {
			record.Candidates = append(record.Candidates, fp.game.name+" / "+fp.targetName())
		}
		sort.Strings(record.Candidates)
		return true, nil
	}
	record.Status, record.MatchedBy, record.Reason = "matched", "variant-chd-header", "declared CHD combined SHA1 matches the prepared variant"
	if err := attachFingerprintRecord(root, matches[0], record, profile, system); err != nil {
		return true, err
	}
	record.Warnings = append(record.Warnings, "Variant identity uses declared CHD header content; no decompression, payload integrity or Redump track verification")
	return true, nil
}

// Every observed member must have a declared modified identity in the same game.
// Different member bases/versions remain distinct provenance, not an inferred edition.
func bindVariantCHDGroup(root string, index *catalogIndex, record *Record, profile Profile, system string) (bool, error) {
	if record.Payload != nil || !ValidDiscObservation(record.ROM, record.Size, record.Disc) || !record.Disc.Directory {
		return false, nil
	}
	var first *Record
	members := []DiscVariant{}
	complete := true
	for _, file := range record.Disc.Files {
		part := Record{ROM: path.Join(record.ROM, file.Name), Size: file.Size, Disc: &DiscObservation{Kind: record.Disc.Kind, Files: []DiscFile{file}}, Status: "unsupported", Fields: map[string]Value{}, Media: map[string]Media{}}
		handled, err := bindVariantCHD(root, index, &part, profile, system)
		if err != nil {
			return true, err
		}
		if handled && part.Status != "matched" {
			record.Status, record.Reason = part.Status, part.Reason
			record.Candidates = part.Candidates
			return true, nil
		}
		if !handled {
			complete = false
			continue
		}
		if first == nil {
			copy := part
			first = &copy
		} else if first.Game != part.Game {
			record.Status, record.Reason = "ambiguous", "CHD variants identify different catalog games"
			return true, nil
		}
		members = append(members, DiscVariant{Name: file.Name, Variant: part.Variant, MetadataRelease: part.MetadataRelease, Info: part.VariantInfo})
	}
	if first == nil {
		return false, nil
	}
	if !complete {
		record.Status, record.Reason = "unmatched", "CHD folder member lacks a declared variant identity"
		return true, nil
	}
	rom, size, disc, representation := record.ROM, record.Size, record.Disc, record.Representation
	*record = *first
	record.ROM, record.Size, record.Disc, record.Representation = rom, size, disc, representation
	record.DiscVariants = members
	record.Reason = "all observed CHD members match prepared variants of one catalog game"
	record.Warnings = append(record.Warnings, "Group metadata uses the first observed disc; member variants remain separate, without an inferred patch edition or completeness claim")
	return true, nil
}
