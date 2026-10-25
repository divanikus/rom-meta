package hasheous

import (
	"sort"
	"strings"
)

// Component hashes are usable only in the MAME-specific path, together with
// returned set identity. They are never treated as whole-archive fingerprints.
func mameLookupRequests(game catalogGame) []lookupRequest {
	releases := append(game.MAMEReleases[:0:0], game.MAMEReleases...)
	// listxml lists inherited BIOS alternatives before game data (notably Neo
	// Geo). Prefer larger payloads so the bounded query is not BIOS-only. Exact
	// set verification below remains mandatory regardless of this heuristic.
	fingerprints := append(game.Fingerprints[:0:0], game.Fingerprints...)
	sort.SliceStable(fingerprints, func(i, j int) bool { return fingerprints[i].Size > fingerprints[j].Size })
	sort.SliceStable(releases, func(i, j int) bool {
		return releases[i].Name == game.DefaultRelease && releases[j].Name != game.DefaultRelease
	})
	var requests []lookupRequest
	for _, release := range releases {
		var inputs []lookupInput
		seen := map[string]bool{}
		for _, fingerprint := range fingerprints {
			if fingerprint.Release != release.Name || fingerprint.PayloadKind != "rom-component" {
				continue
			}
			sha1 := cleanHash(fingerprint.SHA1, 40)
			if sha1 == "" || seen[sha1] {
				continue
			}
			seen[sha1] = true
			inputs = append(inputs, lookupInput{MAMESet: release.MAMESet, Release: release.Name, Name: fingerprint.Name, SHA1: sha1, Label: "mame:" + release.MAMESet + "/" + fingerprint.Name + " sha1:" + sha1})
			if len(inputs) == 5 {
				break
			}
		}
		if len(inputs) == 0 {
			continue
		}
		requests = append(requests, lookupRequest{Release: release.Name, Inputs: inputs, Label: requestLabel(release.Name, inputs)})
		if len(requests) == maxReleaseLookups {
			break
		}
	}
	return requests
}

func verifiedMAMEResponse(response lookupResponse, inputs []lookupInput) (lookupResponse, bool) {
	var verified []signatureResponse
	for source, signatures := range response.Signatures {
		if !strings.EqualFold(source, "MAMEArcade") {
			continue
		}
		for _, signature := range signatures {
			if !strings.EqualFold(signature.Game.System, "Arcade") {
				continue
			}
			for _, input := range inputs {
				if input.MAMESet != "" && strings.EqualFold(strings.TrimSpace(signature.Game.Description), input.MAMESet) && input.SHA1 != "" && strings.EqualFold(signature.ROM.SHA1, input.SHA1) {
					verified = append(verified, signature)
					break
				}
			}
		}
	}
	response.Signatures = map[string][]signatureResponse{"MAMEArcade": verified}
	return response, len(verified) > 0 && anyString(response.ID) != ""
}

func unambiguousMAMEResponses(responses []lookupResponse, inputs []lookupInput) []lookupResponse {
	var verified []lookupResponse
	id := ""
	for _, response := range responses {
		match, ok := verifiedMAMEResponse(response, inputs)
		if !ok {
			continue
		}
		if id != "" && id != anyString(match.ID) {
			return nil
		}
		id = anyString(match.ID)
		verified = append(verified, match)
	}
	return verified
}
