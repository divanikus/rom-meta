package openvgdb

import (
	"net/url"
	"path"
	"strconv"
	"strings"

	"rommeta/internal/contentidentity"
	"rommeta/internal/regions"
	"rommeta/internal/titlematch"
)

func referenceKey(value string) string {
	u, err := url.Parse(value)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Host), "www.") + strings.TrimSuffix(u.Path, "/")
}

// Some snapshot rows reuse another edition's external identity and artwork.
// Reject a contradictory numbered title only when a peer on the same reference
// agrees with its independently supplied URL title. Missing/opaque references
// and regional names without comparable numbers are not guessed.
func conflictingReference(candidate releaseMetadata, peers []releaseMetadata) bool {
	return referenceConflict(candidate, peers, true)
}

func referenceConflict(candidate releaseMetadata, peers []releaseMetadata, sameSeries bool) bool {
	if len(peers) < 2 {
		return false
	}
	u, err := url.Parse(candidate.ReferenceURL)
	if err != nil {
		return false
	}
	_, slug, ok := strings.Cut(path.Base(u.Path), "-")
	if !ok {
		return false
	}
	id, _, _ := strings.Cut(path.Base(u.Path), "-")
	if _, err := strconv.Atoi(id); err != nil {
		return false
	}
	refNumbers, refSymbols := numberedTitle(slug)
	ownNumbers, ownSymbols := numberedTitle(candidate.Title)
	if refNumbers == "" || ownNumbers == "" || refNumbers == ownNumbers && refSymbols == ownSymbols {
		return false
	}
	for _, peer := range peers {
		numbers, symbols := numberedTitle(peer.Title)
		if numbers == refNumbers && symbols == refSymbols && (!sameSeries || sameKnownRegion(candidate.Region, peer.Region) && numberlessTitle(candidate.Title) == numberlessTitle(peer.Title)) {
			return true
		}
	}
	return false
}

func numberedTitle(value string) (string, string) {
	value, _ = contentidentity.Analyze(value)
	var numbers, symbols []string
	roman := map[string]string{"i": "1", "ii": "2", "iii": "3", "iv": "4", "v": "5", "vi": "6", "vii": "7", "viii": "8", "ix": "9", "x": "10", "xi": "11", "xii": "12", "xiii": "13", "xiv": "14", "xv": "15", "xvi": "16", "xvii": "17", "xviii": "18", "xix": "19", "xx": "20"}
	for _, word := range strings.Fields(titlematch.Key(value)) {
		if number, err := strconv.Atoi(word); err == nil {
			numbers = append(numbers, strconv.Itoa(number))
		} else if n := roman[word]; n != "" {
			numbers = append(numbers, n)
		} else if word == "prime" || word == "plus" {
			symbols = append(symbols, word)
		}
	}
	return strings.Join(numbers, ","), strings.Join(symbols, ",")
}

// A changed product subtitle or regional series name is not a contradiction
// merely because it contains different numbers.
func numberlessTitle(value string) string {
	value, _ = contentidentity.Analyze(value)
	var words []string
	for _, word := range strings.Fields(titlematch.Key(value)) {
		n, s := numberedTitle(word)
		if n != "" || s != "" || word == "and" {
			continue
		}
		words = append(words, word)
	}
	return strings.Join(words, " ")
}

func sameKnownRegion(a, b string) bool {
	left, ok := regions.NormalizeDATDisplay(regions.NormalizeDisplay(a))
	if !ok || left == "World" {
		return false
	}
	right, ok := regions.NormalizeDATDisplay(regions.NormalizeDisplay(b))
	return ok && left == right
}
