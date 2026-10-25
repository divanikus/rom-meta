package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"rommeta/internal/catalogapi"
	"rommeta/internal/romm"
)

// This consumer-only stdin seam lets the separate native-runtime job reuse the
// tested Go mapper. It is deliberately not part of the generic HTTP API.
func runRomMProject(args []string) error {
	var reportConflicts bool
	flags := flag.NewFlagSet("project-snapshot", flag.ContinueOnError)
	flags.BoolVar(&reportConflicts, "report-conflicts", false, "Return typed identity conflicts per input; other failures remain fatal")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("project-snapshot reads one JSON document from stdin")
	}
	var input struct {
		Selection catalogapi.Selection                  `json:"selection"`
		Before    map[string]map[string]json.RawMessage `json:"before"`
	}
	decoder := json.NewDecoder(os.Stdin)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return fmt.Errorf("invalid projection input: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("projection requires exactly one JSON document")
	}
	if input.Selection.SchemaVersion != 1 || len(input.Selection.Inputs) < 1 {
		return fmt.Errorf("invalid projection selection")
	}
	type result struct {
		ROM       string                 `json:"rom"`
		Providers *romm.ProviderEvidence `json:"providers"`
		Form      map[string]string      `json:"form"`
		Conflict  string                 `json:"conflict,omitempty"`
	}
	output := []result{}
	for _, entry := range input.Selection.Inputs {
		card, ok := input.Selection.Cards[entry.Record.Game]
		if !ok {
			return fmt.Errorf("missing input card")
		}
		p := entry.Payload
		evidence, form, err := romm.ProjectSnapshot(entry.Record, []byte(card.Game.YAML), []byte(card.Fingerprints.YAML), romm.VerificationProof{Size: p.Size, SHA1: p.SHA1, SHA256: p.SHA256, MD5: p.MD5, CRC32: p.CRC32}, input.Before[entry.Record.ROM])
		if err != nil {
			var conflict *romm.IdentityConflict
			if reportConflicts && errors.As(err, &conflict) {
				output = append(output, result{ROM: entry.Record.ROM, Conflict: conflict.Error()})
				continue
			}
			return fmt.Errorf("%s [%s]: %w", entry.Record.ROM, entry.Record.Game, err)
		}
		output = append(output, result{ROM: entry.Record.ROM, Providers: evidence, Form: form})
	}
	return json.NewEncoder(os.Stdout).Encode(output)
}
