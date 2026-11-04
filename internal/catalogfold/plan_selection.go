package catalogfold

import (
	"fmt"
	"strings"
)

func (p *Plan) selectMembers(pg PlannedGroup, folders []string) ([]GameRef, bool, error) {
	current := append([]GameRef{pg.Group.Keep}, pg.Group.Merge...)
	if folders == nil {
		return current, false, nil
	}
	if len(folders) < 2 {
		return nil, false, fmt.Errorf("select at least two cards")
	}
	allowed := map[string]GameRef{}
	before := map[string]bool{}
	for _, ref := range pg.Candidates {
		allowed[ref.Folder] = ref
	}
	for _, ref := range current {
		before[ref.Folder] = true
	}
	seen := map[string]bool{}
	changed := len(current) != len(folders)
	for _, folder := range folders {
		if _, ok := allowed[folder]; !ok || seen[folder] {
			return nil, false, fmt.Errorf("select distinct cards from this candidate group")
		}
		seen[folder] = true
		changed = changed || !before[folder]
	}
	if !changed {
		return current, false, nil
	}
	var refs []GameRef
	for _, ref := range pg.Candidates {
		if seen[ref.Folder] {
			refs = append(refs, ref)
		}
	}
	candidate := pg.Group
	candidate.Keep = refs[0]
	candidate.Merge = refs[1:]
	games, err := readGroup(p.options.DataRoot, candidate)
	if err != nil {
		return nil, false, err
	}
	indexes := make([]int, len(games))
	for i := range indexes {
		indexes[i] = i
	}
	keep := chooseKeep(games, indexes, p.regions())
	refs[0], refs[keep] = refs[keep], refs[0]
	return refs, true, nil
}
func (p *Plan) evaluateIdentity(pg *PlannedGroup, games []catalogGame) error {
	g := &pg.Group
	for _, game := range games {
		if game.HasRomsetBindings {
			return fmt.Errorf("MAME cards require their snapshot plan")
		}
	}
	checked := withEditionEvidence(append([]catalogGame(nil), games...), false)
	var err error
	checked, err = withPS3DiscFamilies(checked)
	if err != nil {
		return err
	}
	checked = withRegionalAliasEvidence(checked)
	checked = withXboxSerialHints(checked)
	indexes := make([]int, len(checked))
	for i := range indexes {
		indexes[i] = i
	}
	reason := identityGroupHoldReason(checked, indexes)
	if family := ps3FamilyGroupHoldReason(checked, indexes); family != "" {
		reason = strings.Trim(strings.Join([]string{reason, family}, "; "), "; ")
	}
	if reason == "" && serialHintsRequired(checked, indexes) {
		reason = "Shared serials are only a discovery hint; review these cards before merging."
	}
	pg.NeedsIdentity = pg.Manual || reason != ""
	if pg.NeedsIdentity && reason == "" {
		reason = "Confirm that the selected cards belong together as one game."
	}
	pg.IdentityReason = reason
	g.HoldReason = ""
	g.Skipped = false
	g.SkipReason = ""
	g.ReviewRequired = reason
	g.ReleaseCount = 0
	g.FingerprintCount = 0
	selected := map[string]bool{g.Keep.Folder: true}
	for _, ref := range g.Merge {
		selected[ref.Folder] = true
		g.ReleaseCount += ref.ReleaseCount
	}
	for _, game := range games {
		if game.Folder != g.Keep.Folder {
			g.FingerprintCount += game.FingerprintCount
		}
	}
	g.Evidence = nil
	for _, source := range pg.original.Evidence {
		e := source
		e.Games = nil
		for _, folder := range source.Games {
			if selected[folder] {
				e.Games = append(e.Games, folder)
			}
		}
		if len(e.Games) >= 2 {
			g.Evidence = append(g.Evidence, e)
		}
	}
	g.ownerReview = nil
	if pg.NeedsIdentity && g.ReviewAccepted {
		if strings.TrimSpace(g.ReviewReason) == "" {
			g.ReviewReason = "Owner confirmed these selected cards belong together as one game."
		}
		g.ownerReview = newOwnerMergeReview(games, pg.Members, g.ReviewReason)
	} else {
		g.ReviewReason = ""
	}
	return nil
}
