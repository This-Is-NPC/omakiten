---
name: Walking skeleton first
severity: warning
---
Build the thin end-to-end slice — input to output, demoable — before adding depth or polish to any single piece. Wire the whole path so its shape is observable, then flesh out the parts that matter. Half a feature in isolation is worse than a thin slice of the whole. See the tracer-bullet practice in Hunt & Thomas, *The Pragmatic Programmer*, and the "walking skeleton" definition in Cockburn, *Crystal Clear*.

Bad: polished one component in isolation with no end-to-end path wired; nothing demoable at the end.
Good: a thin input→processing→output path returning canned data first; iterated on the load-bearing parts once the whole shape was visible.
