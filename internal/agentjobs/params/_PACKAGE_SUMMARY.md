# Package: `internal/agentjobs/params`

## Role

Job parameters (ADR 0x044): the `{{name}}` placeholders of a job prompt, their typed definitions, the owner's values, and the prompt rendered for a run.

## Responsibilities

- **`Placeholders`:** the distinct placeholder names in a prompt, in order of first use.
- **`Prepare`:** normalises definitions and values and checks them against the prompt.
  It returns a `*ValidationError` listing undefined placeholders and every definition or value problem at once.
  A definition no placeholder uses is fine.
  Values for unknown names and select values with an option no longer offered are dropped, not rejected.
- **`Missing` / `IsSet`:** which parameters have no usable value.
- **`Render`:** the prompt with each defined placeholder replaced; a `*MissingError` if any value is missing.
- **`CarryOverValues`:** the values still valid when a job's definitions are replaced by a newer version (a shared job's upgrade): same name, same type, still usable.
- **`StatusAfterSave`:** the status a job gets when it is saved with or without missing values.

## Dependencies

- **Inbound:** `internal/datastore`, `internal/agentjobs/scheduler`, `internal/agent`, `internal/handlers/agentjob`, and the private overlay's Nexus job install.
- **Outbound:** `internal/modeltypes` (the stored types), `internal/models` (job status).

## Non-obvious decisions

- Pure (no I/O), so the API, the agent tools, the scheduler and the Nexus install share one set of rules.
- A multi-value parameter renders in its definition's `format`: `comma` (default for `multi_select`) or `bullets` (default for `list`).
  A bullet list that replaces a placeholder mid-line is moved onto its own lines.
- A placeholder with no definition renders as written, so prompts saved before parameters existed run unchanged.
- A `multi_select` value that picks any option no longer offered is dropped whole, so a stale partial selection never runs.

## Testing

- `params_test.go` — placeholders, validation problems, normalisation, value dropping, rendering in both formats, JSON shape of values, carry-over on upgrade, status rule.

## Related documentation

- [ADR 0x044](../../../docs/adr/0x044-job-parameters.md); [`internal/agentjobs/scheduler`](../scheduler/_PACKAGE_SUMMARY.md).
