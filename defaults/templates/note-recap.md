---
name: Note — Recap
description: Temporal recap skeleton — window range, notes grouped by kind, tasks moved to done. Structure only; tone lives in persona/skill.
entity: note
laws:
  - template-fidelity
---
# Recap — {{.WindowSince}} → {{.WindowUntil}}

{{if .NotesByKind -}}
## Notes by kind
{{- range .NotesByKind}}

### {{.Kind}}
{{.Body}}
{{- end}}
{{end -}}

{{if .TasksDoneWindow -}}
## Tasks moved to done
{{.TasksDoneWindow}}
{{end -}}

---
{{if .AuthorModel}}_author: {{.AuthorModel}}_{{end}}{{if and .AuthorModel .Timestamp}} · {{end}}{{if .Timestamp}}_{{.Timestamp}}_{{end}}
