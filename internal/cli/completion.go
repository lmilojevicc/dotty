package cli

import (
	"fmt"
	"io/fs"
	"os"
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

func completeDirectories(
	cmd *cobra.Command,
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveFilterDirs
}

func completeFilesystemPaths(
	cmd *cobra.Command,
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveDefault
}

func (a *app) completeLinkArgs(
	cmd *cobra.Command,
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	track, _ := cmd.Flags().GetBool("track")
	if track {
		return a.completeRepoSelectors(args, toComplete)
	}
	return a.completeManifestAndRepoSelectors(cmd, args, toComplete)
}

func (a *app) completeUnlinkArgs(
	cmd *cobra.Command,
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	untrack, _ := cmd.Flags().GetBool("untrack")
	if untrack {
		return a.completeManifestSelectors(args, toComplete)
	}
	return a.completeManifestAndRepoSelectors(cmd, args, toComplete)
}

func (a *app) completeTrackArgs(
	cmd *cobra.Command,
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return a.completeRepoSelectors(args, toComplete)
	}
	return nil, cobra.ShellCompDirectiveDefault
}

func (a *app) completeUntrackArgs(
	cmd *cobra.Command,
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return a.completeManifestSelectors(args, toComplete)
	}
	return a.completeTargets(cmd, args, toComplete)
}

func (a *app) completeListArgs(
	cmd *cobra.Command,
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return a.completePackages(cmd, args, toComplete)
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

func (a *app) completeManifestAndRepoSelectors(
	cmd *cobra.Command,
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	if all, err := cmd.Flags().GetBool("all"); err == nil && all {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	manifest, repo, _, err := a.completionManifest()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	allowedPackages := map[string]bool{}
	for packageName := range manifest.Packages {
		allowedPackages[packageName] = true
	}
	choices := mergeCompletionValues(
		manifestSelectorValues(manifest),
		repoSelectorValues(repo, allowedPackages),
	)
	return completeStrings(
		choices,
		selectedCompletions(args),
		toComplete,
	), cobra.ShellCompDirectiveNoFileComp
}

func (a *app) completeManifestSelectors(
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	manifest, _, _, err := a.completionManifest()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completeStrings(
		manifestSelectorValues(manifest),
		selectedCompletions(args),
		toComplete,
	), cobra.ShellCompDirectiveNoFileComp
}

func (a *app) completeRepoSelectors(
	args []string,
	toComplete string,
) ([]string, cobra.ShellCompDirective) {
	_, repo, _, err := a.completionManifest()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completeStrings(
		repoSelectorValues(repo, nil),
		selectedCompletions(args),
		toComplete,
	), cobra.ShellCompDirectiveNoFileComp
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
	if useFilesystem, valid := filesystemTargetCompletion(cmd, args); useFilesystem {
		return nil, cobra.ShellCompDirectiveDefault
	} else if !valid {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	manifest, _, env, err := a.completionManifest()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	selectedValues, _ := cmd.Flags().GetStringArray("target")
	selectedValues = append(selectedValues, positionalTargetCompletions(cmd, args)...)
	selected := selectedCompletions(selectedValues)
	targets, ok := a.completionTargetsForScope(cmd, manifest, args, env)
	if !ok {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completeStrings(targets, selected, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func filesystemTargetCompletion(
	cmd *cobra.Command,
	args []string,
) (useFilesystem bool, valid bool) {
	if cmd.Name() != "link" {
		return false, true
	}
	track, _ := cmd.Flags().GetBool("track")
	if !track {
		return false, true
	}
	all, _ := cmd.Flags().GetBool("all")
	collections, _ := cmd.Flags().GetStringArray("collection")
	if all || len(collections) > 0 || len(args) != 1 {
		return false, false
	}
	return true, true
}

func positionalTargetCompletions(cmd *cobra.Command, args []string) []string {
	if cmd.Name() != "untrack" || len(args) < 2 {
		return nil
	}
	return args[1:]
}

func (a *app) completionTargetsForScope(
	cmd *cobra.Command,
	manifest *dotty.Manifest,
	args []string,
	env dotty.Env,
) ([]string, bool) {
	packages := args
	collections, _ := cmd.Flags().GetStringArray("collection")
	all, _ := cmd.Flags().GetBool("all")
	if all && (len(packages) > 0 || len(collections) > 0) {
		return nil, false
	}
	if cmd.Name() == "untrack" {
		return completionTargetsForSelector(manifest, args, env)
	}
	if untrack, _ := cmd.Flags().GetBool("untrack"); cmd.Name() == "unlink" && untrack {
		if all || len(collections) > 0 || len(args) != 1 {
			return nil, false
		}
		return completionTargetsForSelector(manifest, args, env)
	}

	if len(packages) > 0 || len(collections) > 0 || all {
		selected, err := resolveCompletionTargetScope(manifest, packages, collections, all, env)
		if err == nil {
			targets := make([]string, 0, len(selected))
			for _, item := range selected {
				targets = append(targets, item.Link.Target)
			}
			return targets, true
		}
		return nil, false
	}

	targets := []string{}
	for _, packageName := range sortedKeys(manifest.Packages) {
		pkg := manifest.Packages[packageName]
		for _, link := range pkg.Links {
			targets = append(targets, link.Target)
		}
	}
	return targets, true
}

func completionTargetsForSelector(
	manifest *dotty.Manifest,
	args []string,
	env dotty.Env,
) ([]string, bool) {
	if len(args) == 0 {
		return nil, false
	}
	selector, err := dotty.ParseSelector(args[0])
	if err != nil {
		return nil, false
	}
	selected, err := dotty.ResolveSelectors(manifest, dotty.ResolveOptions{
		Selectors: []dotty.Selector{selector},
	}, env)
	if err != nil {
		return nil, false
	}
	targets := make([]string, 0, len(selected))
	for _, item := range selected {
		targets = append(targets, item.Link.Target)
	}
	return targets, true
}

func resolveCompletionTargetScope(
	manifest *dotty.Manifest,
	args []string,
	collections []string,
	all bool,
	env dotty.Env,
) ([]dotty.SelectedLinkMapping, error) {
	if containsSourceSelector(args) {
		selectors := make([]dotty.Selector, 0, len(args))
		for _, arg := range args {
			selector, err := dotty.ParseSelector(arg)
			if err != nil {
				return nil, err
			}
			selectors = append(selectors, selector)
		}
		return dotty.ResolveSelectors(manifest, dotty.ResolveOptions{
			Selectors:   selectors,
			Collections: collections,
			All:         all,
		}, env)
	}
	return dotty.ResolveSelectedLinkMappings(manifest, args, collections, all, nil, env)
}

func containsSourceSelector(args []string) bool {
	for _, arg := range args {
		if strings.Contains(arg, "/") {
			return true
		}
	}
	return false
}

func manifestSelectorValues(manifest *dotty.Manifest) []string {
	values := []string{}
	seen := map[string]bool{}
	for _, packageName := range sortedKeys(manifest.Packages) {
		appendCompletionValue(&values, seen, packageName)
		pkg := manifest.Packages[packageName]
		for _, link := range pkg.Links {
			if link.Source == "." {
				continue
			}
			appendCompletionValue(&values, seen, packageName+"/"+link.Source)
		}
	}
	slices.Sort(values)
	return values
}

func repoSelectorValues(repo string, allowedPackages map[string]bool) []string {
	entries, err := filepath.Glob(filepath.Join(repo, "*"))
	if err != nil {
		return nil
	}
	values := []string{}
	seen := map[string]bool{}
	for _, packageRoot := range entries {
		stat, err := os.Stat(packageRoot)
		if err != nil || !stat.IsDir() {
			continue
		}
		packageName := filepath.Base(packageRoot)
		if _, err := dotty.ParseSelector(packageName); err != nil {
			continue
		}
		if allowedPackages != nil && !allowedPackages[packageName] {
			continue
		}
		appendCompletionValue(&values, seen, packageName)
		walkPackageSources(packageRoot, packageName, &values, seen)
	}
	slices.Sort(values)
	return values
}

func walkPackageSources(
	packageRoot string,
	packageName string,
	values *[]string,
	seen map[string]bool,
) {
	_ = filepath.WalkDir(packageRoot, func(path string, entry fs.DirEntry, err error) error {
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
		appendCompletionValue(values, seen, packageName+"/"+filepath.ToSlash(rel))
		return nil
	})
}

func mergeCompletionValues(groups ...[]string) []string {
	values := []string{}
	seen := map[string]bool{}
	for _, group := range groups {
		for _, value := range group {
			appendCompletionValue(&values, seen, value)
		}
	}
	slices.Sort(values)
	return values
}

func appendCompletionValue(values *[]string, seen map[string]bool, value string) {
	if value == "" || seen[value] {
		return
	}
	seen[value] = true
	*values = append(*values, value)
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
