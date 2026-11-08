package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/douglasmiranda/godownload/internal/config"
	"github.com/douglasmiranda/godownload/internal/downloader"
	"github.com/douglasmiranda/godownload/internal/logging"
	"github.com/douglasmiranda/godownload/internal/ui"
)

const reportSchemaVersion = "1"

// exitInterrupted is the conventional exit code after Ctrl+C (128 + SIGINT).
const exitInterrupted = 130

// version is set at build time with -ldflags "-X main.version=v1.2.3".
var version = "dev"

type taskReport struct {
	TaskName      string            `json:"task_name"`
	Saved         int               `json:"saved"`
	Skipped       int               `json:"skipped"`
	Warnings      []string          `json:"warnings"`
	Errors        []string          `json:"errors"`
	SavedFiles    []string          `json:"saved_files"`
	MetadataFiles []string          `json:"metadata_files"`
	Planned       []plannedDownload `json:"planned,omitempty"`
	Aborted       bool              `json:"aborted"`
}

type plannedDownload struct {
	URL  string `json:"url"`
	Path string `json:"path"`
}

type runReport struct {
	ReportVersion string       `json:"report_version"`
	Tasks         []taskReport `json:"tasks"`
	TaskCount     int          `json:"task_count"`
	SavedTotal    int          `json:"saved_total"`
	SkippedTotal  int          `json:"skipped_total"`
	PlannedTotal  int          `json:"planned_total,omitempty"`
	WarningTotal  int          `json:"warning_total"`
	ErrorTotal    int          `json:"error_total"`
	Diagnostics   []string     `json:"diagnostics"`
	NotRun        []string     `json:"not_run"`
	OutputMode    string       `json:"output_mode"`
	ConfigPath    string       `json:"config_path"`
	ReportFile    string       `json:"report_file,omitempty"`
	DryRun        bool         `json:"dry_run,omitempty"`
	StopTriggered bool         `json:"stop_triggered"`
	Interrupted   bool         `json:"interrupted"`
}

type taskExecutionResult struct {
	name string
	task *config.DownloadTask
	res  downloader.TaskResult
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ctx.Done()
		// Restore the default handling so a second Ctrl+C exits immediately.
		stop()
	}()
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(parent context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("godownload", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "./download.yaml", "Path to the download config file")
	outputMode := fs.String("output", "text", "Output mode: text or json")
	reportFile := fs.String("report-file", "", "Optional path to write the structured run report as JSON")
	noColor := fs.Bool("no-color", false, "Disable color output")
	tasks := fs.String("tasks", "", "Comma-separated task names to run (default: all tasks)")
	validateOnly := fs.Bool("validate", false, "Validate the config and exit without downloading")
	dryRun := fs.Bool("dry-run", false, "Fetch pages to show what would be downloaded, without saving files or running hooks")
	showVersion := fs.Bool("version", false, "Print the version and exit")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected argument %q; use -config to choose the config file\n", fs.Arg(0))
		return 2
	}
	if *showVersion {
		fmt.Fprintf(stdout, "godownload %s\n", buildVersion())
		return 0
	}
	if *outputMode != "text" && *outputMode != "json" {
		fmt.Fprintf(stderr, "invalid -output value %q, expected text or json\n", *outputMode)
		return 2
	}
	if *validateOnly && *dryRun {
		fmt.Fprintln(stderr, "-validate and -dry-run cannot be used together")
		return 2
	}

	report := runReport{
		ReportVersion: reportSchemaVersion,
		Tasks:         []taskReport{},
		Diagnostics:   []string{},
		NotRun:        []string{},
		OutputMode:    *outputMode,
		ConfigPath:    *configPath,
		ReportFile:    *reportFile,
		DryRun:        *dryRun,
	}

	cfg, diags, err := config.LoadFile(*configPath)
	debugLogs := cfg != nil && cfg.Config.LogLevel == "DEBUG"
	redact := logging.RedactText
	if cfg != nil {
		redact = logging.NewRedactor(configSecrets(cfg))
	}

	// Progress bars would be interleaved with debug logs and dry-run lines.
	useInteractiveUI := *outputMode == "text" && ui.IsTerminal(stdout) && !*validateOnly && !*dryRun && !debugLogs
	printer := ui.New(stdout, stderr, ui.Options{
		NoColor:     *noColor,
		Interactive: useInteractiveUI,
	})

	for _, d := range diags {
		msg := redact(d.String())
		report.Diagnostics = append(report.Diagnostics, msg)
		if *outputMode == "text" {
			printer.PrintDiagnostic(msg)
		}
	}
	if err != nil {
		printer.PrintError(fmt.Sprintf("failed to load config: %s", redact(err.Error())))
		if emitErr := emitReport(stdout, report); emitErr != nil {
			printer.PrintError(redact(emitErr.Error()))
		}
		return 1
	}

	names, unknownTasks := selectedTaskNames(cfg.Downloads, *tasks)
	if len(unknownTasks) > 0 {
		printer.PrintError(fmt.Sprintf("unknown task(s): %s", strings.Join(unknownTasks, ", ")))
		return 2
	}

	taskCount := len(names)
	report.TaskCount = taskCount
	if *validateOnly || taskCount == 0 {
		if emitErr := emitReport(stdout, report); emitErr != nil {
			printer.PrintError(redact(emitErr.Error()))
			return 1
		}
		if *outputMode == "text" {
			if *validateOnly {
				fmt.Fprintf(stdout, "config is valid: %d task(s)\n", taskCount)
			} else {
				fmt.Fprintln(stdout, "no download tasks configured")
			}
		}
		return 0
	}

	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	svc := &downloader.Service{
		OnAbortAll: cancel,
		Redact:     redact,
		DryRun:     *dryRun,
	}
	if debugLogs {
		svc.Logger = slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	if printer.Interactive() && printer.Progress() != nil {
		svc.TaskProgress = printer.Progress()
	}

	taskConcurrency := cfg.Config.TaskConcurrency
	if taskConcurrency <= 0 {
		taskConcurrency = config.DefaultTaskConcurrency
	}
	sem := make(chan struct{}, taskConcurrency)
	resultsCh := make(chan taskExecutionResult, len(names))
	started := make(map[string]bool, len(names))
	var wg sync.WaitGroup

scheduleLoop:
	for _, name := range names {
		select {
		case <-ctx.Done():
			break scheduleLoop
		default:
		}

		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break scheduleLoop
		}

		started[name] = true
		task := cfg.Downloads[name]
		wg.Add(1)
		go func(taskName string, task *config.DownloadTask) {
			defer wg.Done()
			defer func() { <-sem }()

			res := svc.RunTask(ctx, taskName, task, cfg.Config)
			resultsCh <- taskExecutionResult{name: taskName, task: task, res: res}
		}(name, task)
	}

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	totalSaved := 0
	totalPlanned := 0
	totalWarnings := 0
	totalErrors := 0
	totalSkipped := 0
	stopTriggered := false
	stopTask := ""

	var tableRows []ui.TaskRow
	var tableDetails []ui.TaskDetails

	for outcome := range resultsCh {
		name := outcome.name
		task := outcome.task
		res := outcome.res

		savedCount := len(res.Saved)
		skippedCount := 0
		for _, s := range res.Saved {
			if s.Skipped {
				skippedCount++
			}
		}

		totalSaved += savedCount - skippedCount
		totalSkipped += skippedCount
		totalPlanned += len(res.Planned)
		totalWarnings += len(res.Warnings)
		totalErrors += len(res.Errors)

		warningMsgs := make([]string, 0, len(res.Warnings))
		for _, w := range res.Warnings {
			warningMsgs = append(warningMsgs, redact(w))
		}
		errorMsgs := make([]string, 0, len(res.Errors))
		for _, e := range res.Errors {
			errorMsgs = append(errorMsgs, redact(e.Error()))
		}
		savedFiles := make([]string, 0, len(res.Saved))
		metadataFiles := make([]string, 0, len(res.Saved))
		for _, s := range res.Saved {
			if !s.Skipped && s.FilePath != "" {
				savedFiles = append(savedFiles, s.FilePath)
			}
			if s.MetadataPath != "" {
				metadataFiles = append(metadataFiles, s.MetadataPath)
			}
		}
		planned := make([]plannedDownload, 0, len(res.Planned))
		for _, p := range res.Planned {
			planned = append(planned, plannedDownload{URL: redact(p.URL), Path: p.Path})
		}
		sort.Slice(planned, func(i, j int) bool { return planned[i].Path < planned[j].Path })

		report.Tasks = append(report.Tasks, taskReport{
			TaskName:      name,
			Saved:         savedCount - skippedCount,
			Skipped:       skippedCount,
			Warnings:      warningMsgs,
			Errors:        errorMsgs,
			SavedFiles:    savedFiles,
			MetadataFiles: metadataFiles,
			Planned:       planned,
			Aborted:       res.AbortAll,
		})

		if *outputMode == "text" {
			switch {
			case printer.Interactive():
				tableRows = append(tableRows, ui.TaskRow{
					Name:     name,
					Saved:    savedCount - skippedCount,
					Skipped:  skippedCount,
					Warnings: len(res.Warnings),
					Errors:   len(res.Errors),
					Failed:   len(res.Errors) > 0,
				})
				if len(warningMsgs) > 0 || len(errorMsgs) > 0 {
					tableDetails = append(tableDetails, ui.TaskDetails{
						Name:     name,
						Warnings: warningMsgs,
						Errors:   errorMsgs,
					})
				}
			default:
				if *dryRun {
					printer.PrintDryRunTaskLine(name, len(planned), skippedCount, len(res.Warnings), len(res.Errors))
					for _, p := range planned {
						printer.PrintPlannedDownload(p.URL, p.Path)
					}
				} else {
					printer.PrintLegacyTaskLine(name, savedCount-skippedCount, skippedCount, len(res.Warnings), len(res.Errors))
				}
				for _, w := range warningMsgs {
					printer.PrintLegacyWarning(name, w)
				}
				for _, e := range errorMsgs {
					printer.PrintLegacyError(name, e)
				}
			}
		}

		if task != nil && res.AbortAll && !stopTriggered {
			stopTriggered = true
			stopTask = name
			if *outputMode == "text" && !printer.Interactive() {
				printer.PrintLegacyStop(name)
			}
		}
	}

	interrupted := parent.Err() != nil
	for _, name := range names {
		if !started[name] {
			report.NotRun = append(report.NotRun, name)
		}
	}
	sort.Slice(report.Tasks, func(i, j int) bool { return report.Tasks[i].TaskName < report.Tasks[j].TaskName })
	sort.Slice(tableRows, func(i, j int) bool { return tableRows[i].Name < tableRows[j].Name })
	sort.Slice(tableDetails, func(i, j int) bool { return tableDetails[i].Name < tableDetails[j].Name })

	report.SavedTotal = totalSaved
	report.SkippedTotal = totalSkipped
	report.PlannedTotal = totalPlanned
	report.WarningTotal = totalWarnings
	report.ErrorTotal = totalErrors
	report.StopTriggered = stopTriggered
	report.Interrupted = interrupted
	if err := emitReport(stdout, report); err != nil {
		printer.PrintError(redact(err.Error()))
		return 1
	}

	if *outputMode == "text" {
		switch {
		case printer.Interactive():
			printer.PrintResultsTable(tableRows, tableDetails, taskCount, totalSaved, totalSkipped, totalWarnings, totalErrors, stopTriggered, stopTask)
		case *dryRun:
			printer.PrintDryRunSummary(taskCount, totalPlanned, totalSkipped, totalWarnings, totalErrors)
		default:
			printer.PrintLegacySummary(taskCount, totalSaved, totalSkipped, totalWarnings, totalErrors)
		}
		if len(report.NotRun) > 0 {
			printer.PrintError(fmt.Sprintf("tasks not run: %s", strings.Join(report.NotRun, ", ")))
		}
		if interrupted {
			printer.PrintError("interrupted; running tasks were cancelled")
		}
	}
	if interrupted {
		return exitInterrupted
	}
	if totalErrors > 0 {
		return 1
	}
	return 0
}

// configSecrets returns the credential values that must never appear in
// output, whether the configured auth is global or per task.
func configSecrets(cfg *config.Config) []string {
	var out []string
	add := func(a *config.AuthConfig) {
		if a != nil {
			out = append(out, a.Password, a.Token, a.KeyValue)
		}
	}
	add(cfg.Config.Auth)
	for _, task := range cfg.Downloads {
		if task != nil {
			add(task.Auth)
		}
	}
	return out
}

func buildVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}

// emitReport writes the report file, if requested, and prints the report in
// JSON output mode.
func emitReport(stdout io.Writer, report runReport) error {
	if err := writeReportFile(report.ReportFile, report); err != nil {
		return fmt.Errorf("failed to write report file: %w", err)
	}
	if report.OutputMode != "json" {
		return nil
	}
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode report: %w", err)
	}
	fmt.Fprintf(stdout, "%s\n", b)
	return nil
}

func sortedTaskNames(m map[string]*config.DownloadTask) []string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

func selectedTaskNames(m map[string]*config.DownloadTask, tasks string) ([]string, []string) {
	if strings.TrimSpace(tasks) == "" {
		return sortedTaskNames(m), nil
	}

	requested := make(map[string]struct{})
	for _, name := range strings.Split(tasks, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			requested[name] = struct{}{}
		}
	}
	if len(requested) == 0 {
		return sortedTaskNames(m), nil
	}

	names := make([]string, 0, len(requested))
	unknown := make([]string, 0)
	for name := range requested {
		if _, ok := m[name]; ok {
			names = append(names, name)
		} else {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(names)
	sort.Strings(unknown)
	return names, unknown
}

func writeReportFile(path string, report runReport) error {
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	dir := filepath.Dir(path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create report directory: %w", err)
		}
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write report file: %w", err)
	}
	return nil
}
