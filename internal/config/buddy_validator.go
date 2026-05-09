package config

import (
	"fmt"
	"sort"
	"strings"
)

// ValidateBuddy enforces every required field on Buddy plus the cross-
// field invariants (style=custom needs a complete CustomBorder, visible
// borders need a width and color, animations have contiguous frames,
// colors match the resolver grammar). Errors are wrapped with the
// buddy name + source path so the user can pinpoint which file is at
// fault even when several buddies are loaded.
func ValidateBuddy(buddy Buddy) error {
	if strings.TrimSpace(buddy.Name) == "" {
		return wrapBuddyErr("", buddy.SourcePath, fmt.Errorf("name is required"))
	}

	if buddy.Size.Width <= 0 || buddy.Size.Height <= 0 {
		return wrapBuddyErr(buddy.Name, buddy.SourcePath, fmt.Errorf("size.width and size.height must be > 0"))
	}

	if err := IsValidColorSyntax(buddy.Background); err != nil {
		return wrapBuddyErr(buddy.Name, buddy.SourcePath, fmt.Errorf("background: %w", err))
	}

	if buddy.FrameIntervalMs <= 0 {
		return wrapBuddyErr(buddy.Name, buddy.SourcePath, fmt.Errorf("frame_interval_ms must be > 0"))
	}

	if err := validateBuddyStyle(buddy); err != nil {
		return wrapBuddyErr(buddy.Name, buddy.SourcePath, err)
	}

	if err := validateBuddyBorder(buddy.Border); err != nil {
		return wrapBuddyErr(buddy.Name, buddy.SourcePath, err)
	}

	if buddy.Style == BuddyStyleCustom {
		if err := validateCustomBorder(buddy.CustomBorder); err != nil {
			return wrapBuddyErr(buddy.Name, buddy.SourcePath, err)
		}
	}

	if len(buddy.Animations) == 0 {
		return wrapBuddyErr(buddy.Name, buddy.SourcePath, fmt.Errorf("animations: at least one animation is required"))
	}
	for _, animName := range sortedKeys(buddy.Animations) {
		if err := validateAnimation(animName, buddy.Animations[animName]); err != nil {
			return wrapBuddyErr(buddy.Name, buddy.SourcePath, err)
		}
	}

	if err := validateBubble(buddy.Bubble); err != nil {
		return wrapBuddyErr(buddy.Name, buddy.SourcePath, err)
	}

	return nil
}

func validateBuddyStyle(buddy Buddy) error {
	if buddy.Style == "" {
		return fmt.Errorf("style is required (one of %s)", strings.Join(BuddyStyles, ", "))
	}
	for _, allowed := range BuddyStyles {
		if buddy.Style == allowed {
			return nil
		}
	}
	return fmt.Errorf("style %q is not one of %s", buddy.Style, strings.Join(BuddyStyles, ", "))
}

func validateBuddyBorder(border BuddyBorder) error {
	if !border.Visible {
		return nil
	}
	if border.Width <= 0 {
		return fmt.Errorf("border.width must be > 0 when border.visible is true")
	}
	if strings.TrimSpace(border.Color) == "" {
		return fmt.Errorf("border.color is required when border.visible is true")
	}
	if err := IsValidColorSyntax(border.Color); err != nil {
		return fmt.Errorf("border.color: %w", err)
	}
	if strings.TrimSpace(border.Background) != "" {
		if err := IsValidColorSyntax(border.Background); err != nil {
			return fmt.Errorf("border.background: %w", err)
		}
	}
	return nil
}

func validateCustomBorder(cb BuddyCustomBorder) error {
	missing := []string{}
	if cb.Top == "" {
		missing = append(missing, "top")
	}
	if cb.Bottom == "" {
		missing = append(missing, "bottom")
	}
	if cb.Left == "" {
		missing = append(missing, "left")
	}
	if cb.Right == "" {
		missing = append(missing, "right")
	}
	if cb.TopLeft == "" {
		missing = append(missing, "top_left")
	}
	if cb.TopRight == "" {
		missing = append(missing, "top_right")
	}
	if cb.BottomLeft == "" {
		missing = append(missing, "bottom_left")
	}
	if cb.BottomRight == "" {
		missing = append(missing, "bottom_right")
	}
	if len(missing) > 0 {
		return fmt.Errorf("custom_border requires %s when style=custom", strings.Join(missing, ", "))
	}
	return nil
}

func validateAnimation(name string, frames []BuddyFrame) error {
	if len(frames) == 0 {
		return fmt.Errorf("animations.%s: at least one frame is required", name)
	}
	seen := make([]bool, len(frames))
	for _, frame := range frames {
		if frame.Frame < 0 || frame.Frame >= len(frames) {
			return fmt.Errorf("animations.%s.frame %d is outside the contiguous range 0..%d", name, frame.Frame, len(frames)-1)
		}
		if seen[frame.Frame] {
			return fmt.Errorf("animations.%s.frame %d declared more than once", name, frame.Frame)
		}
		if frame.Value == "" {
			return fmt.Errorf("animations.%s.frame %d: value is required", name, frame.Frame)
		}
		seen[frame.Frame] = true
	}
	return nil
}

func validateBubble(bubble BuddyBubble) error {
	if bubble.TailSide == "" {
		return fmt.Errorf("bubble.tail_side is required (one of %s)", strings.Join(BuddyTailSides, ", "))
	}
	for _, allowed := range BuddyTailSides {
		if bubble.TailSide == allowed {
			return nil
		}
	}
	return fmt.Errorf("bubble.tail_side %q is not one of %s", bubble.TailSide, strings.Join(BuddyTailSides, ", "))
}

func wrapBuddyErr(name, path string, err error) error {
	switch {
	case name != "" && path != "":
		return fmt.Errorf("buddy %q (%s): %w", name, path, err)
	case path != "":
		return fmt.Errorf("buddy at %s: %w", path, err)
	case name != "":
		return fmt.Errorf("buddy %q: %w", name, err)
	default:
		return fmt.Errorf("buddy: %w", err)
	}
}

func sortedKeys(m map[string][]BuddyFrame) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
