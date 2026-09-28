# TUI declarativa — freeze, componentes, telas

Plano para travar o visual atual, fazer os componentes da shelf serem esse visual, e só então tornar as telas 100% declarativas: a tela declara comportamento + organismos, o mount pinta.

Não é YAML, não é um framework novo, não é “tudo vira `panel.Frame`”. Também não é o invariante P1 (composição fora do render). Produção já abandonou `screenbody`; o tipo continua `func(Canvas) Block`. Declarativo aqui é **estrutura**: arquétipo + payload + teclas.

```
hoje:   Cell(spec, func(canvas) { inner; Frame; wrap; return Block })
depois: Cell(spec, framed.List(payload))   // ou Lane, Detail, Form…
```

A tela **não** pinta `┌ ├ │ ▸`, não calcula `inner = width-2`, não escolhe `PadRight`. Isso fica no organismo. `screengrid` continua o arranjo; o organismo devolve `screenlayout.Block`.

---

## Por que essa ordem

Se invertermos (telas declarativas agora), cada tela reimplementa o chrome de novo — foi o #2876. Se atualizarmos componentes sem travar o visual, os goldens viram o produto e o dono vê drift.

O freeze é o contrato: **componente novo, pixels iguais**.

A gallery (`cmd/okt-gallery`) é vitrine, não kit. Nada em produção importa `okt-gallery`. O gate `TestEveryComponentPackageHasAGalleryEntry` só exige que o *pacote* tenha uma entrada. Sem um organismo que a tela só alimenta (como Studio faz com `selectlist`), cada pass de chrome continua sendo N telas × inner / wrap / cards / indent.

Planos já fechados neste eixo (não reabrir): `tui-layout-standardization` (#168), `tui-shelf-adoption` (#173), `tui-shelf-audit-closeout` (#174), `tui-uniform-screen-assembly` (#175). Este plano começa **depois** do sign-off de #2874 / #2875 / #2876.

---

## Fase 0 — Sign-off do visual atual

#2874 (Task Detail), #2875 (Studio) e #2876 (resto Frame) em review são o visual a congelar. Sem comentário `#documentation` em cada um **e** o ok do dono de que os follow-ups de paint chegam, não há freeze.

Fora deste plano (e de #2876):

- overlay de help (`internal/tui/components/overlay/help.go`)
- form (`theme.Input` / `field`)
- plan-network (tabela custom)
- Settings Guards (`renderGuardMatrix`)
- tokens / cores novos
- Blocker picker HRule, unificação KV, help overlay — só se o dono apontar a família

Não commitar a árvore suja atual neste plano. Não reabrir #193–#196, #103, #381, nem o cluster de processo #2493–#2511.

---

## Fase 1 — Travar o visual

O lock já existe: `screentest.Record` + gallery de **telas** (`screenfixture.Drive`, o mesmo `Build` dos goldens).

Regras:

1. Pixel das telas é o produto. Mudança de golden de chrome = trabalho de produto, não refactor.
2. Extração de componente é **golden-neutra**. Se o dump da tela muda, a extração falhou.
3. Gallery de **componente** não é o lock. O demo de `panel` ainda mostra Wrap / FixedBox / HRule / Chevron, não `panel.Frame`. Atualizar esse demo é fase 2, não evidência de regressão.

Mecanismo: nenhuma tela nova, nenhum pass de “alinhar”. Commit de extração com goldens idênticos.

Critério: `go test ./internal/tui/ -count=1 -timeout 180s -run 'Golden' -vet=off` verde **sem** `-update`.

---

## Fase 2 — Os componentes passam a *ser* o visual travado

Hoje o shelf está defasado do produto:

| Organismo | Produto | Componente | Tela ainda faz |
|---|---|---|---|
| Caixa de seção | `┌` kicker `├─┤` `│…│` `└` | `panel.Frame` | Table / Plans / Logs / Stats / Graph / Home / pickers montam inner / wrap / Chrome na mão |
| Lista em caixa | Studio | `selectlist` → Block | **só Studio**; `inspector_body.go` ainda pinta `┌├└` |
| Lane | Board | `lane.Paint` → Frame | Task Detail **não** chama `lane`; envolve o board com `taskSectionFrame` |
| Card | Board / Home / entity / activity | `card` + `cardtable` | ok — não wrap de card no Frame |
| KV / Detail | Settings, Totals, description | `gridtable` | ok — **não** é Frame |
| Form | taskform | `field` | ok — fora |
| Empty | várias | `kit.Panel(...)` | placeholder, não o corpo vivo |

`screenbody` em produção: **ninguém**. Só `cmd/okt-gallery/screenbody_demo.go` e um probe.

Trabalho desta fase, nesta ordem:

1. **`panel.Frame` é o único pintor da caixa.** Task Detail apaga `taskSectionFrame` e chama `panel.Frame`. Studio `inspector_body` também. Goldens de Task Detail e Studio **não mudam**.
2. **`Frame` devolve `screenlayout.Block`.** Header / Items / Footer / Chrome. A tela deixa de repetir inner + wrap + `WrapLine`. Table, Plans, Logs, Graph, Home, pickers passam a `return framed.Paint(...)`.
3. **Gallery de `panel` mostra `Frame`**, não só Wrap / FixedBox. Senão a vitrine mente sobre o produto.
4. **Indent de tabela** (ID / SLUG / MODEL) entra no organismo da tabela com seleção, não no render da tela. Catálogo continua **sem** `// ` nessas chaves de coluna.
5. **Task Detail subtasks** usa `lane`, não um board local + frame copiado.

Critério: a mesma suíte Golden verde **sem** `-update`. Se precisar `-update`, a extração mudou o visual — parar e tratar como bug.

Não envolver card, form, help ou plan-network no Frame. Inner = `width-2`. Sem segundo `kit.Panel`, sem `PanelChrome` em cima, sem `gridtable.PadLine` no inner do Frame (sanitiza SGR).

---

## Fase 3 — Telas declarativas

P7 hoje é review (“archetype before a loose `Spec`”). Vira construtor.

Arquétipos (os do census de organismos, não um só):

| Arquétipo | Construtor | Quem já quase é |
|---|---|---|
| `ListInspector` | lista + inspector | Studio |
| `Kanban` | lanes + cards | Board |
| `FramedList` | lista em caixa | Table, Plans, Logs feed, Stats model |
| `CardGrid` | cards + Frame externo | Home, entity-list |
| `Detail` | documento | description, comment, entity, plan goal |
| `Form` | campos | taskform, comment edit |
| `Picker` | dropdown + Frame | relationshippicker, settingspicker |
| `Editorial` | Frame + `#N` interno | Insights, Resume |
| `Dashboard` | KV / Summaries | Project, Settings General |

Uma tela nova vira:

1. `ID()` e modos
2. Árvore de arquétipos (não `Spec` solto)
3. Funções de payload (domínio → `selectlist.Row` / `card.Spec` / campos)
4. Teclas (já no grid)

O arquivo da tela **não** contém `strings.Repeat("─"` nem `PadRight`.

Gate novo, pequeno: `internal/tui/screens/**` não chama `panel.Top` / `Join` / `Bottom` / `Frame` direto — só o organismo. Mesmo espírito de `screens-do-not-import-lipgloss` / `TestScreenLipglossBoundary`.

Pilot: **Studio** (já é `framedList` + `selectlist`) ou **Table** (um Cell, Frame já extraído). Não começar por Task Detail.

---

## O que este plano *não* é

- Unificar KV, form, overlay, plan-network no Frame
- Fazer a gallery ser o runtime (o chrome dela é lipgloss de propósito: `TestTheChromeDoesNotImportWhatItInspects`)
- Reviver `screenbody` para “declarativo”
- Um task-monstro “reescrever o TUI”
- Mexer em `.mise.toml`, commitar sem pedido, ou fechar #2876 com `#documentation` sem o ok do dono

---

## Como cobra

Cada onda: 1 organismo, 1 construtor, goldens iguais, `mise run --skip-tools tui:bare` no piloto, comentário `#tests-passing`.

- Fase 2 mede **zero `-update`**.
- Fase 3 mede **tela sem glifo de caixa**.
- P7 deixa de ser review quando o construtor existe e a tela não redeclara o par.

TUI ao vivo **não** hot-reload. Depois de paint: confirmar o pane (`tmux list-panes`), `C-c`, `mise run --skip-tools tui:bare`, esperar o board. `mise run tui` é install fresco de `dev_env`.

---

## Sequência no board

Depois do sign-off de #2874–#2876:

1. **Freeze** — regra + “gallery de tela é o lock, gallery de componente não”
2. **Frame is Block** — `taskSectionFrame` e `inspector_body` morrem; goldens iguais
3. **Screens call Block** — Table → Plans → Logs → resto Frame
4. **Gallery `panel` mostra Frame**
5. **Pilot declarativo** — Table ou Studio numa API `framed.List(...)`
6. **Resto por arquétipo**, um por vez, dono aponta

Sem a fase 2, a fase 3 só move o mesmo wrap para outro arquivo.

Slug sugerido se o dono pedir o plano no board: `tui-declarative-screens`. Não abrir o plano em cima da árvore suja atual.

---

## Estado de partida (census)

Todas as telas de produção já montam no `screengrid` (21 pacotes, 23 tipos, 11 modos). Gate: `TestConcreteScreengridPaths` + `TestScreenStateCensus` (0 violações). Doc normativa: `.docs/internal/tui-screen-assembly.md`.

Uso real da shelf (imports de produção, sem testes):

| Pacote | Telas |
|---|---|
| `screengrid` / `screenkit` / `screenlayout` | todas |
| `selectlist` | **só Studio** |
| `lane` | **só Board** |
| `card` | Board, Home, entity-list, project, Task Detail |
| `dropdown` / `choice` | os dois pickers |
| `field` | forms, Task Detail, comment, plan-network |
| `gridtable` | 12 telas (KV / Detail / matrix) |
| `panel` | 15 (várias chamam `Frame`; Task Detail importa `panel` só por `Borders` e copia os glifos) |
| `screenstate` | ~11 (empty / loading) |
| `screenbody` | **ninguém em produção** |

Indent de seleção já fechado nas três tabelas (Table `ID`, Plans `SLUG`, Stats `MODEL`): paint `CursorMarker(false)+" "`, catálogo sem `// `. Reabrir só se aparecer outra `│LABEL` colada na borda.

Organismos que **não** entram no Frame nesta onda: KV (`// TASKS` de propósito), Detail documento (`// SLUG` em entity inspector), Editorial (`#N // KICKER` + `kit.HRule` in-box), Form (`> // TITLE`), Overlay, Custom (plan-network, Guards), Host (`00 // HOME`), Empty (`kit.Panel`).
