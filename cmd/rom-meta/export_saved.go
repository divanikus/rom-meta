package main

import (
	"flag"
	"fmt"
	"rommeta/internal/exporter"
	"rommeta/internal/exportsettings"
)

func exportFlagSet(flags *flag.FlagSet, name string) bool {
	found := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}
func listExportSettings(root string, profiles bool) error {
	if profiles {
		doc, _, _, err := exportsettings.ReadProfiles(root)
		if err != nil {
			return err
		}
		if len(doc.Profiles) == 0 {
			fmt.Println("No saved export profiles. Save one in the web export form.")
			return nil
		}
		for _, p := range doc.Profiles {
			fmt.Printf("%s\t%s\t%s\n", p.Name, p.Target, p.ID)
		}
	} else {
		doc, _, _, err := exportsettings.ReadDestinations(root)
		if err != nil {
			return err
		}
		if len(doc.Destinations) == 0 {
			fmt.Println("No saved export destinations. Save one in the web export form.")
			return nil
		}
		for _, d := range doc.Destinations {
			fmt.Printf("%s\t%s\t%s\t%s\n", d.Name, d.Target, d.Path, d.ID)
		}
	}
	return nil
}
func applySavedExportSettings(flags *flag.FlagSet, options *exporter.Options, target, destination, mediaRoot *string, all *bool, destinationName, profileName string) error {
	if profileName != "" && exportFlagSet(flags, "profile") {
		return fmt.Errorf("choose --profile or --profile-name, not both")
	}
	if destinationName != "" {
		d, err := exportsettings.FindDestination(options.DataRoot, destinationName)
		if err != nil {
			return err
		}
		if !exportFlagSet(flags, "target") {
			*target = d.Target
		}
		if !exportFlagSet(flags, "destination") {
			*destination = d.Path
		}
		if !exportFlagSet(flags, "media-root") {
			*mediaRoot = d.MediaRoot
		}
		if !exportFlagSet(flags, "rom-root") {
			options.ROMRoot = d.ROMRoot
		}
		if !exportFlagSet(flags, "platform") && !exportFlagSet(flags, "platforms") && !exportFlagSet(flags, "all") {
			options.Platform = d.Platform
			*all = d.All
		}
		if profileName == "" && !exportFlagSet(flags, "profile") {
			if err = exportsettings.ResolveDestinationProfile(options.DataRoot, &d); err != nil {
				return err
			}
			options.ProfileContent = d.ProfileYAML
		}
	}
	if profileName != "" {
		p, err := exportsettings.FindProfile(options.DataRoot, profileName)
		if err != nil {
			return err
		}
		options.ProfileContent = p.ProfileYAML
		if destinationName == "" && !exportFlagSet(flags, "target") {
			*target = p.Target
		}
	}
	if options.ProfileContent != "" {
		p, err := exporter.ExportProfile(options.DataRoot, options.ProfileContent)
		if err != nil {
			return fmt.Errorf("saved profile: %w", err)
		}
		normalized, err := exporter.NormalizeTarget(*target)
		if err != nil {
			return err
		}
		if err = exporter.ValidateTargetMedia(p, normalized); err != nil {
			return err
		}
	}
	return nil
}
