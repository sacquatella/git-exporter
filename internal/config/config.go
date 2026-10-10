package config

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/viper"
)

type Project struct {
	Name             string `mapstructure:"name"`
	Source           string `mapstructure:"source"`
	Ref              string `mapstructure:"ref"`
	Username         string `mapstructure:"username"`
	TokenEnv         string `mapstructure:"token_env"`
	SSHKey           string `mapstructure:"ssh_key"`
	SSHPassphraseEnv string `mapstructure:"ssh_passphrase_env"`
	SiteURLTemplate  string `mapstructure:"site_url_template"`
}

type Config struct {
	Output   string    `mapstructure:"output"`
	Projects []Project `mapstructure:"projects"`
}

type Overrides struct {
	Output    string
	OutputSet bool
	Repos     []string
	Fields    map[string][]string
}

var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// IsRemote reports whether source uses URL or SCP-style syntax rather than a local path.
func IsRemote(source string) bool {
	return strings.Contains(source, "://") || (!filepath.IsAbs(source) && strings.Contains(source, ":"))
}

// relative resolves a relative path against base, leaving empty and absolute paths unchanged.
func relative(base, value string) string {
	if value == "" || filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(base, value)
}

// Load reads optional YAML configuration, resolves its paths relative to the file,
// applies CLI overrides, and validates the resulting output and project settings.
func Load(filename string, overrides Overrides) (Config, error) {
	var cfg Config
	if filename != "" {
		absolute, err := filepath.Abs(filename)
		if err != nil {
			return cfg, err
		}
		v := viper.New()
		v.SetConfigFile(absolute)
		v.SetConfigType("yaml")
		if err := v.ReadInConfig(); err != nil {
			return cfg, fmt.Errorf("reading configuration: %w", err)
		}
		if err := v.UnmarshalExact(&cfg); err != nil {
			return cfg, fmt.Errorf("invalid configuration: %w", err)
		}
		base := filepath.Dir(absolute)
		cfg.Output = relative(base, cfg.Output)
		for i := range cfg.Projects {
			p := &cfg.Projects[i]
			if !IsRemote(p.Source) {
				p.Source = relative(base, p.Source)
			}
			p.SSHKey = relative(base, p.SSHKey)
		}
	}
	indices := map[string]int{}
	for i, p := range cfg.Projects {
		if _, exists := indices[p.Name]; exists {
			return cfg, fmt.Errorf("duplicate project: %s", p.Name)
		}
		indices[p.Name] = i
	}
	cliNames := map[string]bool{}
	for _, item := range overrides.Repos {
		name, value, ok := strings.Cut(item, "=")
		if !ok || value == "" {
			return cfg, fmt.Errorf("--repo expects name=source")
		}
		if cliNames[name] {
			return cfg, fmt.Errorf("duplicate --repo: %s", name)
		}
		cliNames[name] = true
		i, exists := indices[name]
		if !exists {
			i = len(cfg.Projects)
			indices[name] = i
			cfg.Projects = append(cfg.Projects, Project{Name: name})
		}
		cfg.Projects[i].Source = value
	}
	for field, items := range overrides.Fields {
		for _, item := range items {
			name, value, ok := strings.Cut(item, "=")
			if !ok {
				return cfg, fmt.Errorf("--%s expects name=value", field)
			}
			i, exists := indices[name]
			if !exists {
				return cfg, fmt.Errorf("--%s: unknown project %s", field, name)
			}
			p := &cfg.Projects[i]
			switch field {
			case "ref":
				p.Ref = value
			case "username":
				p.Username = value
			case "token-env":
				p.TokenEnv = value
			case "ssh-key":
				p.SSHKey = value
			case "ssh-passphrase-env":
				p.SSHPassphraseEnv = value
			case "site-url-template":
				p.SiteURLTemplate = value
			default:
				return cfg, fmt.Errorf("unknown option: %s", field)
			}
		}
	}
	if overrides.OutputSet {
		cfg.Output = overrides.Output
	}
	if cfg.Output == "" {
		return cfg, fmt.Errorf("output directory required (--output or output)")
	}
	if len(cfg.Projects) == 0 {
		return cfg, fmt.Errorf("at least one project is required")
	}
	for _, p := range cfg.Projects {
		if !namePattern.MatchString(p.Name) {
			return cfg, fmt.Errorf("invalid project name: %q", p.Name)
		}
		if p.Source == "" {
			return cfg, fmt.Errorf("%s: source required", p.Name)
		}
		if strings.Contains(p.Source, "://") {
			u, err := url.Parse(p.Source)
			if err != nil {
				return cfg, fmt.Errorf("%s: invalid URL", p.Name)
			}
			if u.Scheme != "https" && u.Scheme != "ssh" {
				return cfg, fmt.Errorf("%s: only HTTPS and SSH are supported", p.Name)
			}
			if u.User != nil {
				if _, set := u.User.Password(); set {
					return cfg, fmt.Errorf("%s: use token_env instead of embedding a secret in the URL", p.Name)
				}
			}
			if u.RawQuery != "" || u.Fragment != "" {
				return cfg, fmt.Errorf("%s: query parameters and fragments are not allowed in the Git URL", p.Name)
			}
		}
	}
	return cfg, nil
}
