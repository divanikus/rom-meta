package main

import (
	"archive/zip"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/zipnames"
)

var creditRE = regexp.MustCompile(`\[T-En(?: by)? ([^\]]+)\]`)
var versionRE = regexp.MustCompile(`\s+v([0-9].*)$`)
var tokenRE = regexp.MustCompile(`[\p{L}\p{N}]+`)

func translationCredit(s string) (string, string) {
	m := creditRE.FindStringSubmatch(s)
	if m == nil {
		return "", ""
	}
	group := m[1]
	v := versionRE.FindStringSubmatchIndex(group)
	if v == nil {
		return group, ""
	}
	return strings.TrimSpace(group[:v[0]]), group[v[2]:v[3]]
}
func stripROMExtension(s string) string {
	switch strings.ToLower(filepath.Ext(s)) {
	case ".nes", ".sfc", ".smc", ".ngc", ".ngp", ".gb", ".gbc", ".gba", ".nds", ".z64", ".v64", ".n64", ".gg", ".sms", ".md", ".gen", ".bin", ".iso", ".img", ".zip":
		return strings.TrimSuffix(s, filepath.Ext(s))
	}
	return s
}
func literalName(s string) string {
	return strings.Join(strings.Fields(cases.Fold().String(norm.NFKC.String(stripROMExtension(s)))), " ")
}
func tokenName(s string) string {
	return strings.Join(tokenRE.FindAllString(literalName(s), -1), "\x00")
}
func sourceIdentity(p string, f fingerprint) string {
	return fmt.Sprintf("%s\x00%d\x00%s", p, f.Size, strings.ToLower(f.SHA1))
}

type parentBundle struct {
	Platform, Game string
	Files          []*copyFile
}
type parentTarget struct {
	Bundle  *parentBundle
	Release string
}

func (t parentTarget) key() string {
	return t.Bundle.Platform + "\x00" + t.Bundle.Game + "\x00" + t.Release
}

type baseFingerprint struct {
	Target parentTarget
	File   fingerprint
}
type parentIndex struct {
	Literal, Tokens map[string][]parentTarget
	Base            []baseFingerprint
	Owned           map[string]bool
	Files           []*copyFile
}

func catalogParents(o options, platforms []string) (*parentIndex, error) {
	idx := &parentIndex{Literal: map[string][]parentTarget{}, Tokens: map[string][]parentTarget{}, Owned: map[string]bool{}}
	add := func(t parentTarget, name string) {
		if name == "" {
			return
		}
		prefix := t.Bundle.Platform + "\x00"
		idx.Literal[prefix+literalName(name)] = append(idx.Literal[prefix+literalName(name)], t)
		idx.Tokens[prefix+tokenName(name)] = append(idx.Tokens[prefix+tokenName(name)], t)
	}
	for _, p := range platforms {
		if !safeName(p) {
			return nil, fmt.Errorf("unsafe platform %q", p)
		}
		folder := filepath.Join(o.Catalog, "platforms", p)
		entries, err := catalogfs.ReadDir(folder)
		if err != nil {
			return nil, err
		}
		for _, game := range entries {
			if !game.IsDir() || strings.HasPrefix(game.Name(), ".") || strings.HasPrefix(game.Name(), "_") {
				continue
			}
			b := &parentBundle{Platform: p, Game: game.Name()}
			gfile, err := planFile(o.Catalog, filepath.Join("platforms", p, game.Name(), "game.yaml"), true)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, err
			}
			fpfile, err := planFile(o.Catalog, filepath.Join("platforms", p, game.Name(), "fingerprints.yaml"), true)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, err
			}
			b.Files = []*copyFile{gfile, fpfile}
			idx.Files = append(idx.Files, b.Files...)
			g, err := catalogyaml.Parse(gfile.data)
			if err != nil {
				return nil, err
			}
			f, err := catalogyaml.Parse(fpfile.data)
			if err != nil {
				return nil, err
			}
			if err = rejectDuplicateYAML(g.Root()); err != nil {
				return nil, err
			}
			if err = rejectDuplicateYAML(f.Root()); err != nil {
				return nil, err
			}
			if err = checkBundle(g, f); err != nil {
				return nil, err
			}
			err = catalogyaml.Mapping(catalogyaml.Value(g.Root(), "releases"), func(name string, n *yaml.Node) error {
				t := parentTarget{b, name}
				add(t, name)
				add(t, catalogyaml.Scalar(catalogyaml.Value(catalogyaml.Value(n, "authority"), "entry_name")))
				return nil
			})
			if err != nil {
				return nil, err
			}
			items := catalogyaml.Value(f.Root(), "items")
			if items == nil {
				items = catalogyaml.Value(f.Root(), "roms")
			}
			err = catalogyaml.Sequence(items, func(n *yaml.Node) error {
				release := catalogyaml.Scalar(catalogyaml.Value(n, "release"))
				file := catalogyaml.Value(n, "file")
				if catalogyaml.Scalar(catalogyaml.Value(file, "kind")) != "" {
					return nil
				}
				value := func(key string) string { return strings.ToLower(catalogyaml.Scalar(catalogyaml.Value(file, key))) }
				hash := fingerprint{Size: catalogyaml.Int64(catalogyaml.Value(file, "size")), SHA1: value("sha1"), SHA256: value("sha256"), MD5: value("md5"), CRC32: value("crc32")}
				if hash.SHA1 != "" {
					idx.Owned[sourceIdentity(p, hash)] = true
				}
				if release == "" {
					return nil
				}
				t := parentTarget{b, release}
				add(t, catalogyaml.Scalar(catalogyaml.Value(file, "name")))
				add(t, catalogyaml.Scalar(catalogyaml.Value(catalogyaml.Value(n, "source"), "entry_name")))
				idx.Base = append(idx.Base, baseFingerprint{t, hash})
				return nil
			})
			if err != nil {
				return nil, err
			}
		}
	}
	return idx, nil
}
func uniqueTargets(in []parentTarget) []parentTarget {
	m := map[string]parentTarget{}
	for _, t := range in {
		m[t.key()] = t
	}
	keys := []string{}
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := []parentTarget{}
	for _, k := range keys {
		out = append(out, m[k])
	}
	return out
}
func (idx *parentIndex) parent(s sourceRecord) ([]parentTarget, string, error) {
	prefix := s.Platform + "\x00"
	named := uniqueTargets(idx.Literal[prefix+literalName(s.Parent)])
	method := "literal"
	if len(named) == 0 {
		named = uniqueTargets(idx.Tokens[prefix+tokenName(s.Parent)])
		method = "token-preserving"
	}
	strong := false
	for _, key := range []string{"sha1", "sha256"} {
		if v, ok := s.Base[key].(string); ok && v != "" {
			strong = true
		}
	}
	if !strong {
		return named, method, nil
	}
	matched := []parentTarget{}
	for _, base := range idx.Base {
		if base.Target.Bundle.Platform != s.Platform {
			continue
		}
		agree := true
		for key, value := range map[string]string{"sha1": base.File.SHA1, "sha256": base.File.SHA256, "md5": base.File.MD5, "crc32": base.File.CRC32} {
			if want, ok := s.Base[key].(string); ok && want != "" && !strings.EqualFold(want, value) {
				agree = false
			}
		}
		if agree {
			matched = append(matched, base.Target)
		}
	}
	matched = uniqueTargets(matched)
	if len(matched) == 0 {
		return nil, "", fmt.Errorf("published base hashes do not match an official catalog release")
	}
	if len(named) > 0 {
		overlap := false
		for _, a := range named {
			for _, b := range matched {
				if a.key() == b.key() {
					overlap = true
				}
			}
		}
		if !overlap {
			return nil, "", fmt.Errorf("published base hashes conflict with the source parent name")
		}
	}
	return matched, "source-base-hash", nil
}

type collectedInput struct {
	Platform, Relative, Problem string
	Input                       input
	File                        fingerprint
	Info                        os.FileInfo
}

func collectInputs(o options, platforms []string, subdir string) ([]collectedInput, error) {
	if !safeRelative(subdir) {
		return nil, fmt.Errorf("unsafe translation subdirectory")
	}
	result := []collectedInput{}
	for _, p := range platforms {
		root := filepath.Join(o.ROMRoot, p, filepath.FromSlash(subdir))
		err := filepath.WalkDir(root, func(name string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("symlink in ROM selection")
			}
			if d.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(name))
			if map[string]bool{".chd": true, ".iso": true, ".img": true, ".cue": true, ".gdi": true, ".rvz": true, ".7z": true, ".rar": true}[ext] {
				rel, e := filepath.Rel(o.ROMRoot, name)
				if e != nil {
					return e
				}
				info, e := os.Stat(name)
				if e != nil {
					return e
				}
				result = append(result, collectedInput{Platform: p, Relative: filepath.ToSlash(rel), Info: info, Problem: "representation needs a dedicated adapter"})
				return nil
			}
			if ext != ".zip" && stripROMExtension(name) == name {
				return nil
			}
			rel, err := filepath.Rel(o.ROMRoot, name)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			real, err := regularInput(o.ROMRoot, rel)
			if err != nil {
				return err
			}
			before, err := os.Stat(real)
			if err != nil {
				return err
			}
			c := collectedInput{Platform: p, Relative: rel, Info: before, Input: input{Path: rel, Size: before.Size(), MTimeNS: before.ModTime().UnixNano()}}
			f, err := os.Open(real)
			if err != nil {
				return err
			}
			defer f.Close()
			var reader io.Reader = f
			if ext == ".zip" {
				z, err := zip.NewReader(f, before.Size())
				if err != nil {
					return err
				}
				var selected *zip.File
				for _, member := range z.File {
					memberName := zipnames.Name(member)
					if member.FileInfo().IsDir() || stripROMExtension(memberName) == memberName || strings.EqualFold(filepath.Ext(memberName), ".bin") || strings.EqualFold(filepath.Ext(memberName), ".iso") {
						continue
					}
					if !safeRelative(memberName) || selected != nil || member.Mode()&os.ModeSymlink != 0 {
						return fmt.Errorf("unsafe or multiple ROM payloads in %s", rel)
					}
					selected = member
				}
				if selected == nil {
					return fmt.Errorf("no supported cartridge payload in %s", rel)
				}
				r, err := selected.Open()
				if err != nil {
					return err
				}
				defer r.Close()
				reader = r
				c.Input.Member = zipnames.Name(selected)
			}
			a, b, m, crc := sha1.New(), sha256.New(), md5.New(), crc32.NewIEEE()
			size, err := io.Copy(io.MultiWriter(a, b, m, crc), io.LimitReader(reader, (512<<20)+1))
			if err != nil {
				return err
			}
			if size <= 0 || size > 512<<20 {
				return fmt.Errorf("unsupported payload size in %s", rel)
			}
			after, err := os.Stat(real)
			if err != nil {
				return err
			}
			if !sameStat(before, after) {
				return fmt.Errorf("ROM changed while collecting")
			}
			c.File = fingerprint{Name: filepath.Base(rel), Size: size, SHA1: fmt.Sprintf("%x", a.Sum(nil)), SHA256: fmt.Sprintf("%x", b.Sum(nil)), MD5: fmt.Sprintf("%x", m.Sum(nil)), CRC32: fmt.Sprintf("%08x", crc.Sum32())}
			if c.Input.Member != "" {
				c.File.Name = c.Input.Member
			}
			result = append(result, c)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Relative < result[j].Relative })
	return result, nil
}
func readableName(fingerprintName string) string {
	// This is only a display label; parent resolution never uses the input name.
	name := stripROMExtension(filepath.Base(fingerprintName))
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
}
