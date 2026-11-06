package main

import (
	"strings"
)

// Hold one root lock for actual CLI catalog writes, including index publication.
// Read-only previews and frontend exports never acquire it.
func catalogWriterRoot(args []string) (string, bool) {
	if len(args) == 0 {
		return "", false
	}
	switch args[0] {
	case "catalog", "fetch", "dat", "gamelist", "scummvm", "launchbox":
	default:
		return "", false
	}
	// Backup management owns its lock for both listing and deletion.
	if len(args) > 1 && args[0] == "catalog" && args[1] == "backups" {
		return "", false
	}
	root := "."
	apply := false
	for i, arg := range args {
		if isHelpArg(arg) {
			return "", false
		}
		if arg == "--apply" || arg == "--apply=true" || arg == "--interactive" || arg == "--interactive=true" {
			apply = true
		}
		if strings.HasPrefix(arg, "--data-root=") {
			root = strings.TrimPrefix(arg, "--data-root=")
		}
		if arg == "--data-root" && i+1 < len(args) {
			root = args[i+1]
		}
	}
	return root, apply
}
