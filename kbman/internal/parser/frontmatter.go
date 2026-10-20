package parser

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"kbman/internal/metadata"

	"gopkg.in/yaml.v3"
)

// Document represents a Markdown file with parsed YAML Frontmatter and raw content.
type Document struct {
	Meta    metadata.Metadata
	Content string

	rawYAML []string
	scalar  map[string]string
	blocks  map[string][]string
	order   []string
	hasHead bool
}

var topLevelKeyRegex = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_-]*)\s*:`)

// Parse extracts YAML Frontmatter and remaining Markdown content from raw bytes.
func Parse(data []byte) (*Document, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	text := string(data)

	lines := strings.Split(text, "\n")
	firstIdx := -1
	secondIdx := -1

	for i, line := range lines {
		clean := strings.TrimRight(line, "\r")
		if clean == "---" {
			if firstIdx == -1 {
				if strings.TrimSpace(line) != clean {
					continue
				}
				firstIdx = i
			} else {
				secondIdx = i
				break
			}
		}
	}

	if firstIdx != -1 && firstIdx != 0 {
		firstIdx = -1
	}

	if firstIdx == -1 || secondIdx == -1 {
		return &Document{
			Meta:    metadata.Metadata{},
			Content: text,
			scalar:  map[string]string{},
			blocks:  map[string][]string{},
		}, nil
	}

	rawYAML := make([]string, secondIdx-firstIdx-1)
	for i := firstIdx + 1; i < secondIdx; i++ {
		rawYAML[i-firstIdx-1] = strings.TrimRight(lines[i], "\r")
	}
	content := strings.Join(lines[secondIdx+1:], "\n")

	meta, err := unmarshalMeta(strings.Join(rawYAML, "\n"))
	if err != nil {
		return nil, fmt.Errorf("unmarshal frontmatter error: %w", err)
	}

	return &Document{
		Meta:    meta,
		Content: content,
		rawYAML: rawYAML,
		scalar:  map[string]string{},
		blocks:  map[string][]string{},
		hasHead: true,
	}, nil
}

func unmarshalMeta(yamlBlock string) (metadata.Metadata, error) {
	var meta metadata.Metadata
	if err := yaml.Unmarshal([]byte(yamlBlock), &meta); err == nil {
		return meta, nil
	}

	// Retry with a tolerant rewrite of unquoted scalar values that contain
	// colons or backticks (a common pitfall in hand-written frontmatter).
	if err := yaml.Unmarshal([]byte(sanitizeYAML(yamlBlock)), &meta); err != nil {
		return metadata.Metadata{}, err
	}
	return meta, nil
}

// sanitizeYAML rewrites unquoted `description:` values into block scalars.
func sanitizeYAML(y string) string {
	lines := strings.Split(y, "\n")
	out := make([]string, 0, len(lines))

	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if !strings.HasPrefix(trimmed, "description:") {
			out = append(out, l)
			continue
		}

		val := strings.TrimSpace(strings.TrimPrefix(trimmed, "description:"))
		if strings.HasPrefix(val, `"`) || strings.HasPrefix(val, `'`) ||
			strings.HasPrefix(val, "|") || strings.HasPrefix(val, ">") {
			out = append(out, l)
			continue
		}

		out = append(out, "description: |-")
		out = append(out, "  "+val)
	}

	return strings.Join(out, "\n")
}

// HasKey reports whether the frontmatter already declares a top-level key.
func (d *Document) HasKey(key string) bool {
	for _, line := range d.rawYAML {
		if k, ok := topLevelKey(line); ok && k == key {
			return true
		}
	}
	return false
}

// SetScalar replaces (or appends) a top-level scalar field.
func (d *Document) SetScalar(key, value string) {
	if d.scalar == nil {
		d.scalar = map[string]string{}
	}
	if _, exists := d.scalar[key]; !exists {
		d.order = append(d.order, key)
	}
	d.scalar[key] = renderScalar(value)
}

// SetBlock replaces (or appends) a top-level field with raw YAML lines.
func (d *Document) SetBlock(key string, lines []string) {
	if d.blocks == nil {
		d.blocks = map[string][]string{}
	}
	if _, exists := d.blocks[key]; !exists {
		d.order = append(d.order, key)
	}
	d.blocks[key] = lines
}

// Render reconstructs the Markdown file, preserving untouched frontmatter lines
// verbatim and only rewriting the fields that were explicitly set.
func (d *Document) Render() ([]byte, error) {
	body := make([]string, 0, len(d.rawYAML)+len(d.order)+2)
	seen := map[string]bool{}

	skipIndented := false
	for _, line := range d.rawYAML {
		if skipIndented {
			if isIndented(line) || strings.TrimSpace(line) == "" {
				continue
			}
			skipIndented = false
		}

		key, ok := topLevelKey(line)
		if !ok {
			body = append(body, line)
			continue
		}

		if block, exists := d.blocks[key]; exists {
			body = append(body, block...)
			seen[key] = true
			skipIndented = true
			continue
		}
		if val, exists := d.scalar[key]; exists {
			body = append(body, key+": "+val)
			seen[key] = true
			skipIndented = true
			continue
		}

		body = append(body, line)
	}

	for _, key := range d.order {
		if seen[key] {
			continue
		}
		if block, exists := d.blocks[key]; exists {
			body = append(body, block...)
			continue
		}
		if val, exists := d.scalar[key]; exists {
			body = append(body, key+": "+val)
		}
	}

	var buf bytes.Buffer
	if d.hasHead {
		buf.WriteString("---\n")
		for _, line := range body {
			buf.WriteString(line)
			buf.WriteString("\n")
		}
		buf.WriteString("---")
		buf.WriteString("\n")
	}

	buf.WriteString(d.Content)

	return buf.Bytes(), nil
}

func topLevelKey(line string) (string, bool) {
	if isIndented(line) {
		return "", false
	}
	m := topLevelKeyRegex.FindStringSubmatch(line)
	if m == nil {
		return "", false
	}
	return m[1], true
}

func isIndented(line string) bool {
	return strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")
}

var plainScalarRegex = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_./-]*$`)

// renderScalar emits a YAML scalar, quoting only when required.
func renderScalar(v string) string {
	if v == "" {
		return `""`
	}
	// Identifiers such as `verified` or `windows` stay unquoted; numeric IDs,
	// dates and CJK slugs are quoted so yaml.v3 keeps them as strings.
	if plainScalarRegex.MatchString(v) && v != "true" && v != "false" && v != "null" {
		return v
	}
	return strconvQuote(v)
}

func strconvQuote(v string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`)
	return `"` + replacer.Replace(v) + `"`
}
