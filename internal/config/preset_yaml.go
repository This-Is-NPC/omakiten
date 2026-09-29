package config

import (
	"bytes"
	"os"
	"reflect"
	"sort"

	"gopkg.in/yaml.v3"
)

func readableYAML(node *yaml.Node) ([]byte, error) {
	compactYAML(node)
	return encodeYAML(node)
}

func encodeYAML(value any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(value); err != nil {
		_ = enc.Close()
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func compactYAML(node *yaml.Node) {
	for _, child := range node.Content {
		compactYAML(child)
	}
	if node.Kind != yaml.MappingNode && node.Kind != yaml.SequenceNode {
		return
	}
	node.Style &^= yaml.FlowStyle
	if len(node.Content) == 0 {
		return
	}
	for _, child := range node.Content {
		if child.Kind != yaml.ScalarNode || child.Style&yaml.LiteralStyle != 0 || child.HeadComment != "" || child.LineComment != "" || child.FootComment != "" {
			return
		}
	}
	node.Style |= yaml.FlowStyle
	raw, err := yaml.Marshal(node)
	if err != nil || len(raw) > 96 {
		node.Style &^= yaml.FlowStyle
	}
}

func presetConfigFiles(w wiring) (map[string]PresetFile, error) {
	var root yaml.Node
	if err := root.Encode(w); err != nil {
		return nil, err
	}
	files := make(map[string]*yaml.Node)
	settings := mappingValueByKey(&root, "config")
	for _, key := range []string{"views", "events", "hooks"} {
		movePresetSection(settings, key, key+".yaml", "", files)
	}
	for _, section := range []struct{ key, file, fragment string }{
		{"config", "settings.yaml", ""},
		{"workflows", "workflows.yaml", ""},
		{"surfaces", "surfaces.yaml", ""},
		{"skills", "catalog.yaml", "skills"},
		{"laws", "catalog.yaml", "laws"},
		{"personas", "personas.yaml", ""},
		{"commands", "bindings.yaml", ""},
	} {
		movePresetSection(&root, section.key, section.file, section.fragment, files)
	}
	files["preset.yaml"] = &root
	out := make(map[string]PresetFile, len(files))
	for name, node := range files {
		raw, err := readableYAML(node)
		if err != nil {
			return nil, err
		}
		out["config/"+name] = PresetFile{Content: string(raw), Mode: 0o644}
	}
	return out, nil
}

func movePresetSection(parent *yaml.Node, key, file, fragment string, files map[string]*yaml.Node) {
	if parent == nil {
		return
	}
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value != key {
			continue
		}
		target := "./" + file
		if fragment == "" {
			files[file] = parent.Content[i+1]
		} else {
			if files[file] == nil {
				files[file] = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			}
			files[file].Content = append(files[file].Content, parent.Content[i], parent.Content[i+1])
			target += "#" + fragment
		}
		parent.Content[i+1] = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "from"},
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: target},
		}}
		return
	}
}

type presetYAMLEditor struct {
	docs  map[string]*yaml.Node
	dirty map[string]bool
}

// savePresetWiring updates the sources that own changed configuration values.
func savePresetWiring(path string, after wiring) error {
	bundle, err := LoadBundle(path)
	if err != nil {
		return err
	}
	before, err := bundleToWiring(bundle)
	if err != nil {
		return err
	}
	var oldNode, newNode yaml.Node
	if err := oldNode.Encode(before); err != nil {
		return err
	}
	if err := newNode.Encode(after); err != nil {
		return err
	}
	editor := presetYAMLEditor{docs: make(map[string]*yaml.Node), dirty: make(map[string]bool)}
	source, err := editor.read(path, "")
	if err != nil {
		return err
	}
	if err := editor.patch(source, &oldNode, &newNode, path); err != nil {
		return err
	}
	return editor.publish()
}

func (e *presetYAMLEditor) publish() error {
	paths := make([]string, 0, len(e.dirty))
	for name := range e.dirty {
		paths = append(paths, name)
	}
	sort.Strings(paths)
	for _, name := range paths {
		raw, err := readableYAML(e.docs[name])
		if err != nil {
			return err
		}
		existing, err := readFileBounded(name, MaxWiringFileBytes)
		if err != nil {
			return err
		}
		if bytes.Equal(existing, raw) {
			continue
		}
		info, err := os.Stat(name)
		if err != nil {
			return err
		}
		if err := WriteAtomic(name, raw); err != nil {
			return err
		}
		if err := os.Chmod(name, info.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

func (e *presetYAMLEditor) read(path, fragment string) (*yaml.Node, error) {
	if e.docs[path] == nil {
		raw, err := readFileBounded(path, MaxWiringFileBytes)
		if err != nil {
			return nil, err
		}
		var doc yaml.Node
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			return nil, err
		}
		e.docs[path] = &doc
	}
	root := documentRoot(e.docs[path])
	if fragment == "" {
		return root, nil
	}
	return extractFragment(root, fragment, path)
}

func (e *presetYAMLEditor) imported(node *yaml.Node, path string) (*yaml.Node, string, error) {
	file, fragment := parseImportPath(node.Value)
	target, err := resolveImportPath(path, file)
	if err != nil {
		return nil, "", err
	}
	imported, err := e.read(target, fragment)
	return imported, target, err
}

func (e *presetYAMLEditor) patch(source, before, after *yaml.Node, path string) error {
	if sameYAMLValue(before, after) {
		return nil
	}
	if class := classifyImport(source); class.kind == importDirective {
		target, file, err := e.imported(mappingValueByKey(source, "from"), path)
		if err != nil {
			return err
		}
		return e.patch(target, before, after, file)
	}
	if source.Kind == yaml.MappingNode && before != nil && after != nil && before.Kind == yaml.MappingNode && after.Kind == yaml.MappingNode {
		return e.patchMapping(source, before, after, path)
	}
	if source.Kind == yaml.SequenceNode && before != nil && after != nil && before.Kind == yaml.SequenceNode && after.Kind == yaml.SequenceNode && len(source.Content) == len(before.Content) && len(before.Content) == len(after.Content) {
		for i := range source.Content {
			if err := e.patch(source.Content[i], before.Content[i], after.Content[i], path); err != nil {
				return err
			}
		}
		return nil
	}
	head, line, foot := source.HeadComment, source.LineComment, source.FootComment
	*source = *after
	source.HeadComment, source.LineComment, source.FootComment = head, line, foot
	e.dirty[path] = true
	return nil
}

func (e *presetYAMLEditor) patchMapping(source, before, after *yaml.Node, path string) error {
	for _, key := range mappingKeys(before, after) {
		oldValue, newValue := mappingValueByKey(before, key), mappingValueByKey(after, key)
		if sameYAMLValue(oldValue, newValue) {
			continue
		}
		owner, ownerPath := source, path
		if mappingValueByKey(owner, key) == nil && mappingValueByKey(owner, "merge_from") != nil {
			var err error
			owner, ownerPath, err = e.imported(mappingValueByKey(owner, "merge_from"), path)
			if err != nil {
				return err
			}
		}
		if err := e.patchKey(owner, key, oldValue, newValue, ownerPath); err != nil {
			return err
		}
	}
	return nil
}

func mappingKeys(nodes ...*yaml.Node) []string {
	keys := make(map[string]bool)
	for _, node := range nodes {
		for i := 0; i+1 < len(node.Content); i += 2 {
			keys[node.Content[i].Value] = true
		}
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	return ordered
}

func (e *presetYAMLEditor) patchKey(owner *yaml.Node, key string, before, after *yaml.Node, path string) error {
	for i := 0; i+1 < len(owner.Content); i += 2 {
		if owner.Content[i].Value != key {
			continue
		}
		if after == nil {
			if classifyImport(owner.Content[i+1]).kind == importDirective {
				return e.patch(owner.Content[i+1], before, &yaml.Node{Kind: before.Kind, Tag: before.Tag}, path)
			}
			owner.Content = append(owner.Content[:i], owner.Content[i+2:]...)
			e.dirty[path] = true
			return nil
		}
		return e.patch(owner.Content[i+1], before, after, path)
	}
	if after != nil {
		owner.Content = append(owner.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, after)
		e.dirty[path] = true
	}
	return nil
}

func sameYAMLValue(a, b *yaml.Node) bool {
	if a == nil || b == nil {
		return a == b
	}
	var av, bv any
	return a.Decode(&av) == nil && b.Decode(&bv) == nil && reflect.DeepEqual(av, bv)
}
