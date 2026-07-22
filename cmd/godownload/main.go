package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/douglasmiranda/godownload/internal/config"
	"github.com/douglasmiranda/godownload/internal/downloader"
	"github.com/douglasmiranda/godownload/internal/logging"
	"github.com/douglasmiranda/godownload/internal/ui"
)

const reportSchemaVersion = "1"

type taskReport struct {
	TaskName      string   `json:"task_name"`
	Saved         int      `json:"saved"`
	Skipped       int      `json:"skipped"`
	Warnings      []string `json:"warnings"`
	Errors        []string `json:"errors"`
	SavedFiles    []string `json:"saved_files"`
	MetadataFiles []string `json:"metadata_files"`
	Aborted       bool     `json:"aborted"`
}

type runReport struct {
	ReportVersion string       `json:"report_version"`
	Tasks         []taskReport `json:"tasks"`
	TaskCount     int          `json:"task_count"`
	SavedTotal    int          `json:"saved_total"`
	SkippedTotal  int          `json:"skipped_total"`
	WarningTotal  int          `json:"warning_total"`
	ErrorTotal    int          `json:"error_total"`
	Diagnostics   []string     `json:"diagnostics"`
	OutputMode    string       `json:"output_mode"`
	ConfigPath    string       `json:"config_path"`
	ReportFile    string       `json:"report_file,omitempty"`
	StopTriggered bool         `json:"stop_triggered"`
}

type taskExecutionResult struct {
	name string
	task *config.DownloadTask
	res  downloader.TaskResult
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("godownload", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "./download.yaml", "Path to the download config file")
	outputMode := fs.String("output", "text", "Output mode: text or json")
	reportFile := fs.String("report-file", "", "Optional path to write the structured run report as JSON")
	noColor := fs.Bool("no-color", false, "Disable color output")
	tasks := fs.String("tasks", "", "Comma-separated task names to run (default: all tasks)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *outputMode != "text" && *outputMode != "json" {
		fmt.Fprintf(stderr, "invalid -output value %q, expected text or json\n", *outputMode)
		return 2
	}

	useInteractiveUI := *outputMode == "text" && ui.IsTerminal(stdout)
	printer := ui.New(stdout, stderr, ui.Options{
		NoColor:     *noColor,
		Interactive: useInteractiveUI,
	})

	cfg, diags, err := config.LoadFile(*configPath)
	report := runReport{
		ReportVersion: reportSchemaVersion,
		Tasks:         []taskReport{},
		Diagnostics:   []string{},
		OutputMode:    *outputMode,
		ConfigPath:    *configPath,
		ReportFile:    *reportFile,
	}
	for _, d := range diags {
		msg := logging.RedactText(d.String())
		report.Diagnostics = append(report.Diagnostics, msg)
		if *outputMode == "text" {
			printer.PrintDiagnostic(msg)
		}
	}
	if err != nil {
		printer.PrintError(fmt.Sprintf("failed to load config: %s", logging.RedactText(err.Error())))
		if writeErr := writeReportFile(*reportFile, report); writeErr != nil {
			printer.PrintError(fmt.Sprintf("failed to write report file: %s", logging.RedactText(writeErr.Error())))
		}
		if *outputMode == "json" {
			_ = json.NewEncoder(stdout).Encode(report)
		}
		return 1
	}

	names, unknownTasks := selectedTaskNames(cfg.Downloads, *tasks)
	if len(unknownTasks) > 0 {
		printer.PrintError(fmt.Sprintf("unknown task(s): %s", strings.Join(unknownTasks, ", ")))
		return 2
	}

	taskCount := len(names)
	if taskCount == 0 {
		report.TaskCount = 0
		if writeErr := writeReportFile(*reportFile, report); writeErr != nil {
			printer.PrintError(fmt.Sprintf("failed to write report file: %s", logging.RedactText(writeErr.Error())))
			return 1
		}
		if *outputMode == "json" {
			b, err := json.MarshalIndent(report, "", "  ")
			if err != nil {
				printer.PrintError(fmt.Sprintf("failed to encode report: %s", logging.RedactText(err.Error())))
				return 1
			}
			fmt.Fprintf(stdout, "%s\n", string(b))
			return 0
		}
		fmt.Fprintln(stdout, "no download tasks configured")
		return 0
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var cancelOnce sync.Once
	svc := &downloader.Service{
		OnAbortAll: func() { cancelOnce.Do(cancel) },
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
		totalWarnings += len(res.Warnings)
		totalErrors += len(res.Errors)

		warningMsgs := make([]string, 0, len(res.Warnings))
		for _, w := range res.Warnings {
			warningMsgs = append(warningMsgs, logging.RedactText(w))
		}
		errorMsgs := make([]string, 0, len(res.Errors))
		for _, e := range res.Errors {
			errorMsgs = append(errorMsgs, logging.RedactText(e.Error()))
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

		report.Tasks = append(report.Tasks, taskReport{
			TaskName:      name,
			Saved:         savedCount - skippedCount,
			Skipped:       skippedCount,
			Warnings:      warningMsgs,
			Errors:        errorMsgs,
			SavedFiles:    savedFiles,
			MetadataFiles: metadataFiles,
			Aborted:       res.AbortAll,
		})

		if *outputMode == "text" {
			if printer.Interactive() {
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
			} else {
				printer.PrintLegacyTaskLine(name, savedCount-skippedCount, skippedCount, len(res.Warnings), len(res.Errors))
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

	report.TaskCount = taskCount
	report.SavedTotal = totalSaved
	report.SkippedTotal = totalSkipped
	report.WarningTotal = totalWarnings
	report.ErrorTotal = totalErrors
	report.StopTriggered = stopTriggered
	if writeErr := writeReportFile(*reportFile, report); writeErr != nil {
		printer.PrintError(fmt.Sprintf("failed to write report file: %s", logging.RedactText(writeErr.Error())))
		return 1
	}

	if *outputMode == "json" {
		b, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			printer.PrintError(fmt.Sprintf("failed to encode report: %s", logging.RedactText(err.Error())))
			return 1
		}
		fmt.Fprintf(stdout, "%s\n", string(b))
	} else if printer.Interactive() {
		printer.PrintResultsTable(tableRows, tableDetails, taskCount, totalSaved, totalSkipped, totalWarnings, totalErrors, stopTriggered, stopTask)
	} else {
		printer.PrintLegacySummary(taskCount, totalSaved, totalSkipped, totalWarnings, totalErrors)
	}
	if totalErrors > 0 {
		return 1
	}
	return 0
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
