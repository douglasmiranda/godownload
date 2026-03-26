package urlgen

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/douglasmiranda/godownload/internal/config"
)

type FileLoader func(path string) ([]string, error)

func Generate(task *config.DownloadTask, fileLoader FileLoader) ([]string, error) {
	if task == nil {
		return nil, fmt.Errorf("task is nil")
	}

	switch {
	case task.URLGenRange != nil:
		return generateRange(task.URL, task.URLGenRange)
	case task.URLGenList != nil:
		return generateList(task.URL, task.URLGenList)
	case task.URLGenDates != nil:
		return generateDates(task.URL, task.URLGenDates)
	case task.URLGenFile != nil:
		if fileLoader == nil {
			return nil, fmt.Errorf("file loader is required for url_gen_file")
		}
		return generateFromFile(task.URL, task.URLGenFile, fileLoader)
	default:
		return []string{task.URL}, nil
	}
}

func generateRange(tpl string, cfg *config.URLGenRange) ([]string, error) {
	step := cfg.Step
	if step == 0 {
		step = 1
	}
	if cfg.RangeEnd < cfg.RangeStart {
		return nil, fmt.Errorf("range_end must be >= range_start")
	}
	out := []string{}
	for i := cfg.RangeStart; i <= cfg.RangeEnd; i += step {
		out = append(out, replaceVar(tpl, cfg.VarName, strconv.Itoa(i)))
	}
	return out, nil
}

func generateList(tpl string, cfg *config.URLGenList) ([]string, error) {
	if len(cfg.Values) == 0 {
		return nil, fmt.Errorf("url_gen_list.values must not be empty")
	}
	out := make([]string, 0, len(cfg.Values))
	for _, v := range cfg.Values {
		out = append(out, replaceVar(tpl, cfg.VarName, v))
	}
	return out, nil
}

func generateFromFile(tpl string, cfg *config.URLGenFile, fileLoader FileLoader) ([]string, error) {
	lines, err := fileLoader(cfg.File)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, l := range lines {
		v := strings.TrimSpace(l)
		if v == "" {
			continue
		}
		out = append(out, replaceVar(tpl, cfg.VarName, v))
	}
	return out, nil
}

func generateDates(tpl string, cfg *config.URLGenDates) ([]string, error) {
	layout, err := dateFormatToGoLayout(cfg.DateFormat)
	if err != nil {
		return nil, err
	}
	start, err := time.Parse(layout, cfg.DateRangeStart)
	if err != nil {
		return nil, err
	}
	end, err := time.Parse(layout, cfg.DateRangeEnd)
	if err != nil {
		return nil, err
	}
	if end.Before(start) {
		return nil, fmt.Errorf("date_range_end must be >= date_range_start")
	}
	out := []string{}
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		out = append(out, replaceVar(tpl, cfg.VarName, d.Format(layout)))
	}
	return out, nil
}

func replaceVar(tpl, varName, value string) string {
	needle := "{{." + varName + "}}"
	return strings.ReplaceAll(tpl, needle, value)
}

func dateFormatToGoLayout(format string) (string, error) {
	switch strings.TrimSpace(format) {
	case "", "YYYY-MM-DD":
		return "2006-01-02", nil
	case "MM/DD/YYYY":
		return "01/02/2006", nil
	case "DD-MM-YYYY":
		return "02-01-2006", nil
	default:
		return "", fmt.Errorf("unsupported date format: %s", format)
	}
}
