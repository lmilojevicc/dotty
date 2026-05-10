package cli

import (
	"fmt"
	"io"
	"strings"
)

func RenderError(out io.Writer, err error) {
	if err == nil {
		return
	}
	message, hint := splitDiagnosticHint(err.Error())
	if strings.HasPrefix(message, "usage: ") {
		fmt.Fprintf(out, "%s invalid arguments\n", errorStyle.Render("error:"))
		fmt.Fprintf(out, "  %s\n", message)
	} else {
		fmt.Fprintf(out, "%s %s\n", errorStyle.Render("error:"), message)
	}
	if hint != "" {
		fmt.Fprintf(out, "%s %s\n", hintStyle.Render("hint:"), hint)
	}
}

func splitDiagnosticHint(message string) (string, string) {
	if idx := strings.LastIndex(message, " ("); idx >= 0 && strings.HasSuffix(message, ")") {
		hint := strings.TrimSuffix(message[idx+2:], ")")
		if isDiagnosticHint(hint) {
			return message[:idx], hint
		}
	}
	if before, hint, ok := strings.Cut(message, "; "); ok && strings.HasPrefix(hint, "run `") {
		return before, hint
	}
	return message, ""
}

func isDiagnosticHint(hint string) bool {
	for _, prefix := range []string{
		"choose ",
		"edit ",
		"inspect ",
		"or use ",
		"remove ",
		"restore ",
		"run `",
		"use ",
	} {
		if strings.HasPrefix(hint, prefix) {
			return true
		}
	}
	return false
}
