package main

import (
	"encoding/xml"
	"fmt"
	"os"
	"strings"
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
	header   string
	outputs  map[string]fingerprint
	sha256   string
	machines []tenMachine
}

// readTEn reads a local published output list. No original game is inferred from
// the output label, patch credits, header or a default catalog release.
func readTEn(o options, name string, m *manifest) (*tenSource, error) {
	if o.Sources == "" {
		return nil, fmt.Errorf("--sources required for T-En DAT references")
	}
	path, err := regularInput(o.Sources, name)
	if err != nil {
		return nil, err
	}
	before, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if before.Size() > 32<<20 {
		return nil, fmt.Errorf("T-En DAT exceeds 32 MiB")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	after, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !sameStat(before, after) {
		return nil, fmt.Errorf("T-En DAT changed while reading")
	}
	hash := digest(raw)
	pinned := false
	for _, s := range m.Snapshots {
		if s.Path == name {
			if !strings.EqualFold(s.SHA256, hash) {
				return nil, fmt.Errorf("pinned T-En DAT changed: %s", name)
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
	if !strings.Contains(strings.ToLower(d.Header.Name), "[t-en]") {
		return nil, fmt.Errorf("expected a T-En XML DAT header")
	}
	result := &tenSource{header: d.Header.Name, outputs: map[string]fingerprint{}, sha256: hash, machines: append(d.Machines, d.Games...)}
	add := func(row tenOutput, kind string) error {
		if row.Status == "nodump" {
			return nil
		}
		f := fingerprint{Name: row.Name, Kind: kind, Size: row.Size, SHA1: strings.ToLower(row.SHA1), MD5: strings.ToLower(row.MD5), CRC32: strings.ToLower(row.CRC32)}
		if kind != "" && (row.MD5 != "" || row.CRC32 != "" || row.Size != 0) {
			return fmt.Errorf("disk record has ambiguous raw-file attributes: %s", row.Name)
		}
		if _, exists := result.outputs[row.Name]; exists {
			return fmt.Errorf("duplicate T-En output name: %s", row.Name)
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
			return fmt.Errorf("T-En output needs a separate reviewed original-game link")
		}
		src := loaded[e.DAT.Path]
		if src == nil {
			var err error
			src, err = readTEn(o, e.DAT.Path, m)
			if err != nil {
				return err
			}
			loaded[e.DAT.Path] = src
		}
		sourcePlatform := e.Platform
		if e.DAT.SourcePlatform != "" {
			sourcePlatform = e.DAT.SourcePlatform
		}
		if !safeName(sourcePlatform) {
			return fmt.Errorf("unsafe T-En source platform")
		}
		if !strings.HasPrefix(strings.ToLower(src.header), strings.ToLower(sourcePlatform+" [T-En]")) {
			return fmt.Errorf("T-En platform differs from selected game: %s", e.Platform)
		}
		output, ok := src.outputs[e.DAT.Name]
		if !ok {
			return fmt.Errorf("T-En output missing or unsupported: %s", e.DAT.Name)
		}
		supplied := e.File
		if supplied.Name != "" && supplied.Name != output.Name || supplied.Kind != "" && supplied.Kind != output.Kind || supplied.Size != 0 && supplied.Size != output.Size {
			return fmt.Errorf("supplied output differs from T-En record")
		}
		for _, pair := range [][2]string{{supplied.SHA1, output.SHA1}, {supplied.SHA256, output.SHA256}, {supplied.MD5, output.MD5}, {supplied.CRC32, output.CRC32}} {
			if pair[0] != "" && !strings.EqualFold(pair[0], pair[1]) {
				return fmt.Errorf("supplied hash differs from T-En record")
			}
		}
		e.File = output
		link := sourceLink("T-En DAT", e.DAT.Path, src.sha256, e.DAT.Name, e.Game)
		link["role"] = "modified-output"
		e.Evidence.Links = append(e.Evidence.Links, link)
	}
	return nil
}
