package main

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"rommeta/internal/catalogfold"
	"rommeta/internal/catalogstate"
	"rommeta/internal/dat"
	"rommeta/internal/platformnames"
	"rommeta/internal/sourceaudit"
	"rommeta/internal/store"
)

func TestFormatFetchMetadataProgressUsesJobLabel(t *testing.T) {
	line := formatTaskProgressLine(
		"screenscraper/metadata",
		"matching",
		5,
		10,
		fetchMetadataProgressLabel(12, 3, "Star Fox 64"),
		false,
	)

	for _, expected := range []string{
		"screenscraper/metadata [==============--------------] 5/10  50%",
		"matching",
		"queries=12",
		"matches=3",
		"| Star Fox 64",
	} {
		if !strings.Contains(line, expected) {
			t.Fatalf("progress line missing %q:\n%s", expected, line)
		}
	}
	if strings.Contains(line, "current=") || strings.Contains(line, "games") {
		t.Fatalf("metadata progress line should not use legacy labels:\n%s", line)
	}
}

func TestFormatFetchMetadataProgressHandlesUnknownTotal(t *testing.T) {
	line := formatTaskProgressLine(
		"hasheous/metadata",
		"matching",
		17,
		0,
		fetchMetadataProgressLabel(12, 3, "Sonic the Hedgehog CD"),
		false,
	)

	for _, expected := range []string{
		"hasheous/metadata | matching",
		"queries=12",
		"matches=3",
		"| Sonic the Hedgehog CD",
	} {
		if !strings.Contains(line, expected) {
			t.Fatalf("progress line missing %q:\n%s", expected, line)
		}
	}
	if strings.Contains(line, "17/0") || strings.Contains(line, "  0%") || strings.Contains(line, "games") {
		t.Fatalf("unknown-total progress line should not render fake total/percent:\n%s", line)
	}
}

func TestTrimProgressTextUsesAsciiEllipsis(t *testing.T) {
	if got := trimProgressText("123456789", 6); got != "123..." {
		t.Fatalf("unexpected trim result: %q", got)
	}
}

func TestFormatTaskProgressLineCanUseColor(t *testing.T) {
	line := formatTaskProgressLine("dat import", "writing", 2, 4, "Star Fox 64", true)

	for _, expected := range []string{
		ansiCyan + "dat import" + ansiReset,
		ansiGreen,
		ansiYellow + "writing" + ansiReset,
		"2/4  50%",
		"| Star Fox 64",
	} {
		if !strings.Contains(line, expected) {
			t.Fatalf("task progress line missing %q:\n%s", expected, line)
		}
	}
	if strings.Contains(line, "current=") || strings.Contains(line, ansiYellow+"current"+ansiReset) {
		t.Fatalf("task progress line should not contain current label:\n%s", line)
	}
}

func TestFormatTaskBytesProgressLineUsesHumanUnits(t *testing.T) {
	line := formatTaskBytesProgressLine("launchbox/metadata", "downloading snapshot", 1024*1024, 4*1024*1024, "Metadata.zip", false)

	for _, expected := range []string{
		"launchbox/metadata [=======---------------------] 1.0 MiB/4.0 MiB  25%",
		"downloading snapshot",
		"| Metadata.zip",
	} {
		if !strings.Contains(line, expected) {
			t.Fatalf("byte progress line missing %q:\n%s", expected, line)
		}
	}
	if strings.Contains(line, "current=") {
		t.Fatalf("byte progress line should not contain current=:\n%s", line)
	}
}

func TestFetchConcurrencyUsesConfigAndSourceLimits(t *testing.T) {
	config := store.SourcesConfig{
		FetchConcurrency: 4,
		Sources: []store.Source{
			{Name: "emumovies", FetchConcurrency: 2},
			{Name: "screenscraper", FetchConcurrency: 1},
		},
	}

	tests := []struct {
		name    string
		options fetchOptions
		job     fetchJob
		want    int
	}{
		{
			name: "default artwork concurrency",
			job:  fetchJob{Source: "launchbox", Type: "artwork"},
			want: 4,
		},
		{
			name: "source stanza override",
			job:  fetchJob{Source: "emumovies", Type: "video"},
			want: 2,
		},
		{
			name: "cli override",
			options: fetchOptions{
				Concurrency:    3,
				ConcurrencySet: true,
			},
			job:  fetchJob{Source: "launchbox", Type: "artwork"},
			want: 3,
		},
		{
			name: "metadata stays sequential",
			job:  fetchJob{Source: "launchbox", Type: "metadata"},
			want: 1,
		},
		{
			name: "hasheous metadata uses global concurrency",
			job:  fetchJob{Source: "hasheous", Type: "metadata"},
			want: 4,
		},
		{
			name:    "hasheous metadata CLI override",
			options: fetchOptions{Concurrency: 6, ConcurrencySet: true},
			job:     fetchJob{Source: "hasheous", Type: "metadata"},
			want:    6,
		},
		{
			name: "screenscraper stays sequential",
			options: fetchOptions{
				Concurrency:    3,
				ConcurrencySet: true,
			},
			job:  fetchJob{Source: "screenscraper", Type: "artwork"},
			want: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := fetchConcurrency(test.options, config, test.job); got != test.want {
				t.Fatalf("fetchConcurrency() = %d, want %d", got, test.want)
			}
		})
	}
	config.Sources = append(config.Sources, store.Source{Name: "hasheous", FetchConcurrency: 2})
	if got := fetchConcurrency(fetchOptions{}, config, fetchJob{Source: "hasheous", Type: "metadata"}); got != 2 {
		t.Fatalf("Hasheous source concurrency = %d, want 2", got)
	}
}

func TestFetchJobLabelUsesDisplayNames(t *testing.T) {
	tests := []struct {
		job  fetchJob
		want string
	}{
		{job: fetchJob{Source: "igdb", Type: "metadata"}, want: "IGDB metadata"},
		{job: fetchJob{Source: "emumovies", Type: "video"}, want: "EmuMovies videos"},
	}

	for _, test := range tests {
		if got := fetchJobLabel(test.job); got != test.want {
			t.Fatalf("fetchJobLabel(%#v) = %q, want %q", test.job, got, test.want)
		}
	}
}

func TestOmittedFailuresLineShowsTruncatedFailureLists(t *testing.T) {
	if got := omittedFailuresLine(3587, 10); got != "  ... 3577 more failures omitted" {
		t.Fatalf("omittedFailuresLine() = %q", got)
	}
	if got := omittedFailuresLine(11, 10); got != "  ... 1 more failure omitted" {
		t.Fatalf("omittedFailuresLine() = %q", got)
	}
	if got := omittedFailuresLine(10, 10); got != "" {
		t.Fatalf("expected no omitted line, got %q", got)
	}
}

func TestValidateFetchJobsRejectsUnsupportedPairs(t *testing.T) {
	err := validateFetchJobs([]fetchJob{{Source: "emumovies", Type: "metadata"}})
	if err == nil {
		t.Fatalf("expected unsupported fetch job error")
	}
	if !strings.Contains(err.Error(), `fetch source "emumovies" does not support type "metadata"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunFetchJobsContinuesAfterFailedJob(t *testing.T) {
	jobs := []fetchJob{
		{Source: "igdb", Type: "metadata"},
		{Source: "launchbox", Type: "metadata"},
		{Source: "screenscraper", Type: "metadata"},
	}
	var ran []string
	var out bytes.Buffer

	err := runFetchJobs(&out, jobs, fetchOptions{}, store.SourcesConfig{}, func(job fetchJob, options fetchOptions, config store.SourcesConfig) error {
		ran = append(ran, job.Source+"/"+job.Type)
		if job.Source == "launchbox" {
			return errors.New("snapshot unavailable")
		}
		return nil
	})

	if got := strings.Join(ran, ","); got != "igdb/metadata,launchbox/metadata,screenscraper/metadata" {
		t.Fatalf("expected all jobs to run, got %s", got)
	}
	if err == nil || !strings.Contains(err.Error(), "launchbox/metadata") {
		t.Fatalf("expected aggregate job error, got %v", err)
	}
	if !strings.Contains(out.String(), "Result: failed; error: snapshot unavailable") {
		t.Fatalf("expected failed job to be reported:\n%s", out.String())
	}
}

func TestTerminalProgressRendersStableWorkerSlots(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "progress-*.txt")
	if err != nil {
		t.Fatalf("CreateTemp returned error: %v", err)
	}
	defer file.Close()
	progress := &terminalProgress{
		out:         file,
		interactive: true,
		color:       false,
	}

	progress.UpdateWorker("launchbox/artwork", "waiting", 1, 2, "waiting...")
	progress.UpdateWorkerBytes("launchbox/artwork", "downloading", 2, 2, 416*1024, -1, "Nintendo - Nintendo 64 / Artist Tool / box-front.launchbox.jpg")

	if err := file.Sync(); err != nil {
		t.Fatalf("Sync returned error: %v", err)
	}
	data, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	output := string(data)
	if !strings.Contains(output, "#1") || !strings.Contains(output, "waiting...") || !strings.Contains(output, "#2") || !strings.Contains(output, "Artist Tool") {
		t.Fatalf("expected stable worker slot output:\n%s", output)
	}
	if strings.Contains(output, "current=") || strings.Contains(output, "launchbox/artwork | downloading") {
		t.Fatalf("worker rows should not repeat task/stage/current labels:\n%s", output)
	}
	if !strings.Contains(output, "\n") {
		t.Fatalf("expected multiline worker progress output:\n%s", output)
	}
}

func TestTerminalProgressKeepsCompactTaskLineWithWorkerSlots(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "progress-*.txt")
	if err != nil {
		t.Fatalf("CreateTemp returned error: %v", err)
	}
	defer file.Close()
	progress := &terminalProgress{
		out:         file,
		interactive: true,
		color:       false,
		compactTask: true,
	}

	progress.UpdateTask("launchbox/artwork", "scanning", 2, 10, "Nintendo - Nintendo 64 / Star Fox 64")
	progress.UpdateWorkerBytes("launchbox/artwork", "downloading", 1, 2, 1, 4, "Nintendo - Nintendo 64 / Star Fox 64 / box-front.launchbox.jpg")

	if err := file.Sync(); err != nil {
		t.Fatalf("Sync returned error: %v", err)
	}
	data, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	output := string(data)
	if !strings.Contains(output, "scanning") || !strings.Contains(output, "Nintendo - Nintendo 64 / Star Fox 64") {
		t.Fatalf("expected task progress line to stay visible:\n%s", output)
	}
	if !strings.Contains(output, "#1") || !strings.Contains(output, "box-front.launchbox.jpg") {
		t.Fatalf("expected worker progress line to stay visible:\n%s", output)
	}
	if strings.Contains(output, "current=") || strings.Contains(output, "launchbox/artwork | downloading") {
		t.Fatalf("fetch progress should use compact task and worker lines:\n%s", output)
	}
}

func TestFormatWorkerProgressLineUsesFixedMetricColumns(t *testing.T) {
	started := time.Now().Add(-1 * time.Second)
	preparing := formatWorkerProgressLine(byteProgressItem{
		Stage:   "preparing",
		Label:   "label",
		Worker:  1,
		Started: started,
	}, false)
	short := formatWorkerProgressLine(byteProgressItem{
		Stage:   "downloading",
		Label:   "label",
		Worker:  1,
		Current: 1024,
		Total:   -1,
		Started: started,
	}, false)
	long := formatWorkerProgressLine(byteProgressItem{
		Stage:   "downloading",
		Label:   "label",
		Worker:  1,
		Current: 12 * 1024 * 1024,
		Total:   -1,
		Started: started,
	}, false)

	known := formatWorkerProgressLine(byteProgressItem{
		Stage:   "downloading",
		Label:   "label",
		Worker:  1,
		Current: 1024,
		Total:   4096,
		Started: started,
	}, false)

	labels := []int{
		strings.Index(short, "| label"),
		strings.Index(long, "| label"),
		strings.Index(known, "| label"),
	}
	for _, label := range labels {
		if label < 0 || label != labels[0] {
			t.Fatalf("worker activity columns should keep the label stable:\n%s\n%s\n%s\n%s", preparing, short, long, known)
		}
	}
	if !strings.Contains(short, " (") || !strings.Contains(short, "/s)") || strings.Count(short, "|") != 1 {
		t.Fatalf("worker download line should keep speed inside the activity block:\n%s", short)
	}
	preparingLabel := strings.Index(preparing, "| label")
	if preparingLabel < 0 || preparingLabel >= labels[0] {
		t.Fatalf("preparing should stay compact instead of aligning with download metrics:\n%s\n%s", preparing, short)
	}
}

func TestTerminalProgressReturnsUnknownSizeWorkerToWaiting(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "progress-*.txt")
	if err != nil {
		t.Fatalf("CreateTemp returned error: %v", err)
	}
	defer file.Close()
	progress := &terminalProgress{
		out:         file,
		interactive: true,
		color:       false,
	}

	progress.UpdateTask("launchbox/artwork", "scanning", 2, 10, "Nintendo - Nintendo 64 / Star Fox 64")
	progress.UpdateWorkerBytes("launchbox/artwork", "downloading", 1, 2, 64*1024, -1, "box-front.launchbox.jpg")
	if len(progress.byteItems) != 1 {
		t.Fatalf("expected one active worker item, got %d", len(progress.byteItems))
	}

	progress.UpdateWorkerBytes("launchbox/artwork", "downloading", 1, 2, -1, -1, "box-front.launchbox.jpg")
	if len(progress.byteItems) != 1 {
		t.Fatalf("expected worker slot to stay visible, got %d items", len(progress.byteItems))
	}
	for _, item := range progress.byteItems {
		if item.Stage != "waiting" || item.Label != "waiting..." {
			t.Fatalf("expected worker to return to waiting, got %#v", item)
		}
	}
}

func TestValidateProgressLineOmitsRedundantStage(t *testing.T) {
	line := formatTaskProgressLine("validate", "", 2, 4, "Nintendo 64 / Star Fox 64", false)

	if strings.Contains(line, "stage") || strings.Contains(line, "validating") {
		t.Fatalf("validate progress should not repeat validating stage:\n%s", line)
	}
	if !strings.Contains(line, "validate [==============--------------] 2/4  50%") {
		t.Fatalf("validate progress missing compact prefix:\n%s", line)
	}
	if !strings.Contains(line, "| Nintendo 64 / Star Fox 64") {
		t.Fatalf("validate progress missing current item:\n%s", line)
	}
	if strings.Contains(line, "current=") {
		t.Fatalf("validate progress should not contain current=:\n%s", line)
	}
}

func TestRuneWidthIgnoresANSISequences(t *testing.T) {
	colored := ansiCyan + "validate" + ansiReset

	if got := runeWidth(colored); got != len("validate") {
		t.Fatalf("runeWidth(%q) = %d", colored, got)
	}
}

func TestRenderSourcesListUsesUserFacingFields(t *testing.T) {
	config := store.SourcesConfig{
		Root: "X:\\romsets\\metadata",
		Path: "X:\\romsets\\metadata\\config\\sources.yaml",
		Fetch: map[string][]string{
			"metadata": {"launchbox", "igdb"},
			"artwork":  {"launchbox"},
		},
		FetchConcurrency: 4,
		Sources: []store.Source{
			{
				Name:         "launchbox",
				Kind:         "metadata-and-media",
				Enabled:      false,
				SnapshotPath: ".rommeta/sources/launchbox/Metadata.zip",
				SnapshotURL:  "https://example.test/Metadata.zip",
			},
			{
				Name:             "igdb",
				Kind:             "metadata-and-media",
				Enabled:          false,
				FetchConcurrency: 3,
				CredentialType:   "secrets",
				Credentials: []store.CredentialRef{
					{Name: "client_id", Ref: "client_id"},
					{Name: "client_secret", Ref: "client_secret"},
				},
				APIBaseURL: "https://api.igdb.com/v4",
			},
		},
	}

	output := renderSourcesList(config, false)

	for _, expected := range []string{
		"Source configuration",
		"concurrency: 4",
		"provides: metadata, artwork",
		"concurrency: 3",
		"snapshot: .rommeta/sources/launchbox/Metadata.zip",
		"endpoint: https://api.igdb.com/v4",
		"missing credentials: sources.igdb.client_id, sources.igdb.client_secret",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("sources list missing %q:\n%s", expected, output)
		}
	}
	for _, unwanted := range []string{"enabled:", "kind:", "levels:", "fetch:", "credentials: 0 set"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("sources list should not contain %q:\n%s", unwanted, output)
		}
	}
}

func TestRenderSourcesListCanUseColor(t *testing.T) {
	config := store.SourcesConfig{
		Fetch: map[string][]string{"metadata": {"igdb"}},
		Sources: []store.Source{
			{
				Name:           "igdb",
				CredentialType: "secrets",
				Credentials:    []store.CredentialRef{{Name: "client_id", Ref: "client_id"}},
			},
		},
	}

	output := renderSourcesList(config, true)

	for _, expected := range []string{
		ansiCyan + "Source configuration" + ansiReset,
		ansiCyan + "igdb" + ansiReset,
		ansiGreen + "metadata" + ansiReset,
		ansiRed + "missing credentials" + ansiReset,
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("colored sources list missing %q:\n%s", expected, output)
		}
	}
}

func TestRenderPlatformListUsesReadableBlocks(t *testing.T) {
	result := platformListResult{
		DataRoot: `X:\romsets\metadata`,
		Platforms: []platformnames.Platform{
			{
				Name:    "Sega - Master System - Mark III",
				Aliases: []string{"mastersystem", "sms", "Master System / Mark III"},
				Sources: map[string]platformnames.SourceRef{
					"launchbox":     {Name: "Sega Master System"},
					"screenscraper": {ID: "2"},
				},
			},
		},
	}

	output := renderPlatformListText(result, false, false)

	for _, expected := range []string{
		"Platform registry",
		`Library: X:\romsets\metadata`,
		"Platforms: 1",
		"Sega - Master System - Mark III",
		"aliases: mastersystem, sms, Master System / Mark III",
		"source mappings: launchbox, screenscraper",
		"providers may also match by name",
		"Missing mappings do not establish source availability or downloaded coverage",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("platform list output missing %q:\n%s", expected, output)
		}
	}
	for _, unwanted := range []string{"platform  aliases", "----------------"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("platform list should not use the old clipped table layout:\n%s", output)
		}
	}
}

func TestRenderDATImportResultGroupsFields(t *testing.T) {
	result := dat.ImportResult{
		Plan: dat.Plan{
			InputPath:    `D:\Temp\dats\Redump\Sega - Mega CD.zip`,
			Container:    "zip",
			MemberPath:   "Sega - Mega CD.dat",
			Format:       "clrmamepro-xml",
			Authority:    "redump",
			DatasetName:  "Sega - Mega CD & Sega CD",
			PlatformName: "Sega - Mega CD & Sega CD",
			Version:      "2026-05-28 18-06-58",
			Author:       "redump.org",
			GameCount:    549,
			ROMCount:     8426,
			HashCounts:   dat.HashCounts{CRC32: 8426, MD5: 8426, SHA1: 8426},
			SampleGames: []dat.GameSample{
				{Name: "[BIOS] Mega-CD (Japan)"},
			},
		},
		SampleGames: []dat.GameSample{
			{Name: "Cliffhanger (USA)"},
			{Name: "Dragon's Lair (Japan)"},
		},
		DataRoot:         `X:\romsets\metadata`,
		PlatformPath:     `X:\romsets\metadata\platforms\Sega - Mega CD & Sega CD`,
		GameBundleCount:  263,
		ReleaseCount:     549,
		FingerprintCount: 8426,
		WrittenFileCount: 528,
		Apply:            true,
		Changes:          dat.ImportChanges{Action: "create", Added: 263},
	}

	output := renderDATImportResult(result, false)

	for _, expected := range []string{
		"DAT import summary",
		"Source",
		`input: D:\Temp\dats\Redump\Sega - Mega CD.zip`,
		"format: clrmamepro-xml; authority: redump",
		"dataset: Sega - Mega CD & Sega CD",
		"version: 2026-05-28 18-06-58; author: redump.org",
		"Catalog",
		`library: X:\romsets\metadata`,
		"platform: Sega - Mega CD & Sega CD (auto)",
		"operation: create new platform",
		"generated: 263 game bundles, 549 releases, 8,426 fingerprints",
		"Contents",
		"entries: 549 releases",
		"payloads: 8,426 ROMs",
		"hashes: crc32 8,426, md5 8,426, sha1 8,426, sha256 0",
		"Sample releases",
		"  - Cliffhanger (USA)",
		"Result: applied; written files: 528",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("DAT import output missing %q:\n%s", expected, output)
		}
	}
	if strings.Contains(output, "[BIOS] Mega-CD") {
		t.Fatalf("DAT import output should sample generated releases, not raw BIOS entries:\n%s", output)
	}
	for _, unwanted := range []string{"roms with crc32", "fingerprint entries:", "game bundles:"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("DAT import output should not contain old field %q:\n%s", unwanted, output)
		}
	}
	if strings.Contains(output, "changes:") {
		t.Fatalf("new platform output should not repeat generated counts as changes:\n%s", output)
	}
}

func TestRenderDATImportMergeReportsPlainChanges(t *testing.T) {
	result := dat.ImportResult{
		Plan: dat.Plan{
			InputPath:    `D:\Temp\dats\Nintendo - Nintendo 64.dat`,
			Container:    "file",
			Format:       "clrmamepro-xml",
			Authority:    "no-intro",
			DatasetName:  "Nintendo - Nintendo 64",
			PlatformName: "Nintendo - Nintendo 64",
		},
		DataRoot:         `X:\romsets\metadata`,
		PlatformPath:     `X:\romsets\metadata\platforms\Nintendo - Nintendo 64`,
		GameBundleCount:  500,
		ReleaseCount:     900,
		FingerprintCount: 900,
		Apply:            true,
		Changes: dat.ImportChanges{
			Action:     "merge",
			Added:      3,
			Reconciled: 495,
			Conflicts:  []string{`incoming game "New Group" matches multiple existing games: "Old Name", "Other Name"`},
		},
	}

	output := renderDATImportResult(result, false)
	for _, expected := range []string{
		"operation: merge into existing platform",
		"incoming: 500 game bundles, 900 releases, 900 fingerprints",
		"changes: 3 new, 495 reconciled, 1 unresolved match",
		"Result: needs review; no catalog files were changed",
		"Unresolved matches",
		`incoming game "New Group" matches multiple existing games: "Old Name", "Other Name"`,
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("DAT merge output missing %q:\n%s", expected, output)
		}
	}
}

func TestRenderCatalogFoldResultShowsDryRunAndDetails(t *testing.T) {
	result := catalogfold.Result{
		DataRoot:            `X:\romsets\metadata`,
		PlatformFilter:      "Nintendo - Nintendo Entertainment System",
		PlatformCount:       1,
		GameCount:           2,
		IdentityCount:       1,
		CandidateGroupCount: 1,
		MergedGameCount:     1,
		ReleaseCount:        1,
		FingerprintCount:    2,
		PlatformResults: []catalogfold.PlatformResult{
			{
				Platform:            "Nintendo - Nintendo Entertainment System",
				GameCount:           2,
				IdentityCount:       1,
				CandidateGroupCount: 1,
				MergedGameCount:     1,
				ReleaseCount:        1,
				FingerprintCount:    2,
			},
		},
		Groups: []catalogfold.Group{
			{
				Platform: "Nintendo - Nintendo Entertainment System",
				Keep:     catalogfold.GameRef{Folder: "Castlevania III - Dracula's Curse (USA)"},
				Merge:    []catalogfold.GameRef{{Folder: "Akumajou Densetsu (Japan)"}},
				Evidence: []catalogfold.Evidence{{Source: "launchbox", Field: "database_id", Value: "42"}},
			},
		},
	}

	output := renderCatalogFoldResult(result, true, false)

	for _, expected := range []string{
		"Catalog fold summary",
		`Library: X:\romsets\metadata`,
		"Scanned: 1 platforms, 2 games",
		"Candidates: 1 groups, 1 game bundles to merge, 1 evidence links",
		"Data to merge: 1 releases, 2 fingerprints",
		"Result: dry run; rerun with --apply to merge candidate groups",
		"By platform",
		"platform",
		"groups",
		"to merge",
		"releases",
		"fingerprints",
		"Fold details",
		"Nintendo - Nintendo Entertainment System / Castlevania III - Dracula's Curse (USA) <- Akumajou Densetsu (Japan)",
		"evidence:",
		"    - launchbox.database_id=42",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("catalog fold output missing %q:\n%s", expected, output)
		}
	}
}

func TestRenderCatalogFoldResultDoesNotShowDryRunNextAfterApply(t *testing.T) {
	result := catalogfold.Result{
		DataRoot:            `X:\romsets\metadata`,
		PlatformFilter:      "NEC - PC Engine - TurboGrafx-16",
		PlatformCount:       1,
		GameCount:           351,
		IdentityCount:       8,
		CandidateGroupCount: 8,
		MergedGameCount:     8,
		ReleaseCount:        9,
		FingerprintCount:    9,
		Apply:               true,
		AppliedGroupCount:   8,
		PlatformResults: []catalogfold.PlatformResult{
			{
				Platform:            "NEC - PC Engine - TurboGrafx-16",
				GameCount:           351,
				IdentityCount:       8,
				CandidateGroupCount: 8,
				MergedGameCount:     8,
				ReleaseCount:        9,
				FingerprintCount:    9,
				AppliedGroupCount:   8,
			},
		},
		Groups: []catalogfold.Group{
			{
				Platform: "NEC - PC Engine - TurboGrafx-16",
				Keep:     catalogfold.GameRef{Folder: "World Class Baseball"},
				Merge:    []catalogfold.GameRef{{Folder: "Power League"}},
				Evidence: []catalogfold.Evidence{{Source: "igdb", Field: "igdb_id", Value: "1"}},
			},
		},
	}

	output := renderCatalogFoldResult(result, false, false)

	if !strings.Contains(output, "Result: applied; groups: 8 applied, 0 skipped; moved files: 0") {
		t.Fatalf("apply result missing applied summary:\n%s", output)
	}
	if strings.Contains(output, "Next:") || strings.Contains(output, "proposed folds") {
		t.Fatalf("apply result should not show dry-run next step:\n%s", output)
	}
}

func TestRenderCatalogFoldResultDoesNotRepeatDetailsHintAfterDryRun(t *testing.T) {
	result := catalogfold.Result{
		DataRoot:            `X:\romsets\metadata`,
		PlatformFilter:      "Sega - Mega CD & Sega CD",
		PlatformCount:       1,
		GameCount:           263,
		IdentityCount:       41,
		CandidateGroupCount: 26,
		MergedGameCount:     27,
		ReleaseCount:        34,
		FingerprintCount:    858,
		PlatformResults: []catalogfold.PlatformResult{
			{
				Platform:            "Sega - Mega CD & Sega CD",
				GameCount:           263,
				IdentityCount:       41,
				CandidateGroupCount: 26,
				MergedGameCount:     27,
				ReleaseCount:        34,
				FingerprintCount:    858,
			},
		},
		Groups: []catalogfold.Group{
			{
				Platform: "Sega - Mega CD & Sega CD",
				Keep:     catalogfold.GameRef{Folder: "Eye of the Beholder"},
				Merge:    []catalogfold.GameRef{{Folder: "Advanced Dungeons & Dragons - Eye of the Beholder"}},
				Evidence: []catalogfold.Evidence{{Source: "igdb", Field: "igdb_id", Value: "5384"}},
			},
		},
	}

	output := renderCatalogFoldResult(result, false, false)

	if !strings.Contains(output, "Result: dry run; rerun with --apply to merge candidate groups") {
		t.Fatalf("dry-run result missing summary:\n%s", output)
	}
	if strings.Contains(output, "Next:") || strings.Contains(output, "proposed folds") {
		t.Fatalf("dry-run summary should not repeat details hint:\n%s", output)
	}
}

func TestRenderCatalogFoldResultShowsNothingToMergeWhenNoCandidates(t *testing.T) {
	result := catalogfold.Result{
		DataRoot:      `X:\romsets\metadata`,
		PlatformCount: 1,
		GameCount:     57,
		PlatformResults: []catalogfold.PlatformResult{
			{
				Platform:  "Sega - 32X",
				GameCount: 57,
			},
		},
	}

	output := renderCatalogFoldResult(result, false, false)

	if !strings.Contains(output, "Result: dry run; nothing to merge") {
		t.Fatalf("zero-candidate fold output should say nothing to merge:\n%s", output)
	}
	if strings.Contains(output, "rerun with --apply") {
		t.Fatalf("zero-candidate fold output should not suggest apply:\n%s", output)
	}
}

func TestFormatCatalogFoldEvidenceShowsURLs(t *testing.T) {
	output := strings.Join(formatCatalogFoldEvidenceItems([]catalogfold.Evidence{
		{Source: "igdb", Field: "igdb_id", Value: "1", URL: "https://www.igdb.com/games/doom"},
		{Source: "screenscraper", Field: "screenscraper_id", Value: "752", URL: "https://www.screenscraper.fr/gameinfos.php?gameid=752"},
	}), "\n")

	for _, expected := range []string{
		"igdb.igdb_id=1 -> https://www.igdb.com/games/doom",
		"screenscraper.screenscraper_id=752 -> https://www.screenscraper.fr/gameinfos.php?gameid=752",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("fold evidence output missing %q:\n%s", expected, output)
		}
	}
}

func TestPromptCatalogFoldReviewUsesYesNoSkipQuit(t *testing.T) {
	group := catalogfold.Group{
		Platform: "Sega - Mega Drive - Genesis",
		Keep:     catalogfold.GameRef{Folder: "Sports Talk Baseball"},
		Merge:    []catalogfold.GameRef{{Folder: "Nikkan Sports Pro Yakyuu VAN"}},
		Evidence: []catalogfold.Evidence{{Source: "screenscraper", Field: "screenscraper_id", Value: "752"}},
	}
	tests := []struct {
		input string
		want  catalogfold.ReviewDecision
	}{
		{input: "y\n", want: catalogfold.ReviewApply},
		{input: "n\n", want: catalogfold.ReviewIgnoreEvidence},
		{input: "s\n", want: catalogfold.ReviewSkip},
		{input: "q\n", want: catalogfold.ReviewQuit},
	}
	for _, test := range tests {
		var out strings.Builder
		got, err := promptCatalogFoldReview(
			catalogfold.ReviewPrompt{Index: 1, Total: 1, Group: group},
			bufio.NewReader(strings.NewReader(test.input)),
			&out,
			false,
		)
		if err != nil {
			t.Fatalf("prompt returned error: %v", err)
		}
		if got != test.want {
			t.Fatalf("prompt %q returned %v, want %v", test.input, got, test.want)
		}
		if !strings.Contains(out.String(), "Apply fold? yes/no/skip/quit:") {
			t.Fatalf("prompt output missing question:\n%s", out.String())
		}
	}
}

func TestRenderCatalogFoldStartHintShowsDetailsTipBeforeRun(t *testing.T) {
	output := renderCatalogFoldStartHint(false, false)
	if !strings.Contains(output, "Tip: add --details to review fold candidates and source-ID evidence") {
		t.Fatalf("fold start hint missing details tip:\n%s", output)
	}
	if got := renderCatalogFoldStartHint(true, false); got != "" {
		t.Fatalf("fold start hint should be hidden when details are enabled: %q", got)
	}
}

func TestRenderCatalogFoldPlatformResultShowsPerPlatformSummary(t *testing.T) {
	result := catalogfold.PlatformResult{
		Platform:            "Sega - Mega CD & Sega CD",
		GameCount:           263,
		IdentityCount:       44,
		CandidateGroupCount: 28,
		MergedGameCount:     29,
		ReleaseCount:        36,
		FingerprintCount:    894,
	}

	output := renderCatalogFoldPlatformResult(result, false, false, false)

	for _, expected := range []string{
		"Sega - Mega CD & Sega CD fold summary",
		"Scanned: 263 games",
		"Candidates: 28 groups, 29 game bundles to merge, 44 evidence links",
		"Data to merge: 36 releases, 894 fingerprints",
		"Result: dry run; rerun with --apply to merge candidate groups",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("platform fold output missing %q:\n%s", expected, output)
		}
	}
}

func TestRenderCatalogFoldPlatformResultShowsInteractiveSummary(t *testing.T) {
	result := catalogfold.PlatformResult{
		Platform:            "Sega - Mega Drive - Genesis",
		GameCount:           1570,
		CandidateGroupCount: 3,
		MergedGameCount:     3,
		IdentityCount:       3,
		AppliedGroupCount:   1,
		IgnoredGroupCount:   1,
		IgnoredFileCount:    1,
		ReviewSkippedCount:  1,
	}

	output := renderCatalogFoldPlatformResult(result, false, true, false)

	for _, expected := range []string{
		"Result: reviewed; accepted: 1, rejected: 1, skipped: 1",
		"rejection markers written: 1",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("interactive platform fold output missing %q:\n%s", expected, output)
		}
	}
}

func TestRenderCatalogStatusShowsReadableSummaryAndEmptyGames(t *testing.T) {
	result := catalogstate.Status{
		DataRoot:               `X:\romsets\metadata`,
		PlatformCount:          10,
		GameCount:              9309,
		ReleaseCount:           19608,
		GamesWithSourceData:    9000,
		GamesWithoutSourceData: 309,
		GamesWithSourceMatches: 6500,
		GamesCheckedNoMatches:  2500,
		GamesWithLocalMedia:    12,
		LocalMediaFileCount:    34,
		GamesWithMediaURLs:     7762,
		SourceMediaURLCount:    392173,
		EmptyPlaceholderCount:  2500,
		SourceCounts: []catalogstate.SourceCount{
			{Source: "igdb", GameCount: 9309, MatchedGameCount: 1928, NoMatchGameCount: 7381},
			{Source: "launchbox", GameCount: 9309, MatchedGameCount: 6885, NoMatchGameCount: 2424},
		},
	}

	output := renderCatalogStatus(result, false, false)

	for _, expected := range []string{
		"Catalog status",
		`Library: X:\romsets\metadata`,
		"Catalog: 10 platforms, 9,309 games, 19,608 releases",
		"Metadata: 6,500 (70%) matched, 2,500 (27%) not matched, 309 (3%) not checked",
		"Empty games (no metadata found): 2,500 (27%)",
		"Media: 12 games with files (34 files), 7,762 games with source URLs (392,173 URLs)",
		"Sources",
		"source",
		"matched",
		"not matched",
		"checked",
		"igdb",
		"1,928 (21%)",
		"7,381 (79%)",
		"9,309 (100%)",
		"launchbox",
		"6,885 (74%)",
		"2,424 (26%)",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("catalog status output missing %q:\n%s", expected, output)
		}
	}
	if strings.Contains(output, "Empty placeholders") {
		t.Fatalf("catalog status should use user-facing empty game wording:\n%s", output)
	}
}

func TestRenderCatalogStatusCanUseColor(t *testing.T) {
	result := catalogstate.Status{
		DataRoot:               `X:\romsets\metadata`,
		PlatformCount:          1,
		GameCount:              2,
		ReleaseCount:           3,
		GamesWithSourceData:    2,
		GamesWithSourceMatches: 1,
		GamesCheckedNoMatches:  1,
		EmptyPlaceholderCount:  1,
	}

	output := renderCatalogStatus(result, false, true)

	for _, expected := range []string{
		ansiCyan + "Catalog status" + ansiReset,
		ansiYellow + "Catalog" + ansiReset + ": 1 platforms, 2 games, 3 releases",
		ansiYellow + "Empty games (no metadata found)" + ansiReset + ": " + ansiRed + "1 (50%)" + ansiReset,
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("colored catalog status missing %q:\n%s", expected, output)
		}
	}
}

func TestRenderSourceCoverageReportShowsCheckedMatchedAndEntries(t *testing.T) {
	report := sourceaudit.CoverageReport{
		DataRoot:      `X:\romsets\metadata`,
		PlatformCount: 4,
		GameCount:     1129,
		LowScore:      120,
		Sources: []sourceaudit.SourceCoverage{
			{
				Source:             "igdb",
				GameCount:          1129,
				CoveredGameCount:   1129,
				MatchedGameCount:   900,
				MissingGameCount:   0,
				MatchCount:         940,
				LowConfidenceCount: 11,
			},
			{
				Source:           "launchbox",
				GameCount:        1129,
				CoveredGameCount: 1000,
				MatchedGameCount: 796,
				MissingGameCount: 129,
				MatchCount:       796,
			},
		},
	}

	output := renderSourceCoverageReport(report, false)

	for _, expected := range []string{
		"Source coverage",
		`Library: X:\romsets\metadata`,
		"Catalog: 4 platforms, 1,129 games, 2 sources",
		"Low score threshold: 120",
		"Sources",
		"checked",
		"matched",
		"not matched",
		"missing",
		"entries",
		"igdb",
		"1,129 (100%)",
		"900 (80%)",
		"229 (20%)",
		"940",
		"11",
		"launchbox",
		"1,000 (89%)",
		"129 (11%)",
		"Next: rerun with --details",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("source coverage output missing %q:\n%s", expected, output)
		}
	}
	if strings.Contains(output, "coverage:") {
		t.Fatalf("source coverage should not use ambiguous coverage label:\n%s", output)
	}
}

func TestRenderSourceCoverageReportCanUseColor(t *testing.T) {
	report := sourceaudit.CoverageReport{
		DataRoot:      `X:\romsets\metadata`,
		PlatformCount: 1,
		GameCount:     2,
		LowScore:      120,
		Sources: []sourceaudit.SourceCoverage{
			{Source: "igdb", GameCount: 2, CoveredGameCount: 2, MatchedGameCount: 1, MatchCount: 1, LowConfidenceCount: 1},
		},
	}

	output := renderSourceCoverageReport(report, true)

	for _, expected := range []string{
		ansiCyan + "Source coverage" + ansiReset,
		ansiYellow + "Catalog" + ansiReset + ": 1 platforms, 2 games, 1 sources",
		ansiRed + "1" + ansiReset,
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("colored source coverage missing %q:\n%s", expected, output)
		}
	}
}

func TestRenderDATPlanCanUseColor(t *testing.T) {
	plan := dat.Plan{
		InputPath:        "mame.xml",
		Container:        "xml",
		Format:           "mame-listxml",
		Authority:        "mame",
		DatasetName:      "MAME",
		PlatformName:     "arcade",
		MachineCount:     10,
		PlayableCount:    8,
		BIOSCount:        1,
		DeviceCount:      1,
		ROMCount:         1000,
		DiskCount:        3,
		SampleCount:      2,
		CloneLinkCount:   4,
		HashCounts:       dat.HashCounts{CRC32: 1000, SHA1: 1000},
		PlatformOverride: true,
		SampleGames: []dat.GameSample{
			{Name: "sf2", Description: "Street Fighter II", CloneOf: "sf2ce", Runnable: "no"},
		},
	}

	output := renderDATPlan(plan, true)

	for _, expected := range []string{
		ansiCyan + "DAT import plan" + ansiReset,
		ansiCyan + "Source" + ansiReset,
		ansiYellow + "format" + ansiReset + ": mame-listxml",
		ansiCyan + "Catalog" + ansiReset,
		ansiYellow + "platform" + ansiReset + ": arcade (override)",
		ansiCyan + "Contents" + ansiReset,
		"10 machines, 8 runnable candidates, 1 BIOS, 1 device",
		"1,000 ROMs, 3 disks/CHDs, 2 samples",
		ansiCyan + "Sample machines" + ansiReset,
		"sf2 - Street Fighter II (not runnable, clone of sf2ce)",
		ansiYellow + "Note" + ansiReset + ": dat import automatically classifies MAME systems",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("colored DAT plan missing %q:\n%s", expected, output)
		}
	}
}

func TestNestedHelpCommandsReturnNil(t *testing.T) {
	tests := []struct {
		name string
		run  func([]string) error
		args []string
	}{
		{name: "init", run: runInit, args: []string{"--help"}},
		{name: "catalog status", run: runCatalogStatus, args: []string{"--help"}},
		{name: "catalog prune", run: runCatalogPruneEmpty, args: []string{"--help"}},
		{name: "catalog fold", run: runCatalogFold, args: []string{"--help"}},
		{name: "sources", run: runSources, args: []string{"--help"}},
		{name: "sources list", run: runSourcesList, args: []string{"--help"}},
		{name: "sources coverage", run: runSourcesCoverage, args: []string{"--help"}},
		{name: "serve", run: runServe, args: []string{"--help"}},
		{name: "dat", run: runDAT, args: []string{"--help"}},
		{name: "dat import", run: runDATImport, args: []string{"--help"}},
		{name: "dat plan", run: runDATPlan, args: []string{"--help"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(test.args); err != nil {
				t.Fatalf("help returned error: %v", err)
			}
		})
	}
}
