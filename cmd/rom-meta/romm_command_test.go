package main

import (
	"strings"
	"testing"
)

func TestRetiredStockRomMCommandsDoNotRun(t *testing.T) {
	for _, command := range []string{"plan", "apply", "repair-dates"} {
		t.Run(command, func(t *testing.T) {
			err := runRomM([]string{command})
			if err == nil || !strings.Contains(err.Error(), "server example") {
				t.Fatalf("retired stock API command was not rejected: %v", err)
			}
		})
	}
}
