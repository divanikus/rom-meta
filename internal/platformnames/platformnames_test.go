package platformnames

import "testing"

func TestDefaultAliasesLoadFromYAML(t *testing.T) {
	loaded := loadAliases(defaultAliasYAML)
	if len(loaded) < 40 {
		t.Fatalf("expected embedded aliases.yaml to load many aliases, got %d", len(loaded))
	}
	if got := loaded[key("Sega - Mega Drive - Genesis")].LaunchBox; got != "Sega Genesis" {
		t.Fatalf("embedded alias did not load source field, got %q", got)
	}
}

func TestForSourceKeepsNintendo64Convention(t *testing.T) {
	for _, source := range []string{SourceEmuMovies, SourceIGDB, SourceLaunchBox, SourceOpenVGDB, SourceScreenScraper} {
		if got := ForSource("Nintendo - Nintendo 64", source); got != "Nintendo 64" {
			t.Fatalf("unexpected Nintendo 64 name for %s: %q", source, got)
		}
	}
}

func TestForSourceMapsRedumpDiscPlatformConventions(t *testing.T) {
	tests := []struct {
		platform string
		source   string
		want     string
	}{
		{"Fujitsu - FM Towns series", SourceIGDB, "FM Towns"},
		{"Fujitsu - FM Towns series", SourceLaunchBox, "Fujitsu FM Towns Marty"},
		{"Fujitsu - FM Towns series", SourceScreenScraper, "FM Towns"},
		{"Fujitsu - FM-Towns", SourceIGDB, "FM Towns"},
		{"SNK - Neo Geo CD", SourceLaunchBox, "SNK Neo Geo CD"},
		{"SNK - Neo Geo CD", SourceEmuMovies, "SNK Neo Geo CD"},
		{"SNK - Neo Geo CD", SourceIGDB, "Neo Geo CD"},
		{"NEC - PC-FX & PC-FXGA", SourceLaunchBox, "NEC PC-FX"},
		{"NEC - PC-FX & PC-FXGA", SourceEmuMovies, "NEC PC-FX"},
		{"NEC - PC-FX & PC-FXGA", SourceIGDB, "PC-FX"},
		{"NEC - PC Engine - TurboGrafx-16", SourceLaunchBox, "NEC TurboGrafx-16"},
		{"NEC - PC Engine - TurboGrafx-16", SourceEmuMovies, "NEC TurboGrafx-16"},
		{"NEC - PC Engine - TurboGrafx-16", SourceIGDB, "TurboGrafx-16/PC Engine"},
		{"NEC - PC Engine - TurboGrafx-16", SourceOpenVGDB, "NEC PC Engine/TurboGrafx-16"},
		{"Atari - Jaguar CD Interactive Multimedia System", SourceLaunchBox, "Atari Jaguar CD"},
		{"Nintendo - Nintendo 64 (BigEndian)", SourceLaunchBox, "Nintendo 64"},
		{"Nintendo - Nintendo Entertainment System (Headered)", SourceIGDB, "Nintendo Entertainment System"},
		{"Nintendo - Family Computer Disk System (FDS)", SourceOpenVGDB, "Nintendo Famicom Disk System"},
		{"Nintendo - Super Nintendo Entertainment System", SourceScreenScraper, "Super Nintendo"},
		{"Nintendo - Super Nintendo Entertainment System", SourceOpenVGDB, "Nintendo Super Nintendo Entertainment System"},
		{"Nintendo - Game Boy Advance", SourceLaunchBox, "Nintendo Game Boy Advance"},
		{"Sega - Mega Drive - Genesis", SourceLaunchBox, "Sega Genesis"},
		{"Sega - Mega Drive - Genesis", SourceIGDB, "Sega Mega Drive/Genesis"},
		{"Sega - Mega Drive - Genesis", SourceOpenVGDB, "Sega Genesis/Mega Drive"},
		{"Sega - Master System - Mark III", SourceIGDB, "Sega Master System/Mark III"},
		{"Sega - Master System - Mark III", SourceOpenVGDB, "Sega Master System"},
		{"NEC - PC Engine CD & TurboGrafx CD", SourceLaunchBox, "NEC TurboGrafx-CD"},
		{"NEC - PC Engine CD & TurboGrafx CD", SourceIGDB, "Turbografx-16/PC Engine CD"},
		{"NEC - PC Engine CD & TurboGrafx CD", SourceOpenVGDB, "NEC PC Engine CD/TurboGrafx-CD"},
		{"Sega - 32X", SourceOpenVGDB, "Sega 32X"},
		{"Sega - Mega CD & Sega CD", SourceOpenVGDB, "Sega CD/Mega-CD"},
		{"Sony - PlayStation Portable (PSN) (Minis) (Encrypted)", SourceLaunchBox, "Sony PSP Minis"},
		{"Sony - PlayStation Vita (PSN) (Content)", SourceIGDB, "PlayStation Vita"},
		{"Microsoft - Xbox 360", SourceLaunchBox, "Microsoft Xbox 360"},
		{"SNK - NeoGeo Pocket Color", SourceLaunchBox, "SNK Neo Geo Pocket Color"},
	}
	for _, test := range tests {
		if got := ForSource(test.platform, test.source); got != test.want {
			t.Fatalf("ForSource(%q, %q) = %q, want %q", test.platform, test.source, got, test.want)
		}
	}
}

func TestAliasesResolveFromDisplayAndSourceNames(t *testing.T) {
	tests := []struct {
		platform string
		source   string
		want     string
	}{
		{"PC Engine / TurboGrafx-16", SourceLaunchBox, "NEC TurboGrafx-16"},
		{"NEC TurboGrafx-16", SourceIGDB, "TurboGrafx-16/PC Engine"},
		{"Mega Drive / Genesis", SourceLaunchBox, "Sega Genesis"},
		{"Sega Genesis", SourceIGDB, "Sega Mega Drive/Genesis"},
		{"PlayStation Portable", SourceLaunchBox, "Sony PSP"},
		{"Sony PSP", SourceIGDB, "PlayStation Portable"},
		{"Nintendo 3DS", SourceIGDB, "Nintendo 3DS"},
	}

	for _, test := range tests {
		if got := ForSource(test.platform, test.source); got != test.want {
			t.Fatalf("ForSource(%q, %q) = %q, want %q", test.platform, test.source, got, test.want)
		}
	}
}

func TestResolveCLIUsesShortPlatformKeys(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"fmtowns", "Fujitsu - FM Towns series"},
		{"FM Towns", "Fujitsu - FM Towns series"},
		{"Fujitsu - FM-Towns", "Fujitsu - FM Towns series"},
		{"Fujitsu - FM Towns", "Fujitsu - FM Towns series"},
		{"n64", "Nintendo - Nintendo 64"},
		{"nes", "Nintendo - Nintendo Entertainment System"},
		{"snes", "Nintendo - Super Nintendo Entertainment System"},
		{"segacd", "Sega - Mega CD & Sega CD"},
		{"Sega CD", "Sega - Mega CD & Sega CD"},
		{"megadrive", "Sega - Mega Drive - Genesis"},
		{"genesis", "Sega - Mega Drive - Genesis"},
		{"sms", "Sega - Master System - Mark III"},
		{"mastersystem", "Sega - Master System - Mark III"},
		{"pcengine", "NEC - PC Engine - TurboGrafx-16"},
		{"tg16", "NEC - PC Engine - TurboGrafx-16"},
		{"Nintendo - Nintendo 64 (BigEndian)", "Nintendo - Nintendo 64"},
		{"Nintendo - Nintendo 64", "Nintendo - Nintendo 64"},
		{"Custom Platform", "Custom Platform"},
		{"atari2600", "Atari - Atari 2600"},
		{"atari5200", "Atari - Atari 5200"},
		{"ws", "Bandai - WonderSwan"},
		{"wsc", "Bandai - WonderSwan Color"},
		{"cd32", "Commodore - Amiga CD32"},
		{"64dd", "Nintendo - Nintendo 64DD"},
		{"vb", "Nintendo - Virtual Boy"},
		{"beena", "Sega - Beena"},
		{"zeebo", "Zeebo - Zeebo"},
	}

	for _, test := range tests {
		if got := ResolveCLI(test.input); got != test.want {
			t.Fatalf("ResolveCLI(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestResolveCLIWithAliasesUsesLocalOverrides(t *testing.T) {
	aliases := map[string]string{
		"sega cd": "segacd",
		"my-pce":  "NEC - PC Engine - TurboGrafx-16",
	}

	tests := []struct {
		input string
		want  string
	}{
		{"Sega-CD", "Sega - Mega CD & Sega CD"},
		{"my pce", "NEC - PC Engine - TurboGrafx-16"},
	}

	for _, test := range tests {
		if got := ResolveCLIWithAliases(test.input, aliases); got != test.want {
			t.Fatalf("ResolveCLIWithAliases(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestRegistryParsesCompactPlatformConfig(t *testing.T) {
	registry, err := ParseRegistryYAML(`
"Sega - Mega CD & Sega CD":
  aliases: [segacd, "sega cd", "mega cd"]
  sources:
    redump: "Sega - Mega CD & Sega CD"
    es-de: segacd
    launchbox: "Sega CD"
    screenscraper: { id: 20, name: "Mega-CD" }
`)
	if err != nil {
		t.Fatalf("ParseRegistryYAML returned error: %v", err)
	}

	for _, input := range []string{"segacd", "Sega CD", "Mega-CD", "Sega - Mega CD & Sega CD"} {
		if got := registry.ResolveCLI(input); got != "Sega - Mega CD & Sega CD" {
			t.Fatalf("ResolveCLI(%q) = %q", input, got)
		}
	}
	if got := registry.ForSource("segacd", SourceLaunchBox); got != "Sega CD" {
		t.Fatalf("ForSource launchbox = %q", got)
	}
	if id, ok := registry.SourceID("Sega CD", SourceScreenScraper); !ok || id != "20" {
		t.Fatalf("SourceID screenscraper = %q, %t", id, ok)
	}
}

func TestDefaultRegistryIncludesEmbeddedAliases(t *testing.T) {
	registry := DefaultRegistry()

	if got := registry.ResolveCLI("segacd"); got != "Sega - Mega CD & Sega CD" {
		t.Fatalf("default ResolveCLI(segacd) = %q", got)
	}
	if got := registry.ForSource("pcengine", SourceIGDB); got != "TurboGrafx-16/PC Engine" {
		t.Fatalf("default ForSource(pcengine, igdb) = %q", got)
	}
	for _, test := range []struct {
		platform string
		want     string
	}{
		{"fmtowns", "253"},
		{"snes", "4"},
		{"n64", "14"},
		{"segacd", "20"},
		{"psp", "61"},
	} {
		if got, ok := registry.SourceID(test.platform, SourceScreenScraper); !ok || got != test.want {
			t.Fatalf("default SourceID(%q, screenscraper) = %q, %t; want %q, true", test.platform, got, ok, test.want)
		}
	}
}

func TestResolveForSourcePrefersAvailableSourceName(t *testing.T) {
	available := []string{"NEC TurboGrafx-16", "Nintendo 64"}

	got := ResolveForSource("PC Engine / TurboGrafx-16", SourceLaunchBox, available)

	if got != "NEC TurboGrafx-16" {
		t.Fatalf("ResolveForSource returned %q, want source-provided name", got)
	}
}

func TestMatchForSourceSkipsAmbiguousAvailableNames(t *testing.T) {
	_, ok := MatchForSource("Example Platform", SourceLaunchBox, []string{"Example Platform", "Example-Platform"})

	if ok {
		t.Fatalf("expected ambiguous normalized source names not to match")
	}
}

func TestDisplayUsesFriendlyAlias(t *testing.T) {
	if got := Display("NEC - PC-FX & PC-FXGA"); got != "PC-FX" {
		t.Fatalf("unexpected PC-FX display name: %q", got)
	}
}

func TestFMTownsMappingsAllowLocalRegistryOverrides(t *testing.T) {
	registry := DefaultRegistry()
	local, err := ParseRegistryYAML(`
"Fujitsu - FM Towns series":
  sources:
    igdb: "Custom Towns"
    screenscraper: { id: 999, name: "Custom Towns" }
`)
	if err != nil {
		t.Fatal(err)
	}
	registry.Merge(local)
	for _, alias := range []string{"fmtowns", "FM Towns", "Fujitsu - FM-Towns"} {
		if got := registry.ForSource(alias, SourceIGDB); got != "Custom Towns" {
			t.Fatalf("local mapping for %q = %q", alias, got)
		}
		if got, ok := registry.SourceID(alias, SourceScreenScraper); !ok || got != "999" {
			t.Fatalf("local system for %q = %q, %t", alias, got, ok)
		}
		if got := registry.ForSource(alias, SourceLaunchBox); got != "Fujitsu FM Towns Marty" {
			t.Fatalf("unmodified provider mapping for %q = %q", alias, got)
		}
	}
}

func TestDigitalCatalogMappingsStayDistinctFromDiscs(t *testing.T) {
	r := DefaultRegistry()
	for _, tc := range []struct{ alias, platform, video, id string }{
		{"wii", "Nintendo - Wii", "Nintendo Wii", "16"},
		{"wiidigital", "Nintendo - Wii (Digital) (CDN)", "Nintendo WiiWare", "16"},
		{"wiiu", "Nintendo - Wii U", "Nintendo Wii U", "18"},
		{"wiiueshop", "Nintendo - Wii U (Digital) (CDN)", "Nintendo Wii U eShop", "18"},
		{"ps3", "Sony - PlayStation 3", "Sony PlayStation 3", "59"},
		{"ps3psn", "Sony - PlayStation 3 (PSN) (Content)", "Sony Playstation 3 - PSN", "59"},
	} {
		if got := r.ResolveCLI(tc.alias); got != tc.platform {
			t.Errorf("%s resolved to %s", tc.alias, got)
		}
		if got := r.ForSource(tc.platform, SourceEmuMovies); got != tc.video {
			t.Errorf("%s video source %s", tc.platform, got)
		}
		if id, ok := r.SourceID(tc.platform, SourceScreenScraper); !ok || id != tc.id {
			t.Errorf("%s SS id %s", tc.platform, id)
		}
	}
}

func TestDigitalNintendoIGDBUsesAvailableHardwareNames(t *testing.T) {
	r := DefaultRegistry()
	available := []string{"Wii", "Wii U", "PlayStation 3"}
	for _, tc := range []struct{ alias, want string }{
		{"wiidigital", "Wii"},
		{"wiiueshop", "Wii U"},
	} {
		t.Run(tc.alias, func(t *testing.T) {
			if got, ok := r.MatchForSource(tc.alias, SourceIGDB, available); !ok || got != tc.want {
				t.Fatalf("digital platform = %q, %v; want %q, true", got, ok, tc.want)
			}
			if got := r.ForSource(tc.alias, SourceIGDB); got != tc.want {
				t.Fatalf("source mapping = %q; want %q", got, tc.want)
			}
		})
	}
}

func TestExplicitAliasOutranksSharedProviderName(t *testing.T) {
	r, err := ParseRegistryYAML(`
"Disc Catalog":
  aliases: [hardware, ambiguous]
  sources:
    igdb: "Shared Provider"
"Digital Catalog":
  aliases: [digital, ambiguous]
  sources:
    igdb: "hardware"
"Third Catalog":
  sources:
    igdb: "ambiguous"
`)
	if err != nil {
		t.Fatal(err)
	}
	if got := r.ResolveCLI("hardware"); got != "Disc Catalog" {
		t.Fatalf("explicit alias = %q", got)
	}
	if got := r.ResolveCLI("digital"); got != "Digital Catalog" {
		t.Fatalf("digital alias = %q", got)
	}
	if got := r.ResolveCLI("ambiguous"); got != "ambiguous" {
		t.Fatalf("ambiguous explicit alias resolved to %q", got)
	}
	if got := r.ResolveCLI("Shared Provider"); got != "Disc Catalog" {
		t.Fatalf("unique provider name = %q", got)
	}
}
