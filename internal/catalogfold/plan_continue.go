package catalogfold

import (
	"context"
	"sort"
	"strings"
)

// Continue builds a fresh review after an apply. The old plan remains consumed;
// only drafts whose complete candidate data still matches are carried forward.
func (p *Plan) Continue(ctx context.Context, decisions []Decision) (*Plan, error) {
	options := p.options
	options.Context = ctx
	options.Progress = nil // The original streaming response is already closed.
	if p.manual {
		return &Plan{Platform: p.Platform, options: options, manual: true, Groups: []PlannedGroup{}}, nil
	}
	next, err := BuildPlan(options, nil)
	if err != nil {
		return nil, err
	}
	applied := map[int]bool{}
	for _, d := range decisions {
		applied[d.ID] = true
	}
	oldGroups := map[string]PlannedGroup{}
	for _, pg := range p.Groups {
		if !applied[pg.ID] && pg.Revision > 1 && p.verify(pg) == nil {
			oldGroups[continuationKey(pg)] = pg
		}
	}
	for _, pg := range next.Groups {
		old, ok := oldGroups[continuationKey(pg)]
		if !ok || old.Stamp != pg.Stamp || old.Group.NativeDigest != pg.Group.NativeDigest {
			continue
		}
		folders := []string{old.Group.Keep.Folder}
		for _, ref := range old.Group.Merge {
			folders = append(folders, ref.Folder)
		}
		if old.Group.NativeDigest != "" {
			folders = nil
		}
		// Preview rechecks current data and recalculates readiness and identity evidence.
		// A draft that no longer validates stays a fresh, unconfirmed group.
		_, _ = next.Preview(pg.ID, Draft{Folders: folders, Primary: old.Group.Keep.Folder, Fields: old.Group.Edits, Reason: old.Group.ReviewReason, Confirm: old.Group.ReviewAccepted})
	}
	return next, nil
}

func continuationKey(pg PlannedGroup) string {
	folders := make([]string, 0, len(pg.Candidates))
	for _, ref := range pg.Candidates {
		folders = append(folders, ref.Folder)
	}
	sort.Strings(folders)
	return strings.Join(folders, "\x00")
}
