package exporter

import "fmt"

// MediaRole describes a supported frontend destination, not an available asset.
type MediaRole struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// ExportMediaRoles lists roles understood by the selected native adapter.
func ExportMediaRoles(target string) []MediaRole {
	common := []MediaRole{{"cover", "Box front"}, {"back-cover", "Box back"}, {"box-3d", "Box 3D"}, {"screenshot", "Gameplay screenshot"}, {"title-screen", "Title screen"}, {"logo", "Clear logo"}, {"fanart", "Background"}, {"video", "Video"}, {"manual", "Manual (PDF)"}}
	if target == "launchbox" {
		common = append(common, MediaRole{"box-spine", "Box spine"}, MediaRole{"cart-front", "Cartridge front"}, MediaRole{"cart-back", "Cartridge back"}, MediaRole{"cart-3d", "Cartridge 3D"}, MediaRole{"disc", "Disc"}, MediaRole{"banner", "Banner"}, MediaRole{"icon", "Icon"})
	}
	return common
}

// ValidateTargetMedia keeps the shared selection wire format neutral while
// preventing a native adapter from silently dropping unsupported destinations.
func ValidateTargetMedia(profile Profile, target string) error {
	allowed := map[string]bool{}
	for _, role := range ExportMediaRoles(target) {
		allowed[role.ID] = true
	}
	for role := range profile.Selection.Media {
		if !allowed[role] {
			return fmt.Errorf("media role %s is not supported by %s", role, target)
		}
	}
	return nil
}
