package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"golang.org/x/text/language"
)

type tenOutput struct {
	Name   string `xml:"name,attr"`
	Size   int64  `xml:"size,attr"`
	SHA1   string `xml:"sha1,attr"`
	MD5    string `xml:"md5,attr"`
	CRC32  string `xml:"crc,attr"`
	Status string `xml:"status,attr"`
}
type tenMachine struct {
	Name  string      `xml:"name,attr"`
	ROMs  []tenOutput `xml:"rom"`
	Disks []tenOutput `xml:"disk"`
}
type tenDocument struct {
	XMLName xml.Name `xml:"datafile"`
	Header  struct {
		Name string `xml:"name"`
	} `xml:"header"`
	Machines []tenMachine `xml:"machine"`
	Games    []tenMachine `xml:"game"`
}
type tenSource struct {
	header    string
	outputs   map[string]fingerprint
	sha256    string
	machines  []tenMachine
	platform  string
	prefix    string
	languages []string
}

// readTEn reads a local published output list. No original game is inferred from
// the output label, patch credits, header or a default catalog release.
func readTEn(o options, name string, m *manifest) (*tenSource, error) {
	return readTranslationDAT(o, datReference{Path: name}, m)
}

func readTranslationDAT(o options, ref datReference, m *manifest) (*tenSource, error) {
	name := ref.Path
	if o.Sources == "" {
		return nil, fmt.Errorf("--sources required for translation DAT references")
	}
	path, err := regularInput(o.Sources, name)
	if err != nil {
		return nil, err
	}
	before, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if ref.Member == "" && before.Size() > 32<<20 {
		return nil, fmt.Errorf("translation DAT exceeds 32 MiB")
	}
	raw, hash, err := readDATBytes(path, ref.Member)
	if err != nil {
		return nil, err
	}
	after, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !sameStat(before, after) {
		return nil, fmt.Errorf("translation DAT changed while reading")
	}
	pinned := false
	for _, s := range m.Snapshots {
		if s.Path == name {
			if !strings.EqualFold(s.SHA256, hash) {
				return nil, fmt.Errorf("pinned translation DAT changed: %s", name)
			}
			pinned = true
		}
	}
	if !pinned {
		m.Snapshots = append(m.Snapshots, snapshot{Path: name, SHA256: hash})
	}
	var d tenDocument
	if err := xml.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	platform, prefix, langs, err := translationHeader(d.Header.Name)
	if err != nil {
		return nil, err
	}
	result := &tenSource{header: d.Header.Name, outputs: map[string]fingerprint{}, sha256: hash, machines: append(d.Machines, d.Games...), platform: platform, prefix: prefix, languages: langs}
	add := func(row tenOutput, kind string) error {
		if row.Status == "nodump" {
			return nil
		}
		f := fingerprint{Name: row.Name, Kind: kind, Size: row.Size, SHA1: strings.ToLower(row.SHA1), MD5: strings.ToLower(row.MD5), CRC32: strings.ToLower(row.CRC32)}
		if kind != "" && (row.MD5 != "" || row.CRC32 != "" || row.Size != 0) {
			return fmt.Errorf("disk record has ambiguous raw-file attributes: %s", row.Name)
		}
		if _, exists := result.outputs[row.Name]; exists {
			return fmt.Errorf("duplicate translation DAT output name: %s", row.Name)
		}
		result.outputs[row.Name] = f
		return nil
	}
	for _, machine := range append(d.Machines, d.Games...) {
		// Documentation/BIOS are not translated playable outputs. A multi-payload
		// ROM group needs its own set adapter, rather than importing one member.
		if (strings.HasPrefix(machine.Name, "_") || strings.HasPrefix(strings.ToUpper(machine.Name), "[BIOS]")) || len(machine.ROMs) > 1 || (len(machine.ROMs) > 0 && len(machine.Disks) > 0) {
			continue
		}
		for _, disk := range machine.Disks {
			if err := add(disk, "chd-combined-sha1"); err != nil {
				return nil, err
			}
		}
		if len(machine.ROMs) == 1 {
			if err := add(machine.ROMs[0], ""); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

func resolveTEn(m *manifest, o options) error {
	loaded := map[string]*tenSource{}
	for i := range m.Entries {
		e := &m.Entries[i]
		if e.DAT == nil {
			continue
		}
		if !safeRelative(e.DAT.Path) || strings.TrimSpace(e.DAT.Name) == "" {
			return fmt.Errorf("DAT reference needs a safe path and exact output name")
		}
		// The DAT proves the published output identity, not its original game.
		if len(e.Evidence.Links) == 0 {
			return fmt.Errorf("translation DAT output needs a separate reviewed original-game link")
		}
		cacheKey := e.DAT.Path + "\x00" + e.DAT.Member
		src := loaded[cacheKey]
		if src == nil {
			var err error
			src, err = readTranslationDAT(o, *e.DAT, m)
			if err != nil {
				return err
			}
			loaded[cacheKey] = src
		}
		sourcePlatform := e.Platform
		if e.DAT.SourcePlatform != "" {
			sourcePlatform = e.DAT.SourcePlatform
		}
		if !safeName(sourcePlatform) {
			return fmt.Errorf("unsafe translation DAT source platform")
		}
		if !strings.EqualFold(src.prefix, sourcePlatform) {
			return fmt.Errorf("translation DAT platform differs from selected game: %s", e.Platform)
		}
		output, ok := src.outputs[e.DAT.Name]
		if !ok {
			return fmt.Errorf("translation DAT output missing or unsupported: %s", e.DAT.Name)
		}
		supplied := e.File
		if supplied.Name != "" && supplied.Name != output.Name || supplied.Kind != "" && supplied.Kind != output.Kind || supplied.Size != 0 && supplied.Size != output.Size {
			return fmt.Errorf("supplied output differs from translation DAT record")
		}
		for _, pair := range [][2]string{{supplied.SHA1, output.SHA1}, {supplied.SHA256, output.SHA256}, {supplied.MD5, output.MD5}, {supplied.CRC32, output.CRC32}} {
			if pair[0] != "" && !strings.EqualFold(pair[0], pair[1]) {
				return fmt.Errorf("supplied hash differs from translation DAT record")
			}
		}
		e.File = output
		sourceName := "Translation DAT"
		if strings.Contains(strings.ToLower(src.header), "[t-en]") {
			sourceName = "T-En DAT"
		}
		link := sourceLink(sourceName, e.DAT.Path, src.sha256, e.DAT.Name, e.Game)
		link["role"] = "modified-output"
		if e.DAT.Member != "" {
			link["archive_member"] = e.DAT.Member
		}
		e.Evidence.Links = append(e.Evidence.Links, link)
	}
	return nil
}

// Language identifiers follow BCP 47 rather than a product-specific vocabulary.
var translationTagRE = regexp.MustCompile(`(?i)\[T-([a-z0-9,+-]+)(?: by)?(?: ([^\]]*))?\]`)

func translationLanguages(text string) []string {
	langs := []string{}
	for _, m := range translationTagRE.FindAllStringSubmatch(text, -1) {
		parsed := []string{}
		valid := true
		for _, raw := range strings.FieldsFunc(m[1], func(r rune) bool { return r == '+' || r == ',' }) {
			tag, err := language.Parse(raw)
			if err != nil || tag == language.Und {
				valid = false
				break
			}
			parsed = appendUnique(parsed, tag.String())
		}
		if valid {
			for _, lang := range parsed {
				langs = appendUnique(langs, lang)
			}
		}
	}
	return langs
}
func translationHeader(header string) (platform, prefix string, langs []string, err error) {
	loc := translationTagRE.FindStringSubmatchIndex(header)
	if loc == nil || loc[0] == 0 {
		return "", "", nil, fmt.Errorf("expected a translation XML DAT header with a [T-language] label")
	}
	prefix = strings.TrimSpace(header[:loc[0]])
	platform = strings.TrimSpace(strings.TrimSuffix(prefix, "[GA]"))
	label := header[loc[0]:loc[1]]
	// A collection label declares language only, not a per-patch author credit.
	if strings.Contains(label, " ") {
		return "", "", nil, fmt.Errorf("collection header needs a plain [T-language] label")
	}
	langs = translationLanguages(label)
	if len(langs) == 0 || !safeName(platform) || !safeName(prefix) {
		return "", "", nil, fmt.Errorf("invalid translation DAT platform or language")
	}
	return
}
func readDATBytes(path, member string) ([]byte, string, error) {
	if member == "" {
		b, err := metadataBytes(path)
		return b, digest(b), err
	}
	if !safeRelative(member) {
		return nil, "", fmt.Errorf("unsafe DAT archive member")
	}
	z, err := zip.OpenReader(path)
	if err != nil {
		return nil, "", err
	}
	defer z.Close()
	var found *zip.File
	for _, f := range z.File {
		if f.Name == member {
			if found != nil {
				return nil, "", fmt.Errorf("duplicate DAT archive member")
			}
			found = f
		}
	}
	if found == nil || found.FileInfo().IsDir() || found.Mode()&os.ModeSymlink != 0 || found.UncompressedSize64 > 32<<20 {
		return nil, "", fmt.Errorf("missing, oversized or unsupported DAT member")
	}
	r, err := found.Open()
	if err != nil {
		return nil, "", err
	}
	b, err := io.ReadAll(io.LimitReader(r, (32<<20)+1))
	closeErr := r.Close()
	if err != nil {
		return nil, "", err
	}
	if closeErr != nil {
		return nil, "", closeErr
	}
	if len(b) > 32<<20 {
		return nil, "", fmt.Errorf("translation DAT exceeds 32 MiB")
	}
	// Pin the complete source container, as with other local source snapshots.
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return nil, "", err
	}
	return b, fmt.Sprintf("%x", h.Sum(nil)), nil
}
