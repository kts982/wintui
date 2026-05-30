package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/glamour/v2"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
)

var notesSourceFlag string

// notesFetchFn is the winget-show fetcher behind `wintui notes`. It is a var so
// tests can stub it with a synthetic PackageDetail instead of calling winget.
var notesFetchFn = showPackageCtx

var notesCmd = &cobra.Command{
	Use:   "notes <id>",
	Short: "Show a package's release notes (when the winget manifest has them)",
	Long: `Fetch and render the release notes winget has for a package's latest
version, formatted as markdown.

Heads up: many winget manifests ship only a release-notes URL — or nothing —
so notes shows the URL (or reports that none are available) when there's no
text to render. The manifest also carries only the latest version's notes, not
a changelog across the versions you may have skipped.`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeInstalledIDs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runNotes(args[0], notesSourceFlag, jsonFlag, os.Stdout)
	},
}

// notesOutput is the `--json` payload for `wintui notes`.
type notesOutput struct {
	ID              string `json:"id"`
	Source          string `json:"source"`
	Version         string `json:"version"`
	ReleaseNotes    string `json:"release_notes"`
	ReleaseNotesURL string `json:"release_notes_url"`
}

func runNotes(id, source string, jsonOut bool, out io.Writer) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("package id is required")
	}
	switch source {
	case "":
		source = "winget"
	case "winget", "msstore":
		// OK — the winget arg builders only honor these two.
	default:
		return fmt.Errorf("invalid --source %q: must be 'winget' or 'msstore'", source)
	}

	detail, err := notesFetchFn(context.Background(), Package{ID: id, Source: source}, "")
	if err != nil {
		return err
	}
	return renderNotes(detail, jsonOut, out)
}

// renderNotes prints the release notes for a fetched package detail. Kept
// separate from the winget fetch so it is unit-testable without winget.
func renderNotes(d PackageDetail, jsonOut bool, out io.Writer) error {
	if jsonOut {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(notesOutput{
			ID:              d.ID,
			Source:          d.Source,
			Version:         d.Version,
			ReleaseNotes:    d.ReleaseNotes,
			ReleaseNotesURL: d.ReleaseNotesURL,
		})
	}

	name := d.Name
	if name == "" {
		name = d.ID
	}
	header := name
	if d.Version != "" {
		header += " " + d.Version
	}
	fmt.Fprintln(out, header)

	notes := strings.TrimSpace(d.ReleaseNotes)
	switch {
	case notes != "":
		fmt.Fprint(out, renderReleaseNotesMarkdown(notes))
		if d.ReleaseNotesURL != "" {
			fmt.Fprintf(out, "Full notes: %s\n", d.ReleaseNotesURL)
		}
	case d.ReleaseNotesURL != "":
		fmt.Fprintf(out, "\nNo release notes in the winget manifest. See %s\n", d.ReleaseNotesURL)
	default:
		fmt.Fprintln(out, "\nNo release notes available for this package.")
	}
	return nil
}

// renderReleaseNotesMarkdown renders markdown notes via glamour. On any glamour
// error it falls back to the raw text so the user still sees the content.
func renderReleaseNotesMarkdown(notes string) string {
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(notesGlamourStyle()),
		glamour.WithWordWrap(notesWidth()),
	)
	if err != nil {
		return notes + "\n"
	}
	rendered, err := r.Render(notes)
	if err != nil {
		return notes + "\n"
	}
	return rendered
}

// notesGlamourStyle picks a glamour style. When stdout is not a terminal
// (piped/redirected) it uses the plain "notty" style so the output stays clean.
// Otherwise it assumes a dark terminal — matching WinTUI's theme default, since
// a one-shot CLI can't do the TUI's async background probe.
func notesGlamourStyle() string {
	if !term.IsTerminal(os.Stdout.Fd()) {
		return "notty"
	}
	return "dark"
}

// notesWidth returns a readable word-wrap width based on the terminal size,
// clamped to a sane range (80-column default when size is unavailable).
func notesWidth() int {
	w := 80
	if cols, _, err := term.GetSize(os.Stdout.Fd()); err == nil && cols > 0 {
		w = cols - 2
	}
	if w > 100 {
		w = 100
	}
	if w < 40 {
		w = 40
	}
	return w
}
