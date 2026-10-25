package sourceassets

// LocalMediaCategory is the offline saved-provider category bound by the same
// target/legacy naming projection as downloading and NameMedia. It exposes no URL,
// credentials, network operation or acquisition behavior to offline consumers.
type LocalMediaCategory struct{ Path, SourceType string }

func LocalMediaCategories(gamePath, source string) ([]LocalMediaCategory, error) {
	candidates, err := readGameAssetCandidates(gamePath, source)
	if err != nil {
		return nil, err
	}
	result := []LocalMediaCategory{}
	for _, candidate := range candidates {
		if candidate.MediaType != "box_front" {
			continue
		}
		result = append(result, LocalMediaCategory{candidate.Target, candidate.SourceType})
		if candidate.LegacyTarget != candidate.Target {
			result = append(result, LocalMediaCategory{candidate.LegacyTarget, candidate.SourceType})
		}
	}
	return result, nil
}
