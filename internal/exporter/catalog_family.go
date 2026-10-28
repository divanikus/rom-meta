package exporter

// Dataset families describe hardware compatibility, not interchangeable editions.
// Acquisition remains scoped to each DAT; only consumer recognition is combined.
func catalogFamily(name string) string {
	switch name {
	case "Nintendo - Nintendo DSi (Digital) (CDN)", "Nintendo - Nintendo DSi (Digital) (CDN) (Decrypted)":
		return "Nintendo - Nintendo DSi"
	case "Nintendo - Wii (Digital) (CDN)":
		return "Nintendo - Wii"
	case "Nintendo - Wii U (Digital) (CDN)":
		return "Nintendo - Wii U"
	case "Sony - PlayStation 3 (PSN) (Content)":
		return "Sony - PlayStation 3"
	case "Sony - PlayStation Portable (PSN) (Minis) (Encrypted)":
		return "Sony - PlayStation Portable"
	}
	return name
}

func (catalog *LookupCatalog) groupDatasets() {
	groups := map[string][]int{}
	for i, p := range catalog.Platforms {
		if p.family != "" {
			groups[p.family] = append(groups[p.family], i)
		}
	}
	for family, members := range groups {
		anchor, bases := members[0], 0
		distinct := false
		for _, i := range members {
			p := catalog.Platforms[i]
			if p.header == family {
				anchor = i
				bases++
			} else {
				distinct = true
			}
		}
		// Duplicate headers retain the existing ambiguity; never union arbitrary DATs.
		if !distinct || bases > 1 {
			continue
		}
		p := &catalog.Platforms[anchor]
		for _, i := range members {
			p.Catalogs = append(p.Catalogs, catalog.Platforms[i].Platform)
			if i != anchor {
				catalog.Platforms[i].ExportSystems = nil
			}
		}
		p.Aliases = append(p.Aliases, family)
	}
}

func (catalog *LookupCatalog) scope(platform string) []string {
	for _, p := range catalog.Platforms {
		if p.Platform == platform && len(p.Catalogs) > 0 {
			return p.Catalogs
		}
	}
	return []string{platform}
}

func mergeCatalogIndex(dst, src *catalogIndex) {
	if len(src.ByCHD) > 0 && dst.ByCHD == nil {
		dst.ByCHD = map[string][]fingerprint{}
	}
	for k, v := range src.BySize {
		dst.BySize[k] = append(dst.BySize[k], v...)
	}
	for k, v := range src.ByCHD {
		dst.ByCHD[k] = append(dst.ByCHD[k], v...)
	}
	for k, v := range src.ByName {
		dst.ByName[k] = append(dst.ByName[k], v...)
	}
	if len(src.ByCompleteName) > 0 && dst.ByCompleteName == nil {
		dst.ByCompleteName = map[string][]fingerprint{}
	}
	for k, v := range src.ByCompleteName {
		dst.ByCompleteName[k] = append(dst.ByCompleteName[k], v...)
	}
	if len(src.ByCompleteGameName) > 0 && dst.ByCompleteGameName == nil {
		dst.ByCompleteGameName = map[string][]*game{}
	}
	for k, v := range src.ByCompleteGameName {
		dst.ByCompleteGameName[k] = append(dst.ByCompleteGameName[k], v...)
	}
	for k, v := range src.GamesByName {
		dst.GamesByName[k] = append(dst.GamesByName[k], v...)
	}
}
