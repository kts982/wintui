package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var themeListFlag bool

var themeCmd = &cobra.Command{
	Use:   "theme [name]",
	Short: "Show, list, or set the WinTUI color theme",
	Long: `Print the active theme, list every available theme, or set a new one.

With no arguments, prints the current theme and background mode. With a theme
name, persists it to settings.json — the TUI and CLI help pick it up on the
next run. Use --list to see every theme.

Example:
  wintui theme            # show the active theme
  wintui theme --list     # list all themes
  wintui theme nord       # switch to the Nord palette`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: themeNameCompletion,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := ""
		if len(args) == 1 {
			name = args[0]
		}
		return runTheme(name, themeListFlag, os.Stdout)
	},
}

// runTheme drives `wintui theme`. With list=true it prints the full theme
// table; with a non-empty name it validates and persists the selection;
// otherwise it prints the active theme.
func runTheme(name string, list bool, out io.Writer) error {
	if list {
		printThemeList(out)
		return nil
	}

	if strings.TrimSpace(name) == "" {
		printActiveTheme(out)
		return nil
	}

	requested := strings.ToLower(strings.TrimSpace(name))
	if _, ok := themes[requested]; !ok {
		return fmt.Errorf("unknown theme %q; run \"wintui theme --list\" to see the available themes", name)
	}

	// setValue mirrors the settings-UI normalization (persists "" for the
	// default slot rather than a literal "default").
	appSettings.setValue("theme", requested)
	if err := SaveSettings(appSettings); err != nil {
		return fmt.Errorf("could not save settings: %w", err)
	}
	fmt.Fprintf(out, "Theme set to %s.\n", lookupTheme(requested).Label)
	return nil
}

func printActiveTheme(out io.Writer) {
	id := normalizeTheme(appSettings.Theme)
	fmt.Fprintf(out, "Theme:      %s (%s)\n", lookupTheme(id).Label, id)
	fmt.Fprintf(out, "Background: %s\n", themeBackgroundLabel())
	fmt.Fprintln(out, "\nRun \"wintui theme --list\" to see all themes, or \"wintui theme <name>\" to switch.")
}

func printThemeList(out io.Writer) {
	active := normalizeTheme(appSettings.Theme)
	fmt.Fprintf(out, "Available themes (active: %s):\n\n", lookupTheme(active).Label)
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, id := range themeOrder {
		marker := ""
		if id == active {
			marker = "(active)"
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", id, themes[id].Label, marker)
	}
	_ = tw.Flush()
}

// themeBackgroundLabel renders the OSC 11 background mode as a friendly word.
func themeBackgroundLabel() string {
	if normalizeThemeBackground(appSettings.ThemeBackground) == ThemeBackgroundTheme {
		return "theme"
	}
	return "terminal"
}

// themeNameCompletion completes theme IDs for `wintui theme <TAB>`.
func themeNameCompletion(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	out := make([]string, 0, len(themeOrder))
	for _, id := range themeOrder {
		out = append(out, id+"\t"+themes[id].Label)
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}
