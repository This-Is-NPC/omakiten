<!-- GENERATED snapshot — hand-refreshed via `mise run mcp:prompts` until the dedicated `mcp:prompts:costs` subtask lands. Numbers move with persona body / skill / law / template bindings. -->

# Prompt Costs — omakase canonical kit

| Prompt | Bytes | ~Tokens | Drivers |
|---|---|---|---|
| `okt-document` | 2073 | 520 | documentation-agent + 5 skills + 5 laws |
| `okt-resume` | 2185 | 545 | engineer + 6 skills (TBD/CI/TDD/DORA + implementation/markdown) + 4 laws + persona body (trunk-based loop) |
| `okt` | 2201 | 550 | engineer + 6 skills + 4 laws + persona body |
| `okt-continue` | 2272 | 570 | engineer + 6 skills + 4 laws + persona body |
| `okt-config` | 2367 | 590 | documentation-agent + 5 skills + 5 laws + config-orientation metadata (JIT) |
| `okt-imagine` | 4439 | 1110 | product-owner + 10 product-discipline skills + 7 laws (template-fidelity disabled) + persona body (5W2H Discovery loop) + 2 templates metadata (JIT) |
| `okt-implement` | 6270 | 1570 | engineer + 6 skills + 12 laws (4 globals/inherited + 8 command-level TBD/CI/DORA/TDD) + persona body + 3 templates metadata (JIT) |
| `okt-create` | 7131 | 1780 | product-owner + 10 skills + 9 laws (3 globals + 6 persona frontmatter + 2 command-level) + persona body + 4 templates metadata (JIT) |

Without JIT, `okt-implement` would carry the full `pull-request` body (~700 extra tokens). Templates bound via `mcp_commands.<cmd>.templates` ship only metadata in the prompt and are fetched JIT via `templates.show`.

A regression test (`internal/agentruntime/prompt_budget_test.go`) caps each prompt at current size + ~30% headroom. Current budgets (bytes): okt 2300 · okt-imagine 4900 · okt-create 7600 · okt-resume 2300 · okt-continue 2400 · okt-implement 8200 · okt-document 2700 · okt-config 3050.
