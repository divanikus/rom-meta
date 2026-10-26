package exporter

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

func (p PayloadProof) Equal(other PayloadProof) bool { return reflect.DeepEqual(p, other) }

// PayloadParts returns actual byte proofs, not a hash of a synthetic container.
func PayloadParts(p PayloadProof) []PayloadProof {
	parts := append([]PayloadProof{p}, p.Components...)
	parts[0].Components = nil
	return parts
}

// ExactPayloadSet requires a complete bijection with the authoritative release.
// Multiple copies of a fingerprint in imported DAT history are one member.
func ExactPayloadSet(p PayloadProof, expected []PayloadProof) bool {
	parts := PayloadParts(p)
	unique := map[string]PayloadProof{}
	for _, fp := range expected {
		key := fmt.Sprintf("%d:%s:%s", fp.Size, fp.SHA1, fp.SHA256)
		unique[key] = fp
	}
	if len(parts) != len(unique) {
		return false
	}
	used := map[string]bool{}
	for _, actual := range parts {
		key := ""
		for k, fp := range unique {
			if actual.Size == fp.Size && fingerprintMatches(fingerprint{sha1: fp.SHA1, sha256: fp.SHA256, md5: fp.MD5, crc: fp.CRC32}, hashes{actual.Size, actual.SHA1, actual.SHA256, actual.MD5, actual.CRC32}) {
				if key != "" {
					return false
				}
				key = k
			}
		}
		if key == "" || used[key] {
			return false
		}
		used[key] = true
	}
	return true
}

func observePayload(path, system string) (PayloadProof, error) {
	if !strings.EqualFold(filepath.Ext(path), ".zip") {
		return InspectROM(path, system)
	}
	z, err := zip.OpenReader(path)
	if err != nil {
		return PayloadProof{}, err
	}
	defer z.Close()
	var files []*zip.File
	seen := map[string]bool{}
	for _, f := range z.File {
		if f.FileInfo().IsDir() || !payloadExtension(system, strings.ToLower(filepath.Ext(f.Name))) {
			continue
		}
		key := strings.ToLower(f.Name)
		if !ValidInputPath(f.Name) || seen[key] || f.Mode()&os.ModeSymlink != 0 || f.Flags&1 != 0 || len(files) >= 16 {
			return PayloadProof{}, fmt.Errorf("unsafe, duplicate or excessive ZIP payload members")
		}
		seen[key] = true
		files = append(files, f)
	}
	if len(files) < 2 {
		return InspectROM(path, system)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	var parts []PayloadProof
	var total int64
	seen = map[string]bool{}
	for _, f := range files {
		if f.UncompressedSize64 > maxPayload {
			return PayloadProof{}, fmt.Errorf("payload exceeds 512 MiB limit")
		}
		in, e := f.Open()
		if e != nil {
			return PayloadProof{}, e
		}
		h, e := hashStream(in, maxPayload)
		in.Close()
		if e != nil {
			return PayloadProof{}, e
		}
		total += h.size
		if total > maxPayload || seen[h.sha256] {
			return PayloadProof{}, fmt.Errorf("duplicate or excessive ZIP payload bytes")
		}
		seen[h.sha256] = true
		parts = append(parts, PayloadProof{Size: h.size, SHA1: h.sha1, SHA256: h.sha256, MD5: h.md5, CRC32: h.crc, Member: f.Name})
	}
	result := parts[0]
	result.Components = parts[1:]
	return result, nil
}

func bindCompound(root string, index *catalogIndex, record *Record, profile Profile, system string) error {
	p := *record.Payload
	seen := map[string]bool{}
	var candidates []fingerprint
	for _, fp := range index.BySize[p.Size] {
		if !fingerprintMatches(fp, hashes{p.Size, p.SHA1, p.SHA256, p.MD5, p.CRC32}) {
			continue
		}
		key := fp.targetKey()
		if seen[key] {
			continue
		}
		seen[key] = true
		var expected []PayloadProof
		for _, group := range index.BySize {
			for _, member := range group {
				if member.targetKey() == fp.targetKey() {
					expected = append(expected, PayloadProof{Size: member.size, SHA1: member.sha1, SHA256: member.sha256, MD5: member.md5, CRC32: member.crc})
				}
			}
		}
		if ExactPayloadSet(p, expected) {
			candidates = append(candidates, fp)
		}
	}
	if len(candidates) == 1 {
		record.Status, record.MatchedBy, record.Reason = "matched", "hash", "complete cartridge payload set matches authoritative release"
		return attachFingerprintRecord(root, candidates[0], record, profile, system)
	}
	record.Reason = "no complete unambiguous cartridge payload set in catalog"
	if len(candidates) > 1 {
		record.Status = "ambiguous"
	}
	return nil
}
