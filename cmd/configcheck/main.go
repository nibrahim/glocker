// configcheck parses a YAML file against glocker's Config struct in strict
// mode (unknown keys are errors), runs the same validator the daemon uses,
// and lists any top-level sections the YAML left at zero value. It's a dev
// helper for verifying that sample/live configs stay aligned with the struct
// after schema changes — not installed by `glocker -install`.
//
// Usage: go run ./cmd/configcheck <path-to-yaml>
// Exit codes: 0 ok, 1 parse/validate failure, 2 usage/I/O error.
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"

	"glocker/internal/config"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: configcheck <path-to-yaml>")
		os.Exit(2)
	}
	path := os.Args[1]
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read %s: %v\n", path, err)
		os.Exit(2)
	}

	// Expand any !include directives before the strict decode, so the check
	// covers the fully-assembled config (including conf.d/ list files).
	resolved, err := config.ResolveIncludesBytes(data, filepath.Dir(path))
	if err != nil {
		fmt.Printf("[PARSE] %s: %v\n", path, err)
		os.Exit(1)
	}

	var cfg config.Config
	dec := yaml.NewDecoder(bytes.NewReader(resolved))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		fmt.Printf("[PARSE] %s: %v\n", path, err)
		os.Exit(1)
	}
	fmt.Printf("[PARSE] %s: ok\n", path)

	if err := config.ValidateConfig(&cfg); err != nil {
		fmt.Printf("[VALIDATE] %s: %v\n", path, err)
		os.Exit(1)
	}
	fmt.Printf("[VALIDATE] %s: ok\n", path)

	reportEmpty(reflect.ValueOf(cfg))
}

// reportEmpty lists top-level YAML-tagged fields the file left at their
// zero value. Helps catch sample/live drift: a brand-new struct field with
// no presence in the YAML shows up here.
func reportEmpty(v reflect.Value) {
	t := v.Type()
	for i := range v.NumField() {
		f := t.Field(i)
		yamlTag := strings.Split(f.Tag.Get("yaml"), ",")[0]
		if yamlTag == "" || yamlTag == "-" {
			continue
		}
		if v.Field(i).IsZero() {
			fmt.Printf("[EMPTY-FIELD] %s (struct field %s) at zero value\n", yamlTag, f.Name)
		}
	}
}
