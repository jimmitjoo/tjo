package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/fatih/color"
	"golang.org/x/text/language"
)

// doLocale creates a catalogue stub for a language, filled with the keys this
// project actually uses.
//
// Adding a language should be a command rather than an archaeology exercise.
// The alternative is grepping the project for every t("...") by hand, which is
// how a locale ends up missing the six keys nobody thought of.
func doLocale(tag string) error {
	if tag == "" {
		return errors.New("locale needs a language tag: tjo make locale sv")
	}

	parsed, err := language.Parse(tag)
	if err != nil {
		return fmt.Errorf("%q is not a language tag: %w", tag, err)
	}

	keys, err := usedKeys(".")
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return errors.New("no translation keys found: use t(\"key\") in views or i18n.From(ctx).T(\"key\") in handlers")
	}

	dir := filepath.Join(".", "locales")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	path := filepath.Join(dir, parsed.String()+".json")

	// Merge rather than overwrite: re-running after adding a screen should add
	// the new keys and keep the translations that exist. A generator that
	// clobbered a translator's work would be used exactly once.
	existing := map[string]any{}
	if content, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(content, &existing); err != nil {
			return fmt.Errorf("%s is not valid JSON, refusing to overwrite it: %w", path, err)
		}
	}

	added := 0
	for _, key := range keys {
		if _, ok := existing[key]; ok {
			continue
		}
		// Empty rather than the English text: a translator seeing "" knows it
		// is untranslated, and a runtime lookup that finds an empty string
		// falls back to the source language anyway.
		existing[key] = ""
		added++
	}

	content, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(content, '\n'), 0o644); err != nil {
		return err
	}

	color.Green("\tCreated %s", path)
	color.Yellow("\t%d keys in use, %d newly added", len(keys), added)
	color.Yellow("")
	color.Yellow("Load it alongside the framework's own catalogue:")
	color.Yellow("")
	color.Yellow("\t//go:embed locales/*.json")
	color.Yellow("\tvar locales embed.FS")
	color.Yellow("")
	color.Yellow("\tcatalogue := i18n.NewWithFramework(language.English)")
	color.Yellow("\tcatalogue.Load(locales, \"locales/*.json\")")
	color.Yellow("\tapp.HTTP.Router.Use(catalogue.Middleware)")

	return nil
}

// keyPattern matches the two ways a key is written: t("...") in a view, and
// .T("...") or .N("...") in Go.
var keyPattern = regexp.MustCompile(`(?:\bt\(|\bn\(|\.T\(|\.N\()\s*"([a-zA-Z0-9_.\-]+)"`)

// usedKeys walks a project for translation keys.
//
// Static extraction, deliberately: a key built at runtime from a variable
// cannot be found by any tool, and a generator that pretended otherwise would
// produce a catalogue that is quietly incomplete. Keys should be literals.
func usedKeys(root string) ([]string, error) {
	seen := map[string]bool{}

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "tmp", "dist", "locales":
				return filepath.SkipDir
			}
			return nil
		}

		switch filepath.Ext(d.Name()) {
		case ".go", ".jet", ".html", ".gohtml":
		default:
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		for _, match := range keyPattern.FindAllSubmatch(content, -1) {
			key := string(match[1])
			// A key has a dot in it. Without that rule every t(x) helper in
			// somebody's JavaScript becomes a translation key.
			if strings.Contains(key, ".") {
				seen[key] = true
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, nil
}
