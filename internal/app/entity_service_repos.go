package app

// EntityServiceRepos bundles the three ports SkillService, LawService,
// and PersonaService all compose. Callers used to thread the same
// editor / file-writer / slugger triple into four constructors;
// aggregating here is purely a wiring convenience that keeps the
// hexagonal-port shape (each field is the narrow surface the service
// actually depends on). Mirrors ContextRepoSet's W8 pattern.
//
// LawService's registry stays outside the aggregate — only the law
// surface needs it, and the registry lifecycle is tied to the
// per-project Snapshot rather than the editor/file/slugger triple.
type EntityServiceRepos struct {
	Editor  *BundleEditor
	Files   EntityFileWriter
	Slugger Slugifier
}
