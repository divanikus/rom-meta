package exporter

import (
	"fmt"
	"path/filepath"
)

// CatalogPreviewXML uses native renderers with explicit placeholder inputs.
// It does not match a ROM, discover a launch file or merge native frontend IDs.
// Image destinations are separate because LaunchBox discovers images by folder.
type CatalogPreviewXML struct {
	XML        string            `json:"xml"`
	MediaPaths map[string]string `json:"media_paths"`
}

func RenderCatalogPreviewXML(preview CatalogPreview, profile Profile, target, platform, folder string) (CatalogPreviewXML, error) {
	if preview.record.Fields == nil {
		return CatalogPreviewXML{}, fmt.Errorf("catalog selection required")
	}
	record := preview.record
	record.Fields = map[string]Value{}
	for field, value := range preview.Fields {
		record.Fields[field] = value
	}
	record.Status = "matched" // Metadata selection only; never returned as a ROM match.
	record.ROM = "ROM_FILE"
	record.Game = filepath.ToSlash(filepath.Join("platforms", platform, folder))
	record.Media = map[string]Media{}
	for role, media := range preview.Media {
		record.Media[role] = media
	}
	plan := &Plan{System: "SYSTEM", PlatformName: platform, ROMRoot: "/ROM_ROOT", Profile: profile, Records: []Record{record}}
	if err := ProjectTarget(plan, target); err != nil {
		return CatalogPreviewXML{}, err
	}
	var content []byte
	var err error
	if plan.Target == "launchbox" {
		content, err = RenderLaunchBox(plan)
	} else {
		content, err = RenderGamelist(plan)
	}
	if err != nil {
		return CatalogPreviewXML{}, err
	}
	paths := map[string]string{}
	for role, media := range plan.Records[0].Media {
		paths[role] = media.OutputPath
	}
	return CatalogPreviewXML{XML: string(content), MediaPaths: paths}, nil
}
