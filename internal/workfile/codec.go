package workfile

import (
	"bytes"
	"fmt"
	"io"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
	"omakiten/internal/config"
	"omakiten/internal/domain"
)

const MaxBytes = 16 << 20

// Decode reads one UTF-8 Markdown document with an OKF frontmatter mapping.
func Decode(reader io.Reader) (domain.WorkDocument, error) {
	var doc domain.WorkDocument
	data, err := Read(reader)
	if err != nil {
		return doc, err
	}
	header, body, err := config.SplitFrontmatter(data)
	if err != nil {
		return doc, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(header))
	if err := decoder.Decode(&doc); err != nil {
		return doc, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return doc, fmt.Errorf("frontmatter must contain one YAML document")
	}
	doc.Body = string(body)
	if doc.Type == "Omakiten Task" && doc.Spec.Task != nil {
		doc.Spec.Task.Title, doc.Spec.Task.Description = doc.Title, doc.Body
	}
	return doc, nil
}

// Parse decodes one document and reports every syntax failure as a
// validation error.
func Parse(reader io.Reader) (domain.WorkDocument, error) {
	doc, err := Decode(reader)
	if err != nil {
		return doc, domain.NewError(domain.ErrValidation, err.Error(), nil)
	}
	return doc, nil
}

// Read bounds document input and rejects invalid UTF-8.
func Read(reader io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxBytes || !utf8.Valid(data) {
		return nil, fmt.Errorf("document must be UTF-8 and at most %d bytes", MaxBytes)
	}
	return data, nil
}

// Encode emits a single document with deterministic YAML and Markdown content.
func Encode(doc domain.WorkDocument) ([]byte, error) {
	if doc.Spec.Task != nil {
		task := *doc.Spec.Task
		doc.Title, doc.Body = task.Title, task.Description
		task.Title, task.Description = "", ""
		doc.Spec.Task = &task
	}
	header, err := yaml.Marshal(doc)
	if err != nil {
		return nil, err
	}
	return config.JoinFrontmatter(header, []byte(doc.Body)), nil
}
