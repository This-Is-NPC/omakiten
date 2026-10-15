package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync/atomic"
)

// PresetRequirements names the minimum okt release a preset needs.
type PresetRequirements struct {
	Okt string `yaml:"okt,omitempty" json:"okt,omitempty"`
}

var (
	presetRequirementVersion = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)
	releaseVersion           = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)
	runningVersion           atomic.Value
)

// SetRunningVersion records the executable's version for preset requirement
// checks. Only a release version (X.Y.Z or vX.Y.Z) enforces requirements;
// development and untagged builds load every preset.
func SetRunningVersion(version string) {
	runningVersion.Store(version)
}

func validatePresetRequirements(r PresetRequirements) error {
	if r.Okt != "" && !presetRequirementVersion.MatchString(r.Okt) {
		return fmt.Errorf("preset requires.okt %q must be a version X.Y.Z", r.Okt)
	}
	return nil
}

// checkPresetRequirements fails when a release executable is older than the
// preset's requires.okt.
func checkPresetRequirements(m PresetManifest) error {
	if m.Requires.Okt == "" {
		return nil
	}
	running, _ := runningVersion.Load().(string)
	have := releaseVersion.FindStringSubmatch(running)
	need := presetRequirementVersion.FindStringSubmatch(m.Requires.Okt)
	if have == nil || need == nil {
		return nil
	}
	for i := 1; i <= 3; i++ {
		h, _ := strconv.Atoi(have[i])
		n, _ := strconv.Atoi(need[i])
		if h != n {
			if h < n {
				return fmt.Errorf("preset %q requires okt >= %s (running %s)", m.Name, m.Requires.Okt, running)
			}
			return nil
		}
	}
	return nil
}

// checkSelectedPresetRequirements checks the manifest of the package that
// holds the selected config at physical (<package>/config/<file>.yaml).
func checkSelectedPresetRequirements(physical string) error {
	raw, err := readFileBounded(filepath.Join(filepath.Dir(filepath.Dir(physical)), PresetManifestFile), MaxWiringFileBytes)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var m PresetManifest
	if err := decodeYAMLStrict(raw, &m); err != nil {
		return err
	}
	return checkPresetRequirements(m)
}
