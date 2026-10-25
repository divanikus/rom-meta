package romm

import (
	"fmt"
	"io"
	"rommeta/internal/exporter"
	"sort"
)

type CatalogAsset = exporter.CatalogAsset

func InventoryAvailableMedia(p *Plan) error {
	assets, err := exporter.InventoryMedia(p.DataRoot, func() []exporter.Record {
		records := make([]exporter.Record, len(p.Entries))
		for i := range p.Entries {
			records[i] = p.Entries[i].Source
		}
		return records
	}())
	p.AvailableMedia = assets
	return err
}
func PrintAPICapabilities(w io.Writer, p *Plan) {
	counts := map[string]int{}
	bytesByKind := map[string]int64{}
	var total int64
	for _, asset := range p.AvailableMedia {
		counts[asset.Kind]++
		bytesByKind[asset.Kind] += asset.Size
		total += asset.Size
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Fprintf(w, "Local catalog availability: %d distinct files / %d bytes (%.2f MiB); availability is not an import action.\n", len(p.AvailableMedia), total, float64(total)/(1<<20))
	for _, key := range keys {
		support := "no shared Resources multipart upload in this profile"
		if key == "box-front" {
			support = "stock artwork cover; fill only empty cover"
		}
		if key == "manual" {
			support = "stock manual upload; fill only empty manual"
		}
		fmt.Fprintf(w, "  %s: %d files / %d bytes | %s\n", key, counts[key], bytesByKind[key], support)
	}
	texts := map[string]int{}
	selected := map[string]int{}
	slugs, emptyLinks, summaryMatches := 0, 0, 0
	for _, entry := range p.Entries {
		if v, ok := entry.Source.Fields["description"]; ok && v.Value != "" {
			selected[v.Source+"/"+v.Language]++
			if v.Value == rawString(entry.Before, "summary") {
				summaryMatches++
			}
		}
		if entry.Providers == nil {
			continue
		}
		for _, snapshot := range entry.Providers.SourceTexts {
			texts[snapshot.Source] += len(snapshot.Descriptions)
		}
		if slot, ok := entry.Providers.Slots["igdb"]; ok {
			if slot.SourceSlug != "" {
				slugs++
			}
			if rawString(entry.Before, "slug") == "" {
				emptyLinks++
			}
		}
	}
	keys = keys[:0]
	for key := range texts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	fmt.Fprintln(w, "Provider/language descriptions retained as source provenance (native API has one summary):")
	for _, key := range keys {
		fmt.Fprintf(w, "  %s: %d source/language snapshots across selected ROMs\n", key, texts[key])
	}
	keys = keys[:0]
	for key := range selected {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(w, "  selected summary candidate: %s for %d ROMs\n", key, selected[key])
	}
	fmt.Fprintf(w, "Native summary matches the selected candidate for %d/%d ROMs; nonempty differing summaries are preserved.\n", summaryMatches, len(p.Entries))
	fmt.Fprintf(w, "IGDB links: %d source slugs retained, %d native slugs empty. Stock 5.4 edit API cannot set slug; IDs remain correct.\n", slugs, emptyLinks)
	fmt.Fprintln(w, "API-only boundary: no local resource path/SSH requirement, no fake user screenshots, no external asset fetch. Extra artwork/video/shared gallery are not imported by this profile. Native provider fetching and other upload routes use different input/storage semantics; alternate descriptions remain provenance.")
}
