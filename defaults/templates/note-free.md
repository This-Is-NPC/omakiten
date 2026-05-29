---
name: Note — Free
description: Minimal note shell — title, body, footer. Structure only; tone lives in persona/skill.
entity: note
laws:
  - template-fidelity
---
# {{.Title}}

{{.Body}}

---
{{if .Kind}}_kind: {{.Kind}}_{{end}}{{if and .Kind .Timestamp}} · {{end}}{{if .Timestamp}}_{{.Timestamp}}_{{end}}{{if and (or .Kind .Timestamp) .AuthorModel}} · {{end}}{{if .AuthorModel}}_author: {{.AuthorModel}}_{{end}}
