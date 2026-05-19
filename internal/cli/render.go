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
	dotty.StateBlocked:       lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true),
	dotty.StateMissingSource: lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true),
	dotty.StateEmpty:         lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Bold(true),
	dotty.StateUntracked:     lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Bold(true),
}

const (
	// Package summaries use one compact name column followed by state.
	statusPackageColumnWidth = 24
	// Verbose status preserves stable columns without table borders.
	verbosePackageColumnWidth = 18
	verboseSourceColumnWidth  = 20
	verboseTargetColumnWidth  = 36
)

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
			verb = linkDryRunVerb(result.Action)
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

func linkDryRunVerb(action string) string {
	switch action {
	case dotty.LinkResultActionCreate:
		return "would create link"
	case dotty.LinkResultActionNoop:
		return "already linked"
	case dotty.LinkResultActionNormalize:
		return "would normalize link"
	case dotty.LinkResultActionReplaceConflict:
		return "would replace conflict"
	default:
		return "would link"
	}
}

func renderUnlinkResults(out io.Writer, results []dotty.UnlinkResult) {
	for _, result := range results {
		verb := "unlinked"
		note := "link removed"
		if result.LeaveCopy {
			note = "copy left"
		}
		if result.DryRun {
			verb, note = unlinkDryRunVerbAndNote(result.Action, result.LeaveCopy)
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

func unlinkDryRunVerbAndNote(action string, leaveCopy bool) (string, string) {
	switch action {
	case dotty.UnlinkResultActionCopySource:
		return "would copy Package Source", "leave-copy"
	case dotty.UnlinkResultActionRemoveLink:
		return "would remove link", "link removed"
	case dotty.UnlinkResultActionNoop:
		return "already absent", "no-op"
	default:
		if leaveCopy {
			return "would leave copy", "copy left"
		}
		return "would remove link", "link removed"
	}
}

func renderTrackResults(out io.Writer, results []dotty.TrackResult) {
	for _, result := range results {
		verb := "tracked"
		if result.DryRun {
			verb = "would track"
		}
		fmt.Fprintf(
			out,
			"%s %s: %s -> %s\n",
			successStyle.Render(verb),
			packageStyle.Render(result.Package),
			pathStyle.Render(result.Target),
			sourceStyle.Render(result.Source),
		)
	}
}

func renderUntrackResults(out io.Writer, results []dotty.UntrackResult) {
	for _, result := range results {
		verb := "untracked"
		if result.DryRun {
			verb = "would untrack"
		}
		note := ""
		if result.LinkExists {
			note = " (link still exists)"
		}
		fmt.Fprintf(
			out,
			"%s %s: %s -> %s%s\n",
			successStyle.Render(verb),
			packageStyle.Render(result.Package),
			pathStyle.Render(result.Target),
			sourceStyle.Render(result.Source),
			mutedStyle.Render(note),
		)
	}
}

func renderStatus(out io.Writer, report *dotty.StatusReport, verbose bool) {
	renderStatusHeader(out, report)
	if verbose {
		renderVerboseStatus(out, report)
		renderStatusSummary(out, report)
		return
	}
	for _, pkg := range report.Packages {
		renderPadded(out, pkg.Name, packageStyle, statusPackageColumnWidth)
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
	renderStatusSummary(out, report)
}

func renderVerboseStatus(out io.Writer, report *dotty.StatusReport) {
	for _, pkg := range report.Packages {
		if len(pkg.Entries) == 0 {
			renderVerboseStatusRow(out, pkg.Name, "-", "-", pkg.State, "")
			continue
		}
		for _, entry := range pkg.Entries {
			renderVerboseStatusRow(
				out,
				entry.Package,
				entry.Source,
				entry.Target,
				entry.State,
				entry.BlockedBy,
			)
		}
	}
	if len(report.Untracked) > 0 {
		if len(report.Packages) > 0 {
			fmt.Fprintln(out)
		}
		for _, item := range report.Untracked {
			renderVerboseUntrackedRow(out, item)
		}
	}
}

func renderVerboseStatusRow(
	out io.Writer,
	packageName string,
	source string,
	target string,
	state dotty.State,
	blockedBy string,
) {
	renderPadded(out, packageName, packageStyle, verbosePackageColumnWidth)
	fmt.Fprint(out, " ")
	renderPadded(out, source, sourceStyle, verboseSourceColumnWidth)
	fmt.Fprint(out, " ")
	renderPadded(out, target, pathStyle, verboseTargetColumnWidth)
	fmt.Fprintf(out, " %s\n", renderStateWithBlockedBy(state, blockedBy))
}

func renderVerboseUntrackedRow(out io.Writer, item dotty.UntrackedItem) {
	packageName := item.Package
	packageRenderStyle := packageStyle
	if packageName == "" {
		packageName = "-"
		packageRenderStyle = mutedStyle
	}
	source := item.Source
	if source == "" {
		source = item.Path
	}
	renderPadded(out, packageName, packageRenderStyle, verbosePackageColumnWidth)
	fmt.Fprint(out, " ")
	renderPadded(out, source, sourceStyle, verboseSourceColumnWidth)
	fmt.Fprint(out, " ")
	renderPadded(out, "-", mutedStyle, verboseTargetColumnWidth)
	fmt.Fprintf(out, " %s\n", renderState(item.State))
}

func renderStatusHeader(out io.Writer, report *dotty.StatusReport) {
	if report.RepoPath == "" {
		return
	}
	fmt.Fprintf(out, "Repository: %s\n\n", pathStyle.Render(report.RepoPath))
}

func renderStatusSummary(out io.Writer, report *dotty.StatusReport) {
	fmt.Fprintln(out)
	packageCount := len(report.Packages)
	parts := summarizePackageStates(report.Packages)
	summary := fmt.Sprintf(
		"Summary: %d %s",
		packageCount,
		pluralize(packageCount, "package", "packages"),
	)
	if len(parts) > 0 {
		summary += ": " + strings.Join(parts, ", ")
	}
	if len(report.Untracked) > 0 {
		summary += fmt.Sprintf(
			"; %d %s",
			len(report.Untracked),
			pluralize(len(report.Untracked), "untracked", "untracked"),
		)
	}
	fmt.Fprintln(out, mutedStyle.Render(summary))
}

func summarizePackageStates(packages []dotty.PackageStatus) []string {
	counts := map[dotty.State]int{}
	for _, pkg := range packages {
		counts[pkg.State]++
	}
	ordered := []struct {
		state    dotty.State
		singular string
		plural   string
	}{
		{dotty.StateLinked, "linked", "linked"},
		{dotty.StateUnlinked, "unlinked", "unlinked"},
		{dotty.StateConflict, "conflict", "conflicts"},
		{dotty.StateMissingSource, "missing source", "missing sources"},
		{dotty.StatePartial, "partial", "partial"},
		{dotty.StateEmpty, "empty", "empty"},
	}
	parts := []string{}
	for _, item := range ordered {
		count := counts[item.state]
		if count > 0 {
			parts = append(
				parts,
				fmt.Sprintf("%d %s", count, pluralize(count, item.singular, item.plural)),
			)
		}
	}
	return parts
}

func pluralize(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

func renderPadded(out io.Writer, text string, style lipgloss.Style, width int) {
	fmt.Fprint(out, style.Render(text))
	if padding := width - lipgloss.Width(text); padding > 0 {
		fmt.Fprint(out, strings.Repeat(" ", padding))
	}
}

func renderInventory(out io.Writer, inventory *dotty.Inventory) {
	if inventory.Detail != nil {
		renderInventoryPackageDetail(out, inventory.Detail)
		return
	}
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

func renderInventoryPackageDetail(out io.Writer, pkg *dotty.InventoryPackage) {
	fmt.Fprintf(out, "%s %s\n", packageStyle.Render("Package"), packageStyle.Render(pkg.Name))
	if len(pkg.Links) == 0 {
		fmt.Fprintf(out, "  %s\n", mutedStyle.Render("no links"))
		return
	}
	for _, link := range pkg.Links {
		fmt.Fprintf(
			out,
			"  %s -> %s\n",
			sourceStyle.Render(link.Source),
			pathStyle.Render(link.Target),
		)
	}
}

func renderState(state dotty.State) string {
	return renderStateWithBlockedBy(state, "")
}

func renderStateWithBlockedBy(state dotty.State, blockedBy string) string {
	label := string(state)
	if state == dotty.StateBlocked && blockedBy != "" {
		label += " by " + blockedBy
	}
	if style, ok := stateStyles[state]; ok {
		return style.Render(label)
	}
	return label
}
