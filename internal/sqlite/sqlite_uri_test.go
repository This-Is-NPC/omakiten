package sqlite

import "testing"

func TestSQLiteFileURIIsPlatformAware(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		goos  string
		path  string
		query string
		want  string
	}{
		"unix absolute path": {
			goos:  "linux",
			path:  "/tmp/Omakiten Data/live.db",
			query: "mode=rw",
			want:  "file:///tmp/Omakiten%20Data/live.db?mode=rw",
		},
		"windows drive path": {
			goos:  "windows",
			path:  `C:\Omakiten Data\live.db`,
			query: "mode=ro",
			want:  "file:///C:/Omakiten%20Data/live.db?mode=ro",
		},
		"windows extended drive path": {
			goos:  "windows",
			path:  `\\?\C:\Omakiten Data\live.db`,
			query: "mode=ro",
			want:  "file:///C:/Omakiten%20Data/live.db?mode=ro",
		},
		"windows UNC path": {
			goos:  "windows",
			path:  `\\server\private share\live.db`,
			query: "mode=rw",
			want:  "file://server/private%20share/live.db?mode=rw",
		},
		"windows extended UNC path": {
			goos:  "windows",
			path:  `\\?\UNC\server\private share\live.db`,
			query: "mode=rw",
			want:  "file://server/private%20share/live.db?mode=rw",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := sqliteFileURIForPlatform(test.goos, test.path, test.query); got != test.want {
				t.Fatalf("sqliteFileURIForPlatform(%q, %q, %q) = %q, want %q", test.goos, test.path, test.query, got, test.want)
			}
		})
	}
}

func TestNormalizeWindowsSQLitePath(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		path string
		want string
	}{
		"drive path unchanged": {
			path: `C:\Omakiten Data\live.db`,
			want: `C:\Omakiten Data\live.db`,
		},
		"UNC path unchanged": {
			path: `\\server\private share\live.db`,
			want: `\\server\private share\live.db`,
		},
		"extended drive path": {
			path: `\\?\C:\Omakiten Data\live.db`,
			want: `C:\Omakiten Data\live.db`,
		},
		"extended UNC path": {
			path: `\\?\UNC\server\private share\live.db`,
			want: `\\server\private share\live.db`,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeWindowsSQLitePath(test.path); got != test.want {
				t.Fatalf("normalizeWindowsSQLitePath(%q) = %q, want %q", test.path, got, test.want)
			}
		})
	}
}
