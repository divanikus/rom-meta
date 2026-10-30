package exporter

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"

	"rommeta/internal/catalogfs"
)

type BatchOptions struct {
	Options
	All     bool
	Systems []string
}
type SkippedSystem struct {
	System string `json:"system"`
	Reason string `json:"reason"`
}
type BatchPlan struct {
	SchemaVersion int             `json:"schema_version"`
	Target        string          `json:"target"`
	DataRoot      string          `json:"data_root"`
	ROMRoot       string          `json:"rom_root"`
	Out           string          `json:"-"`
	Plans         []*Plan         `json:"systems"`
	Skipped       []SkippedSystem `json:"skipped"`
}

var errNoInputs = errors.New("no supported ROM inputs selected")

func BuildBatch(options BatchOptions) (*BatchPlan, error) {
	target, err := NormalizeTarget(options.Target)
	if err != nil {
		return nil, err
	}
	options.Target = target
	if options.Platform != "" || options.All == (len(options.Systems) > 0) {
		return nil, fmt.Errorf("choose --platform, --platforms or --all; batch needs --all or --platforms")
	}
	if options.ROMRoot == "" || options.Out == "" {
		return nil, fmt.Errorf("--rom-root and --out are required")
	}
	if options.Limit < 0 {
		return nil, fmt.Errorf("--limit must be non-negative")
	}
	root, err := realPath(options.DataRoot)
	if err != nil {
		return nil, err
	}
	romRoot, err := realPath(options.ROMRoot)
	if err != nil {
		return nil, err
	}
	out, err := prospectivePath(options.Out)
	if err != nil {
		return nil, err
	}
	if within(root, out) || within(out, root) || within(romRoot, out) || within(out, romRoot) {
		return nil, fmt.Errorf("output must be separate from the catalog and ROM trees")
	}
	// Validate the shared policy even if every folder is skipped.
	if _, err := loadProfile(root, options.ProfilePath); err != nil {
		return nil, err
	}
	selector, err := LoadPlatformSelector(root)
	if err != nil {
		return nil, err
	}
	selected, skipped, err := selector.Folders(romRoot, options.Systems, options.All)
	if err != nil {
		return nil, err
	}
	batch := &BatchPlan{SchemaVersion: 1, Target: target, DataRoot: root, ROMRoot: romRoot, Out: out, Plans: []*Plan{}, Skipped: skipped}
	for _, selected := range selected {
		selection := selected.Selection
		system := selection.System
		skip := func(reason string) {
			batch.Skipped = append(batch.Skipped, SkippedSystem{System: filepath.Base(selection.Folder), Reason: reason})
		}
		child := options.Options
		child.DataRoot, child.ROMRoot, child.Out, child.Platform, child.System = root, selection.Folder, out, selected.Catalog, system
		child.selector, child.selection, child.selectedCatalog = selector, selection, selected.Catalog
		if options.Progress != nil {
			child.Progress = func(stage, name string, current, total int) {
				options.Progress(system+" / "+stage, name, current, total)
			}
		}
		plan, err := Build(child)
		if errors.Is(err, errNoInputs) || errors.Is(err, errArcadeSnapshot) {
			skip(err.Error())
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", system, err)
		}
		batch.Plans = append(batch.Plans, plan)
	}
	return batch, nil
}

func ApplyBatch(batch *BatchPlan, progress func(string, string, int, int)) error {
	if len(batch.Plans) == 0 {
		return fmt.Errorf("no systems with selected inputs; no package created")
	}
	seen := map[string]bool{}
	for _, plan := range batch.Plans {
		path := strings.ToLower(outputXMLPath(plan))
		if seen[path] {
			return fmt.Errorf("duplicate target platform output: %s", path)
		}
		seen[path] = true
	}
	staging, err := beginPackage(batch.DataRoot, batch.ROMRoot, batch.Out)
	if err != nil {
		return err
	}
	defer catalogfs.RemoveAll(staging)
	var report strings.Builder
	fmt.Fprintf(&report, "RomMeta offline %s batch export\n\n", batch.Target)
	for _, plan := range batch.Plans {
		childProgress := progress
		if progress != nil {
			system := plan.System
			childProgress = func(stage, name string, current, total int) { progress(system+" / "+stage, name, current, total) }
		}
		if err := writePlan(plan, staging, filepath.Join("reports", plan.System), childProgress); err != nil {
			return err
		}
		report.WriteString(reportSummary(plan))
	}
	for _, skipped := range batch.Skipped {
		fmt.Fprintf(&report, "Skipped %s: %s\n", skipped.System, skipped.Reason)
	}
	manifest, err := json.MarshalIndent(batch, "", "  ")
	if err != nil {
		return err
	}
	if err := writeOutput(filepath.Join(staging, "manifest.json"), append(manifest, '\n')); err != nil {
		return err
	}
	if err := writeOutput(filepath.Join(staging, "report.txt"), []byte(report.String())); err != nil {
		return err
	}
	readme := fmt.Sprintf(`RomMeta local/personal ES-DE batch package

ROM root: %s
ROMs and source catalog were not changed.
Open preview.html and per-system reports before installation.
Install: rom-meta export --target es-de --package <this package> --destination <ES-DE-data-directory>
Close ES-DE and add --apply after reviewing the native plan. Existing native
curation is preserved; replaced files are backed up automatically.
Package layout: gamelists/<system>/gamelist.xml and downloaded_media/<system>/.
Skipped systems have no generated files. Keep source ROM paths available.
Consumer/emulator compatibility and stronger disc/arcade proof are not implied.
`, batch.ROMRoot)

	if batch.Target == "launchbox" {
		readme = launchBoxInstructions()
	}
	if err := writeOutput(filepath.Join(staging, "README.txt"), []byte(readme)); err != nil {
		return err
	}
	t, err := template.New("batch").Funcs(template.FuncMap{"xmlPath": outputXMLPath}).Parse(batchPreviewTemplate)
	if err != nil {
		return err
	}
	file, err := catalogfs.OpenFile(filepath.Join(staging, "preview.html"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	renderErr := t.Execute(file, batch)
	closeErr := file.Close()
	if renderErr != nil {
		return renderErr
	}
	if closeErr != nil {
		return closeErr
	}
	return publishPackage(staging, batch.Out)
}
func reportSummary(plan *Plan) string {
	return fmt.Sprintf("%s: %d ROMs; %d matched (%d hash / %d named release or set / %d game-only), %d ambiguous, %d unmatched, %d unsupported; %d media\n", plan.System, len(plan.Records), plan.Matched, plan.HashMatches, plan.NameMatches, plan.GameOnly, plan.Ambiguous, plan.Unmatched, plan.Unsupported, plan.MediaFiles)
}

const batchPreviewTemplate = `<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>RomMeta Р’В· {{.Target}} export</title><style>body{max-width:1000px;margin:40px auto;padding:20px;background:#111827;color:#e5e7eb;font:16px/1.7 system-ui}a{color:#93c5fd}article{padding:20px;margin:16px 0;background:#1f2937;border-radius:10px}.warning{color:#fbbf24}</style><h1>{{.Target}} Р’В· offline export</h1><p>{{len .Plans}} systems Р’В· {{len .Skipped}} skipped</p>{{range .Plans}}<article><h2><a href="reports/{{.System}}/preview.html">{{.System}} Р’В· {{.Platform}}</a></h2><p>{{len .Records}} ROMs Р’В· {{.Matched}} matched Р’В· {{.Ambiguous}} ambiguous Р’В· {{.Unmatched}} unmatched Р’В· {{.Unsupported}} unsupported Р’В· {{.MediaFiles}} media</p><a href="{{xmlPath .}}">Frontend XML</a> Р’В· <a href="reports/{{.System}}/report.txt">Report</a></article>{{end}}{{range .Skipped}}<p class="warning">Skipped {{.System}}: {{.Reason}}</p>{{end}}<p><a href="manifest.json">Manifest</a> Р’В· <a href="report.txt">Summary</a> Р’В· <a href="README.txt">Installation</a></p></html>`
