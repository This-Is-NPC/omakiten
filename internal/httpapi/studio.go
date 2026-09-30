package httpapi

import (
	"net/http"
	"sort"

	"omakiten/internal/config"
	"omakiten/internal/domain"
	"omakiten/internal/settingsprojection"
)

// StudioResponse is what the TUI Studio and Settings show of a project's
// configuration: the workflow and its guards, the agent wiring, the hooks,
// and the effective settings. It is read-only.
type StudioResponse struct {
	Kit      string         `json:"kit"`
	Workflow StudioWorkflow `json:"workflow"`
	// Subtask is the sub-task workflow when its guards differ from the root's.
	Subtask   *StudioWorkflow  `json:"subtask,omitempty"`
	Commands  []StudioCommand  `json:"commands"`
	Personas  []StudioPersona  `json:"personas"`
	Laws      []StudioLaw      `json:"laws"`
	Skills    []StudioSkill    `json:"skills"`
	Templates []StudioTemplate `json:"templates"`
	Hooks     []StudioHook     `json:"hooks"`
	Settings  []StudioSection  `json:"settings"`
	Warnings  []string         `json:"warnings,omitempty"`
}

type StudioWorkflow struct {
	Key         string             `json:"key"`
	Name        string             `json:"name"`
	Buckets     []StudioBucket     `json:"buckets"`
	Transitions []StudioTransition `json:"transitions"`
}

type StudioBucket struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Position int    `json:"position"`
	Final    bool   `json:"final,omitempty"`
}

type StudioTransition struct {
	From   string                   `json:"from"`
	To     string                   `json:"to"`
	Guards []domain.TransitionGuard `json:"guards"`
}

type StudioCommand struct {
	Key          string   `json:"key"`
	Persona      string   `json:"persona,omitempty"`
	Skills       []string `json:"skills,omitempty"`
	Laws         []string `json:"laws,omitempty"`
	LawsDisabled []string `json:"laws_disabled,omitempty"`
	Templates    []string `json:"templates,omitempty"`
}

type StudioPersona struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Skills      []string `json:"skills,omitempty"`
	Laws        []string `json:"laws,omitempty"`
	Body        string   `json:"body,omitempty"`
}

type StudioLaw struct {
	Slug     string `json:"slug"`
	Name     string `json:"name,omitempty"`
	Severity string `json:"severity"`
	Scope    string `json:"scope,omitempty"`
	Body     string `json:"body,omitempty"`
}

type StudioSkill struct {
	Slug         string   `json:"slug"`
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	RoleAffinity []string `json:"role_affinity,omitempty"`
	Body         string   `json:"body,omitempty"`
}

type StudioTemplate struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Entity      string   `json:"entity,omitempty"`
	Laws        []string `json:"laws,omitempty"`
	Body        string   `json:"body,omitempty"`
}

type StudioHook struct {
	On           string            `json:"on"`
	When         map[string]string `json:"when,omitempty"`
	Do           string            `json:"do,omitempty"`
	Notification string            `json:"notification,omitempty"`
	Message      string            `json:"message,omitempty"`
}

type StudioSection struct {
	Name    string          `json:"name"`
	Entries []StudioSetting `json:"entries"`
}

type StudioSetting struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Source string `json:"source,omitempty"`
}

func (s *Server) knowledge(r *http.Request) (domain.KnowledgeSnapshot, error) {
	return s.opts.Runtimes.Knowledge(r.Context(), r.PathValue("project"))
}

func (s *Server) studio(r *http.Request) (StudioResponse, error) {
	snap, err := s.opts.Runtimes.Snapshot(r.Context(), r.PathValue("project"))
	if err != nil {
		return StudioResponse{}, err
	}
	return studioOf(snap), nil
}

func studioOf(snap *config.Snapshot) StudioResponse {
	out := StudioResponse{
		Kit:       snap.KitKey(),
		Workflow:  studioWorkflow(snap),
		Commands:  studioCommands(snap.Commands()),
		Personas:  []StudioPersona{},
		Laws:      []StudioLaw{},
		Skills:    []StudioSkill{},
		Templates: []StudioTemplate{},
		Hooks:     []StudioHook{},
		Settings:  []StudioSection{},
	}
	if sub, ok := snap.SubtaskKit(); ok && !settingsprojection.GuardMatricesEqual(snap, sub) {
		workflow := studioWorkflow(sub)
		out.Subtask = &workflow
	}
	for _, persona := range snap.Personas() {
		out.Personas = append(out.Personas, StudioPersona{Slug: persona.Slug, Name: persona.Name, Description: persona.Description, Skills: persona.SkillRepertoire, Laws: persona.Laws, Body: persona.Body})
	}
	for _, law := range snap.Laws() {
		out.Laws = append(out.Laws, StudioLaw{Slug: law.Slug, Name: law.Name, Severity: law.Severity, Scope: law.Scope, Body: law.Body})
	}
	for _, skill := range snap.Skills() {
		out.Skills = append(out.Skills, StudioSkill{Slug: skill.Slug, Name: skill.Name, Description: skill.Description, RoleAffinity: skill.RoleAffinity, Body: skill.Body})
	}
	for _, template := range snap.Templates() {
		out.Templates = append(out.Templates, StudioTemplate{Slug: template.Slug, Name: template.Name, Description: template.Description, Entity: template.Entity, Laws: template.Laws, Body: template.Body})
	}
	for _, hook := range snap.Hooks() {
		out.Hooks = append(out.Hooks, StudioHook{On: hook.On, When: hook.When, Do: hook.Do, Notification: hook.Notification, Message: hook.Message})
	}
	for _, section := range settingsprojection.EffectiveSections(snap) {
		entries := make([]StudioSetting, 0, len(section.Tuples))
		for _, tuple := range section.Tuples {
			entries = append(entries, StudioSetting{Key: tuple.Key, Value: tuple.Value, Source: tuple.Source})
		}
		out.Settings = append(out.Settings, StudioSection{Name: section.Name, Entries: entries})
	}
	for _, warning := range snap.Warnings() {
		out.Warnings = append(out.Warnings, warning.Message)
	}
	return out
}

func studioWorkflow(snap *config.Snapshot) StudioWorkflow {
	workflow := snap.Workflow()
	out := StudioWorkflow{Key: workflow.Key, Name: workflow.Name, Buckets: []StudioBucket{}, Transitions: []StudioTransition{}}
	for _, bucket := range settingsprojection.OrderedBuckets(workflow.Buckets) {
		out.Buckets = append(out.Buckets, StudioBucket{Key: bucket.Key, Name: bucket.Name, Position: bucket.Position, Final: snap.IsFinalBucket(bucket.ID)})
	}
	for _, transition := range snap.Transitions() {
		guards := snap.Guards(transition.FromBucketID, transition.ToBucketID)
		if guards == nil {
			guards = []domain.TransitionGuard{}
		}
		out.Transitions = append(out.Transitions, StudioTransition{From: transition.FromBucketKey, To: transition.ToBucketKey, Guards: guards})
	}
	return out
}

func studioCommands(commands map[string]config.CommandSpec) []StudioCommand {
	keys := make([]string, 0, len(commands))
	for key := range commands {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]StudioCommand, 0, len(keys))
	for _, key := range keys {
		spec := commands[key]
		out = append(out, StudioCommand{Key: key, Persona: spec.Persona, Skills: spec.Skills, Laws: spec.Laws, LawsDisabled: spec.LawsDisabled, Templates: spec.Templates})
	}
	return out
}
