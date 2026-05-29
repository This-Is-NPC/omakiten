---
name: Security review lens
description: OWASP-aligned review prompts for diff-level security findings — injection, authn/z, secrets, deserialisation, supply chain.
schema_version: 2
role_affinity:
  - Reviewer
---
Read every diff through the OWASP Top 10 (2021) lens. One line per finding, severity-tagged.

- **A01 — Broken Access Control**. Signal: route handler skips the authz middleware; `IsAdmin()` checked but result unused; horizontal-privilege check missing on user-owned resources.
- **A02 — Cryptographic Failures**. Signal: MD5/SHA-1 for password hashing; hardcoded IV; secrets in code; TLS verification disabled (`InsecureSkipVerify: true`).
- **A03 — Injection**. Signal: SQL built via string concat; shell command interpolating user input; HTML rendered without escaping; LDAP/NoSQL/XPath query assembly.
- **A04 — Insecure Design**. Signal: rate-limit absent on auth endpoints; password reset email contains the new password; trust-on-first-use without revocation.
- **A05 — Security Misconfiguration**. Signal: debug endpoints in production; default credentials; permissive CORS (`*` with credentials); verbose error messages leaking stack traces.
- **A06 — Vulnerable and Outdated Components**. Signal: pinned dep with a known CVE; transitive dep bump without changelog review.
- **A07 — Identification and Authentication Failures**. Signal: session id in URL; weak JWT signing (`alg: none`, HS256 with weak secret); credential stuffing without lockout.
- **A08 — Software and Data Integrity Failures**. Signal: unsigned update mechanism; trust placed in unsigned serialized data; CI/CD pipeline modified without provenance check.
- **A09 — Security Logging and Monitoring Failures**. Signal: auth failures not logged; logs contain raw passwords/tokens; no alert on privilege escalation.
- **A10 — Server-Side Request Forgery**. Signal: server fetches a URL supplied by user input without an allow-list; SSRF defence relies on string-blacklist of localhost.

Bridge: when the project has a dedicated `security-review` skill or runbook, defer to it for the deeper check — this lens is the diff-level fast pass.
