package buddy

import (
	"fmt"
	"strings"
)

// ArgKey constants name the keys recognised inside a buddy.show
// hook's args block. Centralising them here lets the validator and
// the action share one source of truth.
const (
	ArgAnimation       = "animation"
	ArgPosition        = "position"
	ArgTypingMsPerChar = "typing_ms_per_char"
	ArgDismiss         = "dismiss"
	ArgMessageField    = "message_field"
	ArgFrameIntervalMs = "frame_interval_ms"

	DismissArgMode    = "mode"
	DismissArgKeys    = "keys"
	DismissArgAfterMs = "after_ms"
)

// ParsedArgs is the strongly-typed projection of the hook's args map.
// FrameIntervalMs is zero when the hook did not supply it; the action
// falls back to the buddy yaml's value at execute time. MessageField
// and Animation are required; the validator rejects empty strings.
type ParsedArgs struct {
	Animation       string
	Position        Position
	TypingMsPerChar int
	Dismiss         DismissConfig
	MessageField    string
	FrameIntervalMs int
}

// ParseArgs lifts the YAML-decoded map into ParsedArgs and runs the
// per-field shape checks. Animation is NOT validated against the
// active buddy's catalog here — that lives in ValidateShowArgs which
// also knows the buddy.
func ParseArgs(args map[string]any) (ParsedArgs, error) {
	out := ParsedArgs{}

	anim, err := requireString(args, ArgAnimation)
	if err != nil {
		return out, err
	}
	out.Animation = anim

	posStr, err := requireString(args, ArgPosition)
	if err != nil {
		return out, err
	}
	if !IsValidPosition(posStr) {
		return out, fmt.Errorf("args.%s %q is not one of %v", ArgPosition, posStr, positionList())
	}
	out.Position = Position(posStr)

	typingMs, err := requireInt(args, ArgTypingMsPerChar)
	if err != nil {
		return out, err
	}
	if typingMs < 0 {
		return out, fmt.Errorf("args.%s must be >= 0, got %d", ArgTypingMsPerChar, typingMs)
	}
	out.TypingMsPerChar = typingMs

	field, err := requireString(args, ArgMessageField)
	if err != nil {
		return out, err
	}
	out.MessageField = field

	dismiss, err := parseDismiss(args)
	if err != nil {
		return out, err
	}
	out.Dismiss = dismiss

	if raw, ok := args[ArgFrameIntervalMs]; ok {
		v, err := toInt(ArgFrameIntervalMs, raw)
		if err != nil {
			return out, err
		}
		if v <= 0 {
			return out, fmt.Errorf("args.%s must be > 0 when set", ArgFrameIntervalMs)
		}
		out.FrameIntervalMs = v
	}

	return out, nil
}

func parseDismiss(args map[string]any) (DismissConfig, error) {
	raw, ok := args[ArgDismiss]
	if !ok {
		return DismissConfig{}, fmt.Errorf("args.%s is required", ArgDismiss)
	}
	mp, err := toMap(ArgDismiss, raw)
	if err != nil {
		return DismissConfig{}, err
	}
	mode, err := requireString(mp, DismissArgMode)
	if err != nil {
		return DismissConfig{}, fmt.Errorf("args.%s.%w", ArgDismiss, err)
	}
	cfg := DismissConfig{Mode: DismissMode(mode)}
	switch DismissMode(mode) {
	case DismissModeKey:
		keys, err := requireStringSlice(mp, DismissArgKeys)
		if err != nil {
			return DismissConfig{}, fmt.Errorf("args.%s.%w", ArgDismiss, err)
		}
		if len(keys) == 0 {
			return DismissConfig{}, fmt.Errorf("args.%s.%s must be non-empty when mode=key", ArgDismiss, DismissArgKeys)
		}
		cfg.Keys = keys
	case DismissModeTimeout:
		after, err := requireInt(mp, DismissArgAfterMs)
		if err != nil {
			return DismissConfig{}, fmt.Errorf("args.%s.%w", ArgDismiss, err)
		}
		if after <= 0 {
			return DismissConfig{}, fmt.Errorf("args.%s.%s must be > 0 when mode=timeout", ArgDismiss, DismissArgAfterMs)
		}
		cfg.AfterMs = after
	case DismissModeNextStatus:
		// no extra fields
	default:
		return DismissConfig{}, fmt.Errorf("args.%s.%s %q is not one of %v", ArgDismiss, DismissArgMode, mode, dismissModeList())
	}
	return cfg, nil
}

func requireString(args map[string]any, key string) (string, error) {
	raw, ok := args[key]
	if !ok {
		return "", fmt.Errorf("args.%s is required", key)
	}
	s, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("args.%s must be a string, got %T", key, raw)
	}
	if strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("args.%s must not be empty", key)
	}
	return s, nil
}

func requireInt(args map[string]any, key string) (int, error) {
	raw, ok := args[key]
	if !ok {
		return 0, fmt.Errorf("args.%s is required", key)
	}
	return toInt(key, raw)
}

func toInt(key string, raw any) (int, error) {
	switch v := raw.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		if v != float64(int(v)) {
			return 0, fmt.Errorf("args.%s must be an integer, got %v", key, v)
		}
		return int(v), nil
	default:
		return 0, fmt.Errorf("args.%s must be an integer, got %T", key, raw)
	}
}

func toMap(key string, raw any) (map[string]any, error) {
	switch v := raw.(type) {
	case map[string]any:
		return v, nil
	case map[any]any:
		out := make(map[string]any, len(v))
		for k, val := range v {
			ks, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("args.%s key %v is not a string", key, k)
			}
			out[ks] = val
		}
		return out, nil
	default:
		return nil, fmt.Errorf("args.%s must be a mapping, got %T", key, raw)
	}
}

func requireStringSlice(args map[string]any, key string) ([]string, error) {
	raw, ok := args[key]
	if !ok {
		return nil, fmt.Errorf("%s is required", key)
	}
	switch v := raw.(type) {
	case []string:
		return v, nil
	case []any:
		out := make([]string, 0, len(v))
		for i, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s[%d] must be a string, got %T", key, i, item)
			}
			out = append(out, s)
		}
		return out, nil
	}
	return nil, fmt.Errorf("%s must be an array of strings, got %T", key, raw)
}

func positionList() []string {
	out := make([]string, 0, len(Positions))
	for _, p := range Positions {
		out = append(out, string(p))
	}
	return out
}

func dismissModeList() []string {
	out := make([]string, 0, len(DismissModes))
	for _, m := range DismissModes {
		out = append(out, string(m))
	}
	return out
}
