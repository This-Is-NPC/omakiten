package config

// Buddy is the in-memory shape of a defaults/buddies/<name>.yaml or
// custom/<name>.yaml file. Every field is required — the validator
// rejects zero values rather than silently filling defaults so config
// drift surfaces at LoadBundle, never at first event.
type Buddy struct {
	Name            string                  `yaml:"name" json:"name"`
	Description     string                  `yaml:"description" json:"description"`
	Size            BuddySize               `yaml:"size" json:"size"`
	Background      string                  `yaml:"background" json:"background"`
	FrameIntervalMs int                     `yaml:"frame_interval_ms" json:"frame_interval_ms"`
	Style           string                  `yaml:"style" json:"style"`
	Border          BuddyBorder             `yaml:"border" json:"border"`
	CustomBorder    BuddyCustomBorder       `yaml:"custom_border,omitempty" json:"custom_border,omitempty"`
	Animations      map[string][]BuddyFrame `yaml:"animations" json:"animations"`
	Bubble          BuddyBubble             `yaml:"bubble" json:"bubble"`

	SourcePath string `yaml:"-" json:"-"`
	IsCustom   bool   `yaml:"-" json:"-"`
}

// BuddySize is the inner-card cell budget the renderer is allowed to
// paint — borders sit OUTSIDE this rectangle. Both fields must be > 0.
type BuddySize struct {
	Width  int `yaml:"width" json:"width"`
	Height int `yaml:"height" json:"height"`
}

// BuddyBorder is the per-buddy override for the lipgloss border shown
// around the card. Width is character cells (lipgloss caps it at 1 for
// most styles). Color/Background accept the color-resolver grammar.
type BuddyBorder struct {
	Visible    bool   `yaml:"visible" json:"visible"`
	Width      int    `yaml:"width,omitempty" json:"width,omitempty"`
	Color      string `yaml:"color,omitempty" json:"color,omitempty"`
	Background string `yaml:"background,omitempty" json:"background,omitempty"`
}

// BuddyCustomBorder is the per-side glyph table consumed when
// Buddy.Style == "custom". Ignored for the named lipgloss styles.
type BuddyCustomBorder struct {
	Top         string `yaml:"top,omitempty" json:"top,omitempty"`
	Bottom      string `yaml:"bottom,omitempty" json:"bottom,omitempty"`
	Left        string `yaml:"left,omitempty" json:"left,omitempty"`
	Right       string `yaml:"right,omitempty" json:"right,omitempty"`
	TopLeft     string `yaml:"top_left,omitempty" json:"top_left,omitempty"`
	TopRight    string `yaml:"top_right,omitempty" json:"top_right,omitempty"`
	BottomLeft  string `yaml:"bottom_left,omitempty" json:"bottom_left,omitempty"`
	BottomRight string `yaml:"bottom_right,omitempty" json:"bottom_right,omitempty"`
}

// BuddyFrame is a single ASCII frame inside an animation. Frames are
// indexed 0..N-1; the validator rejects gaps so the renderer can index
// without bounds-check fallback paths.
type BuddyFrame struct {
	Frame int    `yaml:"frame" json:"frame"`
	Value string `yaml:"value" json:"value"`
}

// BuddyBubble is the speech-balloon configuration. TailSide picks which
// edge the tail glyph attaches to ("bottom" | "top" | "left" | "right").
type BuddyBubble struct {
	TailSide string `yaml:"tail_side" json:"tail_side"`
}

// Known values used by the validator and the TUI component.
const (
	BuddyStyleRounded = "rounded"
	BuddyStyleSquare  = "square"
	BuddyStyleDouble  = "double"
	BuddyStyleThick   = "thick"
	BuddyStyleHidden  = "hidden"
	BuddyStyleCustom  = "custom"

	BuddyTailBottom = "bottom"
	BuddyTailTop    = "top"
	BuddyTailLeft   = "left"
	BuddyTailRight  = "right"
)

// BuddyStyles is the closed set of valid style values.
var BuddyStyles = []string{
	BuddyStyleRounded,
	BuddyStyleSquare,
	BuddyStyleDouble,
	BuddyStyleThick,
	BuddyStyleHidden,
	BuddyStyleCustom,
}

// BuddyTailSides is the closed set of valid bubble tail anchors.
var BuddyTailSides = []string{
	BuddyTailBottom,
	BuddyTailTop,
	BuddyTailLeft,
	BuddyTailRight,
}
