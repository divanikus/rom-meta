package dat

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
	"rommeta/internal/catalogfs"
	"rommeta/internal/catalogyaml"
	"rommeta/internal/contentidentity"
)

type platformMerge struct {
	normalized    []gameBundle
	changes       ImportChanges
	newToExisting map[string]string
}

type existingBundle struct {
	contentKey        string
	labelContent      []contentidentity.Profile
	content           []contentidentity.Profile
	arcadeFamily      string
	folder            string
	entryIDs          map[string]bool
	entryNames        map[string]bool
	releaseSignatures map[string]bool
}

type newBundleIdentity struct {
	content           []contentidentity.Profile
	arcadeFamily      string
	folder            string
	entryIDs          map[string]bool
	entryNames        map[string]bool
	releaseSignatures map[string]bool
}

type existingBundleIndex struct {
	byFolder           map[string]*existingBundle
	byEntryID          map[string][]*existingBundle
	byEntryName        map[string][]*existingBundle
	byReleaseSignature map[string][]*existingBundle
}

func planPlatformMerge(platformPath string, plan Plan, bundles []gameBundle, progress ProgressFunc) (platformMerge, error) {
	merge := platformMerge{
		changes: ImportChanges{
			Action: "create",
			Added:  len(bundles),
		},
		newToExisting: map[string]string{},
	}
	info, err := catalogfs.Stat(platformPath)
	if err != nil {
		if os.IsNotExist(err) {
			reportProgress(progress, ImportProgress{
				Stage:   "checking",
				Current: len(bundles),
				Total:   len(bundles),
				Item:    plan.PlatformName,
			})
			return merge, nil
		}
		return platformMerge{}, err
	}
	if !info.IsDir() {
		return platformMerge{}, fmt.Errorf("platform path is not a directory: %s", platformPath)
	}

	existing, err := readExistingBundles(platformPath, progress)
	if err != nil {
		return platformMerge{}, err
	}
	merge.changes = ImportChanges{Action: "merge"}
	usedExisting := map[string]newBundleIdentity{}
	existingIndex := indexExistingBundles(existing)
	bundles, err = partitionKnownReviewedBundles(plan, bundles, existingIndex)
	if err != nil {
		return platformMerge{}, err
	}
	merge.normalized = bundles

	for index, bundle := range bundles {
		reportProgress(progress, ImportProgress{
			Stage:   "matching",
			Current: index + 1,
			Total:   len(bundles),
			Item:    bundle.Folder,
		})
		incoming, err := identifyNewBundle(plan, bundle)
		if err != nil {
			return platformMerge{}, err
		}
		if reason := reviewedAnchorConflict(incoming, existingIndex); reason != "" {
			merge.changes.Conflicts = append(merge.changes.Conflicts, reason)
			continue
		}
		candidates := strongestCandidates(incoming, existingIndex)
		chosen, conflict := chooseExistingBundle(incoming, candidates, existingIndex.byFolder)
		if conflict != "" {
			merge.changes.Conflicts = append(merge.changes.Conflicts, conflict)
			continue
		}
		if chosen == nil {
			merge.changes.Added++
			continue
		}
		if other, used := usedExisting[chosen.folder]; used && (!incomingHasKnownRelease(incoming, chosen) || !incomingHasKnownRelease(other, chosen)) {
			merge.changes.Conflicts = append(merge.changes.Conflicts,
				fmt.Sprintf("incoming games %q and %q both match existing game %q", other.folder, bundle.Folder, chosen.folder))
			continue
		}
		usedExisting[chosen.folder] = incoming
		merge.newToExisting[bundle.Folder] = chosen.folder
		merge.changes.Reconciled++
	}

	sort.Strings(merge.changes.Conflicts)
	return merge, nil
}

func readExistingBundles(platformPath string, progress ProgressFunc) ([]existingBundle, error) {
	entries, err := catalogfs.ReadDir(platformPath)
	if err != nil {
		return nil, err
	}
	gameDirectories := make([]os.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			gameDirectories = append(gameDirectories, entry)
		}
	}
	var bundles []existingBundle
	for index, entry := range gameDirectories {
		reportProgress(progress, ImportProgress{
			Stage:   "checking existing",
			Current: index + 1,
			Total:   len(gameDirectories),
			Item:    entry.Name(),
		})
		path := filepath.Join(platformPath, entry.Name())
		gameContents, err := catalogfs.ReadFile(filepath.Join(path, "game.yaml"))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		fingerprintContents, err := catalogfs.ReadFile(filepath.Join(path, "fingerprints.yaml"))
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		identity, err := identifyExistingBundle(entry.Name(), gameContents, fingerprintContents)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		bundles = append(bundles, identity)
	}
	return bundles, nil
}

func identifyExistingBundle(folder string, gameContents, fingerprintContents []byte) (existingBundle, error) {
	document, err := catalogyaml.Parse(gameContents)
	if err != nil {
		return existingBundle{}, err
	}
	result := existingBundle{
		contentKey:        catalogyaml.Scalar(catalogyaml.Value(catalogyaml.Value(document.Root(), "fold"), "content_key")),
		arcadeFamily:      catalogyaml.Scalar(catalogyaml.Value(catalogyaml.Value(document.Root(), "arcade"), "platform")),
		folder:            folder,
		entryIDs:          map[string]bool{},
		entryNames:        map[string]bool{},
		releaseSignatures: map[string]bool{},
	}
	releases := catalogyaml.Value(document.Root(), "releases")
	if err := catalogyaml.Mapping(releases, func(name string, value *yaml.Node) error {
		profile := contentidentity.Release(name, catalogyaml.Scalar(catalogyaml.Value(document.Root(), "title")), catalogyaml.Strings(catalogyaml.Value(value, "tags"))...)
		result.content = append(result.content, profile)
		authority := catalogyaml.Value(value, "authority")
		if authority == nil {
			return nil
		}
		source := catalogyaml.Scalar(catalogyaml.Value(authority, "source"))
		if entryID := scopedIdentity(source, catalogyaml.Scalar(catalogyaml.Value(authority, "entry_id"))); entryID != "" {
			result.entryIDs[entryID] = true
		}
		if entryName := normalizedIdentity(catalogyaml.Scalar(catalogyaml.Value(authority, "entry_name"))); entryName != "" {
			result.entryNames[entryName] = true
		}
		return nil
	}); err != nil {
		return existingBundle{}, err
	}
	result.labelContent = append([]contentidentity.Profile(nil), result.content...)
	// Saved content taxonomy is a negative boundary during reconciliation too.
	if err := catalogyaml.Mapping(catalogyaml.Value(document.Root(), "sources"), func(source string, stanza *yaml.Node) error {
		return catalogyaml.Sequence(catalogyaml.Value(stanza, "matches"), func(match *yaml.Node) error {
			if source == "igdb" && len(catalogyaml.Strings(catalogyaml.Value(match, "platforms"))) == 0 {
				return nil
			}
			kind := contentidentity.SourceKind(catalogyaml.Scalar(catalogyaml.Value(match, "game_type")))
			if kind == "" {
				return nil
			}
			i := 0
			return catalogyaml.Mapping(releases, func(name string, _ *yaml.Node) error {
				if catalogyaml.MatchIncludesRelease(match, name) {
					result.content = contentidentity.ApplyKind(result.content, i, kind)
				}
				i++
				return nil
			})
		})
	}); err != nil {
		return existingBundle{}, err
	}
	if len(fingerprintContents) > 0 {
		signatures, err := fingerprintReleaseSignatures(fingerprintContents)
		if err != nil {
			return existingBundle{}, err
		}
		result.releaseSignatures = signatures
	}
	return result, nil
}

func identifyNewBundle(plan Plan, bundle gameBundle) (newBundleIdentity, error) {
	result := newBundleIdentity{
		folder:            bundle.Folder,
		entryIDs:          map[string]bool{},
		entryNames:        map[string]bool{},
		releaseSignatures: map[string]bool{},
	}
	if plan.ArcadeSnapshot != "" && len(bundle.Releases) > 0 {
		result.arcadeFamily = plan.ArcadeFamilies[bundle.Releases[0].Name].Platform
	}
	for _, release := range bundle.Releases {
		_, profile := contentidentity.Analyze(release.Name)
		result.content = append(result.content, profile)
		if id := scopedIdentity(authorityKey(plan), release.ID); id != "" {
			result.entryIDs[id] = true
		}
		if plan.ArcadeSnapshot != "" {
			continue
		}
		if name := normalizedIdentity(release.Name); name != "" {
			result.entryNames[name] = true
		}
		roms, err := gameROMs(release)
		if err != nil {
			return newBundleIdentity{}, fmt.Errorf("read fingerprints for %s: %w", release.Name, err)
		}
		for _, signature := range romReleaseSignatures(roms) {
			result.releaseSignatures[signature] = true
		}
	}
	return result, nil
}

func indexExistingBundles(existing []existingBundle) existingBundleIndex {
	index := existingBundleIndex{
		byFolder:           map[string]*existingBundle{},
		byEntryID:          map[string][]*existingBundle{},
		byEntryName:        map[string][]*existingBundle{},
		byReleaseSignature: map[string][]*existingBundle{},
	}
	for bundleIndex := range existing {
		bundle := &existing[bundleIndex]
		index.byFolder[bundle.folder] = bundle
		indexBundleValues(index.byEntryID, bundle.entryIDs, bundle)
		indexBundleValues(index.byEntryName, bundle.entryNames, bundle)
		indexBundleValues(index.byReleaseSignature, bundle.releaseSignatures, bundle)
	}
	return index
}

func indexBundleValues(index map[string][]*existingBundle, values map[string]bool, bundle *existingBundle) {
	for value := range values {
		index[value] = append(index[value], bundle)
	}
}

func strongestCandidates(incoming newBundleIdentity, index existingBundleIndex) []*existingBundle {
	if candidates := indexedCandidates(incoming.entryIDs, index.byEntryID); len(candidates) > 0 {
		return candidates
	}
	if candidates := indexedCandidates(incoming.releaseSignatures, index.byReleaseSignature); len(candidates) > 0 {
		return candidates
	}
	return indexedCandidates(incoming.entryNames, index.byEntryName)
}

func indexedCandidates(incoming map[string]bool, index map[string][]*existingBundle) []*existingBundle {
	if len(incoming) == 0 {
		return nil
	}
	var candidates []*existingBundle
	seen := map[string]bool{}
	for value := range incoming {
		for _, bundle := range index[value] {
			if seen[bundle.folder] {
				continue
			}
			seen[bundle.folder] = true
			candidates = append(candidates, bundle)
		}
	}
	sort.Slice(candidates, func(left, right int) bool {
		return candidates[left].folder < candidates[right].folder
	})
	return candidates
}

func chooseExistingBundle(
	incoming newBundleIdentity,
	candidates []*existingBundle,
	byFolder map[string]*existingBundle,
) (*existingBundle, string) {
	if len(candidates) > 1 {
		return nil, fmt.Sprintf("incoming game %q matches multiple existing games: %s",
			incoming.folder, quotedFolders(candidates))
	}
	if len(candidates) == 1 {
		if incoming.arcadeFamily != "" && candidates[0].arcadeFamily != "" && incoming.arcadeFamily != candidates[0].arcadeFamily {
			return nil, fmt.Sprintf("incoming game %q changes arcade family from %q to %q; requires review", incoming.folder, candidates[0].arcadeFamily, incoming.arcadeFamily)
		}
		if incoming.arcadeFamily == "" {
			if reason := reconciliationContentReason(incoming, candidates[0]); reason != "" {
				return nil, fmt.Sprintf("incoming game %q: %s in %q", incoming.folder, reason, candidates[0].folder)
			}
		}
		return candidates[0], ""
	}
	if exact := byFolder[incoming.folder]; exact != nil {
		if hasIdentity(exact) {
			return nil, fmt.Sprintf("incoming game %q reuses an existing folder without matching its DAT identity", incoming.folder)
		}
		if incoming.arcadeFamily == "" {
			if reason := reconciliationContentReason(incoming, exact); reason != "" {
				return nil, fmt.Sprintf("incoming game %q: %s", incoming.folder, reason)
			}
		}
		return exact, ""
	}
	return nil, ""
}

func hasIdentity(bundle *existingBundle) bool {
	return bundle != nil && (len(bundle.entryIDs) > 0 || len(bundle.entryNames) > 0 || len(bundle.releaseSignatures) > 0)
}

func quotedFolders(bundles []*existingBundle) string {
	names := make([]string, 0, len(bundles))
	for _, bundle := range bundles {
		names = append(names, fmt.Sprintf("%q", bundle.folder))
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func normalizedIdentity(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func scopedIdentity(source, value string) string {
	source = normalizedIdentity(source)
	value = normalizedIdentity(value)
	if source == "" || value == "" {
		return ""
	}
	return source + "\x00" + value
}

func fingerprintReleaseSignatures(contents []byte) (map[string]bool, error) {
	document, err := catalogyaml.Parse(contents)
	if err != nil {
		return nil, err
	}
	itemsByRelease := map[string][]*yaml.Node{}
	items := catalogyaml.Value(document.Root(), "items")
	if items == nil {
		items = catalogyaml.Value(document.Root(), "roms")
	}
	if err := catalogyaml.Sequence(items, func(item *yaml.Node) error {
		release := catalogyaml.Scalar(catalogyaml.Value(item, "release"))
		if release == "" {
			return nil
		}
		itemsByRelease[release] = append(itemsByRelease[release], item)
		return nil
	}); err != nil {
		return nil, err
	}
	result := map[string]bool{}
	for _, items := range itemsByRelease {
		for _, signature := range fingerprintNodeReleaseSignatures(items) {
			result[signature] = true
		}
	}
	return result, nil
}

func fingerprintNodeHash(item *yaml.Node) string {
	file := catalogyaml.Value(item, "file")
	for _, field := range []string{"sha256", "sha1", "md5"} {
		if value := normalizedIdentity(catalogyaml.Scalar(catalogyaml.Value(file, field))); value != "" {
			return field + ":" + value
		}
	}
	crc := normalizedIdentity(catalogyaml.Scalar(catalogyaml.Value(file, "crc32")))
	size := strings.TrimSpace(catalogyaml.Scalar(catalogyaml.Value(file, "size")))
	if crc != "" && size != "" {
		return "crc32:" + crc + ":" + size
	}
	return ""
}

func fingerprintNodeReleaseSignatures(items []*yaml.Node) []string {
	if len(items) == 0 {
		return nil
	}
	var signatures []string
	for _, field := range []string{"sha256", "sha1", "md5"} {
		parts := make([]string, 0, len(items))
		for _, item := range items {
			file := catalogyaml.Value(item, "file")
			value := normalizedIdentity(catalogyaml.Scalar(catalogyaml.Value(file, field)))
			if value == "" {
				parts = nil
				break
			}
			parts = append(parts, field+":"+value)
		}
		if signature := releaseSignature(parts); signature != "" {
			signatures = append(signatures, signature)
		}
	}
	crcParts := make([]string, 0, len(items))
	for _, item := range items {
		file := catalogyaml.Value(item, "file")
		crc := normalizedIdentity(catalogyaml.Scalar(catalogyaml.Value(file, "crc32")))
		size := strings.TrimSpace(catalogyaml.Scalar(catalogyaml.Value(file, "size")))
		if crc == "" || size == "" {
			crcParts = nil
			break
		}
		crcParts = append(crcParts, "crc32:"+crc+":"+size)
	}
	if signature := releaseSignature(crcParts); signature != "" {
		signatures = append(signatures, signature)
	}
	return signatures
}

func romReleaseSignatures(roms []datROM) []string {
	if len(roms) == 0 {
		return nil
	}
	var signatures []string
	for _, algorithm := range []struct {
		name  string
		value func(datROM) string
	}{
		{name: "sha256", value: func(rom datROM) string { return rom.SHA256 }},
		{name: "sha1", value: func(rom datROM) string { return rom.SHA1 }},
		{name: "md5", value: func(rom datROM) string { return rom.MD5 }},
	} {
		parts := make([]string, 0, len(roms))
		for _, rom := range roms {
			value := normalizedIdentity(algorithm.value(rom))
			if value == "" {
				parts = nil
				break
			}
			parts = append(parts, algorithm.name+":"+value)
		}
		if signature := releaseSignature(parts); signature != "" {
			signatures = append(signatures, signature)
		}
	}
	crcParts := make([]string, 0, len(roms))
	for _, rom := range roms {
		if rom.CRC32 == "" || rom.Size == "" {
			crcParts = nil
			break
		}
		crcParts = append(crcParts, "crc32:"+normalizedIdentity(rom.CRC32)+":"+strings.TrimSpace(rom.Size))
	}
	if signature := releaseSignature(crcParts); signature != "" {
		signatures = append(signatures, signature)
	}
	return signatures
}

func releaseSignature(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

func (merge platformMerge) existingFolder(newFolder string) string {
	if existing := merge.newToExisting[newFolder]; existing != "" {
		return existing
	}
	return newFolder
}

func mergeConflictError(conflicts []string) error {
	const shown = 5
	var details []string
	for index, conflict := range conflicts {
		if index >= shown {
			break
		}
		details = append(details, conflict)
	}
	message := fmt.Sprintf("DAT import has %d unresolved match(es): %s", len(conflicts), strings.Join(details, "; "))
	if len(conflicts) > shown {
		message += fmt.Sprintf("; ... %d more", len(conflicts)-shown)
	}
	return fmt.Errorf("%s; no catalog files were changed", message)
}

// Reuse an already folded destination only when each incoming bundle contains a persisted exact authority name.
// A common checksum or broad provider ID alone cannot approve another bundle.
func incomingHasKnownRelease(incoming newBundleIdentity, existing *existingBundle) bool {
	for name := range incoming.entryNames {
		if existing.entryNames[name] {
			return true
		}
	}
	return false
}

func reconciliationContentReason(incoming newBundleIdentity, existing *existingBundle) string {
	labels := existing.labelContent
	if labels == nil {
		labels = existing.content
	}
	allKnown := len(incoming.entryNames) > 0
	for name := range incoming.entryNames {
		if !existing.entryNames[name] {
			allKnown = false
		}
	}
	// A durable reviewed destination already owns these exact authority names.
	// Refreshing that ownership must not reinterpret the regional edition labels
	// that explicit review resolved. Unknown additions still take the conservative
	// path below; conflicting saved authority IDs are checked by the caller.
	if allKnown && existing.contentKey != "" {
		return ""
	}
	if allKnown && contentidentity.KnownBoundary(incoming.content, labels) == "" && contentidentity.KnownBoundary(existing.content) == "" {
		return ""
	}
	if reason := contentidentity.Boundary(incoming.content, labels); reason != "" {
		return reason
	}
	if reason := contentidentity.Boundary(existing.content); reason != "" {
		return reason
	}
	// A saved exact DAT anchor inherits the card's descriptive provider type.
	// Its absence from DAT is not evidence that an unchanged edition became retail.
	if incomingHasKnownRelease(incoming, existing) {
		return ""
	}
	return contentidentity.Boundary(incoming.content, existing.content)
}

// A changed DAT ID must not hide an exact saved authority name in another reviewed
// partition merely because the ordinary scorer prefers IDs over other anchors.
func reviewedAnchorConflict(incoming newBundleIdentity, index existingBundleIndex) string {
	keys := map[string]bool{}
	for _, candidates := range [][]*existingBundle{
		indexedCandidates(incoming.entryIDs, index.byEntryID),
		indexedCandidates(incoming.entryNames, index.byEntryName),
	} {
		for _, existing := range candidates {
			if existing.contentKey != "" {
				keys[existing.contentKey] = true
			}
		}
	}
	if len(keys) > 1 {
		return fmt.Sprintf("incoming game %q crosses different reviewed content identities; requires review", incoming.folder)
	}
	return ""
}
