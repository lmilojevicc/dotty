package cli

import (
	"fmt"
	"io"
	"strings"

	"dotty/internal/dotty"

	"github.com/charmbracelet/lipgloss"
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
	fmt.Fprintf(out, "%s %s: %s -> %s\n", successStyle.Render("added"), packageStyle.Render(result.Package), pathStyle.Render(result.Target), pathStyle.Render(result.SourcePath))
}

func renderLinkResults(out io.Writer, results []dotty.LinkResult) {
	for _, result := range results {
		fmt.Fprintf(out, "%s %s: %s -> %s\n", successStyle.Render("linked"), packageStyle.Render(result.Package), pathStyle.Render(result.Target), pathStyle.Render(result.SourcePath))
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
		fmt.Fprintf(out, "%s %s: %s (%s)\n", successStyle.Render(verb), packageStyle.Render(result.Package), pathStyle.Render(result.Target), mutedStyle.Render(note))
	}
}

func renderStatus(out io.Writer, report *dotty.StatusReport, verbose bool) {
	if verbose {
		renderVerboseStatus(out, report)
		return
	}
	for _, pkg := range report.Packages {
		fmt.Fprintf(out, "%-24s %s\n", packageStyle.Render(pkg.Name), renderState(pkg.State))
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
			fmt.Fprintf(out, "%-18s %-20s %-36s %s\n", packageStyle.Render(pkg.Name), mutedStyle.Render("-"), mutedStyle.Render("-"), renderState(pkg.State))
			continue
		}
		for _, entry := range pkg.Entries {
			fmt.Fprintf(out, "%-18s %-20s %-36s %s\n", packageStyle.Render(entry.Package), sourceStyle.Render(entry.Source), pathStyle.Render(entry.Target), renderState(entry.State))
		}
	}
	if len(report.Untracked) > 0 {
		if len(report.Packages) > 0 {
			fmt.Fprintln(out)
		}
		for _, item := range report.Untracked {
			fmt.Fprintf(out, "%-18s %-20s %-36s %s\n", mutedStyle.Render("-"), sourceStyle.Render(item.Path), mutedStyle.Render("-"), renderState(dotty.StateUntracked))
		}
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
			fmt.Fprintf(out, "  %-24s %s\n", packageStyle.Render(pkg.Name), mutedStyle.Render(fmt.Sprintf("%d %s", pkg.LinkCount, label)))
		}
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, packageStyle.Render("Collections"))
	if len(inventory.Collections) == 0 {
		fmt.Fprintf(out, "  %s\n", mutedStyle.Render("none"))
		return
	}
	for _, collection := range inventory.Collections {
		fmt.Fprintf(out, "  %-24s %s\n", packageStyle.Render(collection.Name), strings.Join(collection.Packages, ", "))
	}
}

func renderState(state dotty.State) string {
	if style, ok := stateStyles[state]; ok {
		return style.Render(string(state))
	}
	return string(state)
}
