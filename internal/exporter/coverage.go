package exporter

type DisplayCoverage struct{ Matched, Descriptions, Covers, GameDescriptions, UnknownVariants int }

func Coverage(plan *Plan) DisplayCoverage {
	var result DisplayCoverage
	for _, r := range plan.Records {
		if r.Status != "matched" || r.Hidden {
			continue
		}
		result.Matched++
		if v := r.Fields["description"]; v.Value != "" {
			result.Descriptions++
			if v.Selection == "game" {
				result.GameDescriptions++
			}
		}
		if _, ok := r.Media["cover"]; ok {
			result.Covers++
		}
		for _, m := range r.Media {
			if m.Variant == "unknown" {
				result.UnknownVariants++
			}
		}
	}
	return result
}
