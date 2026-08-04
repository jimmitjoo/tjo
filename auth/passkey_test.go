package auth

import (
	"errors"
	"strings"
	"testing"

	"github.com/go-webauthn/webauthn/webauthn"
)

func TestPasskeyRecordRoundTrips(t *testing.T) {
	original := &webauthn.Credential{
		ID:              []byte("credential-id-bytes"),
		PublicKey:       []byte("public-key-bytes"),
		AttestationType: "basic_full",
	}

	rec, err := EncodePasskey(original.ID, original, []string{"internal", "hybrid"})
	if err != nil {
		t.Fatal(err)
	}

	var decoded webauthn.Credential
	transports, err := DecodePasskey(rec.Record, &decoded)
	if err != nil {
		t.Fatal(err)
	}

	if string(decoded.ID) != string(original.ID) {
		t.Errorf("ID = %q", decoded.ID)
	}
	if string(decoded.PublicKey) != string(original.PublicKey) {
		t.Errorf("PublicKey = %q", decoded.PublicKey)
	}
	if decoded.AttestationType != "basic_full" {
		t.Errorf("AttestationType = %q", decoded.AttestationType)
	}

	// Sorted on encode, so the record is stable for the same credential.
	if strings.Join(transports, ",") != "hybrid,internal" {
		t.Errorf("transports = %v", transports)
	}
}

// The record is opaque to the application. This asserts the shape Valsorda's
// proposal describes, because the whole argument for it is that it is
// recognisable and parseable by something other than this package.
func TestPasskeyRecordHasTheDocumentedShape(t *testing.T) {
	rec, err := EncodePasskey([]byte("id"), &webauthn.Credential{ID: []byte("id")}, []string{"hybrid"})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(rec.Record, "$webauthn$v=1$") {
		t.Errorf("record does not start with the versioned prefix: %q", rec.Record)
	}
	if !strings.Contains(rec.Record, "$transports=hybrid$") {
		t.Errorf("transports parameter missing: %q", rec.Record)
	}

	// One line, no whitespace: it goes in a database column and into logs.
	if strings.ContainsAny(rec.Record, " \t\n") {
		t.Errorf("record contains whitespace: %q", rec.Record)
	}
}

// Encoding is stable, so the same credential always produces the same string.
// Without that, records are not comparable and a rewrite looks like a change.
func TestPasskeyEncodingIsStable(t *testing.T) {
	cred := &webauthn.Credential{ID: []byte("id"), PublicKey: []byte("pk")}

	a, err := EncodePasskey(cred.ID, cred, []string{"hybrid", "internal"})
	if err != nil {
		t.Fatal(err)
	}
	// Same transports, different order and casing, with a duplicate.
	b, err := EncodePasskey(cred.ID, cred, []string{"INTERNAL", "hybrid", "internal", " "})
	if err != nil {
		t.Fatal(err)
	}

	if a.Record != b.Record {
		t.Errorf("the same credential encoded differently:\n  %s\n  %s", a.Record, b.Record)
	}
}

// A record from a future major version must be refused rather than guessed at.
// Silently accepting one would mean interpreting a payload whose shape has
// changed.
func TestUnknownVersionIsRefused(t *testing.T) {
	var cred webauthn.Credential
	_, err := DecodePasskey("$webauthn$v=2$"+strings.Repeat("A", 8), &cred)
	if !errors.Is(err, ErrMalformedRecord) {
		t.Errorf("err = %v, want ErrMalformedRecord", err)
	}
}

// An unknown *parameter* within a known version must be ignored, not rejected.
// That is what makes the format forward-compatible rather than merely
// versioned: a record written by a newer minor version still decodes here.
func TestUnknownParametersAreIgnored(t *testing.T) {
	rec, err := EncodePasskey([]byte("id"), &webauthn.Credential{ID: []byte("id")}, []string{"hybrid"})
	if err != nil {
		t.Fatal(err)
	}

	// Splice in a parameter this version does not know about.
	withExtra := strings.Replace(rec.Record, "$transports=hybrid$", "$transports=hybrid$backupState=true$", 1)

	var decoded webauthn.Credential
	transports, err := DecodePasskey(withExtra, &decoded)
	if err != nil {
		t.Fatalf("a record with an unknown parameter was refused: %v", err)
	}
	if string(decoded.ID) != "id" {
		t.Errorf("ID = %q", decoded.ID)
	}
	if len(transports) != 1 || transports[0] != "hybrid" {
		t.Errorf("transports = %v", transports)
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	var cred webauthn.Credential

	for name, record := range map[string]string{
		"empty":            "",
		"no prefix":        "not-a-record",
		"prefix only":      "$webauthn$",
		"no payload":       "$webauthn$v=1$",
		"bad base64":       "$webauthn$v=1$!!!not-base64!!!",
		"payload not json": "$webauthn$v=1$" + "aGVsbG8",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodePasskey(record, &cred); !errors.Is(err, ErrMalformedRecord) {
				t.Errorf("DecodePasskey(%q) = %v, want ErrMalformedRecord", record, err)
			}
		})
	}
}

func TestEncodeRequiresACredentialID(t *testing.T) {
	if _, err := EncodePasskey(nil, &webauthn.Credential{}, nil); err == nil {
		t.Error("a credential with no id was encoded")
	}
}

// The library owns its own compatibility migrations, and storing its JSON
// verbatim is what lets it apply them.
//
// go-webauthn v0.17.4 treats "none" in attestationType as a legacy value --
// earlier releases stored the attestation *format* in that field -- and moves
// it to AttestationFormat on decode. A schema that decomposed the credential
// into one column per field would have frozen the old meaning in the database
// and made this migration the application's problem.
//
// This is the argument for the opaque record, expressed as a test.
func TestTheLibraryStillAppliesItsOwnMigrations(t *testing.T) {
	rec, err := EncodePasskey([]byte("id"), &webauthn.Credential{
		ID:              []byte("id"),
		AttestationType: "none", // legacy: actually a format
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	var decoded webauthn.Credential
	if _, err := DecodePasskey(rec.Record, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.AttestationFormat != "none" {
		t.Errorf("AttestationFormat = %q; the library's migration did not run", decoded.AttestationFormat)
	}
	if decoded.AttestationType != "" {
		t.Errorf("AttestationType = %q; the legacy value was not migrated out", decoded.AttestationType)
	}
}
