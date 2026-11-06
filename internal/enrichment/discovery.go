package enrichment

import "rommeta/internal/platformnames"

func missingMediaPlatform(platform string, skipped map[string]string, err error) error {
	if err != nil {
		return err
	}
	if platform != "" && skipped[platform] != "" {
		return &platformnames.ResolutionError{Source: "emumovies", Platform: platform}
	}
	return nil
}
