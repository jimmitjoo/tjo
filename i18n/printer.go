package i18n

import (
	"fmt"
	"strings"
	"time"

	"golang.org/x/text/currency"
	"golang.org/x/text/feature/plural"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/number"
)

// Printer renders messages in one locale.
//
// Get one from a request with From(ctx). Cheap to construct, safe to share.
type Printer struct {
	catalogue *Catalogue
	tag       language.Tag
	printer   *message.Printer
}

// Printer returns a printer for a locale.
func (c *Catalogue) Printer(tag language.Tag) *Printer {
	return &Printer{
		catalogue: c,
		tag:       tag,
		printer:   message.NewPrinter(tag),
	}
}

// Tag is the locale this printer renders in.
func (p *Printer) Tag() language.Tag { return p.tag }

// T translates a key.
//
// Arguments are named and substituted into {placeholders}:
//
//	p.T("greeting.hello", "name", user.FirstName)
//
// A missing key returns the key itself. Not an empty string, and not a panic:
// a screen showing "cart.checkout" is obviously broken and still usable, which
// is what you want from a half-translated deployment.
func (p *Printer) T(key string, args ...any) string {
	message, _, ok := p.catalogue.lookup(p.tag, key)
	if !ok {
		return key
	}
	return interpolate(message.form(plural.Other), args)
}

// N translates a key with a count, choosing the plural form for the locale.
//
//	p.N("cart.items", 3, "count", 3)
//
// The count is passed twice on purpose: once to choose the form, once as a
// placeholder value. They are usually the same number and occasionally are
// not -- "3 of 10 selected" chooses its form on one of them.
func (p *Printer) N(key string, count int, args ...any) string {
	message, tag, ok := p.catalogue.lookup(p.tag, key)
	if !ok {
		return key
	}

	// The CLDR operands for an integer: i is the integer part, and the four
	// fraction operands are zero. golang.org/x/text/feature/plural is marked
	// "UNDER CONSTRUCTION" upstream, which is why it is called here and not
	// exposed: if its API changes, this is the only line that moves.
	form := plural.Cardinal.MatchPlural(tag, count, 0, 0, 0, 0)

	if len(args) == 0 {
		args = []any{"count", count}
	}
	return interpolate(message.form(form), args)
}

// Has reports whether a key exists, so a caller can fall back to something
// other than the key.
func (p *Printer) Has(key string) bool {
	_, _, ok := p.catalogue.lookup(p.tag, key)
	return ok
}

// Number formats a number for the locale: 1 234,5 in Swedish, 1,234.5 in
// English, ١٬٢٣٤٫٥ in Arabic.
func (p *Printer) Number(v any) string {
	return p.printer.Sprint(number.Decimal(v))
}

// Percent formats a fraction as a percentage.
func (p *Printer) Percent(v any) string {
	return p.printer.Sprint(number.Percent(v))
}

// Money formats an amount in a currency, in the locale's convention.
//
// code is an ISO 4217 code: "SEK", "EUR", "JPY". The number of decimal places
// is the currency's, not a guess -- yen has none and Kuwaiti dinar has three,
// and getting that wrong is a rounding error in somebody's invoice.
func (p *Printer) Money(code string, amount float64) string {
	unit, err := currency.ParseISO(code)
	if err != nil {
		return fmt.Sprintf("%s %.2f", code, amount)
	}
	return p.printer.Sprint(currency.Symbol(unit.Amount(amount)))
}

// Date formats a date for the locale.
//
// Deliberately limited to a few shapes rather than exposing a pattern
// language. x/text has no date formatter -- CLDR date patterns are not
// implemented there -- so these are hand-written per locale family, and
// pretending to offer arbitrary patterns would be pretending to a completeness
// this does not have.
func (p *Printer) Date(t time.Time) string {
	return t.Format(p.dateLayout())
}

// DateTime formats a date and a time.
func (p *Printer) DateTime(t time.Time) string {
	return t.Format(p.dateLayout() + " 15:04")
}

// dateLayout returns the date order for the locale.
//
// Three orders cover almost everyone: year-month-day, day-month-year and
// month-day-year. The last is essentially the United States.
func (p *Printer) dateLayout() string {
	base, _ := p.tag.Base()

	switch base.String() {
	case "en":
		if region, confidence := p.tag.Region(); confidence != language.No && region.String() == "US" {
			return "01/02/2006"
		}
		return "02/01/2006"
	case "sv", "lt", "ja", "zh", "ko", "hu":
		return "2006-01-02"
	case "de", "ru", "pl", "cs", "fi", "tr", "da", "nb", "no":
		return "02.01.2006"
	default:
		return "02/01/2006"
	}
}

// Direction is the writing direction of the locale.
type Direction string

const (
	LeftToRight Direction = "ltr"
	RightToLeft Direction = "rtl"
)

// Dir returns the writing direction, for the HTML dir attribute.
//
// Arabic, Hebrew, Persian and Urdu are four hundred million people, and a
// layout that does not mirror is not merely untranslated -- it is unusable.
func (p *Printer) Dir() Direction {
	base, _ := p.tag.Base()
	switch base.String() {
	case "ar", "he", "fa", "ur", "yi", "dv", "ps", "sd", "ug":
		return RightToLeft
	default:
		return LeftToRight
	}
}

// interpolate replaces {name} with the value for name.
//
// Not text/template. A catalogue is edited by translators, often through a web
// tool, and a format that can execute is a format in which a translation can do
// something other than translate.
func interpolate(text string, args []any) string {
	if len(args) == 0 || !strings.ContainsRune(text, '{') {
		return text
	}

	replacements := make([]string, 0, len(args))
	for i := 0; i+1 < len(args); i += 2 {
		name, ok := args[i].(string)
		if !ok {
			continue
		}
		replacements = append(replacements, "{"+name+"}", fmt.Sprint(args[i+1]))
	}

	if len(replacements) == 0 {
		return text
	}
	return strings.NewReplacer(replacements...).Replace(text)
}
