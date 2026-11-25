//go:build linux

package location

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWiFiToolDiscoveryIgnoresUserControlledPATH(t *testing.T) {
	// Changing PATH must neither invent an installed system tool nor change
	// which executable a scan would use.
	nmcliBefore, iwBefore, pathBefore := hasNmcli(), hasIW(), iwPath()
	dir := t.TempDir()
	for _, name := range []string{"nmcli", "iw"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nexit 99\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	if hasNmcli() != nmcliBefore || hasIW() != iwBefore {
		t.Fatal("PATH changed system tool availability")
	}
	if path := iwPath(); path != pathBefore || !filepath.IsAbs(path) || filepath.Dir(path) == dir {
		t.Fatalf("scan executable depends on PATH: %q", path)
	}
}
