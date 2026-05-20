package cli

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"omakiten/internal/domain"
	"omakiten/internal/lifecycle"
)

// uninstallInputs is the resolved flag set the headless and picker
// paths converge on before runUninstall executes the lifecycle calls.
// PurgeData / PurgeConfig keep their independent shape so the JSON
// envelope can report per-target outcomes; the --purge convenience
// shorthand collapses into both being true.
type uninstallInputs struct {
	PurgeData   bool
	PurgeConfig bool
}

func newUninstallCommand(opts *runtimeOptions) *cobra.Command {
	var (
		yes         bool
		purgeData   bool
		purgeConfig bool
		purge       bool
	)

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: opts.t("cli.uninstall.short"),
		Long:  opts.t("cli.uninstall.long"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			inputs := uninstallInputs{
				PurgeData:   purgeData || purge,
				PurgeConfig: purgeConfig || purge,
			}

			explicit := cmd.Flags().Changed("purge-data") ||
				cmd.Flags().Changed("purge-config") ||
				cmd.Flags().Changed("purge") ||
				yes

			return runJSON(cmd, func(ctx context.Context) (any, error) {
				final, err := resolveUninstallInputs(ctx, inputs, explicit)
				if err != nil {
					return nil, err
				}
				return runUninstall(ctx, final)
			})
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, opts.t("cli.uninstall.flag.yes"))
	cmd.Flags().BoolVar(&purgeData, "purge-data", false, opts.t("cli.uninstall.flag.purge-data"))
	cmd.Flags().BoolVar(&purgeConfig, "purge-config", false, opts.t("cli.uninstall.flag.purge-config"))
	cmd.Flags().BoolVar(&purge, "purge", false, opts.t("cli.uninstall.flag.purge"))

	return cmd
}

// resolveUninstallInputs returns the final flag set, falling through
// to the bubbletea picker when no flags were supplied AND stdin is a
// TTY. The headless path (any explicit flag, including --yes) returns
// the inputs verbatim. The no-TTY headless invocation without --yes
// errors with a coded validation_error so the JSON envelope carries a
// machine-readable signal instead of hanging on a closed stdin.
func resolveUninstallInputs(ctx context.Context, inputs uninstallInputs, explicit bool) (uninstallInputs, error) {
	if explicit {
		return inputs, nil
	}
	if !stdinIsTTY() {
		return uninstallInputs{}, domain.NewError(domain.ErrValidation, t("cli.uninstall.picker.no_tty"), nil)
	}
	return runUninstallPicker(ctx, inputs)
}

// runUninstall applies the resolved inputs. Returns the JSON envelope
// payload; coded errors (validation_error, etc.) bubble up so runJSON
// emits the failure envelope. A successful run always reports
// uninstall_completed even when nothing was on disk to remove — the
// per-target booleans in the payload tell the caller which steps
// were no-ops.
//
// ctx is checked between every filesystem-mutating step so a cancelled
// parent (sigint mid-purge, JSON envelope timeout) aborts before the
// next irreversible delete fires. The lifecycle helpers themselves are
// not ctx-aware (os.RemoveAll has no cancellation contract); the gate
// here is best-effort but covers the common case of a user hitting
// ctrl+c after the picker accepts.
func runUninstall(ctx context.Context, inputs uninstallInputs) (any, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	binaryPath := lifecycle.BinaryPath(home)

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	binaryRemoved, err := lifecycle.RemoveBinary(binaryPath)
	if err != nil {
		return nil, domain.NewError(domain.ErrUninstallFailed, err.Error(), map[string]any{"binary": binaryPath})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	wrappersRemoved, err := lifecycle.RemoveAllWrappers(home)
	if err != nil {
		return nil, domain.NewError(domain.ErrUninstallFailed, err.Error(), nil)
	}

	result := map[string]any{
		"code":           "uninstall_completed",
		"binary_path":    binaryPath,
		"binary_removed": binaryRemoved,
		"wrappers":       wrappersRemoved,
	}

	if inputs.PurgeData {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path, removed, err := lifecycle.PurgeDataDir()
		if err != nil {
			return nil, domain.NewError(domain.ErrUninstallFailed, err.Error(), map[string]any{"data_dir": path})
		}
		result["data_dir"] = path
		result["data_removed"] = removed
	}
	if inputs.PurgeConfig {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path, removed, err := lifecycle.PurgeConfigRoot()
		if err != nil {
			return nil, domain.NewError(domain.ErrUninstallFailed, err.Error(), map[string]any{"config_root": path})
		}
		result["config_root"] = path
		result["config_removed"] = removed
	}

	return result, nil
}

// uninstallStep identifies which row of the picker the cursor is on.
// stepBinary / stepWrappers are fixed (always part of the uninstall)
// and render as informational rows; stepData / stepConfig are the
// toggleable purge targets.
type uninstallStep int

const (
	uninstallStepBinary uninstallStep = iota
	uninstallStepWrappers
	uninstallStepData
	uninstallStepConfig
	uninstallStepCount
)

// uninstallPickerModel backs the bubbletea picker. Decoupled from the
// cobra command so the test suite can feed tea.KeyMsg values into
// Update without driving a real terminal.
type uninstallPickerModel struct {
	cursor     uninstallStep
	purgeData  bool
	purgeCfg   bool
	binaryPath string
	dataDir    string
	dataSize   int64
	configDir  string
	configSize int64

	styles  setupStyles
	aborted bool
	done    bool
}

func newUninstallPickerModel(inputs uninstallInputs) (uninstallPickerModel, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return uninstallPickerModel{}, err
	}

	dataDir, dataSize, err := probeDir(probeData)
	if err != nil {
		return uninstallPickerModel{}, err
	}
	configDir, configSize, err := probeDir(probeConfig)
	if err != nil {
		return uninstallPickerModel{}, err
	}

	theme, _ := loadBundledTheme()
	return uninstallPickerModel{
		cursor:     uninstallStepBinary,
		purgeData:  inputs.PurgeData,
		purgeCfg:   inputs.PurgeConfig,
		binaryPath: lifecycle.BinaryPath(home),
		dataDir:    dataDir,
		dataSize:   dataSize,
		configDir:  configDir,
		configSize: configSize,
		styles:     newSetupStyles(theme),
	}, nil
}

func (m uninstallPickerModel) Init() tea.Cmd { return nil }

// Update routes key messages. ctrl+c aborts before any side-effect;
// `y` is the universal apply (works on every row); enter toggles the
// active purge row. Non-purge rows ignore enter (binary + wrappers are
// always removed) so the user cannot accidentally opt out of the
// always-applied steps.
func (m uninstallPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if key.Type == tea.KeyCtrlC {
		m.aborted = true
		return m, tea.Quit
	}
	switch key.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < uninstallStepCount-1 {
			m.cursor++
		}
	case "enter", " ":
		switch m.cursor {
		case uninstallStepData:
			m.purgeData = !m.purgeData
		case uninstallStepConfig:
			m.purgeCfg = !m.purgeCfg
		}
	case "y", "Y":
		m.done = true
		return m, tea.Quit
	}
	return m, nil
}

func (m uninstallPickerModel) View() string {
	var b []string
	b = append(b, "", m.styles.title.Render(t("cli.uninstall.picker.title")), "")
	rows := []string{
		formatStep(m, uninstallStepBinary, "[x]", fmt.Sprintf(t("cli.uninstall.picker.binary"), m.binaryPath)),
		formatStep(m, uninstallStepWrappers, "[x]", t("cli.uninstall.picker.wrappers")),
		formatStep(m, uninstallStepData, checkbox(m.purgeData), fmt.Sprintf(t("cli.uninstall.picker.data"), m.dataDir, lifecycle.FormatBytes(m.dataSize))),
		formatStep(m, uninstallStepConfig, checkbox(m.purgeCfg), fmt.Sprintf(t("cli.uninstall.picker.config"), m.configDir, lifecycle.FormatBytes(m.configSize))),
	}
	b = append(b, rows...)
	if m.purgeData || m.purgeCfg {
		b = append(b, "", lipgloss.NewStyle().Bold(true).Render(t("cli.uninstall.picker.warn")))
	}
	b = append(b, "", m.styles.hint.Render(t("cli.uninstall.picker.hint")), "")
	return joinLines(b)
}

func formatStep(m uninstallPickerModel, step uninstallStep, box, label string) string {
	prefix := "  "
	if step == m.cursor {
		prefix = m.styles.marker.Render("› ")
	}
	box = m.styles.boxOn.Render(box)
	return prefix + box + " " + label
}

func checkbox(on bool) string {
	if on {
		return "[x]"
	}
	return "[ ]"
}

func joinLines(lines []string) string {
	out := ""
	for _, l := range lines {
		out += l + "\n"
	}
	return out
}

// runUninstallPicker drives the bubbletea program when the headless
// path could not pre-resolve every input. Returns the populated
// inputs on success, a coded validation_error on ctrl+c.
func runUninstallPicker(ctx context.Context, inputs uninstallInputs) (uninstallInputs, error) {
	model, err := newUninstallPickerModel(inputs)
	if err != nil {
		return uninstallInputs{}, err
	}
	prog := tea.NewProgram(model, tea.WithContext(ctx), tea.WithInput(os.Stdin), tea.WithOutput(os.Stderr))
	final, err := prog.Run()
	if err != nil {
		return uninstallInputs{}, fmt.Errorf("run uninstall picker: %w", err)
	}
	result, ok := final.(uninstallPickerModel)
	if !ok {
		return uninstallInputs{}, fmt.Errorf("uninstall picker returned unexpected model type %T", final)
	}
	if result.aborted || !result.done {
		return uninstallInputs{}, domain.NewError(domain.ErrValidation, t("cli.uninstall.picker.aborted"), nil)
	}
	return uninstallInputs{PurgeData: result.purgeData, PurgeConfig: result.purgeCfg}, nil
}

// probeKind tags which lifecycle.Purge* helper a probe should target.
// Allows probeDir to share size-resolution code without exposing the
// underlying paths package to this file's call sites.
type probeKind int

const (
	probeData probeKind = iota
	probeConfig
)

func probeDir(kind probeKind) (string, int64, error) {
	switch kind {
	case probeData:
		path, _, err := lifecycle.PreviewDataDir()
		if err != nil {
			return "", 0, err
		}
		size, err := lifecycle.DirSize(path)
		if err != nil {
			return path, 0, err
		}
		return path, size, nil
	case probeConfig:
		path, _, err := lifecycle.PreviewConfigRoot()
		if err != nil {
			return "", 0, err
		}
		size, err := lifecycle.DirSize(path)
		if err != nil {
			return path, 0, err
		}
		return path, size, nil
	default:
		panic(fmt.Sprintf("uninstall: unknown probeKind %d", kind))
	}
}
