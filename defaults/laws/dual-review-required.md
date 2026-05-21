---
name: Dual review required
severity: error
---
High-rigor presets require two independent reviewers before promotion past `review`. Independence means neither wrote the code under review. One reviewer is a courtesy; two reviewers catch what the first one missed and break the single-point-of-failure on judgement calls.

Bad: author + one reviewer approve a payment-flow change; latent overflow ships.
Good: author + two independent reviewers sign off via `#peer-review`; second reviewer catches the overflow on a cold read.
