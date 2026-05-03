package paths

import (
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
			want:    filepath.Join("/etc/xdg", AppName),
		},
		{
			name:    "default falls back to ~/.config when both unset",
			homeDir: "/home/user",
			want:    filepath.Join("/home/user", ".config", AppName),
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
