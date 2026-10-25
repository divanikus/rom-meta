package catalogyaml

import "gopkg.in/yaml.v3"

// MatchIncludesRelease applies the union of exact release and releases scopes.
// An absent scope is game-wide; an explicitly empty scope matches no release.
func MatchIncludesRelease(match *yaml.Node, release string) bool {
	single := Value(match, "release")
	many := Value(match, "releases")
	if single == nil && many == nil {
		return true
	}
	if Scalar(single) == release {
		return true
	}
	for _, name := range Strings(many) {
		if name == release {
			return true
		}
	}
	return false
}
