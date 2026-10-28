package main

import (
	"archive/zip"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type discoveredSource struct {
	Path      string   `json:"path"`
	Member    string   `json:"member,omitempty"`
	Type      string   `json:"type"`
	Platform  string   `json:"platform,omitempty"`
	Languages []string `json:"languages,omitempty"`
	Status    string   `json:"status"`
	Reason    string   `json:"reason,omitempty"`
}
type sourceInventory struct {
	DAT, GA       []datReference
	RHA, HackHash []string
	Inputs        []discoveredSource
	Ignored       int
}

func metadataBytes(name string) ([]byte, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (32<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > 32<<20 {
		return nil, fmt.Errorf("metadata exceeds 32 MiB")
	}
	return b, nil
}

// Only metadata files are inspected. Unknown files are counted, never interpreted
// as output identities, and the dedicated sources tree stays read-only.
func inspectDiscoverySources(o options, d discoveryOptions) (*sourceInventory, error) {
	out := &sourceInventory{}
	seen := map[string]bool{}
	explicit := map[string]string{}
	for _, p := range []struct{ path, kind string }{{d.GA, "gaming-alexandria"}, {d.RHA, "romhack-archive"}, {d.HackHash, "hackhash"}} {
		if p.path == "" {
			continue
		}
		if !safeRelative(p.path) && !(p.path == "." && p.kind == "romhack-archive") {
			return nil, fmt.Errorf("unsafe explicit source path")
		}
		explicit[p.path] = p.kind
		switch p.kind {
		case "gaming-alexandria":
			out.GA = append(out.GA, datReference{Path: p.path})
		case "romhack-archive":
			out.RHA = appendUnique(out.RHA, p.path)
		case "hackhash":
			out.HackHash = appendUnique(out.HackHash, p.path)
		}
		out.Inputs = append(out.Inputs, discoveredSource{Path: p.path, Type: p.kind, Status: "selected"})
	}
	selections := append(append([]string{}, d.DAT...), d.TEN...)
	autoDAT := len(selections) == 0
	selectedFiles := map[string]bool{}
	probeXML := func(ref datReference, b []byte, required bool) error {
		var h struct {
			XMLName xml.Name `xml:"datafile"`
			Header  struct {
				Name string `xml:"name"`
			} `xml:"header"`
		}
		err := xml.Unmarshal(b, &h)
		if err != nil {
			if required {
				return fmt.Errorf("%s: invalid XML DAT: %w", ref.Path, err)
			}
			out.Inputs = append(out.Inputs, discoveredSource{Path: ref.Path, Member: ref.Member, Type: "unknown", Status: "ignored", Reason: "not a supported XML DAT"})
			out.Ignored++
			return nil
		}
		p, _, langs, err := translationHeader(h.Header.Name)
		if err != nil {
			if required {
				return fmt.Errorf("%s: %w", ref.Path, err)
			}
			out.Ignored++
			return nil
		}
		if strings.Contains(h.Header.Name, "[GA]") && strings.Contains(strings.ToLower(p), "famicom") && len(langs) == 1 && langs[0] == "en" && explicit[ref.Path] == "" {
			out.GA = append(out.GA, ref)
			out.Inputs = append(out.Inputs, discoveredSource{Path: ref.Path, Member: ref.Member, Type: "gaming-alexandria", Status: "selected"})
		}
		if autoDAT || selectedFiles[ref.Path] {
			out.DAT = append(out.DAT, ref)
			out.Inputs = append(out.Inputs, discoveredSource{Path: ref.Path, Member: ref.Member, Type: "translation-dat", Platform: p, Languages: langs, Status: "selected"})
		}
		return nil
	}
	var file func(string, bool) error
	file = func(relative string, required bool) error {
		if seen[relative] {
			return nil
		}
		seen[relative] = true
		name, err := regularInput(o.Sources, relative)
		if err != nil {
			return err
		}
		ext := strings.ToLower(filepath.Ext(name))
		info, err := os.Stat(name)
		if err != nil {
			return err
		}
		limit := int64(32 << 20)
		if ext == ".zip" {
			limit = 256 << 20
		}
		if info.Size() > limit {
			if required {
				return fmt.Errorf("%s: source metadata exceeds supported size", relative)
			}
			out.Inputs = append(out.Inputs, discoveredSource{Path: relative, Type: "unknown", Status: "ignored", Reason: "source metadata exceeds supported size"})
			out.Ignored++
			return nil
		}
		switch ext {
		case ".zip":
			z, err := zip.OpenReader(name)
			if err != nil {
				return err
			}
			defer z.Close()
			matched := len(out.DAT)
			names := map[string]bool{}
			for _, f := range z.File {
				ext := strings.ToLower(filepath.Ext(f.Name))
				if f.FileInfo().IsDir() || (ext != ".dat" && ext != ".xml") {
					continue
				}
				if !safeRelative(f.Name) || f.Mode()&os.ModeSymlink != 0 {
					return fmt.Errorf("unsafe or oversized metadata member in %s", relative)
				}
				if f.UncompressedSize64 > 32<<20 {
					out.Inputs = append(out.Inputs, discoveredSource{Path: relative, Member: f.Name, Type: "unknown", Status: "ignored", Reason: "metadata member exceeds 32 MiB"})
					out.Ignored++
					continue
				}
				if names[f.Name] {
					return fmt.Errorf("duplicate metadata member in %s", relative)
				}
				names[f.Name] = true
				r, err := f.Open()
				if err != nil {
					return err
				}
				b, err := io.ReadAll(io.LimitReader(r, (32<<20)+1))
				ce := r.Close()
				if err != nil {
					return err
				}
				if ce != nil {
					return ce
				}
				if len(b) > 32<<20 {
					return fmt.Errorf("oversized metadata member in %s", relative)
				}
				if err = probeXML(datReference{Path: relative, Member: f.Name}, b, false); err != nil {
					return err
				}
			}
			if required && len(out.DAT) == matched {
				return fmt.Errorf("%s: ZIP contains no supported translation XML DAT", relative)
			}
		case ".dat", ".xml":
			b, err := metadataBytes(name)
			if err != nil {
				return err
			}
			return probeXML(datReference{Path: relative}, b, required)
		case ".json":
			if explicit[relative] == "hackhash" {
				return nil
			}
			b, err := metadataBytes(name)
			if err != nil {
				return err
			}
			var v struct {
				Machines []struct {
					ROMName  string          `json:"romName"`
					Platform string          `json:"platform"`
					Details  json.RawMessage `json:"details"`
				} `json:"machines"`
			}
			if json.Unmarshal(b, &v) == nil && len(v.Machines) > 0 && v.Machines[0].ROMName != "" && v.Machines[0].Platform != "" {
				detailed := false
				for _, m := range v.Machines {
					if len(m.Details) > 0 && string(m.Details) != "null" {
						detailed = true
						break
					}
				}
				status, reason := "selected", ""
				if detailed {
					out.HackHash = appendUnique(out.HackHash, relative)
				} else {
					status = "ignored"
					reason = "minimal JSON lacks parent relationships; download HackHash format=detailed"
					out.Ignored++
				}
				out.Inputs = append(out.Inputs, discoveredSource{Path: relative, Type: "hackhash", Status: status, Reason: reason})
			} else {
				out.Ignored++
			}
		default:
			if required {
				return fmt.Errorf("%s: select an XML .dat/.xml, a ZIP of DATs, or a directory", relative)
			}
		}
		return nil
	}
	// --dat narrows only DAT selection. Optional metadata adapters are discovered
	// from the same root without additional flags.

	for _, selection := range selections {
		if selection != "." && !safeRelative(selection) {
			return nil, fmt.Errorf("unsafe --dat selection")
		}
		path := filepath.Join(o.Sources, filepath.FromSlash(selection))
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("symlink DAT selection")
		}
		if !info.IsDir() {
			selectedFiles[selection] = true
			if err = file(selection, true); err != nil {
				return nil, err
			}
			continue
		}
		err = filepath.WalkDir(path, func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("symlink in DAT selection")
			}
			if entry.IsDir() {
				if strings.HasPrefix(entry.Name(), ".") && name != path {
					return filepath.SkipDir
				}
				return nil
			}
			rel, err := filepath.Rel(o.Sources, name)
			if err != nil {
				return err
			}
			selectedFiles[filepath.ToSlash(rel)] = true
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	err := filepath.WalkDir(o.Sources, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") && name != o.Sources {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink in source metadata tree")
		}
		rel, err := filepath.Rel(o.Sources, name)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.Name() == "romhack.json" {
			parts := strings.Split(rel, "/")
			for i, part := range parts {
				if part == "database" && len(parts)-i-1 == 4 {
					root := strings.Join(parts[:i], "/")
					if root == "" {
						root = "."
					}
					if !containsString(out.RHA, root) {
						out.RHA = append(out.RHA, root)
						out.Inputs = append(out.Inputs, discoveredSource{Path: root, Type: "romhack-archive", Status: "selected"})
					}
					return nil
				}
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(rel))
		if ext == ".json" || ext == ".dat" || ext == ".xml" || ext == ".zip" {
			return file(rel, false)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(out.DAT) == 0 {
		return nil, fmt.Errorf("no translation XML DATs found under --sources; see docs/prepare-variants.md for downloads (or use --dat)")
	}
	sort.Slice(out.DAT, func(i, j int) bool {
		a, b := out.DAT[i], out.DAT[j]
		return a.Path+"\x00"+a.Member < b.Path+"\x00"+b.Member
	})
	for _, xs := range [][]string{out.RHA, out.HackHash} {
		sort.Strings(xs)
	}
	return out, nil
}
func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
