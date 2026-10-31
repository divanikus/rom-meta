package exportclient

import (
	"context"
	"fmt"
	"reflect"
	"rommeta/internal/catalogapi"
	"rommeta/internal/exporter"
)

type discStamp struct {
	Root, System, ROM string
	Disc              *exporter.DiscObservation
}

func discInputSize(input exporter.ExportInput) int64 {
	var size int64
	if input.Disc != nil {
		for _, file := range input.Disc.Files {
			size += file.Size
		}
	}
	return size
}

func (c *client) exportBatch(ctx context.Context, request catalogapi.ExportRequest, options exporter.Options) (*catalogapi.ExportResponse, []exporter.ExportInput, error) {
	var firstProfile *exporter.Profile
	for pass := 0; pass < 2; pass++ {
		var response catalogapi.ExportResponse
		if err := c.json(ctx, "POST", "/v1/export", request, &response); err != nil {
			return nil, nil, err
		}
		if response.SchemaVersion != 1 || response.Revision == "" || response.Plan == nil || response.Plan.Platform != request.Platform || response.Plan.System != request.System || response.Plan.Target != "es-de" || len(response.Plan.Records) != len(request.Inputs) {
			return nil, nil, fmt.Errorf("API export response does not match selected inputs")
		}
		if c.revision != "" && c.revision != response.Revision {
			return nil, nil, fmt.Errorf("catalog changed between export requests")
		}
		c.revision = response.Revision
		if firstProfile != nil && !reflect.DeepEqual(*firstProfile, response.Plan.Profile) {
			return nil, nil, fmt.Errorf("export policy changed during CHD hash fallback")
		}
		profile := response.Plan.Profile
		firstProfile = &profile
		for i := range response.Plan.Records {
			if err := validateRecord(request.Inputs[i], &response.Plan.Records[i], request.System, response.Assets); err != nil {
				return nil, nil, err
			}
		}
		inputs, changed, err := exporter.HashRequiredInputs(options, request.Inputs, response.Plan.Records)
		if err != nil {
			return nil, nil, err
		}
		if !changed {
			return &response, request.Inputs, nil
		}
		request.Inputs, request.Revision = inputs, response.Revision
	}
	return nil, nil, fmt.Errorf("API requested hashes after full-file observations")
}
