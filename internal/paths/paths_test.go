package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigDirPrecedence(t *testing.T) {
	tests := []struct {
		name        string
		omakitenEnv string
		xdgEnv      string
		homeDir     string
		want        string
	}{
		{
			name:        "OMAKITEN_HOME wins over XDG and default",
			omakitenEnv: "/srv/omakiten",
			xdgEnv:      "/etc/xdg",
			homeDir:     "/home/user",
			want:        filepath.Join("/srv/omakiten", "config"),
		},
		{
			name:    "XDG wins over default when OMAKITEN_HOME unset",
			xdgEnv:  "/etc/xdg",
			homeDir: "/home/user",
			want:    filepath.Join("/etc/xdg", AppName, "config"),
		},
		{
			name:    "default falls back to ~/.config when both unset",
			homeDir: "/home/user",
			want:    filepath.Join("/home/user", ".config", AppName, "config"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(HomeEnv, tt.omakitenEnv)
			t.Setenv("XDG_CONFIG_HOME", tt.xdgEnv)
			t.Setenv("HOME", tt.homeDir)

			got, err := ConfigDir()
			if err != nil {
				t.Fatalf("ConfigDir() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ConfigDir() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConfigRootDoesNotIncludeConfigSubdir(t *testing.T) {
	tests := []struct {
		name        string
		omakitenEnv string
		xdgEnv      string
		homeDir     string
		want        string
	}{
		{
			name:        "OMAKITEN_HOME is the root",
			omakitenEnv: "/srv/omakiten",
			homeDir:     "/home/user",
			want:        "/srv/omakiten",
		},
		{
			name:    "XDG root is <xdg>/omakiten",
			xdgEnv:  "/etc/xdg",
			homeDir: "/home/user",
			want:    filepath.Join("/etc/xdg", AppName),
		},
		{
			name:    "default root is ~/.config/omakiten",
			homeDir: "/home/user",
			want:    filepath.Join("/home/user", ".config", AppName),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(HomeEnv, tt.omakitenEnv)
			t.Setenv("XDG_CONFIG_HOME", tt.xdgEnv)
			t.Setenv("HOME", tt.homeDir)
			got, err := ConfigRoot()
			if err != nil {
				t.Fatalf("ConfigRoot() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ConfigRoot() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestActiveConfigFileFallsBackToDefault(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(HomeEnv, tmp)
	t.Setenv("XDG_CONFIG_HOME", "")

	got, err := ActiveConfigFile()
	if err != nil {
		t.Fatalf("ActiveConfigFile() error = %v", err)
	}
	want := filepath.Join(tmp, "config", DefaultConfigFilename)
	if got != want {
		t.Fatalf("ActiveConfigFile() = %q, want %q", got, want)
	}
}

func TestSetActiveConfigPersistsAcrossLookups(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(HomeEnv, tmp)
	t.Setenv("XDG_CONFIG_HOME", "")

	if err := SetActiveConfig("config-custom-user-001.yaml"); err != nil {
		t.Fatalf("SetActiveConfig() error = %v", err)
	}
	got, err := ActiveConfigFile()
	if err != nil {
		t.Fatalf("ActiveConfigFile() error = %v", err)
	}
	want := filepath.Join(tmp, "config", "config-custom-user-001.yaml")
	if got != want {
		t.Fatalf("ActiveConfigFile() = %q, want %q", got, want)
	}

	// State file is at the expected location for manual inspection.
	if _, err := os.Stat(filepath.Join(tmp, "config", ActiveConfigStateFile)); err != nil {
		t.Fatalf("state file missing: %v", err)
	}
}

func TestActiveConfigFileResolvesCustomBeforeDefault(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(HomeEnv, tmp)
	t.Setenv("XDG_CONFIG_HOME", "")

	configDir := filepath.Join(tmp, "config")
	if err := os.MkdirAll(filepath.Join(configDir, "custom"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	// Both default and custom locations contain a same-named profile —
	// custom/ must win.
	if err := os.WriteFile(filepath.Join(configDir, "shared.yaml"), []byte("default\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(default) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "custom", "shared.yaml"), []byte("custom\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(custom) error = %v", err)
	}
	if err := SetActiveConfig("shared.yaml"); err != nil {
		t.Fatalf("SetActiveConfig() error = %v", err)
	}

	got, err := ActiveConfigFile()
	if err != nil {
		t.Fatalf("ActiveConfigFile() error = %v", err)
	}
	want := filepath.Join(configDir, "custom", "shared.yaml")
	if got != want {
		t.Fatalf("ActiveConfigFile() = %q, want %q (custom should win)", got, want)
	}
}

func TestSetActiveConfigRejectsPathSeparators(t *testing.T) {
	t.Setenv(HomeEnv, t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	if err := SetActiveConfig("nested/file.yaml"); err == nil {
		t.Fatal("SetActiveConfig() error = nil, want rejection of path with separator")
	}
}

func TestEntityDirsResolveSiblingOfConfig(t *testing.T) {
	t.Setenv(HomeEnv, "/srv/omakiten")
	t.Setenv("XDG_CONFIG_HOME", "")

	dir, err := EntityDir("personas")
	if err != nil {
		t.Fatalf("EntityDir() error = %v", err)
	}
	if want := filepath.Join("/srv/omakiten", "personas"); dir != want {
		t.Fatalf("EntityDir(personas) = %q, want %q (must be sibling of config/)", dir, want)
	}

	custom, err := EntityCustomDir("personas")
	if err != nil {
		t.Fatalf("EntityCustomDir() error = %v", err)
	}
	if want := filepath.Join("/srv/omakiten", "personas", "custom"); custom != want {
		t.Fatalf("EntityCustomDir(personas) = %q, want %q", custom, want)
	}
}

func TestDataDirPrecedence(t *testing.T) {
	tests := []struct {
		name        string
		omakitenEnv string
		xdgEnv      string
		homeDir     string
		want        string
	}{
		{
			name:        "OMAKITEN_HOME wins over XDG and default",
			omakitenEnv: "/srv/omakiten",
			xdgEnv:      "/var/xdg",
			homeDir:     "/home/user",
			want:        filepath.Join("/srv/omakiten", "data"),
		},
		{
			name:    "XDG wins over default when OMAKITEN_HOME unset",
			xdgEnv:  "/var/xdg",
			homeDir: "/home/user",
			want:    filepath.Join("/var/xdg", AppName),
		},
		{
			name:    "default falls back to ~/.local/share when both unset",
			homeDir: "/home/user",
			want:    filepath.Join("/home/user", ".local", "share", AppName),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(HomeEnv, tt.omakitenEnv)
			t.Setenv("XDG_DATA_HOME", tt.xdgEnv)
			t.Setenv("HOME", tt.homeDir)

			got, err := DataDir()
			if err != nil {
				t.Fatalf("DataDir() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("DataDir() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConfigAndDatabaseFileNamesUnderOmakitenHome(t *testing.T) {
	t.Setenv(HomeEnv, "/srv/omakiten")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")

	cfg, err := ConfigFile()
	if err != nil {
		t.Fatalf("ConfigFile() error = %v", err)
	}
	if want := filepath.Join("/srv/omakiten", "config", "omakiten.yaml"); cfg != want {
		t.Fatalf("ConfigFile() = %q, want %q", cfg, want)
	}
	db, err := DatabaseFile()
	if err != nil {
		t.Fatalf("DatabaseFile() error = %v", err)
	}
	if want := filepath.Join("/srv/omakiten", "data", "omakiten.db"); db != want {
		t.Fatalf("DatabaseFile() = %q, want %q", db, want)
	}
}
