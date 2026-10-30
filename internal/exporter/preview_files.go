package exporter

import (
	"encoding/json"
	"fmt"
)

// PreviewFile is generated metadata or an asset proof, without copying asset bytes.
type PreviewFile struct {
	Data   []byte
	SHA256 string
	Size   int64
}

func PreviewFiles(plans []*Plan, single bool) (map[string]PreviewFile, error) {
	if len(plans) == 0 {
		return nil, fmt.Errorf("no systems with selected inputs")
	}
	files := map[string]PreviewFile{}
	var manifest []byte
	var err error
	if single {
		manifest, err = json.Marshal(plans[0])
	} else {
		manifest, err = json.Marshal(BatchPlan{SchemaVersion: 1, Target: plans[0].Target, Plans: plans})
	}
	if err != nil {
		return nil, err
	}
	files["manifest.json"] = PreviewFile{Data: manifest}
	for _, plan := range plans {
		var body []byte
		if plan.Target == "launchbox" {
			body, err = RenderLaunchBox(plan)
		} else {
			body, err = RenderGamelist(plan)
		}
		if err != nil {
			return nil, err
		}
		name := outputXMLPath(plan)
		if _, ok := files[name]; ok {
			return nil, fmt.Errorf("duplicate target platform output: %s", name)
		}
		files[name] = PreviewFile{Data: body}
		playlists, err := launchBoxPlaylistData(plan)
		if err != nil {
			return nil, err
		}
		for name, body := range playlists {
			files[name] = PreviewFile{Data: body}
		}
		for _, record := range plan.Records {
			for _, media := range record.Media {
				files[media.OutputPath] = PreviewFile{SHA256: media.SHA256, Size: media.Size}
			}
		}
	}
	return files, nil
}
