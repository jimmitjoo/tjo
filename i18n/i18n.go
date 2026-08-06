// Package i18n makes an application translatable, and this framework with it.
//
// English is the first language of about 5% of the world. Until v0.13.0 this
// framework produced English on every path a user sees -- validation messages,
// the generated authentication flows, the admin panel, the ops dashboard -- and
// an application could not change any of it without forking the templates.
//
// # The decision that outlives the code
//
// The catalogue format. It is what translators are handed, what a translation
// management system imports, and the thing nobody will migrate twice. It is
// JSON, one file per locale, keyed by message key:
//
//	{
//	  "auth.invalid_credentials": "These credentials do not match our records.",
//	  "cart.items": {
//	    "one":   "{count} item",
//	    "other": "{count} items"
//	  }
//	}
//
// JSON because every translation tool reads it, because it diffs sanely in
// review, and because parsing it needs nothing that is not in the standard
// library.
//
// # Plurals are the part that is easy to get wrong
//
// English has two forms, and `if count == 1` is right in English and wrong in
// most of the world. CLDR defines six categories -- zero, one, two, few, many,
// other -- and languages use different subsets: Japanese uses one, Polish four,
// Arabic all six. A catalogue that stored {singular, plural} would be broken
// for Polish before it shipped, and would be discovered by a Polish user rather
// than by a test.
//
// So a plural message is an object keyed by CLDR category, the category is
// chosen by golang.org/x/text/feature/plural, and a message may declare
// exactly the categories its language uses.
//
// # Placeholders are not templates
//
// {name} is replaced from the arguments. It is deliberately not text/template:
// a catalogue is edited by translators, frequently through a web tool, and a
// format that can execute is a format where a translation can do something
// other than translate.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"

	"golang.org/x/text/feature/plural"
	"golang.org/x/text/language"
)

// Message is one entry in a catalogue: either a string, or a string per CLDR
// plural category.
type Message struct {
	// One is the singular form, and so on. Only the categories a language
	// actually uses need to be present.
	Zero, One, Two, Few, Many, Other string

	// simple is the whole message when it has no plural forms.
	simple string
}

// isPlural reports whether any plural form is filled in.
//
// Inferred rather than flagged, because Message is exported and a caller
// writing i18n.Message{One: "...", Other: "..."} cannot set an unexported
// bool. A struct whose zero value is subtly wrong from outside its own package
// is a struct that will be constructed wrongly.
func (m Message) isPlural() bool {
	return m.Zero != "" || m.One != "" || m.Two != "" ||
		m.Few != "" || m.Many != "" || m.Other != ""
}

// UnmarshalJSON accepts either a bare string or an object of plural forms.
func (m *Message) UnmarshalJSON(data []byte) error {
	var simple string
	if err := json.Unmarshal(data, &simple); err == nil {
		m.simple = simple
		return nil
	}

	var forms struct {
		Zero, One, Two, Few, Many, Other string
	}
	if err := json.Unmarshal(data, &forms); err != nil {
		return fmt.Errorf("i18n: a message must be a string or an object of plural forms: %w", err)
	}

	m.Zero, m.One, m.Two = forms.Zero, forms.One, forms.Two
	m.Few, m.Many, m.Other = forms.Few, forms.Many, forms.Other

	if m.Other == "" {
		// Every language has "other". A catalogue without it has a hole that
		// only shows up for the counts nobody tested.
		return fmt.Errorf(`i18n: a plural message must have an "other" form`)
	}
	return nil
}

// form returns the string for a CLDR category, falling back towards "other".
func (m Message) form(f plural.Form) string {
	if !m.isPlural() {
		return m.simple
	}

	var candidate string
	switch f {
	case plural.Zero:
		candidate = m.Zero
	case plural.One:
		candidate = m.One
	case plural.Two:
		candidate = m.Two
	case plural.Few:
		candidate = m.Few
	case plural.Many:
		candidate = m.Many
	}

	if candidate != "" {
		return candidate
	}
	// A language that does not distinguish this category, or a catalogue that
	// has not filled it in. "other" is the one form every language has.
	return m.Other
}

// Catalogue holds the messages for every loaded locale.
//
// Safe for concurrent use once loaded. Loading is not concurrent with reading:
// load at start-up.
type Catalogue struct {
	mu sync.RWMutex

	// messages is locale -> key -> message.
	messages map[language.Tag]map[string]Message

	// fallback is used when a key is missing from the negotiated locale.
	fallback language.Tag

	// matcher is rebuilt whenever a locale is added, because it has to know
	// every tag it may choose between.
	matcher language.Matcher
	tags    []language.Tag
}

// New returns an empty catalogue whose fallback locale is fallback.
//
// The fallback is what a missing translation resolves to. It should be the
// language the source strings are written in -- English, here -- so that a
// half-translated catalogue degrades to readable text rather than to keys.
func New(fallback language.Tag) *Catalogue {
	c := &Catalogue{
		messages: map[language.Tag]map[string]Message{},
		fallback: fallback,
	}
	c.add(fallback, nil)
	return c
}

// Load reads every JSON file matching pattern from fsys.
//
// The locale is taken from the file name: "en.json" is English, "pt-BR.json" is
// Brazilian Portuguese. Designed for embedding:
//
//	//go:embed locales/*.json
//	var locales embed.FS
//
//	catalogue := i18n.New(language.English)
//	err := catalogue.Load(locales, "locales/*.json")
func (c *Catalogue) Load(fsys fs.FS, pattern string) error {
	names, err := fs.Glob(fsys, pattern)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return fmt.Errorf("i18n: no catalogues matched %q", pattern)
	}

	for _, name := range names {
		base := strings.TrimSuffix(path.Base(name), path.Ext(name))

		tag, err := language.Parse(base)
		if err != nil {
			return fmt.Errorf("i18n: %s is not a language tag: %w", base, err)
		}

		content, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}

		var messages map[string]Message
		if err := json.Unmarshal(content, &messages); err != nil {
			return fmt.Errorf("i18n: %s: %w", name, err)
		}

		c.add(tag, messages)
	}

	return nil
}

// Set adds or replaces one message, for tests and for messages that are not in
// a file.
func (c *Catalogue) Set(tag language.Tag, key string, message Message) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.messages[tag] == nil {
		c.messages[tag] = map[string]Message{}
		c.appendTag(tag)
	}
	c.messages[tag][key] = message
}

// SetString adds a message with no plural forms.
func (c *Catalogue) SetString(tag language.Tag, key, message string) {
	c.Set(tag, key, Message{simple: message})
}

// Plain builds a message with no plural forms, for callers outside this
// package that need a Message value rather than a Set call.
func Plain(message string) Message { return Message{simple: message} }

func (c *Catalogue) add(tag language.Tag, messages map[string]Message) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.messages[tag] == nil {
		c.messages[tag] = map[string]Message{}
		c.appendTag(tag)
	}
	for key, message := range messages {
		c.messages[tag][key] = message
	}
}

// appendTag records a tag and rebuilds the matcher. The caller holds the lock.
func (c *Catalogue) appendTag(tag language.Tag) {
	for _, existing := range c.tags {
		if existing == tag {
			return
		}
	}

	c.tags = append(c.tags, tag)

	// The fallback must be first: language.NewMatcher treats the first tag as
	// the default, and a matcher that defaulted to whichever locale was loaded
	// first would pick a different language depending on directory order.
	sort.SliceStable(c.tags, func(i, j int) bool {
		return c.tags[i] == c.fallback && c.tags[j] != c.fallback
	})

	c.matcher = language.NewMatcher(c.tags)
}

// Locales returns the loaded locales, fallback first.
func (c *Catalogue) Locales() []language.Tag {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]language.Tag(nil), c.tags...)
}

// Match picks the best loaded locale for a set of preferences.
//
// This is matching rather than string comparison, which is the whole reason to
// use language.Matcher: "sv-FI" selects "sv" when only "sv" is loaded, and
// "pt-BR" selects "pt" rather than falling all the way to English.
func (c *Catalogue) Match(preferred ...string) language.Tag {
	c.mu.RLock()
	matcher := c.matcher
	fallback := c.fallback
	c.mu.RUnlock()

	if matcher == nil {
		return fallback
	}

	tags, _, err := language.ParseAcceptLanguage(strings.Join(preferred, ","))
	if err != nil || len(tags) == 0 {
		return fallback
	}

	_, index, confidence := matcher.Match(tags...)
	if confidence == language.No {
		return fallback
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	if index < 0 || index >= len(c.tags) {
		return fallback
	}
	return c.tags[index]
}

// lookup finds a message, falling back to the base language and then to the
// fallback locale.
//
// The chain matters: a catalogue with "pt-BR" and "en" loaded, asked for a key
// only "pt" has, should not jump straight to English.
func (c *Catalogue) lookup(tag language.Tag, key string) (Message, language.Tag, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if message, ok := c.messages[tag][key]; ok {
		return message, tag, true
	}

	if base, confidence := tag.Base(); confidence != language.No {
		baseTag := language.MustParse(base.String())
		if message, ok := c.messages[baseTag][key]; ok {
			return message, baseTag, true
		}
	}

	if message, ok := c.messages[c.fallback][key]; ok {
		return message, c.fallback, true
	}

	return Message{}, tag, false
}

//go:embed locales/*.json
var frameworkLocales embed.FS

// FrameworkLocales holds the framework's own messages, so an application
// building its own catalogue can start from them:
//
//	catalogue := i18n.New(language.English)
//	catalogue.Load(i18n.FrameworkLocales, "locales/*.json")
//	catalogue.Load(myLocales, "locales/*.json")
//
// Or, the same thing:
//
//	catalogue := i18n.NewWithFramework(language.English)
//	catalogue.Load(myLocales, "locales/*.json")
var FrameworkLocales = frameworkLocales

// NewWithFramework returns a catalogue preloaded with the framework's own
// English messages.
//
// This is what an application wants: its own translations for its own strings,
// plus working defaults for the validation messages, the admin panel and the
// ops dashboard that it did not write.
func NewWithFramework(fallback language.Tag) *Catalogue {
	c := New(fallback)
	if err := c.Load(frameworkLocales, "locales/*.json"); err != nil {
		panic("i18n: the framework's own catalogue did not load: " + err.Error())
	}
	return c
}

// init loads the framework's own English messages into the default catalogue.
//
// Shipped as the fallback rather than as the only option: an application adds
// locales/sv.json and gets Swedish for the keys it translated and English for
// the rest, which is what makes a partial translation useful rather than
// embarrassing.
//
// Only English is shipped. A framework that shipped a stale Turkish
// translation would be worse than one that shipped none, because the stale one
// silently wins over the source language.
func init() {
	if err := defaultCatalogue.Load(frameworkLocales, "locales/*.json"); err != nil {
		// The catalogue is embedded at build time, so a failure here means the
		// binary is malformed rather than misconfigured.
		panic("i18n: the framework's own catalogue did not load: " + err.Error())
	}
}
