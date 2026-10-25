package exporter

import (
	"path/filepath"
	"regexp"
	"strings"

	"rommeta/internal/catalogfs"
)

var ps3SerialPattern = regexp.MustCompile(`^[A-Z]{4}[0-9]{5}$`)

// A serial descriptor is a frontend launch file, not a disc fingerprint.
// Until a PSN identity reader exists, only an explicit unique title can bind it.
func observeSerial(romRoot, path string) (ExportInput, error) {
	rel, _ := filepath.Rel(romRoot, path)
	input := ExportInput{ROM: filepath.ToSlash(rel), Kind: "unsupported", Representation: "ps3-serial-file"}
	f, err := catalogfs.Open(path)
	if err != nil {
		return input, err
	}
	h, err := hashStream(f, 64)
	f.Close()
	if err != nil {
		input.Reason = "PS3 serial file exceeds 64 bytes"
		return input, nil
	}
	data, err := catalogfs.ReadFile(path)
	if err != nil {
		return input, err
	}
	input.Serial = strings.TrimSpace(string(data))
	if !ps3SerialPattern.MatchString(input.Serial) {
		input.Serial = ""
		input.Reason = "invalid PS3 game serial descriptor"
		return input, nil
	}
	input.Kind = "serial"
	input.Proof = PayloadProof{Size: h.size, SHA1: h.sha1, SHA256: h.sha256, MD5: h.md5, CRC32: h.crc}
	return input, nil
}
