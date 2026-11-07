# UI style standards

New screens keep falling back to browser defaults or one-off Tailwind styling.
These are the house rules for the Angular app in `web/app`, so a new screen looks like the rest of the app in light and dark mode without anyone restyling it later.
Every primitive below lives in `src/app/shared/ui/` and is exported from `shared/ui/index.ts`.

## 1. Checkboxes: always `ui-checkbox`

Use `<ui-checkbox>` (`shared/ui/checkbox`) for every checkbox.
Never add a bare `<input type="checkbox">`, a Tailwind-styled one (`rounded border-gray-300 …`), `accent-color`, or a copy of the `appearance: none` box styles.

It is the Memory Manager checkbox: an accent-filled box with a white tick, a dash when indeterminate, a focus ring, and theme tokens for light and dark.
Inside it is a real native `<input type="checkbox">`, so labels, keyboard use, `role=checkbox`, `getByRole('checkbox')` and `input[type="checkbox"]` selectors all still work.

```html
<!-- With visible text: wrap it in your own <label>, as you would a native input. -->
<label class="filters__toggle">
  <ui-checkbox [checked]="pinnedOnly()" (checkedChange)="pinnedOnlyChange.emit($event)" />
  Pinned only
</label>

<!-- No visible text: give it an accessible name. -->
<ui-checkbox [checked]="checked()" (change)="toggleSelect.emit(row.id)" [ariaLabel]="'Select thread ' + row.name" />
```

| Input | Purpose |
| --- | --- |
| `checked`, `indeterminate`, `disabled` | State. `indeterminate` shows the dash and is announced as "mixed". |
| `ariaLabel` | Accessible name when there is no visible label. It goes on the inner input only, so do not also put `aria-label` on `<ui-checkbox>`. |
| `inputId`, `name` | Set on the inner input (`inputId` for a `<label for>` elsewhere). |
| `testId` | `data-testid` on the inner input. |
| `inputClass` | Extra classes on the inner input, as a hook for tests or e2e. Do not use it to restyle the box. |
| `tooltip` | A `uiTooltip` on the inner input, so it also shows on keyboard focus. |

| Output | Purpose |
| --- | --- |
| `(checkedChange)` | The new state as a boolean. Together with `[checked]` it gives you `[(checked)]`. Prefer it whenever the handler only needs the value; use `(change)` only when it needs the input itself, as `saveCheckbox` does to put the box back after a failed save. |
| `(change)` | The native change event, fired once. `$event.target` is the inner input, so `$any($event.target).checked` and `saveCheckbox($any($event.target), …)` (`core/utils/checkbox.helpers.ts`) work unchanged. |

It is also a `ControlValueAccessor`, so `ngModel`, `formControl` and `formControlName` work on it directly.
Layout utilities such as `class="mt-0.5"` go on `<ui-checkbox>` itself.

Every public `<input type="checkbox">` now goes through `<ui-checkbox>`.
The one exception is the gallery grid tile's check, a `<button role="checkbox">` with its own behaviour, which needs a pass of its own.
Do not add a raw checkbox.

## 1a. Radios: `ui-radio`

`<ui-radio>` (`shared/ui/radio`) is the radio that goes with `ui-checkbox`: a native `<input type="radio">` with the same border, accent and focus ring.
Like the checkbox it renders only the control, so wrap it in your own `<label>`, and give every radio of a group the same `name` (arrow keys then move within the group natively).
Inputs: `name` (required), `checked`, `disabled`, `value`, `ariaLabel`, `inputId`, `testId`. Output: `(selected)` when it becomes the group's choice.

## 2. Tables with selection: `ui-data-table` + `ui-bulk-action-bar`

When a screen lists rows that people select and act on together, use the shared table rather than building a new one.
The Thread Manager (`features/chat/components/thread-list-panel/thread-list-panel.component.ts`) and the gallery list view (`features/gallery/components/gallery-list.component.html`) both use it.

**`ui-data-table`** (`shared/ui/data-table`) renders the bordered table, a first column with the select-all checkbox, your header cells, and your rows.
While loading, when empty, or after an error, it shows a message instead of the rows.

- Header cells: an `<ng-container ngProjectAs="[uiTableHead]">` of `<th scope="col">`.
- Rows: the default slot. Each row is a row component with `:host { display: contents }` that renders its own `<tr>`, and whose **first cell is its selection `ui-checkbox`** (see `thread-row.component.ts` and `gallery-list-row.component.html`). Rows that cannot be selected, such as gallery folders, leave that cell empty.
- Inputs: `selectAllState` (`'none' | 'some' | 'all'`), `selectAllLabel` (required; the checkbox's accessible name, e.g. "Select all threads"), `selectAllTooltip`, `message` and `messageTone` (`'muted' | 'error'`; `error` makes the message an alert), `bodyLabel`, and `bodyRole` (`listbox` when rows are options, as in the Thread Manager).
- Output: `(selectAllChange)`, with no payload. You decide what select-all means.
- A wide table can set `--ui-data-table-min-width` on the host (the Thread Manager uses `52rem`) so columns do not get squeezed on desktop.

**Selection state** lives in the service that owns the rows, in a `SelectionModel` (`shared/ui/helpers/selection-model.ts`).
It holds a set of ids plus `toggle`, `setAll`, `replace`, `clear` and `retain`, and its `stateOf(visibleIds)` gives the `'none' | 'some' | 'all'` that drives the select-all box (`'some'` makes it indeterminate).
Pass the ids that are **shown**, so select-all only affects visible rows:

- Thread Manager: `selectAllState = threads.selectionState(visibleIds)`, and `onSelectAllChange()` calls `threads.setAllSelected(visibleIds, state !== 'all')`.
- Gallery: `GalleryViewService.toggleSelectAll()` selects every image shown, or clears the selection when they all already are. After a reload, `retain(...)` drops ids that are no longer loaded.

**`ui-bulk-action-bar`** (`shared/ui/bulk-action-bar`) is the bar shown while something is selected: "N selected", a "Choose action" menu, and Clear.
Render it only while the count is above 0 (both adopters do).

- Inputs: `count`, `actions` (`BulkAction[]`: `{ id, label, danger?, ariaLabel? }`, where `danger` is for destructive actions such as Delete), `label`, and `variant` (`strip` spans the panel edge to edge in the Thread Manager; `card` is a rounded box, used in the gallery).
- Outputs: `(action)` with the chosen id, and `(clear)`.
- Slots: `[uiBulkLead]` before the count (for example, the gallery grid's own select-all), the default slot after it, and `[uiBulkTrail]` after Clear (for example, a Done button).

## 3. Use these before rolling your own

| Need | Use |
| --- | --- |
| Button / icon-only button | `ui-button` (`variant`: primary, secondary, ghost, danger; `size`: md, sm, or xs for surfaces with many actions, such as cards and toolbars — it grows back to a 36px target on touch screens; `loading`) / `ui-icon-button` (`ariaLabel` required; `size`: md or xs) |
| Toggle chip / filter pill | `ui-tag` (`selected`, `(toggle)`) |
| Status pill | `ui-badge` (`intent`: neutral, info, success, warning, danger) |
| Surface | `ui-card` (`variant`: flat, elevated) |
| Dialog / bottom sheet | `ui-modal` (`open`, `labelledBy`, `size`, `(dismiss)` with a reason) / `ui-sheet` |
| Tabs | `ui-tabs` + `ui-tab` + `ui-tab-panel` (`size="sm"` on `ui-tabs` inside a card) |
| Empty list | `ui-empty-state` (`heading`, `body`) |
| Hover/focus hint / click-to-open explainer | `uiTooltip` directive / `ui-help-hint` (works on touch, can link a guide) |
| Loading | `ui-spinner` |
| Person/persona picture | `ui-avatar` |
| Icons | `ui-*-icon` components in `shared/ui/icons` |
| Behaviour | `shared/ui/helpers`: `SelectionModel`, focus trap, body scroll lock, keyboard helpers, initials |

## Colours and theming

- Use the theme tokens (`src/styles/tokens.scss` lists the names, `themes.scss` has the light and dark values): `var(--text-primary)`, `var(--bg-card)`, `var(--accent)`, and so on. In SCSS, the Tailwind bridge names such as `var(--color-accent)` and `var(--color-border-base)` work too, and so do Tailwind classes like `bg-surface-card` and `text-text-primary`.
- Prefer tokens over hard-coded hex colours and per-element `dark:` Tailwind variants (older screens still have some). The tokens already switch between light and dark, so a new screen needs no dark-mode-specific styling.
- When one surface needs its own colour, give it its own token in `themes.scss` rather than changing a shared one. For example, the user chat bubble reads `--user-bubble`, not `--accent`.
