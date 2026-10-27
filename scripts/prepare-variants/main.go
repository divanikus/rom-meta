// Command prepare-variants stages explicitly linked derivatives outside the core CLI.
// It reads catalogs/ROMs, validates supplied relationships, and writes only a new
// sparse catalog. It never edits the source catalog or discovers parents.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
)

type options struct {
	Catalog, ROMRoot, Sources, Links, Out, Report, Media string
	Apply                                                bool
	Progress                                             io.Writer
}

func main() {
	var o options
	var c collectOptions
	flag.StringVar(&o.Catalog, "catalog", "", "Read-only source catalog root")
	flag.StringVar(&o.ROMRoot, "rom-root", "", "Read-only root for relative input paths")
	flag.StringVar(&o.Sources, "sources", "", "Read-only root for pinned source snapshots")
	flag.StringVar(&o.Links, "links", "", "Prepared relationship JSON (schema_version 1)")
	flag.StringVar(&o.Out, "out", "", "Separate sparse staging catalog; never the source")
	flag.StringVar(&o.Report, "report", "", "Optional new JSON report outside input trees")
	flag.StringVar(&o.Media, "media", "artwork", "Copy artwork, all, or none")
	flag.BoolVar(&o.Apply, "apply", false, "Materialize staging after checking all entries")
	flag.StringVar(&c.Out, "collect-links", "", "New prepared-link JSON; collect instead of staging")
	flag.StringVar(&c.GA, "ga", "", "Gaming Alexandria Famicom DAT, relative to --sources")
	flag.StringVar(&c.RHA, "rha", "", "Romhack Archive root with database/, relative to --sources")
	flag.StringVar(&c.HackHash, "hackhash", "", "HackHash detailed JSON, relative to --sources")
	flag.StringVar(&c.Platforms, "platforms", "", "Comma-separated exact platform names/aliases for collection")
	flag.StringVar(&c.Subdir, "subdir", "translations", "ROM subdirectory within each selected platform")
	flag.Parse()
	o.Progress = os.Stderr
	if c.Out != "" {
		if err := collectCLI(o, c); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if c.GA != "" || c.RHA != "" || c.HackHash != "" || c.Platforms != "" {
		fmt.Fprintln(os.Stderr, "source adapters require --collect-links")
		os.Exit(1)
	}
	r, err := run(o)
	if r != nil {
		if o.Report != "" {
			if e := writeReport(o, r); e != nil {
				fmt.Fprintln(os.Stderr, e)
				os.Exit(1)
			}
		}
		fmt.Printf("%s: %d entries, %d bundles, %d held, %d errors; %d files / %d bytes\n",
			r.Status, r.Entries, r.Bundles, len(r.Holds), len(r.Errors), len(r.Files), r.Bytes)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func writeReport(o options, r *report) error {
	name, err := reportPath(o)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	err = enc.Encode(r)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
