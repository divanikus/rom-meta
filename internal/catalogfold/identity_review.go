package catalogfold

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"

	"rommeta/internal/contentidentity"
)

// ReviewedIdentity is an explicit content-equivalence decision, not discovery
// evidence. Every member's previous boundary and every exact release must be
// acknowledged; the surrounding plan additionally binds both metadata hashes.
// References preserve the independent evidence used by the operator.
type ReviewedIdentity struct {
	ContentKey   string            `json:"content_key"`
	PreviousKeys map[string]string `json:"previous_keys"`
	Releases     []string          `json:"releases"`
	References   []string          `json:"references"`
}

func reviewedIdentityGames(games []catalogGame, parent string, review *ReviewedIdentity) ([]catalogGame, string) {
	if review == nil {
		return reviewedDevelopmentGames(games, parent)
	}
	if parent != "" {
		return games, "identity_review and development_parent are mutually exclusive"
	}
	hash, err := hex.DecodeString(strings.TrimPrefix(review.ContentKey, "reviewed."))
	if !strings.HasPrefix(review.ContentKey, "reviewed.") || err != nil || len(hash) != sha256.Size {
		return games, "identity review requires a durable reviewed SHA256 content key"
	}
	if len(review.PreviousKeys) != len(games) {
		return games, "identity review must acknowledge every previous member content key"
	}
	if len(review.References) == 0 {
		return games, "identity review requires independent evidence references"
	}
	for _, reference := range review.References {
		u, err := url.Parse(reference)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return games, "identity review requires absolute HTTPS evidence references"
		}
	}
	actual := map[string]bool{}
	classes := map[string]bool{}
	for _, game := range games {
		old, exists := review.PreviousKeys[game.Folder]
		if !exists || old != game.ContentKey {
			return games, "identity review has a changed or unacknowledged previous content key"
		}
		if len(game.Content) == 0 {
			return games, "identity review cannot infer missing release content"
		}
		for _, release := range game.Releases {
			actual[release.Key] = true
		}
		for _, profile := range game.Content {
			class := "game"
			if profile.Component != "" {
				classes[class] = true
				continue
			}
			switch profile.Kind {
			case "addon", "expansion", "standalone expansion", "supplement", "update", "compilation":
				class = profile.Kind
			}
			if profile.Product != "" {
				switch profile.Product {
				case "addon", "expansion", "standalone expansion", "supplement", "update", "compilation":
					class = profile.Product
				}
			}
			classes[class] = true
		}
	}
	// Explicit equivalence may resolve regional edition labels and mistaken
	// provider taxonomy, but cannot absorb an addon/supplement into a full game.
	if len(classes) != 1 {
		return games, "identity review cannot combine a game with a separate addon, supplement or compilation"
	}
	names := map[string]bool{}
	for _, name := range review.Releases {
		if !actual[name] || names[name] {
			return games, fmt.Sprintf("identity review has a duplicate or unknown release %q", name)
		}
		names[name] = true
	}
	if len(names) != len(actual) {
		return games, "identity review must cover every exact release"
	}
	checked := append([]catalogGame(nil), games...)
	for i := range checked {
		checked[i].ContentKey = review.ContentKey
		// Compatibility is explicitly reviewed. Original role/build/name/source
		// values live in the preserved YAML blocks, not in this temporary profile.
		checked[i].Content = []contentidentity.Profile{{Kind: "retail"}}
	}
	return checked, ""
}
