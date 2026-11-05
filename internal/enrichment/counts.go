package enrichment

import (
	"rommeta/internal/emumovies"
	"rommeta/internal/hasheous"
	"rommeta/internal/igdb"
	"rommeta/internal/launchbox"
	"rommeta/internal/openvgdb"
	"rommeta/internal/screenscraper"
	"rommeta/internal/sourceassets"
)

// Counts deliberately excludes provider URLs, credentials, paths and raw errors.
type Counts struct {
	Games      int `json:"games"`
	Matched    int `json:"matched"`
	Written    int `json:"written"`
	Downloaded int `json:"downloaded"`
	Existing   int `json:"existing"`
	Failed     int `json:"failed"`
	NotFound   int `json:"not_found"`
	Skipped    int `json:"skipped"`
}

func Summarize(result any) Counts {
	switch v := result.(type) {
	case launchbox.ImportResult:
		return Counts{Games: v.GameBundleCount, Matched: v.GameFileCount, Written: v.WrittenFileCount, NotFound: v.NotFoundGameCount, Existing: v.SkippedExistingCount}
	case openvgdb.ImportResult:
		return Counts{Games: v.GameBundleCount, Matched: v.GameFileCount, Written: v.WrittenFileCount, NotFound: v.NotFoundGameCount, Existing: v.SkippedExistingCount}
	case igdb.ImportResult:
		return Counts{Games: v.GameBundleCount, Matched: v.MatchedGameCount, Written: v.WrittenFileCount, NotFound: v.NotFoundGameCount, Existing: v.SkippedExistingCount, Failed: v.FailedSearchCount}
	case screenscraper.ImportResult:
		return Counts{Games: v.GameBundleCount, Matched: v.MatchedGameCount, Written: v.WrittenFileCount, NotFound: v.NotFoundGameCount, Existing: v.SkippedExistingCount, Failed: v.FailedSearchCount}
	case hasheous.ImportResult:
		return Counts{Games: v.GameBundleCount, Matched: v.MatchedGameCount, Written: v.WrittenFileCount, NotFound: v.NotFoundGameCount, Existing: v.SkippedExistingCount, Failed: v.FailedLookupCount, Skipped: v.SkippedWithoutEvidenceCount}
	case sourceassets.Result:
		return Counts{Games: v.GameBundleCount, Downloaded: v.DownloadedFileCount, Existing: v.ExistingFileCount, Failed: v.FailedDownloadCount}
	case emumovies.VideoResult:
		return Counts{Games: v.GameBundleCount, Downloaded: v.DownloadedFileCount, Existing: v.ExistingFileCount, Failed: v.FailedDownloadCount}
	case emumovies.ArtworkResult:
		return Counts{Games: v.GameBundleCount, Written: v.WrittenArtworkFileCount, Downloaded: v.DownloadedPackageCount, Existing: v.ExistingArtworkFileCount, Failed: v.FailedDownloadCount}
	}
	return Counts{}
}
