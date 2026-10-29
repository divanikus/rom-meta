package catalogcache

import (
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"rommeta/internal/catalogyaml"
	"rommeta/internal/regions"
)

func parseGameYAML(contents []byte) (Game, error) {
	document, err := catalogyaml.Parse(contents)
	if err != nil {
		return Game{}, err
	}
	if _, err := catalogyaml.Variants(document.Root()); err != nil {
		return Game{}, err
	}
	game := gameFromDocument(document)
	game.PreservedSources, err = catalogyaml.PreservedSources(document.Root())
	return game, err
}

func gameFromDocument(document *catalogyaml.Document) Game {
	root := document.Root()
	game := Game{
		ArcadePlatform: catalogyaml.Scalar(catalogyaml.Value(catalogyaml.Value(root, "arcade"), "platform")),
		Title:          catalogyaml.Scalar(catalogyaml.Value(root, "title")),
		DefaultRelease: catalogyaml.Scalar(catalogyaml.Value(root, "default_release")),
	}
	game.Variants, _ = catalogyaml.Variants(root)
	for _, key := range []string{"alternate_titles", "aliases"} {
		for _, alias := range catalogyaml.Strings(catalogyaml.Value(root, key)) {
			game.Aliases = appendUnique(game.Aliases, alias)
		}
	}
	_ = catalogyaml.Mapping(catalogyaml.Value(root, "releases"), func(name string, value *yaml.Node) error {
		release := Release{Name: name, Regions: regionTags(name), Tags: releaseTags(name, nil)}
		release.Title = catalogyaml.Scalar(catalogyaml.Value(value, "title"))
		if values := catalogyaml.Strings(catalogyaml.Value(value, "regions")); len(values) > 0 {
			release.Regions = values
		}
		release.Tags = releaseTags(name, catalogyaml.Strings(catalogyaml.Value(value, "tags")))

		authority := catalogyaml.Value(value, "authority")
		if catalogyaml.Scalar(catalogyaml.Value(authority, "source")) == "mame" {
			release.MAMESet = catalogyaml.Scalar(catalogyaml.Value(authority, "entry_id"))
			release.Tags = releaseTags(release.Title, release.Tags)
			manufacturer := catalogyaml.Scalar(catalogyaml.Value(catalogyaml.Value(value, "mame"), "manufacturer"))
			qualifiers := manufacturer
			for _, group := range tagValuePattern.FindAllString(release.Title, -1) {
				qualifiers += " " + group
			}
			for _, marker := range arcadeUnofficialPattern.FindAllString(strings.ToLower(qualifiers), -1) {
				release.Tags = appendUnique(release.Tags, marker)
			}
		}
		game.Releases = append(game.Releases, release)
		return nil
	})
	mediaOrder := 0
	_ = catalogyaml.Mapping(catalogyaml.Value(root, "sources"), func(source string, value *yaml.Node) error {
		game.Sources = appendUnique(game.Sources, source)
		stat := SourceStat{Source: source, Status: catalogyaml.Scalar(catalogyaml.Value(value, "status"))}
		matches := catalogyaml.Value(value, "matches")
		_ = catalogyaml.Sequence(matches, func(match *yaml.Node) error {
			stat.MatchCount++
			parseSourceMatchMedia(&game, source, match, &mediaOrder)
			return nil
		})
		game.SourceStats = append(game.SourceStats, stat)
		return nil
	})
	sort.Strings(game.Sources)
	return game
}

var arcadeUnofficialPattern = regexp.MustCompile(`\b(?:bootleg|hack)\b`)

func parseSourceMatchMedia(game *Game, source string, match *yaml.Node, order *int) {
	directRelease := catalogyaml.Scalar(catalogyaml.Value(match, "release"))
	parseMediaMap(game, source, directRelease, catalogyaml.Value(match, "media"), order)
	releases := catalogyaml.Value(match, "releases")
	if releases == nil || releases.Kind != yaml.SequenceNode {
		return
	}
	for _, item := range releases.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		release := catalogyaml.Scalar(catalogyaml.Value(item, "release"))
		parseMediaMap(game, source, release, catalogyaml.Value(item, "media"), order)
	}
}

func parseMediaMap(game *Game, source, release string, media *yaml.Node, order *int) {
	_ = catalogyaml.Mapping(media, func(mediaType string, candidates *yaml.Node) error {
		_ = catalogyaml.Sequence(candidates, func(candidate *yaml.Node) error {
			url := catalogyaml.Scalar(catalogyaml.Value(candidate, "url"))
			if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
				return nil
			}
			*order++
			item := MediaURL{
				Source:    source,
				Release:   release,
				MediaType: mediaType,
				URL:       url,
				Order:     *order,
			}
			item.Region = regions.NormalizeDisplay(catalogyaml.Scalar(catalogyaml.Value(candidate, "region")))
			item.Language = catalogyaml.Scalar(catalogyaml.Value(candidate, "language"))
			item.Quality = catalogyaml.Scalar(catalogyaml.Value(candidate, "quality"))
			item.Format = catalogyaml.Scalar(catalogyaml.Value(candidate, "format"))
			item.SourceID = catalogyaml.Scalar(catalogyaml.Value(candidate, "source_id"))
			item.SourceFile = catalogyaml.Scalar(catalogyaml.Value(candidate, "source_file"))
			item.SourceType = catalogyaml.Scalar(catalogyaml.Value(candidate, "source_type"))
			item.CRC32 = catalogyaml.Scalar(catalogyaml.Value(candidate, "crc32"))
			item.MD5 = catalogyaml.Scalar(catalogyaml.Value(candidate, "md5"))
			item.SHA1 = catalogyaml.Scalar(catalogyaml.Value(candidate, "sha1"))
			item.Size = catalogyaml.Int64(catalogyaml.Value(candidate, "size"))
			game.MediaURLs = append(game.MediaURLs, item)
			return nil
		})
		return nil
	})
}
