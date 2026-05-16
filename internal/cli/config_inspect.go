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
		Short: "Report the value at a dotted YAML key path in the active config",
		Long: `Walk the active config (or a chosen layer) by dotted YAML key
and report {key, value, path, source}. Without --layer the resolver
picks the install that the runtime would use: the discovered .omakiten/
when present, otherwise the user-global ConfigRoot.

The standalone model means there is no merge, so a value at any key
comes from exactly one install. --layer pins the lookup to "global" or
"local" so callers can verify a specific layer rather than the resolved
one (e.g. "is this set in the user-global install even though my repo
overrides it locally").`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				if err := primeDiscoveryStart(ctx, opts); err != nil {
					return nil, err
				}
				key := args[0]
				layer, err := parseLayer(layerFilter)
				if err != nil {
					return nil, err
				}
				return resolveWhy(opts, key, layer)
			})
		},
	}
	cmd.Flags().StringVar(&layerFilter, "layer", "", "pin the lookup to one layer: global or local (default = whatever the resolver would pick)")
	return cmd
}

func newConfigDiffCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff <left> <right>",
		Short: "Diff two config sources by structural YAML comparison",
		Long: `Compare two YAML config sources and emit one entry per divergent
leaf. Each operand is one of:
  global              - the user-global active yaml
  local               - the active yaml inside the CWD walk-up .omakiten/
  local:<path>        - the active yaml inside <path>/.omakiten/
  <path/to/file.yaml> - any yaml file on disk

Output entries carry op = added | removed | changed plus the relevant
side values. Maps descend recursively; lists / scalars compare by deep
equality.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runJSON(cmd, func(ctx context.Context) (any, error) {
				if err := primeDiscoveryStart(ctx, opts); err != nil {
					return nil, err
				}
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
				return map[string]any{
					"left":  map[string]string{"spec": args[0], "path": leftPath},
					"right": map[string]string{"spec": args[1], "path": rightPath},
					"diff":  diffMaps("", leftMap, rightMap),
				}, nil
			})
		},
	}
	return cmd
}

func parseLayer(raw string) (string, error) {
	switch raw {
	case "", "global", "local":
		return raw, nil
	default:
		return "", domain.NewError(domain.ErrValidation, "invalid --layer (want global or local)", map[string]any{"layer": raw})
	}
}

func resolveWhy(opts *runtimeOptions, key, layerFilter string) (any, error) {
	parts := strings.Split(key, ".")
	consult := func(layer string) (any, string, bool, error) {
		path, err := resolveActiveFileForScope(opts, layer)
		if err != nil {
			return nil, "", false, err
		}
		val, ok, err := lookupYAMLKey(path, parts)
		if err != nil {
			return nil, "", false, err
		}
		return val, path, ok, nil
	}

	if layerFilter != "" {
		value, path, found, err := consult(layerFilter)
		if err != nil {
			// Missing local install is a "not_set" answer rather than a
			// hard error so callers can ask the question safely.
			var coded *domain.CodedError
			if layerFilter == "local" && err != nil {
				if asCoded(err, &coded) && coded.Code == domain.ErrValidation {
					return map[string]any{"key": key, "source": "not_set", "layer": layerFilter}, nil
				}
			}
			return nil, err
		}
		if !found {
			return map[string]any{"key": key, "source": "not_set", "layer": layerFilter}, nil
		}
		return map[string]any{"key": key, "value": value, "source": layerFilter, "path": path}, nil
	}

	// No filter: resolver decides. Walk up from discoveryStart (or CWD)
	// for .omakiten/; fall back to global. Matches runtime resolution.
	start := opts.discoveryStart
	if start == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		start = cwd
	}
	if dir, ok, err := config.FindRepoLocal(start); err == nil && ok {
		path, err := paths.ActiveConfigFileInDir(filepath.Join(dir, "config"))
		if err == nil {
			val, found, err := lookupYAMLKey(path, parts)
			if err != nil {
				return nil, err
			}
			if found {
				return map[string]any{"key": key, "value": val, "source": "local", "path": path}, nil
			}
			return map[string]any{"key": key, "source": "not_set", "path": path}, nil
		}
	}
	value, path, found, err := consult("global")
	if err != nil {
		return nil, err
	}
	if !found {
		return map[string]any{"key": key, "source": "not_set"}, nil
	}
	return map[string]any{"key": key, "value": value, "source": "global", "path": path}, nil
}

func asCoded(err error, out **domain.CodedError) bool {
	var coded *domain.CodedError
	for cur := err; cur != nil; cur = unwrap(cur) {
		if c, ok := cur.(*domain.CodedError); ok {
			coded = c
			break
		}
	}
	if coded == nil {
		return false
	}
	*out = coded
	return true
}

func unwrap(err error) error {
	type unwrapper interface{ Unwrap() error }
	if u, ok := err.(unwrapper); ok {
		return u.Unwrap()
	}
	return nil
}

func resolveDiffSource(opts *runtimeOptions, spec string) (string, error) {
	switch {
	case spec == "global":
		return resolveActiveFileForScope(opts, "global")
	case spec == "local":
		return resolveActiveFileForScope(opts, "local")
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
			return "", domain.NewError(domain.ErrValidation, "no active yaml inside supplied .omakiten/config", map[string]any{"path": repoLocalDir, "error": err.Error()})
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

type diffEntry struct {
	Key   string `json:"key"`
	Op    string `json:"op"`
	Left  any    `json:"left,omitempty"`
	Right any    `json:"right,omitempty"`
}

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
			if !reflect.DeepEqual(lv, rv) {
				out = append(out, diffEntry{Key: key, Op: "changed", Left: lv, Right: rv})
			}
		}
	}
	return out
}
