import { Location } from '@angular/common';
import { ChangeDetectionStrategy, Component, OnInit, computed, effect, inject, input, output, signal } from '@angular/core';
import { TooltipDirective } from '../../../../shared/ui/tooltip/tooltip.directive';

import { Chat } from '../../../../core/models/chat.model';
import { Personality } from '../../../../core/models/personality.model';
import { ThreadListService } from '../../../../core/services/thread-list.service';
import { ConfirmationService } from '../../../../core/services/confirmation.service';
import { ModalComponent } from '../../../../shared/ui/modal/modal.component';
import { ThreadRowComponent } from './thread-row.component';
import { ChatImportModalComponent } from '../chat-import-modal/chat-import-modal.component';

const MAX_THREAD_TAGS = 10;
const MAX_THREAD_TAG_LENGTH = 10;

type ThreadListTab = 'active' | 'archived';

@Component({
  selector: 'app-thread-list-panel',
  standalone: true,
  imports: [ModalComponent, ThreadRowComponent, TooltipDirective, ChatImportModalComponent],
  template: `
    <aside class="panel" aria-label="Threads">
      <header class="panel__header">
        <div class="panel__title-row">
          <div class="panel__title-spacer"></div>
          <h2>Thread Manager</h2>
          <div class="panel__title-actions">
            <button type="button" class="panel__back-btn" (click)="goBack()" aria-label="Go back">
              <svg width="13" height="13" viewBox="0 0 13 13" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                <polyline points="8,2 4,6.5 8,11"/>
              </svg>
              <span class="panel__back-label">Back</span>
            </button>
            <button
              type="button"
              class="panel__import-btn"
              aria-label="Import Conversations"
              uiTooltip="Import Conversations"
              placement="left"
              (click)="importOpen.set(true); onThreadListTab('archived')"
            >
              <svg width="14" height="14" viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                <polyline points="2,10 2,12 12,12 12,10"/>
                <line x1="7" y1="2" x2="7" y2="9"/>
                <polyline points="4,6 7,9 10,6"/>
              </svg>
              Import
            </button>
          </div>
        </div>
        <div class="panel__toolbar">
          <div class="panel__tabs" role="tablist" aria-label="Thread status">
            <button
              type="button"
              role="tab"
              [attr.aria-selected]="threadListTab() === 'active'"
              [class.panel__tab--active]="threadListTab() === 'active'"
              (click)="onThreadListTab('active')"
            >
              Active
            </button>
            <button
              type="button"
              role="tab"
              [attr.aria-selected]="threadListTab() === 'archived'"
              [class.panel__tab--active]="threadListTab() === 'archived'"
              (click)="onThreadListTab('archived')"
            >
              Archived
            </button>
          </div>

          <div
            class="panel__search"
            (click)="searchInput.focus()"
            (focusout)="closeTagDropdownSoon()"
          >
            @for (tag of selectedTagFilters(); track tag) {
              <span class="panel__tag-chip">
                {{ tag }}
                <button type="button" (click)="removeTagFilter(tag); $event.stopPropagation()" [attr.aria-label]="'Remove tag filter ' + tag">×</button>
              </span>
            }
            <input
              #searchInput
              type="search"
              [value]="threads.query()"
              (input)="threads.setQuery($any($event.target).value)"
              (focus)="tagDropdownFocused.set(true)"
              (keydown.backspace)="removeLastTagIfEmpty($event)"
              placeholder="Search titles and tags…"
              aria-label="Search thread titles and tags"
            />
            @if (showTagDropdown()) {
              <div class="panel__tag-dropdown" role="listbox" aria-label="Tag suggestions">
                @for (tag of tagOptions(); track tag) {
                  <button type="button" role="option" (mousedown)="addTagFilter(tag, $event)">
                    <span>{{ tag }}</span>
                  </button>
                }
              </div>
            }
          </div>
        </div>
      </header>

      @if (threads.listTruncated()) {
        <p class="panel__truncate-hint" role="status">
          Showing the most recent threads (list capped for performance).
        </p>
      }

      @if (threads.selectedCount() > 0) {
        <div class="panel__bulk-bar" role="region" aria-label="Bulk actions">
          <span class="panel__bulk-count" aria-live="polite">{{ threads.selectedCount() }} selected</span>
          <div class="panel__bulk-action" (focusout)="closeBulkMenuSoon()" (keydown.escape)="bulkActionMenuOpen.set(false)">
            <button
              type="button"
              class="panel__bulk-action-btn"
              aria-haspopup="listbox"
              [attr.aria-expanded]="bulkActionMenuOpen()"
              (click)="bulkActionMenuOpen.set(!bulkActionMenuOpen())"
            >
              Choose action <span aria-hidden="true">▾</span>
            </button>
            @if (bulkActionMenuOpen()) {
              <div class="panel__bulk-action-menu" role="listbox" aria-label="Bulk actions">
                @if (threadListTab() === 'active') {
                  <button type="button" role="option" (mousedown)="runBulkArchive()">Archive</button>
                } @else {
                  <button type="button" role="option" (mousedown)="runBulkRestore()">Restore</button>
                }
                <button type="button" role="option" (mousedown)="openBulkPersonalityPicker()">Assign to personality…</button>
                <button type="button" role="option" class="panel__bulk-action-menu-item--danger" (mousedown)="runBulkDelete()">Delete</button>
              </div>
            }
          </div>
          <button type="button" class="panel__bulk-clear" (click)="threads.clearSelection()" aria-label="Clear selection">
            Clear <span aria-hidden="true">×</span>
          </button>
        </div>
      }

      <section
        class="panel__body"
        tabindex="0"
        [attr.aria-activedescendant]="activeThreadId() ?? null"
        aria-label="Thread manager table"
        (keydown)="onListboxKeydown($event)"
      >
        <div class="panel__table-shell">
          @if (threads.error(); as error) {
            <p class="panel__empty panel__error" role="alert">{{ error }}</p>
          } @else if (threads.loading()) {
            <p class="panel__empty">Loading threads…</p>
          } @else if (visibleThreads().length === 0) {
            <p class="panel__empty">
              {{ threadListTab() === 'archived' ? 'No archived threads' : 'No threads found' }}
            </p>
          } @else {
            <table>
              <thead>
                <tr>
                  <th scope="col" class="panel__select-col">
                    <input
                      type="checkbox"
                      class="panel__select-all"
                      [checked]="selectAllState() === 'all'"
                      [indeterminate]="selectAllState() === 'some'"
                      (change)="onSelectAllChange()"
                      aria-label="Select all threads"
                    />
                  </th>
                  <th scope="col">
                    <button type="button" (click)="threads.pinnedOnly.set(!threads.pinnedOnly())">
                      STARRED? <span aria-hidden="true">⬍</span>
                    </button>
                  </th>
                  <th scope="col">
                    <label>
                      PERSONALITY
                      <select
                        [value]="threads.selectedPersonalityId() ?? ''"
                        (change)="threads.selectedPersonalityId.set($any($event.target).value || null)"
                        aria-label="Filter by personality"
                      >
                        <option value="">All</option>
                        @for (personality of threads.personalities(); track personality.id) {
                          <option [value]="personality.id">{{ personality.label }}</option>
                        }
                      </select>
                    </label>
                  </th>
                  <th scope="col">
                    <button type="button" (click)="threads.sort.set('alphabetical')">
                      TITLE <span aria-hidden="true">⬍</span>
                    </button>
                  </th>
                  <th scope="col">
                    <button type="button" (click)="threads.sort.set('newest')">
                      CREATED <span aria-hidden="true">⬍</span>
                    </button>
                  </th>
                  <th scope="col">
                    <button type="button" (click)="threads.sort.set('recent')">
                      UPDATED <span aria-hidden="true">▼</span>
                    </button>
                  </th>
                  <th scope="col">TAGS</th>
                  <th scope="col">{{ threadListTab() === 'archived' ? 'RESTORE?' : 'ARCHIVE?' }}</th>
                  <th scope="col" class="panel__delete-col"><span class="panel__sr-only">Delete</span></th>
                </tr>
              </thead>
              <tbody role="listbox" aria-label="Thread list">
                @for (thread of visibleThreads(); track thread.id) {
                  <app-thread-row
                    [thread]="thread"
                    [active]="activeThreadId() === thread.id"
                    [personality]="personalityForThread(thread)"
                    [isArchivedView]="threadListTab() === 'archived'"
                    [checked]="threads.selectedIds().has(thread.id)"
                    (select)="selectThread.emit($event)"
                    (toggleSelect)="threads.toggleSelected($event)"
                    (rename)="rename($event.thread, $event.name)"
                    (togglePin)="togglePin($event)"
                    (editTags)="editTags($event)"
                    (deleteThread)="deleteThread($event)"
                    (archiveThread)="archiveThread($event)"
                    (restoreThread)="restoreThread($event)"
                  />
                }
              </tbody>
            </table>
          }
        </div>
      </section>
    </aside>

    <ui-modal [open]="tagEditThread() !== null" [labelledBy]="tagEditTitleId" size="md" (dismiss)="closeTagEdit()">
      <div modal-header class="thread-tag-edit__header">
        <h2 [id]="tagEditTitleId" class="thread-tag-edit__title">Edit tags</h2>
        @if (tagEditThread(); as t) {
          <p class="thread-tag-edit__subtitle">{{ t.name }}</p>
        }
      </div>

      <div class="thread-tag-edit">
        @if (tagEditError(); as err) {
          <p class="thread-tag-edit__error" role="alert">{{ err }}</p>
        }
        <p class="thread-tag-edit__hint">
          Add up to {{ maxThreadTags }} tags, {{ maxThreadTagLength }} characters each. Press Enter, comma, or Space to add multiple tags.
        </p>
        <div
          class="thread-tag-edit__field"
          (click)="tagEditInput.focus()"
        >
          @for (tag of tagEditChips(); track $index) {
            <span class="thread-tag-edit__chip">
              {{ tag }}
              <button
                type="button"
                class="thread-tag-edit__chip-remove"
                [attr.aria-label]="'Remove tag ' + tag"
                (click)="removeTagChip($index); $event.stopPropagation()"
              >×</button>
            </span>
          }
          <input
            #tagEditInput
            type="text"
            class="thread-tag-edit__input"
            [value]="tagInputValue()"
            (input)="onTagInputField($event)"
            (keydown)="onTagKeydown($event)"
            placeholder="Add tag…"
            [attr.aria-label]="'New tag, ' + tagEditChips().length + ' of ' + maxThreadTags + ' used'"
          />
        </div>
      </div>

      <div modal-footer class="thread-tag-edit__footer">
        <button type="button" class="thread-tag-edit__btn thread-tag-edit__btn--ghost" (click)="closeTagEdit()">Cancel</button>
        <button type="button" class="thread-tag-edit__btn thread-tag-edit__btn--primary" (click)="saveTagEdit()">Save</button>
      </div>
    </ui-modal>

    <ui-modal [open]="bulkPersonalityPickerOpen()" [labelledBy]="bulkPersonalityTitleId" size="sm" (dismiss)="closeBulkPersonalityPicker()">
      <div modal-header>
        <h2 [id]="bulkPersonalityTitleId" class="thread-tag-edit__title">Assign to personality</h2>
        <p class="thread-tag-edit__subtitle">{{ threads.selectedCount() }} thread(s) selected</p>
      </div>

      <ul class="bulk-personality-picker" role="listbox" aria-label="Personalities">
        @for (personality of personalities(); track personality.id) {
          <li>
            <button type="button" role="option" (click)="assignBulkPersonality(personality.id)">
              {{ personality.name }}
            </button>
          </li>
        } @empty {
          <li class="bulk-personality-picker__empty">No personalities available.</li>
        }
      </ul>

      <div modal-footer class="thread-tag-edit__footer">
        <button type="button" class="thread-tag-edit__btn thread-tag-edit__btn--ghost" (click)="closeBulkPersonalityPicker()">Cancel</button>
      </div>
    </ui-modal>

    <app-chat-import-modal
      [open]="importOpen()"
      (dismiss)="onImportClosed()"
      (imported)="onConversationsImported()"
    />
  `,
  styles: [`
    .panel {
      background: var(--color-surface-base);
      display: flex;
      flex-direction: column;
      height: 100%;
      min-height: 0;
    }

    .panel__header {
      border-bottom: 1px solid var(--color-border-base);
      display: flex;
      flex-direction: column;
      gap: 0.625rem;
      padding: 0.875rem 1.25rem 0.75rem;
    }

    .panel__title-row {
      align-items: center;
      display: grid;
      grid-template-columns: 1fr auto 1fr;
    }

    .panel__title-spacer {
      /* balances the right-side actions so h2 stays centered */
    }

    .panel__title-row h2 {
      text-align: center;
    }

    .panel__title-actions {
      align-items: center;
      display: flex;
      gap: 6px;
      justify-content: flex-end;
    }

    .panel__back-btn,
    .panel__import-btn {
      align-items: center;
      background: transparent;
      border: 1px solid var(--color-border-base);
      border-radius: 6px;
      color: var(--color-text-secondary);
      cursor: pointer;
      display: inline-flex;
      font-size: 0.75rem;
      font-weight: 500;
      gap: 4px;
      padding: 3px 10px 3px 7px;
      transition: color 0.15s, border-color 0.15s;

      &:hover {
        border-color: var(--color-accent);
        color: var(--color-accent);
      }
    }

    .panel__truncate-hint {
      background: var(--color-surface-elevated, var(--color-surface-base));
      border-bottom: 1px solid var(--color-border-base);
      color: var(--color-text-muted);
      font-size: 0.8125rem;
      margin: 0;
      padding: 0.5rem 1.25rem;
      text-align: center;
    }

    h2 {
      color: var(--color-text-primary);
      font-size: 1.125rem;
      font-weight: 700;
      margin: 0;
      text-align: center;
    }

    .panel__select-col {
      text-align: center;
      width: 2.5rem;
    }

    .panel__bulk-bar {
      align-items: center;
      background: color-mix(in srgb, var(--color-accent) 10%, transparent);
      border-bottom: 1px solid var(--color-border-base);
      display: flex;
      gap: 0.75rem;
      padding: 0.5rem 1.25rem;
    }

    .panel__bulk-count {
      color: var(--color-accent);
      font-size: 0.75rem;
      font-weight: 700;
    }

    .panel__bulk-action {
      position: relative;
    }

    .panel__bulk-action-btn {
      align-items: center;
      background: var(--color-surface-base);
      border: 1px solid var(--color-accent);
      border-radius: 0.375rem;
      color: var(--color-accent);
      cursor: pointer;
      display: inline-flex;
      font-size: 0.75rem;
      font-weight: 600;
      gap: 0.25rem;
      padding: 0.35rem 0.75rem;
    }

    .panel__bulk-action-menu {
      background: var(--color-surface-elevated);
      border: 1px solid var(--color-border-base);
      border-radius: 0.5rem;
      box-shadow: 0 0.5rem 1.5rem color-mix(in srgb, black 18%, transparent);
      left: 0;
      min-width: 12rem;
      overflow: hidden;
      position: absolute;
      top: calc(100% + 0.25rem);
      z-index: 10;
    }

    .panel__bulk-action-menu button {
      background: transparent;
      border: 0;
      color: var(--color-text-primary);
      cursor: pointer;
      display: flex;
      font-size: 0.75rem;
      padding: 0.5rem 0.75rem;
      text-align: left;
      width: 100%;
    }

    .panel__bulk-action-menu button:hover,
    .panel__bulk-action-menu button:focus-visible {
      background: var(--color-surface-muted);
    }

    .panel__bulk-action-menu-item--danger {
      color: var(--danger);
    }

    .panel__bulk-clear {
      background: transparent;
      border: 0;
      color: var(--color-text-muted);
      cursor: pointer;
      font-size: 0.75rem;
      font-weight: 600;
      margin-left: auto;
      padding: 0.35rem 0.5rem;
    }

    .panel__bulk-clear:hover {
      color: var(--color-text-primary);
    }

    .bulk-personality-picker {
      list-style: none;
      margin: 0;
      max-height: 16rem;
      overflow: auto;
      padding: 0;
    }

    .bulk-personality-picker button {
      background: transparent;
      border: 0;
      border-radius: 0.375rem;
      color: var(--color-text-primary);
      cursor: pointer;
      display: block;
      font-size: 0.875rem;
      padding: 0.5rem 0.75rem;
      text-align: left;
      width: 100%;
    }

    .bulk-personality-picker button:hover,
    .bulk-personality-picker button:focus-visible {
      background: var(--color-surface-muted);
    }

    .bulk-personality-picker__empty {
      color: var(--color-text-muted);
      font-size: 0.8125rem;
      padding: 0.5rem 0.75rem;
    }

    .panel__toolbar {
      align-items: center;
      --panel-toolbar-control-height: 2.125rem;
      display: flex;
      gap: 0.75rem;
      min-width: 0;
    }

    .panel__tabs {
      border: 1px solid var(--color-border-base);
      border-radius: 0.375rem;
      display: flex;
      flex-shrink: 0;
      height: var(--panel-toolbar-control-height);
      min-height: var(--panel-toolbar-control-height);
      overflow: hidden;
    }

    .panel__tabs button {
      align-items: center;
      background: transparent;
      border: 0;
      color: var(--color-text-muted);
      cursor: pointer;
      display: inline-flex;
      font-size: 0.68rem;
      font-weight: 600;
      height: var(--panel-toolbar-control-height);
      line-height: 1;
      padding: 0 0.875rem;
    }

    .panel__tabs .panel__tab--active {
      background: color-mix(in srgb, var(--color-accent) 14%, transparent);
      color: var(--color-accent);
    }

    .panel__search {
      align-items: center;
      background: var(--color-surface-muted);
      border: 1px solid var(--color-border-base);
      border-radius: 0.4375rem;
      cursor: text;
      display: flex;
      flex: 1;
      flex-wrap: wrap;
      gap: 0.25rem;
      min-height: var(--panel-toolbar-control-height);
      min-width: 0;
      padding: 0.25rem 0.5rem;
      position: relative;
    }

    .panel__search input {
      background: transparent;
      border: 0;
      color: var(--color-text-primary);
      flex: 1;
      font-size: 0.75rem;
      min-width: 6.25rem;
      outline: 0;
      padding: 0.125rem 0;
    }

    .panel__tag-chip {
      align-items: center;
      background: color-mix(in srgb, var(--color-accent) 14%, transparent);
      border: 1px solid var(--color-accent);
      border-radius: 999px;
      color: var(--color-accent);
      display: flex;
      flex-shrink: 0;
      font-size: 0.625rem;
      font-weight: 700;
      gap: 0.125rem;
      padding: 0.0625rem 0.25rem 0.0625rem 0.5rem;
    }

    .panel__tag-chip button {
      background: transparent;
      border: 0;
      color: inherit;
      cursor: pointer;
      font-size: 0.75rem;
      opacity: 0.75;
      padding: 0 0.125rem;
    }

    .panel__tag-dropdown {
      background: var(--color-surface-elevated);
      border: 1px solid var(--color-border-base);
      border-radius: 0.5rem;
      box-shadow: 0 0.5rem 1.5rem color-mix(in srgb, black 18%, transparent);
      left: 0;
      overflow: hidden;
      position: absolute;
      right: 0;
      top: calc(100% + 0.25rem);
      z-index: 10;
    }

    .panel__tag-dropdown button {
      background: transparent;
      border: 0;
      color: var(--color-text-primary);
      cursor: pointer;
      display: flex;
      padding: 0.45rem 0.75rem;
      text-align: left;
      width: 100%;
    }

    .panel__tag-dropdown button:hover,
    .panel__tag-dropdown button:focus-visible {
      background: var(--color-surface-muted);
    }

    .panel__tag-dropdown span {
      background: var(--color-surface-muted);
      border-radius: 999px;
      color: var(--color-text-secondary);
      font-size: 0.625rem;
      font-weight: 600;
      padding: 0.125rem 0.45rem;
    }

    .panel__body {
      flex: 1;
      min-height: 0;
      overflow: auto;
      padding: 1.25rem;
    }

    .panel__table-shell {
      border: 1px solid var(--color-border-base);
      border-radius: 0.5rem;
      min-width: 52rem;
      overflow: hidden;
    }

    table {
      border-collapse: collapse;
      width: 100%;
    }

    th {
      background: var(--color-surface-muted);
      color: var(--color-text-muted);
      font-size: 0.68rem;
      font-weight: 800;
      letter-spacing: 0.04em;
      padding: 0.5rem 0.75rem;
      text-align: left;
      white-space: nowrap;
    }

    th button,
    th label {
      align-items: center;
      background: transparent;
      border: 0;
      color: inherit;
      display: inline-flex;
      font: inherit;
      gap: 0.25rem;
      letter-spacing: inherit;
      padding: 0;
    }

    th button {
      cursor: pointer;
    }

    th select {
      background: transparent;
      border: 0;
      color: inherit;
      font: inherit;
      max-width: 1.25rem;
      outline: 0;
    }

    .panel__delete-col {
      padding-inline: 0.375rem;
      width: 2.75rem;
    }

    .panel__sr-only {
      border: 0;
      clip: rect(0, 0, 0, 0);
      height: 1px;
      margin: -1px;
      overflow: hidden;
      padding: 0;
      position: absolute;
      white-space: nowrap;
      width: 1px;
    }

    .panel__empty,
    .panel__error {
      color: var(--color-text-muted);
      margin: 0;
      padding: 2.5rem;
      text-align: center;
    }

    .panel__error {
      color: var(--color-danger);
    }

    @media (max-width: 1023px) {
      .panel__body {
        padding: 0.875rem;
      }

      .panel__table-shell {
        min-width: 0;
      }

      .panel__toolbar {
        align-items: stretch;
        flex-direction: column;
      }

      .panel__tabs,
      .panel__search {
        width: 100%;
      }

      .panel__search input {
        font-size: 1rem;
      }

      th select {
        max-width: 5.5rem;
      }
    }

    @media (max-width: 767px) {
      .panel__header {
        padding-inline: 0.875rem;
      }

      .panel__body {
        padding: 0.75rem;
      }

      .panel__table-shell {
        overflow-x: auto;
      }

      th:nth-child(4),
      th:nth-child(6) {
        display: none;
      }

      .panel__import-btn {
        display: none;
      }

      .panel__back-btn .panel__back-label {
        display: none;
      }

      .panel__back-btn {
        padding: 3px 7px;
      }
    }

    :host ::ng-deep .ui-modal__header {
      align-items: flex-start;
    }

    .thread-tag-edit__header {
      flex: 1;
      min-width: 0;
      text-align: left;
    }

    .thread-tag-edit__title {
      color: var(--color-text-primary);
      font-size: 1rem;
      font-weight: 700;
      margin: 0;
      text-align: left;
      width: 100%;
    }

    .thread-tag-edit__subtitle {
      color: var(--color-text-muted);
      font-size: 0.75rem;
      margin: 0.25rem 0 0;
    }

    .thread-tag-edit {
      display: flex;
      flex-direction: column;
      gap: 0.75rem;
    }

    .thread-tag-edit__hint {
      color: var(--color-text-muted);
      font-size: 0.75rem;
      margin: 0;
    }

    .thread-tag-edit__error {
      color: var(--color-danger);
      font-size: 0.8125rem;
      margin: 0;
    }

    .thread-tag-edit__field {
      align-items: center;
      background: var(--color-surface-muted);
      border: 1px solid var(--color-border-base);
      border-radius: 0.5rem;
      display: flex;
      flex-wrap: wrap;
      gap: 0.375rem;
      min-height: 2.5rem;
      padding: 0.375rem 0.5rem;
    }

    .thread-tag-edit__chip {
      align-items: center;
      background: color-mix(in srgb, var(--color-accent) 14%, transparent);
      border: 1px solid var(--color-accent);
      border-radius: 999px;
      color: var(--color-accent);
      display: inline-flex;
      font-size: 0.75rem;
      font-weight: 600;
      gap: 0.125rem;
      max-width: 100%;
      padding: 0.125rem 0.25rem 0.125rem 0.5rem;
    }

    .thread-tag-edit__chip-remove {
      background: transparent;
      border: 0;
      color: inherit;
      cursor: pointer;
      font-size: 0.875rem;
      line-height: 1;
      opacity: 0.75;
      padding: 0 0.125rem;
    }

    .thread-tag-edit__chip-remove:hover {
      opacity: 1;
    }

    .thread-tag-edit__input {
      background: transparent;
      border: 0;
      color: var(--color-text-primary);
      flex: 1;
      font-size: 0.8125rem;
      min-width: 8rem;
      outline: 0;
      padding: 0.25rem 0.125rem;
    }

    .thread-tag-edit__footer {
      display: flex;
      gap: 0.5rem;
      justify-content: flex-end;
    }

    .thread-tag-edit__btn {
      border-radius: 0.5rem;
      cursor: pointer;
      font-size: 0.8125rem;
      font-weight: 600;
      padding: 0.4375rem 1rem;
    }

    .thread-tag-edit__btn--ghost {
      background: transparent;
      border: 1px solid var(--color-border-base);
      color: var(--color-text-primary);
    }

    .thread-tag-edit__btn--primary {
      background: var(--color-accent);
      border: 0;
      color: #fff;
    }
  `],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ThreadListPanelComponent implements OnInit {
  readonly activeThreadId = input<string | null>(null);
  readonly personalities = input<readonly Personality[]>([]);
  /** Monotonic counter; each increment (e.g. from a sidebar import action) opens the import modal. */
  readonly importTrigger = input<number>(0);
  readonly selectThread = output<string>();
  readonly newThread = output<void>();
  readonly threadListTab = signal<ThreadListTab>('active');
  readonly importOpen = signal(false);
  readonly selectedTagFilters = signal<string[]>([]);
  readonly tagDropdownFocused = signal(false);
  readonly tagEditThread = signal<Chat | null>(null);
  readonly tagEditChips = signal<string[]>([]);
  readonly tagInputValue = signal('');
  readonly tagEditError = signal<string | null>(null);
  readonly tagEditTitleId = `thread-tag-edit-${Math.random().toString(36).slice(2, 10)}`;
  readonly bulkActionMenuOpen = signal(false);
  readonly bulkPersonalityPickerOpen = signal(false);
  readonly bulkPersonalityTitleId = `thread-bulk-personality-${Math.random().toString(36).slice(2, 10)}`;
  readonly selectAllState = computed(() =>
    this.threads.selectionState(this.visibleThreads().map(thread => thread.id)),
  );
  readonly maxThreadTags = MAX_THREAD_TAGS;
  readonly maxThreadTagLength = MAX_THREAD_TAG_LENGTH;
  readonly tagOptions = computed(() => {
    const query = this.threads.query().trim().toLowerCase();
    if (!query) return [];
    const selected = new Set(this.selectedTagFilters().map(tag => tag.toLowerCase()));
    return this.threads.tags()
      .filter(tag => tag.toLowerCase().includes(query) && !selected.has(tag.toLowerCase()))
      .slice(0, 8);
  });
  readonly showTagDropdown = computed(() => this.tagDropdownFocused() && this.tagOptions().length > 0);
  readonly visibleThreads = computed(() => {
    const selected = this.selectedTagFilters();
    if (selected.length === 0) return this.threads.filteredThreads();
    return this.threads.filteredThreads().filter(thread => {
      const tags = new Set((thread.tags ?? []).map(tag => tag.toLowerCase()));
      return selected.every(tag => tags.has(tag.toLowerCase()));
    });
  });
  private readonly flatThreadIds = computed(() =>
    this.visibleThreads().map(thread => thread.id),
  );
  private readonly personalitiesById = computed(() => {
    const map = new Map<string, Personality>();
    for (const personality of this.personalities()) {
      map.set(personality.id, personality);
    }
    return map;
  });
  private readonly personalitiesByName = computed(() => {
    const map = new Map<string, Personality>();
    for (const personality of this.personalities()) {
      const key = personality.name.trim().toLowerCase();
      if (key && !map.has(key)) {
        map.set(key, personality);
      }
    }
    return map;
  });

  private readonly location = inject(Location);
  private readonly confirmation = inject(ConfirmationService);
  private lastImportTrigger = 0;

  constructor(readonly threads: ThreadListService) {
    effect(() => {
      const trigger = this.importTrigger();
      if (trigger > 0 && trigger !== this.lastImportTrigger) {
        this.lastImportTrigger = trigger;
        // Imports land in the archive — switch there before the modal opens so the
        // underlying list is already the right place when the user closes it.
        this.onThreadListTab('archived');
        this.importOpen.set(true);
      }
    });
  }

  goBack(): void {
    this.location.back();
  }

  ngOnInit(): void {
    this.threads.setArchivedPanelOnly(this.threadListTab() === 'archived');
  }

  onThreadListTab(tab: ThreadListTab): void {
    this.threadListTab.set(tab);
    this.threads.setArchivedPanelOnly(tab === 'archived');
  }

  /** After an import completes, surface the freshly created (archived) threads. */
  onConversationsImported(): void {
    this.onThreadListTab('archived');
    // Force reload even when we were already on the archived tab (setArchivedPanelOnly no-ops then).
    void this.threads.refresh();
  }

  /**
   * Closing the importer always lands on the Archived tab (imports are archived by default) and
   * refreshes so newly imported threads appear without a full page reload.
   */
  onImportClosed(): void {
    this.importOpen.set(false);
    this.onThreadListTab('archived');
    void this.threads.refresh();
  }

  async rename(thread: Chat, name: string): Promise<void> {
    await this.threads.renameThread(thread, name);
  }

  async togglePin(thread: Chat): Promise<void> {
    await this.threads.togglePinned(thread);
  }

  async deleteThread(thread: Chat): Promise<void> {
    const confirmed = await this.confirmation.confirm({
      title: 'Delete thread?',
      message: `Are you sure you want to delete "${thread.name}"? This cannot be undone.`,
      confirmText: 'Delete',
      cancelText: 'Cancel',
      type: 'danger',
    });
    if (!confirmed) return;
    await this.threads.deleteThread(thread);
  }

  async archiveThread(thread: Chat): Promise<void> {
    await this.threads.setThreadArchived(thread, true);
  }

  async restoreThread(thread: Chat): Promise<void> {
    await this.threads.setThreadArchived(thread, false);
  }

  onSelectAllChange(): void {
    const ids = this.visibleThreads().map(thread => thread.id);
    this.threads.setAllSelected(ids, this.selectAllState() !== 'all');
  }

  closeBulkMenuSoon(): void {
    setTimeout(() => this.bulkActionMenuOpen.set(false), 150);
  }

  async runBulkArchive(): Promise<void> {
    this.bulkActionMenuOpen.set(false);
    await this.threads.bulkSetArchived([...this.threads.selectedIds()], true);
  }

  async runBulkRestore(): Promise<void> {
    this.bulkActionMenuOpen.set(false);
    await this.threads.bulkSetArchived([...this.threads.selectedIds()], false);
  }

  async runBulkDelete(): Promise<void> {
    this.bulkActionMenuOpen.set(false);
    const count = this.threads.selectedCount();
    const confirmed = await this.confirmation.confirm({
      title: 'Delete threads?',
      message: `Are you sure you want to delete ${count} thread${count === 1 ? '' : 's'}? This cannot be undone.`,
      confirmText: 'Delete',
      cancelText: 'Cancel',
      type: 'danger',
    });
    if (!confirmed) return;
    await this.threads.bulkDelete([...this.threads.selectedIds()]);
  }

  openBulkPersonalityPicker(): void {
    this.bulkActionMenuOpen.set(false);
    this.bulkPersonalityPickerOpen.set(true);
  }

  closeBulkPersonalityPicker(): void {
    this.bulkPersonalityPickerOpen.set(false);
  }

  async assignBulkPersonality(personalityId: string): Promise<void> {
    await this.threads.bulkAssignPersonality([...this.threads.selectedIds()], personalityId);
    this.closeBulkPersonalityPicker();
  }

  async editTags(thread: Chat): Promise<void> {
    this.tagEditThread.set(thread);
    this.tagEditChips.set([...(thread.tags ?? [])]);
    this.tagInputValue.set('');
    this.tagEditError.set(null);
  }

  closeTagEdit(): void {
    this.tagEditThread.set(null);
    this.tagEditChips.set([]);
    this.tagInputValue.set('');
    this.tagEditError.set(null);
  }

  onTagInputField(event: Event): void {
    const value = (event.target as HTMLInputElement).value;
    this.tagInputValue.set(value);
    this.tagEditError.set(null);
  }

  onTagKeydown(event: KeyboardEvent): void {
    if (event.key === 'Enter' || event.key === ' ' || event.key === ',') {
      event.preventDefault();
      this.commitTagInput();
      return;
    }
    if (event.key === 'Backspace' && !this.tagInputValue()) {
      event.preventDefault();
      this.tagEditChips.update(tags => tags.slice(0, -1));
    }
  }

  commitTagInput(): void {
    const raw = this.tagInputValue().trim();
    if (!raw) return;
    const pieces = raw.split(',').map(s => s.trim()).filter(Boolean);
    for (const piece of pieces) {
      if (!this.tryAddTag(piece)) {
        break;
      }
    }
    this.tagInputValue.set('');
  }

  /** @returns false if a limit was hit and we should stop processing more pieces */
  private tryAddTag(tag: string): boolean {
    const normalized = tag.trim();
    if (!normalized) return true;
    if (normalized.length > MAX_THREAD_TAG_LENGTH) {
      this.tagEditError.set(`Each tag must be at most ${MAX_THREAD_TAG_LENGTH} characters.`);
      return false;
    }
    if (this.tagEditChips().length >= MAX_THREAD_TAGS) {
      this.tagEditError.set(`You can add at most ${MAX_THREAD_TAGS} tags.`);
      return false;
    }
    const lower = normalized.toLowerCase();
    const existing = new Set(this.tagEditChips().map(t => t.toLowerCase()));
    if (existing.has(lower)) {
      return true;
    }
    this.tagEditChips.update(tags => [...tags, normalized]);
    this.tagEditError.set(null);
    return true;
  }

  removeTagChip(index: number): void {
    this.tagEditChips.update(tags => tags.filter((_, i) => i !== index));
    this.tagEditError.set(null);
  }

  async saveTagEdit(): Promise<void> {
    const thread = this.tagEditThread();
    if (!thread) return;
    this.commitTagInput();
    if (this.tagEditError()) {
      return;
    }
    const ok = await this.threads.setTags(thread, this.tagEditChips());
    if (!ok) {
      const msg =
        this.threads.error()?.trim() || 'Could not save tags. Please try again.';
      this.tagEditError.set(msg);
      return;
    }
    this.closeTagEdit();
  }

  addTagFilter(tag: string, event: MouseEvent): void {
    event.preventDefault();
    if (!this.selectedTagFilters().includes(tag)) {
      this.selectedTagFilters.update(tags => [...tags, tag]);
    }
    this.threads.setQuery('');
    this.tagDropdownFocused.set(true);
  }

  removeTagFilter(tag: string): void {
    this.selectedTagFilters.update(tags => tags.filter(item => item !== tag));
  }

  removeLastTagIfEmpty(event: Event): void {
    if (this.threads.query() || this.selectedTagFilters().length === 0) return;
    event.preventDefault();
    this.selectedTagFilters.update(tags => tags.slice(0, -1));
  }

  closeTagDropdownSoon(): void {
    setTimeout(() => this.tagDropdownFocused.set(false), 150);
  }

  onListboxKeydown(event: KeyboardEvent): void {
    const ids = this.flatThreadIds();
    if (ids.length === 0) return;
    const current = this.activeThreadId();
    const currentIndex = current ? ids.indexOf(current) : -1;

    if (event.key === 'ArrowDown') {
      event.preventDefault();
      const nextIndex = currentIndex >= ids.length - 1 ? 0 : currentIndex + 1;
      this.selectThread.emit(ids[nextIndex]);
      return;
    }

    if (event.key === 'ArrowUp') {
      event.preventDefault();
      const nextIndex = currentIndex <= 0 ? ids.length - 1 : currentIndex - 1;
      this.selectThread.emit(ids[nextIndex]);
      return;
    }

    if (event.key === 'Enter' && current) {
      event.preventDefault();
      this.selectThread.emit(current);
    }
  }

  personalityForThread(thread: Chat): Personality | null {
    const byId = thread.personality_id ? this.personalitiesById().get(thread.personality_id) : null;
    if (byId) return byId;
    const byName = thread.personality_name?.trim()
      ? this.personalitiesByName().get(thread.personality_name.trim().toLowerCase())
      : null;
    return byName ?? null;
  }
}
