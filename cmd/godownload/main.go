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
	"sync"

	"github.com/douglasmiranda/godownload/internal/config"
	"github.com/douglasmiranda/godownload/internal/downloader"
	"github.com/douglasmiranda/godownload/internal/logging"
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
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *outputMode != "text" && *outputMode != "json" {
		fmt.Fprintf(stderr, "invalid -output value %q, expected text or json\n", *outputMode)
		return 2
	}

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
			fmt.Fprintf(stderr, "%s\n", msg)
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "failed to load config: %s\n", logging.RedactText(err.Error()))
		if writeErr := writeReportFile(*reportFile, report); writeErr != nil {
			fmt.Fprintf(stderr, "failed to write report file: %s\n", logging.RedactText(writeErr.Error()))
		}
		if *outputMode == "json" {
			_ = json.NewEncoder(stdout).Encode(report)
		}
		return 1
	}

	if len(cfg.Downloads) == 0 {
		report.TaskCount = 0
		if writeErr := writeReportFile(*reportFile, report); writeErr != nil {
			fmt.Fprintf(stderr, "failed to write report file: %s\n", logging.RedactText(writeErr.Error()))
			return 1
		}
		if *outputMode == "json" {
			b, err := json.MarshalIndent(report, "", "  ")
			if err != nil {
				fmt.Fprintf(stderr, "failed to encode report: %s\n", logging.RedactText(err.Error()))
				return 1
			}
			fmt.Fprintf(stdout, "%s\n", string(b))
			return 0
		}
		fmt.Fprintln(stdout, "no download tasks configured")
		return 0
	}

	svc := &downloader.Service{}
	names := sortedTaskNames(cfg.Downloads)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
			if task != nil && res.AbortAll {
				cancel()
			}
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
			fmt.Fprintf(stdout, "task %s: saved=%d skipped=%d warnings=%d errors=%d\n", name, savedCount-skippedCount, skippedCount, len(res.Warnings), len(res.Errors))
		}

		for _, w := range warningMsgs {
			if *outputMode == "text" {
				fmt.Fprintf(stderr, "[warning] %s: %s\n", name, w)
			}
		}
		for _, e := range errorMsgs {
			if *outputMode == "text" {
				fmt.Fprintf(stderr, "[error] %s: %s\n", name, e)
			}
		}

		if task != nil && res.AbortAll && !stopTriggered {
			stopTriggered = true
			if *outputMode == "text" {
				fmt.Fprintf(stderr, "stop_all_tasks triggered by task %s; aborting remaining tasks\n", name)
			}
		}
	}

	report.TaskCount = len(cfg.Downloads)
	report.SavedTotal = totalSaved
	report.SkippedTotal = totalSkipped
	report.WarningTotal = totalWarnings
	report.ErrorTotal = totalErrors
	report.StopTriggered = stopTriggered
	if writeErr := writeReportFile(*reportFile, report); writeErr != nil {
		fmt.Fprintf(stderr, "failed to write report file: %s\n", logging.RedactText(writeErr.Error()))
		return 1
	}

	if *outputMode == "json" {
		b, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "failed to encode report: %s\n", logging.RedactText(err.Error()))
			return 1
		}
		fmt.Fprintf(stdout, "%s\n", string(b))
	} else {
		fmt.Fprintf(stdout, "summary: tasks=%d saved=%d skipped=%d warnings=%d errors=%d\n", len(cfg.Downloads), totalSaved, totalSkipped, totalWarnings, totalErrors)
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
