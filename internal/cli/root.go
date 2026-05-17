package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lmilojevicc/dotty/internal/dotty"
)

type app struct {
	out      io.Writer
	err      io.Writer
	repoFlag string
	env      dotty.Env
}

func NewRootCommand(out, errOut io.Writer) *cobra.Command {
	app := &app{out: out, err: errOut}
	cmd := &cobra.Command{
		Use:           "dotty",
		Short:         "Sync configuration files across machines using a manifest",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			env, err := dotty.EnvFromOS()
			if err != nil {
				return err
			}
			app.env = env
			return nil
		},
	}
	configureHelp(cmd)
	cmd.PersistentFlags().
		StringVar(&app.repoFlag, "repo", "", "dotfiles repository path (overrides DOTTY_REPO and config)")
	mustRegisterFlagCompletion(cmd, "repo", completeDirectories)
	cmd.AddCommand(app.versionCommand())
	cmd.AddCommand(app.initCommand())
	cmd.AddCommand(app.addCommand())
	cmd.AddCommand(app.mapCommand())
	cmd.AddCommand(app.unmapCommand())
	cmd.AddCommand(app.linkCommand())
	cmd.AddCommand(app.unlinkCommand())
	cmd.AddCommand(app.statusCommand())
	cmd.AddCommand(app.listCommand())
	cmd.AddCommand(app.repoCommand())
	cmd.AddCommand(app.completionCommand())
	return cmd
}

func (a *app) service() (dotty.Service, error) {
	repo, err := dotty.ResolveRepo(a.repoFlag, a.env)
	if err != nil {
		return dotty.Service{}, err
	}
	return dotty.NewService(repo, a.env), nil
}

func (a *app) versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:               "version",
		Short:             "Print the version number",
		Args:              noArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintf(a.out, "%s version %s\n", cmd.Root().Name(), Version)
		},
	}
}

func (a *app) initCommand() *cobra.Command {
	return &cobra.Command{
		Use:               "init [<path>]",
		Short:             "Initialize a dotty repository and remember it as the default",
		Args:              maximumArgs(1),
		ValidArgsFunction: completeInitArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "."
			if len(args) == 1 {
				path = args[0]
			}
			svc, err := dotty.InitRepo(path, a.env)
			if err != nil {
				return err
			}
			fmt.Fprintf(
				a.out,
				"%s %s\n",
				successStyle.Render("initialized"),
				pathStyle.Render(svc.Repo),
			)
			return nil
		},
	}
}

func (a *app) addCommand() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:               "add <path> <package>",
		Short:             "Adopt an existing file, directory, or symlink target into a package",
		Args:              exactArgs(2),
		ValidArgsFunction: a.completeAddArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := a.service()
			if err != nil {
				return err
			}
			result, err := svc.AddWithOptions(
				dotty.AddOptions{Target: args[0], Package: args[1], DryRun: dryRun},
			)
			if err != nil {
				return err
			}
			renderAddResult(a.out, result)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would change without writing files")
	return cmd
}

func (a *app) mapCommand() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:               "map <package> <source> <target>",
		Short:             "Add a Manifest Link Mapping without changing files",
		Args:              exactArgs(3),
		ValidArgsFunction: a.completeMapArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := a.service()
			if err != nil {
				return err
			}
			result, err := svc.Map(
				dotty.MapOptions{
					Package: args[0],
					Source:  args[1],
					Target:  args[2],
					DryRun:  dryRun,
				},
			)
			if err != nil {
				return err
			}
			renderMapResult(a.out, result)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would change without writing files")
	return cmd
}

func (a *app) unmapCommand() *cobra.Command {
	var targets []string
	var dryRun bool
	cmd := &cobra.Command{
		Use:               "unmap <package> --target <target>",
		Short:             "Remove Manifest Link Mappings without changing files",
		Args:              unmapArgs(&targets),
		ValidArgsFunction: a.completePackages,
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := a.service()
			if err != nil {
				return err
			}
			results, err := svc.Unmap(
				dotty.UnmapOptions{Package: args[0], Targets: targets, DryRun: dryRun},
			)
			if err != nil {
				return err
			}
			renderUnmapResults(a.out, results)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would change without writing files")
	cmd.Flags().StringArrayVar(&targets, "target", nil, "Target Path to unmap (can be repeated)")
	mustRegisterFlagCompletion(cmd, "target", a.completeTargets)
	return cmd
}

func (a *app) linkCommand() *cobra.Command {
	var collections []string
	var targets []string
	var all bool
	var force bool
	var dryRun bool
	cmd := &cobra.Command{
		Use:               "link <package>... | --all | --collection <collection>",
		Short:             "Create links for packages, all packages, or an explicit collection",
		Args:              selectionArgs(&collections, &all),
		ValidArgsFunction: a.completePackages,
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := a.service()
			if err != nil {
				return err
			}
			linked, err := svc.Link(
				dotty.LinkOptions{
					Packages:    args,
					Collections: collections,
					Targets:     targets,
					All:         all,
					Force:       force,
					DryRun:      dryRun,
				},
			)
			if err != nil {
				return err
			}
			renderLinkResults(a.out, linked)
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "link all packages")
	cmd.Flags().
		StringArrayVarP(&collections, "collection", "c", nil, "collection to link (can be repeated)")
	mustRegisterFlagCompletion(cmd, "collection", a.completeCollections)
	cmd.Flags().BoolVar(&force, "force", false, "destructively replace target conflicts")
	cmd.Flags().StringArrayVar(&targets, "target", nil, "Target Path to link (can be repeated)")
	mustRegisterFlagCompletion(cmd, "target", a.completeTargets)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would change without writing files")
	return cmd
}

func (a *app) unlinkCommand() *cobra.Command {
	var collections []string
	var targets []string
	var all bool
	var hard bool
	var dryRun bool
	cmd := &cobra.Command{
		Use:               "unlink <package>... | --all | --collection <collection>",
		Short:             "Remove links for packages, all packages, or an explicit collection",
		Args:              selectionArgs(&collections, &all),
		ValidArgsFunction: a.completePackages,
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := a.service()
			if err != nil {
				return err
			}
			unlinked, err := svc.Unlink(
				dotty.UnlinkOptions{
					Packages:    args,
					Collections: collections,
					Targets:     targets,
					All:         all,
					Hard:        hard,
					DryRun:      dryRun,
				},
			)
			if err != nil {
				return err
			}
			renderUnlinkResults(a.out, unlinked)
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "unlink all packages")
	cmd.Flags().
		StringArrayVarP(&collections, "collection", "c", nil, "collection to unlink (can be repeated)")
	mustRegisterFlagCompletion(cmd, "collection", a.completeCollections)
	cmd.Flags().
		BoolVar(&hard, "hard", false, "remove expected links without leaving target-side copies")
	cmd.Flags().StringArrayVar(&targets, "target", nil, "Target Path to unlink (can be repeated)")
	mustRegisterFlagCompletion(cmd, "target", a.completeTargets)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would change without writing files")
	return cmd
}

func (a *app) statusCommand() *cobra.Command {
	var verbose bool
	var stateFilters []string
	cmd := &cobra.Command{
		Use:               "status [<package>...]",
		Short:             "Show linked, unlinked, conflict, missing-source, empty, partial, and untracked states",
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: a.completePackages,
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := a.service()
			if err != nil {
				return err
			}
			parsedStates := make([]dotty.State, 0, len(stateFilters))
			for _, stateValue := range stateFilters {
				state, err := dotty.ParseStatusFilterValue(stateValue)
				if err != nil {
					return err
				}
				parsedStates = append(parsedStates, state)
			}
			report, err := svc.Status(args)
			if err != nil {
				return err
			}
			report = dotty.FilterStatusReport(report, parsedStates)
			effectiveVerbose := verbose || len(args) == 1
			if len(args) > 0 {
				report.Untracked = nil
			}
			renderStatus(a.out, report, effectiveVerbose)
			return nil
		},
	}
	cmd.Flags().
		BoolVarP(&verbose, "verbose", "v", false, "show detailed status output per Link Mapping")
	cmd.Flags().
		StringArrayVar(&stateFilters, "state", nil, "filter by status state (can be repeated)")
	mustRegisterFlagCompletion(cmd, "state", a.completeStatusStates)
	return cmd
}

func (a *app) listCommand() *cobra.Command {
	return &cobra.Command{
		Use:               "list",
		Short:             "List packages and collections defined in the manifest",
		Args:              noArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
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

func (a *app) repoCommand() *cobra.Command {
	return &cobra.Command{
		Use:               "repo",
		Short:             "Show the resolved dotfiles repository and config path",
		Args:              noArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, err := dotty.ResolveRepo(a.repoFlag, a.env)
			if err != nil {
				return err
			}
			fmt.Fprintf(
				a.out,
				"Repository: %s\n",
				pathStyle.Render(dotty.HomeRelative(repo, a.env)),
			)
			fmt.Fprintf(
				a.out,
				"Config: %s\n",
				pathStyle.Render(dotty.HomeRelative(a.env.ConfigFilePath(), a.env)),
			)
			return nil
		},
	}
}

func exactArgs(count int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != count {
			return usageError(cmd)
		}
		return nil
	}
}

func maximumArgs(count int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) > count {
			return usageError(cmd)
		}
		return nil
	}
}

func noArgs(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return usageError(cmd)
	}
	return nil
}

func selectionArgs(collections *[]string, all *bool) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 && len(*collections) == 0 && !*all {
			return usageError(cmd)
		}
		return nil
	}
}

func unmapArgs(targets *[]string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 || len(*targets) == 0 {
			return usageError(cmd)
		}
		return nil
	}
}

func usageError(cmd *cobra.Command) error {
	return fmt.Errorf("usage: %s", sampleUsage(cmd))
}

func sampleUsage(cmd *cobra.Command) string {
	useParts := strings.Fields(cmd.Use)
	if len(useParts) <= 1 {
		return cmd.CommandPath()
	}
	return cmd.CommandPath() + " " + strings.Join(useParts[1:], " ")
}
