package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"rommeta/internal/exporter"
)

func runRomMInspect(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("inspect-inputs reads one JSON document from stdin")
	}
	var input struct {
		Root        string   `json:"root"`
		System      string   `json:"system"`
		ROMs        []string `json:"roms"`
		NameBinding bool     `json:"name_binding,omitempty"`
	}
	decoder := json.NewDecoder(os.Stdin)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return err
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF || input.Root == "" || !exporter.LookupSystem(input.System) && !(input.NameBinding && input.System == "catalog") {
		return fmt.Errorf("invalid native observation request")
	}
	output := struct {
		Inputs  []exporter.ObservedInput `json:"inputs"`
		Skipped []map[string]string      `json:"skipped"`
	}{Inputs: []exporter.ObservedInput{}, Skipped: []map[string]string{}}
	seen := map[string]bool{}
	for i, rom := range input.ROMs {
		if seen[rom] {
			return fmt.Errorf("duplicate native input")
		}
		seen[rom] = true
		fmt.Fprintf(os.Stderr, "Inspect native ROM %d/%d | %s\n", i+1, len(input.ROMs), rom)
		observed, err := exporter.ObserveInput(input.Root, input.System, rom)
		if err != nil && input.NameBinding {
			observed, err = exporter.ObserveCatalogInput(input.Root, rom)
		}
		if err != nil {
			output.Skipped = append(output.Skipped, map[string]string{"rom": rom, "status": "unsupported", "reason": err.Error()})
			continue
		}
		output.Inputs = append(output.Inputs, observed)
	}
	return json.NewEncoder(os.Stdout).Encode(output)
}
