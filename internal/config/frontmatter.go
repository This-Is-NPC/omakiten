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
// leading BOM are tolerated. The returned body has its trailing newline trimmed
// to keep round-trips stable.
func SplitFrontmatter(data []byte) (frontmatter, body []byte, err error) {
	data = bytes.TrimPrefix(data, utf8BOM)
	normalized := bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))

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
		return []byte{}, []byte(strings.TrimRight(bodySection, "\n")), nil
	}

	closeIdx := strings.Index(rest, "\n"+frontmatterDelim)
	if closeIdx < 0 {
		return nil, nil, errors.New("missing closing --- frontmatter delimiter")
	}

	frontmatter = []byte(rest[:closeIdx])
	bodyStart := closeIdx + 1 + len(frontmatterDelim)
	if bodyStart >= len(rest) {
		return frontmatter, []byte{}, nil
	}
	bodySection := rest[bodyStart:]
	bodySection = strings.TrimPrefix(bodySection, "\n")
	body = []byte(strings.TrimRight(bodySection, "\n"))
	return frontmatter, body, nil
}

// JoinFrontmatter renders an entity file from its frontmatter and body, always
// terminating with a single trailing newline so editors don't add one on save.
func JoinFrontmatter(frontmatter, body []byte) []byte {
	var buf bytes.Buffer
	buf.WriteString(frontmatterDelim)
	buf.WriteByte('\n')
	buf.Write(bytes.TrimRight(frontmatter, "\n"))
	buf.WriteByte('\n')
	buf.WriteString(frontmatterDelim)
	buf.WriteByte('\n')
	if len(body) > 0 {
		buf.Write(bytes.TrimRight(body, "\n"))
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}
