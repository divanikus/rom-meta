package exporter

import (
	"fmt"
	"io"
	"reflect"
	"rommeta/internal/frontenddata"
	"strings"
)

// DestinationData hands native fields to the merge adapter without XML or a JSON manifest.
func DestinationData(plans []*Plan) (*frontenddata.Library, map[string]PreviewFile, error) {
	if len(plans) == 0 {
		return nil, nil, fmt.Errorf("no systems with selected inputs")
	}
	library := &frontenddata.Library{Selection: frontenddata.Selection{Schema: 1, Target: plans[0].Target}, Native: map[string][]frontenddata.Entry{}}
	files := map[string]PreviewFile{}
	for _, plan := range plans {
		if plan.Target != library.Selection.Target {
			return nil, nil, fmt.Errorf("mixed native targets")
		}
		selection := frontenddata.Selection{System: plan.System, DataRoot: plan.DataRoot, ROMRoot: plan.ROMRoot}
		for _, r := range plan.Records {
			input := frontenddata.Input{ROM: r.ROM, LaunchFile: r.LaunchFile, Game: r.Game, Status: r.Status, Hidden: r.Hidden, Media: map[string]frontenddata.Media{}}
			for role, media := range r.Media {
				input.Media[role] = frontenddata.Media{OutputPath: media.OutputPath}
				file := PreviewFile{SourcePath: media.SourcePath, SHA256: media.SHA256, Size: media.Size}
				if plan.SourceAPI == "" {
					selected := media
					file.Open = func() (io.ReadCloser, error) { return openMedia(plan, selected) }
				}
				files[media.OutputPath] = file
			}
			selection.Records = append(selection.Records, input)
		}
		path := outputXMLPath(plan)
		if plan.Target == "launchbox" {
			if plan.LaunchBox == nil {
				projection, err := buildLaunchBoxProjection(plan)
				if err != nil {
					return nil, nil, err
				}
				plan.LaunchBox = projection
			}
			if _, ok := library.Native[path]; !ok {
				library.Native[path] = []frontenddata.Entry{}
			}
			list, err := launchBoxRecords(plan)
			if err != nil {
				return nil, nil, err
			}
			for _, g := range list.Games {
				library.Native[path] = append(library.Native[path], nativeEntry("Game", g))
			}
			for _, v := range list.Versions {
				library.Native[path] = append(library.Native[path], nativeEntry("AdditionalApplication", v))
			}
			selection.LaunchBox = &frontenddata.Groups{}
			for _, g := range plan.LaunchBox.Groups {
				group := frontenddata.Group{ID: g.ID, Game: g.Game}
				for _, v := range g.Versions {
					group.Versions = append(group.Versions, frontenddata.Version{Records: v.Records})
				}
				selection.LaunchBox.Groups = append(selection.LaunchBox.Groups, group)
			}
		} else {
			if _, exists := library.Native[path]; exists {
				return nil, nil, fmt.Errorf("duplicate target platform output: %s", path)
			}
			library.Native[path] = []frontenddata.Entry{}
			list := gamelistRecords(plan)
			for _, g := range list.Games {
				library.Native[path] = append(library.Native[path], nativeEntry("game", g))
			}
			for _, f := range list.Folders {
				library.Native[path] = append(library.Native[path], nativeEntry("folder", f))
			}
		}
		playlists, err := launchBoxPlaylistData(plan)
		if err != nil {
			return nil, nil, err
		}
		for name, body := range playlists {
			files[name] = PreviewFile{Data: body}
		}
		library.Selection.Systems = append(library.Selection.Systems, selection)
	}
	return library, files, nil
}

// Read the existing native struct tags, keeping one field contract for packages and direct export.
func nativeEntry(name string, record any) frontenddata.Entry {
	result := frontenddata.Entry{Name: name, Fields: map[string]string{}}
	value := reflect.ValueOf(record)
	typ := value.Type()
	for i := 0; i < value.NumField(); i++ {
		tag := strings.Split(typ.Field(i).Tag.Get("xml"), ",")
		field := value.Field(i)
		if len(tag) > 1 && tag[1] == "omitempty" && field.IsZero() {
			continue
		}
		result.Fields[tag[0]] = fmt.Sprint(field.Interface())
	}
	return result
}
