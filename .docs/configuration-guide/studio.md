# Studio scope, edit semantics, and preview/apply flow

Studio is the TUI workbench for structured edits to the active YAML profile. It defines the information architecture, allowed edits, import behavior, reference rewrites, prompt-preview contract, and apply safety rules for `internal/tui/screens/studio`.

Source files inspected for this decision:

- `internal/config/loader.go` (`readWiringDetailed`, import expansion, `Bundle.SourcePaths`).
- `internal/config/import_resolver.go` (`from` / `merge_from` semantics and imported source tracking).
- `internal/config/bundle.go` (`Bundle`, `wiring`, `CommandSpec`, workflow structs).
- `internal/config/saver.go` (`SaveBundle`, `bundleToWiring`).
- `internal/app/bundle_editor.go` (`BundleEditor.ApplyWithFiles`).
- `internal/operation/service_command.go` (`ResolveCommand`, `renderCommandMarkdown`).
- `.docs/configuration-guide/workflows.md`, `guards.md`, `command-bindings.md`, and `views.md`.

## Information Architecture

The top-level TUI order after Studio lands is:

| Position | Top-level area | Responsibility |
|---|---|---|
| `01` | `Tasks` | Task execution surfaces: board, table, graph, plans. |
| `02` | `Stats` | Project metrics, logs, and operational inspection. |
| `03` | `Studio` | Workflow and agent playbook authoring for the active YAML profile. |
| `04` | `Settings` | Runtime preferences, entity catalogs, tags, and project-level pickers. |

Studio MVP subs, in palette order (`31`–`34`; `,` / `//` cycle these four):

| Sub | Palette | Scope |
|---|---|---|
| `Workflow` | `31` | Buckets, transitions, and guards as one list. Permissions, reorder, key edits, add/remove edges. |
| `Commands` | `32` | `commands` bindings: persona, skills, laws, laws_disabled, templates, plus composed prompt preview. |
| `Personas` | `33` | Wired persona roster and reverse command index. RELATED skill, law, command, and empty-state text is sanitized at the terminal render sink; harmless Unicode is preserved. |
| `Hooks` | `34` | Configured HookSpecs with per-index `hook.executed` history. |

Apply is not a fifth tab. `ctrl+s` on any of the four editors opens the apply overlay (`internal/tui/screens/studio/apply.go`); a second `ctrl+s` confirms `StudioDraft.Apply`. `esc` discards the overlay without mutating the candidate.

Settings remains the owner for non-Studio configuration:

| Settings sub/concept | Decision |
|---|---|
| `General` | Remains in Settings: active config/theme pickers, paths, SQLite path, runtime read-only values. It may deep-link to Studio Workflow but does not duplicate Studio editors. |
| `Laws`, `Personas`, `Skills`, `Templates` | Remain in Settings as entity catalogs and file-backed entity editors. Studio Commands only edits command wiring references to these entities. |
| `Tags` | Remains in Settings. Tags are operational metadata, not YAML authoring. |
| Workflow/bucket/guard concepts | Move to Studio. Settings should not expose write controls for workflow structure once Studio exists. |
| CLI command bindings | Move to Studio Commands. Settings may show related entity availability but should not edit `commands`. |

## Supported MVP Edits

Every Studio write runs through a draft validation/apply layer before touching disk. The draft layer produces an in-memory candidate bundle, validates it with `config.ValidateBundle`, and only then applies atomically to YAML through `BundleEditor.Apply`.

Studio writes the active profile YAML path exposed in Studio Workflow / `Repositories.ConfigPath`. It does not persist configuration to SQLite; SQLite remains operational state for tasks, comments, plans, events, tags, and metrics.

Studio hook history is operational read-only data. Its event query always carries the active `ProjectID`; selecting another project clears the cached history before the new project can render, and a history load becomes ready only after that scoped query succeeds. Delayed results from an earlier project or load generation are discarded.

Studio draft, editor, catalog, and apply state are scoped to the active project runtime. Project selection and config/runtime snapshot rotation install the new runtime before refreshing the model, increment the Studio runtime generation, and replace the Studio session; a dirty draft is therefore never rendered or applied under another project and does not return on an `A -> B -> A` visit unless Studio opens A's current source again. Command prompt previews use the same project/runtime scope for both cache entries and asynchronous success or failure results.

Supported workflow edits:

| Edit | Rules |
|---|---|
| Bucket display name | Update `workflows[].buckets[].name`; preserve bucket `id`, `key`, and position. |
| Bucket key | Update the bucket key and every key-based reference listed in [Bucket key reference updates](#bucket-key-reference-updates). Do not rewrite transitions for key-only edits because transitions are ID-based. |
| Bucket order | Update `position` values to produce a unique ordered sequence. Warn when the edit changes the final bucket because archive moves active tasks to the current final bucket. |
| Bucket permissions | Edit `permissions.task.{edit,delete}` and `permissions.comment.{create,edit,delete}` while preserving omitted vs explicit false semantics. |
| Add bucket | Generate a new bucket `id` that is greater than the max existing bucket id within the workflow. Generate a unique key from the name, set a valid position, and leave transitions empty until explicitly added. |
| Delete bucket | Allow only when active-task impact is safe. Block if any active task in the project uses the bucket id or if any transition/guard/reference would become invalid and cannot be removed explicitly in the same draft. The blocked reason must name the active task count and the affected bucket. |
| Add/remove transitions | Edit `workflows[].transitions` by bucket id. Reject duplicate `(from,to)` pairs and unknown bucket ids. |
| Transition guards | Add, edit, remove, and reorder `transitions[].guards`. Guard payloads must match `.docs/configuration-guide/guards.md`. |
| Operation guards | Add, edit, remove, and reorder `workflows[].operations.{archive,delete,unarchive}.guards`. |

Supported command edits:

| Edit | Rules |
|---|---|
| `persona` | Set or clear `commands.<name>.persona`; warn or validate using the same missing-reference behavior as the loader. |
| `skills` | Set command skill subset. For schema v2 personas, every selected skill must be in the persona `skill_repertoire`. |
| `laws` | Set command-specific added laws. |
| `laws_disabled` | Set command-specific law removals. A law must not appear in both `laws` and `laws_disabled` for the same command. |
| `templates` | Set template slugs rendered in prompt metadata. |

## Unsupported MVP Edits

Studio MVP must not support:

| Unsupported edit | Reason |
|---|---|
| Direct bucket ID editing | Bucket IDs are stored on tasks and resolved through the active workflow snapshot; changing them is outside Studio's structured edit scope. |
| Raw YAML editing inside Studio | Studio edits structured fields only. Raw YAML remains a user/editor workflow outside Studio. |
| Silent writes into imported source files | Current loader provenance is insufficient for safe write-through. See [Imported blocks](#imported-blocks). |
| Writing config to SQLite | YAML remains source of truth. SQLite remains operational data only. |

## Preview And Apply Flow

Apply is overlay-only. There is no Preview screen. `ctrl+s` on Workflow, Commands, Personas, or Hooks opens the apply overlay, which renders the current draft report from `StudioDraft.Report()` and, when a draft exists, bucket/final-bucket impact from `StudioDraft.ImpactPreview()`. Prompt preview lives on Commands via `PromptPreview` / `resolveStudioCandidateCommand`. `StudioFlowWarnings` still feed overlay warnings.

Overlay sections:

| Section | Source |
|---|---|
| Candidate state | `StudioDraftReport.Dirty`, `ValidationError`, and imported-block `BlockedReason`. |
| Warnings | Edit warnings, flow warnings from `StudioFlowWarnings`, and command missing-reference warnings from `studioCommandWarnings`. |
| Impact | Removed buckets, bucket key changes, final-bucket changes, and active task counts for affected bucket keys. |
| Diff | First line of `DiffStudioBundles(original, candidate)` structural summary. |
| Prompt preview | Commands inspector: candidate command resolution via the shared agent command composition helper. |

Apply controls:

- `ctrl+s` on Workflow, Commands, Personas, or Hooks opens the overlay and is the only `StudioDraft.Apply` call site (`applyStudioCandidate`).
- Apply is disabled when validation fails or imported-block editing is blocked.
- The overlay requires an explicit second `ctrl+s` when bucket removal, bucket key changes, or final-bucket changes produce impact warnings (and always confirms once before writing).
- Successful apply clears dirty state by replacing the draft original/candidate with the applied bundle and reloads the runtime bundle cache through `reloadBundle`.
- Project selection and runtime/config reload discard the existing draft and apply confirmation before the new editor, catalog, and snapshot are bound. Same-project editing keeps its draft between normal Studio tab binds; only a project or runtime-generation change starts a new session.
- If the YAML write succeeds but runtime reload fails, the already-published file remains in place and the result is reported as published but unverified; fix the file and retry the reload. BundleEditor carries the caller's coherent planning hashes into apply and rejects a stale wiring or entity-file content version before that file is published or removed. FileOps are relative to the current bundle root and reject absolute, parent-traversal, outside-root, and symlink-escaping paths before publication. A write/delete durability error after publication is reported as ambiguous, naming the affected path and instructing the user to reload, inspect, retry, or repair it. Independent multi-file failures identify the files already published and the affected file.

Manual smoke path for Studio-specific changes:

1. Open `okt tui` in a registered project.
2. Navigate to `03 // Studio`.
3. Change a bucket permission or transition on `Studio › Workflow`.
4. Press `ctrl+s` to open the apply overlay; confirm dirty state plus the diff and warnings.
5. Press `ctrl+s` again to apply; if an impact warning appears, verify it and press `ctrl+s` again only when expected. `esc` discards without writing.
6. Confirm the active YAML profile changed and the TUI/runtime reload reflects the new value.
7. Restore the test config change.
8. Change a command persona/skill/template selection in `Studio › Commands` and confirm the prompt preview in the inspector reflects the candidate binding before apply.

## Imported Blocks

Current behavior:

| Area | Representation on disk | Representation in `config.Bundle` |
|---|---|---|
| `workflows` | May be inline, `from: ./file.yaml`, or use nested `merge_from`. | Imports are expanded before strict decode; `Bundle.Workflows` contains only the resolved values. |
| `personas` wiring | May be inline or imported as YAML wiring. Persona bodies remain separate Markdown files. | `Bundle.Personas` contains picked resolved personas with wiring applied; import provenance for each wiring entry is not retained. |
| `commands` | May be inline, `from: ./file.yaml`, or use nested `merge_from`. | `Bundle.Commands` is a resolved map; per-entry import provenance is not retained. |

`LoadBundle` records only `Bundle.SourcePaths`: the active profile plus every imported file in first-encounter order. `SaveBundle` writes the resolved `Bundle` back to the active profile path by converting it to the canonical `wiring` struct. `BundleEditor.ApplyWithFiles` writes that same active profile path atomically. There is no field-level source map for imported workflow, persona wiring, or command entries.

MVP decision: Studio blocks edits to an imported target block. Write-through to imported files is not part of the MVP.

Blocked status message downstream tasks must use:

```text
Studio cannot edit this section because it is imported from <path>. Edit the owning YAML file directly or inline the section in the active profile, then reload Studio.
```

Detection requirement for downstream implementation:

- If the top-level target block in the active profile is a value-level `from:` import, block all Studio edits for that block.
- If a target subtree is introduced by `merge_from`, block edits to imported keys unless the draft layer can prove the edited key is explicitly overridden inline in the active profile.
- When provenance cannot be proven, choose the blocked state rather than writing to the active profile and flattening imports.
- Tests must cover imported `workflows`, imported `commands`, and imported `personas` wiring before enabling write-capable editors for those areas.

This keeps imported-block behavior settled without silently flattening modular config or writing to the wrong file.

## Bucket Key Reference Updates

Bucket key edits are allowed, but they must update every key-based reference in the active candidate bundle.

Required rewrites:

- `workflows[].buckets[].key`: replace the edited bucket key.
- `workflows[].transitions[].guards[].buckets` for guards of type `blockers_in`: replace matching bucket keys.
- `workflows[].operations.*.guards[].buckets` for guards of type `blockers_in`: replace matching bucket keys.
- `config.views.table.filter.bucket`: replace matching bucket keys when present.

Do not rewrite:

- `workflows[].transitions[].from` or `to`. Transitions are bucket-ID based.
- `tasks.bucket_id`. Tasks store bucket IDs, not bucket keys.
- Historical events, comments, or logs. They are operational records, not config references.

Validation requirements:

- After the rewrite, validate the candidate with `validateWorkflows`, `validateViewSettings`, and the normal `LoadBundle`/`ValidateBundle` path.
- Reject key edits that create duplicate bucket keys in the workflow.
- If any unhandled YAML path in the active candidate references the old key, block and surface the path rather than partially writing.

## Prompt Preview

Studio Commands must use the real command composition rules from `internal/operation/service_command.go` or a candidate-bundle equivalent that produces identical structured inputs.

Prompt-preview requirements:

- Start from the candidate bundle, not the last persisted snapshot, when there are unapplied Studio changes.
- Resolve the command playbook from the bound `okt-<slug>-playbook` skill.
- Include persona body when `commands.<name>.persona` resolves.
- Include command-selected skills, falling back exactly like `ResolveCommand`: command `skills`, then persona `skill_repertoire`.
- Include effective laws as `global laws + persona laws + command laws + template laws - laws_disabled`, deduped in first-seen order.
- Include templates using the same metadata rendering as `renderCommandMarkdown`.
- Include the entity-sourced playbook in `## Skills` and the configured agent output language suffix when present; there is no separate `## Action` section.
- Surface missing references as candidate warnings consistent with loader warnings; do not silently omit them without visibility.

Implemented shape:

- `agent.ResolveCommandFromCatalog` is the pure resolver shared by runtime `ResolveCommand` and Studio candidate prompt preview.
- `resolveStudioCandidateCommand` projects candidate bundle entities into agent catalogs and passes candidate `commands` plus `config.languages.agent_output` to that shared resolver.
- `TestStudioPreviewRendersCandidatePrompt` covers persona, skills, inherited/template laws, `laws_disabled` subtraction, templates, and output language rendering from candidate state on Commands; agent resolver tests cover entity-sourced playbook composition.

## Risks and Mitigations

| Risk | Mitigation |
|---|---|
| Flattening imported config by saving the resolved bundle to the active profile. | Block imported target blocks until field-level provenance and write-through are implemented. |
| Corrupting active tasks by editing bucket IDs. | Do not support direct ID editing in MVP; generated IDs only for new buckets. |
| Breaking guard/view references during bucket key edits. | Rewrite all documented key references in the candidate and validate before apply. |
| Confusing Settings/Studio split. | Studio owns workflow and CLI command authoring; Settings owns runtime preferences and entity catalogs. |
| Preview drift from real agent playbooks. | Reuse `ResolveCommand` semantics or enforce parity tests against it. |
| SQLite accidentally becoming config storage. | Keep Studio writes file-backed only; SQLite remains operational data only. |

## Downstream Acceptance Notes

Before any Studio write-capable task is accepted, it must cite this document and prove:

- Draft validation runs before disk writes.
- Imported targets show the exact blocked message above.
- Bucket key edits update the documented key references and leave ID-based transitions untouched.
- Prompt preview output is derived from the same composition rules as agent playbooks.
