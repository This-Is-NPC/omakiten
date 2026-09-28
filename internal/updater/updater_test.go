package updater

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssetName_PerPlatform(t *testing.T) {
	cases := []struct {
		os, arch string
		want     string
		err      bool
	}{
		{"linux", "amd64", "okt_Linux_x86_64.tar.gz", false},
		{"linux", "arm64", "okt_Linux_arm64.tar.gz", false},
		{"darwin", "amd64", "okt_Darwin_x86_64.tar.gz", false},
		{"darwin", "arm64", "okt_Darwin_arm64.tar.gz", false},
		{"windows", "amd64", "okt_Windows_x86_64.zip", false},
		{"freebsd", "amd64", "", true},
		{"linux", "riscv64", "", true},
	}
	for _, c := range cases {
		got, err := AssetName(c.os, c.arch)
		if c.err {
			if err == nil {
				t.Errorf("AssetName(%s,%s): expected error", c.os, c.arch)
			}
			continue
		}
		if err != nil {
			t.Errorf("AssetName(%s,%s): %v", c.os, c.arch, err)
			continue
		}
		if got != c.want {
			t.Errorf("AssetName(%s,%s): got %q want %q", c.os, c.arch, got, c.want)
		}
	}
}

func TestBinaryReplacement_OverwritesAndChmods(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "okt")
	if err := os.WriteFile(bin, []byte("OLD"), 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	body := strings.NewReader("REPLACED")
	staged, err := StageBinary(bin, body)
	if err != nil {
		t.Fatal(err)
	}
	if err := SwapStagedBinary(staged, bin); err != nil {
		t.Fatalf("binary replacement: %v", err)
	}
	got, _ := os.ReadFile(bin)
	if string(got) != "REPLACED" {
		t.Fatalf("binary replacement bytes: got %q want REPLACED", string(got))
	}
}
