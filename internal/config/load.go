package config

// Portions adapted from MeetHub (MIT): internal/config/load.go

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// EnvPrefix prefixes every environment variable StreamGo reads.
const EnvPrefix = "STREAMGO_"

// DefaultPaths are tried in order when no --config path is given.
var DefaultPaths = []string{"config.toml", "/etc/streamgo/config.toml"}

// Load builds the configuration from defaults, the TOML file and the
// environment (os.LookupEnv), then validates it. path is the --config value;
// when empty, DefaultPaths are tried and a missing file is not an error.
func Load(path string) (*Config, error) {
	return LoadWith(path, os.LookupEnv, os.ReadFile)
}

// LoadWith is Load with injectable environment lookup and file reading, for tests.
func LoadWith(path string, lookup func(string) (string, bool), readFile func(string) ([]byte, error)) (*Config, error) {
	cfg := Default()
	data, file, err := readConfigFile(path, readFile)
	if err != nil {
		return nil, err
	}
	if data != nil {
		if err := decodeTOML(data, cfg); err != nil {
			return nil, fmt.Errorf("config file %s: %w", file, err)
		}
		if err := rejectFileSecrets(cfg); err != nil {
			return nil, fmt.Errorf("config file %s: %w", file, err)
		}
	}
	if err := applyEnv(cfg, lookup); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration:\n%w", err)
	}
	return cfg, nil
}

func readConfigFile(path string, readFile func(string) ([]byte, error)) ([]byte, string, error) {
	if path != "" {
		data, err := readFile(path)
		if err != nil {
			return nil, "", fmt.Errorf("read config file: %w", err)
		}
		return data, path, nil
	}
	for _, p := range DefaultPaths {
		data, err := readFile(p)
		if err == nil {
			return data, p, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, "", fmt.Errorf("read config file %s: %w", p, err)
		}
	}
	return nil, "", nil
}

func decodeTOML(data []byte, cfg *Config) error {
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		var strict *toml.StrictMissingError
		if errors.As(err, &strict) {
			return fmt.Errorf("unknown settings:\n%s", strict.String())
		}
		return err
	}
	return nil
}

// rejectFileSecrets fails when the TOML file set any secret field.
func rejectFileSecrets(cfg *Config) error {
	var errs []error
	walk(reflect.ValueOf(cfg).Elem(), EnvPrefix, "", func(f reflect.Value, sf reflect.StructField, env, key string) {
		if sf.Tag.Get("secret") == "true" && !f.IsZero() {
			errs = append(errs, fmt.Errorf("%s is a secret: remove it from the file and set %s in the environment", key, env))
		}
	})
	return errors.Join(errs...)
}

// applyEnv overrides fields from STREAMGO_<SECTION>_<KEY> variables.
func applyEnv(cfg *Config, lookup func(string) (string, bool)) error {
	var errs []error
	walk(reflect.ValueOf(cfg).Elem(), EnvPrefix, "", func(f reflect.Value, _ reflect.StructField, env, _ string) {
		val, ok := lookup(env)
		if !ok {
			return
		}
		if err := setValue(f, val); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", env, err))
		}
	})
	return errors.Join(errs...)
}

// walk visits every leaf field with its environment variable name and TOML key.
func walk(v reflect.Value, envPrefix, keyPrefix string, fn func(f reflect.Value, sf reflect.StructField, env, key string)) {
	t := v.Type()
	for i := range t.NumField() {
		sf := t.Field(i)
		name := strings.Split(sf.Tag.Get("toml"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		f := v.Field(i)
		env := envPrefix + strings.ToUpper(name)
		key := keyPrefix + name
		if f.Kind() == reflect.Struct {
			walk(f, env+"_", key+".", fn)
			continue
		}
		fn(f, sf, env, key)
	}
}

// Setting describes one configuration key.
type Setting struct {
	Key    string // TOML key, e.g. "server.addr"
	Env    string // environment variable, e.g. "STREAMGO_SERVER_ADDR"
	Secret bool   // environment only
}

// Settings lists every key the loader knows, in declaration order.
func Settings() []Setting {
	var out []Setting
	walk(reflect.ValueOf(Default()).Elem(), EnvPrefix, "", func(_ reflect.Value, sf reflect.StructField, env, key string) {
		out = append(out, Setting{Key: key, Env: env, Secret: sf.Tag.Get("secret") == "true"})
	})
	return out
}

var durationType = reflect.TypeFor[Duration]()

func setValue(f reflect.Value, s string) error {
	if f.Type() == durationType {
		var d Duration
		if err := d.UnmarshalText([]byte(strings.TrimSpace(s))); err != nil {
			return err
		}
		f.Set(reflect.ValueOf(d))
		return nil
	}
	switch f.Kind() {
	case reflect.String:
		f.SetString(s)
	case reflect.Int:
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return fmt.Errorf("not an integer: %q", s)
		}
		f.SetInt(int64(n))
	case reflect.Bool:
		b, err := strconv.ParseBool(strings.TrimSpace(s))
		if err != nil {
			return fmt.Errorf("not a boolean: %q", s)
		}
		f.SetBool(b)
	case reflect.Slice:
		var items []string
		for p := range strings.SplitSeq(s, ",") {
			if p = strings.TrimSpace(p); p != "" {
				items = append(items, p)
			}
		}
		f.Set(reflect.ValueOf(items))
	default:
		return fmt.Errorf("unsupported setting type %s", f.Kind())
	}
	return nil
}
