package catalogfold

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"rommeta/internal/arcaderepair"
	"rommeta/internal/catalogedit"
	"rommeta/internal/catalogfs"
	"sort"
	"strings"
)

// Plan is one bounded platform review, not a durable job. Close releases MAME staging.
type Plan struct {
	Platform string         `json:"platform"`
	Groups   []PlannedGroup `json:"groups"`
	options  Options
	mame     *mameFoldPlan
	manual   bool
}
type PlannedGroup struct {
	ID             int               `json:"id"`
	Revision       int               `json:"revision"`
	Status         string            `json:"status"`
	Reason         string            `json:"reason,omitempty"`
	Group          Group             `json:"group"`
	Members        []ReviewedMember  `json:"-"`
	Stamp          string            `json:"-"`
	Result         *catalogedit.Card `json:"result,omitempty"`
	Manual         bool              `json:"manual,omitempty"`
	NeedsIdentity  bool              `json:"needs_identity,omitempty"`
	Conflicts      []string          `json:"conflicts,omitempty"`
	Candidates     []GameRef         `json:"candidates"`
	IdentityReason string            `json:"identity_reason,omitempty"`
	original       Group
	inspectionErr  error
}
type Decision struct {
	ID       int    `json:"id"`
	Revision int    `json:"revision"`
	Action   string `json:"action"`
}
type Draft struct {
	Folders []string          `json:"folders,omitempty"`
	Primary string            `json:"primary"`
	Fields  catalogedit.Patch `json:"fields"`
	Reason  string            `json:"reason"`
	Confirm bool              `json:"confirm"`
}
type AppliedPlan struct {
	Applied      int      `json:"applied"`
	KeptSeparate int      `json:"kept_separate"`
	Backups      []string `json:"backups,omitempty"`
}

func (p *Plan) Close() {
	if p.mame != nil {
		p.mame.close()
		p.mame = nil
	}
}
func groupDocument(root string, g Group, regions regionRules) ([]byte, error) {
	games, err := readGroup(root, g)
	if err != nil {
		return nil, err
	}
	return renderGroupDocument(games, regions, g)
}
func readGroup(root string, g Group) ([]catalogGame, error) {
	var games []catalogGame
	for _, ref := range append([]GameRef{g.Keep}, g.Merge...) {
		path, err := catalogedit.GamePath(root, g.Platform, ref.Folder)
		if err != nil {
			return nil, err
		}
		game, err := readGameFull(catalogGame{Platform: g.Platform, Folder: ref.Folder, Path: path})
		if err != nil {
			return nil, err
		}
		games = append(games, game)
	}
	return games, nil
}
func groupMembers(root string, g Group) ([]ReviewedMember, error) {
	var members []ReviewedMember
	for _, ref := range append([]GameRef{g.Keep}, g.Merge...) {
		path, err := catalogedit.GamePath(root, g.Platform, ref.Folder)
		if err != nil {
			return nil, err
		}
		game, err := catalogfs.ReadFile(filepath.Join(path, "game.yaml"))
		if err != nil {
			return nil, err
		}
		fp, err := catalogfs.ReadFile(filepath.Join(path, "fingerprints.yaml"))
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		members = append(members, ReviewedMember{Folder: ref.Folder, GameSHA256: catalogedit.Hash(game), FingerprintsSHA256: catalogedit.Hash(fp)})
	}
	return members, nil
}
func stampTree(path string) (string, error) {
	var items []string
	err := catalogfs.WalkDir(path, func(p string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		i, err := e.Info()
		if err != nil {
			return err
		}
		if !i.Mode().IsRegular() {
			return fmt.Errorf("unsupported file type: %s", p)
		}
		rel, err := filepath.Rel(path, p)
		if err != nil {
			return err
		}
		items = append(items, fmt.Sprintf("%s\x00%d\x00%d", rel, i.Size(), i.ModTime().UnixNano()))
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(items)
	return catalogedit.Hash([]byte(strings.Join(items, "\n"))), nil
}
func groupStamp(root string, g Group) (string, error) {
	var stamps []string
	for _, m := range append([]GameRef{g.Keep}, g.Merge...) {
		s, err := stampTree(filepath.Join(root, "platforms", g.Platform, m.Folder))
		if err != nil {
			return "", err
		}
		stamps = append(stamps, m.Folder+":"+s)
	}
	sort.Strings(stamps)
	return catalogedit.Hash([]byte(strings.Join(stamps, "\n"))), nil
}
func (p *Plan) regions() regionRules {
	return rulesForPlatform(newRegionRules(p.options.RegionPriority, p.options.RegionGroups), newPlatformRegionRules(p.options.PlatformRegionPriority, p.options.RegionGroups), p.Platform)
}
func BuildPlan(options Options, folders []string) (*Plan, error) {
	if !catalogedit.Component(options.Platform) {
		return nil, fmt.Errorf("choose one platform")
	}
	options.Apply = false
	options.Review = nil
	options.ReviewPlan = ""
	options.skipMAME = true
	options.planGroupsOnly = true
	p := &Plan{Platform: options.Platform, options: options, manual: len(folders) > 0, Groups: []PlannedGroup{}}
	var groups []Group
	if len(folders) > 0 {
		if len(folders) < 2 || len(folders) > 12 {
			return nil, fmt.Errorf("choose 2 to 12 cards")
		}
		seen := map[string]bool{}
		var games []catalogGame
		for _, folder := range folders {
			if seen[strings.ToLower(folder)] {
				return nil, fmt.Errorf("duplicate card")
			}
			seen[strings.ToLower(folder)] = true
			path, err := catalogedit.GamePath(options.DataRoot, options.Platform, folder)
			if err != nil {
				return nil, err
			}
			game, err := readGameFull(catalogGame{Platform: options.Platform, Folder: folder, Path: path})
			if err != nil {
				return nil, err
			}
			if game.HasRomsetBindings {
				return nil, fmt.Errorf("MAME cards use their snapshot plan; build a platform plan")
			}
			games = append(games, game)
		}
		indexes := make([]int, len(games))
		for i := range games {
			indexes[i] = i
		}
		keep := chooseKeep(games, indexes, p.regions())
		group := Group{Platform: options.Platform, Keep: gameRef(games[keep]), ReviewRequired: "Confirm that the selected cards belong together as one game."}
		for i, game := range games {
			if i != keep {
				group.Merge = append(group.Merge, gameRef(game))
				group.ReleaseCount += len(game.Releases)
				group.FingerprintCount += game.FingerprintCount
			}
		}
		group.HoldReason = contentGroupHoldReason(games)
		if conflicts := groupDirectIDConflicts(games, indexes); len(conflicts) > 0 {
			group.HoldReason = strings.Join(conflicts, "; ")
		}
		groups = []Group{group}
	} else {
		result, err := Fold(options)
		if err != nil {
			return nil, err
		}
		groups = result.Groups
		if options.Platform == "Arcade" {
			mame, err := prepareMAMEFold(options.DataRoot, options)
			if err != nil {
				return nil, err
			}
			p.mame = mame
			var pr PlatformResult
			if err := mame.finish(options.DataRoot, options, &pr, &groups); err != nil {
				p.Close()
				return nil, err
			}
			// This plan applies exact selections, so one conflicting MAME group
			// does not hold unrelated prepared groups as the all-at-once CLI does.
			if mame != nil {
				for i := range groups {
					if groups[i].NativeDigest == "" {
						continue
					}
					for _, candidate := range mame.report.Groups {
						if candidate.Target == groups[i].TargetFolder {
							groups[i].Skipped = candidate.Conflict != ""
							groups[i].SkipReason = candidate.Conflict
						}
					}
				}
			}
		}
	}
	for _, group := range groups {
		if options.Context != nil {
			if err := options.Context.Err(); err != nil {
				p.Close()
				return nil, err
			}
		}
		pg := PlannedGroup{ID: len(p.Groups), Revision: 1, Group: group, original: group, Candidates: append([]GameRef{group.Keep}, group.Merge...), Manual: len(folders) > 0, NeedsIdentity: group.ReviewRequired != ""}
		members, err := groupMembers(options.DataRoot, group)
		pg.Members = members
		if err == nil {
			pg.Stamp, err = groupStamp(options.DataRoot, group)
		}
		if err != nil {
			pg.blockInspection(err)
		} else {
			p.evaluate(&pg)
		}
		p.Groups = append(p.Groups, pg)
	}
	if options.Context != nil && options.Context.Err() != nil {
		p.Close()
		return nil, options.Context.Err()
	}
	return p, nil
}
func (pg *PlannedGroup) blockInspection(err error) {
	pg.inspectionErr = err
	pg.Status = "blocked"
	pg.Reason = fmt.Sprintf("Cannot inspect this group's files: %s. Restore access and rebuild the plan.", err)
	pg.Result = nil
	pg.Conflicts = nil
}
func (p *Plan) evaluate(pg *PlannedGroup) {
	pg.Result = nil
	pg.Conflicts = nil
	pg.Status = "ready"
	pg.Reason = ""
	g := &pg.Group
	if g.NativeDigest != "" && g.HoldReason != "" && len(g.MetadataConflicts) == 0 {
		pg.Status = "blocked"
		pg.Reason = g.HoldReason
		return
	}
	if g.NativeDigest != "" && g.Skipped {
		pg.Status = "blocked"
		pg.Reason = g.SkipReason
		return
	}
	var data []byte
	var err error
	if g.NativeDigest != "" {
		data, err = catalogfs.ReadFile(filepath.Join(p.mame.work, "review", "staged", "platforms", g.Platform, g.TargetFolder, "game.yaml"))
	} else {
		games, e := readGroup(p.options.DataRoot, *g)
		if e != nil {
			pg.blockInspection(e)
			return
		}
		if err = p.evaluateIdentity(pg, games); err != nil {
			pg.Status = "blocked"
			pg.Reason = err.Error()
			return
		}
		changes, reason, e := planGroupFiles(p.options.DataRoot, *g)
		if e != nil {
			pg.blockInspection(e)
			return
		} else if reason != "" {
			err = fmt.Errorf("%s", reason)
		} else {
			g.FileChanges = changes
			if pg.NeedsIdentity && !g.ReviewAccepted {
				data, err = mergedDocument(games, p.regions(), g.Edits)
			} else {
				data, err = renderGroupDocument(games, p.regions(), *g)
			}
		}
	}
	if err != nil {
		pg.Status = "blocked"
		pg.Reason = err.Error()
		if conflict, ok := err.(*fieldConflicts); ok {
			pg.Conflicts = conflict.Fields
			editable := true
			for _, key := range conflict.Fields {
				editable = editable && catalogedit.Editable(strings.Split(key, ".")[0])
			}
			if editable {
				pg.Status = "review"
			}
		}
		return
	}
	card, err := catalogedit.Inspect(data)
	if err != nil {
		pg.Status = "blocked"
		pg.Reason = err.Error()
		return
	}
	pg.Result = &card
	if pg.NeedsIdentity && !g.ReviewAccepted {
		pg.Status = "review"
		pg.Reason = g.ReviewRequired
	}
}
func (p *Plan) verify(pg PlannedGroup) error {
	if pg.inspectionErr != nil {
		return fmt.Errorf("%s", pg.Reason)
	}
	for _, m := range pg.Members {
		if err := verifyReviewedMember(p.options.DataRoot, p.Platform, m); err != nil {
			return fmt.Errorf("plan is stale; rebuild it: %w", err)
		}
	}
	stamp, err := groupStamp(p.options.DataRoot, pg.original)
	if err != nil || stamp != pg.Stamp {
		return fmt.Errorf("card files changed; rebuild the plan")
	}
	return nil
}
func (p *Plan) Preview(id int, draft Draft) (PlannedGroup, error) {
	if id < 0 || id >= len(p.Groups) {
		return PlannedGroup{}, fmt.Errorf("unknown group")
	}
	pg := p.Groups[id]
	if err := p.verify(pg); err != nil {
		return pg, err
	}
	if pg.Group.NativeDigest != "" {
		if draft.Folders != nil {
			return pg, fmt.Errorf("MAME membership follows snapshot bindings")
		}
		if len(draft.Fields) > 0 || draft.Primary != "" && draft.Primary != pg.Group.Keep.Folder {
			return pg, fmt.Errorf("MAME primary follows snapshot bindings; edit cards separately and rebuild")
		}
	} else {
		refs, changed, err := p.selectMembers(pg, draft.Folders)
		if err != nil {
			return pg, err
		}
		if changed {
			pg.Manual = true
		}
		if draft.Primary != "" {
			found := false
			for i, ref := range refs {
				if ref.Folder == draft.Primary {
					refs[0], refs[i] = refs[i], refs[0]
					found = true
					break
				}
			}
			if !found {
				return pg, fmt.Errorf("primary must be a group member")
			}
		}
		pg.Group.Keep = refs[0]
		pg.Group.Merge = append([]GameRef(nil), refs[1:]...)
		pg.Group.Edits = draft.Fields
		pg.Group.ReviewAccepted = draft.Confirm
		pg.Group.ownerReview = nil
		pg.Group.ReviewReason = strings.TrimSpace(draft.Reason)
		if draft.Confirm && pg.Group.ReviewReason == "" {
			pg.Group.ReviewReason = "Owner confirmed these selected cards belong together as one game."
		}
	}
	pg.Revision++
	p.evaluate(&pg)
	p.Groups[id] = pg
	return pg, nil
}
func (p *Plan) Apply(ctx context.Context, decisions []Decision) (AppliedPlan, error) {
	result := AppliedPlan{}
	if len(decisions) == 0 {
		return result, fmt.Errorf("select groups to apply")
	}
	seen := map[int]bool{}
	var ordinary []Decision
	var targets []string
	for _, d := range decisions {
		if d.ID < 0 || d.ID >= len(p.Groups) || seen[d.ID] {
			return result, fmt.Errorf("invalid or duplicate group")
		}
		seen[d.ID] = true
		pg := p.Groups[d.ID]
		if d.Revision != pg.Revision {
			return result, fmt.Errorf("preview changed; review the current result")
		}
		if err := p.verify(pg); err != nil {
			return result, err
		}
		if d.Action == "merge" {
			if pg.Status != "ready" {
				return result, fmt.Errorf("group is not ready: %s", pg.Reason)
			}
		} else if d.Action == "separate" {
			if pg.Group.NativeDigest != "" || len(evidenceTokens(pg.Group.Evidence)) == 0 {
				return result, fmt.Errorf("no fold evidence to reject; leave this case unresolved")
			}
		} else {
			return result, fmt.Errorf("unknown decision")
		}
		if pg.Group.NativeDigest != "" {
			targets = append(targets, pg.Group.TargetFolder)
		} else {
			ordinary = append(ordinary, d)
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	// Ordinary groups share one existing platform transaction, with an external
	// writer/stale check before the swap. MAME retains its binding-aware transaction.
	if len(ordinary) > 0 {
		platformPath := filepath.Join(p.options.DataRoot, "platforms", p.Platform)
		before, err := stampTree(platformPath)
		if err != nil {
			return result, err
		}
		tx, err := beginPlatformTransaction(p.options.DataRoot, p.Platform)
		if err != nil {
			return result, err
		}
		defer tx.abort()
		for _, d := range ordinary {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			pg := p.Groups[d.ID]
			g := pg.Group
			for _, member := range pg.Members {
				if err := verifyReviewedMember(tx.root, p.Platform, member); err != nil {
					return result, err
				}
			}
			if d.Action == "separate" {
				if err := ignoreGroupEvidence(tx.root, &g); err != nil {
					return result, err
				}
			} else {
				if len(g.MetadataConflicts) > 0 {
					g.HoldReason = ""
				}
				if err := applyGroup(tx.root, &g, p.regions()); err != nil {
					return result, err
				}
				if g.Skipped {
					return result, fmt.Errorf("group no longer ready: %s", g.SkipReason)
				}
			}
		}
		after, err := stampTree(platformPath)
		if err != nil || after != before {
			return result, fmt.Errorf("catalog changed during apply; rebuild the plan")
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := tx.commit(); err != nil {
			return result, err
		}
		result.Backups = append(result.Backups, tx.backupPath)
		for _, d := range ordinary {
			if d.Action == "merge" {
				result.Applied++
			} else {
				result.KeptSeparate++
			}
		}
	}
	if len(targets) > 0 {
		backup, err := arcaderepair.ApplySelected(p.options.DataRoot, filepath.Join(p.mame.work, "review", "review.json"), targets)
		if backup != "" {
			result.Backups = append(result.Backups, backup)
		}
		if err != nil {
			return result, err
		}
		result.Applied += len(targets)
	}
	return result, nil
}
func nativeDigest(group arcaderepair.Group, binding string) string {
	data, _ := json.Marshal(struct {
		Before  map[string]map[string]string
		After   map[string]string
		Binding string
	}{group.Before, group.After, binding})
	return catalogedit.Hash(data)
}
