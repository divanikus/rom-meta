package romm

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/exporter"
)

// DerivativeIdentity retains modification identity outside original provider data.
type DerivativeIdentity struct {
	Variant         string                `json:"variant"`
	MetadataRelease string                `json:"metadata_release"`
	MatchedBy       string                `json:"matched_by"`
	Info            *exporter.VariantInfo `json:"variant_info"`
}

func emptyProof(p VerificationProof) bool {
	return p.Size == 0 && p.SHA1 == "" && p.SHA256 == "" && p.MD5 == "" && p.CRC32 == "" && len(p.Components) == 0
}

func snapshotVariant(r exporter.Record, gameRaw, fpRaw []byte, p VerificationProof) (*DerivativeIdentity, error) {
	if r.Status != "matched" || r.Variant == "" || r.Release != "" {
		return nil, fmt.Errorf("invalid derivative identity")
	}
	g, err := catalogyaml.Parse(gameRaw)
	if err != nil {
		return nil, err
	}
	chain, base, err := catalogyaml.VariantChain(g.Root(), r.Variant)
	if err != nil {
		return nil, err
	}
	metadataRelease := base
	if metadataRelease == "" {
		metadataRelease = catalogyaml.DefaultRelease(g.Root())
	}
	if r.MetadataRelease != metadataRelease {
		return nil, fmt.Errorf("variant metadata release differs from its base or game metadata selection")
	}
	all, err := catalogyaml.Variants(g.Root())
	if err != nil {
		return nil, err
	}
	byName := map[string]catalogyaml.Variant{}
	for _, v := range all {
		byName[v.Name] = v
	}
	v := byName[r.Variant]
	info := &exporter.VariantInfo{Kind: v.Kind, BasedOn: v.BasedOn, Chain: chain, Group: v.Group, Version: v.Version}
	for _, name := range chain {
		ancestor := byName[name]
		if len(info.Languages) == 0 {
			info.Languages = ancestor.Languages
		}
		if name != r.Variant {
			info.Ancestors = append(info.Ancestors, ancestor)
		}
	}
	if !reflect.DeepEqual(info, r.VariantInfo) {
		return nil, fmt.Errorf("variant credits/chain differ from source YAML")
	}
	f, err := catalogyaml.Parse(fpRaw)
	if err != nil {
		return nil, err
	}
	items := catalogyaml.Value(f.Root(), "items")
	if items == nil {
		items = catalogyaml.Value(f.Root(), "roms")
	}
	var expected []exporter.PayloadProof
	headerMatch := false
	err = catalogyaml.Sequence(items, func(item *yaml.Node) error {
		_, variant, err := catalogyaml.FingerprintTarget(g.Root(), item)
		if err != nil {
			return err
		}
		if variant != r.Variant {
			return nil
		}
		file := catalogyaml.Value(item, "file")
		value := func(key string) string { return strings.ToLower(catalogyaml.Scalar(catalogyaml.Value(file, key))) }
		size := catalogyaml.Int64(catalogyaml.Value(file, "size"))
		if value("kind") == "chd-combined-sha1" {
			if r.Disc != nil && len(r.Disc.Files) == 1 {
				h, e := hex.DecodeString(r.Disc.Files[0].Header)
				if e == nil && len(h) == 124 && value("sha1") == hex.EncodeToString(h[84:104]) && (size == 0 || size == int64(binary.BigEndian.Uint64(h[32:40]))) {
					headerMatch = true
				}
			}
			return nil
		}
		expected = append(expected, exporter.PayloadProof{Size: size, SHA1: value("sha1"), SHA256: value("sha256"), MD5: value("md5"), CRC32: value("crc32")})
		return nil
	})
	if err != nil {
		return nil, err
	}
	switch r.MatchedBy {
	case "hash":
		if r.Catalog != nil || r.Disc != nil || r.Payload == nil || r.Size != p.Size || r.SHA1 != p.SHA1 || len(p.SHA1) != 40 || len(p.SHA256) != 64 || len(p.MD5) != 32 || len(p.CRC32) != 8 ||
			r.Payload.Size != p.Size || r.Payload.SHA1 != p.SHA1 || r.Payload.SHA256 != p.SHA256 || r.Payload.MD5 != p.MD5 || r.Payload.CRC32 != p.CRC32 || !exporter.ExactPayloadSet(*r.Payload, expected) {
			return nil, fmt.Errorf("modified payload differs from variant fingerprints")
		}
	case "catalog-name":
		if !emptyProof(p) || r.Disc != nil || r.Payload != nil || r.SHA1 != "" || !exporter.ValidCatalogObservation(r.ROM, r.Size, r.Catalog) {
			return nil, fmt.Errorf("invalid variant name/stat observation")
		}
	case "variant-chd-header":
		if !emptyProof(p) || r.Catalog != nil || r.Payload != nil || r.SHA1 != "" || !exporter.ValidDiscObservation(r.ROM, r.Size, r.Disc) || !headerMatch {
			return nil, fmt.Errorf("variant CHD header differs from its declared representation")
		}
	default:
		return nil, fmt.Errorf("unsupported derivative proof %q", r.MatchedBy)
	}
	return &DerivativeIdentity{Variant: r.Variant, MetadataRelease: metadataRelease, MatchedBy: r.MatchedBy, Info: info}, nil
}
