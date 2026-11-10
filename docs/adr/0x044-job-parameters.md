# ADR 0x044: Job parameters

- **Status:** Accepted
- **Date:** 2026-11-08
- **Deciders:** What Iff maintainers
- **Issue:** none (raised in conversation)
- **PR:** see the pull request that adds this file

## Context

A job is a prompt on a schedule.
Everything that varies between two people who want "the same" job lives inside the prompt text: the topics a news job searches, the tickers a market job watches, the tone of a summary.
That was fine while a job was something you wrote for yourself.
Now that jobs can be shared, it is the main thing in the way.
Someone who installs a shared news job gets the publisher's topics hard-coded in a paragraph of prose, and has to find and rewrite them by hand.

What the installer actually needs is a short form: "Topics: ___", "Region: (US / EU / Global)".
The publisher needs a way to mark which parts of the prompt are those inputs, and what kind of input each one is.

## Decision

### 1. Placeholders in the prompt, definitions beside it

A job prompt can contain placeholders written `{{name}}`.
A name is a letter or underscore followed by letters, digits or underscores (`[A-Za-z_][A-Za-z0-9_]*`), case-sensitive, and may be padded with spaces inside the braces (`{{ topics }}`).
Text in double braces that is not a valid name is left alone: it is not a placeholder.
There is no escape syntax; nothing else in a prompt uses double braces.

A job gains a list of **parameter definitions** (`parameters`), one per name:

| Field | Meaning |
| --- | --- |
| `name` | The placeholder name. Unique within the job. |
| `label` | What the form shows. Defaults to the name. |
| `description` | Optional help text under the field. |
| `type` | `single_select`, `multi_select`, `text` or `list`. |
| `options` | The allowed values, for the two select types (at least one; unique). |
| `format` | How a multi-value parameter renders: `comma` or `bullets` (decision 3). Only for `multi_select` and `list`. |

The four types:

- `single_select`: pick one of the options. The value is a string.
- `multi_select`: pick one or more of the options. The value is a list of strings.
- `text`: one free-text value. The value is a string.
- `list`: any number of free-text values the user adds one by one. The value is a list of strings.

Definitions are an ordered list, and the form shows fields in that order.

### 2. Values are stored separately from definitions

A job gains a second field, `parameter_values`: a map from parameter name to its value.
Keeping values apart from definitions is what lets the definitions travel (sharing, export) without the values.

A value is **set** when:

- `text`: non-blank after trimming.
- `single_select`: one of the current options.
- `multi_select`: a non-empty subset of the current options.
- `list`: at least one non-blank item.

Values for names that have no definition are dropped on save.
A select value that is no longer one of the options (the options changed) is dropped on save too, so a stale selection can never render.
For `multi_select` the whole selection is dropped if any of its options is gone, rather than quietly keeping what is left: the parameter then needs a value again, and the owner sees it.

### 3. Rendering

The prompt is rendered just before a run: each placeholder is replaced by its value.

- `text` and `single_select` insert the value as it is.
- `multi_select` and `list` insert their items in the definition's `format`:
  - `comma`: the items joined with `, ` on one line, for a placeholder in the middle of a sentence ("focus on {{regions}}").
  - `bullets`: a Markdown bullet list, one `- item` per line, for a placeholder on its own line.

  `multi_select` defaults to `comma` and `list` to `bullets`, since a fixed choice usually reads as part of a sentence and a free list usually stands on its own.
  The builder shows the format as a toggle on those two types.

When a bullet list replaces a placeholder that is not at the start of a line, a line break is inserted before it, so the list always renders as a list.
Rendering is a pure function (`internal/agentjobs/params`) shared by the scheduler, "Run now" and the editor's preview.

The rendered prompt is what the run sends and what the reply's job record stores (ADR 0x040), so a reply shows what it actually answered.

### 4. All parameters are required

In this version every defined parameter is required.
A job whose parameters are not all set **cannot be active**: it cannot be resumed, run on its schedule, or run with "Run now".

Such a job gets a new status, `needs_values` ("Needs setup" in the UI).
It is set by the server whenever a job is saved with a missing value, and cleared to `active` when a later save fills them all in.
The scheduler never schedules a `needs_values` job, and the status and run-now endpoints refuse it with a message that names the missing parameters.
`paused` keeps its meaning: the user chose to stop it.
A paused job that is saved with missing values stays `paused`; filling them in does not resume it.

Job responses gain `missing_parameters`: the names of the defined parameters that are not set, in definition order, so every client can explain the state without re-deriving it.

### 5. Validation

On every create and update, the server checks:

- every placeholder in the prompt has a definition (error: names the undefined placeholders);
- names are valid and unique, types are known, select types have at least one option and no duplicate options;
- limits: 20 parameters per job, 50 options per select, 50 items per list, 2,000 characters per value or item.

A definition that no placeholder uses is **not** an error.
The editor shows it as a note ("not used in the prompt"), since a publisher may be mid-edit.

Missing values are not a validation error either: they make the job `needs_values` (decision 4).

Existing jobs whose prompts already contain `{{name}}` text are not touched by the migration.
They keep running, and an undefined placeholder renders as the literal text it always was.
The undefined-placeholder error applies the next time such a job's prompt is saved.

### 6. The editor

The job page shows parameters as a section of the job form, under the prompt.

**Form view (the default).**
If the job has parameters, the section renders one field per definition, in order:

- `single_select`: a select.
- `multi_select`: a group of `ui-checkbox` options.
- `text`: a text input.
- `list`: a chip input; Enter adds an item, each chip has a remove button.

Each missing value is marked.
A job with no parameters shows only an "Add parameters" button.
An "Edit parameters" button switches the section to the builder.

**Builder view.**
One row per definition: name, label, type, options (for the select types, as chips), description, reorder and delete.
Under the rows, the section lists what it found in the prompt:

- **Errors** for placeholders with no definition, each with an "Add definition" action that adds a `text` definition with that name.
- **Notes** for definitions no placeholder uses.

Renaming a definition offers to rename its placeholders in the prompt in the same edit.
A "Done" button returns to the form view; it is disabled while there are errors.
The job can't be saved while the builder has errors, and the server enforces the same rule (decision 5).

Definitions and values are part of the form's draft: they are saved with the rest of the job by "Save changes", and they count towards "Unsaved edits".

### 7. Saving with missing values

When the user saves a job (or creates one) with parameters that are not all set, the page first asks:

> **Some parameters have no value**
> Topics and Region are empty. You can save now, but this job can't run until every parameter has a value.
> [Back to editing] [Save anyway]

"Save anyway" saves the job, which becomes `needs_values` (decision 4).

### 8. Leaving a job that needs values

When the user leaves the job page (in-app navigation) while the saved job is `needs_values`, the page asks before leaving:

> **This job needs setup**
> Topics and Region are empty. This job can't run until every parameter has a value.
> [Stay and fill them in] [Leave anyway]

This is a route guard (`canDeactivate`) on the job detail route.
It is there so that someone who has just installed a shared job, and opened it, sees that it needs their input before it can run.
It does not fire for browser-level navigation (closing the tab or reloading), where a page can't show its own dialog.

The same guard covers unsaved edits, which the job page did not warn about before.
Leaving with unsaved edits (any field of the form, parameter definitions or values) asks:

> **Discard unsaved changes?**
> Your changes to this job haven't been saved.
> [Keep editing] [Discard changes]

If the job also needs values, the one dialog says both.
For browser-level navigation the page registers a `beforeunload` handler while there are unsaved edits, so the browser shows its own generic prompt.

The jobs list, job cards and the sidebar's recent jobs show the `needs_values` status as "Needs setup", with the missing parameter names in the tooltip.

### 9. Agent tools

`create_agent_job` and `update_agent_job` gain `parameters` and `parameter_values`, with the same validation and the same `needs_values` rule.
The tools take values as a list of `{name, value}` or `{name, items}` rather than a name-keyed object, because strict tool schemas can't describe an object with free-form keys.
On update, the values passed are merged over the stored ones (an empty value clears one), so the agent can set one parameter without restating the rest.
The tool result lists any missing parameters, so the agent can ask the user for them.
This keeps "set my news job's topics to AI and climate" working in chat, and stops an agent edit of a parameterised prompt from silently dropping a definition.

### 10. Sharing and export

A job's parameter definitions are part of the job wherever it is shared or exported.
Its parameter values are the owner's own configuration, like the time zone and the thread: they are never published.
A job installed from a shared copy has definitions and no values, so it starts as `needs_values`, and the user is taken through decision 8 the first time they open it.

When an installed job is updated to a newer shared version, the installer's values are kept wherever they are still valid under the new definitions: the parameter still exists, with the same type, and (for the select types) every selected option is still offered.
Values that are no longer valid are dropped, and so are values for removed parameters.
If that leaves a parameter without a value, including a parameter the new version added, the job becomes `needs_values`; a paused job stays paused.

An account backup is the owner's own data and keeps both.

## Consequences

- The four types cover the cases raised so far (a fixed choice, several fixed choices, a free value, a free list); numbers, dates, booleans and defaults are left for later.
- `needs_values` is a fourth non-terminal state the list, filters and status helpers have to know about.
- The rendered prompt varies per user, so the same shared job's replies are no longer comparable prompt-for-prompt; that is the point.

## Later

- Optional parameters, defaults (including publisher-suggested defaults on shared jobs).
- More types: number, date, boolean.
- Per-run overrides ("Run now with these values").
