package ui

import (
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
)

// TaskRow is one line in the end-of-run summary table.
type TaskRow struct {
	Name     string
	Saved    int
	Skipped  int
	Warnings int
	Errors   int
	Failed   bool
}

// TaskDetails holds per-task warning and error messages for display after the table.
type TaskDetails struct {
	Name     string
	Warnings []string
	Errors   []string
}

// Options configures Printer behavior.
type Options struct {
	NoColor     bool
	Interactive bool
}

// Printer renders CLI output for text mode.
type Printer struct {
	stdout io.Writer
	stderr io.Writer
	color  bool
	interactive bool
	progress *ProgressTracker
}

// IsTerminal reports whether w is a terminal device.
func IsTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return isatty.IsTerminal(f.Fd())
}

func New(stdout, stderr io.Writer, opts Options) *Printer {
	interactive := opts.Interactive
	colorOn := interactive && !opts.NoColor
	if interactive && !opts.NoColor {
		// fatih/color respects NO_COLOR; ensure we don't force color on non-tty stderr.
		if f, ok := stdout.(*os.File); ok && !isatty.IsTerminal(f.Fd()) {
			colorOn = false
		}
	}

	p := &Printer{
		stdout:      stdout,
		stderr:      stderr,
		color:       colorOn,
		interactive: interactive,
	}
	if interactive {
		p.progress = newProgressTracker()
	}
	return p
}

// Interactive reports whether the deferred table + progress UI is active.
func (p *Printer) Interactive() bool {
	return p.interactive
}

// Progress returns the task progress tracker, or nil in legacy mode.
func (p *Printer) Progress() *ProgressTracker {
	return p.progress
}

// PrintDiagnostic writes a config diagnostic line to stderr.
func (p *Printer) PrintDiagnostic(msg string) {
	if p.color {
		color.New(color.FgYellow).Fprintln(p.stderr, msg)
		return
	}
	fmt.Fprintln(p.stderr, msg)
}

// PrintError writes a plain error line to stderr.
func (p *Printer) PrintError(msg string) {
	if p.color {
		color.New(color.FgRed).Fprintln(p.stderr, msg)
		return
	}
	fmt.Fprintln(p.stderr, msg)
}

// PrintLegacyTaskLine writes the streaming per-task summary line to stdout.
func (p *Printer) PrintLegacyTaskLine(name string, saved, skipped, warnings, errors int) {
	fmt.Fprintf(p.stdout, "task %s: saved=%d skipped=%d warnings=%d errors=%d\n",
		name, saved, skipped, warnings, errors)
}

// PrintLegacyWarning writes a streaming warning to stderr.
func (p *Printer) PrintLegacyWarning(taskName, msg string) {
	fmt.Fprintf(p.stderr, "[warning] %s: %s\n", taskName, msg)
}

// PrintLegacyError writes a streaming error to stderr.
func (p *Printer) PrintLegacyError(taskName, msg string) {
	fmt.Fprintf(p.stderr, "[error] %s: %s\n", taskName, msg)
}

// PrintLegacyStop writes the stop_all_tasks message to stderr.
func (p *Printer) PrintLegacyStop(taskName string) {
	fmt.Fprintf(p.stderr, "stop_all_tasks triggered by task %s; aborting remaining tasks\n", taskName)
}

// PrintLegacySummary writes the final summary line to stdout.
func (p *Printer) PrintLegacySummary(taskCount, saved, skipped, warnings, errors int) {
	fmt.Fprintf(p.stdout, "summary: tasks=%d saved=%d skipped=%d warnings=%d errors=%d\n",
		taskCount, saved, skipped, warnings, errors)
}

// Wait flushes progress bars before other stdout output.
func (p *Printer) Wait() {
	if p.progress != nil {
		p.progress.Wait()
	}
}

// PrintResultsTable writes the colored task table and summary to stdout, and diagnostics to stderr.
func (p *Printer) PrintResultsTable(rows []TaskRow, details []TaskDetails, taskCount, saved, skipped, warnings, errors int, stopTriggered bool, stopTask string) {
	p.Wait()

	w := tabwriter.NewWriter(p.stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TASK\tSAVED\tSKIP\tWARN\tERR\tSTATUS")
	for _, row := range rows {
		status := p.statusText(row.Failed || row.Errors > 0)
		fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%s\n",
			row.Name, row.Saved, row.Skipped, row.Warnings, row.Errors, status)
	}
	_ = w.Flush()

	fmt.Fprintln(p.stdout)

	for _, d := range details {
		for _, w := range d.Warnings {
			if p.color {
				color.New(color.FgYellow).Fprintf(p.stderr, "  [warning] %s: %s\n", d.Name, w)
			} else {
				fmt.Fprintf(p.stderr, "  [warning] %s: %s\n", d.Name, w)
			}
		}
		for _, e := range d.Errors {
			if p.color {
				color.New(color.FgRed).Fprintf(p.stderr, "  [error] %s: %s\n", d.Name, e)
			} else {
				fmt.Fprintf(p.stderr, "  [error] %s: %s\n", d.Name, e)
			}
		}
	}

	if stopTriggered {
		msg := fmt.Sprintf("stop_all_tasks triggered by task %s; aborting remaining tasks", stopTask)
		if p.color {
			color.New(color.FgRed).Fprintln(p.stderr, msg)
		} else {
			fmt.Fprintln(p.stderr, msg)
		}
	}

	summary := fmt.Sprintf("Summary: %d tasks | %d saved | %d skipped | %d warnings | %d errors",
		taskCount, saved, skipped, warnings, errors)
	if p.color {
		if errors > 0 {
			color.New(color.FgRed).Fprintln(p.stdout, summary)
		} else {
			color.New(color.FgGreen).Fprintln(p.stdout, summary)
		}
	} else {
		fmt.Fprintln(p.stdout, summary)
	}
}

func (p *Printer) statusText(failed bool) string {
	if failed {
		if p.color {
			return color.RedString("failed")
		}
		return "failed"
	}
	if p.color {
		return color.GreenString("ok")
	}
	return "ok"
}
