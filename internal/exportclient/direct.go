package exportclient

import (
	"context"
	"fmt"
	"io"
	"rommeta/internal/exporter"
)

// CheckInputs validates the current local ROMs before direct native writing.
func (j *Job) CheckInputs(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := j.fresh(); err != nil {
		return err
	}
	for _, plan := range j.Batch.Plans {
		plan.Context = ctx
		if err := exporter.CheckInputs(plan); err != nil {
			return err
		}
	}
	return nil
}

// OpenAsset streams one selected asset; the native writer verifies its declared
// size and SHA256 while writing. No download cache or export package is created.
func (j *Job) OpenAsset(ctx context.Context, source string) (io.ReadCloser, error) {
	asset, ok := j.assets[source]
	if !ok {
		return nil, fmt.Errorf("asset is not part of this export")
	}
	response, err := j.client.request(ctx, "GET", asset.URI, nil)
	if err != nil {
		return nil, err
	}
	return response.Body, nil
}
