package main

import "fmt"

// The server example calls these JSON protocol helpers. Native publication lives
// in that version-specific example; the former stock-API uploader is retired.
func runRomM(args []string) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		fmt.Println("RomM server example protocol: inspect-inputs and project-snapshot read JSON from stdin. For import setup and plan/apply/check, see examples/romm-importer/README.md.")
		return nil
	}
	switch args[0] {
	case "inspect-inputs":
		return runRomMInspect(args[1:])
	case "project-snapshot":
		return runRomMProject(args[1:])
	default:
		return fmt.Errorf("romm command is inspect-inputs or project-snapshot; use the server example for import: examples/romm-importer/README.md")
	}
}
