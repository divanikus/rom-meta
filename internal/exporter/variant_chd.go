package exporter

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sort"
)

// bindVariantCHD identifies declared standalone CHD content. It never promotes
// an embedded hash into full-file, decompressed payload or Redump track proof.
func bindVariantCHD(root string, index *catalogIndex, record *Record, profile Profile, system string) (bool, error) {
	if record.Disc == nil || len(record.Disc.Files) != 1 {
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
