package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/paths"
)

func newConfigWhyCommand(opts *runtimeOptions) *cobra.Command {
	var layerFilter string
	cmd := &cobra.Command{
		Use:   "why <key>",
		Short: "Report which config layer owns the value at a dotted key path",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(context.Context) (any, error) {
				key := args[0]
				layer, err := parseLayer(layerFilter)
				if err != nil {
					return nil, err
				}
				return resolveWhy(opts, key, layer)
			})
		},
	}
	cmd.Flags().StringVar(&layerFilter, "layer", "", "restrict lookup to one layer: global or local")
	return cmd
}

func newConfigDiffCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff <left> <right>",
		Short: "Diff two config sources by identity key",
		Long: `Compare two YAML config sources. Each operand is one of:
  global              - the user-global active yaml (honours --config)
  local               - the active yaml inside the CWD walk-up .omakiten/
  local:<path>        - the active yaml inside <path>/.omakiten/
  <path/to/file.yaml> - any yaml file on disk

The output is a structured list of key-level differences. Top-level keys
common to both sides whose nested contents differ are reported once per
divergent leaf rather than as a single opaque "changed" entry.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(context.Context) (any, error) {
				leftPath, err := resolveDiffSource(opts, args[0])
				if err != nil {
					return nil, err
				}
				rightPath, err := resolveDiffSource(opts, args[1])
				if err != nil {
					return nil, err
				}
				leftMap, err := readYAMLMap(leftPath)
				if err != nil {
					return nil, err
				}
				rightMap, err := readYAMLMap(rightPath)
				if err != nil {
					return nil, err
				}
				entries := diffMaps("", leftMap, rightMap)
				return map[string]any{
					"left":  map[string]string{"spec": args[0], "path": leftPath},
					"right": map[string]string{"spec": args[1], "path": rightPath},
					"diff":  entries,
				}, nil
			})
		},
	}
	return cmd
}

// parseLayer accepts an empty string ("scan all"), "global", or "local".
// Any other input rejects with validation_error.
func parseLayer(raw string) (string, error) {
	switch raw {
	case "", "global", "local":
		return raw, nil
	default:
		return "", domain.NewError(domain.ErrValidation, "invalid --layer (want global or local)", map[string]any{"layer": raw})
	}
}

// resolveWhy walks the chosen layer(s) and returns {key, value, source}.
// When layerFilter is "", the order is repo-local first (since the runtime
// applies it as the higher-precedence overlay) and global second; the first
// hit wins. When layerFilter is explicit, only that layer is consulted and
// a missing key yields source = "not_set".
func resolveWhy(opts *runtimeOptions, key, layerFilter string) (any, error) {
	parts := strings.Split(key, ".")

	consult := func(layer string) (any, string, bool, error) {
		switch layer {
		case "global":
			path, err := opts.resolvedConfigPath()
			if err != nil {
				return nil, "", false, err
			}
			data, ok, err := lookupYAMLKey(path, parts)
			if err != nil {
				return nil, "", false, err
			}
			return data, path, ok, nil
		case "local":
			cwd, err := os.Getwd()
			if err != nil {
				return nil, "", false, err
			}
			repoDir, found, err := config.FindRepoLocal(cwd)
			if err != nil {
				return nil, "", false, err
			}
			if !found {
				return nil, "", false, nil
			}
			path, err := paths.ActiveConfigFileInDir(filepath.Join(repoDir, "config"))
			if err != nil {
				return nil, "", false, nil
			}
			data, ok, err := lookupYAMLKey(path, parts)
			if err != nil {
				return nil, "", false, err
			}
			return data, path, ok, nil
		default:
			return nil, "", false, fmt.Errorf("resolve why: unknown layer %q", layer)
		}
	}

	if layerFilter != "" {
		value, path, found, err := consult(layerFilter)
		if err != nil {
			return nil, err
		}
		if !found {
			return map[string]any{"key": key, "source": "not_set", "layer": layerFilter}, nil
		}
		return map[string]any{"key": key, "value": value, "source": layerFilter, "path": path}, nil
	}

	for _, layer := range []string{"local", "global"} {
		value, path, found, err := consult(layer)
		if err != nil {
			return nil, err
		}
		if found {
			return map[string]any{"key": key, "value": value, "source": layer, "path": path}, nil
		}
	}
	return map[string]any{"key": key, "source": "not_set"}, nil
}

// resolveDiffSource maps a CLI operand to an absolute yaml file path.
//   - "global"            → opts.resolvedConfigPath()
//   - "local"             → walk-up .omakiten/, then ActiveConfigFileInDir
//   - "local:<path>"      → <path>/.omakiten/, then ActiveConfigFileInDir
//   - any other input     → treated as a literal file path (relative resolved
//                           against CWD)
func resolveDiffSource(opts *runtimeOptions, spec string) (string, error) {
	switch {
	case spec == "global":
		return resolveScopeActiveFile(opts, config.ScopeGlobal)
	case spec == "local":
		return resolveScopeActiveFile(opts, config.ScopeLocal)
	case strings.HasPrefix(spec, "local:"):
		root := strings.TrimPrefix(spec, "local:")
		if root == "" {
			return "", domain.NewError(domain.ErrValidation, "local:<path> requires a non-empty path", map[string]any{"spec": spec})
		}
		repoLocalDir := filepath.Join(root, config.RepoLocalDirName)
		if info, err := os.Stat(repoLocalDir); err != nil || !info.IsDir() {
			return "", domain.NewError(domain.ErrValidation, "no .omakiten/ at supplied path", map[string]any{"path": repoLocalDir})
		}
		path, err := paths.ActiveConfigFileInDir(filepath.Join(repoLocalDir, "config"))
		if err != nil {
			return "", domain.NewError(domain.ErrValidation, "no active library yaml inside supplied .omakiten/config", map[string]any{"path": repoLocalDir, "error": err.Error()})
		}
		return path, nil
	default:
		abs, err := filepath.Abs(spec)
		if err != nil {
			return "", err
		}
		if info, err := os.Stat(abs); err != nil || info.IsDir() {
			return "", domain.NewError(domain.ErrValidation, "diff source not a readable file", map[string]any{"path": abs})
		}
		return abs, nil
	}
}

func lookupYAMLKey(path string, parts []string) (any, bool, error) {
	m, err := readYAMLMap(path)
	if err != nil {
		return nil, false, err
	}
	var cur any = m
	for _, p := range parts {
		mp, ok := cur.(map[string]any)
		if !ok {
			return nil, false, nil
		}
		v, present := mp[p]
		if !present {
			return nil, false, nil
		}
		cur = v
	}
	return cur, true, nil
}

func readYAMLMap(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if node.Kind == 0 {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := node.Decode(&m); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return m, nil
}

// diffEntry is one line in the diff output.
type diffEntry struct {
	Key   string `json:"key"`
	Op    string `json:"op"`
	Left  any    `json:"left,omitempty"`
	Right any    `json:"right,omitempty"`
}

// diffMaps walks two YAML maps recursively and emits one entry per
// divergent leaf. Maps are descended into; lists and scalars are compared
// by deep equality. Keys are reported in their dotted form so the output is
// stable and grep-friendly.
func diffMaps(prefix string, left, right map[string]any) []diffEntry {
	out := []diffEntry{}
	seen := make(map[string]struct{}, len(left)+len(right))
	for k := range left {
		seen[k] = struct{}{}
	}
	for k := range right {
		seen[k] = struct{}{}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		lv, lOK := left[k]
		rv, rOK := right[k]
		switch {
		case lOK && !rOK:
			out = append(out, diffEntry{Key: key, Op: "removed", Left: lv})
		case !lOK && rOK:
			out = append(out, diffEntry{Key: key, Op: "added", Right: rv})
		default:
			lm, lIsMap := lv.(map[string]any)
			rm, rIsMap := rv.(map[string]any)
			if lIsMap && rIsMap {
				out = append(out, diffMaps(key, lm, rm)...)
				continue
			}
			if !deepEqual(lv, rv) {
				out = append(out, diffEntry{Key: key, Op: "changed", Left: lv, Right: rv})
			}
		}
	}
	return out
}

func deepEqual(a, b any) bool {
	return reflect.DeepEqual(a, b)
}
