// Command okt-gallery is the dev-only gallery for the TUI's shared components
// and every screenhost surface. Components render in isolation; screens mount
// through screenfixture.Drive — the same Build path the goldens record — so
// what you approve by eye is what the fixture grabs.
//
// It is not shipped: `mise run build` builds cmd/okt alone, and nothing in the
// okt binary imports this package. Run it with `mise run gallery`.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"omakiten/internal/config"
	"omakiten/internal/tui"
	"omakiten/internal/tui/components/screenkit"
)

func main() {
	configPath := flag.String("config", "dev_env/config/omakase.yaml", "config bundle the theme is resolved from")
	dump := flag.Bool("dump", false, "render every component variant to stdout and exit instead of opening the browser")
	width := flag.Int("width", 120, "terminal width assumed by -dump")
	height := flag.Int("height", 40, "terminal height assumed by -dump")
	asJSON := flag.Bool("json", false, "write the catalog to stdout as JSON and exit — entries, declared floors, and every scenario including generated ones")
	flag.Parse()

	if err := run(*configPath, *dump, *asJSON, *width, *height); err != nil {
		fmt.Fprintf(os.Stderr, "okt-gallery: %v\n", err)
		os.Exit(1)
	}
}

func run(configPath string, dump, asJSON bool, width, height int) error {
	// The catalog is data before it is pixels, and -json needs no theme: it
	// reports what the gallery WOULD show, not what a palette makes of it.
	if asJSON {
		return exportCatalog(os.Stdout)
	}
	theme, styles, err := loadTheme(configPath)
	if err != nil {
		return err
	}

	if dump {
		fmt.Print(dumpAll(theme, newDemoTheme(theme, styles), width, height))
		return nil
	}
	_, err = tea.NewProgram(newModel(theme, newDemoTheme(theme, styles)), tea.WithAltScreen()).Run()
	return err
}

// loadTheme resolves the same bundle the TUI would load, so the gallery paints
// with the palette a user actually sees rather than a fixture one.
func loadTheme(path string) (config.Theme, screenkit.Styles, error) {
	bundle, err := config.LoadBundle(path)
	if err != nil {
		return config.Theme{}, screenkit.Styles{}, fmt.Errorf("load config bundle %s: %w", path, err)
	}
	snapshot := config.BuildSnapshot(bundle)
	if themeErr := snapshot.ThemeError(); themeErr != nil {
		return config.Theme{}, screenkit.Styles{}, fmt.Errorf("resolve theme from %s: %w", path, themeErr)
	}
	theme := snapshot.Theme()
	if len(theme.Colors) == 0 {
		return config.Theme{}, screenkit.Styles{}, errors.New("resolved theme carries no colors — is the bundle path right?")
	}
	return theme, tui.ScreenStyles(theme), nil
}
