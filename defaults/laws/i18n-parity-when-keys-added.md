---
name: i18n parity when keys added
severity: error
---
When a change adds an i18n key, the key lands in every locale pack, not only the source locale. A key present in one pack and missing in others is a partial translation that surfaces as a blank string or a fallback at runtime for every locale that lacks it. Add the key to all packs in the same change; a parity test enforces this, so "source locale only, translate later" is not a valid hand-back state.

Bad: a new error message added to `en.yaml` alone; users on the other 20 locales saw an empty error body until the gap was noticed weeks later.
Good: the new key added to all 21 locale packs in one commit, with a placeholder translation where the wording is pending — the parity test stays green and no locale renders blank.
