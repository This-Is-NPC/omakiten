package config

// Notification is the in-memory shape of a defaults/notifications/<slug>.yaml or
// custom/<slug>.yaml file. One file declares ONE notification card:
// visuals, animation, position, dismiss, and message all live here so
// the omakiten.yaml hook entry only has to name the notification slug.
//
// Every field is required unless explicitly marked optional; the
// validator rejects zero values rather than silently filling defaults
// so config drift surfaces at LoadBundle, never at first event.
type Notification struct {
	Name            string            `yaml:"name" json:"name"`
	Description     string            `yaml:"description" json:"description"`
	Size            NotificationSize         `yaml:"size" json:"size"`
	Background      string            `yaml:"background" json:"background"`
	FrameIntervalMs int               `yaml:"frame_interval_ms" json:"frame_interval_ms"`
	Style           string            `yaml:"style" json:"style"`
	Border          NotificationBorder       `yaml:"border" json:"border"`
	CustomBorder    NotificationCustomBorder `yaml:"custom_border,omitempty" json:"custom_border,omitempty"`
	Animation       []NotificationFrame      `yaml:"animation" json:"animation"`
	Bubble          NotificationBubble       `yaml:"bubble" json:"bubble"`
	Padding         NotificationPadding      `yaml:"padding,omitempty" json:"padding,omitempty"`
	AutoHeight      *bool                    `yaml:"auto_height,omitempty" json:"auto_height,omitempty"`
	PaddingInside   bool                     `yaml:"padding_inside,omitempty" json:"padding_inside,omitempty"`
	FooterVisible   bool                     `yaml:"footer_visible,omitempty" json:"footer_visible,omitempty"`
	Position        string            `yaml:"position" json:"position"`
	Dismiss         NotificationDismiss      `yaml:"dismiss" json:"dismiss"`
	TypingMsPerChar int               `yaml:"typing_ms_per_char" json:"typing_ms_per_char"`
	Message         string            `yaml:"message,omitempty" json:"message,omitempty"`
	MessageField    string            `yaml:"message_field,omitempty" json:"message_field,omitempty"`

	SourcePath string `yaml:"-" json:"-"`
	IsCustom   bool   `yaml:"-" json:"-"`
}

// NotificationDismiss is the close-strategy for the rendered card. Mode
// chooses key|timeout|next_status; Keys is required for key, AfterMs
// for timeout. The renderer rejects shapes that pair the wrong field
// with the chosen mode at validate time.
type NotificationDismiss struct {
	Mode    string   `yaml:"mode" json:"mode"`
	Keys    []string `yaml:"keys,omitempty" json:"keys,omitempty"`
	AfterMs int      `yaml:"after_ms,omitempty" json:"after_ms,omitempty"`
}

// NotificationSize is the inner-card cell budget the renderer is allowed to
// paint — borders sit OUTSIDE this rectangle. Both fields must be > 0.
type NotificationSize struct {
	Width  int `yaml:"width" json:"width"`
	Height int `yaml:"height" json:"height"`
}

// NotificationBorder is the per-notification override for the lipgloss border shown
// around the card. Width is character cells (lipgloss caps it at 1 for
// most styles). Color/Background accept the color-resolver grammar.
type NotificationBorder struct {
	Visible    bool   `yaml:"visible" json:"visible"`
	Width      int    `yaml:"width,omitempty" json:"width,omitempty"`
	Color      string `yaml:"color,omitempty" json:"color,omitempty"`
	Background string `yaml:"background,omitempty" json:"background,omitempty"`
}

// NotificationCustomBorder is the per-side glyph table consumed when
// Notification.Style == "custom". Ignored for the named lipgloss styles.
type NotificationCustomBorder struct {
	Top         string `yaml:"top,omitempty" json:"top,omitempty"`
	Bottom      string `yaml:"bottom,omitempty" json:"bottom,omitempty"`
	Left        string `yaml:"left,omitempty" json:"left,omitempty"`
	Right       string `yaml:"right,omitempty" json:"right,omitempty"`
	TopLeft     string `yaml:"top_left,omitempty" json:"top_left,omitempty"`
	TopRight    string `yaml:"top_right,omitempty" json:"top_right,omitempty"`
	BottomLeft  string `yaml:"bottom_left,omitempty" json:"bottom_left,omitempty"`
	BottomRight string `yaml:"bottom_right,omitempty" json:"bottom_right,omitempty"`
}

// NotificationPadding is the per-side cell pad applied INSIDE the
// border. Each value is the row/column count to inset; zero means
// flush against the border. The block is optional — omit to render
// the body flush against the border.
type NotificationPadding struct {
	Top    int `yaml:"top,omitempty" json:"top,omitempty"`
	Right  int `yaml:"right,omitempty" json:"right,omitempty"`
	Bottom int `yaml:"bottom,omitempty" json:"bottom,omitempty"`
	Left   int `yaml:"left,omitempty" json:"left,omitempty"`
}

// NotificationFrame is a single ASCII frame inside an animation. Frames are
// indexed 0..N-1; the validator rejects gaps so the renderer can index
// without bounds-check fallback paths.
type NotificationFrame struct {
	Frame int    `yaml:"frame" json:"frame"`
	Value string `yaml:"value" json:"value"`
}

// NotificationBubble is the speech-balloon configuration. TailSide picks which
// edge the tail glyph attaches to ("bottom" | "top" | "left" | "right").
type NotificationBubble struct {
	TailSide string `yaml:"tail_side" json:"tail_side"`
}

// Known values used by the validator and the TUI component.
const (
	NotificationStyleRounded = "rounded"
	NotificationStyleSquare  = "square"
	NotificationStyleDouble  = "double"
	NotificationStyleThick   = "thick"
	NotificationStyleHidden  = "hidden"
	NotificationStyleCustom  = "custom"

	NotificationTailBottom = "bottom"
	NotificationTailTop    = "top"
	NotificationTailLeft   = "left"
	NotificationTailRight  = "right"
)

// NotificationStyles is the closed set of valid style values.
var NotificationStyles = []string{
	NotificationStyleRounded,
	NotificationStyleSquare,
	NotificationStyleDouble,
	NotificationStyleThick,
	NotificationStyleHidden,
	NotificationStyleCustom,
}

// NotificationTailSides is the closed set of valid bubble tail anchors.
var NotificationTailSides = []string{
	NotificationTailBottom,
	NotificationTailTop,
	NotificationTailLeft,
	NotificationTailRight,
}

// Notification dismiss + position enum closed sets.
const (
	NotificationDismissModeKey        = "key"
	NotificationDismissModeTimeout    = "timeout"
	NotificationDismissModeNextStatus = "next_status"

	NotificationPositionTopLeft      = "top-left"
	NotificationPositionTopCenter    = "top-center"
	NotificationPositionTopRight     = "top-right"
	NotificationPositionMiddleLeft   = "middle-left"
	NotificationPositionCenter       = "center"
	NotificationPositionMiddleRight  = "middle-right"
	NotificationPositionBottomLeft   = "bottom-left"
	NotificationPositionBottomCenter = "bottom-center"
	NotificationPositionBottomRight  = "bottom-right"
)

// NotificationDismissModes is the closed set of dismiss modes.
var NotificationDismissModes = []string{
	NotificationDismissModeKey,
	NotificationDismissModeTimeout,
	NotificationDismissModeNextStatus,
}

// NotificationPositions is the closed set of nine fixed anchor names.
var NotificationPositions = []string{
	NotificationPositionTopLeft, NotificationPositionTopCenter, NotificationPositionTopRight,
	NotificationPositionMiddleLeft, NotificationPositionCenter, NotificationPositionMiddleRight,
	NotificationPositionBottomLeft, NotificationPositionBottomCenter, NotificationPositionBottomRight,
}
