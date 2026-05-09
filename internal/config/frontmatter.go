package config

import (
	"bytes"
	"errors"
	"strings"
)

// frontmatterDelim is the YAML frontmatter fence used by every entity file.
const frontmatterDelim = "---"

// utf8BOM is the optional byte-order mark some editors prepend.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// SplitFrontmatter separates the YAML frontmatter from the markdown body of an
// entity file. The fence is `---` on its own line. CRLF line endings and a
// leading BOM are tolerated. Both the returned frontmatter and body have their
// trailing `\r` and `\n` trimmed so that round-tripping through JoinFrontmatter
// is stable — without that, FuzzSplitFrontmatter caught Split→Join→Split drift
// when a section happened to end in a lone CR (carriage return survives the
// CRLF replacement pass at the top of this function but JoinFrontmatter would
// strip it on the next write).
func SplitFrontmatter(data []byte) (frontmatter, body []byte, err error) {
	data = bytes.TrimPrefix(data, utf8BOM)
	// Repeated ReplaceAll until idempotent: a single pass leaves overlap-ish
	// shapes like `\r\r\n` as `\r\n` (the engine matches non-overlappingly
	// left-to-right), which would silently re-introduce CRLF on the next
	// Split call. Loop until no `\r\n` survives so the output is canonical.
	normalized := data
	for {
		next := bytes.ReplaceAll(normalized, []byte("\r\n"), []byte("\n"))
		if bytes.Equal(next, normalized) {
			break
		}
		normalized = next
	}

	text := string(normalized)
	if !strings.HasPrefix(text, frontmatterDelim) {
		return nil, nil, errors.New("missing opening --- frontmatter delimiter")
	}

	rest := text[len(frontmatterDelim):]
	if !strings.HasPrefix(rest, "\n") {
		return nil, nil, errors.New("opening --- must be followed by a newline")
	}
	rest = rest[1:]

	// Empty-frontmatter case: `---\n---\n...` — no characters between the two
	// fences. Treat the immediate `---\n` as the close.
	if strings.HasPrefix(rest, frontmatterDelim+"\n") || rest == frontmatterDelim {
		bodyStart := len(frontmatterDelim)
		if bodyStart >= len(rest) {
			return []byte{}, []byte{}, nil
		}
		bodySection := rest[bodyStart:]
		bodySection = strings.TrimPrefix(bodySection, "\n")
		return []byte{}, []byte(strings.TrimRight(bodySection, "\r\n")), nil
	}

	closeIdx := strings.Index(rest, "\n"+frontmatterDelim)
	if closeIdx < 0 {
		return nil, nil, errors.New("missing closing --- frontmatter delimiter")
	}

	frontmatter = []byte(strings.TrimRight(rest[:closeIdx], "\r\n"))
	bodyStart := closeIdx + 1 + len(frontmatterDelim)
	if bodyStart >= len(rest) {
		return frontmatter, []byte{}, nil
	}
	bodySection := rest[bodyStart:]
	bodySection = strings.TrimPrefix(bodySection, "\n")
	body = []byte(strings.TrimRight(bodySection, "\r\n"))
	return frontmatter, body, nil
}

// JoinFrontmatter renders an entity file from its frontmatter and body, always
// terminating with a single trailing newline so editors don't add one on save.
// Trailing CR is stripped alongside LF: SplitFrontmatter normalizes CRLF to LF
// on read, so a body that ends with `\r` would survive Split but get collapsed
// to no-trailer when re-encoded — caught as a Split→Join→Split round-trip
// drift by FuzzSplitFrontmatter.
func JoinFrontmatter(frontmatter, body []byte) []byte {
	var buf bytes.Buffer
	buf.WriteString(frontmatterDelim)
	buf.WriteByte('\n')
	buf.Write(bytes.TrimRight(frontmatter, "\r\n"))
	buf.WriteByte('\n')
	buf.WriteString(frontmatterDelim)
	buf.WriteByte('\n')
	body = bytes.TrimRight(body, "\r\n")
	if len(body) > 0 {
		buf.Write(body)
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}
