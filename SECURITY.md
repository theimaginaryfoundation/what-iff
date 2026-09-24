# Security policy

Please do not report vulnerabilities in public issues. Until a dedicated security contact is published, contact the repository maintainers privately through the GitHub security-advisory reporting flow.

Include reproduction steps, impact, and affected versions. We will acknowledge reports and coordinate a fix before public disclosure.

## gosec baseline

CI runs `gosec -severity=medium ./...` on relevant pull requests and pushes to `main`.
The scan is initially report-only because introducing it exposed pre-existing MEDIUM+
findings in the repository. Those findings should be triaged separately rather than
making unrelated existing code block every pull request. Once that baseline is clean
or explicitly accounted for, the workflow can be made blocking by removing
`continue-on-error` from the gosec step.

The scan intentionally starts at MEDIUM severity rather than the default severity.
At default severity gosec reports 261 G104 ("errors unhandled") findings, 255 of them in
`internal/datastore` — nearly all of it one idiom, `tx.Rollback()` inside a
`recover()` block or on a `return nil, err` path, where there is nothing
meaningful to do with the rollback error because the transaction is already
being abandoned. Rewriting those call sites would make the code worse and
diverge from ent's own generated style, so they are not going to be fixed;
gating CI on them would just make G104 permanently red. `-severity=medium`
drops G104 (LOW) out of scope while keeping every MEDIUM+ rule visible.
