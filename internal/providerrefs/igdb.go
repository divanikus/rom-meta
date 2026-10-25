// Package providerrefs describes explicit curated foreign-provider decisions.
package providerrefs

import (
	"fmt"
	"net/url"
)

type IGDBDecision struct {
	Source   string   `json:"source" yaml:"source"`
	FromID   int64    `json:"from_id" yaml:"from_id"`
	Action   string   `json:"action" yaml:"action"`
	ToID     int64    `json:"to_id,omitempty" yaml:"to_id,omitempty"`
	Reason   string   `json:"reason" yaml:"reason"`
	Evidence []string `json:"evidence" yaml:"evidence"`
}

func (d IGDBDecision) Validate() error {
	if d.Source != "hasheous" || d.FromID < 1 || d.Reason == "" || len(d.Evidence) == 0 ||
		(d.Action != "suppress" && d.Action != "replace") ||
		(d.Action == "suppress" && d.ToID != 0) || (d.Action == "replace" && (d.ToID < 1 || d.ToID == d.FromID)) {
		return fmt.Errorf("invalid reviewed IGDB reference decision")
	}
	for _, evidence := range d.Evidence {
		u, e := url.Parse(evidence)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return fmt.Errorf("IGDB decision requires public HTTPS evidence")
		}
	}
	return nil
}
