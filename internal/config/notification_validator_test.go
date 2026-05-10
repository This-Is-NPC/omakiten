package config

import (
	"strings"
	"testing"
)

func validNotification() Notification {
	return Notification{
		Name:            "kit",
		Description:     "test",
		Size:            NotificationSize{Width: 20, Height: 6},
		Background:      "transparent",
		FrameIntervalMs: 500,
		Style:           NotificationStyleRounded,
		Border:          NotificationBorder{Visible: boolPtr(true), Width: 1, Color: "#ffffff"},
		Animation:       []NotificationFrame{{Frame: 0, Value: "x"}},
		Bubble:          NotificationBubble{TailSide: NotificationTailBottom},
		Padding:         zeroNotificationPadding(),
		AutoHeight:      boolPtr(false),
		PaddingInside:   boolPtr(true),
		FooterVisible:   boolPtr(true),
		FooterPosition:  NotificationFooterCenter,
		Position:        NotificationPositionCenter,
		Dismiss:         NotificationDismiss{Mode: NotificationDismissModeKey, Keys: []string{"esc"}},
		TypingMsPerChar: intPtr(0),
		MessageField:    "hint",
	}
}

func boolPtr(v bool) *bool { return &v }

func intPtr(v int) *int { return &v }

func zeroNotificationPadding() *NotificationPadding {
	return &NotificationPadding{Top: intPtr(0), Right: intPtr(0), Bottom: intPtr(0), Left: intPtr(0)}
}

func TestValidateNotification_happyPath(t *testing.T) {
	if err := ValidateNotification(validNotification()); err != nil {
		t.Fatalf("happy path failed: %v", err)
	}
}

func TestValidateNotification_rejectsEmptyName(t *testing.T) {
	b := validNotification()
	b.Name = ""
	if err := ValidateNotification(b); err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateNotification_rejectsZeroSize(t *testing.T) {
	b := validNotification()
	b.Size.Width = 0
	if err := ValidateNotification(b); err == nil {
		t.Fatalf("expected error for zero width")
	}
	b = validNotification()
	b.Size.Height = -1
	if err := ValidateNotification(b); err == nil {
		t.Fatalf("expected error for negative height")
	}
}

func TestValidateNotification_rejectsBadBackground(t *testing.T) {
	b := validNotification()
	b.Background = "blue"
	if err := ValidateNotification(b); err == nil || !strings.Contains(err.Error(), "background") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateNotification_rejectsZeroFrameInterval(t *testing.T) {
	b := validNotification()
	b.FrameIntervalMs = 0
	if err := ValidateNotification(b); err == nil || !strings.Contains(err.Error(), "frame_interval_ms") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateNotification_rejectsNegativeTyping(t *testing.T) {
	b := validNotification()
	b.TypingMsPerChar = intPtr(-1)
	if err := ValidateNotification(b); err == nil || !strings.Contains(err.Error(), "typing_ms_per_char") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateNotification_rejectsUnknownStyle(t *testing.T) {
	b := validNotification()
	b.Style = "ninja"
	if err := ValidateNotification(b); err == nil || !strings.Contains(err.Error(), "style") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateNotification_styleCustomNeedsAllSides(t *testing.T) {
	b := validNotification()
	b.Style = NotificationStyleCustom
	if err := ValidateNotification(b); err == nil || !strings.Contains(err.Error(), "custom_border") {
		t.Fatalf("got %v", err)
	}
	b.CustomBorder = NotificationCustomBorder{
		Top: "-", Bottom: "-", Left: "|", Right: "|",
		TopLeft: "+", TopRight: "+", BottomLeft: "+", BottomRight: "+",
	}
	if err := ValidateNotification(b); err != nil {
		t.Fatalf("complete custom_border still fails: %v", err)
	}
}

func TestValidateNotification_visibleBorderNeedsWidthAndColor(t *testing.T) {
	b := validNotification()
	b.Border.Width = 0
	if err := ValidateNotification(b); err == nil || !strings.Contains(err.Error(), "border.width") {
		t.Fatalf("got %v", err)
	}
	b = validNotification()
	b.Border.Color = ""
	if err := ValidateNotification(b); err == nil || !strings.Contains(err.Error(), "border.color") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateNotification_invisibleBorderSkipsColor(t *testing.T) {
	b := validNotification()
	b.Border = NotificationBorder{Visible: boolPtr(false)}
	if err := ValidateNotification(b); err != nil {
		t.Fatalf("invisible border with empty color should pass: %v", err)
	}
}

// TestValidateNotification_emptyAnimationOK pins the relaxed contract:
// notifications without an ASCII frame are valid (plain-text bubbles).
// frame_interval_ms is then optional too — irrelevant without frames.
func TestValidateNotification_emptyAnimationOK(t *testing.T) {
	b := validNotification()
	b.Animation = nil
	b.FrameIntervalMs = 0
	if err := ValidateNotification(b); err != nil {
		t.Fatalf("notification without animation should be valid; got %v", err)
	}
}

func TestValidateNotification_animationRequiresFrameInterval(t *testing.T) {
	b := validNotification()
	b.FrameIntervalMs = 0
	if err := ValidateNotification(b); err == nil || !strings.Contains(err.Error(), "frame_interval_ms") {
		t.Fatalf("expected frame_interval_ms error when animation set, got %v", err)
	}
}

func TestValidateNotification_animationWithNonContiguousFrames(t *testing.T) {
	b := validNotification()
	b.Animation = []NotificationFrame{{Frame: 0, Value: "a"}, {Frame: 5, Value: "b"}}
	if err := ValidateNotification(b); err == nil || !strings.Contains(err.Error(), "contiguous") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateNotification_duplicateFrameIndex(t *testing.T) {
	b := validNotification()
	b.Animation = []NotificationFrame{{Frame: 0, Value: "a"}, {Frame: 0, Value: "b"}}
	if err := ValidateNotification(b); err == nil || !strings.Contains(err.Error(), "more than once") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateNotification_emptyFrameValue(t *testing.T) {
	b := validNotification()
	b.Animation = []NotificationFrame{{Frame: 0, Value: ""}}
	if err := ValidateNotification(b); err == nil || !strings.Contains(err.Error(), "value is required") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateNotification_invalidTailSide(t *testing.T) {
	b := validNotification()
	b.Bubble.TailSide = "elsewhere"
	if err := ValidateNotification(b); err == nil || !strings.Contains(err.Error(), "tail_side") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateNotification_invalidPosition(t *testing.T) {
	b := validNotification()
	b.Position = "north-pole"
	if err := ValidateNotification(b); err == nil || !strings.Contains(err.Error(), "position") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateNotification_invalidFooterPosition(t *testing.T) {
	b := validNotification()
	b.FooterPosition = "bottom"
	if err := ValidateNotification(b); err == nil || !strings.Contains(err.Error(), "footer_position") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateNotification_footerPositionRequiredWhenFooterVisible(t *testing.T) {
	b := validNotification()
	b.FooterPosition = ""
	if err := ValidateNotification(b); err == nil || !strings.Contains(err.Error(), "footer_position") {
		t.Fatalf("expected footer_position error, got %v", err)
	}
	b.FooterVisible = boolPtr(false)
	if err := ValidateNotification(b); err != nil {
		t.Fatalf("empty footer_position should pass when footer hidden: %v", err)
	}
	b.FooterPosition = NotificationFooterRight
	if err := ValidateNotification(b); err != nil {
		t.Fatalf("right footer_position should pass: %v", err)
	}
}

func TestValidateNotification_requiresExplicitBehaviourFields(t *testing.T) {
	tests := map[string]func(*Notification){
		"auto_height":    func(n *Notification) { n.AutoHeight = nil },
		"padding_inside": func(n *Notification) { n.PaddingInside = nil },
		"footer_visible": func(n *Notification) { n.FooterVisible = nil },
		"border.visible": func(n *Notification) { n.Border.Visible = nil },
		"padding.left":   func(n *Notification) { n.Padding.Left = nil },
	}
	for want, mutate := range tests {
		b := validNotification()
		mutate(&b)
		if err := ValidateNotification(b); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: got %v", want, err)
		}
	}
}

func TestValidateNotification_dismissKeyNeedsKeys(t *testing.T) {
	b := validNotification()
	b.Dismiss = NotificationDismiss{Mode: NotificationDismissModeKey}
	if err := ValidateNotification(b); err == nil || !strings.Contains(err.Error(), "dismiss.keys") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateNotification_dismissTimeoutNeedsAfterMs(t *testing.T) {
	b := validNotification()
	b.Dismiss = NotificationDismiss{Mode: NotificationDismissModeTimeout}
	if err := ValidateNotification(b); err == nil || !strings.Contains(err.Error(), "after_ms") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateNotification_dismissTimeoutAllowsKeys(t *testing.T) {
	b := validNotification()
	b.Dismiss = NotificationDismiss{Mode: NotificationDismissModeTimeout, Keys: []string{"esc"}, AfterMs: 12000}
	if err := ValidateNotification(b); err != nil {
		t.Fatalf("timeout with manual close keys should pass: %v", err)
	}
}

func TestValidateNotification_dismissRejectsEmptyKey(t *testing.T) {
	b := validNotification()
	b.Dismiss = NotificationDismiss{Mode: NotificationDismissModeTimeout, Keys: []string{""}, AfterMs: 12000}
	if err := ValidateNotification(b); err == nil || !strings.Contains(err.Error(), "dismiss.keys") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateNotification_dismissModeUnknown(t *testing.T) {
	b := validNotification()
	b.Dismiss = NotificationDismiss{Mode: "shake"}
	if err := ValidateNotification(b); err == nil || !strings.Contains(err.Error(), "dismiss.mode") {
		t.Fatalf("got %v", err)
	}
}

// TestValidateNotification_messageOptional pins the relaxed contract: a
// notification YAML without message/message_field is valid. The hook entry
// (or ev.Body fallback) is allowed to supply the bubble text — the
// hooks_validator covers the combined-presence rule.
func TestValidateNotification_messageOptional(t *testing.T) {
	b := validNotification()
	b.MessageField = ""
	b.Message = ""
	if err := ValidateNotification(b); err != nil {
		t.Fatalf("notification with no message source should be valid at the notification layer; got %v", err)
	}
}

func TestValidateNotification_messageAndFieldExclusive(t *testing.T) {
	b := validNotification()
	b.Message = "literal"
	if err := ValidateNotification(b); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateNotification_acceptsLiteralMessage(t *testing.T) {
	b := validNotification()
	b.MessageField = ""
	b.Message = "hello"
	if err := ValidateNotification(b); err != nil {
		t.Fatalf("literal message should pass: %v", err)
	}
}

func TestValidateNotification_errorIncludesNameAndPath(t *testing.T) {
	b := validNotification()
	b.SourcePath = "/tmp/kit.yaml"
	b.Style = "bogus"
	err := ValidateNotification(b)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), `notification "kit"`) || !strings.Contains(err.Error(), "/tmp/kit.yaml") {
		t.Fatalf("error should cite name + path, got %v", err)
	}
}
