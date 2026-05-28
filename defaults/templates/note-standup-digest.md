---
name: Note — Standup Digest
description: Cross-project standup digest skeleton with per-project loop. Structure only; tone lives in persona/skill.
entity: note
laws:
  - template-fidelity
---
# Standup digest

**Window** — {{.Window}}{{if .ProjectCount}} · **Projects** — {{.ProjectCount}}{{end}}

{{range .Projects}}
## {{.Name}}

{{if .LatestHandoff -}}
**Latest handoff**
{{.LatestHandoff}}
{{end -}}

{{if .DeltaSinceHandoff -}}
**Delta since handoff** — {{.DeltaSinceHandoff}}
{{end -}}
{{end}}

---
{{if .AuthorModel}}_author: {{.AuthorModel}}_{{end}}{{if and .AuthorModel .Timestamp}} · {{end}}{{if .Timestamp}}_{{.Timestamp}}_{{end}}
