// Package config loads settings from the environment and a .env file.
package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// LoadDotEnv sets variables from a KEY=VALUE file without overriding the
// real environment. A missing file is not an error.
func LoadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if _, set := os.LookupEnv(k); !set {
			os.Setenv(k, v)
		}
	}
}

// Require returns the named variables, or an error naming every missing one.
func Require(keys ...string) (map[string]string, error) {
	out := map[string]string{}
	var missing []string
	for _, k := range keys {
		if out[k] = os.Getenv(k); out[k] == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing %s (see .env.example)", strings.Join(missing, ", "))
	}
	return out, nil
}
