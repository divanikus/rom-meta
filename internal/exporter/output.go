package exporter

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"rommeta/internal/catalogfs"
)

func prospectivePath(name string) (string, error) {
	abs, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	current := abs
	var suffix []string
	for {
		_, err := catalogfs.Stat(current)
		if err == nil {
			real, err := catalogfs.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				real = filepath.Join(real, suffix[i])
			}
			return real, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

type xmlGame struct {
	Hidden      bool   `xml:"hidden,omitempty"`
	Path        string `xml:"path"`
	Name        string `xml:"name"`
	Description string `xml:"desc,omitempty"`
	Date        string `xml:"releasedate,omitempty"`
	Developer   string `xml:"developer,omitempty"`
	Publisher   string `xml:"publisher,omitempty"`
	Genre       string `xml:"genre,omitempty"`
	Players     string `xml:"players,omitempty"`
}
type xmlList struct {
	XMLName xml.Name  `xml:"gameList"`
	Games   []xmlGame `xml:"game"`
	Folders []xmlGame `xml:"folder"`
}

func RenderGamelist(plan *Plan) ([]byte, error) {
	list := xmlList{Games: []xmlGame{}}
	for _, r := range plan.Records {
		if !r.Hidden && (r.Status == "unsupported" || (systems[plan.System].Arcade || r.Catalog != nil || r.Representation == "romset-payload") && r.Status != "matched") {
			continue
		}
		get := func(key string) string { return r.Fields[key].Value }
		date := ""
		if parsed, err := time.Parse("2006-01-02", get("release_date")); err == nil {
			date = parsed.Format("20060102T000000")
		}
		entry := xmlGame{Hidden: r.Hidden, Path: "./" + r.ROM, Name: get("title"), Description: recordDescription(r), Date: date, Developer: get("developer"), Publisher: get("publisher"), Genre: get("genres"), Players: get("players")}
		if r.Catalog != nil && r.Catalog.Directory {
			list.Folders = append(list.Folders, entry)
		} else {
			list.Games = append(list.Games, entry)
		}
	}
	body, err := xml.MarshalIndent(list, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), append(body, '\n')...), nil
}

// Apply writes a new, complete local/personal package. Existing outputs are never
// replaced. The caller can create a fresh output directory for subsequent runs.
func Apply(plan *Plan, progress func(string, string, int, int)) error {
	staging, err := beginPackage(plan.DataRoot, plan.ROMRoot, plan.Out)
	if err != nil {
		return err
	}
	defer catalogfs.RemoveAll(staging)
	if err := writePlan(plan, staging, "", progress); err != nil {
		return err
	}
	return publishPackage(staging, plan.Out)
}

func beginPackage(dataRoot, romRoot, out string) (string, error) {
	current, err := prospectivePath(out)
	if err != nil {
		return "", err
	}
	if current != out {
		return "", fmt.Errorf("output parent changed since planning")
	}
	if dataRoot != "" && (within(dataRoot, current) || within(current, dataRoot)) || within(romRoot, current) || within(current, romRoot) {
		return "", fmt.Errorf("output overlaps input trees")
	}
	if _, err := catalogfs.Lstat(out); err == nil {
		return "", fmt.Errorf("output already exists; choose a new directory: %s", out)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	parent := filepath.Dir(out)
	if err := catalogfs.MkdirAll(parent, 0o755); err != nil {
		return "", err
	}
	return os.MkdirTemp(parent, "rommeta-export-")
}
func publishPackage(staging, out string) error {
	if _, err := catalogfs.Lstat(out); err == nil {
		return fmt.Errorf("output appeared during export; staged result was not published")
	} else if !os.IsNotExist(err) {
		return err
	}
	return catalogfs.Rename(staging, out)
}

func writePlan(plan *Plan, staging, sidecarDir string, progress func(string, string, int, int)) error {
	var err error
	for i := range plan.Records {
		if plan.Context != nil {
			if err := plan.Context.Err(); err != nil {
				return err
			}
		}
		record := &plan.Records[i]
		if progress != nil {
			progress("write", record.ROM, i+1, len(plan.Records))
		}
		if record.Catalog != nil {
			if err := CheckROMSetObservation(plan.ROMRoot, record.ROM, record.Catalog); err != nil {
				return err
			}
		}
		if record.SHA1 != "" {
			inputName := record.ROM
			if record.LaunchFile != "" {
				inputName = record.LaunchFile
			}
			input := filepath.Join(plan.ROMRoot, filepath.FromSlash(inputName))
			real, err := realPath(input)
			if err != nil {
				return err
			}
			if !within(plan.ROMRoot, real) {
				return fmt.Errorf("ROM path escaped input root")
			}
			reader := plan.System
			if record.Representation == "romset-payload" {
				reader = "romset"
			}
			hashes, member, err := hashROMContext(plan.Context, real, reader)
			if err != nil {
				return err
			}
			if hashes.sha1 != record.SHA1 || hashes.size != record.Size || member != record.Member {
				return fmt.Errorf("ROM changed since planning: %s", record.ROM)
			}
		}
		for _, part := range record.Playlist {
			path, err := realPath(filepath.Join(plan.ROMRoot, filepath.FromSlash(part.ROM)))
			if err != nil {
				return err
			}
			if !within(plan.ROMRoot, path) {
				return fmt.Errorf("playlist input escapes ROM root")
			}
			h, _, err := hashROMContext(plan.Context, path, plan.System)
			if err != nil {
				return err
			}
			if h.size != part.Size || h.sha1 != part.SHA1 {
				return fmt.Errorf("playlist member changed since planning: %s", part.ROM)
			}
		}
		roleNames := make([]string, 0, len(record.Media))
		for role := range record.Media {
			roleNames = append(roleNames, role)
		}
		sort.Strings(roleNames)
		for _, role := range roleNames {
			media := record.Media[role]
			mediaRoot := plan.DataRoot
			if plan.MediaRoot != "" {
				mediaRoot = plan.MediaRoot
			}
			sourcePath := media.SourcePath
			if cached, ok := plan.MediaPaths[sourcePath]; ok {
				sourcePath = cached
			}
			source := filepath.Join(mediaRoot, filepath.FromSlash(sourcePath))
			real, err := realPath(source)
			if err != nil {
				return err
			}
			if !within(mediaRoot, real) {
				return fmt.Errorf("media escaped data root")
			}
			destination := filepath.Join(staging, filepath.FromSlash(media.OutputPath))
			if !within(staging, destination) {
				return fmt.Errorf("invalid media output path")
			}
			digest, err := copyAssetContext(plan.Context, real, destination, media.Size)
			if err != nil {
				return err
			}
			if media.SHA256 != "" && digest != media.SHA256 {
				return fmt.Errorf("media digest changed since export planning")
			}
			media.SHA256 = digest
			record.Media[role] = media
		}
	}
	if err := writeLaunchBoxPlaylists(plan, staging); err != nil {
		return err
	}
	xmlBytes, err := RenderGamelist(plan)
	if plan.Target == "launchbox" {
		xmlBytes, err = RenderLaunchBox(plan)
	}
	if err != nil {
		return err
	}
	xmlPath := filepath.Join(staging, filepath.FromSlash(outputXMLPath(plan)))
	if previous, readErr := catalogfs.ReadFile(xmlPath); readErr == nil && plan.Target == "launchbox" {
		// staging is owned by beginPackage; this cannot replace an installed library.
		merged, err := mergeGeneratedLaunchBox(previous, xmlBytes)
		if err != nil {
			return err
		}
		if err := catalogfs.WriteFile(xmlPath, merged, 0o644); err != nil {
			return err
		}
	} else if err := writeOutput(xmlPath, xmlBytes); err != nil {
		return err
	}
	manifest, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	if err := writeOutput(filepath.Join(staging, sidecarDir, "manifest.json"), append(manifest, '\n')); err != nil {
		return err
	}
	if err := writeOutput(filepath.Join(staging, sidecarDir, "report.txt"), []byte(report(plan))); err != nil {
		return err
	}
	if err := writeOutput(filepath.Join(staging, sidecarDir, "README.txt"), []byte(instructions(plan))); err != nil {
		return err
	}
	prefix := ""
	if sidecarDir != "" {
		relative, err := filepath.Rel(sidecarDir, ".")
		if err != nil {
			return err
		}
		prefix = filepath.ToSlash(relative) + "/"
	}
	preview, err := template.New("preview").Funcs(template.FuncMap{
		"value":   func(r Record, key string) string { return r.Fields[key].Value },
		"xmlPath": outputXMLPath,
		"asset": func(r Record, role string) string {
			if path := r.Media[role].OutputPath; path != "" {
				return prefix + path
			}
			return ""
		},
		"root": func(path string) string { return prefix + path },
	}).Parse(previewTemplate)
	if err != nil {
		return err
	}
	previewPath := filepath.Join(staging, sidecarDir, "preview.html")
	if err := catalogfs.MkdirAll(filepath.Dir(previewPath), 0o755); err != nil {
		return err
	}
	output, err := catalogfs.OpenFile(previewPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	renderErr := preview.Execute(output, plan)
	closeErr := output.Close()
	if renderErr != nil {
		return renderErr
	}
	if closeErr != nil {
		return closeErr
	}
	return nil
}

func copyAsset(source, destination string, expected int64) (string, error) {
	return copyAssetContext(context.Background(), source, destination, expected)
}
func copyAssetContext(ctx context.Context, source, destination string, expected int64) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	input, err := catalogfs.Open(source)
	if err != nil {
		return "", err
	}
	defer input.Close()
	before, err := input.Stat()
	if err != nil {
		return "", err
	}
	if !before.Mode().IsRegular() || before.Size() != expected {
		return "", fmt.Errorf("media changed since planning: %s", source)
	}
	if err := catalogfs.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return "", err
	}
	output, err := catalogfs.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(output, h), contextReader{ctx: ctx, input: input})
	closeErr := output.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	after, err := input.Stat()
	if err != nil {
		return "", err
	}
	if size != expected || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return "", fmt.Errorf("media changed while copying: %s", source)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
func writeOutput(path string, contents []byte) error {
	if err := catalogfs.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return catalogfs.WriteFile(path, contents, 0o644)
}
func report(plan *Plan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "RomMeta offline %s export\nPlatform: %s\nROMs: %d\nMatched: %d\nAmbiguous: %d\nUnmatched: %d\nUnsupported: %d\nMedia files: %d\nMedia bytes: %d\nScope: local/personal\n\n", plan.Target, plan.Platform, len(plan.Records), plan.Matched, plan.Ambiguous, plan.Unmatched, plan.Unsupported, plan.MediaFiles, plan.MediaBytes)
	fmt.Fprintf(&b, "Bindings: %d hash / %d release/set-name / %d game-name\n\n", plan.HashMatches, plan.NameMatches, plan.GameOnly)
	if plan.LaunchBox != nil {
		fmt.Fprintf(&b, "LaunchBox: %d game cards / %d versions\n", len(plan.LaunchBox.Groups), plan.LaunchBox.VersionCount)
		for _, group := range plan.LaunchBox.Groups {
			fmt.Fprintf(&b, "Game: %s | %s\n", group.Title, group.Game)
			for _, version := range group.Versions {
				marker := "version"
				if version.ID == group.Primary {
					marker = "primary"
				}
				fmt.Fprintf(&b, "  %s: %s -> %s\n", marker, version.Name, version.Path)
			}
		}
		for _, warning := range plan.LaunchBox.Warnings {
			fmt.Fprintf(&b, "Warning: %s\n", warning)
		}
		b.WriteString("\n")
	}
	c := Coverage(plan)
	if c.SourceSets > 0 {
		fmt.Fprintf(&b, "Source sets requiring extraction/installation and launch setup: %d\n", c.SourceSets)
	}
	fmt.Fprintf(&b, "Matched display coverage: %d/%d descriptions / %d/%d covers\n", c.Descriptions, c.Matched, c.Covers, c.Matched)
	fmt.Fprintf(&b, "Game-scope descriptions: %d | Unknown artwork variants: %d\n\n", c.GameDescriptions, c.UnknownVariants)
	for _, record := range plan.Records {
		fmt.Fprintf(&b, "[%s] %s\n", record.Status, record.ROM)
		fmt.Fprintf(&b, "  Binding: %s | %s\n", record.MatchedBy, record.Reason)
		if record.ArcadeSnapshot != "" {
			fmt.Fprintf(&b, "  Catalog MAME snapshot: %s (input version not verified)\n", record.ArcadeSnapshot)
		}
		if record.Release == "" && record.MetadataRelease != "" {
			fmt.Fprintf(&b, "  Metadata basis: %s\n", record.MetadataRelease)
		}
		if record.Release != "" {
			fmt.Fprintf(&b, "  Release: %s\n", record.Release)
		} else {
			fmt.Fprintf(&b, "  %s\n", record.Reason)
		}
		if description, ok := record.Fields["description"]; ok {
			fmt.Fprintf(&b, "  Description: %s / %s\n", description.Source, description.Language)
		} else if record.Status == "matched" {
			b.WriteString("  No eligible description\n")
		}
		for _, warning := range record.Warnings {
			fmt.Fprintf(&b, "  %s\n", warning)
		}
	}
	return b.String()
}
func instructions(plan *Plan) string {
	if plan.Target == "launchbox" {
		return launchBoxInstructions()
	}
	return fmt.Sprintf(`RomMeta ES-DE staging package (local/personal)

Open preview.html to inspect metadata and selected assets offline.
The preview is a report, not an ES-DE consumer compatibility test.

ROMs were not copied, renamed or changed. Your original system directory is:
%s

This is the %s system's optional portable package. Install it with:
rom-meta export --target es-de --package <this package> --destination <ES-DE-data-directory>
Close ES-DE and repeat with --apply after reviewing the native plan.
Existing nonempty metadata, favorites, history and emulator choices are preserved;
replaced files are backed up automatically. Source ROM paths must remain usable.
Package layout: gamelists/%s/gamelist.xml and downloaded_media/%s/.
Native layout: <ES-DE-data>/gamelists/%s/gamelist.xml and <configured-media>/%s/.

manifest.json contains private input locations, binding evidence, effective profile,
selected source/language and asset paths/digests. report.txt lists coverage gaps.
Ordinary unknown/ambiguous inputs have filename-only entries. Unbound/ambiguous
arcade archives and unsupported inputs are reported and omitted from gamelist.xml.
There are no online fallback requests.

See https://gitlab.com/es-de/emulationstation-de/-/blob/master/INSTALL.md#gamelistxml
and https://gitlab.com/es-de/emulationstation-de/-/blob/master/USERGUIDE.md
`, plan.ROMRoot, plan.System, plan.System, plan.System, plan.System, plan.System)
}

const previewTemplate = `<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>RomMeta Р’В· {{.Platform}} export</title><style>body{margin:0;background:#111827;color:#e5e7eb;font:16px/1.6 system-ui,sans-serif}main{max-width:1180px;margin:auto;padding:32px}h1{margin-bottom:0}header p{color:#9ca3af}.stats{display:flex;gap:20px;flex-wrap:wrap;margin:24px 0}.stats span{background:#1f2937;border-radius:10px;padding:12px 18px}article{background:#1f2937;border:1px solid #374151;border-radius:12px;margin:20px 0;padding:24px;display:grid;grid-template-columns:220px 1fr;gap:24px}article img{max-width:100%;max-height:280px;object-fit:contain}.badge{font-size:12px;text-transform:uppercase;color:#6ee7b7}h2{margin:4px 0}.release,.provenance{color:#9ca3af;font-size:13px}.description{white-space:pre-line}.assets{display:flex;gap:16px;flex-wrap:wrap}a{color:#93c5fd}.assets img{max-width:260px;max-height:160px}code{overflow-wrap:anywhere}.warning{color:#fbbf24}@media(max-width:650px){article{grid-template-columns:1fr}}</style><main><header><h1>{{.Platform}} Р’В· offline export</h1><p>Local metadata and media. ROM filenames preserved. {{.Target}} staging package.</p></header>{{with .LaunchBox}}<section><h2>{{len .Groups}} game cards Р’В· {{.VersionCount}} versions</h2>{{range $group := .Groups}}<h3>{{.Title}}</h3><ul>{{range .Versions}}<li>{{if eq .ID $group.Primary}}Primary Р’В· {{end}}{{.Name}}<br><code>{{.Path}}</code></li>{{end}}</ul>{{end}}{{range .Warnings}}<p class="warning">{{.}}</p>{{end}}</section>{{end}}<div class="stats"><span>{{len .Records}} ROMs</span><span>{{.Matched}} matched</span><span>{{.Ambiguous}} ambiguous</span><span>{{.Unmatched}} unmatched</span><span>{{.MediaFiles}} media files</span></div>{{range .Records}}<article><div>{{with asset . "cover"}}<a href="{{.}}"><img src="{{.}}" alt="Selected cover"></a>{{end}}{{with asset . "logo"}}<img src="{{.}}" alt="Game logo">{{end}}</div><div><span class="badge">{{.Status}}</span><h2>{{value . "title"}}</h2><p class="release">{{.Release}}<br><code>{{.ROM}}</code></p>{{with value . "description"}}<p class="description">{{.}}</p>{{end}}{{with index .Fields "description"}}<p class="provenance">Description: {{.Source}} Р’В· {{.Language}}</p>{{end}}<p>{{value . "developer"}} Р’В· {{value . "publisher"}}<br>{{value . "genres"}} Р’В· {{value . "release_date"}}</p><div class="assets">{{with asset . "screenshot"}}<a href="{{.}}"><img src="{{.}}" alt="Gameplay screenshot"></a>{{end}}{{with asset . "video"}}<a href="{{.}}">Selected video</a>{{end}}</div>{{range .Warnings}}<p class="warning">{{.}}</p>{{end}}{{if ne .Status "matched"}}<p class="warning">{{.Reason}}</p>{{end}}</div></article>{{end}}<p><a href="report.txt">Coverage report</a> Р’В· <a href="manifest.json">Selection manifest</a> Р’В· <a href="{{root (xmlPath .)}}">Frontend XML</a> Р’В· <a href="README.txt">Installation notes</a></p></main></html>`
