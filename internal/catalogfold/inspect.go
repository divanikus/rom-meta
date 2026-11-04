package catalogfold

import "fmt"

// Inspect returns the current reviewed draft without resetting edits or approvals.
func (p *Plan) Inspect(id int) (PlannedGroup, error) {
	if id < 0 || id >= len(p.Groups) {
		return PlannedGroup{}, fmt.Errorf("unknown group")
	}
	group := p.Groups[id]
	if err := p.verify(group); err != nil {
		return group, err
	}
	return group, nil
}
