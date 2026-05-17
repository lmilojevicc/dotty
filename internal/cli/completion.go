package cli

import (
	"fmt"
	"io/fs"
	"path/filepath"
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

func (a *app) completeAddArgs(
	cmd *cobra.Command,
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return nil, cobra.ShellCompDirectiveDefault
	}
	if len(args) == 1 {
		return a.completePackages(cmd, nil, toComplete)
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
	if len(args) == 1 {
		return a.completePackageSources(args[0], toComplete)
	}
	if len(args) == 2 {
		return nil, cobra.ShellCompDirectiveDefault
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

func (a *app) completeTargets(
	cmd *cobra.Command,
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	manifest, _, env, err := a.completionManifest()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	selectedValues, _ := cmd.Flags().GetStringArray("target")
	selected := selectedCompletions(selectedValues)
	targets := a.completionTargetsForScope(cmd, manifest, args, env)
	return completeStrings(targets, selected, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func (a *app) completionTargetsForScope(
	cmd *cobra.Command,
	manifest *dotty.Manifest,
	args []string,
	env dotty.Env,
) []string {
	packages := args
	collections, _ := cmd.Flags().GetStringArray("collection")
	all, _ := cmd.Flags().GetBool("all")
	if cmd.Name() == "unmap" {
		collections = nil
		all = false
		if len(args) > 1 {
			packages = args[:1]
		}
	}

	if len(packages) > 0 || len(collections) > 0 || all {
		selected, err := dotty.ResolveSelectedLinkMappings(
			manifest,
			packages,
			collections,
			all,
			nil,
			env,
		)
		if err == nil {
			targets := make([]string, 0, len(selected))
			for _, item := range selected {
				targets = append(targets, item.Link.Target)
			}
			return targets
		}
	}

	targets := []string{}
	for _, packageName := range sortedKeys(manifest.Packages) {
		pkg := manifest.Packages[packageName]
		for _, link := range pkg.Links {
			targets = append(targets, link.Target)
		}
	}
	return targets
}

func (a *app) completePackageSources(
	packageName string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	manifest, repo, _, err := a.completionManifest()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	if _, ok := manifest.Packages[packageName]; !ok {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	packageRoot := dotty.PackageRoot(repo, packageName)
	choices := []string{}
	err = filepath.WalkDir(packageRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == packageRoot {
			return nil
		}
		rel, err := filepath.Rel(packageRoot, path)
		if err != nil {
			return err
		}
		choices = append(choices, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completeStrings(choices, nil, toComplete), cobra.ShellCompDirectiveNoFileComp
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

func (a *app) completionManifest() (*dotty.Manifest, string, dotty.Env, error) {
	env, err := a.completionEnv()
	if err != nil {
		return nil, "", dotty.Env{}, err
	}
	repo, err := dotty.ResolveRepo(a.repoFlag, env)
	if err != nil {
		return nil, "", dotty.Env{}, err
	}
	manifest, err := dotty.LoadManifest(repo, env)
	if err != nil {
		return nil, "", dotty.Env{}, err
	}
	return manifest, repo, env, nil
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

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func mustRegisterFlagCompletion(cmd *cobra.Command, flagName string, fn cobra.CompletionFunc) {
	if err := cmd.RegisterFlagCompletionFunc(flagName, fn); err != nil {
		panic(err)
	}
}
