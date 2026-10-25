package catalogfold

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"rommeta/internal/arcaderepair"
	"rommeta/internal/catalogfs"
)

// MAME relationships are authoritative inside the accepted hardware boundaries.
// Provider IDs must not feed these cards back into the ordinary merge engine.
type mameFoldPlan struct {
	report   arcaderepair.Report
	work     string
	keepWork bool
}

func prepareMAMEFold(root string, options Options) (*mameFoldPlan, error) {
	info, err := catalogfs.Stat(filepath.Join(root, "bindings", "romsets", "mame"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("MAME bindings path is not a directory")
	}
	plan := &mameFoldPlan{}
	// Edition-only mode concerns provider relationships, not MAME variants.
	if options.EditionsOnly {
		return plan, nil
	}
	plan.work, err = os.MkdirTemp("", "rommeta-mame-fold-")
	if err != nil {
		return nil, err
	}
	plan.report, err = arcaderepair.PrepareMatching(root, filepath.Join(plan.work, "review"), options.Match, func(item string) {
		options.Progress.Update("planning MAME fold", 0, 0, item)
	})
	if err != nil {
		plan.close()
		return nil, fmt.Errorf("plan MAME fold: %w", err)
	}
	return plan, nil
}

func (plan *mameFoldPlan) close() {
	if plan == nil || plan.work == "" || plan.keepWork {
		return
	}
	// Only remove the unique directory created by this invocation, never data-root.
	_ = catalogfs.RemoveAll(plan.work)
}

func (plan *mameFoldPlan) finish(root string, options Options, result *PlatformResult, groups *[]Group) error {
	if plan == nil {
		return nil
	}
	blocked := ""
	if len(plan.report.Conflicts) > 0 {
		blocked = "MAME fold conflicts: " + strings.Join(plan.report.Conflicts, "; ")
	} else if options.Review != nil {
		blocked = "MAME snapshot groups are held during source-ID interactive review; use catalog fold --platform Arcade --details, then --apply"
	}
	applied := false
	if options.Apply && blocked == "" && len(plan.report.Groups) > 0 {
		options.Progress.Update("applying MAME fold", 0, len(plan.report.Groups), "checking files and updating set bindings")
		backup, err := arcaderepair.Apply(root, filepath.Join(plan.work, "review", "review.json"))
		if err != nil {
			plan.keepWork = true
			return fmt.Errorf("MAME fold: %w; prepared files: %s; backup: %s", err, plan.work, backup)
		}
		if backup != "" {
			result.Backups = append(result.Backups, backup)
		}
		applied = true
	}
	for _, candidate := range plan.report.Groups {
		keep := candidate.Sources[0]
		for _, source := range candidate.Sources {
			if source == candidate.Target {
				keep = source
			}
		}
		group := Group{
			Platform: "Arcade", Keep: GameRef{Folder: keep, Title: candidate.Title}, TargetFolder: candidate.Target,
			ReleaseCount: candidate.Releases, FingerprintCount: candidate.Fingerprints,
			Evidence:     []Evidence{{Source: "mame", Field: "snapshot", Value: plan.report.Snapshot, Games: append([]string(nil), candidate.Sources...)}},
			ReviewReason: "MAME import grouping, preserving hardware boundaries and versioned set bindings",
			Applied:      applied,
		}
		for _, source := range candidate.Sources {
			if source != keep {
				group.Merge = append(group.Merge, GameRef{Folder: source, Title: source})
			}
		}
		if blocked != "" {
			group.Skipped, group.SkipReason = true, blocked
			result.SkippedGroupCount++
		}
		if applied {
			group.MovedFileCount = candidate.OtherFiles
			result.AppliedGroupCount++
			result.MovedFileCount += group.MovedFileCount
		}
		if len(candidate.Sources) == 1 {
			result.RenamedGameCount++
		}
		result.CandidateGroupCount++
		result.IdentityCount++
		result.MergedGameCount += len(group.Merge)
		result.ReleaseCount += group.ReleaseCount
		result.FingerprintCount += group.FingerprintCount
		*groups = append(*groups, group)
	}
	return nil
}
