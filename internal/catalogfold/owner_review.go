package catalogfold

import (
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogedit"
	"rommeta/internal/catalogyaml"
)

// ownerMergeReview exists only in an editable browser plan. It records a user's
// catalog organization decision; it does not turn provider hints into evidence.
type ownerMergeReview struct {
	Reason       string            `json:"reason" yaml:"reason"`
	Members      []ReviewedMember  `json:"members" yaml:"members"`
	PreviousKeys map[string]string `json:"previous_keys" yaml:"previous_keys"`
	Releases     []string          `json:"releases" yaml:"releases"`
	Key          string            `json:"-" yaml:"-"`
}

func newOwnerMergeReview(games []catalogGame, members []ReviewedMember, reason string) *ownerMergeReview {
	r := &ownerMergeReview{Reason: reason, PreviousKeys: map[string]string{}}
	for _, game := range games {
		r.PreviousKeys[game.Folder] = game.ContentKey
		for _, m := range members {
			if m.Folder == game.Folder {
				r.Members = append(r.Members, m)
			}
		}
		for _, release := range game.Releases {
			r.Releases = appendUniqueString(r.Releases, release.Key)
		}
	}
	sort.Slice(r.Members, func(i, j int) bool { return r.Members[i].Folder < r.Members[j].Folder })
	sort.Strings(r.Releases)
	data, _ := yaml.Marshal(r)
	r.Key = "reviewed." + catalogedit.Hash(data)
	return r
}
func groupReviewedGames(games []catalogGame, g Group) ([]catalogGame, string) {
	r := g.ownerReview
	if r == nil {
		return reviewedIdentityGames(games, g.DevelopmentParent, g.IdentityReview)
	}
	if !g.ReviewAccepted || r.Reason == "" || len(r.Members) != len(games) {
		return games, "owner confirmation does not cover the selected cards"
	}
	actual := newOwnerMergeReview(games, r.Members, r.Reason)
	if actual.Key != r.Key {
		return games, "owner-confirmed membership or releases changed; review the selection again"
	}
	checked := append([]catalogGame(nil), games...)
	for i := range checked {
		if checked[i].HasRomsetBindings {
			return games, "MAME cards require their snapshot plan"
		}
		checked[i].ContentKey = r.Key
	}
	return checked, ""
}
func renderGroupDocument(games []catalogGame, regions regionRules, g Group) ([]byte, error) {
	checked, reason := groupReviewedGames(games, g)
	if reason != "" {
		return nil, fmt.Errorf("%s", reason)
	}
	data, err := mergedDocument(checked, regions, g.Edits)
	if err != nil || g.ownerReview == nil {
		return data, err
	}
	doc, err := catalogyaml.Parse(data)
	if err != nil {
		return nil, err
	}
	fold := catalogyaml.Value(doc.Root(), "fold")
	if fold == nil {
		fold = &yaml.Node{Kind: yaml.MappingNode}
		if err = catalogyaml.Set(doc.Root(), "fold", fold); err != nil {
			return nil, err
		}
	}
	reviews := catalogyaml.Value(fold, "owner_reviews")
	if reviews == nil {
		reviews = &yaml.Node{Kind: yaml.MappingNode}
		if err = catalogyaml.Set(fold, "owner_reviews", reviews); err != nil {
			return nil, err
		}
	}
	if reviews.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("fold.owner_reviews must be a mapping")
	}
	entry := &yaml.Node{}
	if err = entry.Encode(g.ownerReview); err != nil {
		return nil, err
	}
	if err = catalogyaml.Set(reviews, g.ownerReview.Key, entry); err != nil {
		return nil, err
	}
	return doc.Marshal()
}
