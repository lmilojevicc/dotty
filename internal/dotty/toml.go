package dotty

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
)

// decodeTOML retains decoder causes and their full key segments and locations.
// In particular, StrictMissingError.Error alone omits all affected keys.
func decodeTOML(data []byte, value any) error {
	if !utf8.Valid(data) {
		return fmt.Errorf("invalid UTF-8; replace invalid bytes with valid UTF-8 before retrying")
	}
	err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(value)
	if err == nil {
		return nil
	}
	var strict *toml.StrictMissingError
	if errors.As(err, &strict) {
		details := make([]string, 0, len(strict.Errors))
		for i := range strict.Errors {
			child := &strict.Errors[i]
			segments := make([]string, len(child.Key()))
			for j, segment := range child.Key() {
				segments[j] = tomlKey(segment)
			}
			line, column := child.Position()
			details = append(
				details,
				fmt.Sprintf("%s (line %d, column %d)", strings.Join(segments, "."), line, column),
			)
		}
		return fmt.Errorf(
			"unsupported TOML fields: %s; remove unsupported fields before retrying; third-party extensions are unsupported: %w",
			strings.Join(details, ", "),
			err,
		)
	}
	var syntax *toml.DecodeError
	if errors.As(err, &syntax) {
		line, column := syntax.Position()
		return fmt.Errorf("line %d, column %d: %w", line, column, err)
	}
	return err
}

func validateTOMLString(kind, value string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf(
			"%s %q is not valid UTF-8; replace invalid bytes with valid UTF-8 before retrying",
			kind,
			value,
		)
	}
	return nil
}

// tomlBasicString encodes valid UTF-8 as a TOML basic string, not a Go literal:
// TOML has no \x, \a, or \v escapes. Non-ASCII bytes are copied verbatim so even
// invalid input is never silently replaced; callers must validate before saving.
func tomlBasicString(value string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(value) + 2)
	b.WriteByte('"')
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch c {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if c < 0x20 || c == 0x7f {
				b.WriteString(`\u00`)
				b.WriteByte(hex[c>>4])
				b.WriteByte(hex[c&0xf])
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
