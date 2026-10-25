package catalogfold

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"rommeta/internal/catalogfs"
)

// ReviewPlan records explicitly reviewed partitions. Hashes bind each decision
// to the reviewed data; this is not a universal title alias list.
type ReviewPlan struct {
	Version int             `json:"version"`
	Groups  []ReviewedGroup `json:"groups"`
}

type ReviewedGroup struct {
	Platform          string            `json:"platform"`
	Members           []ReviewedMember  `json:"members"`
	PrimaryCandidates []string          `json:"primary_candidates,omitempty"`
	Reason            string            `json:"reason"`
	DevelopmentParent string            `json:"development_parent,omitempty"`
	IdentityReview    *ReviewedIdentity `json:"identity_review,omitempty"`
}

type ReviewedMember struct {
	Folder             string `json:"folder"`
	GameSHA256         string `json:"game_sha256"`
	FingerprintsSHA256 string `json:"fingerprints_sha256"`
}

func readReviewPlan(path string) (ReviewPlan, error) {
	contents, err := catalogfs.ReadFile(path)
	if err != nil {
		return ReviewPlan{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(bytes.TrimPrefix(contents, []byte{0xef, 0xbb, 0xbf})))
	decoder.DisallowUnknownFields()
	var plan ReviewPlan
	if err := decoder.Decode(&plan); err != nil {
		return plan, fmt.Errorf("read fold review plan: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return plan, fmt.Errorf("fold review plan must contain exactly one JSON document")
	}
	if plan.Version != 1 || len(plan.Groups) == 0 {
		return plan, fmt.Errorf("fold review plan requires version 1 and at least one group")
	}
	return plan, nil
}

func prepareReviewPlan(root string, plan ReviewPlan, options Options, defaults regionRules, overrides map[string]regionRules) ([]string, map[string][]Group, error) {
	groups := map[string][]Group{}
	seen := map[string]bool{}
	for n, reviewed := range plan.Groups {
		options.Progress.Update("checking review plan", n+1, len(plan.Groups), reviewed.Platform)
		if !reviewComponent(reviewed.Platform) || len(reviewed.Members) < 2 || strings.TrimSpace(reviewed.Reason) == "" {
			return nil, nil, fmt.Errorf("review group %d needs a safe platform name, at least two members and a reason", n+1)
		}
		var games []catalogGame
		memberIndexes := map[string]int{}
		releases := map[string]any{}
		for _, member := range reviewed.Members {
			if !reviewComponent(member.Folder) {
				return nil, nil, fmt.Errorf("unsafe reviewed folder %q", member.Folder)
			}
			key := strings.ToLower(reviewed.Platform + "/" + member.Folder)
			if seen[key] {
				return nil, nil, fmt.Errorf("duplicate reviewed member %s", key)
			}
			seen[key] = true
			if err := verifyReviewedMember(root, reviewed.Platform, member); err != nil {
				return nil, nil, err
			}
			path := filepath.Join(root, "platforms", reviewed.Platform, member.Folder)
			if err := validateReviewedContent(path, releases); err != nil {
				return nil, nil, fmt.Errorf("reviewed member %s: %w", key, err)
			}
			game, err := readGameFull(catalogGame{Platform: reviewed.Platform, Folder: member.Folder, Path: path})
			if err != nil {
				return nil, nil, err
			}
			memberIndexes[member.Folder] = len(games)
			games = append(games, game)
		}
		var reason string
		games, reason = reviewedIdentityGames(games, reviewed.DevelopmentParent, reviewed.IdentityReview)
		if reason != "" {
			return nil, nil, fmt.Errorf("review group %d: %s", n+1, reason)
		}
		var eligible []int
		if reviewed.DevelopmentParent != "" {
			eligible = []int{memberIndexes[reviewed.DevelopmentParent]}
			if len(reviewed.PrimaryCandidates) > 0 && (len(reviewed.PrimaryCandidates) != 1 || reviewed.PrimaryCandidates[0] != reviewed.DevelopmentParent) {
				return nil, nil, fmt.Errorf("development parent must be the sole primary candidate")
			}
		} else if len(reviewed.PrimaryCandidates) == 0 {
			for i := range games {
				eligible = append(eligible, i)
			}
		} else {
			for _, folder := range reviewed.PrimaryCandidates {
				i, ok := memberIndexes[folder]
				if !ok {
					return nil, nil, fmt.Errorf("primary candidate %q is not a member of review group %d", folder, n+1)
				}
				eligible = append(eligible, i)
			}
		}
		games = withEditionEvidence(games, false)
		keep := chooseKeep(games, eligible, rulesForPlatform(defaults, overrides, reviewed.Platform))
		group := Group{Platform: reviewed.Platform, Keep: gameRef(games[keep]), ReviewReason: reviewed.Reason, DevelopmentParent: reviewed.DevelopmentParent, IdentityReview: reviewed.IdentityReview}
		for i, game := range games {
			if i == keep {
				continue
			}
			group.Merge = append(group.Merge, gameRef(game))
			group.ReleaseCount += len(game.Releases)
			group.FingerprintCount += game.FingerprintCount
		}
		sort.Slice(group.Merge, func(i, j int) bool { return group.Merge[i].Folder < group.Merge[j].Folder })
		groups[reviewed.Platform] = append(groups[reviewed.Platform], group)
	}
	var platforms []string
	for platform := range groups {
		platforms = append(platforms, platform)
	}
	sort.Strings(platforms)
	return platforms, groups, nil
}

func reviewComponent(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || value == "." || value == ".." || strings.HasSuffix(value, ".") || strings.ContainsAny(value, `/\:<>"|?*`) {
		return false
	}
	for _, char := range value {
		if char < 32 {
			return false
		}
	}
	base := strings.ToUpper(strings.SplitN(value, ".", 2)[0])
	switch base {
	case "CON", "PRN", "AUX", "NUL":
		return false
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
		return false
	}
	return true
}

func verifyReviewedMember(root, platform string, member ReviewedMember) error {
	// Reject linked path components as well as linked files before any write.
	path := root
	for _, component := range []string{"platforms", platform, member.Folder} {
		path = filepath.Join(path, component)
		info, err := catalogfs.Lstat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("reviewed path is not a regular directory: %s", path)
		}
	}
	for _, file := range []struct{ name, hash string }{{"game.yaml", member.GameSHA256}, {"fingerprints.yaml", member.FingerprintsSHA256}} {
		expected, err := hex.DecodeString(file.hash)
		if err != nil || len(expected) != sha256.Size {
			return fmt.Errorf("invalid SHA256 for %s / %s", path, file.name)
		}
		filename := filepath.Join(path, file.name)
		info, err := catalogfs.Lstat(filename)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("reviewed file is not regular: %s", filename)
		}
		contents, err := catalogfs.ReadFile(filename)
		if err != nil {
			return err
		}
		actual := sha256.Sum256(contents)
		if !bytes.Equal(actual[:], expected) {
			return fmt.Errorf("review plan is stale: %s changed; review it again", filename)
		}
	}
	return nil
}

func verifyReviewedPlatform(root, platform string, plan ReviewPlan) error {
	for _, group := range plan.Groups {
		if group.Platform != platform {
			continue
		}
		for _, member := range group.Members {
			if err := verifyReviewedMember(root, platform, member); err != nil {
				return err
			}
		}
	}
	return nil
}

// The merge renderer owns these fields only. Refuse manual fields and conflicting
// release payloads instead of silently dropping them in a reviewed fold.
func validateReviewedContent(path string, releases map[string]any) error {
	for _, file := range []string{"game.yaml", "fingerprints.yaml"} {
		contents, err := catalogfs.ReadFile(filepath.Join(path, file))
		if err != nil {
			if file == "fingerprints.yaml" && os.IsNotExist(err) {
				continue
			}
			return err
		}
		var doc map[string]any
		if err := yaml.Unmarshal(contents, &doc); err != nil {
			return err
		}
		for key, value := range doc {
			if file == "fingerprints.yaml" {
				if key != "items" {
					return fmt.Errorf("unsupported fingerprint field %q", key)
				}
				continue
			}
			switch key {
			case "title", "default_release":
			case "aliases", "alternate_titles":
				items, ok := value.([]any)
				if !ok {
					return fmt.Errorf("%s must be a string sequence", key)
				}
				for _, item := range items {
					if _, ok := item.(string); !ok {
						return fmt.Errorf("%s must contain strings", key)
					}
				}
			case "releases":
				values, ok := value.(map[string]any)
				if !ok {
					return fmt.Errorf("releases must be a mapping")
				}
				for name, data := range values {
					if previous, exists := releases[name]; exists && !reflect.DeepEqual(previous, data) {
						return fmt.Errorf("conflicting release %q", name)
					}
					releases[name] = data
				}
			case "fold":
				values, ok := value.(map[string]any)
				if !ok {
					return fmt.Errorf("fold must be a mapping")
				}
				for name := range values {
					if name == "content_key" {
						if key, ok := values[name].(string); !ok || strings.TrimSpace(key) == "" {
							return fmt.Errorf("fold.content_key must be a nonempty string")
						}
						continue
					}
					if name != "ignore_evidence" {
						return fmt.Errorf("unsupported fold field %q", name)
					}
				}
			case "sources":
				values, ok := value.(map[string]any)
				if !ok {
					return fmt.Errorf("sources must be a mapping")
				}
				for source, raw := range values {
					stanza, ok := raw.(map[string]any)
					if !ok {
						return fmt.Errorf("source %s must be a mapping", source)
					}
					for name, value := range stanza {
						// Hasheous writes a diagnostic with its not_found state.
						// The renderer preserves that raw stanza when no match replaces it.
						if source == "hasheous" && name == "reason" {
							if _, ok := value.(string); !ok {
								return fmt.Errorf("source hasheous.reason must be a string")
							}
							continue
						}
						if name != "matches" && name != "status" && name != "checked_at" {
							return fmt.Errorf("unsupported source field %s.%s", source, name)
						}
					}
				}
			default:
				return fmt.Errorf("unsupported canonical field %q", key)
			}
		}
	}
	return nil
}
