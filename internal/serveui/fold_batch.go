package serveui

import (
	"context"
	"fmt"
	"rommeta/internal/catalogfold"
)

// foldBatch retains independent platform plans behind one review ID. Global group
// IDs never identify cards across platforms; all writes delegate to the owning plan.
type foldBatch struct {
	Platform  string
	Platforms []string
	Groups    []catalogfold.PlannedGroup
	plans     []*catalogfold.Plan
	refs      []foldGroupRef
}
type foldGroupRef struct{ platform, group int }

func (b *foldBatch) add(p *catalogfold.Plan) {
	index := len(b.plans)
	b.plans = append(b.plans, p)
	b.Platforms = append(b.Platforms, p.Platform)
	if len(b.plans) == 1 {
		b.Platform = p.Platform
	} else {
		b.Platform = ""
	}
	for _, g := range p.Groups {
		b.refs = append(b.refs, foldGroupRef{index, g.ID})
		g.ID = len(b.Groups)
		b.Groups = append(b.Groups, g)
	}
}
func (b *foldBatch) Close() {
	for _, p := range b.plans {
		p.Close()
	}
}
func (b *foldBatch) group(id int) (*catalogfold.Plan, int, error) {
	if id < 0 || id >= len(b.refs) {
		return nil, 0, fmt.Errorf("unknown group")
	}
	ref := b.refs[id]
	return b.plans[ref.platform], ref.group, nil
}
func (b *foldBatch) Inspect(id int) (catalogfold.PlannedGroup, error) {
	p, local, err := b.group(id)
	if err != nil {
		return catalogfold.PlannedGroup{}, err
	}
	g, err := p.Inspect(local)
	g.ID = id
	return g, err
}
func (b *foldBatch) Preview(id int, draft catalogfold.Draft) (catalogfold.PlannedGroup, error) {
	p, local, err := b.group(id)
	if err != nil {
		return catalogfold.PlannedGroup{}, err
	}
	g, err := p.Preview(local, draft)
	g.ID = id
	if err == nil {
		b.Groups[id] = g
	}
	return g, err
}
func (b *foldBatch) split(decisions []catalogfold.Decision) ([][]catalogfold.Decision, error) {
	out := make([][]catalogfold.Decision, len(b.plans))
	seen := map[int]bool{}
	for _, d := range decisions {
		if d.ID < 0 || d.ID >= len(b.refs) || seen[d.ID] {
			return nil, fmt.Errorf("invalid or duplicate group")
		}
		seen[d.ID] = true
		ref := b.refs[d.ID]
		d.ID = ref.group
		out[ref.platform] = append(out[ref.platform], d)
	}
	return out, nil
}
func (b *foldBatch) Apply(ctx context.Context, decisions []catalogfold.Decision, keepBackup bool) (catalogfold.AppliedPlan, error) {
	result := catalogfold.AppliedPlan{}
	if len(decisions) == 0 {
		return result, fmt.Errorf("select groups to apply")
	}
	split, err := b.split(decisions)
	if err != nil {
		return result, err
	}
	// Validate every selected platform before the first mutation. The shared writer
	// lock spans this whole batch; each platform still has its own transaction.
	for i, d := range split {
		if len(d) > 0 {
			if err := b.plans[i].Validate(ctx, d); err != nil {
				return result, fmt.Errorf("%s: %w", b.Platforms[i], err)
			}
		}
	}
	for i, d := range split {
		if len(d) == 0 {
			continue
		}
		part, err := b.plans[i].ApplyWithBackup(ctx, d, keepBackup)
		result.Applied += part.Applied
		result.KeptSeparate += part.KeptSeparate
		result.Backups = append(result.Backups, part.Backups...)
		if err != nil {
			return result, fmt.Errorf("%s: %w", b.Platforms[i], err)
		}
	}
	return result, nil
}
func (b *foldBatch) Continue(ctx context.Context, decisions []catalogfold.Decision) (*foldBatch, error) {
	split, err := b.split(decisions)
	if err != nil {
		return nil, err
	}
	next := &foldBatch{Groups: []catalogfold.PlannedGroup{}}
	for i, p := range b.plans {
		plan, err := p.Continue(ctx, split[i])
		if err != nil {
			next.Close()
			return nil, fmt.Errorf("%s: %w", p.Platform, err)
		}
		next.add(plan)
	}
	return next, nil
}
