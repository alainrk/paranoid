package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/alainrk/paranoid/internal/gitx"
	"github.com/alainrk/paranoid/internal/history"
)

// formatHTML is the default output format for `paranoid history`;
// history's other accepted value, formatJSON, is already declared in
// verify.go.
const formatHTML = "html"

// defaultHistoryOutput is the HTML output file `paranoid history`
// writes to when --output is not given.
const defaultHistoryOutput = "paranoid-history.html"

// newHistoryCmd builds the `paranoid history` command: it loads every
// report saved by `verify --save` under .paranoid/history/ and
// renders a score-over-time trend, either as one self-contained HTML
// file or as a JSON series for scripting.
func newHistoryCmd() *cobra.Command {
	var (
		output string
		format string
	)
	cmd := &cobra.Command{
		Use:   "history [path]",
		Short: "Render a score-over-time trend from saved reports",
		Long: "Render a score-over-time trend from reports saved by `verify --save`\n" +
			"under .paranoid/history/. The default output is one self-contained\n" +
			"HTML file with an inline SVG chart; --format json dumps the parsed\n" +
			"series instead, for scripting.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "."
			if len(args) == 1 {
				path = args[0]
			}
			switch format {
			case formatHTML, formatJSON:
			default:
				return fmt.Errorf("unknown --format %q, want html or json", format)
			}

			repo, err := gitx.Open(cmd.Context(), path)
			if err != nil {
				return err
			}
			envs, warnings, err := history.LoadDir(history.Dir(repo.Root()))
			if err != nil {
				return err
			}
			for _, w := range warnings {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "paranoid: history: %s\n", w)
			}
			series := history.BuildSeries(envs)

			if format == formatJSON {
				rendered, err := history.RenderJSON(series)
				if err != nil {
					return err
				}
				return writeReport(cmd, output, rendered)
			}
			rendered, err := history.RenderHTML(series)
			if err != nil {
				return err
			}
			outPath := output
			if outPath == "" {
				outPath = defaultHistoryOutput
			}
			return writeReport(cmd, outPath, rendered)
		},
	}
	cmd.Flags().StringVar(&format, "format", formatHTML, "output format: html or json")
	cmd.Flags().StringVar(&output, "output", "", "output file (default: "+defaultHistoryOutput+" for html, stdout for json)")
	return cmd
}
