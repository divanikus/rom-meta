package emumovies

import (
	"path/filepath"
	"sort"
	"strings"
)

// mediaGameIndex narrows the existing scorer to games sharing an exact release
// key or a base title key. It does not change scores or resolve ambiguous ties.
type mediaGameIndex struct {
	games       []catalogGame
	exact, base map[string][]int
}

func newMediaGameIndex(games []catalogGame) mediaGameIndex {
	index := mediaGameIndex{games: games, exact: map[string][]int{}, base: map[string][]int{}}
	add := func(target map[string][]int, keys []string, game int) {
		for _, key := range keys {
			items := target[key]
			if len(items) == 0 || items[len(items)-1] != game {
				target[key] = append(items, game)
			}
		}
	}
	for i, game := range games {
		addBase := func(name string) {
			add(index.base, titleMatchKeys(name), i)
			add(index.base, titleMatchKeys(catalogMediaName(game.Platform, "", name)), i)
		}
		addBase(game.Title)
		for _, alias := range game.Aliases {
			addBase(alias)
		}
		for _, release := range game.Releases {
			add(index.exact, exactTitleKeys(release.Name), i)
			add(index.exact, exactTitleKeys(release.MAMESet), i)
			addBase(release.Title)
			addBase(release.Name)
		}
	}
	return index
}

func (index mediaGameIndex) candidates(fileName string) []catalogGame {
	name := filepath.Base(fileName)
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	seen := map[int]bool{}
	for _, key := range exactTitleKeys(stem) {
		for _, i := range index.exact[key] {
			seen[i] = true
		}
	}
	for _, key := range titleMatchKeys(stem) {
		for _, i := range index.base[key] {
			seen[i] = true
		}
	}
	ordered := make([]int, 0, len(seen))
	for i := range seen {
		ordered = append(ordered, i)
	}
	sort.Ints(ordered)
	games := make([]catalogGame, 0, len(ordered))
	for _, i := range ordered {
		games = append(games, index.games[i])
	}
	return games
}
