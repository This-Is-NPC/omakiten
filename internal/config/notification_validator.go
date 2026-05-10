package config

import (
	"fmt"
	"strings"
)

// ValidateNotification enforces every required field on Notification plus the cross-
// field invariants. Each notification YAML is one notification card — the
// validator rejects shapes that would produce an ambiguous or empty
// popup at render time. Errors are wrapped with the notification name + path
// so the user can pinpoint the offending file.
func ValidateNotification(notification Notification) error {
	if strings.TrimSpace(notification.Name) == "" {
		return wrapNotificationErr("", notification.SourcePath, fmt.Errorf("name is required"))
	}

	if notification.Size.Width <= 0 || notification.Size.Height <= 0 {
		return wrapNotificationErr(notification.Name, notification.SourcePath, fmt.Errorf("size.width and size.height must be > 0"))
	}

	if err := IsValidColorSyntax(notification.Background); err != nil {
		return wrapNotificationErr(notification.Name, notification.SourcePath, fmt.Errorf("background: %w", err))
	}

	// frame_interval_ms is only meaningful when an animation is
	// declared. Empty animation → field ignored. Animation present →
	// must be > 0.
	if len(notification.Animation) > 0 && notification.FrameIntervalMs <= 0 {
		return wrapNotificationErr(notification.Name, notification.SourcePath, fmt.Errorf("frame_interval_ms must be > 0 when an animation is set"))
	}

	if notification.TypingMsPerChar < 0 {
		return wrapNotificationErr(notification.Name, notification.SourcePath, fmt.Errorf("typing_ms_per_char must be >= 0"))
	}

	if err := validateNotificationStyle(notification); err != nil {
		return wrapNotificationErr(notification.Name, notification.SourcePath, err)
	}

	if err := validateNotificationBorder(notification.Border); err != nil {
		return wrapNotificationErr(notification.Name, notification.SourcePath, err)
	}

	if notification.Style == NotificationStyleCustom {
		if err := validateCustomBorder(notification.CustomBorder); err != nil {
			return wrapNotificationErr(notification.Name, notification.SourcePath, err)
		}
	}

	if err := validateAnimation(notification.Animation); err != nil {
		return wrapNotificationErr(notification.Name, notification.SourcePath, err)
	}

	if err := validateBubble(notification.Bubble); err != nil {
		return wrapNotificationErr(notification.Name, notification.SourcePath, err)
	}

	if err := validatePosition(notification.Position); err != nil {
		return wrapNotificationErr(notification.Name, notification.SourcePath, err)
	}

	if err := validateDismiss(notification.Dismiss); err != nil {
		return wrapNotificationErr(notification.Name, notification.SourcePath, err)
	}

	if err := validatePadding(notification.Padding); err != nil {
		return wrapNotificationErr(notification.Name, notification.SourcePath, err)
	}

	if err := validateFooterPosition(notification.FooterPosition); err != nil {
		return wrapNotificationErr(notification.Name, notification.SourcePath, err)
	}

	if err := validateNotificationMessage(notification); err != nil {
		return wrapNotificationErr(notification.Name, notification.SourcePath, err)
	}

	return nil
}

func validateNotificationStyle(notification Notification) error {
	if notification.Style == "" {
		return fmt.Errorf("style is required (one of %s)", strings.Join(NotificationStyles, ", "))
	}
	for _, allowed := range NotificationStyles {
		if notification.Style == allowed {
			return nil
		}
	}
	return fmt.Errorf("style %q is not one of %s", notification.Style, strings.Join(NotificationStyles, ", "))
}

func validateNotificationBorder(border NotificationBorder) error {
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

func validateCustomBorder(cb NotificationCustomBorder) error {
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

func validateAnimation(frames []NotificationFrame) error {
	// Animation is optional — a notification without an ASCII frame
	// renders as a plain bubble. When provided, frame indices must
	// still be contiguous 0..N-1 and every frame value must be set.
	if len(frames) == 0 {
		return nil
	}
	seen := make([]bool, len(frames))
	for _, frame := range frames {
		if frame.Frame < 0 || frame.Frame >= len(frames) {
			return fmt.Errorf("animation.frame %d is outside the contiguous range 0..%d", frame.Frame, len(frames)-1)
		}
		if seen[frame.Frame] {
			return fmt.Errorf("animation.frame %d declared more than once", frame.Frame)
		}
		if frame.Value == "" {
			return fmt.Errorf("animation.frame %d: value is required", frame.Frame)
		}
		seen[frame.Frame] = true
	}
	return nil
}

// validateBubble lets bubble.tail_side be empty (default = "bottom"
// when an animation is present; ignored when not). When set, it
// must be one of the closed enum values.
func validateBubble(bubble NotificationBubble) error {
	if bubble.TailSide == "" {
		return nil
	}
	for _, allowed := range NotificationTailSides {
		if bubble.TailSide == allowed {
			return nil
		}
	}
	return fmt.Errorf("bubble.tail_side %q is not one of %s", bubble.TailSide, strings.Join(NotificationTailSides, ", "))
}

func validatePadding(p NotificationPadding) error {
	for name, v := range map[string]int{"top": p.Top, "right": p.Right, "bottom": p.Bottom, "left": p.Left} {
		if v < 0 {
			return fmt.Errorf("padding.%s must be >= 0, got %d", name, v)
		}
	}
	return nil
}

func validateFooterPosition(position string) error {
	if position == "" {
		return nil
	}
	for _, allowed := range NotificationFooterPositions {
		if position == allowed {
			return nil
		}
	}
	return fmt.Errorf("footer_position %q is not one of %s", position, strings.Join(NotificationFooterPositions, ", "))
}

func validatePosition(position string) error {
	if position == "" {
		return fmt.Errorf("position is required (one of %s)", strings.Join(NotificationPositions, ", "))
	}
	for _, allowed := range NotificationPositions {
		if position == allowed {
			return nil
		}
	}
	return fmt.Errorf("position %q is not one of %s", position, strings.Join(NotificationPositions, ", "))
}

func validateDismiss(d NotificationDismiss) error {
	if d.Mode == "" {
		return fmt.Errorf("dismiss.mode is required (one of %s)", strings.Join(NotificationDismissModes, ", "))
	}
	known := false
	for _, allowed := range NotificationDismissModes {
		if d.Mode == allowed {
			known = true
			break
		}
	}
	if !known {
		return fmt.Errorf("dismiss.mode %q is not one of %s", d.Mode, strings.Join(NotificationDismissModes, ", "))
	}
	switch d.Mode {
	case NotificationDismissModeKey:
		if len(d.Keys) == 0 {
			return fmt.Errorf("dismiss.keys must be non-empty when dismiss.mode=key")
		}
	case NotificationDismissModeTimeout:
		if d.AfterMs <= 0 {
			return fmt.Errorf("dismiss.after_ms must be > 0 when dismiss.mode=timeout")
		}
	}
	for i, key := range d.Keys {
		if key == "" {
			return fmt.Errorf("dismiss.keys[%d] must not be empty", i)
		}
	}
	return nil
}

func validateNotificationMessage(notification Notification) error {
	hasMessage := strings.TrimSpace(notification.Message) != ""
	hasField := strings.TrimSpace(notification.MessageField) != ""
	if hasMessage && hasField {
		return fmt.Errorf("message and message_field are mutually exclusive — pick one")
	}
	// Both empty is allowed at the notification layer: the hook entry can
	// supply the message instead. The hooks_validator enforces that
	// (notification + hook) together resolve to at least one source.
	return nil
}

func wrapNotificationErr(name, path string, err error) error {
	switch {
	case name != "" && path != "":
		return fmt.Errorf("notification %q (%s): %w", name, path, err)
	case path != "":
		return fmt.Errorf("notification at %s: %w", path, err)
	case name != "":
		return fmt.Errorf("notification %q: %w", name, err)
	default:
		return fmt.Errorf("notification: %w", err)
	}
}
