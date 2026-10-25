package serveui

import (
	"os/exec"
	"testing"
)

func TestBrowserLoading(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is optional; install it to run embedded-browser logic tests")
	}
	cmd := exec.Command(node, "--test", "browser_test.cjs")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("browser tests: %v\n%s", err, output)
	}
}
