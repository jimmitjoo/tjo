package main

import "strings"

func doMail(arg3 string) error {
	rootPath := getRootPath()
	var pathBuilder strings.Builder
	lowerArg := strings.ToLower(arg3)

	// Build htmlMail path
	pathBuilder.WriteString(rootPath)
	pathBuilder.WriteString("/email/")
	pathBuilder.WriteString(lowerArg)
	pathBuilder.WriteString(".html.tmpl")
	htmlMail := pathBuilder.String()

	// Build plainTextMail path
	pathBuilder.Reset()
	pathBuilder.WriteString(rootPath)
	pathBuilder.WriteString("/email/")
	pathBuilder.WriteString(lowerArg)
	pathBuilder.WriteString(".plain.tmpl")
	plainTextMail := pathBuilder.String()

	err := copyFileFromTemplate("templates/email/html.tmpl.txt", htmlMail)
	if err != nil {
		return err
	}

	err = copyFileFromTemplate("templates/email/plain.tmpl.txt", plainTextMail)
	if err != nil {
		return err
	}

	return nil
}
