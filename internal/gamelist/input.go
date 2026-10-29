// Package gamelist harvests private frontend curation with optional local NES evidence.
package gamelist

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"rommeta/internal/zipnames"
)

type Entry struct {
	Path        string `xml:"path"`
	Title       string `xml:"name"`
	Description string `xml:"desc"`
	Date        string `xml:"releasedate"`
	Developer   string `xml:"developer"`
	Publisher   string `xml:"publisher"`
	Genre       string `xml:"genre"`
	Players     string `xml:"players"`
	Rating      string `xml:"rating"`
	Region      string `xml:"region"`
	Languages   string `xml:"lang"`
	MD5         string `xml:"md5"`
	ID          string `xml:"id,attr"`
	Image       string `xml:"image"`
	Thumbnail   string `xml:"thumbnail"`
	Video       string `xml:"video"`
	Marquee     string `xml:"marquee"`
	Fanart      string `xml:"fanart"`
	Manual      string `xml:"manual"`
}
type gameList struct {
	XMLName  xml.Name `xml:"gameList"`
	Provider struct {
		System string `xml:"system"`
	} `xml:"provider"`
	Games []Entry `xml:"game"`
}
type input struct {
	origin, member, system string
	entries                []Entry
	open                   func(string) (io.ReadCloser, error)
	close                  func() error
}

func safeRelative(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") || strings.ContainsAny(name, "\\:\x00") {
		return false
	}
	for _, s := range strings.Split(name, "/") {
		if s == "" || s == "." || s == ".." {
			return false
		}
	}
	return true
}
func relativeRef(name string) (string, error) {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	name = strings.TrimPrefix(name, "./")
	if !safeRelative(name) {
		return "", fmt.Errorf("unsafe frontend path %q", name)
	}
	return name, nil
}
func openInput(options Options) (*input, error) {
	in := &input{close: func() error { return nil }}
	if strings.EqualFold(filepath.Ext(options.Input), ".zip") {
		archive, err := zip.OpenReader(options.Input)
		if err != nil {
			return nil, err
		}
		in.close = archive.Close
		files := map[string]*zip.File{}
		var lists []string
		for _, f := range archive.File {
			n := zipnames.Name(f)
			if f.FileInfo().IsDir() {
				continue
			}
			if !safeRelative(n) || f.Mode()&os.ModeSymlink != 0 {
				archive.Close()
				return nil, fmt.Errorf("unsafe ZIP member %q", n)
			}
			if files[n] != nil {
				archive.Close()
				return nil, fmt.Errorf("duplicate ZIP member %q", n)
			}
			files[n] = f
			if strings.EqualFold(path.Base(n), "gamelist.xml") {
				lists = append(lists, n)
			}
		}
		sort.Strings(lists)
		in.member = options.Member
		if in.member == "" {
			if len(lists) != 1 {
				archive.Close()
				return nil, fmt.Errorf("ZIP contains %d gamelists; select --member", len(lists))
			}
			in.member = lists[0]
		}
		if !safeRelative(in.member) {
			archive.Close()
			return nil, fmt.Errorf("unsafe gamelist member")
		}
		in.open = func(n string) (io.ReadCloser, error) {
			f := files[n]
			if f == nil {
				return nil, os.ErrNotExist
			}
			if f.UncompressedSize64 > 512<<20 {
				return nil, fmt.Errorf("oversized member %q", n)
			}
			return f.Open()
		}
		in.origin = filepath.Base(options.Input) + "/" + in.member
	} else {
		if options.Member != "" {
			return nil, fmt.Errorf("--member requires a ZIP input")
		}
		real, err := filepath.EvalSymlinks(options.Input)
		if err != nil {
			return nil, err
		}
		real, err = filepath.Abs(real)
		if err != nil {
			return nil, err
		}
		base := filepath.Dir(real)
		in.member = filepath.Base(real)
		in.origin = filepath.Base(base) + "/" + in.member
		in.open = func(n string) (io.ReadCloser, error) {
			if !safeRelative(n) {
				return nil, fmt.Errorf("unsafe source path")
			}
			actual, err := filepath.EvalSymlinks(filepath.Join(base, filepath.FromSlash(n)))
			if err != nil {
				return nil, err
			}
			rel, err := filepath.Rel(base, actual)
			if err != nil || !safeRelative(filepath.ToSlash(rel)) {
				return nil, fmt.Errorf("source path escapes frontend root")
			}
			f, err := os.Open(actual)
			if err != nil {
				return nil, err
			}
			info, err := f.Stat()
			if err != nil {
				f.Close()
				return nil, err
			}
			if !info.Mode().IsRegular() || info.Size() > 512<<20 {
				f.Close()
				return nil, fmt.Errorf("source is not a bounded regular file")
			}
			return f, nil
		}
	}
	if options.Origin != "" {
		if !safeRelative(options.Origin) {
			in.close()
			return nil, fmt.Errorf("origin must be a relative portable identifier")
		}
		in.origin = options.Origin
	}
	reader, err := in.open(in.member)
	if err != nil {
		in.close()
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(reader, 64<<20+1))
	reader.Close()
	if err != nil {
		in.close()
		return nil, err
	}
	if len(body) > 64<<20 {
		in.close()
		return nil, fmt.Errorf("gamelist exceeds 64 MiB")
	}
	var list gameList
	// Reject concatenated XML documents and trailing non-whitespace.
	decoder := xml.NewDecoder(bytes.NewReader(body))
	if err = decoder.Decode(&list); err == nil {
		for {
			tok, e := decoder.Token()
			if e == io.EOF {
				break
			}
			if e != nil {
				err = e
				break
			}
			if text, ok := tok.(xml.CharData); !ok || strings.TrimSpace(string(text)) != "" {
				err = fmt.Errorf("trailing XML content")
				break
			}
		}
	}
	if err != nil {
		in.close()
		return nil, err
	}
	in.system = strings.TrimSpace(list.Provider.System)
	in.entries = list.Games
	return in, nil
}
