package validor

import (
	"reflect"
	"testing"
)

func TestParseCommaList(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{"empty", "", nil},
		{"single", "example1", []string{"example1"}},
		{"multiple", "example1,example2,example3", []string{"example1", "example2", "example3"}},
		{"surrounding spaces trimmed", " example1 , example2 ", []string{"example1", "example2"}},
		{"trailing comma dropped", "example1,example2,", []string{"example1", "example2"}},
		{"empty entries dropped", "example1,,example2", []string{"example1", "example2"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseCommaList(tt.value); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseCommaList(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestConfigParseExceptionList(t *testing.T) {
	tests := []struct {
		name      string
		exception string
		want      []string
	}{
		{"empty yields empty list not nil", "", []string{}},
		{"multiple", "example1, example2,", []string{"example1", "example2"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{Exception: tt.exception}
			c.ParseExceptionList()
			if !reflect.DeepEqual(c.ExceptionList, tt.want) {
				t.Fatalf("ExceptionList = %#v, want %#v", c.ExceptionList, tt.want)
			}
		})
	}
}

func TestNewConfig(t *testing.T) {
	tests := []struct {
		name string
		opts []Option
		want Config
	}{
		{
			name: "defaults",
			want: Config{Namespace: defaultNamespace},
		},
		{
			name: "skip destroy",
			opts: []Option{WithSkipDestroy(true)},
			want: Config{Namespace: defaultNamespace, SkipDestroy: true},
		},
		{
			name: "exception is parsed",
			opts: []Option{WithException("example1,example2")},
			want: Config{Namespace: defaultNamespace, Exception: "example1,example2", ExceptionList: []string{"example1", "example2"}},
		},
		{
			name: "example",
			opts: []Option{WithExample("example1")},
			want: Config{Namespace: defaultNamespace, Example: "example1"},
		},
		{
			name: "local",
			opts: []Option{WithLocal(true)},
			want: Config{Namespace: defaultNamespace, Local: true},
		},
		{
			name: "examples path",
			opts: []Option{WithExamplesPath("/custom/path")},
			want: Config{Namespace: defaultNamespace, ExamplesPath: "/custom/path"},
		},
		{
			name: "namespace overrides default",
			opts: []Option{WithNamespace("other")},
			want: Config{Namespace: "other"},
		},
		{
			name: "options compose",
			opts: []Option{WithSkipDestroy(true), WithLocal(true), WithExample("test1"), WithExamplesPath("/path")},
			want: Config{Namespace: defaultNamespace, SkipDestroy: true, Local: true, Example: "test1", ExamplesPath: "/path"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewConfig(tt.opts...); !reflect.DeepEqual(*got, tt.want) {
				t.Fatalf("NewConfig() = %+v, want %+v", *got, tt.want)
			}
		})
	}
}

func TestSetupConfigWithOptions(t *testing.T) {
	tests := []struct {
		name string
		opts []Option
		want *Config
	}{
		{"no options uses flag config", nil, flagConfig},
		{"options build a fresh config", []Option{WithSkipDestroy(true)}, &Config{Namespace: defaultNamespace, SkipDestroy: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := setupConfigWithOptions(tt.opts...)
			if tt.want == flagConfig {
				if got != flagConfig {
					t.Fatal("expected the flag-backed config")
				}
				return
			}
			if got == flagConfig || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("setupConfigWithOptions() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestGetExamplesPath(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
		want   string
	}{
		{"custom path", &Config{ExamplesPath: "/custom/examples"}, "/custom/examples"},
		{"default path", &Config{}, "../examples"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getExamplesPath(tt.config); got != tt.want {
				t.Fatalf("getExamplesPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTestConfigOptions(t *testing.T) {
	config := &Config{Example: "test"}
	tests := []struct {
		name string
		opt  TestOption
		want TestConfig
	}{
		{"WithConfig", WithConfig(config), TestConfig{Config: config}},
		{"WithModules", WithModules([]string{"mod1", "mod2"}), TestConfig{ModuleNames: []string{"mod1", "mod2"}}},
		{"WithLocalSource", WithLocalSource(true), TestConfig{UseLocal: true}},
		{"WithParallel", WithParallel(true), TestConfig{Parallel: true}},
		{"WithTestExamplesPath", WithTestExamplesPath("/test/path"), TestConfig{ExamplesPath: "/test/path"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tc TestConfig
			tt.opt(&tc)
			if !reflect.DeepEqual(tc, tt.want) {
				t.Fatalf("got %+v, want %+v", tc, tt.want)
			}
		})
	}
}
