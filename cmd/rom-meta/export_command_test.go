package main

import (
	"strings"
	"testing"
)

func TestExportHelpAndArgumentValidation(t *testing.T) {
	if err := runExport([]string{"--list-systems"}); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--all", "--platform", "n64"}, {"--all", "--platforms", "n64"}, {"--platforms", "n64,"}} {
		if err := runExport(args); err == nil {
			t.Fatalf("accepted conflicting/bad mode: %v", args)
		}
	}
	if err := runExport([]string{"--help"}); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--target", "missing"}, {"--unknown"}, {"--limit", "-1"}, {"unexpected"}} {
		if err := runExport(args); err == nil {
			t.Fatalf("accepted bad args: %v", args)
		}
	}
	if err := runExport([]string{"--target", "es-de"}); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("missing inputs accepted: %v", err)
	}
}

func TestLaunchBoxExportArgumentValidation(t *testing.T) {
	if err := runExport([]string{"--target", "launchbox", "--list-systems"}); err != nil {
		t.Fatal(err)
	}
	if err := runExport([]string{"--target", "launchbox"}); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatal(err)
	}
}

func TestNetworkExportRejectsMixedCatalogRoots(t *testing.T) {
	err := runExport([]string{"--api", "http://localhost:8787", "--data-root", ".", "--platform", "nes"})
	if err == nil || !strings.Contains(err.Error(), "choose --api or --data-root") {
		t.Fatal(err)
	}
}
