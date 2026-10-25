package dat

import (
	"fmt"
	"sort"
)

// partitionKnownReviewedBundles replays saved release ownership, not a guessed
// title/edition rule. A cross-key DAT family is partitioned only if every release
// has one exact persisted authority-name destination, with no conflicting ID.
// Any unknown/ambiguous release leaves the original conflict intact for review.
func partitionKnownReviewedBundles(plan Plan, bundles []gameBundle, index existingBundleIndex) ([]gameBundle, error) {
	if plan.ArcadeSnapshot != "" {
		return bundles, nil
	}
	used := map[string]bool{}
	for _, bundle := range bundles {
		used[bundle.Folder] = true
	}
	var out []gameBundle
	next := 0
	for _, bundle := range bundles {
		incoming, err := identifyNewBundle(plan, bundle)
		if err != nil {
			return nil, err
		}
		if reviewedAnchorConflict(incoming, index) == "" {
			out = append(out, bundle)
			continue
		}
		parts := map[string][]datGame{}
		valid := true
		for _, release := range bundle.Releases {
			identity, err := identifyNewBundle(plan, gameBundle{Folder: bundle.Folder, Releases: []datGame{release}})
			if err != nil {
				return nil, err
			}
			names := indexedCandidates(identity.entryNames, index.byEntryName)
			if len(names) != 1 || names[0].contentKey == "" {
				valid = false
				break
			}
			owner := names[0]
			for _, byID := range indexedCandidates(identity.entryIDs, index.byEntryID) {
				if byID.folder != owner.folder {
					valid = false
				}
			}
			if !valid {
				break
			}
			parts[owner.folder] = append(parts[owner.folder], release)
		}
		if !valid || len(parts) < 2 {
			out = append(out, bundle)
			continue
		}
		var destinations []string
		for folder := range parts {
			destinations = append(destinations, folder)
		}
		sort.Strings(destinations)
		for _, folder := range destinations {
			var name string
			for {
				next++
				name = fmt.Sprintf("%s [reviewed %d]", bundle.Folder, next)
				if !used[name] {
					break
				}
			}
			used[name] = true
			out = append(out, gameBundle{Title: bundle.Title, Folder: name, Releases: parts[folder]})
		}
	}
	return out, nil
}
