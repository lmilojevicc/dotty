// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE.go-testing file.

package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Selection splitting, rewriting, and first-matching-alternative precedence
// follow Go 1.26's src/testing/match.go. Go's regexp package is also the test
// driver's matcher. A plain strings.Split or full-name regexp is NOT equivalent:
// only unbracketed, unparenthesized slashes and pipes separate filters.
// Emitted names are already rewritten and uniquified by testing.
type selection [][]*regexp.Regexp

func compileSelection(pattern string) (selection, error) {
	var result selection
	for _, alternative := range splitPattern(pattern) {
		var parts []*regexp.Regexp
		for _, part := range alternative {
			r, err := regexp.Compile(rewritePattern(part))
			if err != nil {
				return nil, fmt.Errorf("invalid -run pattern %q: %w", pattern, err)
			}
			parts = append(parts, r)
		}
		result = append(result, parts)
	}
	return result, nil
}

func (s selection) fullMatch(name string) bool {
	parts := strings.Split(name, "/")
nextAlternative:
	for _, alternative := range s {
		for i, part := range parts {
			if i >= len(alternative) {
				break
			}
			if !alternative[i].MatchString(part) {
				continue nextAlternative
			}
		}
		// testing chooses the first matching alternative, even if partial.
		return len(parts) >= len(alternative)
	}
	return false
}

func splitPattern(s string) [][]string {
	var alternatives [][]string
	var parts []string
	classes, parentheses := 0, 0
	for i := 0; i < len(s); {
		switch s[i] {
		case '[':
			classes++
		case ']':
			classes--
			if classes < 0 {
				classes = 0
			}
		case '(':
			if classes == 0 {
				parentheses++
			}
		case ')':
			if classes == 0 {
				parentheses--
			}
		case '\\':
			i++
		case '/', '|':
			if classes == 0 && parentheses == 0 {
				separator := s[i]
				parts = append(parts, s[:i])
				s = s[i+1:]
				i = 0
				if separator == '|' {
					alternatives = append(alternatives, parts)
					parts = nil
				}
				continue
			}
		}
		i++
	}
	return append(alternatives, append(parts, s))
}

func rewritePattern(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case testingSpace(r):
			b.WriteByte('_')
		case !strconv.IsPrint(r):
			quoted := strconv.QuoteRune(r)
			b.WriteString(quoted[1 : len(quoted)-1])
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// This is testing's whitespace set, not Unicode's Z category.
func testingSpace(r rune) bool {
	if r >= 0x2000 && r <= 0x200a {
		return true
	}
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x85, 0xa0, 0x1680,
		0x2028, 0x2029, 0x202f, 0x205f, 0x3000:
		return true
	}
	return false
}
