package tjo

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (g *Tjo) ReadJson(w http.ResponseWriter, r *http.Request, data interface{}) error {
	maxBytes := 1048576 // 1MB
	r.Body = http.MaxBytesReader(w, r.Body, int64(maxBytes))
	dec := json.NewDecoder(r.Body)
	err := dec.Decode(&data)
	if err != nil {
		return err
	}

	err = dec.Decode(&struct{}{})
	if err != io.EOF {
		return errors.New("request body must only contain a single JSON object")
	}

	return nil
}

func (g *Tjo) WriteJson(w http.ResponseWriter, status int, data interface{}, headers ...http.Header) error {
	out, err := json.MarshalIndent(data, "", "\t")
	if err != nil {
		return err
	}

	w = setHeaders(w, status, headers, "application/json")
	_, err = w.Write(out)
	if err != nil {
		return err
	}

	return nil
}

func (g *Tjo) WriteXML(w http.ResponseWriter, status int, data interface{}, headers ...http.Header) error {
	out, err := xml.MarshalIndent(data, "", "\t")
	if err != nil {
		return err
	}

	w = setHeaders(w, status, headers, "application/xml")
	_, err = w.Write(out)
	if err != nil {
		return err
	}

	return nil
}

func (g *Tjo) DownloadFile(w http.ResponseWriter, r *http.Request, pathToFile, filename string) error {
	// Validate that filename doesn't contain path traversal attempts
	if strings.Contains(filename, "..") || strings.Contains(filename, "/") || strings.Contains(filename, "\\") {
		return errors.New("invalid filename")
	}
	
	// Clean and validate the base path
	cleanPath := filepath.Clean(pathToFile)
	
	// Ensure the path is absolute
	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return err
	}
	
	// Join with filename and clean again
	fp := filepath.Join(absPath, filename)
	fileToServe := filepath.Clean(fp)
	
	// Verify the final path is still within the intended directory
	if !strings.HasPrefix(fileToServe, absPath) {
		return errors.New("invalid file path")
	}
	
	// Check if file exists and is not a directory
	fileInfo, err := os.Stat(fileToServe)
	if err != nil {
		return err
	}
	if fileInfo.IsDir() {
		return errors.New("cannot download directory")
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(fileToServe)))
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, fileToServe)

	return nil
}

func (g *Tjo) Error404(w http.ResponseWriter, r *http.Request) {
	g.ErrorStatus(w, http.StatusNotFound)
}

func (g *Tjo) Error500(w http.ResponseWriter, r *http.Request) {
	g.ErrorStatus(w, http.StatusInternalServerError)
}

func (g *Tjo) ErrorUnauthorized(w http.ResponseWriter, r *http.Request) {
	g.ErrorStatus(w, http.StatusUnauthorized)
}

func (g *Tjo) ErrorForbidden(w http.ResponseWriter, r *http.Request) {
	g.ErrorStatus(w, http.StatusForbidden)
}

func (g *Tjo) ErrorStatus(w http.ResponseWriter, status int) {
	http.Error(w, http.StatusText(status), status)
}

func setHeaders(w http.ResponseWriter, status int, headers []http.Header, contentType string) http.ResponseWriter {
	if len(headers) > 0 {
		for key, value := range headers[0] {
			w.Header()[key] = value
		}
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)

	return w
}
