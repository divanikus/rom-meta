package frontend

// Repair only obsolete, unchanged input placeholders created by RomMeta. The
// current manifest supplies the game association; titles never establish it.
import (
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type consolidationManifest struct {
	ROMRoot string `json:"rom_root"`
	Records []struct {
		ROM, Game, Status string
		LaunchFile        string `json:"launch_file"`
		Hidden            bool
	} `json:"records"`
	LaunchBox *struct {
		Groups []struct {
			ID, Game string
			Versions []struct{ Records []int }
		}
	} `json:"launchbox"`
	Systems []consolidationManifest `json:"systems"`
}

func generatedLaunchBoxID(identity string) string {
	h := sha1.New()
	h.Write([]byte{0x6b, 0xa7, 0xb8, 0x11, 0x9d, 0xad, 0x11, 0xd1, 0x80, 0xb4, 0x00, 0xc0, 0x4f, 0xd4, 0x30, 0xc8})
	h.Write([]byte("rommeta:launchbox:" + identity))
	b := h.Sum(nil)[:16]
	b[6] = (b[6] & 0x0f) | 0x50
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func placeholderTitle(raw string) string {
	raw = strings.ReplaceAll(raw, "\\", "/")
	return strings.TrimSuffix(path.Base(raw), path.Ext(raw))
}

// LaunchBox writes these defaults when it saves an otherwise untouched card.
// Any nonempty unknown field or owner state prevents automatic retirement.
var placeholderDefaults = map[string]string{
	"Installed": "true", "InstalledOverride": "true", "InstalledStateVersion": "1",
	"InstallationState": "Unknown", "InstallationStateManagement": "Default",
	"StorefrontIdentityVersion": "0", "StorefrontProviderDataVersion": "0",
	"StorefrontAchievementRefreshVersion": "0", "StorefrontAchievementProgressVersion": "1",
	"StorefrontAchievementAvailability": "Unknown", "StorefrontAchievementAvailabilityMigrationVersion": "2",
	"Emulator": zeroGUID, "EmulatorId": zeroGUID, "Section": "Version", "UseEmulator": "true",
	"Priority": "0", "StartupScreenPostLaunchDisplayTime": "2000",
}
var placeholderZeroFields = strings.Fields(`Completed Favorite ScummVMAspectCorrection ScummVMFullscreen StarRatingFloat StarRating CommunityStarRating CommunityStarRatingTotalVotes UseDosBox UseScummVM PlayCount PlayTime Portable Hide Broken UseStartupScreen HideAllNonExclusiveFullscreenWindows StartupLoadDelay MonitorStartupShutdownWithProcess HideMouseCursorInGame DisableShutdownScreen AggressiveWindowHiding OverrideDefaultStartupScreenSettings UsePauseScreen OverrideDefaultPauseScreenSettings SuspendProcessOnPause ForcefulPauseScreenActivation RetroAchievementsBeatenSoftcore RetroAchievementsBeatenHardcore HasCloudSynced AutoRunAfter AutoRunBefore WaitForExit SideA SideB`)
var placeholderMissingFields = strings.Fields(`MissingVideo MissingBoxFrontImage MissingScreenshotImage MissingMarqueeImage MissingClearLogoImage MissingBackgroundImage MissingBox3dImage MissingCartImage MissingCart3dImage MissingManual MissingBannerImage MissingMusic`)

func untouchedPlaceholder(n native, platformName, launch, sourceName string, app bool) bool {
	d := xml.NewDecoder(bytes.NewReader(n.node.raw))
	for {
		token, err := d.Token()
		if err != nil {
			break
		}
		if e, ok := token.(xml.StartElement); ok && len(e.Attr) > 0 {
			return false
		}
	}
	doc, err := parse(n.node.raw, n.node.name)
	if err != nil {
		return false
	}
	for _, e := range doc.children {
		child, err := parse(e.raw, e.name)
		if err != nil || len(child.children) > 0 {
			return false
		}
	}
	expected := map[string]string{"ApplicationPath": launch, "Platform": platformName, "Title": placeholderTitle(launch), "Name": placeholderTitle(launch), "Version": placeholderTitle(launch)}
	for k, v := range placeholderDefaults {
		expected[k] = v
	}
	for _, k := range placeholderZeroFields {
		expected[k] = "zero"
	}
	for _, k := range placeholderMissingFields {
		expected[k] = "bool"
	}
	for k, v := range n.fields {
		if strings.TrimSpace(v) == "" || k == "ID" || app && (k == "Id" || k == "GameID") {
			continue
		}
		if (k == "DateAdded" || k == "DateModified" || k == "ReleaseDate") && strings.HasPrefix(v, "0001-01-01T00:00:00") {
			continue
		}
		want, ok := expected[k]
		if !ok {
			return false
		}
		if want == "zero" && (v == "0" || v == "false") || want == "bool" && (v == "true" || v == "false") {
			continue
		}
		if k == "ApplicationPath" {
			if keyPath("", v) == keyPath("", launch) {
				continue
			}
			return false
		}
		if (k == "Title" || k == "Name" || k == "Version") && v == strings.TrimSuffix(sourceName, path.Ext(sourceName)) {
			continue
		}
		if v != want {
			return false
		}
	}
	return true
}

func (p *Plan) consolidatePlaceholders(platformName string, games map[string]native, library *document, state identityMap) (*document, map[string]string, map[string]string, error) {
	body, err := p.read(p.Package, "manifest.json", false)
	if err != nil {
		return nil, nil, nil, err
	}
	var manifest consolidationManifest
	if err = json.Unmarshal(body, &manifest); err != nil {
		return nil, nil, nil, err
	}
	// Exact source launch paths must belong to exactly one current catalog card.
	byPath := map[string]string{}
	sourceNames := map[string]string{}
	var visit func(consolidationManifest)
	visit = func(m consolidationManifest) {
		for _, child := range m.Systems {
			visit(child)
		}
		if m.LaunchBox == nil {
			return
		}
		for _, g := range m.LaunchBox.Groups {
			incoming := id(g.ID)
			if g.Game == "" || generatedLaunchBoxID(platformName+"\ncard:"+g.Game) != incoming {
				continue
			}
			if _, ok := games[incoming]; !ok {
				continue
			}
			for _, v := range g.Versions {
				for _, i := range v.Records {
					if i < 0 || i >= len(m.Records) {
						continue
					}
					r := m.Records[i]
					if r.Status != "matched" || r.Game != g.Game || r.Hidden {
						continue
					}
					launch := r.LaunchFile
					if launch == "" {
						launch = r.ROM
					}
					if m.ROMRoot == "" || launch == "" {
						continue
					}
					key := keyPath(m.ROMRoot, launch)
					sourceNames[key] = r.ROM
					if old, ok := byPath[key]; ok && old != incoming {
						byPath[key] = ""
					} else if !ok {
						byPath[key] = incoming
					}
				}
			}
		}
	}
	visit(manifest)
	existing, apps, err := records(library)
	if err != nil {
		return nil, nil, nil, err
	}
	retired := map[string]string{}
	problems := map[string]string{}
	for key, n := range existing {
		launch := n.fields["ApplicationPath"]
		incoming := byPath[keyPath(p.Root, launch)]
		if incoming == "" || key == incoming || id(state.Cards[platformName+"/"+incoming]) == key {
			continue
		}
		// Both the saved ownership and the original generated input ID are needed.
		if id(state.Cards[platformName+"/"+key]) != key || key != generatedLaunchBoxID(platformName+"\ninput:"+keyPath(p.Root, launch)) {
			continue
		}
		safe := untouchedPlaceholder(n, platformName, launch, sourceNames[keyPath(p.Root, launch)], false)
		for _, v := range apps {
			if id(v.fields["GameID"]) == key {
				if id(v.fields["Id"]) != generatedLaunchBoxID(platformName+"\nversion:"+keyPath(p.Root, launch)) || !untouchedPlaceholder(v, platformName, launch, sourceNames[keyPath(p.Root, launch)], true) {
					safe = false
				}
			}
		}
		if !safe {
			problems[incoming] = "previous RomMeta placeholder has owner changes; automatic consolidation would discard them"
			continue
		}
		retired[key] = incoming
	}
	remove := map[int][]byte{}
	for key, incoming := range retired {
		if problems[incoming] != "" {
			delete(retired, key)
			continue
		}
		remove[existing[key].index] = nil
		for _, v := range apps {
			if id(v.fields["GameID"]) == key {
				remove[v.index] = nil
			}
		}
	}
	if len(remove) == 0 {
		return library, retired, problems, nil
	}
	result, err := parse(library.edit(remove, nil), "LaunchBox")
	return result, retired, problems, err
}

// Playlist memberships follow the surviving native IDs; unrelated XML is kept.
func (p *Plan) remapPlaylistCards(retired map[string]string) error {
	if len(retired) == 0 {
		return nil
	}
	entries, err := os.ReadDir(filepath.Join(p.Root, "Data", "Playlists"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".xml") {
			continue
		}
		relative := "Data/Playlists/" + entry.Name()
		body, err := p.read(p.Root, relative, false)
		if err != nil {
			return err
		}
		doc, err := parse(body, "LaunchBox")
		if err != nil {
			return err
		}
		replacements := map[int][]byte{}
		for i, e := range doc.children {
			if e.name != "PlaylistGame" {
				continue
			}
			fields, err := e.fields()
			if err != nil {
				return err
			}
			patch := map[string]string{}
			for _, key := range []string{"GameId", "GameID"} {
				if target := retired[id(fields[key])]; target != "" {
					patch[key] = target
				}
			}
			if len(patch) > 0 {
				raw, err := setFields(e, patch)
				if err != nil {
					return err
				}
				replacements[i] = raw
			}
		}
		if len(replacements) > 0 {
			if _, err = p.write(relative, doc.edit(replacements, nil), ""); err != nil {
				return err
			}
		}
	}
	return nil
}
