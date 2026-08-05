// Package docs embeds this framework's documentation so a tool can serve the
// version that is actually installed.
//
// The problem it addresses is specific: a model's idea of a framework comes
// from its training data, which is some months old and averaged over every
// version that existed then. Fetching the docs from the internet gets whatever
// is on the default branch, which is a different wrong version. Compiling them
// into the binary means `tjo mcp` answers with the documentation for the CLI
// the caller is holding.
package docs

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed *.md
var files embed.FS

// Topics returns the available document names, sorted.
func Topics() []string {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return nil
	}

	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, strings.TrimSuffix(entry.Name(), ".md"))
	}
	sort.Strings(out)
	return out
}

// Read returns one document.
//
// The name is matched against the embedded set rather than joined onto a path:
// a docs tool that took a path from a caller would be a file-read primitive
// wearing a documentation label.
func Read(topic string) (string, error) {
	topic = strings.TrimSuffix(strings.TrimSpace(topic), ".md")

	for _, known := range Topics() {
		if known == topic {
			content, err := files.ReadFile(topic + ".md")
			if err != nil {
				return "", err
			}
			return string(content), nil
		}
	}

	return "", fmt.Errorf("no such topic %q; available: %s", topic, strings.Join(Topics(), ", "))
}

// Search returns the topics whose text contains term, with the matching lines.
//
// Deliberately plain: this is a handful of markdown files, and a search that
// needs an index is a search over a corpus that should not be embedded in a
// binary.
func Search(term string) map[string][]string {
	term = strings.ToLower(strings.TrimSpace(term))
	if term == "" {
		return nil
	}

	out := map[string][]string{}

	for _, topic := range Topics() {
		content, err := Read(topic)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(content, "\n") {
			if strings.Contains(strings.ToLower(line), term) {
				out[topic] = append(out[topic], strings.TrimSpace(line))
			}
		}
	}

	return out
}
