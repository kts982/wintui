package main

import (
	"image/color"

	"charm.land/fang/v2"
	"charm.land/lipgloss/v2"
)

// fangColorScheme maps the active WinTUI palette onto fang's CLI help
// colorscheme so `wintui --help`, error output, and `--version` match the
// user's chosen theme — extending the v2.8.0 palette engine to the headless
// CLI surface.
//
// fang hands us a lipgloss.LightDarkFunc reflecting the terminal background,
// so rather than reading the already-resolved package color vars (which are
// fixed to the dark variant for CLI, since the TUI's async background probe
// never runs here) we feed it the active theme's Light/Dark palette pair and
// let fang pick the right variant per color. Themes without a light variant
// fall back to Dark for both, matching setActiveTheme's behavior.
func fangColorScheme(ld lipgloss.LightDarkFunc) fang.ColorScheme {
	t := lookupTheme(normalizeTheme(appSettings.Theme))
	d := t.Dark
	l := t.Light
	if !hasLightVariant(l) {
		l = d
	}
	return fang.ColorScheme{
		Base:           ld(l.Bright, d.Bright),
		Title:          ld(l.Accent, d.Accent),
		Program:        ld(l.Accent, d.Accent),
		Command:        ld(l.Secondary, d.Secondary),
		Flag:           ld(l.State, d.State),
		Argument:       ld(l.Bright, d.Bright),
		DimmedArgument: ld(l.Dim, d.Dim),
		Description:    ld(l.Subtle, d.Subtle),
		FlagDefault:    ld(l.Dim, d.Dim),
		Comment:        ld(l.Subtle, d.Subtle),
		QuotedString:   ld(l.State, d.State),
		Codeblock:      ld(lipgloss.Color("#EEEEEE"), lipgloss.Color("#2F2E36")),
		Help:           ld(l.Dim, d.Dim),
		Dash:           ld(l.Dim, d.Dim),
		// White-on-danger reads on both light and dark terminals.
		ErrorHeader:  [2]color.Color{lipgloss.Color("#FFFFFF"), ld(l.Danger, d.Danger)},
		ErrorDetails: ld(l.Danger, d.Danger),
	}
}

// fangOptions assembles the fang.Execute options for the root command.
// Kept here so main() stays terse and the fang dependency surface is
// localized to this file.
func fangOptions() []fang.Option {
	return []fang.Option{
		// Preserve the existing rich version string (version (commit) built date)
		// set on rootCmd.Version in init(); fang would otherwise overwrite it
		// from build info and lose the ldflags-injected values.
		fang.WithVersion(rootCmd.Version),
		fang.WithColorSchemeFunc(fangColorScheme),
		// Manpages are pointless on Windows; the hidden `man` subcommand would
		// only add noise. Completions stay enabled (cobra's default command)
		// for `wintui completion powershell`.
		fang.WithoutManpage(),
	}
}
