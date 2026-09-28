//go:build ignore

// new-language-pack.go scaffolds a bundled pack from the English baseline.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func main() {
	if err := scaffold(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func scaffold(args []string) error {
	if len(args) != 3 {
		return fmt.Errorf("usage: mise run language:new <code> <native> <name>")
	}
	code, native, name := args[0], args[1], args[2]
	if !regexp.MustCompile(`^[a-z]+(-[a-z0-9]+)?$`).MatchString(code) {
		return fmt.Errorf("invalid lowercase language code: %q", code)
	}
	if strings.TrimSpace(native) == "" || strings.TrimSpace(name) == "" {
		return fmt.Errorf("language names must not be empty")
	}
	raw, err := os.ReadFile(filepath.Join("defaults", "languages", "en.yaml"))
	if err != nil {
		return err
	}
	var body bytes.Buffer
	for _, header := range []struct{ key, value string }{{"code", code}, {"name", name}, {"native", native}} {
		quoted, _ := json.Marshal(header.value)
		fmt.Fprintf(&body, "%s: %s\n", header.key, quoted)
	}
	keys := false
	keyLine := regexp.MustCompile(`^  ([A-Za-z0-9_.-]+):`)
	for _, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		if !keys && (strings.HasPrefix(line, "code:") || strings.HasPrefix(line, "name:") || strings.HasPrefix(line, "native:")) {
			continue
		}
		if line == "keys:" {
			keys = true
		}
		if match := keyLine.FindStringSubmatch(line); keys && match != nil {
			fmt.Fprintf(&body, "  # TODO(translate): %s\n", match[1])
		}
		fmt.Fprintln(&body, line)
	}
	path := filepath.Join("defaults", "languages", code+".yaml")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(body.Bytes())
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(path)
		return fmt.Errorf("write language pack: %v; close: %v", writeErr, closeErr)
	}
	fmt.Printf("Wrote %s. Translate values, remove TODO markers, and run mise run check.\n", path)
	return nil
}
