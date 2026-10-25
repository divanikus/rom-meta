package medianames

import (
	"strings"
	"testing"
)

func TestVariantQualifierKeepsRegionLanguageAndSourcePositions(t *testing.T) {
	for _, item := range []struct{ name, want, clean string }{
		{"box-front-01.usa.en.variant-original.launchbox", "original", "box-front-01.usa.en.launchbox"},
		{"box-front.variant-reconstructed.launchbox", "reconstructed", "box-front.launchbox"},
		{"box-front.usa.variant-fanart.launchbox", "fanart", "box-front.usa.launchbox"},
		{"box-front.usa.variant-other.launchbox", "unknown", "box-front.usa.launchbox"},
		{"box-front.usa.launchbox", "unknown", "box-front.usa.launchbox"},
		{"box-front", "unknown", "box-front"},
	} {
		variant, clean := SplitVariant(strings.Split(item.name, "."))
		if variant != item.want || strings.Join(clean, ".") != item.clean {
			t.Fatal(item, variant, clean)
		}
	}
}
