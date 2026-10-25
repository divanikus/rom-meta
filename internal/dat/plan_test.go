package dat

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleDAT = `<?xml version="1.0"?>
<datafile>
  <header>
    <name>Nintendo - Nintendo 64 (BigEndian)</name>
    <description>Nintendo - Nintendo 64 (BigEndian)</description>
    <version>20260609-194259</version>
    <author>No-Intro</author>
  </header>
  <game name="Star Fox 64 (USA)" id="0686">
    <rom name="Star Fox 64 (USA).z64" size="12582912" crc="b1fcaa9c" md5="caf9a78db13ee00002ff63a3c0c5eabb" sha1="d8b1088520f7c5f81433292a9258c1184afa1457" sha256="a8d31134f6d2658fb7bdce5e8f8e74ab70cfcbed73ada22d680225915cb0fe22" status="verified" serial="NFXE"/>
  </game>
  <game name="Lylatwars (Europe) (En,Fr,De)" id="0387" cloneofid="0686">
    <rom name="Lylatwars (Europe) (En,Fr,De).z64" size="12582912" crc="50a9c0b1" md5="884ccca35cbeedb8ed288326f9662100" sha1="05b307b8804f992af1a1e2fbafbd588501fdf799" sha256="6604c219c2835446522d03a7a3f3634fb00a27703fd6b7f318d98f003806e3a4" status="verified" serial="NFXP"/>
  </game>
</datafile>`

const sampleMAMEListXML = `<?xml version="1.0"?>
<mame build="0.277">
  <machine name="sf2" sourcefile="cps1.cpp" runnable="yes">
    <description>Street Fighter II: The World Warrior</description>
    <year>1991</year>
    <manufacturer>Capcom</manufacturer>
    <rom name="sf2_30a.11e" size="131072" crc="00000001" sha1="0000000000000000000000000000000000000001"/>
    <disk name="sf2.chd" sha1="0000000000000000000000000000000000000010"/>
    <sample name="coin"/>
  </machine>
  <machine name="sf2ua" sourcefile="cps1.cpp" cloneof="sf2" romof="sf2" runnable="yes">
    <description>Street Fighter II: The World Warrior (US 910206)</description>
    <rom name="sf2u_30a.11e" size="131072" crc="00000002" sha1="0000000000000000000000000000000000000002"/>
  </machine>
  <machine name="neogeo" sourcefile="neogeo.cpp" isbios="yes" runnable="no">
    <description>Neo-Geo MV-6F</description>
    <rom name="sp-s2.sp1" size="131072" crc="00000003" sha1="0000000000000000000000000000000000000003"/>
  </machine>
  <machine name="ym2608" sourcefile="devices.cpp" isdevice="yes" runnable="no">
    <description>Yamaha YM2608 sound chip</description>
    <rom name="ym2608.wav" size="1024" crc="00000004" sha1="0000000000000000000000000000000000000004"/>
  </machine>
</mame>`

const sampleMAMEClrmameXML = `<?xml version="1.0"?>
<datafile>
  <header>
    <name>MAME ROMs (merged)</name>
    <version>0.288</version>
    <author>Pleasuredome</author>
  </header>
  <machine name="sf2" sourcefile="cps1.cpp" runnable="yes">
    <description>Street Fighter II: The World Warrior</description>
    <rom name="sf2_30a.11e" size="131072" crc="00000001" sha1="0000000000000000000000000000000000000001"/>
  </machine>
  <machine name="neogeo" sourcefile="neogeo.cpp" isbios="yes" runnable="no">
    <description>Neo-Geo MV-6F</description>
    <rom name="sp-s2.sp1" size="131072" crc="00000002" sha1="0000000000000000000000000000000000000002"/>
  </machine>
</datafile>`

const sampleRedumpDAT = `<?xml version="1.0"?>
<datafile>
  <header>
    <name>Sony - PlayStation</name>
    <version>2026-06-15 11-55-46</version>
    <author>redump.org</author>
  </header>
  <game name="Chrono Cross (USA, Canada) (Disc 1)">
    <serial>SLUS-01041</serial>
    <version>Rev 1</version>
    <rom name="Chrono Cross (USA, Canada) (Disc 1) (Track 01).bin" size="705600" crc="10000001" md5="00000000000000000000000010000001" sha1="0000000000000000000000000000000010000001"/>
    <rom name="Chrono Cross (USA, Canada) (Disc 1) (Track 02).bin" size="705600" crc="10000002" md5="00000000000000000000000010000002" sha1="0000000000000000000000000000000010000002"/>
  </game>
  <game name="Chrono Cross (USA, Canada) (Disc 2)">
    <rom name="Chrono Cross (USA, Canada) (Disc 2) (Track 01).bin" size="705600" crc="20000001" md5="00000000000000000000000020000001" sha1="0000000000000000000000000000000020000001"/>
  </game>
</datafile>`

func TestBuildPlanInfersPlatformFromDATHeader(t *testing.T) {
	path := writeTempDAT(t, "Nintendo - Nintendo 64 (BigEndian).dat", sampleDAT)

	plan, err := BuildPlan(path, PlanOptions{SampleLimit: 4})
	if err != nil {
		t.Fatalf("BuildPlan returned error: %v", err)
	}

	if plan.DatasetName != "Nintendo - Nintendo 64 (BigEndian)" {
		t.Fatalf("unexpected dataset: %q", plan.DatasetName)
	}
	if plan.PlatformName != "Nintendo - Nintendo 64" {
		t.Fatalf("unexpected platform: %q", plan.PlatformName)
	}
	if plan.LayoutHint != "BigEndian" {
		t.Fatalf("unexpected layout hint: %q", plan.LayoutHint)
	}
	if plan.GameCount != 2 || plan.ROMCount != 2 || plan.CloneLinkCount != 1 {
		t.Fatalf("unexpected counts: games=%d roms=%d clones=%d", plan.GameCount, plan.ROMCount, plan.CloneLinkCount)
	}
	if plan.HashCounts.SHA256 != 2 {
		t.Fatalf("unexpected sha256 count: %d", plan.HashCounts.SHA256)
	}
}

func TestBuildPlanDetectsMAMEListXML(t *testing.T) {
	path := writeTempDAT(t, "mame.xml", sampleMAMEListXML)

	plan, err := BuildPlan(path, PlanOptions{SampleLimit: 3})
	if err != nil {
		t.Fatalf("BuildPlan returned error: %v", err)
	}

	if plan.Format != "mame-listxml" {
		t.Fatalf("unexpected format: %q", plan.Format)
	}
	if plan.Authority != "mame" {
		t.Fatalf("unexpected authority: %q", plan.Authority)
	}
	if plan.DatasetName != "MAME" {
		t.Fatalf("unexpected dataset: %q", plan.DatasetName)
	}
	if plan.PlatformName != "arcade" {
		t.Fatalf("unexpected platform: %q", plan.PlatformName)
	}
	if plan.Version != "0.277" {
		t.Fatalf("unexpected version: %q", plan.Version)
	}
	if plan.GameCount != 4 || plan.MachineCount != 4 {
		t.Fatalf("unexpected entry counts: games=%d machines=%d", plan.GameCount, plan.MachineCount)
	}
	if plan.PlayableCount != 2 || plan.BIOSCount != 1 || plan.DeviceCount != 1 {
		t.Fatalf("unexpected machine counts: playable=%d bios=%d devices=%d", plan.PlayableCount, plan.BIOSCount, plan.DeviceCount)
	}
	if plan.ROMCount != 4 || plan.DiskCount != 1 || plan.SampleCount != 1 {
		t.Fatalf("unexpected payload counts: roms=%d disks=%d samples=%d", plan.ROMCount, plan.DiskCount, plan.SampleCount)
	}
	if plan.CloneLinkCount != 1 {
		t.Fatalf("unexpected clone links: %d", plan.CloneLinkCount)
	}
	if len(plan.SampleGames) != 3 {
		t.Fatalf("unexpected sample count: %d", len(plan.SampleGames))
	}
	if plan.SampleGames[0].Description != "Street Fighter II: The World Warrior" {
		t.Fatalf("unexpected first sample description: %q", plan.SampleGames[0].Description)
	}
	if !plan.SampleGames[2].BIOS {
		t.Fatalf("expected BIOS flag on third sample")
	}
}

func TestBuildPlanTreatsMAMEClrmameROMsAsArcade(t *testing.T) {
	path := writeTempDAT(t, "MAME 0.288 ROMs (merged).xml", sampleMAMEClrmameXML)

	plan, err := BuildPlan(path, PlanOptions{SampleLimit: 2})
	if err != nil {
		t.Fatalf("BuildPlan returned error: %v", err)
	}

	if plan.Format != "clrmamepro-xml" {
		t.Fatalf("unexpected format: %q", plan.Format)
	}
	if plan.Authority != "mame" {
		t.Fatalf("unexpected authority: %q", plan.Authority)
	}
	if plan.PlatformName != "arcade" {
		t.Fatalf("unexpected platform: %q", plan.PlatformName)
	}
	if plan.MachineCount != 2 || plan.PlayableCount != 1 || plan.BIOSCount != 1 {
		t.Fatalf("unexpected machine counts: machines=%d playable=%d bios=%d", plan.MachineCount, plan.PlayableCount, plan.BIOSCount)
	}
}

func TestBuildPlanDetectsRedumpAuthority(t *testing.T) {
	path := writeTempDAT(t, "Sony - PlayStation - Datfile.dat", sampleRedumpDAT)

	plan, err := BuildPlan(path, PlanOptions{SampleLimit: 2})
	if err != nil {
		t.Fatalf("BuildPlan returned error: %v", err)
	}

	if plan.Authority != "redump" {
		t.Fatalf("unexpected authority: %q", plan.Authority)
	}
	if plan.PlatformName != "Sony - PlayStation" {
		t.Fatalf("unexpected platform: %q", plan.PlatformName)
	}
	if plan.GameCount != 2 || plan.ROMCount != 3 {
		t.Fatalf("unexpected counts: games=%d roms=%d", plan.GameCount, plan.ROMCount)
	}
}

func TestBuildPlanSupportsPlatformOverride(t *testing.T) {
	path := writeTempDAT(t, "Nintendo - Nintendo 64 (BigEndian).dat", sampleDAT)

	plan, err := BuildPlan(path, PlanOptions{PlatformOverride: "Nintendo 64", SampleLimit: 0})
	if err != nil {
		t.Fatalf("BuildPlan returned error: %v", err)
	}

	if plan.PlatformName != "Nintendo 64" {
		t.Fatalf("unexpected override platform: %q", plan.PlatformName)
	}
	if !plan.PlatformOverride {
		t.Fatalf("expected platform override flag")
	}
	if len(plan.SampleGames) != 0 {
		t.Fatalf("expected no sample games, got %#v", plan.SampleGames)
	}
}

func TestBuildPlanReadsDATFromZip(t *testing.T) {
	path := writeTempDATZip(t, "Nintendo - Nintendo 64 (BigEndian) (20260609-194259).zip", map[string]string{
		"Nintendo - Nintendo 64 (BigEndian) (20260609-194259).dat": sampleDAT,
	})

	plan, err := BuildPlan(path, PlanOptions{SampleLimit: 1})
	if err != nil {
		t.Fatalf("BuildPlan returned error: %v", err)
	}

	if plan.Container != "zip" {
		t.Fatalf("unexpected container: %q", plan.Container)
	}
	if plan.MemberPath == "" {
		t.Fatalf("expected member path")
	}
	if len(plan.SampleGames) != 1 {
		t.Fatalf("expected one sample, got %#v", plan.SampleGames)
	}
}

func TestBuildPlanReadsSelectedDATZipMember(t *testing.T) {
	path := writeTempDATZip(t, "multi-system.zip", map[string]string{
		"arcade.dat":    sampleMAMEClrmameXML,
		"redump/ps.dat": sampleRedumpDAT,
	})

	plan, err := BuildPlan(path, PlanOptions{MemberPath: "ps.dat", SampleLimit: 1})
	if err != nil {
		t.Fatalf("BuildPlan returned error: %v", err)
	}

	if plan.MemberPath != "redump/ps.dat" {
		t.Fatalf("unexpected member path: %q", plan.MemberPath)
	}
	if plan.Authority != "redump" {
		t.Fatalf("unexpected authority: %q", plan.Authority)
	}
	if plan.PlatformName != "Sony - PlayStation" {
		t.Fatalf("unexpected platform: %q", plan.PlatformName)
	}
}

func TestBuildPlanReportsAvailableMembersWhenSelectedZipMemberIsMissing(t *testing.T) {
	path := writeTempDATZip(t, "multi-system.zip", map[string]string{
		"arcade.dat": sampleMAMEClrmameXML,
		"ps.dat":     sampleRedumpDAT,
	})

	_, err := BuildPlan(path, PlanOptions{MemberPath: "missing.dat"})
	if err == nil {
		t.Fatalf("expected missing member error")
	}
	if !strings.Contains(err.Error(), "arcade.dat") || !strings.Contains(err.Error(), "ps.dat") {
		t.Fatalf("expected available members in error, got: %v", err)
	}
}

func TestBuildPlanFallsBackToFilenameWhenHeaderIsMissing(t *testing.T) {
	path := writeTempDAT(t, "Nintendo - Nintendo 64 (BigEndian) (20260609-194259).dat", `<datafile>
  <game name="Star Fox 64 (USA)">
    <rom name="Star Fox 64 (USA).z64" crc="b1fcaa9c"/>
  </game>
</datafile>`)

	plan, err := BuildPlan(path, PlanOptions{SampleLimit: 1})
	if err != nil {
		t.Fatalf("BuildPlan returned error: %v", err)
	}

	if plan.DatasetName != "Nintendo - Nintendo 64 (BigEndian)" {
		t.Fatalf("unexpected dataset: %q", plan.DatasetName)
	}
	if plan.PlatformName != "Nintendo - Nintendo 64" {
		t.Fatalf("unexpected platform: %q", plan.PlatformName)
	}
}

func TestBuildPlanInfersNoIntroAuthorityFromPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "No-Intro")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	path := filepath.Join(root, "Nintendo - Game Boy (20260615-112845).dat")
	if err := os.WriteFile(path, []byte(`<datafile>
  <header>
    <name>Nintendo - Game Boy</name>
    <description>Nintendo - Game Boy</description>
  </header>
  <game name="Tetris (World)">
    <rom name="Tetris (World).gb" crc="1f2d3c4b"/>
  </game>
</datafile>`), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	plan, err := BuildPlan(path, PlanOptions{SampleLimit: 0})
	if err != nil {
		t.Fatalf("BuildPlan returned error: %v", err)
	}
	if plan.Authority != "no-intro" {
		t.Fatalf("unexpected authority: %q", plan.Authority)
	}
}

func writeTempDAT(t *testing.T, name, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	return path
}

func writeTempDATZip(t *testing.T, name string, members map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	writer := zip.NewWriter(file)
	for name, contents := range members {
		member, err := writer.Create(name)
		if err != nil {
			t.Fatalf("Create member failed: %v", err)
		}
		if _, err := member.Write([]byte(contents)); err != nil {
			t.Fatalf("Write member failed: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close writer failed: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close file failed: %v", err)
	}
	return path
}
