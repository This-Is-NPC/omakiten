# Changelog

## 0.1.0 (2026-05-05)


### Features

* **activity:** add activity logging domain and infrastructure ([578b6fe](https://github.com/This-Is-NPC/omakiten/commit/578b6fe8d14940db88d17d9aa18df0dc85d74fbe))
* **activity:** add activity tracking to core services ([d8e41b3](https://github.com/This-Is-NPC/omakiten/commit/d8e41b38e8d81c0fda1e2991eb8d88884547efb4))
* add install scripts and update README with release one-liners ([d3e4572](https://github.com/This-Is-NPC/omakiten/commit/d3e4572f0095dd8e6decaca168c73c2539cddfac))
* **agent:** add protocol-neutral agent intent layer ([b95b21c](https://github.com/This-Is-NPC/omakiten/commit/b95b21c598259dc953af9bf9df88250b85433e14))
* **agentsetup:** add Claude Desktop MCP harness setup ([ef7188d](https://github.com/This-Is-NPC/omakiten/commit/ef7188d9e60682b05cba5229a9970a1f28ec241c))
* **app:** add law, persona, skill and editor services ([5a2033f](https://github.com/This-Is-NPC/omakiten/commit/5a2033f15212b519b0ad02b38013fc5b5bcaa188))
* **cli:** add law, persona, skill and editor commands ([42c8a4e](https://github.com/This-Is-NPC/omakiten/commit/42c8a4e31d1bbb6ec9132139bea210647b26c98b))
* **cli:** add MCP call, serve, and tools commands ([fe74595](https://github.com/This-Is-NPC/omakiten/commit/fe745952495620c9175d61e82605611da5c1c536))
* **cli:** add MCP commands and activity integration ([877ebb4](https://github.com/This-Is-NPC/omakiten/commit/877ebb4f82f41efcf9675ba5f8708c6fb8039dcb))
* **config:** add markdown entity loader, frontmatter parser and refactor bundle format ([4bb0be4](https://github.com/This-Is-NPC/omakiten/commit/4bb0be418468ddae05079a3e153bda851f1b42be))
* **defaults:** migrate bundled skills, laws and personas to markdown files ([191f7f8](https://github.com/This-Is-NPC/omakiten/commit/191f7f8ac5f59bb0c68078a4a9df05800f1b41b1))
* **domain:** introduce entity models, error codes and database migration ([8ae8cc3](https://github.com/This-Is-NPC/omakiten/commit/8ae8cc33e06682a4e0c2830917953f6a56403551))
* **mcp:** add MCP adapter and stdio server ([d4a93ed](https://github.com/This-Is-NPC/omakiten/commit/d4a93eded435fa1f3abc9212b04134d8d0d2d903))
* **mcp:** add OpenCode harness support and setup command ([b1d0f57](https://github.com/This-Is-NPC/omakiten/commit/b1d0f577f02ea01b6f1a532528d7415aa5ae11e5))
* **paths:** add OMAKITEN_HOME env override for portable config and data ([e3ad6e6](https://github.com/This-Is-NPC/omakiten/commit/e3ad6e64432d272e82377dbca437d60c462faa6a))
* **sqlite:** implement entity storage with migration and repository methods ([d5416c2](https://github.com/This-Is-NPC/omakiten/commit/d5416c2104ec1c07c1a86230229f9e79b9eff515))
* **task:** add priority support to task creation ([ad2f927](https://github.com/This-Is-NPC/omakiten/commit/ad2f927986dd1a7cb1bcf4d31ac2506fc3117948))
* **tui:** add activity log integration to TUI ([4e32187](https://github.com/This-Is-NPC/omakiten/commit/4e321873c7a16b3d7b0bbb981b64c5a3c3a35423))
* **tui:** add priority, blocker, and comment badges to task cards ([aba67eb](https://github.com/This-Is-NPC/omakiten/commit/aba67eb088142a01298751a333e727223e500b78))
* **tui:** add task priority editing and blocker picker ([813cbdb](https://github.com/This-Is-NPC/omakiten/commit/813cbdb2de80efb51e3c0e01fd148936f2cb9e13))
* **tui:** integrate entity browser, task forms and token metrics ([4092cea](https://github.com/This-Is-NPC/omakiten/commit/4092ceae65e64abf4648773c03dfa46a57e5225b))
* **tui:** render entities as cards and unify panels with shared-junction grid tables ([5b37078](https://github.com/This-Is-NPC/omakiten/commit/5b37078278dddf1338f70175d37065d43786e323))
* **tui:** responsive layout, delete confirmation, and contextual help ([863338d](https://github.com/This-Is-NPC/omakiten/commit/863338d6ca6bf62061edfe4310d29b47e6ecc222))


### Bug Fixes

* **gitignore:** anchor okt binary pattern so cmd/okt entrypoint is tracked ([6ed500f](https://github.com/This-Is-NPC/omakiten/commit/6ed500fc8536dde6c707ee44f2519892ddbb182d))
* **mcp:** remove bucket_key from create task schema ([623d1e2](https://github.com/This-Is-NPC/omakiten/commit/623d1e2bc969e36af9886a0cb3b9bb33d9db6012))
* **release:** force initial release to v0.1.0 ([#5](https://github.com/This-Is-NPC/omakiten/issues/5)) ([ec3dd29](https://github.com/This-Is-NPC/omakiten/commit/ec3dd29236763d1f055cfa6854b6ee715e8035f2))
* **release:** pin first version to 0.1.0 + add PR CI ([#4](https://github.com/This-Is-NPC/omakiten/issues/4)) ([dfe51d8](https://github.com/This-Is-NPC/omakiten/commit/dfe51d8904d0cb944e5ee50c0a25e6ec436f9a21))
* **sqlite:** default new tasks to first workflow bucket instead of hardcoded backlog ([8a1292c](https://github.com/This-Is-NPC/omakiten/commit/8a1292cc42240efe13cd928c33dae1eb48bc9636))
