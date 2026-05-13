package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lmilojevicc/dotty/internal/dotty"
)

var supportedCompletionShells = []string{"bash", "zsh", "fish", "powershell"}

func (a *app) completionCommand() *cobra.Command {
	return &cobra.Command{
		Use:               "completion <bash|zsh|fish|powershell>",
		Short:             "Generate shell completion scripts",
		Args:              shellCompletionArgs,
		ValidArgsFunction: completeShells,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletionV2(a.out, true)
			case "zsh":
				return cmd.Root().GenZshCompletion(a.out)
			case "fish":
				return cmd.Root().GenFishCompletion(a.out, true)
			case "powershell":
				return cmd.Root().GenPowerShellCompletionWithDesc(a.out)
			default:
				return unsupportedCompletionShellError(args[0])
			}
		},
	}
}

func shellCompletionArgs(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return usageError(cmd)
	}
	if slices.Contains(supportedCompletionShells, args[0]) {
		return nil
	}
	return unsupportedCompletionShellError(args[0])
}

func unsupportedCompletionShellError(shell string) error {
	return fmt.Errorf(
		"unsupported shell %q (use bash, zsh, fish, or powershell)",
		shell,
	)
}

func completeShells(
	cmd *cobra.Command,
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completeStrings(
		supportedCompletionShells,
		nil,
		toComplete,
	), cobra.ShellCompDirectiveNoFileComp
}

func completeInitArgs(
	cmd *cobra.Command,
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return nil, cobra.ShellCompDirectiveFilterDirs
}

func completeAddArgs(
	cmd *cobra.Command,
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return nil, cobra.ShellCompDirectiveDefault
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}

func (a *app) completeMapArgs(
	cmd *cobra.Command,
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return a.completePackages(cmd, args, toComplete)
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}

func completeDirectories(
	cmd *cobra.Command,
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveFilterDirs
}

func (a *app) completePackages(
	cmd *cobra.Command,
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	if all, err := cmd.Flags().GetBool("all"); err == nil && all {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	inventory, err := a.completionInventory()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	selected := selectedCompletions(args)
	packages := make([]string, 0, len(inventory.Packages))
	for _, pkg := range inventory.Packages {
		packages = append(packages, pkg.Name)
	}
	return completeStrings(packages, selected, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func (a *app) completeCollections(
	cmd *cobra.Command,
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	inventory, err := a.completionInventory()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	selectedValues, _ := cmd.Flags().GetStringArray("collection")
	selected := selectedCompletions(selectedValues)

	collections := make([]string, 0, len(inventory.Collections))
	for _, collection := range inventory.Collections {
		collections = append(collections, collection.Name)
	}
	return completeStrings(collections, selected, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func (a *app) completeStatusStates(
	cmd *cobra.Command,
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	selectedValues, _ := cmd.Flags().GetStringArray("state")
	selected := selectedCompletions(selectedValues)
	return completeStrings(
		dotty.SupportedStatusFilterValues(),
		selected,
		toComplete,
	), cobra.ShellCompDirectiveNoFileComp
}

func (a *app) completionInventory() (*dotty.Inventory, error) {
	env, err := a.completionEnv()
	if err != nil {
		return nil, err
	}
	repo, err := dotty.ResolveRepo(a.repoFlag, env)
	if err != nil {
		return nil, err
	}
	return dotty.NewService(repo, env).List()
}

func (a *app) completionEnv() (dotty.Env, error) {
	if a.env.Home != "" {
		return a.env, nil
	}
	return dotty.EnvFromOS()
}

func completeStrings(values []string, selected map[string]bool, toComplete string) []string {
	var completions []string
	for _, value := range values {
		if selected[value] || !strings.HasPrefix(value, toComplete) {
			continue
		}
		completions = append(completions, value)
	}
	return completions
}

func selectedCompletions(values []string) map[string]bool {
	selected := make(map[string]bool, len(values))
	for _, value := range values {
		selected[value] = true
	}
	return selected
}

func mustRegisterFlagCompletion(cmd *cobra.Command, flagName string, fn cobra.CompletionFunc) {
	if err := cmd.RegisterFlagCompletionFunc(flagName, fn); err != nil {
		panic(err)
	}
}
