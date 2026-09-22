package validor

import (
	"flag"
	"path/filepath"
	"strings"
)

const defaultNamespace = "codectl"

var flagConfig = &Config{
	Namespace: defaultNamespace,
}

func init() {
	flag.BoolVar(&flagConfig.SkipDestroy, "skip-destroy", false, "Skip running terraform destroy after apply")
	flag.StringVar(&flagConfig.Exception, "exception", "", "Comma-separated list of examples to exclude")
	flag.StringVar(&flagConfig.Example, "example", "", "Specific example(s) to test (comma-separated)")
	flag.BoolVar(&flagConfig.Local, "local", false, "Use local source for testing")
	flag.StringVar(&flagConfig.Namespace, "namespace", flagConfig.Namespace, "Terraform registry namespace")
	flag.StringVar(&flagConfig.ExamplesPath, "examples-path", "", "Path to examples directory (defaults to '../examples')")
}

type Config struct {
	SkipDestroy   bool
	Exception     string
	Example       string
	Local         bool
	ExceptionList []string
	Namespace     string
	ExamplesPath  string
}

type Option func(*Config)

func WithSkipDestroy(skip bool) Option {
	return func(c *Config) { c.SkipDestroy = skip }
}

func WithException(exception string) Option {
	return func(c *Config) {
		c.Exception = exception
		c.ParseExceptionList()
	}
}

func WithExample(example string) Option {
	return func(c *Config) { c.Example = example }
}

func WithLocal(local bool) Option {
	return func(c *Config) { c.Local = local }
}

func WithExamplesPath(path string) Option {
	return func(c *Config) { c.ExamplesPath = path }
}

func WithNamespace(namespace string) Option {
	return func(c *Config) { c.Namespace = namespace }
}

func NewConfig(opts ...Option) *Config {
	config := &Config{
		Namespace: defaultNamespace,
	}
	for _, opt := range opts {
		opt(config)
	}
	return config
}

func NewConfigFromFlags() *Config {
	if !flag.Parsed() {
		flag.Parse()
	}
	flagConfig.ParseExceptionList()
	return flagConfig
}

func (c *Config) ParseExceptionList() {
	c.ExceptionList = []string{}
	c.ExceptionList = append(c.ExceptionList, parseCommaList(c.Exception)...)
}

func setupConfigWithOptions(opts ...Option) *Config {
	if len(opts) == 0 {
		return NewConfigFromFlags()
	}
	return NewConfig(opts...)
}

func getExamplesPath(config *Config) string {
	if config.ExamplesPath != "" {
		return config.ExamplesPath
	}
	return filepath.Join("..", "examples")
}

func parseExampleList(example string) []string {
	return parseCommaList(example)
}

func parseCommaList(value string) []string {
	var values []string
	for item := range strings.SplitSeq(value, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}
