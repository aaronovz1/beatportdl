// Parse workflow YAML using the repository's existing, vendored YAML parser.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

func main() {
	workflows := make(map[string]any)
	for _, path := range os.Args[1:] {
		data, err := os.ReadFile(path)
		if err != nil {
			fail(err)
		}
		var workflow map[string]any
		if err := yaml.Unmarshal(data, &workflow); err != nil {
			fail(err)
		}
		workflows[filepath.Base(path)] = workflow
	}
	if err := json.NewEncoder(os.Stdout).Encode(workflows); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
