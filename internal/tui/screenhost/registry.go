package screenhost

import (
	"fmt"
)

// Placement locates a screen in the legacy top/sub navigation hierarchy.
// Cyclic=false keeps sentinel routes such as Home and Project out of tab and
// comma/slash cycling while retaining one authoritative descriptor.
type Placement struct {
	Top      TopID
	TopOrder int
	SubOrder int
	Cyclic   bool
}

// PaletteMetadata identifies the palette route and positional nav code. Empty
// fields mean the screen is intentionally not palette-addressable.
type PaletteMetadata struct {
	Route    string
	Code     string
	TitleKey string
}

// Chrome describes root-owned chrome visible around a screen.
type Chrome struct {
	Navigation bool
	Footer     bool
	Help       bool
}

// DescriptorSpec is the construction form accepted by NewRegistry.
type DescriptorSpec struct {
	ID        ID
	Placement Placement
	TopLabel  string
	SubLabel  string
	Palette   PaletteMetadata
	Factory   Factory
	Chrome    Chrome
	Reload    ReloadPolicy
	HelpKeys  []string
}

// Registry is an immutable, validated screen descriptor collection. All and
// lookup methods return descriptor copies, including copied help-key slices.
type Registry struct {
	all       []DescriptorSpec
	byID      map[ID]DescriptorSpec
	byPalette map[string]DescriptorSpec
}

// NewRegistry validates stable identity, placement, palette metadata, and
// factory availability before publishing a registry.
func NewRegistry(specs []DescriptorSpec) (Registry, error) {
	if len(specs) == 0 {
		return Registry{}, fmt.Errorf("screenhost: registry requires at least one descriptor")
	}
	registry := Registry{
		all:       make([]DescriptorSpec, 0, len(specs)),
		byID:      make(map[ID]DescriptorSpec, len(specs)),
		byPalette: make(map[string]DescriptorSpec, len(specs)),
	}
	codes := make(map[string]ID, len(specs))
	cycleSlots := make(map[string]ID, len(specs))
	for _, spec := range specs {
		if err := validateDescriptor(spec, registry, codes, cycleSlots); err != nil {
			return Registry{}, err
		}
		descriptor := cloneDescriptor(spec)
		registry.all = append(registry.all, descriptor)
		registry.byID[descriptor.ID] = descriptor
		if descriptor.Palette.Route != "" {
			registry.byPalette[descriptor.Palette.Route] = descriptor
		}
	}
	return registry, nil
}

func validateDescriptor(spec DescriptorSpec, registry Registry, codes map[string]ID, cycleSlots map[string]ID) error {
	if spec.ID == "" {
		return fmt.Errorf("screenhost: descriptor has empty ID")
	}
	if spec.Factory == nil {
		return fmt.Errorf("screenhost: screen %q has nil factory", spec.ID)
	}
	if _, duplicate := registry.byID[spec.ID]; duplicate {
		return fmt.Errorf("screenhost: duplicate screen ID %q", spec.ID)
	}
	if err := validatePlacement(spec, cycleSlots); err != nil {
		return err
	}
	if err := validatePalette(spec, registry, codes); err != nil {
		return err
	}
	return validateHelp(spec)
}

func validatePlacement(spec DescriptorSpec, cycleSlots map[string]ID) error {
	if spec.Placement.Top == "" {
		return fmt.Errorf("screenhost: screen %q has empty top placement", spec.ID)
	}
	if !spec.Placement.Cyclic {
		return nil
	}
	if spec.Placement.TopOrder <= 0 || spec.Placement.SubOrder <= 0 {
		return fmt.Errorf("screenhost: cyclic screen %q has non-positive placement", spec.ID)
	}
	if spec.TopLabel == "" || spec.SubLabel == "" {
		return fmt.Errorf("screenhost: cyclic screen %q requires top and sub labels", spec.ID)
	}
	slot := fmt.Sprintf("%s/%d", spec.Placement.Top, spec.Placement.SubOrder)
	if existing, duplicate := cycleSlots[slot]; duplicate {
		return fmt.Errorf("screenhost: screens %q and %q share cycle slot %s", existing, spec.ID, slot)
	}
	cycleSlots[slot] = spec.ID
	return nil
}

func validatePalette(spec DescriptorSpec, registry Registry, codes map[string]ID) error {
	paletteSet := spec.Palette.Route != "" || spec.Palette.Code != "" || spec.Palette.TitleKey != ""
	if !paletteSet {
		return nil
	}
	if spec.Palette.Route == "" || spec.Palette.Code == "" || spec.Palette.TitleKey == "" {
		return fmt.Errorf("screenhost: screen %q has incomplete palette metadata", spec.ID)
	}
	if !validPaletteCode(spec.Palette.Code) {
		return fmt.Errorf("screenhost: screen %q has malformed palette code %q", spec.ID, spec.Palette.Code)
	}
	if spec.Palette.Route != string(spec.ID) {
		return fmt.Errorf("screenhost: screen %q palette route must match its stable ID", spec.ID)
	}
	if _, duplicate := registry.byPalette[spec.Palette.Route]; duplicate {
		return fmt.Errorf("screenhost: duplicate palette route %q", spec.Palette.Route)
	}
	if existing, duplicate := codes[spec.Palette.Code]; duplicate {
		return fmt.Errorf("screenhost: screens %q and %q share palette code %q", existing, spec.ID, spec.Palette.Code)
	}
	codes[spec.Palette.Code] = spec.ID
	return nil
}

func validateHelp(spec DescriptorSpec) error {
	if !spec.Chrome.Help {
		return nil
	}
	if len(spec.HelpKeys) == 0 {
		return fmt.Errorf("screenhost: screen %q enables help without a help key", spec.ID)
	}
	for _, key := range spec.HelpKeys {
		if key == "" {
			return fmt.Errorf("screenhost: screen %q has an empty help key", spec.ID)
		}
	}
	return nil
}

func validPaletteCode(code string) bool {
	return len(code) == 2 && code[0] >= '1' && code[0] <= '9' && code[1] >= '1' && code[1] <= '9'
}

func cloneDescriptor(descriptor DescriptorSpec) DescriptorSpec {
	descriptor.HelpKeys = append([]string(nil), descriptor.HelpKeys...)
	return descriptor
}

// All returns descriptors in declaration order.
func (r Registry) All() []DescriptorSpec {
	descriptors := make([]DescriptorSpec, len(r.all))
	for i, descriptor := range r.all {
		descriptors[i] = cloneDescriptor(descriptor)
	}
	return descriptors
}

func (r Registry) ByID(id ID) (DescriptorSpec, bool) {
	descriptor, ok := r.byID[id]
	return cloneDescriptor(descriptor), ok
}

func (r Registry) ByPaletteRoute(route string) (DescriptorSpec, bool) {
	descriptor, ok := r.byPalette[route]
	return cloneDescriptor(descriptor), ok
}
