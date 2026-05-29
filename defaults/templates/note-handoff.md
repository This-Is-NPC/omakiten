---
name: Note — Handoff
description: Project handoff skeleton — identity, delta since last handoff, active work, progress, decisions, blockers, next steps. Structure only; tone lives in persona/skill.
entity: note
laws:
  - template-fidelity
---
# Handoff — {{.ProjectName}}

**Project** — `{{.ProjectSlug}}`{{if .ProjectRoot}} · `{{.ProjectRoot}}`{{end}}

{{if or .WindowSince .SinceCounts .PreviousHandoffRef -}}
## Since last handoff
{{- if .WindowSince}}
- Window — {{.WindowSince}}
{{- end}}
{{- if .SinceCounts}}
- Delta — {{.SinceCounts}}
{{- end}}
{{- if .PreviousHandoffRef}}
- Previous — {{.PreviousHandoffRef}}
{{- end}}
{{end -}}

{{if or .ActiveTasks .ActivePlanWave -}}
## Active work
{{- if .ActiveTasks}}
{{.ActiveTasks}}
{{- end}}
{{- if .ActivePlanWave}}

**Active plan wave** — {{.ActivePlanWave}}
{{- end}}
{{end -}}

{{if .RecentProgress -}}
## Recent progress
{{.RecentProgress}}
{{end -}}

{{if or .DecisionsWindow .DiscardsWindow -}}
## Decisions and discards
{{- if .DecisionsWindow}}

**Decisions**
{{.DecisionsWindow}}
{{- end}}
{{- if .DiscardsWindow}}

**Discards**
{{.DiscardsWindow}}
{{- end}}
{{end -}}

{{if or .DepsUnmet .BlockerComments -}}
## Blockers
{{- if .DepsUnmet}}

**Unmet dependencies**
{{.DepsUnmet}}
{{- end}}
{{- if .BlockerComments}}

**Tagged blockers**
{{.BlockerComments}}
{{- end}}
{{end -}}

{{if .NextSteps -}}
## Next steps
{{.NextSteps}}
{{end -}}

{{if .ExtraNote -}}
## Extra context
{{.ExtraNote}}
{{end -}}

---
{{if .AuthorModel}}_author: {{.AuthorModel}}_{{end}}{{if and .AuthorModel .Timestamp}} · {{end}}{{if .Timestamp}}_{{.Timestamp}}_{{end}}
