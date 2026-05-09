package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/lmilojevicc/dotty/internal/dotty"
)

var stateStyles = map[dotty.State]lipgloss.Style{
	dotty.StateLinked:        lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true),
	dotty.StateUnlinked:      lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true),
	dotty.StatePartial:       lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true),
	dotty.StateConflict:      lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true),
	dotty.StateMissingSource: lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true),
	dotty.StateEmpty:         lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Bold(true),
	dotty.StateUntracked:     lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Bold(true),
}

func renderAddResult(out io.Writer, result *dotty.AddResult) {
	verb := "added"
	if result.DryRun {
		verb = "would add"
	}
	fmt.Fprintf(
		out,
		"%s %s: %s -> %s\n",
		successStyle.Render(verb),
		packageStyle.Render(result.Package),
		pathStyle.Render(result.Target),
		pathStyle.Render(result.SourcePath),
	)
}

func renderLinkResults(out io.Writer, results []dotty.LinkResult) {
	for _, result := range results {
		verb := "linked"
		if result.DryRun {
			verb = "would link"
		}
		fmt.Fprintf(
			out,
			"%s %s: %s -> %s\n",
			successStyle.Render(verb),
			packageStyle.Render(result.Package),
			pathStyle.Render(result.Target),
			pathStyle.Render(result.SourcePath),
		)
	}
}

func renderUnlinkResults(out io.Writer, results []dotty.UnlinkResult) {
	for _, result := range results {
		verb := "unlinked"
		note := "copy left"
		if result.Hard {
			verb = "hard-unlinked"
			note = "link removed"
		}
		if result.DryRun {
			verb = "would unlink"
			if result.Hard {
				verb = "would hard-unlink"
			}
		}
		fmt.Fprintf(
			out,
			"%s %s: %s (%s)\n",
			successStyle.Render(verb),
			packageStyle.Render(result.Package),
			pathStyle.Render(result.Target),
			mutedStyle.Render(note),
		)
	}
}

func renderStatus(out io.Writer, report *dotty.StatusReport, verbose bool) {
	if verbose {
		renderVerboseStatus(out, report)
		return
	}
	for _, pkg := range report.Packages {
		renderPadded(out, pkg.Name, packageStyle, 24)
		fmt.Fprintf(out, " %s\n", renderState(pkg.State))
	}
	if len(report.Untracked) > 0 {
		if len(report.Packages) > 0 {
			fmt.Fprintln(out)
		}
		fmt.Fprintln(out, renderState(dotty.StateUntracked))
		for _, item := range report.Untracked {
			fmt.Fprintf(out, "  %s\n", pathStyle.Render(item.Path))
		}
	}
}

func renderVerboseStatus(out io.Writer, report *dotty.StatusReport) {
	for _, pkg := range report.Packages {
		if len(pkg.Entries) == 0 {
			renderVerboseStatusRow(out, pkg.Name, "-", "-", pkg.State)
			continue
		}
		for _, entry := range pkg.Entries {
			renderVerboseStatusRow(out, entry.Package, entry.Source, entry.Target, entry.State)
		}
	}
	if len(report.Untracked) > 0 {
		if len(report.Packages) > 0 {
			fmt.Fprintln(out)
		}
		for _, item := range report.Untracked {
			renderVerboseUntrackedRow(out, item.Path, item.State)
		}
	}
}

func renderVerboseStatusRow(out io.Writer, packageName, source, target string, state dotty.State) {
	renderPadded(out, packageName, packageStyle, 18)
	fmt.Fprint(out, " ")
	renderPadded(out, source, sourceStyle, 20)
	fmt.Fprint(out, " ")
	renderPadded(out, target, pathStyle, 36)
	fmt.Fprintf(out, " %s\n", renderState(state))
}

func renderVerboseUntrackedRow(out io.Writer, path string, state dotty.State) {
	renderPadded(out, "-", mutedStyle, 18)
	fmt.Fprint(out, " ")
	renderPadded(out, path, sourceStyle, 20)
	fmt.Fprint(out, " ")
	renderPadded(out, "-", mutedStyle, 36)
	fmt.Fprintf(out, " %s\n", renderState(state))
}

func renderPadded(out io.Writer, text string, style lipgloss.Style, width int) {
	fmt.Fprint(out, style.Render(text))
	if padding := width - lipgloss.Width(text); padding > 0 {
		fmt.Fprint(out, strings.Repeat(" ", padding))
	}
}

func renderInventory(out io.Writer, inventory *dotty.Inventory) {
	fmt.Fprintln(out, packageStyle.Render("Packages"))
	if len(inventory.Packages) == 0 {
		fmt.Fprintf(out, "  %s\n", mutedStyle.Render("none"))
	} else {
		for _, pkg := range inventory.Packages {
			label := "links"
			if pkg.LinkCount == 1 {
				label = "link"
			}
			fmt.Fprintf(
				out,
				"  %-24s %s\n",
				packageStyle.Render(pkg.Name),
				mutedStyle.Render(fmt.Sprintf("%d %s", pkg.LinkCount, label)),
			)
		}
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, packageStyle.Render("Collections"))
	if len(inventory.Collections) == 0 {
		fmt.Fprintf(out, "  %s\n", mutedStyle.Render("none"))
		return
	}
	for _, collection := range inventory.Collections {
		fmt.Fprintf(
			out,
			"  %-24s %s\n",
			packageStyle.Render(collection.Name),
			strings.Join(collection.Packages, ", "),
		)
	}
}

func renderState(state dotty.State) string {
	if style, ok := stateStyles[state]; ok {
		return style.Render(string(state))
	}
	return string(state)
}
