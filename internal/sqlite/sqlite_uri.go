package sqlite

import (
	"net/url"
	"runtime"
	"strings"
)

// sqliteFileURI is the only filesystem-path-to-SQLite-URI conversion point.
// Windows drive paths require a leading slash, while UNC paths map their server
// component to URL authority. Callers supply only trusted query parameters.
func sqliteFileURI(path, rawQuery string) string {
	return sqliteFileURIForPlatform(runtime.GOOS, path, rawQuery)
}

func sqliteFileURIForPlatform(goos, path, rawQuery string) string {
	uri := &url.URL{Scheme: "file", RawQuery: rawQuery}
	if goos != "windows" {
		uri.Path = path
		return uri.String()
	}

	normalized := strings.ReplaceAll(normalizeWindowsSQLitePath(path), `\`, "/")
	if strings.HasPrefix(normalized, "//") {
		unc := strings.TrimPrefix(normalized, "//")
		if server, rest, ok := strings.Cut(unc, "/"); ok {
			uri.Host = server
			uri.Path = "/" + rest
			return uri.String()
		}
	}
	if len(normalized) >= 2 && normalized[1] == ':' {
		normalized = "/" + normalized
	}
	uri.Path = normalized
	return uri.String()
}

func normalizeWindowsSQLitePath(path string) string {
	const extendedPrefix = `\\?\`
	if !strings.HasPrefix(path, extendedPrefix) {
		return path
	}
	remainder := strings.TrimPrefix(path, extendedPrefix)
	if len(remainder) >= 4 && strings.EqualFold(remainder[:4], `UNC\`) {
		return `\\` + remainder[4:]
	}
	if len(remainder) >= 3 && remainder[1] == ':' && remainder[2] == '\\' {
		return remainder
	}
	return path
}
