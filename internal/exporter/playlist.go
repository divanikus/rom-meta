package exporter

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"rommeta/internal/catalogfs"
)

func bindPayload(root string, index *catalogIndex, record *Record, profile Profile, system string, h hashes, ext string) error {
	return bindObservedPayload(root, index, record, profile, system, h, ext, true)
}

func bindObservedPayload(root string, index *catalogIndex, record *Record, profile Profile, system string, h hashes, ext string, allowName bool) error {
	var matches []fingerprint
	seen := map[string]bool{}
	for _, candidate := range index.BySize[h.size] {
		if candidate.chd != (ext == ".chd") || !fingerprintMatches(candidate, h) {
			continue
		}
		key := candidate.targetKey()
		if !seen[key] {
			matches = append(matches, candidate)
			seen[key] = true
		}
	}
	switch len(matches) {
	case 0:
		if !allowName {
			record.Reason = "no exact payload fingerprint in this platform catalog"
			return nil
		}
		if err := nameFallback(root, index, record, profile, system); err != nil {
			return err
		}
	case 1:
		record.Status, record.MatchedBy, record.Reason = "matched", "hash", "exact payload size and strong fingerprint"
		if err := attachFingerprintRecord(root, matches[0], record, profile, system); err != nil {
			return err
		}
	default:
		record.Status, record.Reason = "ambiguous", "same payload identifies multiple catalog releases"
		for _, candidate := range matches {
			record.Candidates = append(record.Candidates, candidate.game.name+" / "+candidate.targetName())
		}
		sort.Strings(record.Candidates)
	}
	return nil
}

func observePlaylist(ctx context.Context, romRoot, path, system string) (ExportInput, error) {
	rel, _ := filepath.Rel(romRoot, path)
	input := ExportInput{ROM: filepath.ToSlash(rel), Kind: "unsupported", Representation: "playlist-directory"}
	hold := func(reason string) (ExportInput, error) {
		input.Kind, input.Reason = "unsupported", reason
		input.Proof = PayloadProof{}
		input.Parts = nil
		return input, nil
	}
	file := filepath.Join(path, filepath.Base(path))
	info, err := catalogfs.Lstat(file)
	if err != nil {
		if os.IsNotExist(err) {
			return hold("playlist directory lacks its matching .m3u file")
		}
		return input, err
	}
	if !info.Mode().IsRegular() || info.Size() > 65536 {
		return hold("playlist must be a regular local text file no larger than 64 KiB")
	}
	contents, err := catalogfs.ReadFile(file)
	if err != nil {
		return input, err
	}
	h, err := hashStream(strings.NewReader(string(contents)), 65536)
	if err != nil {
		return input, err
	}
	input.Proof = PayloadProof{Size: h.size, SHA1: h.sha1, SHA256: h.sha256, MD5: h.md5, CRC32: h.crc}
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimPrefix(string(contents), "\ufeff"), "\n") {
		name := strings.TrimSpace(line)
		if name == "" || strings.HasPrefix(name, "#") {
			continue
		}
		if filepath.IsAbs(name) || strings.ContainsAny(name, "\\:") || len(input.Parts) >= 32 {
			return hold("playlist must contain at most 32 relative local CHD/ISO paths")
		}
		member := filepath.Join(path, filepath.FromSlash(name))
		info, err := catalogfs.Lstat(member)
		if err != nil {
			if os.IsNotExist(err) {
				return hold("missing playlist member: " + name)
			}
			return input, err
		}
		if !info.Mode().IsRegular() {
			return hold("playlist symlink/non-file member is unsupported")
		}
		real, err := realPath(member)
		if err != nil {
			return input, err
		}
		folder, err := realPath(path)
		if err != nil {
			return input, err
		}
		if !within(folder, real) {
			return hold("playlist member escapes its game directory")
		}
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".chd" && ext != ".iso" {
			return hold("playlist member must be CHD/ISO: " + name)
		}
		if seen[strings.ToLower(real)] {
			return hold("duplicate playlist member")
		}
		seen[strings.ToLower(real)] = true
		proof, err := inspectROMContext(ctx, real, system)
		if err != nil {
			return input, err
		}
		partRel, _ := filepath.Rel(romRoot, real)
		part := ExportInput{ROM: filepath.ToSlash(partRel), Kind: "payload", Proof: proof}
		if ext == ".chd" {
			observed, err := observeDisc(romRoot, system, part.ROM)
			if err == nil {
				part.Disc = observed.Disc
			}
		}
		input.Parts = append(input.Parts, part)
	}
	if len(input.Parts) == 0 {
		return hold("playlist is empty")
	}
	input.Kind = "playlist"
	return input, nil
}

func (r *ExportResolver) resolvePlaylist(input ExportInput, profile Profile) (Record, error) {
	record := exportRecord(input)
	record.Status, record.Representation = "unsupported", "playlist-directory"
	record.Size, record.SHA1 = input.Proof.Size, input.Proof.SHA1
	record.LaunchFile = filepath.ToSlash(filepath.Join(input.ROM, filepath.Base(input.ROM)))
	hold := func(reason string) (Record, error) { record.Reason = reason; return record, nil }
	var first *Record
	allHashes := true
	for _, observed := range input.Parts {
		part, err := r.resolve(observed, profile)
		if err != nil {
			return record, err
		}
		name := strings.TrimPrefix(observed.ROM, input.ROM+"/")
		if part.Status != "matched" || (part.Release == "" && part.Variant == "") {
			return hold("playlist member has no unique exact release binding: " + name)
		}
		if first == nil {
			copy := part
			first = &copy
		} else if first.Game != part.Game || (first.Variant == "") != (part.Variant == "") || !sameVariantEdition(*first, part) {
			return hold("playlist mixes catalog games")
		}
		if part.MatchedBy != "hash" {
			allHashes = false
		}
		record.Playlist = append(record.Playlist, PlaylistInput{ROM: part.ROM, Size: part.Size, SHA1: part.SHA1, Release: part.Release, Variant: part.Variant, MatchedBy: part.MatchedBy})
	}
	record.Status, record.Game, record.MetadataRelease = "matched", first.Game, first.MetadataRelease
	record.Variant, record.VariantInfo = first.Variant, first.VariantInfo
	record.Fields, record.Media = first.Fields, first.Media
	record.TitleContext = first.TitleContext
	record.MatchedBy, record.Reason = "playlist-name", "playlist members bind to one catalog game; release-name evidence"
	if allHashes {
		record.MatchedBy, record.Reason = "playlist-hash", "all playlist member payloads match catalog fingerprints"
	}
	record.Fields["title"] = Value{Value: record.Fields["title"].Value, Source: "catalog"}
	for role, media := range record.Media {
		media.OutputPath, _ = ExportOutputPath(r.System, record, role, filepath.Ext(media.OutputPath))
		record.Media[role] = media
	}
	record.Warnings = append(record.Warnings, "Playlist metadata/media uses the first member's release; no new catalog edition identity is invented")
	if !allHashes {
		record.Warnings = append(record.Warnings, "Playlist relies on weaker member names; CHD container identity is not a raw-disc proof")
	}
	return record, nil
}

func playlistSystem(system string) bool {
	return system == "psx" || system == "saturn" || system == "dreamcast"
}
