package main

import "testing"

func TestLaunchBoxMergeArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"merge", "--help"}} {
		if err := runLaunchBox(args); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"missing"}, {"merge"}, {"merge", "--package", "x"}, {"merge", "--unknown"}, {"merge", "unexpected"}} {
		if err := runLaunchBox(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
