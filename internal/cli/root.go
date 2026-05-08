package cli

import (
	"fmt"
	"io"
	"strings"

	"dotty/internal/dotty"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

type app struct {
	out      io.Writer
	err      io.Writer
	repoFlag string
}

func NewRootCommand(out, errOut io.Writer) *cobra.Command {
	app := &app{out: out, err: errOut}
	cmd := &cobra.Command{
		Use:           "dotty",
		Short:         "Manage dotfiles with explicit TOML link mappings",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.PersistentFlags().StringVar(&app.repoFlag, "repo", "", "dotfiles repository path (overrides DOTTY_REPO and config)")
	cmd.AddCommand(app.initCommand())
	cmd.AddCommand(app.addCommand())
	cmd.AddCommand(app.linkCommand())
	cmd.AddCommand(app.unlinkCommand())
	cmd.AddCommand(app.statusCommand())
	cmd.AddCommand(app.listCommand())
	return cmd
}

func (a *app) service() (dotty.Service, error) {
	repo, err := dotty.ResolveRepo(a.repoFlag)
	if err != nil {
		return dotty.Service{}, err
	}
	return dotty.NewService(repo), nil
}

func (a *app) initCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "init [path]",
		Short: "Initialize a dotty repository and remember it as the default",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "."
			if len(args) == 1 {
				path = args[0]
			}
			repo, err := dotty.Init(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "%s %s\n", successStyle.Render("initialized"), pathStyle.Render(repo))
			return nil
		},
	}
}

func (a *app) addCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "add PATH PACKAGE",
		Short: "Adopt an existing file, directory, or symlink target into a package",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := a.service()
			if err != nil {
				return err
			}
			result, err := svc.Add(args[0], args[1])
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "%s %s %s %s\n", successStyle.Render("added"), packageStyle.Render(result.Package), sourceStyle.Render(result.Source), pathStyle.Render(result.Target))
			return nil
		},
	}
}

func (a *app) linkCommand() *cobra.Command {
	var collections []string
	var force bool
	cmd := &cobra.Command{
		Use:   "link [packages...]",
		Short: "Create links for packages or an explicit collection",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := a.service()
			if err != nil {
				return err
			}
			linked, err := svc.Link(dotty.LinkOptions{Packages: args, Collections: collections, Force: force})
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "%s %s\n", successStyle.Render("linked"), strings.Join(linked, ", "))
			return nil
		},
	}
	cmd.Flags().StringArrayVarP(&collections, "collection", "c", nil, "collection to link (can be repeated)")
	cmd.Flags().BoolVar(&force, "force", false, "destructively replace target conflicts")
	return cmd
}

func (a *app) unlinkCommand() *cobra.Command {
	var collections []string
	var hard bool
	cmd := &cobra.Command{
		Use:   "unlink [packages...]",
		Short: "Remove links for packages or an explicit collection",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := a.service()
			if err != nil {
				return err
			}
			unlinked, err := svc.Unlink(dotty.UnlinkOptions{Packages: args, Collections: collections, Hard: hard})
			if err != nil {
				return err
			}
			verb := "unlinked"
			if hard {
				verb = "hard-unlinked"
			}
			fmt.Fprintf(a.out, "%s %s\n", successStyle.Render(verb), strings.Join(unlinked, ", "))
			return nil
		},
	}
	cmd.Flags().StringArrayVarP(&collections, "collection", "c", nil, "collection to unlink (can be repeated)")
	cmd.Flags().BoolVar(&hard, "hard", false, "remove expected links without leaving target-side copies")
	return cmd
}

func (a *app) statusCommand() *cobra.Command {
	var verbose bool
	cmd := &cobra.Command{
		Use:   "status [packages...]",
		Short: "Show linked, unlinked, conflict, missing-source, empty, partial, and untracked states",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := a.service()
			if err != nil {
				return err
			}
			report, err := svc.Status(args)
			if err != nil {
				return err
			}
			renderStatus(a.out, report, verbose)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "show per-link mapping status")
	return cmd
}

func (a *app) listCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List packages and collections defined in the manifest",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := a.service()
			if err != nil {
				return err
			}
			inventory, err := svc.List()
			if err != nil {
				return err
			}
			renderInventory(a.out, inventory)
			return nil
		},
	}
}

var (
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
	packageStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	sourceStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
	pathStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	mutedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)
