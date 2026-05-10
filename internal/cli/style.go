package cli

import (
	"strings"
	"sync"
	"text/template"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var (
	headingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	hintStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	packageStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	sourceStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
	pathStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	mutedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

var configureHelpOnce sync.Once

func configureHelp(cmd *cobra.Command) {
	configureHelpOnce.Do(func() {
		cobra.AddTemplateFuncs(template.FuncMap{
			"dottyHeading": func(text string) string {
				return headingStyle.Render(text)
			},
			"dottyUseLine": func(cmd *cobra.Command) string {
				if !cmd.HasParent() && cmd.HasAvailableSubCommands() {
					return cmd.CommandPath() + " [command]"
				}
				return cmd.UseLine()
			},
			"dottyOptions": func(cmd *cobra.Command) string {
				return flagUsages(cmd.LocalNonPersistentFlags())
			},
			"dottyGlobalOptions": func(cmd *cobra.Command) string {
				if !cmd.HasParent() {
					return flagUsages(cmd.PersistentFlags())
				}
				return flagUsages(cmd.InheritedFlags())
			},
		})
	})
	cmd.SetHelpTemplate(helpTemplate)
}

func flagUsages(flags *pflag.FlagSet) string {
	if flags == nil || !flags.HasAvailableFlags() {
		return ""
	}
	return strings.TrimRight(flags.FlagUsages(), "\n")
}

const helpTemplate = `{{with (or .Long .Short)}}{{.}}{{end}}

{{dottyHeading "Usage"}}:
  {{dottyUseLine .}}{{if .HasAvailableSubCommands}}

{{dottyHeading "Commands"}}:
{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}  {{rpad .Name .NamePadding}} {{.Short}}
{{end}}{{end}}{{else}}
{{end}}{{$options := dottyOptions .}}{{if $options}}
{{dottyHeading "Options"}}:
{{$options}}
{{end}}{{$globalOptions := dottyGlobalOptions .}}{{if $globalOptions}}
{{dottyHeading "Global options"}}:
{{$globalOptions}}
{{end}}{{if .HasAvailableSubCommands}}
Use ` + "`" + `{{.CommandPath}} [command] --help` + "`" + ` for more information about a command.
{{end}}`
