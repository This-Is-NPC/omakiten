# How to change the appearance and language

**The question:** which changes belong to the workflow, and which belong to
Omakiten for every project?

Language preferences belong to the application. A color theme is a presentation
asset in a preset. A persona theme organizes persona identity and labels.
They are separate settings.

## Choose the application languages

```bash
okt config language show
okt config language set --cli pt-br --tui en --agent "Português (Brasil)"
```

`show` lists available bundled catalogs and the preference path. CLI and TUI
use language codes. The agent preference is an output-language directive;
it does not translate a workflow repository's instruction files.

Preferences are stored in `<application config root>/preferences.yaml`:

```yaml
languages:
  cli: pt-br
  tui: en
  agent_output: "Português (Brasil)"
```

The commands work independently of workflow loading and database health.
Selecting a project or using `--config` does not redirect this file. Language
changes leave the preset unchanged.

Reset, including when the preference file is invalid:

```bash
okt config language reset
```

The language catalogs ship inside the executable. Adding a catalog is an
Omakiten contribution, described in [development](development.md#languages).

## Choose a color theme

The preset's settings select a theme:

```yaml
theme: {active: omakiten}
```

The corresponding `themes/omakiten.yaml` declares its identity and colors.
Use the complete file from your preset as the starting point. Main tokens
include background, foreground, primary, secondary, success, warning, error,
border, and highlight; category accents color event types in Logs.

Theme colors also supply Markdown presentation. They do not select personas
or determine the output language. Validate and install the modified preset
through the [workflow editing flow](how-to-customize-a-workflow.md).

## Choose a persona theme

Persona wiring can give a workflow themed roles, names, and identities. For
example, a Naruto persona theme describes roles and characters; it is not a
palette of terminal colors. Inspect `persona list`, `persona show`, and resolved
command instructions to see which persona is active for an action.

## Change a view's defaults

The preset's `config.views` holds view-specific sort, filter, and presentation
defaults. Studio and the terminal's local controls expose supported settings.
Use the complete selected module as the schema example and validate changes.
A view choice changes presentation; it does not complete, assign, or migrate
the tasks being displayed.
