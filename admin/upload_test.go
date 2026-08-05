package admin

import (
	"bytes"
	"database/sql"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A multipart filename is whatever the client felt like sending, and a store
// that joins it onto a folder writes where it was told to.
func TestSafeFilename(t *testing.T) {
	tests := map[string]string{
		"portrait.png":              "portrait.png",
		"../../etc/cron.d/backdoor": "backdoor",
		"/etc/passwd":               "passwd",
		`..\..\windows\system32\a`:  "a",
		"a b c.txt":                 "a-b-c.txt",
		"..":                        "",
		"/":                         "",
		"":                          "",
		"héllo.png":                 "h-llo.png",
	}

	for input, want := range tests {
		if got := safeFilename(input); got != want {
			t.Errorf("safeFilename(%q) = %q, want %q", input, got, want)
		}
	}
}

// uploads through a file field reach the uploader, and what it returns is what
// lands in the column.
func TestFileFieldWritesWhatTheUploaderReturns(t *testing.T) {
	eachDatabase(t, func(t *testing.T, db *sql.DB, driver string) {
		seed(t, db, driver)

		var (
			gotField string
			gotName  string
		)

		panel := New(Config{
			DB: db, Driver: driver, Authorizer: AllowAll,
			Uploads: UploaderFunc(func(ctx Context, field string, file multipart.File, header *multipart.FileHeader) (string, error) {
				gotField = field
				gotName = header.Filename
				return "uploads/stored-key.png", nil
			}),
		})
		panel.Register(Resource{
			Model: Article{}, Table: "articles",
			FieldOverrides: map[string]Field{
				"body": {Label: "Cover", Kind: KindFile},
			},
		})
		h := panel.Handler("/admin")

		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		form.WriteField("title", "With a cover")
		part, err := form.CreateFormFile("body", "cover.png")
		if err != nil {
			t.Fatal(err)
		}
		part.Write([]byte("not really a png"))
		form.Close()

		r := httptest.NewRequest("POST", "/r/articles/1", &body)
		r.Header.Set("Content-Type", form.FormDataContentType())
		r.Header.Set("Sec-Fetch-Site", "same-origin")

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("%d\n%s", rec.Code, rec.Body.String())
		}
		if gotField != "body" || gotName != "cover.png" {
			t.Fatalf("the uploader got field=%q name=%q", gotField, gotName)
		}

		var stored string
		if err := db.QueryRow(`SELECT body FROM articles WHERE id = 1`).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if stored != "uploads/stored-key.png" {
			t.Fatalf("the column holds %q, want what the uploader returned", stored)
		}
	})
}

// Submitting the form without choosing a new file must not blank the column.
func TestAnEmptyFileFieldLeavesTheValueAlone(t *testing.T) {
	eachDatabase(t, func(t *testing.T, db *sql.DB, driver string) {
		seed(t, db, driver)

		panel := New(Config{
			DB: db, Driver: driver, Authorizer: AllowAll,
			Uploads: UploaderFunc(func(Context, string, multipart.File, *multipart.FileHeader) (string, error) {
				t.Fatal("the uploader was called for an empty file field")
				return "", nil
			}),
		})
		panel.Register(Resource{
			Model: Article{}, Table: "articles",
			FieldOverrides: map[string]Field{"body": {Label: "Cover", Kind: KindFile}},
		})
		h := panel.Handler("/admin")

		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		form.WriteField("title", "Unchanged cover")
		form.Close()

		r := httptest.NewRequest("POST", "/r/articles/1", &body)
		r.Header.Set("Content-Type", form.FormDataContentType())
		r.Header.Set("Sec-Fetch-Site", "same-origin")

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("%d\n%s", rec.Code, rec.Body.String())
		}

		var stored string
		if err := db.QueryRow(`SELECT body FROM articles WHERE id = 1`).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stored, "body of First post") {
			t.Fatalf("the column was overwritten with %q", stored)
		}
	})
}

func TestFileStoreRejectsWhatItWasNotAskedToAccept(t *testing.T) {
	store := FileStore{FS: nil, AllowedExtensions: []string{".png"}}

	if _, err := store.Upload(Context{}, "cover", nil, &multipart.FileHeader{Filename: "x.png"}); err == nil {
		t.Error("uploading with no filesystem configured did not fail")
	}
}
