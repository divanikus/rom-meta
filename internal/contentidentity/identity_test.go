package contentidentity

import "testing"

func TestContentBoundaryDoesNotDependOnGameNames(t *testing.T) {
	for _, title := range []string{"Orbit", "A Thousand Rooms", "Demo Racer", "星の物語"} {
		_, base := Analyze(title + " (USA)")
		for _, suffix := range []string{" (Japan) (En,Ja) (Rev 1)", " (Europe) (Disc 2)", " (World) [!]"} {
			_, other := Analyze(title + suffix)
			if base != other {
				t.Fatalf("transport qualifier changed content: %s %+v %+v", suffix, base, other)
			}
		}
		for _, suffix := range []string{" (Expansion)", " (DLC)", " (Bonus Disc)", " (Special Edition)", " (Unclassified Edition)", " [h1]", " - Director's Cut"} {
			_, other := Analyze(title + " (USA)" + suffix)
			if Boundary([]Profile{base}, []Profile{other}) == "" {
				t.Fatalf("lost content boundary: %s%s", title, suffix)
			}
		}
	}
}
func TestContentProfilesAreConservativeAndOrderIndependent(t *testing.T) {
	_, a := Analyze("Game (Japan) (Beta) (2000-06-xx)")
	_, b := Analyze("Game (2000-06-xx) (Beta) (USA)", "beta")
	_, c := Analyze("Game (USA) (Beta) (2000-08-xx)")
	if a != b || a == c {
		t.Fatal(a, b, c)
	}
	_, retail := Analyze("Demo Racer (USA)")
	if retail.Kind != "retail" {
		t.Fatal(retail)
	}
	if Boundary([]Profile{a, retail}, []Profile{c}) != "" {
		t.Fatal("release stages became game boundaries")
	}
}

func TestAdditionalDATTrialAndPayloadQualifiers(t *testing.T) {
	_, retail := Analyze("Game (Europe) (En,Fr,Cs,Hu,Lt,Sk)")
	if retail.Kind != "retail" || retail.Variant != "" {
		t.Fatal("languages became editions", retail)
	}
	_, a := Analyze("Game (USA) (Layer 0) (Beta) (2006-10-02)")
	_, b := Analyze("Game (USA) (Layer 1) (Beta) (2006-10-02)")
	if a != b {
		t.Fatal("payload layers became different content", a, b)
	}
	for _, qualifier := range []string{"Trade Demo", "Tentou-you Taikenban", "Tokubetsu Taikenban", "Cheheompan"} {
		_, p := Analyze("Game (Japan) (" + qualifier + ")")
		if p.Kind != "demo" {
			t.Fatal(qualifier, p)
		}
	}
	for _, qualifier := range []string{"Sample Movie Disc", "Premium Pack Bonus Disc", "Trailer Disc"} {
		_, p := Analyze("Game (Japan) (" + qualifier + ")")
		if p.Kind != "supplement" {
			t.Fatal(qualifier, p)
		}
	}
}

func TestDevelopmentStagesPreserveBuildsInsideGame(t *testing.T) {
	_, retail := Analyze("Orbit (Japan)")
	for _, field := range []string{"Demo", "Trial", "Beta 2", "Proto", "Sample", "Preview", "Debug", "Promo", "Promotional", "Tentou-you Taikenban", "Trade Demo", "Cheheompan"} {
		title, p := Analyze("Orbit (USA) (" + field + ") (2001-02-03)")
		if title != "Orbit" || !p.Development() || p.Build == "" || Boundary([]Profile{retail, p}) != "" {
			t.Fatalf("%s: %s %+v", field, title, p)
		}
		_, edition := Analyze("Orbit (USA) (" + field + ") (Special Edition)")
		if Boundary([]Profile{retail}, []Profile{edition}) == "" {
			t.Fatalf("development label erased edition: %+v", edition)
		}
	}
	title, p := Analyze("Orbit - Trial")
	if title != "Orbit" || !p.Development() {
		t.Fatal(title, p)
	}
	for _, name := range []string{"Demo Racer (USA)", "Beta Fighter (USA)", "Sample Collection (USA)"} {
		_, p := Analyze(name)
		if p.Kind != "retail" {
			t.Fatal("title word became a release stage", name, p)
		}
	}
}

func TestEditionTaxonomyDoesNotEraseDevelopmentStage(t *testing.T) {
	_, retail := Analyze("Game (Japan)")
	_, beta := Analyze("Game (USA) (Beta) (2001-02-03)")
	profiles := []Profile{retail, beta}
	for i := range profiles {
		profiles = ApplyKind(profiles, i, "expanded edition")
	}
	if Boundary(profiles) != "" || !profiles[1].Development() || profiles[1].Build == "" {
		t.Fatal(profiles)
	}
	_, base := Analyze("Game (Europe)")
	if Boundary(profiles, []Profile{base}) == "" {
		t.Fatal("expanded product boundary lost")
	}
}

func TestExplicitNetworkTestStagesRemainGameReleases(t *testing.T) {
	_, retail := Analyze("Orbit (USA)")
	for _, label := range []string{"Online Test", "Online Test Version", "Network Test", "Multiplayer Alpha", "Multiplayer Beta", "Pilot Test"} {
		for _, name := range []string{"Orbit - " + label + " (Japan)", "Orbit (USA) (" + label + ")"} {
			title, profile := Analyze(name)
			if title != "Orbit" || !profile.Development() || profile.Kind != "beta" || profile.Build == "" || Boundary([]Profile{retail, profile}) != "" {
				t.Fatalf("%s: %s %+v", name, title, profile)
			}
		}
	}
	for _, name := range []string{"Test Drive (USA)", "Network Testers (USA)", "Orbit - Online Edition (USA)", "Orbit (USA) (Test Disc)"} {
		_, profile := Analyze(name)
		if profile.Development() {
			t.Fatalf("unproven test title became a development stage: %s %+v", name, profile)
		}
	}
	_, edition := Analyze("Orbit - Online Test Version (Japan) (Special Edition)")
	if KnownBoundary([]Profile{retail}, []Profile{edition}) == "" {
		t.Fatal("network-test label erased commercial edition")
	}
}

func TestCompleteEditionInstallersBelongToTheirEdition(t *testing.T) {
	for _, title := range []string{"Orbit", "A Thousand Rooms", "星の物語"} {
		_, base := Analyze(title + " (USA)")
		_, complete := Analyze(title + " - Game of the Year Edition (USA) (Disc 1)")
		for _, label := range []string{"Add-On Disc", "Add-On Content Disc", "Additional Content Packs Install Disc", "Tsuika Contents Disc"} {
			_, part := Analyze(title + " - Game of the Year Edition (Japan) (Disc 2) (" + label + ")")
			if part.Component == "" || part.Kind != "addon" || Boundary([]Profile{complete, part}) != "" || Boundary([]Profile{base}, []Profile{part}) == "" {
				t.Fatalf("%s: %+v %+v", label, complete, part)
			}
			standaloneName := title + " (Japan) (" + label + ")"
			_, standalone := Analyze(standaloneName)
			if standalone.Component != "" || Boundary([]Profile{base}, []Profile{standalone}) == "" {
				t.Fatalf("standalone addon lost its boundary: %s %+v", standaloneName, standalone)
			}
			expanded := ApplyKind([]Profile{complete, part}, 0, "expanded edition")
			expanded = ApplyKind(expanded, 1, "expanded edition")
			if Boundary(expanded) != "" {
				t.Fatal("edition taxonomy split its installer", expanded)
			}
			parts := ApplyKind([]Profile{part}, 0, "addon")
			if Boundary([]Profile{complete}, parts) != "" {
				t.Fatal(parts)
			}
		}
		for _, label := range []string{"Install Disc", "Content Install Disc", "Play Disc", "Game Disc"} {
			_, part := Analyze(title + " (Japan) (Disc 2) (" + label + ")")
			if part.Component == "" || Boundary([]Profile{base, part}) != "" {
				t.Fatal(label, part)
			}
		}
		_, bonus := Analyze(title + " - Game of the Year Edition (USA) (Bonus Disc)")
		if bonus.Component != "" || Boundary([]Profile{complete}, []Profile{bonus}) == "" {
			t.Fatal(bonus)
		}
	}
}

func TestExplicitEditionSubtitlesDoNotNeedMarketingAdjectiveLists(t *testing.T) {
	_, base := Analyze("Orbit (USA)")
	for _, label := range []string{"Legendary Edition", "Unclassified Edition", "Never Before Seen Edition"} {
		_, edition := Analyze("Orbit - " + label + " (USA)")
		if edition.Edition == "" || Boundary([]Profile{base}, []Profile{edition}) == "" {
			t.Fatal(label, edition)
		}
	}
	for _, label := range []string{"Game of the Year Edition", "GOTY Edition", "Complete Edition", "Ultimate Edition", "Legendary Edition"} {
		_, game := Analyze("Orbit - " + label + " (USA) (Disc 1)")
		_, installer := Analyze("Orbit - " + label + " (Japan) (Add-On Disc)")
		if Boundary([]Profile{base}, []Profile{game}) == "" || Boundary([]Profile{game, installer}) != "" {
			t.Fatal(label, game, installer)
		}
	}
	for _, label := range []string{"Demo Edition", "Beta Edition", "Trial Edition", "Preview Edition"} {
		_, stage := Analyze("Orbit - " + label + " (USA)")
		if !stage.Development() || Boundary([]Profile{base, stage}) != "" {
			t.Fatal(label, stage)
		}
	}
}
