package main

import "testing"

func TestCatalogWriterScope(t *testing.T) {
	for _, tt := range []struct {
		args  []string
		root  string
		write bool
	}{
		{[]string{"catalog", "fold", "--data-root", "/catalog", "--apply"}, "/catalog", true},
		{[]string{"catalog", "fold", "--data-root=/catalog", "--interactive"}, "/catalog", true},
		{[]string{"fetch", "--data-root=/catalog", "--apply=true"}, "/catalog", true},
		{[]string{"catalog", "fold", "--apply", "--help"}, "", false},
		{[]string{"catalog", "fold", "--data-root", "/catalog"}, "/catalog", false},
		{[]string{"export", "--apply", "--data-root", "/catalog"}, "", false},
		{[]string{"serve", "--edit", "--data-root", "/catalog"}, "", false},
	} {
		root, write := catalogWriterRoot(tt.args)
		if root != tt.root || write != tt.write {
			t.Fatal(tt.args, root, write)
		}
	}
}
