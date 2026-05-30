package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/glamour/v2"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
)

var notesSourceFlag string

// errNotesPackageNotFound is returned when winget can't resolve the id to a
// single package (unknown id, or a partial id that matches several).
var errNotesPackageNotFound = errors.New("package not found")

// notesFetchFn is the winget-show fetcher behind `wintui notes`. It is a var so
// tests can stub it with a synthetic PackageDetail instead of calling winget.
var notesFetchFn = fetchPackageNotes

// fetchPackageNotes runs `winget show` for a single package and parses its
// detail. Unlike the TUI detail fetch (showPackageCtx), it does NOT pass
// --exact: winget treats --exact as case-sensitive, but CLI users type ids by
// hand (e.g. `git.git`), and a non-exact `--id` match is case-insensitive. When
// the id resolves to no package — or to several (a partial id) — winget prints
// "No package found" or a disambiguation list with no Version line to parse, so
// we surface errNotesPackageNotFound rather than a misleading "no notes".
func fetchPackageNotes(ctx context.Context, id, source string) (PackageDetail, error) {
	args := []string{"show", "--id", id}
	if source == "winget" || source == "msstore" {
		args = append(args, "--source", source)
	}
	out, err := runWingetCtx(ctx, args...)
	if err != nil && strings.TrimSpace(out) == "" {
		return PackageDetail{}, err
	}

	detail := parseWingetShow(out)
	// A found package always has a Version line; "No package found" / list
	// output has none. Notes/URL presence is an additional positive signal.
	if strings.TrimSpace(detail.Version) == "" &&
		strings.TrimSpace(detail.ReleaseNotes) == "" &&
		strings.TrimSpace(detail.ReleaseNotesURL) == "" {
		return PackageDetail{}, errNotesPackageNotFound
	}
	if detail.ID == "" {
		detail.ID = id
	}
	if detail.Source == "" {
		detail.Source = source
	}
	return detail, nil
}

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

	detail, err := notesFetchFn(context.Background(), id, source)
	if err != nil {
		if errors.Is(err, errNotesPackageNotFound) {
			return fmt.Errorf("no package found matching id %q (check the spelling; ids are matched case-insensitively)", id)
		}
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
