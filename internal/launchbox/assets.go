package launchbox

import (
	"rommeta/internal/sourceassets"
	"rommeta/internal/taskprogress"
)

// AssetOptions controls source-media downloads from LaunchBox candidate URLs.
type AssetOptions struct {
	DataRoot    string
	Apply       bool
	Limit       int
	Concurrency int
	PlanPath    string
	PlanFormat  string
	Progress    taskprogress.Func
}

// AssetResult describes a dry-run or applied LaunchBox asset download.
type AssetResult struct {
	DataRoot             string
	PlatformCount        int
	GameBundleCount      int
	CandidateCount       int
	PlannedDownloadCount int
	ExistingFileCount    int
	DuplicateURLCount    int
	DownloadedFileCount  int
	SkippedByLimitCount  int
	FailedDownloadCount  int
	Failures             []AssetFailure
	PlanPath             string
	PlanFormat           string
	PlanItemCount        int
	ManifestPath         string
	ManifestItemCount    int
	Apply                bool
	Limit                int
}

// AssetFailure is a failed media download that did not stop the whole run.
type AssetFailure struct {
	URL    string
	Target string
	Error  string
}

// FetchAssets downloads source-specific media from LaunchBox media stanzas.
func FetchAssets(options AssetOptions) (AssetResult, error) {
	result, err := sourceassets.Fetch(sourceassets.Options{
		DataRoot:    options.DataRoot,
		Source:      "launchbox",
		Apply:       options.Apply,
		Limit:       options.Limit,
		Concurrency: options.Concurrency,
		PlanPath:    options.PlanPath,
		PlanFormat:  options.PlanFormat,
		PolicyScope: "review",
		PolicyNote:  "Imported from LaunchBox Games Database media URLs; verify rights before sharing.",
		Progress:    options.Progress,
	})
	return assetResult(result), err
}

func assetResult(result sourceassets.Result) AssetResult {
	failures := make([]AssetFailure, 0, len(result.Failures))
	for _, failure := range result.Failures {
		failures = append(failures, AssetFailure(failure))
	}
	return AssetResult{
		DataRoot:             result.DataRoot,
		PlatformCount:        result.PlatformCount,
		GameBundleCount:      result.GameBundleCount,
		CandidateCount:       result.CandidateCount,
		PlannedDownloadCount: result.PlannedDownloadCount,
		ExistingFileCount:    result.ExistingFileCount,
		DuplicateURLCount:    result.DuplicateURLCount,
		DownloadedFileCount:  result.DownloadedFileCount,
		SkippedByLimitCount:  result.SkippedByLimitCount,
		FailedDownloadCount:  result.FailedDownloadCount,
		Failures:             failures,
		PlanPath:             result.PlanPath,
		PlanFormat:           result.PlanFormat,
		PlanItemCount:        result.PlanItemCount,
		ManifestPath:         result.ManifestPath,
		ManifestItemCount:    result.ManifestItemCount,
		Apply:                result.Apply,
		Limit:                result.Limit,
	}
}
