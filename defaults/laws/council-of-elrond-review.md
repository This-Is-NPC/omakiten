---
name: Council of Elrond review
severity: error
---
No change is promoted on the author's word alone. A second reviewer who did not write the code must read the whole diff and record a verdict — approve, or approve-with-findings, or reject — in a `#peer-review` comment that names the scope reviewed. The reviewer reads for structure and security, not only the lines that changed, because a defect's blast radius is rarely confined to the edited region (see Fagan, "Design and Code Inspections", IBM Systems Journal, 1976). A lone author cannot see their own blind spots; the review is the gate, not a courtesy.
