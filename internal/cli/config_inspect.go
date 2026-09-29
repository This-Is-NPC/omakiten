package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/paths"
)

func newConfigWhyCommand(opts *runtimeOptions) *cobra.Command {
	var layerFilter string
	cmd := &cobra.Command{
		Use:   "why <key>",
		Short: opts.t("cli.config.path.short"),
		Long:  opts.t("cli.config.path.long"),
		Args:  cobra.ExactArgs(1),
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
	cmd.Flags().StringVar(&layerFilter, "layer", "", opts.t("cli.config.path.flag.layer"))
	return cmd
}

func newConfigDiffCommand(opts *runtimeOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff <left> <right>",
		Short: opts.t("cli.config.diff.short"),
		Long:  opts.t("cli.config.diff.long"),
		Args:  cobra.ExactArgs(2),
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
		return "", domain.NewError(domain.ErrValidation, t("cli.err.invalid_layer"), map[string]any{"layer": raw})
	}
}

func resolveWhy(opts *runtimeOptions, key, layerFilter string) (any, error) {
	parts := strings.Split(key, ".")
	if layerFilter != "" {
		return resolveWhyLayer(opts, key, parts, layerFilter)
	}
	if opts.configPath != "" {
		path, err := opts.resolvedConfigPath()
		if err != nil {
			return nil, err
		}
		return resolveWhyFile(path, key, parts, "explicit")
	}
	root, err := opts.discoverRepoLocalRoot()
	if err != nil {
		return nil, err
	}
	if root != "" {
		path, err := paths.ActiveConfigFileInDir(filepath.Join(root, "config"))
		if err != nil {
			return nil, err
		}
		return resolveWhyFile(path, key, parts, "local")
	}
	path, err := resolveActiveFileForScope(opts, "global")
	if err != nil {
		return nil, err
	}
	return resolveWhyFile(path, key, parts, "global")
}

func resolveWhyLayer(opts *runtimeOptions, key string, parts []string, layer string) (any, error) {
	path, err := resolveActiveFileForScope(opts, layer)
	if err != nil {
		var coded *domain.CodedError
		if layer == "local" && errors.As(err, &coded) && coded.Code == domain.ErrValidation && coded.Details["start"] != nil {
			return map[string]any{"key": key, "source": "not_set", "layer": layer}, nil
		}
		return nil, err
	}
	return resolveWhyFile(path, key, parts, layer)
}

func resolveWhyFile(path, key string, parts []string, source string) (any, error) {
	value, found, err := lookupYAMLKey(path, parts)
	if err != nil {
		return nil, err
	}
	if !found {
		return map[string]any{"key": key, "source": "not_set", "path": path}, nil
	}
	return map[string]any{"key": key, "value": value, "source": source, "path": path}, nil
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
			return "", domain.NewError(domain.ErrValidation, t("cli.err.diff_local_requires_path"), map[string]any{"spec": spec})
		}
		repoLocalDir := filepath.Join(root, config.RepoLocalDirName)
		if info, err := os.Lstat(repoLocalDir); err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", domain.NewError(domain.ErrValidation, t("cli.err.diff_no_omakiten_at_path"), map[string]any{"path": repoLocalDir})
		}
		path, err := paths.ActiveConfigFileInDir(filepath.Join(repoLocalDir, "config"))
		if err != nil {
			return "", domain.NewError(domain.ErrValidation, t("cli.err.diff_no_active_yaml"), map[string]any{"path": repoLocalDir, "error": err.Error()})
		}
		return path, nil
	default:
		abs, err := filepath.Abs(spec)
		if err != nil {
			return "", err
		}
		if info, err := os.Stat(abs); err != nil || info.IsDir() {
			return "", domain.NewError(domain.ErrValidation, t("cli.err.diff_source_unreadable"), map[string]any{"path": abs})
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
	values, err := config.ReadConfigMap(path)
	if err != nil {
		return nil, configValidationFailure(path, err)
	}
	return values, nil
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
		out = append(out, diffMapEntry(prefix, k, left, right)...)
	}
	return out
}

func diffMapEntry(prefix, key string, left, right map[string]any) []diffEntry {
	lookupKey := key
	if prefix != "" {
		key = prefix + "." + key
	}
	lv, lOK := left[lookupKey]
	rv, rOK := right[lookupKey]
	switch {
	case lOK && !rOK:
		return []diffEntry{{Key: key, Op: "removed", Left: lv}}
	case !lOK && rOK:
		return []diffEntry{{Key: key, Op: "added", Right: rv}}
	}
	lm, lIsMap := lv.(map[string]any)
	rm, rIsMap := rv.(map[string]any)
	if lIsMap && rIsMap {
		return diffMaps(key, lm, rm)
	}
	if !reflect.DeepEqual(lv, rv) {
		return []diffEntry{{Key: key, Op: "changed", Left: lv, Right: rv}}
	}
	return nil
}
