package config

import (
	"strings"
	"testing"
)

func validBuddy() Buddy {
	return Buddy{
		Name:            "kit",
		Description:     "test",
		Size:            BuddySize{Width: 20, Height: 6},
		Background:      "transparent",
		FrameIntervalMs: 500,
		Style:           BuddyStyleRounded,
		Border:          BuddyBorder{Visible: true, Width: 1, Color: "#ffffff"},
		Animations: map[string][]BuddyFrame{
			"idle": {{Frame: 0, Value: "x"}},
		},
		Bubble: BuddyBubble{TailSide: BuddyTailBottom},
	}
}

func TestValidateBuddy_happyPath(t *testing.T) {
	if err := ValidateBuddy(validBuddy()); err != nil {
		t.Fatalf("happy path failed: %v", err)
	}
}

func TestValidateBuddy_rejectsEmptyName(t *testing.T) {
	b := validBuddy()
	b.Name = ""
	err := ValidateBuddy(b)
	if err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateBuddy_rejectsZeroSize(t *testing.T) {
	b := validBuddy()
	b.Size.Width = 0
	if err := ValidateBuddy(b); err == nil {
		t.Fatalf("expected error for zero width")
	}
	b = validBuddy()
	b.Size.Height = -1
	if err := ValidateBuddy(b); err == nil {
		t.Fatalf("expected error for negative height")
	}
}

func TestValidateBuddy_rejectsBadBackground(t *testing.T) {
	b := validBuddy()
	b.Background = "blue"
	err := ValidateBuddy(b)
	if err == nil || !strings.Contains(err.Error(), "background") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateBuddy_rejectsZeroFrameInterval(t *testing.T) {
	b := validBuddy()
	b.FrameIntervalMs = 0
	err := ValidateBuddy(b)
	if err == nil || !strings.Contains(err.Error(), "frame_interval_ms") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateBuddy_rejectsUnknownStyle(t *testing.T) {
	b := validBuddy()
	b.Style = "ninja"
	err := ValidateBuddy(b)
	if err == nil || !strings.Contains(err.Error(), "style") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateBuddy_styleCustomNeedsAllSides(t *testing.T) {
	b := validBuddy()
	b.Style = BuddyStyleCustom
	err := ValidateBuddy(b)
	if err == nil || !strings.Contains(err.Error(), "custom_border") {
		t.Fatalf("got %v", err)
	}
	b.CustomBorder = BuddyCustomBorder{
		Top: "-", Bottom: "-", Left: "|", Right: "|",
		TopLeft: "+", TopRight: "+", BottomLeft: "+", BottomRight: "+",
	}
	if err := ValidateBuddy(b); err != nil {
		t.Fatalf("complete custom_border still fails: %v", err)
	}
}

func TestValidateBuddy_visibleBorderNeedsWidthAndColor(t *testing.T) {
	b := validBuddy()
	b.Border.Width = 0
	if err := ValidateBuddy(b); err == nil || !strings.Contains(err.Error(), "border.width") {
		t.Fatalf("got %v", err)
	}
	b = validBuddy()
	b.Border.Color = ""
	if err := ValidateBuddy(b); err == nil || !strings.Contains(err.Error(), "border.color") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateBuddy_invisibleBorderSkipsColor(t *testing.T) {
	b := validBuddy()
	b.Border = BuddyBorder{Visible: false}
	if err := ValidateBuddy(b); err != nil {
		t.Fatalf("invisible border with empty color should pass: %v", err)
	}
}

func TestValidateBuddy_emptyAnimationsRejected(t *testing.T) {
	b := validBuddy()
	b.Animations = nil
	if err := ValidateBuddy(b); err == nil || !strings.Contains(err.Error(), "animations") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateBuddy_animationWithZeroFrames(t *testing.T) {
	b := validBuddy()
	b.Animations = map[string][]BuddyFrame{"idle": {}}
	if err := ValidateBuddy(b); err == nil || !strings.Contains(err.Error(), "at least one frame") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateBuddy_nonContiguousFrames(t *testing.T) {
	b := validBuddy()
	b.Animations = map[string][]BuddyFrame{"idle": {{Frame: 0, Value: "a"}, {Frame: 5, Value: "b"}}}
	if err := ValidateBuddy(b); err == nil || !strings.Contains(err.Error(), "contiguous") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateBuddy_duplicateFrameIndex(t *testing.T) {
	b := validBuddy()
	b.Animations = map[string][]BuddyFrame{"idle": {{Frame: 0, Value: "a"}, {Frame: 0, Value: "b"}}}
	if err := ValidateBuddy(b); err == nil || !strings.Contains(err.Error(), "more than once") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateBuddy_emptyFrameValue(t *testing.T) {
	b := validBuddy()
	b.Animations = map[string][]BuddyFrame{"idle": {{Frame: 0, Value: ""}}}
	if err := ValidateBuddy(b); err == nil || !strings.Contains(err.Error(), "value is required") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateBuddy_invalidTailSide(t *testing.T) {
	b := validBuddy()
	b.Bubble.TailSide = "elsewhere"
	if err := ValidateBuddy(b); err == nil || !strings.Contains(err.Error(), "tail_side") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateBuddy_emptyTailSide(t *testing.T) {
	b := validBuddy()
	b.Bubble.TailSide = ""
	if err := ValidateBuddy(b); err == nil || !strings.Contains(err.Error(), "tail_side is required") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateBuddy_errorIncludesNameAndPath(t *testing.T) {
	b := validBuddy()
	b.SourcePath = "/tmp/kit.yaml"
	b.Style = "bogus"
	err := ValidateBuddy(b)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), `buddy "kit"`) || !strings.Contains(err.Error(), "/tmp/kit.yaml") {
		t.Fatalf("error should cite name + path, got %v", err)
	}
}

func TestValidateBuddy_themeReferenceColorAccepted(t *testing.T) {
	b := validBuddy()
	b.Border.Color = "$theme.primary"
	b.Background = "$theme.background"
	if err := ValidateBuddy(b); err != nil {
		t.Fatalf("theme refs should pass at LoadBundle time: %v", err)
	}
}
