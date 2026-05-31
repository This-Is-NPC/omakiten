package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// importDirectiveKey is the single reserved mapping key that marks a value-level
// import. A mapping node carrying exactly this one key (no siblings) is replaced
// by the root node of the referenced YAML document before strict decoding runs.
const importDirectiveKey = "from"

// maxImportDepth bounds how deeply imports may nest (the root document is
// depth 0, its imports depth 1, and so on). A fixed cap is defence-in-depth
// alongside cycle detection: even an acyclic but pathologically deep chain is
// rejected rather than risking stack exhaustion. Ten levels is far beyond any
// legitimate config layout, where the deepest real case is a top-level section
// importing a file that imports a fragment.
const maxImportDepth = 10

// importFileMaxBytes bounds a single imported YAML file. Imports carry wiring
// material (hooks, workflows) rather than entity bodies, so the resolver reuses
// the wiring-file budget from size_caps.go and surfaces the same coded
// ErrConfigTooLarge on overflow.
const importFileMaxBytes = MaxWiringFileBytes

// resolveImports walks the YAML node tree rooted at root and expands every
// import directive — a mapping node whose only key is "from", whose value is a
// relative path to another YAML file. Each directive is replaced in place by the
// root node of the imported document; expansion recurses so imported documents
// may themselves contain directives.
//
// rootPath is the path of the file that produced root; it anchors relative-path
// resolution and seeds cycle detection. Each imported path is resolved relative
// to the file that declared the directive, not relative to rootPath.
//
// On success it returns the resolved (mutated) root node plus the ordered set of
// every source file touched: rootPath first, then each imported file in
// first-encounter (depth-first) order, each listed exactly once even when a file
// is imported from more than one branch.
//
// Path safety mirrors resolveSubtaskKitPath (loader.go) verbatim: absolute
// paths, parent ("..") segments, and symlink escapes out of the declaring
// file's directory are rejected. Reads are bounded by importFileMaxBytes.
// Cycles and depth overflows are rejected before the offending document is
// decoded. Every error carries the import chain so the failing directive is
// identifiable.
func resolveImports(root *yaml.Node, rootPath string) (*yaml.Node, []string, error) {
	if root == nil {
		return nil, nil, fmt.Errorf("import resolver: root node is nil")
	}
	absRoot, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, nil, fmt.Errorf("import resolver: resolve root path %q: %w", rootPath, err)
	}
	// Canonicalise the root so its cycle key matches the symlink-resolved paths
	// imported files produce; fall back to the lexical path when the root does
	// not exist on disk (e.g. an in-memory document under test).
	if real, rerr := filepath.EvalSymlinks(absRoot); rerr == nil {
		absRoot = real
	}

	r := &importResolver{
		seen:     map[string]struct{}{absRoot: {}},
		visiting: map[string]struct{}{absRoot: {}},
		sources:  []string{absRoot},
	}

	resolved, err := r.walk(root, absRoot, []string{absRoot}, 0)
	if err != nil {
		return nil, nil, err
	}
	return resolved, r.sources, nil
}

type importResolver struct {
	// seen guards the sources list against duplicates while preserving order.
	seen map[string]struct{}
	// visiting holds the canonical paths on the active import chain, used for
	// cycle detection. A file imported from two distinct branches is allowed;
	// only a file already on the current chain is a cycle.
	visiting map[string]struct{}
	// sources is the ordered, de-duplicated list of every file touched.
	sources []string
}

// walk expands directives within node. filePath is the absolute path of the file
// node came from (anchors relative imports). chain is the active import chain for
// error context, deepest last. depth counts how many import hops led here.
func (r *importResolver) walk(node *yaml.Node, filePath string, chain []string, depth int) (*yaml.Node, error) {
	if node == nil {
		return nil, nil
	}

	// A document node wraps a single content node; unwrap and recurse so a
	// directive may appear as the document root.
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return node, nil
		}
		child, err := r.walk(node.Content[0], filePath, chain, depth)
		if err != nil {
			return nil, err
		}
		node.Content[0] = child
		return node, nil
	}

	switch cls := classifyImport(node); cls.kind {
	case importDirective:
		return r.expand(cls.target, filePath, chain, depth)
	case importMalformed:
		return nil, fmt.Errorf("%s: malformed import directive: %s", chainContext(chain), cls.reason)
	}

	// Not a directive: recurse into composite children. Mapping content
	// alternates key/value; walking both is safe because a key is always a
	// scalar and never classifies as a directive.
	switch node.Kind {
	case yaml.MappingNode, yaml.SequenceNode:
		for i, child := range node.Content {
			expanded, err := r.walk(child, filePath, chain, depth)
			if err != nil {
				return nil, err
			}
			node.Content[i] = expanded
		}
	}
	return node, nil
}

// expand resolves a single directive: validates the path, detects cycles/depth,
// reads and parses the target under the size cap, records the source, then
// recurses into the imported document.
func (r *importResolver) expand(rel, fromFile string, chain []string, depth int) (*yaml.Node, error) {
	if depth+1 > maxImportDepth {
		return nil, fmt.Errorf("import depth exceeds maximum of %d: %s", maxImportDepth, chainContext(append(chain, rel)))
	}

	abs, err := resolveImportPath(fromFile, rel)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", chainContext(chain), err)
	}

	// Cycle detection runs before any read/decode of the offending file.
	if _, looping := r.visiting[abs]; looping {
		return nil, fmt.Errorf("import cycle detected: %s", chainContext(append(chain, abs)))
	}

	data, err := readFileBounded(abs, importFileMaxBytes)
	if err != nil {
		return nil, fmt.Errorf("%s: read import %q: %w", chainContext(chain), rel, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s: parse import %q: %w", chainContext(chain), abs, err)
	}
	imported := documentRoot(&doc)

	if _, ok := r.seen[abs]; !ok {
		r.seen[abs] = struct{}{}
		r.sources = append(r.sources, abs)
	}

	r.visiting[abs] = struct{}{}
	resolved, err := r.walk(imported, abs, append(append([]string(nil), chain...), abs), depth+1)
	delete(r.visiting, abs)
	if err != nil {
		return nil, err
	}
	return resolved, nil
}

type importKind int

const (
	// importNone: the node is not an import directive; walk into it normally.
	importNone importKind = iota
	// importDirective: a well-formed value-level import; expand it.
	importDirective
	// importMalformed: "from" is present but the mapping violates the contract
	// (extra keys, or a missing/empty/non-scalar target). Reject loudly — there
	// is no merge/append/override semantics in v1.
	importMalformed
)

type importClass struct {
	kind   importKind
	target string
	reason string
}

// classifyImport decides whether node is a value-level import directive. A
// directive is a mapping with exactly one key, "from", whose value is a
// non-empty scalar. A mapping that pairs "from" with any other key, or with a
// missing/empty/non-scalar value, is malformed.
func classifyImport(node *yaml.Node) importClass {
	if node.Kind != yaml.MappingNode {
		return importClass{kind: importNone}
	}
	hasFrom := false
	var target *yaml.Node
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i]
		if key.Kind == yaml.ScalarNode && key.Value == importDirectiveKey {
			hasFrom = true
			target = node.Content[i+1]
		}
	}
	if !hasFrom {
		return importClass{kind: importNone}
	}
	// A mapping that carries "from" alongside other keys is NOT an import — it
	// is an ordinary mapping that happens to use "from" as a domain field. The
	// canonical case is a workflow transition (`{from: x, to: y}`), which every
	// real profile declares; flagging it as malformed would break the whole
	// config. Pass it through untouched so the strict decoder sees it verbatim.
	// Only a mapping whose SOLE key is "from" is a candidate import directive.
	if len(node.Content) != 2 {
		return importClass{kind: importNone}
	}
	if target == nil || target.Kind != yaml.ScalarNode {
		return importClass{kind: importMalformed, reason: "'from' value must be a scalar path"}
	}
	if strings.TrimSpace(target.Value) == "" {
		return importClass{kind: importMalformed, reason: "'from' value must not be empty"}
	}
	return importClass{kind: importDirective, target: target.Value}
}

// resolveImportPath enforces the path-safety policy. It mirrors
// resolveSubtaskKitPath (loader.go) verbatim — the only differences are the
// error-message subject ("import" instead of "subtask_kit") and that a missing
// target is reported by the downstream read rather than here.
//
// rel is resolved relative to the directory of fromFile and is rejected when it
// is absolute, carries a parent-directory ("..") segment, or — after symlink
// resolution — escapes that directory.
func resolveImportPath(fromFile, rel string) (string, error) {
	trimmed := strings.TrimSpace(rel)
	if trimmed == "" {
		return "", fmt.Errorf("import path is empty")
	}
	if filepath.IsAbs(trimmed) {
		return "", fmt.Errorf("import %q: path must be relative to %s", rel, filepath.Dir(fromFile))
	}
	for _, part := range strings.Split(filepath.ToSlash(trimmed), "/") {
		if part == ".." {
			return "", fmt.Errorf("import %q: path must not contain parent directory segments", rel)
		}
	}
	configDir := filepath.Dir(fromFile)
	joined := filepath.Join(configDir, trimmed)
	// Reject symlinks that point outside the declaring file's directory. The
	// lexical guards above catch `..` and absolute paths, but a symlink at any
	// segment can still escape; resolve the canonical path and assert it stays
	// rooted under configDir.
	resolvedRoot, err := filepath.EvalSymlinks(configDir)
	if err != nil {
		// configDir itself unreadable surfaces via the downstream open path.
		return joined, nil
	}
	resolvedJoined, err := filepath.EvalSymlinks(joined)
	if err != nil {
		// File does not exist yet (or is unreadable) — let the downstream read
		// produce the canonical not-found error.
		return joined, nil
	}
	if escapesDir(resolvedRoot, resolvedJoined) {
		return "", fmt.Errorf("import %q: resolved path %q escapes directory %q via symlink", rel, resolvedJoined, resolvedRoot)
	}
	return resolvedJoined, nil
}

// documentRoot unwraps a DocumentNode to its single content node, returning a
// null scalar for an empty document so callers always get a concrete node.
func documentRoot(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: ""}
		}
		return doc.Content[0]
	}
	return doc
}

// chainContext renders the active import chain as base names joined by " -> " so
// errors point at the failing directive without leaking absolute paths.
func chainContext(chain []string) string {
	parts := make([]string, len(chain))
	for i, p := range chain {
		parts[i] = filepath.Base(p)
	}
	return "import chain " + strings.Join(parts, " -> ")
}
