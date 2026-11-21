# WhatIff MVP Design Guide

**Status:** MVP source of truth  
**Scope:** `web/app` — Angular, phone and desktop, light and dark themes  
**Version:** 1.0  
**Publish date:** 2026-11-17  
**Purpose:** Help people complete useful work with WhatIff before exposing the machinery behind it.

> This guide is intentionally small. It is a working skeleton, not a complete design system catalogue. When this guide conflicts with observed repository behavior, verify the implementation and update the guide rather than quietly inventing a third pattern.

---

## Scope and adoption

This guide is lightweight direction for new screens and meaningful updates. It does not require existing components, interactions, or patterns to be retrofitted when they already work.

Existing behavior should not be changed solely to satisfy a guideline. When a relevant area is updated, apply the appropriate guidance as part of that work, using existing primitives and patterns where they remain suitable.

For example, the onboarding guidance is a proposed direction for the next onboarding update. It does not block unrelated work, existing onboarding behavior, or pull requests that do not modify onboarding.

**PR usage:** Apply this guide to new screens and the area being changed. Do not treat it as a blanket requirement for unrelated pull requests or as a reason to rebuild functioning UI.

## 1. Product stance

WhatIff is a personal AI assistant. The interface should help a person:

1. state what they need;
2. get useful help;
3. understand or adjust what happened;
4. continue, save, organize, or undo the work.

The default experience is **conversation before cockpit**. Advanced tools, jobs, memory, MCP, integrations, and configuration should remain inspectable and available, but should not compete with the primary task by default.

### Priorities when making a tradeoff

1. Task completion
2. Understanding and user control
3. Reversibility and recovery
4. Privacy and safety
5. Transparency about system behavior
6. Customization
7. Architectural elegance

The next user step should be simple even when the system underneath is complex.

---

## 2. How to use this guide

### For engineers and designers

Before changing a screen:

1. Identify the user job and what “done” means.
2. Find an existing shared primitive or feature pattern that already serves that job.
3. Use semantic theme tokens and the Tailwind bridge.
4. Define loading, empty, error, success, cancellation, and recovery behavior.
5. When runtime tooling is available, check the affected flow at 360px, 390px, 768px, and a desktop width.
6. Record a meaningful deviation instead of silently adding a new pattern.

### For agents and reviewers

- `MUST` rules are implementation constraints.
- `SHOULD` rules are the default; depart from them only for a stated user or technical need.
- Code inspection is not runtime validation. Label claims as code evidence, runtime evidence, or visual reference.
- Do not create a generic component merely because two things look similar. First prove that they serve the same job and share state, interaction, and accessibility needs.

### Compact agent execution contract

Use this contract for new screens and meaningful UI updates. Do not retrofit unrelated existing work or turn the guide into a blanket PR gate.

1. **Applicability:** classify the change as non-UI, bug fix, existing-flow update, or new screen/flow. Apply relevant guidance to the affected surface only.
2. **Inspect:** state the user job and definition of done; inspect nearby primitives, patterns, state ownership, and accessibility behavior before adding anything.
3. **Implement:** choose the smallest suitable reuse path; build the primary path and required loading, empty, error, success, cancellation, and recovery states.
4. **Verify:** check accessibility, theme behavior, and representative responsive widths when runtime tooling is available. Static inspection is not browser validation.
5. **Report:** label code evidence, runtime evidence, hypotheses, visual references, and anything not checked. Record meaningful deviations with their reason and affected scope.
6. **Escalate:** ask for human direction when intent is ambiguous or the change affects onboarding, top-level navigation, destructive or irreversible actions, privacy/permissions, external systems, or a cross-feature abstraction.

When runtime tooling is unavailable, report the missing check plainly rather than claiming confidence. A documented deviation is not automatically a defect, and an unverified assumption is not proof of failure.

---

## 3. Durable MVP rules

### JOB-01 — Start with the job, then connect it to the code

Every new screen, flow, or component begins with:

- **User job:** what is the person trying to accomplish?
- **Definition of done:** what visible result tells them it worked?
- **Primary action:** what should they do next?
- **Failure/recovery:** what can go wrong, and how do they continue?
- **Technical feasibility:** what existing constraints and feasible reuse paths shape the solution?

Engineers may begin an investigation from an existing route, data object, component, test, issue, or internal capability. These are valid entry points, especially when changing functioning code. They are evidence and constraints—not the product brief.

Before defining the solution, connect the implementation entry point to the user’s goal:

1. Identify what the person is trying to accomplish and what “done” means.
2. Inspect the affected route, data, components, tests, state ownership, and existing interaction patterns.
3. Determine which implementation constraints are real, including backend contracts, permissions, asynchronous behavior, accessibility requirements, and reuse opportunities.
4. Choose the smallest feasible change that supports the user job without duplicating state, inventing an unnecessary abstraction, or exposing internal machinery as the primary experience.
5. Define loading, empty, error, success, cancellation, and recovery behavior.
6. Escalate when the user intent, technical constraint, or cross-feature abstraction is materially ambiguous.

Do not let a data object, route, component catalogue, or internal capability define the experience by itself. Use implementation artifacts as clues, constraints, and possible reuse paths; validate them against the actual user job.

Administration and configuration pages may use a more technical presentation when that better supports their job and does not directly affect the main chat experience.

### HIER-01 — Establish a clear priority hierarchy

Give each view one dominant job and, where appropriate, one clear primary action. Do not give navigation, settings, diagnostics, and execution controls equal visual weight.

For the product-level information architecture, prefer:

- **Primary work:** Chat, Create, Organize, Automate, Review
- **Secondary workspaces:** Threads, Files, Notebooks, Gallery, Diagrams, Memories
- **System and configuration:** Personalities, Integrations, Provider Keys, Sync, Profile, Settings

Recent threads and active work should be easy to resume. A command palette may accelerate navigation, but it must not be the only map of the product.

### REUSE-01 — Reuse shared UI before creating primitives

Use existing components in `web/app/src/app/shared/ui/` before adding a new one. In particular, check the existing button, icon button, modal, sheet, checkbox, card, tabs, badge, tag, spinner, empty-state, data-table, bulk-action-bar, tooltip, help-hint, and avatar primitives.

Extend a primitive only when the existing job, states, and accessibility contract genuinely match. Otherwise use a focused feature component. Avoid a universal component with dozens of modes.

### TOKEN-01 — Use one visual vocabulary

Use semantic theme tokens and the Tailwind bridge for color, surfaces, text, borders, focus, spacing, and typography. Do not introduce a parallel palette or silently rely on undefined token names.

Before migrating a screen to a shared primitive, verify that its tokens work in both light and dark themes.

✅ Code evidence (checked 2026-11-17): `--color-border-default` and `--color-surface-muted` are referenced across `web/app/src` (about 100 uses in 40 files) but are not defined in `web/app/src/styles/` and have no fallback value. The defined equivalents are `--color-border-base`, `--color-border-hover`, and `--color-surface-soft`, `--color-surface-card`, `--color-surface-elevated` in `_tailwind-theme.scss`. Pick the defined token when touching these usages. ⚠️ Whether the undefined names render acceptably in light and dark themes, and any contrast risk in CTA, danger, and muted-text colors, still requires runtime verification.

### RHY-01 — Use rhythm to show relationships

Treat 4px as the base beat and use the repository’s existing spacing scale where practical: `4, 8, 12, 16, 24, 32, 48, 64px`.

- Smaller gaps indicate items belong together.
- Larger gaps indicate a new group.
- Establish the page gutter once; avoid competing nested containers.
- Do not enforce spacing arithmetic when accessibility, content, platform behavior, or optical alignment needs an exception.

This is a preferred rhythm, not a hard whitelist for every pixel.

### ACT-01 — Make action hierarchy predictable

Use one clear primary action when a view needs one. Keep secondary actions visually subordinate and destructive actions explicit.

Default action placement:

- Desktop dialogs/forms: secondary, then primary at the end.
- Destructive confirmation: destructive action separated from ordinary continuation.
- Phone: full-width actions, primary first, with safe-area support where sticky.
- Long flows: keep the primary action in a stable location between steps.

Use result-oriented labels such as “Save personality,” “Add selected,” or “Retry,” rather than vague labels such as “Submit” or “Continue” when the result is known.

### REP-01 — Representation follows the task

Choose a representation based on what the person needs to do:

- **List:** scan, select, or manage file-like content.
- **Table:** compare structured values across rows and columns.
- **Grid:** visually browse images or other visual assets.
- **Card:** understand or compare a bounded item where explanation matters.
- **Picker:** select one or more existing assets and confirm the selection.
- **Form:** provide or edit structured information.

Do not turn every feature into cards, and do not turn every collection into a table.

### STATE-01 — Design the whole state path

Every meaningful interaction needs an intentional treatment for:

- initial/loading;
- empty, including what to do next;
- success and confirmation;
- error and retry;
- cancellation;
- partial completion or background work;
- recovery after navigation, refresh, or failure.

For AI work, distinguish clearly between composing, queued, running, streaming, completed, failed, cancelled, and retryable. Do not imply that a configured tool or integration is currently callable without runtime evidence.

### CHAT-01 — Keep chat conversation-first

The chat surface should prioritize:

1. readable conversation;
2. composing the next request;
3. lightweight context and personality awareness;
4. inspectable execution details on demand.

Default presentation should be quiet: a subtle personality indicator, one understandable “add context” action, relevant tools only, and compact expandable execution status. Keep jobs, MCP frames, polling, scratchpad, tools, skills, and diagnostics available without making the composer feel like an operations console.

Starter prompts may help new users; slash commands should remain an accelerator, not the only discoverable path.

### NAV-01 — Navigation should explain the product

Use a small number of consistent navigation levels:

- global navigation for primary workspaces;
- tabs for views within one destination;
- segmented controls for filtering one collection;
- a page header with a predictable back action for drill-down.

Keep page titles, navigation labels, and back destinations derived from one source where possible. Preserve user context when moving between a list and its detail view.

The existing sidebar supports expanded, collapsed, mobile, active-route, mode-switching, and focus states. Treat those as an implementation contract to preserve—not as a reason to invent another sidebar pattern. The guide should focus on what users need once they enter a destination.

### NAV-02 — Configuration is a mode, not a junk drawer

Configuration pages help people inspect, change, connect, automate, protect, and recover system behavior. They may be reached through configuration mode or a contextual link, but each page must still explain its own job.

Do not promote a route to primary navigation merely because a technical feature exists. Group configuration destinations by user task and consequence, not by backend model or implementation ownership.

### A11Y-01 — Accessibility is part of the primitive contract

Target WCAG 2.2 AA.

- Use semantic/native controls first.
- Give every control an accessible name.
- Preserve visible keyboard focus and a sensible focus order.
- Use labels, hints, and errors that are programmatically associated with fields.
- Support keyboard interaction for dialogs, menus, tabs, comboboxes, and selection controls.
- Use at least 24×24 CSS-pixel targets where applicable; prefer 44px touch targets.
- Verify text contrast at 4.5:1 and meaningful non-text UI/focus indicators at 3:1.
- Do not make color the only signal.

Dialogs should use the shared modal/sheet infrastructure so focus trapping, Escape behavior, scroll handling, and focus return are not reimplemented per feature.

### RESP-01 — Phone is a primary context

Verify representative flows at **360px, 390px, 768px, and desktop** widths. Use dynamic viewport units and safe-area insets for full-height or sticky layouts where appropriate.

On small screens:

- protect the primary task first;
- reduce tertiary decoration before reducing usable controls;
- avoid hidden or undiscoverable horizontal scrolling;
- avoid nested scroll regions unless the task clearly requires them;
- preserve readable grouping and reachable actions;
- use sheets for suitable picker/confirmation flows when the existing infrastructure supports them.

### DEV-01 — Document meaningful deviations

A deviation is justified only when the default pattern cannot serve the user job or a verified technical constraint.

Record:

- the rule being departed from;
- the screen and user job;
- why the default pattern fails;
- the alternative;
- how accessibility, privacy, reversibility, and recovery remain covered;
- the owner and verification needed.

A deviation is not permission to create a new visual style because it is faster or more interesting.

---

## 4. MVP interaction patterns

### 4.1 Onboarding

The first-run experience should establish value before teaching internal concepts.

- Avoid making “choose a personality” feel like a prerequisite exam.
- Provide a sensible starter personality or guided default.
- Let the user reach a first useful conversation quickly.
- Expose the full personality editor after value is demonstrated.
- Explain what the personality changes in plain language.

The existing personality setup gate is a functional dependency, not yet a complete welcome experience. This is proposed direction for the next onboarding update—not a claim that the current app already implements it. Apply this guidance when onboarding is changed; it does not block unrelated work or new pull requests.

### 4.2 Configuration pages

Configuration is where users change the assistant’s behavior and connections. Every configuration page should answer:

1. What does this control?
2. What will change if I edit it?
3. Who or what is affected?
4. Is the change saved, active, tested, reversible, or still pending?
5. How can I test, undo, disable, or recover?

Use the smallest page pattern that fits the job:

- **Overview:** orient the user, group configuration by task, show meaningful attention items, and offer one clear next action.
- **Collection/management:** help users find, compare, select, enable, disable, archive, or delete configured entities. Choose list, table, grid, or cards based on that management task.
- **Detail/editor:** state whether the user is creating, editing, duplicating, or inspecting; distinguish required and optional fields; explain defaults; show save state; handle cancellation and unsaved work.
- **Connection/operation:** show whether something is not configured, configured but untested, connected and usable, degraded, expired, or disabled. Separate “saved” from “working.” Explain capability, data flow, test action, and recovery.

For configuration that affects conversations, stored data, external services, privacy, or automation, make scope and consequence explicit before the action. Distinguish delete, disable, archive, reset, disconnect, rotate, and cancel. Use outcome labels such as “Save personality,” “Test connection,” or “Disable sync,” not vague labels such as “Submit.”

Do not expose backend machinery merely because it exists. Advanced details may be inspectable on demand, but the default page should make the user’s next safe step obvious.

### 4.3 Multi-select pickers

Use one selection model across views. A shared picker should own:

- asset kind/filter;
- grid or list view;
- search/query;
- selected IDs;
- loading, empty, error, and retry states;
- a clear confirmation action such as **Add selected**.

Changing from Grid to List or changing filters must not unexpectedly clear valid selections. Use visual grids for image browsing and readable lists for file-oriented content. Verify that the backend contract supports every asset type before presenting it as attachable.

### 4.4 Confirmation and destructive work

Add friction only where the consequence is destructive, irreversible, external, or privacy-sensitive. Ordinary reversible work should remain fast.

Confirm the consequence, not the implementation detail. State what will change, what can be recovered, and which action is final.

### 4.5 AI execution and background work

Show enough status to preserve trust without exposing an internal event stream by default.

A useful default status treatment answers:

- Is the request still working?
- Is the user able to continue?
- What can they inspect?
- Can they cancel, retry, or recover?

Use expandable detail for tools, jobs, MCP, context, and scratchpad information. Never use a decorative “working” state that remains ambiguous after completion or failure.

---

## 5. Definition of done for a UI change

Before considering a screen or flow complete, check:

- [ ] The user job and definition of done are written down.
- [ ] The primary action and its consequence are clear.
- [ ] Existing shared primitives and tokens were considered first.
- [ ] There is no duplicated state or competing source of truth.
- [ ] Loading, empty, success, error, cancellation, and recovery states are handled.
- [ ] Keyboard, focus, labels, semantics, and touch targets work by design.
- [ ] Light and dark theme behavior is considered.
- [ ] The affected flow is checked at 360px, 390px, 768px, and desktop widths when runtime tooling is available; otherwise the missing check is reported as ⏳ Not checked.
- [ ] Any runtime or backend assumption is marked for verification.
- [ ] Any meaningful deviation is documented.

For non-UI changes, mark the responsive and visual checks not applicable.

---

## 6. Evidence and verification language

Use these labels in reviews and implementation notes:

- **✅ Code evidence:** confirmed by reading repository files, tests, or configuration.
- **✅ Runtime evidence:** confirmed by running the app or an automated browser or integration check.
- **⚠️ Runtime/contract hypothesis:** plausible but requires browser, API, or integration verification.
- **🎨 Visual reference:** screenshot, mockup, or generated concept used for direction; not proof of Angular/browser behavior.
- **⏳ Not checked:** verification was unavailable or out of scope.

Do not describe a code reading or visual render as an end-to-end validation.

---

## 7. Deferred from this MVP guide

The following may become useful later, but do not belong in the first canonical skeleton:

- a complete component catalogue and adopter counts;
- a full spacing, color, typography, radius, or breakpoint inventory;
- a build roadmap for every proposed primitive;
- an exhaustive rule register;
- formal sign-off tables and governance workflows;
- a maintained HTML companion;
- exact measurements or claims that have not been rechecked against the current branch and runtime;
- a universal abstraction for every repeated-looking UI.

When a later need is concrete, add the smallest rule or reference needed. Keep this guide a skeleton, not a fossil record.

---

## 8. Change log

### 1.0 — 2026-11-17

Made the guide the MVP source of truth. Added the scope and adoption section, the compact agent execution contract, a technical-feasibility step in JOB-01, NAV-02, configuration-page guidance, runtime-evidence and not-checked labels, and a verified note on undefined tokens in TOKEN-01. Relaxed the responsive-width check to apply when runtime tooling is available.

### 0.1

Rounded out the MVP guide with configuration-page guidance, a navigation contract that reflects the existing sidebar implementation, and incremental adoption guidance. Added page archetypes, status and consequence rules, connection-state distinctions, and recovery expectations without introducing a component catalogue or speculative runtime claims. Clarified that guidelines apply to new screens and relevant updates, not as a retrofit or blanket PR gate.

### Initial draft

Initial MVP skeleton mined from the existing design draft and the WhatIff UX discussion. Consolidated the durable principles, removed proposed inventories and unverified authority claims, and added product hierarchy, onboarding, chat, picker, AI-state, and evidence guidance.
