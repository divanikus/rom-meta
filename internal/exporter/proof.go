package exporter

import (
	"context"
	"io"
)

// PayloadProof describes bytes read by the same bounded cartridge/ZIP reader used
// for offline binding. It is a portable input fact, not consumer verification.
type PayloadProof struct {
	Size   int64  `json:"size"`
	SHA1   string `json:"sha1"`
	SHA256 string `json:"sha256"`
	MD5    string `json:"md5"`
	CRC32  string `json:"crc32"`
	Member string `json:"member,omitempty"`
	// Components are additional cartridge ROMs in the same ZIP. The primary is
	// chosen by member-name order, never as a guess about the main program.
	Components []PayloadProof `json:"components,omitempty"`
}

func InspectROM(name, system string) (PayloadProof, error) {
	h, member, err := hashROM(name, system)
	return PayloadProof{Size: h.size, SHA1: h.sha1, SHA256: h.sha256, MD5: h.md5, CRC32: h.crc, Member: member}, err
}

type contextReader struct {
	ctx   context.Context
	input io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.input.Read(p)
}
func inspectROMContext(ctx context.Context, name, system string) (PayloadProof, error) {
	h, member, err := hashROMContext(ctx, name, system)
	return PayloadProof{Size: h.size, SHA1: h.sha1, SHA256: h.sha256, MD5: h.md5, CRC32: h.crc, Member: member}, err
}
