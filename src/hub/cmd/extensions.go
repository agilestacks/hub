package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"hub/config"
	"hub/ext"
)

var (
	knownExtensions = []string{"pull", "ls", "show", "configure"}
)

var extensionCmd = cobra.Command{
	Use:   "",
	Short: "`%s` extension",
	RunE: func(cmd *cobra.Command, args []string) error {
		return extension(cmd.Use, args)
	},
	DisableFlagParsing: true,
}

var arbitraryExtensionCmd = &cobra.Command{
	Use:   "ext",
	Short: "Call arbitrary extension",
	Long:  "Call arbitrary extension via `hub-<extension name>` calling convention",
	RunE: func(cmd *cobra.Command, args []string) error {
		return arbitraryExtension(args)
	},
	DisableFlagParsing: true,
}

var extensionsCmd = &cobra.Command{
	Use:   "extensions",
	Short: "Manage Hub CLI extensions",
}

var extensionsInstallCmd = &cobra.Command{
	Use:   "install [dir]",
	Short: "Install Hub CLI extensions",
	Long: `Install Hub CLI extension into ~/.hub/ by cloning git@github.com:agilestacks/hub-extensions.git
and installing dependencies.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return extensionsInstall(args)
	},
}

var extensionsUpdateCmd = &cobra.Command{
	Use:   "update [dir]",
	Short: "Update Hub CLI extensions",
	Long: `Update Hub CLI extension via hub pull in ~/.hub/
and refreshing dependencies.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return extensionsUpdate(args)
	},
}

func extension(what string, args []string) error {
	config.AggWarnings = false
	ext.RunExtension(what, args)
	return nil
}

func arbitraryExtension(args []string) error {
	what := ""
	finalArgs := make([]string, 0, len(args))
	for i, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			what = arg
			if i < len(args)-1 {
				finalArgs = append(finalArgs, args[i+1:]...)
			}
			break
		} else {
			finalArgs = append(finalArgs, arg)
		}
	}
	if what == "" {
		return errors.New("Extensions command has at least one mandatory argument - the name of extension command to call")
	}

	return extension(what, finalArgs)
}

func extensionsInstall(args []string) error {
	if len(args) != 0 && len(args) != 1 {
		return errors.New("Extensions Install command has one optional argument - path to Hub CLI extensions folder")
	}
	dir := ""
	if len(args) > 0 {
		dir = args[0]
	}
	config.AggWarnings = false
	ext.Install(dir)
	return nil
}

func extensionsUpdate(args []string) error {
	if len(args) != 0 && len(args) != 1 {
		return errors.New("Extensions Update command has one optional argument - path to Hub CLI extensions folder")
	}
	dir := ""
	if len(args) > 0 {
		dir = args[0]
	}
	config.AggWarnings = false
	ext.Update(dir)
	return nil
}

func init() {
	for _, e := range knownExtensions {
		cmd := extensionCmd
		cmd.Use = e
		cmd.Short = fmt.Sprintf(cmd.Short, e)
		RootCmd.AddCommand(&cmd)
	}
	RootCmd.AddCommand(arbitraryExtensionCmd)
	extensionsCmd.AddCommand(extensionsInstallCmd)
	extensionsCmd.AddCommand(extensionsUpdateCmd)
	RootCmd.AddCommand(extensionsCmd)
}
