package config

import "gopkg.in/yaml.v3"

const (
	DefaultConcurrency      = 10
	DefaultLogLevel         = "INFO"
	DefaultFilenameTemplate = "{{.ORIGINAL_FILENAME}}"
	DefaultMaxDepth         = 1
)

type Config struct {
	EnvFile   []string                 `yaml:"env_file"`
	Env       map[string]string        `yaml:"env"`
	Config    WorkerConfig             `yaml:"config"`
	Downloads map[string]*DownloadTask `yaml:"downloads"`
}

type WorkerConfig struct {
	Concurrency          int               `yaml:"concurrency"`
	LogLevel             string            `yaml:"log_level"`
	Save                 SaveConfig        `yaml:"save"`
	DefaultAttrMapping   map[string]string `yaml:"default_attr_mappings"`
	Selector             string            `yaml:"selector"`
	Attr                 string            `yaml:"attr"`
	RegexFilter          string            `yaml:"regex_filter"`
	AttrValRegexFilter   string            `yaml:"attr_val_regex_filter"`
	URLGenRange          *URLGenRange      `yaml:"url_gen_range"`
	URLGenList           *URLGenList       `yaml:"url_gen_list"`
	URLGenFile           *URLGenFile       `yaml:"url_gen_file"`
	URLGenDates          *URLGenDates      `yaml:"url_gen_dates"`
	ForceDownload        bool              `yaml:"force_download"`
	BaseDownloadDir      string            `yaml:"base_download_dir"`
	StopAllTasks         StopAllTasks      `yaml:"stop_all_tasks"`
	Timeout              int               `yaml:"timeout"`
	DownloadLimit        int               `yaml:"download_limit"`
	SelectorMatchesLimit int               `yaml:"selector_matches_limit"`
	Retry                Retry             `yaml:"retry"`
	Env                  map[string]string `yaml:"env"`
	Auth                 *AuthConfig       `yaml:"auth"`
	Security             SecurityConfig    `yaml:"security"`
	MaxDepth             int               `yaml:"max_depth"`
	setFields            map[string]bool   `yaml:"-"`
}

func (w *WorkerConfig) UnmarshalYAML(value *yaml.Node) error {
	type workerAlias WorkerConfig
	var aux workerAlias
	if err := value.Decode(&aux); err != nil {
		return err
	}
	*w = WorkerConfig(aux)
	w.setFields = collectYAMLFieldPaths(value)
	return nil
}

func (w *WorkerConfig) hasField(path string) bool {
	if w == nil || w.setFields == nil {
		return false
	}
	return w.setFields[path]
}

type DownloadTask struct {
	URL                  string            `yaml:"url"`
	Selector             string            `yaml:"selector"`
	Attr                 string            `yaml:"attr"`
	RegexFilter          string            `yaml:"regex_filter"`
	AttrValRegexFilter   string            `yaml:"attr_val_regex_filter"`
	URLGenRange          *URLGenRange      `yaml:"url_gen_range"`
	URLGenList           *URLGenList       `yaml:"url_gen_list"`
	URLGenFile           *URLGenFile       `yaml:"url_gen_file"`
	URLGenDates          *URLGenDates      `yaml:"url_gen_dates"`
	ForceDownload        bool              `yaml:"force_download"`
	Save                 SaveConfig        `yaml:"save"`
	BaseDownloadDir      string            `yaml:"base_download_dir"`
	StopAllTasks         StopAllTasks      `yaml:"stop_all_tasks"`
	Timeout              int               `yaml:"timeout"`
	DownloadLimit        int               `yaml:"download_limit"`
	SelectorMatchesLimit int               `yaml:"selector_matches_limit"`
	Retry                Retry             `yaml:"retry"`
	Env                  map[string]string `yaml:"env"`
	Auth                 *AuthConfig       `yaml:"auth"`
	Security             SecurityConfig    `yaml:"security"`
	MaxDepth             int               `yaml:"max_depth"`
	setFields            map[string]bool   `yaml:"-"`
}

func (t *DownloadTask) UnmarshalYAML(value *yaml.Node) error {
	type taskAlias DownloadTask
	var aux taskAlias
	if err := value.Decode(&aux); err != nil {
		return err
	}
	*t = DownloadTask(aux)
	t.setFields = collectYAMLFieldPaths(value)
	return nil
}

func (t *DownloadTask) hasField(path string) bool {
	if t == nil || t.setFields == nil {
		return false
	}
	return t.setFields[path]
}

func collectYAMLFieldPaths(node *yaml.Node) map[string]bool {
	out := map[string]bool{}
	collectYAMLFieldPathsRecursive(node, "", out)
	return out
}

func collectYAMLFieldPathsRecursive(node *yaml.Node, prefix string, out map[string]bool) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		k := node.Content[i]
		v := node.Content[i+1]
		if k == nil {
			continue
		}
		path := k.Value
		if prefix != "" {
			path = prefix + "." + path
		}
		out[path] = true
		if v != nil && v.Kind == yaml.MappingNode {
			collectYAMLFieldPathsRecursive(v, path, out)
		}
	}
}

type URLGenRange struct {
	VarName    string `yaml:"var_name"`
	RangeStart int    `yaml:"range_start"`
	RangeEnd   int    `yaml:"range_end"`
	Step       int    `yaml:"step"`
}

type URLGenList struct {
	VarName string   `yaml:"var_name"`
	Values  []string `yaml:"values"`
}

type URLGenFile struct {
	VarName string `yaml:"var_name"`
	File    string `yaml:"file"`
}

type URLGenDates struct {
	VarName        string `yaml:"var_name"`
	DateRangeStart string `yaml:"date_range_start"`
	DateRangeEnd   string `yaml:"date_range_end"`
	DateFormat     string `yaml:"date_format"`
}

type SaveConfig struct {
	BaseDownloadDir  string `yaml:"base_download_dir"`
	FilenameTemplate string `yaml:"filename_template"`
	Filename         string `yaml:"filename"`
	OverrideIfExists bool   `yaml:"override_if_exists"`
	Extension        string `yaml:"extension"`
	SaveMetadata     bool   `yaml:"save_metadata"`
}

type Retry struct {
	Enabled bool
	Config  RetryConfig
}

type RetryConfig struct {
	OnRuntimeError     bool  `yaml:"on_runtime_error"`
	OnHTTPCode         []int `yaml:"on_http_code"`
	Retries            int   `yaml:"retries"`
	Delay              int   `yaml:"delay"`
	ExponentialBackoff *bool `yaml:"exponential_backoff"`
}

type StopAllTasks struct {
	OnRuntimeError bool  `yaml:"on_runtime_error"`
	OnHTTPCode     []int `yaml:"on_http_code"`
}

func (r *Retry) UnmarshalYAML(value *yaml.Node) error {
	type retryAlias RetryConfig
	if value.Kind == yaml.ScalarNode {
		var enabled bool
		if err := value.Decode(&enabled); err != nil {
			return err
		}
		r.Enabled = enabled
		if enabled {
			r.Config = defaultRetryConfig()
		}
		return nil
	}

	if value.Kind == yaml.MappingNode {
		var cfg retryAlias
		if err := value.Decode(&cfg); err != nil {
			return err
		}
		r.Enabled = true
		r.Config = RetryConfig(cfg)
		return nil
	}

	return nil
}

type AuthConfig struct {
	Type     string `yaml:"type"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Token    string `yaml:"token"`
	KeyName  string `yaml:"key_name"`
	KeyValue string `yaml:"key_value"`
	KeyIn    string `yaml:"key_in"`
}

type SecurityConfig struct {
	FollowRedirects   *bool    `yaml:"follow_redirects"`
	AllowInsecureHTTP bool     `yaml:"allow_insecure_http"`
	AllowSelfSigned   bool     `yaml:"allow_self_signed_cert"`
	AllowedDomains    []string `yaml:"allowed_domains"`
	AllowedFileTypes  []string `yaml:"allowed_file_types"`
}
