package medianames

import (
	"reflect"
	"testing"
)

func TestGamelistCollectionSource(t *testing.T) {
	for _, test := range []struct{ in, want []string }{
		{[]string{"box-front", "gamelist", "sample"}, []string{"box-front", "gamelist.sample"}},
		{[]string{"box-front", "usa", "en", "gamelist", "sample"}, []string{"box-front", "usa", "en", "gamelist.sample"}},
		{[]string{"box-front", "usa", "launchbox"}, []string{"box-front", "usa", "launchbox"}},
	} {
		_, got := SplitVariant(test.in)
		if !reflect.DeepEqual(got, test.want) {
			t.Fatalf("%v => %v", test.in, got)
		}
	}
}
