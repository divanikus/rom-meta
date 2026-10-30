package main

import (
	"fmt"
	"strings"
)

// Compatibility alias; all new export/enrichment workflows use runExport.
func runLaunchBox(args []string) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		fmt.Println("Compatibility: rom-meta launchbox merge --package <export> --launchbox-root <installation> [--apply]\nUse rom-meta export --target launchbox --package <export> --destination <installation> instead.")
		return nil
	}
	if args[0] != "merge" {
		return fmt.Errorf("unknown launchbox command: %s", args[0])
	}
	forwarded := []string{"--target", "launchbox"}
	for _, arg := range args[1:] {
		switch {
		case arg == "--launchbox-root" || arg == "-launchbox-root":
			arg = "--destination"
		case strings.HasPrefix(arg, "--launchbox-root="):
			arg = "--destination=" + strings.TrimPrefix(arg, "--launchbox-root=")
		case strings.HasPrefix(arg, "-launchbox-root="):
			arg = "--destination=" + strings.TrimPrefix(arg, "-launchbox-root=")
		}
		forwarded = append(forwarded, arg)
	}
	return runExport(forwarded)
}
