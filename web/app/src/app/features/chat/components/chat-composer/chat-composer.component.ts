import { CommonModule, AsyncPipe } from '@angular/common';
import {
  ChangeDetectionStrategy,
  Component,
  DestroyRef,
  ElementRef,
  HostListener,
  computed,
  effect,
  inject,
  input,
  output,
  signal,
  viewChild,
} from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { FormsModule } from '@angular/forms';
import { Subject, forkJoin, of } from 'rxjs';
import { catchError, finalize, map, switchMap } from 'rxjs/operators';

import { PickerComponent } from '@ctrl/ngx-emoji-mart';
import { EmojiData, EmojiEvent } from '@ctrl/ngx-emoji-mart/ngx-emoji';

import {
  FileAttachment,
  PendingFileAttachment,
  pendingAttachmentKey,
} from '../../../../core/models/file-attachment.model';
import { Model } from '../../../../core/models/model.model';
import { ChatMessage } from '../../../../core/models/message.model';
import { Ritual } from '../../../../core/models/ritual.model';
import { RitualService } from '../../../../core/services/ritual.service';
import { MoodService } from '../../../../core/services/mood.service';
import { ImageGalleryService } from '../../../../core/services/image-gallery.service';
import { Mood } from '../../../../core/models/mood.model';
import { MODE_PLURAL, MODE_SINGULAR } from '../../../mood/mode-ui.labels';
import { AuthImagePipe } from '../../../../core/pipes/auth-image.pipe';
import { MAX_FILE_SIZE_BYTES, isMimeOrExtensionAllowedForChatUpload } from '../../chat.constants';
import {
  matchCommands,
  parseSlash,
  resolveSlashCommand,
  SlashCommand,
} from '../../helpers/slash-command.helpers';
import { ModelPickerComponent } from '../model-picker/model-picker.component';
import { SlashMenuComponent } from '../slash-menu/slash-menu.component';
import { BoltIconComponent, FileIconComponent, ImageIconComponent, PlusIconComponent } from '../../../../shared/ui/icons/icons';
import { ModalComponent } from '../../../../shared/ui/modal/modal.component';
import {
  TEXT_LIMIT_HARD_MAX,
  TEXT_LIMIT_WARNING_THRESHOLD,
} from '../../../../core/constants/text-limits.constants';

const SLASH_COMMANDS: readonly SlashCommand[] = [
  { id: 'attach', label: 'Attach file', description: 'Upload a file', keywords: ['file', 'upload'] },
  { id: 'emoji', label: 'Emoji', description: 'Insert an emoji', keywords: ['reaction'] },
  { id: 'skill', label: 'Skill', description: 'Add a skill to send', keywords: ['ritual', 'rit', 'routine', 'skills'] },
  { id: 'gallery', label: 'Gallery image', description: 'Attach an image from the gallery', keywords: ['photo', 'image'] },
  { id: 'personality', label: 'Personality', description: 'Change chat personality', keywords: ['persona', 'character'] },
  { id: 'mode', label: MODE_SINGULAR, description: `Set the generation ${MODE_SINGULAR.toLowerCase()}`, keywords: ['emotion', 'mood'] },
];
const COMPOSER_DESKTOP_MAX_ROWS = 10;
const COMPOSER_MOBILE_MAX_ROWS = 8;
const COMPOSER_MOBILE_BREAKPOINT = 767;
const COMPOSER_EXPANDED_VIEWPORT_RATIO = 0.75;
const CHAT_LENGTH_HINT_THRESHOLD = 10_000;

@Component({
  selector: 'app-chat-composer',
  standalone: true,
  imports: [
    CommonModule,
    AsyncPipe,
    AuthImagePipe,
    FormsModule,
    PickerComponent,
    ModelPickerComponent,
    SlashMenuComponent,
    BoltIconComponent,
    FileIconComponent,
    ImageIconComponent,
    PlusIconComponent,
    ModalComponent,
  ],
  template: `
    <form class="composer" (submit)="onSubmit($event)" (drop)="onDrop($event)" (dragover)="onDragOver($event)" (dragleave)="isDragOver.set(false)">
      @if (quotaMessage()) {
        <p class="composer__quota" role="alert">{{ quotaMessage() }}</p>
      }

      @if (threadArchived()) {
        <p class="composer__archived" role="status">
          This thread is archived. Restore it from Thread Manager to continue the conversation.
        </p>
      }

      @if (generationRetryTarget()) {
        <div class="composer__retry-bar" role="status">
          <span class="composer__retry-text">Couldn’t complete the last reply.</span>
          <button
            type="button"
            class="composer__retry-btn"
            [disabled]="disabled()"
            (click)="retryGeneration.emit()"
          >
            Retry
          </button>
        </div>
      }

      @if (isDragOver()) {
        <div class="composer__drag" aria-hidden="true">Drop files to attach</div>
      }

      @if (pendingRituals().length) {
        <div class="composer__pending-skills" aria-label="Skills to send with this message">
          @for (r of pendingRituals(); track r.id) {
            <span class="composer__skill-chip">
              {{ r.name }}
              <button
                type="button"
                class="composer__skill-chip-remove"
                [disabled]="disabled()"
                [attr.aria-label]="'Remove skill ' + r.name"
                (click)="removePendingRitual(r.id)"
              >×</button>
            </span>
          }
        </div>
      }

      @if (attachments().length) {
        <div class="composer__attachments" aria-label="Pending attachments">
          @for (attachment of attachments(); track pendingAttachmentKey(attachment)) {
            <span class="composer__attachment" [class.composer__attachment--error]="attachment.uploadError">
              <span class="composer__attachment-name">{{ attachment.attachment?.name ?? attachment.file?.name }}</span>
              @if (attachment.isUploading) {
                <small>uploading</small>
              } @else if (attachment.uploadError) {
                <small>{{ attachment.uploadError }}</small>
              }
              <button
                type="button"
                class="composer__attachment-remove"
                [disabled]="disabled()"
                [attr.aria-label]="'Remove attachment ' + (attachment.attachment?.name ?? attachment.file?.name)"
                (click)="removeAttachment(pendingAttachmentKey(attachment))"
              >×</button>
            </span>
          }
        </div>
      }

      <div class="composer__body">
        <input #fileInput type="file" class="sr-only" multiple (change)="onFileInput($event)" />

        <div
          class="composer__unified-wrap"
          [class.composer__unified-wrap--animating]="inputManualHeight() === null"
          (click)="focusTextarea()"
        >
          <button
            type="button"
            class="composer__resizer"
            aria-label="chat input resizer"
            [class.composer__resizer--active]="inputExpanded()"
            (mousedown)="onResizerMouseDown($event)"
            (touchstart)="onResizerTouchStart($event)"
          >
            <svg width="11" height="11" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <polyline points="5,2 2,2 2,5"/>
              <polyline points="7,10 10,10 10,7"/>
            </svg>
          </button>

          <label class="sr-only" for="chat-composer-input">Message</label>
          <textarea
            #composerTextarea
            id="chat-composer-input"
            rows="1"
            enterkeyhint="enter"
            [value]="draft()"
            [disabled]="composerDisabled()"
            [placeholder]="textareaPlaceholder()"
            (input)="onInput($event)"
            (keydown)="onKeydown($event)"
            (paste)="onPaste($event)"
            (click)="$event.stopPropagation()"
          ></textarea>

          <div class="composer__controls-row" (click)="$event.stopPropagation()">
            <app-model-picker
              [models]="models()"
              [selectedId]="selectedModelId()"
              [disabled]="modelPickerDisabled()"
              (selected)="modelSelected.emit($event)"
            />

            <div class="composer__plus-anchor">
              <button
                type="button"
                class="composer__plus"
                [disabled]="disabled()"
                aria-label="Open chat options"
                [attr.aria-expanded]="plusOpen()"
                (click)="togglePlusMenu()"
              >
                <ui-plus-icon [size]="13" />
              </button>
              @if (emojiOpen()) {
                <div class="composer__emoji-popover" role="dialog" aria-label="Emoji picker">
                  <emoji-mart
                    [isNative]="true"
                    [emojiSize]="20"
                    [perLine]="8"
                    [enableSearch]="true"
                    [showPreview]="false"
                    [autoFocus]="true"
                    (emojiSelect)="onEmojiSelected($event)"
                  />
                </div>
              }
              @if (modePickerOpen()) {
                <div class="composer__mode-popover" role="dialog" [attr.aria-label]="'Choose ' + modeSingular">
                  <label class="composer__skill-search-label" for="composer-mode-filter">Filter modes</label>
                  <input
                    id="composer-mode-filter"
                    type="search"
                    class="composer__skill-search"
                    placeholder="Filter by name…"
                    [value]="modeFilter()"
                    [disabled]="disabled() || modeLoading()"
                    (input)="onModeFilterInput($event)"
                  />
                  @if (modeLoading()) {
                    <p class="composer__skill-status">Loading modes…</p>
                  } @else if (modeLoadError(); as err) {
                    <p class="composer__skill-status composer__skill-status--error" role="alert">{{ err }}</p>
                  } @else {
                    <ul class="composer__skill-list" role="listbox" [attr.aria-label]="'Available ' + modeSingularPlural">
                      <li role="none">
                        <button
                          type="button"
                          role="option"
                          class="composer__skill-row"
                          [class.composer__skill-row--selected]="isAutoMood()"
                          [disabled]="disabled()"
                          [attr.aria-selected]="isAutoMood()"
                          (click)="pickAutoMode()"
                        >
                          <span class="composer__skill-row-main">
                            <span class="composer__skill-name">Auto</span>
                            @if (isAutoMood() && activeMode(); as current) {
                              <span class="composer__skill-subtitle">Currently: {{ current.name }}</span>
                            }
                          </span>
                        </button>
                      </li>
                      @for (m of filteredModeOptions(); track m.id) {
                        <li role="none">
                          <button
                            type="button"
                            role="option"
                            class="composer__skill-row"
                            [class.composer__skill-row--selected]="!isAutoMood() && activeMoodId() === m.id"
                            [disabled]="disabled()"
                            [attr.aria-selected]="!isAutoMood() && activeMoodId() === m.id"
                            (click)="pickMode(m)"
                          >
                            <span class="composer__skill-name">{{ m.name }}</span>
                            @if (isAutoMood() && activeMoodId() === m.id) {
                              <span class="composer__skill-current">Current</span>
                            }
                          </button>
                        </li>
                      }
                    </ul>
                  }
                </div>
              }
              @if (skillPickerOpen()) {
                <div class="composer__skill-popover" role="dialog" aria-label="Choose skills">
                  <label class="composer__skill-search-label" for="composer-skill-filter">Filter skills</label>
                  <input
                    id="composer-skill-filter"
                    type="search"
                    class="composer__skill-search"
                    placeholder="Filter by name…"
                    [value]="skillFilter()"
                    [disabled]="disabled() || skillLoading()"
                    (input)="onSkillFilterInput($event)"
                  />
                  @if (skillLoading()) {
                    <p class="composer__skill-status">Loading skills…</p>
                  } @else if (skillLoadError(); as err) {
                    <p class="composer__skill-status composer__skill-status--error" role="alert">{{ err }}</p>
                  } @else if (filteredSkillOptions().length === 0) {
                    <p class="composer__skill-status">No skills match this chat or filter.</p>
                  } @else {
                    <ul class="composer__skill-list" role="listbox" aria-label="Available skills">
                      @for (r of filteredSkillOptions(); track r.id) {
                        <li role="none">
                          <button
                            type="button"
                            role="option"
                            class="composer__skill-row"
                            [disabled]="disabled() || isRitualPending(r.id)"
                            [attr.aria-selected]="isRitualPending(r.id)"
                            (click)="pickSkill(r)"
                          >
                            <span class="composer__skill-name">{{ r.name }}</span>
                            @if (isRitualPending(r.id)) {
                              <span class="composer__skill-added">Added</span>
                            }
                          </button>
                        </li>
                      }
                    </ul>
                  }
                </div>
              }
              @if (plusOpen()) {
                <div class="composer__plus-menu" role="menu">
                  <button type="button" role="menuitem" (click)="openEmojiPicker()">
                    <span class="composer__menu-emoji" aria-hidden="true">😊</span>
                    Emoji
                  </button>
                  <button type="button" role="menuitem" (click)="openSkillPicker()">
                    <ui-bolt-icon [size]="13" />
                    Skill
                  </button>
                  <button type="button" role="menuitem" (click)="openModePicker()" [attr.aria-label]="modeMenuAriaLabel()">
                    <span aria-hidden="true">◎</span>
                    {{ modeMenuLabel() }}
                  </button>
                  <button type="button" role="menuitem" (click)="openFilePicker()">
                    <ui-file-icon [size]="13" />
                    Attach file
                  </button>
                  <button type="button" role="menuitem" (click)="personaButtonClicked.emit(); plusOpen.set(false); emojiOpen.set(false)">
                    <span aria-hidden="true">{{ personaButtonLabel().charAt(0) }}</span>
                    {{ personaButtonLabel() }}
                  </button>
                  <button type="button" role="menuitem" (click)="openGalleryFromMenu()">
                    <ui-image-icon [size]="13" />
                    Add from Gallery
                  </button>
                </div>
              }
              @if (galleryOpen()) {
                <div class="composer__gallery-popover" role="dialog" aria-label="Pick from gallery">
                  @if (galleryLoading()) {
                    <p class="composer__gallery-status">Loading gallery…</p>
                  } @else if (galleryImages().length === 0) {
                    <p class="composer__gallery-status">No images in your gallery yet.</p>
                  } @else {
                    <div class="composer__gallery-grid">
                      @for (image of galleryImages(); track image.id) {
                        <button
                          type="button"
                          class="composer__gallery-thumb"
                          [disabled]="galleryBusy()"
                          [attr.aria-label]="'Attach ' + (image.name || 'image')"
                          (click)="selectGalleryImage(image)"
                        >
                          <img
                            [src]="(imageGallery.getImageUrl(image.id, 'thumbnail') | authImage | async) ?? ''"
                            [alt]="image.name"
                            loading="lazy"
                          />
                        </button>
                      }
                    </div>
                  }
                  @if (galleryBusy()) {
                    <p class="composer__gallery-status composer__gallery-status--inline">Adding…</p>
                  }
                </div>
              }
            </div>

            <div style="flex: 1"></div>

            @if (isGenerating()) {
              <button
                type="button"
                class="composer__stop"
                aria-label="Stop response"
                (click)="stop.emit()"
              >
                <span class="composer__stop-icon" aria-hidden="true"></span>
              </button>
            } @else {
              <button
                type="submit"
                class="composer__send"
                aria-label="Send message"
                [disabled]="composerDisabled() || hasUploadingAttachments() || !draft().trim() || isOverLimit()"
              >
                <svg width="13" height="13" viewBox="0 0 14 14" fill="none" stroke="white" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                  <path d="M 2.5 7 L 7 2 L 11.5 7 M 7 7 L 7 12"/>
                </svg>
              </button>
            }
          </div>
        </div>

      </div>

      @if (shouldShowLengthHint()) {
        <div class="composer__length" [class.composer__length--warning]="isNearLimit()" [class.composer__length--error]="isOverLimit()">
          <span>{{ characterCountLabel() }} / {{ hardLimitLabel }} characters</span>
          @if (isOverLimit()) {
            <span role="alert">Message exceeds the {{ hardLimitLabel }} character limit.</span>
          } @else if (isNearLimit()) {
            <span>Long messages may reduce response quality.</span>
          }
        </div>
      }

      <div class="composer__menu">
        <app-slash-menu
          #slashMenu
          [open]="slashOpen()"
          [commands]="filteredCommands()"
          (selected)="runCommand($event)"
          (closed)="slashOpen.set(false)"
        />
      </div>

      <ui-modal
        [open]="limitWarningOpen()"
        [labelledBy]="'chat-composer-limit-warning-title'"
        size="md"
        (dismiss)="limitWarningOpen.set(false)"
      >
        <div modal-header>
          <h2 id="chat-composer-limit-warning-title" class="text-base font-semibold text-(--color-text-primary)">
            Large message warning
          </h2>
        </div>
        <p class="text-sm text-(--color-text-secondary)">
          This message is over {{ warningLimitLabel }} characters and may be truncated or degrade output quality.
          Send anyway?
        </p>
        <div modal-footer class="flex justify-end gap-2">
          <button
            type="button"
            class="rounded-lg border border-(--color-border-default) px-3 py-1.5 text-sm font-medium text-(--color-text-primary) hover:bg-(--color-surface-elevated)"
            (click)="limitWarningOpen.set(false)"
          >Cancel</button>
          <button
            type="button"
            class="rounded-lg bg-(--color-accent) px-3 py-1.5 text-sm font-semibold text-white hover:brightness-95"
            (click)="confirmLimitWarningAndSend()"
          >Send anyway</button>
        </div>
      </ui-modal>
    </form>
  `,
  styles: [`
    :host {
      display: block;
    }

    .composer {
      border-top: 1px solid var(--color-border-base);
      display: grid;
      gap: 0.5rem;
      background: var(--color-surface-muted);
      padding: 0.625rem 3.25rem 0.625rem 1rem;
      position: relative;
    }

    .composer__body {
      margin-inline: auto;
      max-width: min(100%, 720px);
      position: relative;
      width: 100%;
    }

    .composer__unified-wrap {
      background: var(--color-surface-base);
      border: 1px solid var(--color-border-base);
      border-radius: 0.75rem;
      cursor: text;
      display: flex;
      flex-direction: column;
      position: relative;
    }

    .composer__unified-wrap--animating {
      transition: height 0.18s ease;
    }

    .composer__unified-wrap:focus-within {
      border-color: var(--color-border-base);
      box-shadow: none;
    }

    .composer__unified-wrap:focus-within .composer__controls-row {
      border-top-color: var(--color-accent-app, var(--color-accent));
    }

    .composer__resizer {
      position: absolute;
      top: 7px;
      right: 9px;
      width: 20px;
      height: 20px;
      border-radius: 5px;
      border: 1px solid var(--color-border-base);
      background: var(--color-surface-muted);
      cursor: ns-resize;
      display: flex;
      align-items: center;
      justify-content: center;
      color: var(--color-text-muted);
      z-index: 5;
      user-select: none;
      -webkit-user-select: none;
      touch-action: none;
      transition: background 0.12s, color 0.12s, border-color 0.12s;
    }

    .composer__resizer:hover,
    .composer__resizer--active {
      background: color-mix(in srgb, var(--color-accent) 10%, transparent);
      color: var(--color-accent);
      border-color: var(--color-border-hover);
    }

    textarea {
      background: transparent;
      border: none;
      border-radius: 0;
      box-sizing: border-box;
      color: var(--color-text-primary);
      flex: 0 0 auto;
      font-size: 0.8125rem;
      line-height: 1.6;
      height: 2.25rem;
      min-height: 2.25rem;
      outline: none;
      overflow-y: hidden;
      padding: 0.625rem 2rem 0.375rem 0.75rem;
      resize: none;
      scrollbar-width: thin;
      scrollbar-color: rgba(128, 128, 128, 0.4) transparent;
      width: 100%;
    }

    textarea:focus,
    textarea:focus-visible {
      border-radius: 0.75rem 0.75rem 0 0;
      outline: 1px solid var(--color-accent-app, var(--color-accent));
      outline-offset: -1px;
    }

    textarea::-webkit-scrollbar {
      width: 4px;
    }

    textarea::-webkit-scrollbar-track {
      background: transparent;
    }

    textarea::-webkit-scrollbar-thumb {
      background: rgba(128, 128, 128, 0.4);
      border-radius: 4px;
    }

    textarea::-webkit-scrollbar-thumb:hover {
      background: rgba(128, 128, 128, 0.65);
    }

    .composer__controls-row {
      align-items: center;
      border-top: 1px solid var(--color-border-base);
      display: flex;
      flex-shrink: 0;
      gap: 0.375rem;
      padding: 0.3125rem 0.5rem;
    }

    .composer__pending-skills {
      align-items: center;
      display: flex;
      flex-wrap: wrap;
      gap: 0.375rem;
      justify-content: flex-start;
      width: 100%;
    }

    .composer__skill-chip {
      align-items: center;
      background: color-mix(in srgb, var(--color-accent) 12%, var(--color-surface-base));
      border: 1px solid color-mix(in srgb, var(--color-accent) 35%, var(--color-border-base));
      border-radius: 999px;
      color: var(--color-text-secondary);
      display: inline-flex;
      font-size: 0.6875rem;
      font-weight: 600;
      gap: 0.25rem;
      max-width: 100%;
      padding: 0.125rem 0.25rem 0.125rem 0.5rem;
    }

    .composer__skill-chip-remove {
      background: transparent;
      border: 0;
      color: var(--color-text-muted);
      cursor: pointer;
      font-size: 0.875rem;
      line-height: 1;
      padding: 0 0.125rem;
    }

    .composer__skill-chip-remove:hover:not(:disabled) {
      color: var(--color-danger);
    }

    .composer__attachments {
      align-items: center;
      display: flex;
      flex-wrap: wrap;
      gap: 0.5rem;
      justify-content: flex-start;
    }

    .composer__attachment {
      align-items: center;
      border: 1px solid var(--color-border-base);
      border-radius: 999px;
      color: var(--color-text-secondary);
      display: inline-flex;
      font-size: 0.75rem;
      gap: 0.35rem;
      max-width: 100%;
      padding: 0.25rem 0.35rem 0.25rem 0.5rem;
    }

    .composer__attachment-name {
      max-width: 14rem;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .composer__attachment-remove {
      background: transparent;
      border: 0;
      color: var(--color-text-muted);
      cursor: pointer;
      flex-shrink: 0;
      font-size: 0.875rem;
      line-height: 1;
      padding: 0 0.125rem;
    }

    .composer__attachment-remove:hover:not(:disabled) {
      color: var(--color-danger);
    }

    .composer__attachments small {
      color: var(--color-text-muted);
      font-size: 0.625rem;
    }

    .composer__attachment--error {
      border-color: var(--color-danger);
    }

    .composer__attachment--error small {
      color: var(--color-danger);
    }

    .composer__send {
      align-items: center;
      background: var(--color-accent);
      border: 0;
      border-radius: 0.4375rem;
      cursor: pointer;
      display: inline-flex;
      flex-shrink: 0;
      height: 1.875rem;
      justify-content: center;
      width: 1.875rem;
    }

    .composer__send:hover:not(:disabled) {
      filter: brightness(1.1);
    }

    .composer__stop {
      align-items: center;
      background: var(--color-danger);
      border: 0;
      border-radius: 0.4375rem;
      cursor: pointer;
      display: inline-flex;
      flex-shrink: 0;
      height: 1.875rem;
      justify-content: center;
      width: 1.875rem;
    }

    .composer__stop:hover {
      filter: brightness(1.08);
    }

    .composer__stop-icon {
      background: white;
      border-radius: 2px;
      display: block;
      height: 0.5rem;
      width: 0.5rem;
    }

    .composer__plus-anchor {
      display: flex;
      position: relative;
    }

    .composer__plus {
      align-items: center;
      background: transparent;
      border: 1px solid var(--color-border-base);
      border-radius: 0.5rem;
      box-sizing: border-box;
      color: var(--color-text-secondary);
      cursor: pointer;
      display: inline-flex;
      flex: 0 0 2.25rem;
      height: 2.25rem;
      justify-content: center;
      width: 2.25rem;
    }

    .composer__plus-menu {
      background: var(--color-surface-elevated, var(--color-surface-base));
      border: 1px solid var(--color-border-base);
      border-radius: 0.5rem;
      bottom: calc(100% + 0.375rem);
      box-shadow: 0 0.5rem 1.5rem color-mix(in srgb, black 20%, transparent);
      display: grid;
      left: 0;
      min-width: 13rem;
      padding: 0.25rem;
      position: absolute;
      z-index: 50;
    }

    .composer__emoji-popover {
      background: var(--color-surface-elevated, var(--color-surface-base));
      border: 1px solid var(--color-border-base);
      border-radius: 0.5rem;
      bottom: calc(100% + 0.375rem);
      box-shadow: 0 0.5rem 1.5rem color-mix(in srgb, black 20%, transparent);
      left: 0;
      max-height: min(20.5rem, 50vh);
      overflow: hidden;
      position: absolute;
      z-index: 60;
    }

    .composer__emoji-popover emoji-mart,
    .composer__emoji-popover ::ng-deep .emoji-mart {
      border: 0;
      width: min(20.5rem, 92vw);
    }

    .composer__mode-popover {
      background: var(--color-surface-elevated, var(--color-surface-base));
      border: 1px solid var(--color-border-base);
      border-radius: 0.5rem;
      bottom: calc(100% + 0.375rem);
      box-shadow: 0 0.5rem 1.5rem color-mix(in srgb, black 20%, transparent);
      display: flex;
      flex-direction: column;
      gap: 0.375rem;
      left: 0;
      max-height: min(20rem, 50vh);
      max-width: min(22rem, 92vw);
      min-width: 14rem;
      overflow: hidden;
      padding: 0.5rem;
      position: absolute;
      z-index: 58;
    }

    .composer__skill-popover {
      background: var(--color-surface-elevated, var(--color-surface-base));
      border: 1px solid var(--color-border-base);
      border-radius: 0.5rem;
      bottom: calc(100% + 0.375rem);
      box-shadow: 0 0.5rem 1.5rem color-mix(in srgb, black 20%, transparent);
      display: flex;
      flex-direction: column;
      gap: 0.375rem;
      left: 0;
      max-height: min(20rem, 50vh);
      max-width: min(22rem, 92vw);
      min-width: 14rem;
      overflow: hidden;
      padding: 0.5rem;
      position: absolute;
      z-index: 58;
    }

    .composer__skill-search-label {
      position: absolute;
      width: 1px;
      height: 1px;
      padding: 0;
      margin: -1px;
      overflow: hidden;
      clip: rect(0, 0, 0, 0);
      white-space: nowrap;
      border: 0;
    }

    .composer__skill-search {
      background: var(--color-surface-input, var(--color-surface-base));
      border: 1px solid var(--color-border-base);
      border-radius: 0.375rem;
      color: var(--color-text-primary);
      font-size: 0.75rem;
      padding: 0.375rem 0.5rem;
      width: 100%;
    }

    .composer__skill-status {
      color: var(--color-text-muted);
      font-size: 0.75rem;
      margin: 0;
      padding: 0.25rem 0;
    }

    .composer__skill-status--error {
      color: var(--color-danger);
    }

    .composer__skill-list {
      list-style: none;
      margin: 0;
      max-height: min(14rem, 40vh);
      overflow-y: auto;
      padding: 0;
    }

    .composer__skill-row {
      align-items: center;
      background: transparent;
      border: 0;
      border-radius: 0.375rem;
      color: var(--color-text-primary);
      cursor: pointer;
      display: flex;
      font-size: 0.75rem;
      gap: 0.5rem;
      justify-content: space-between;
      padding: 0.4375rem 0.5rem;
      text-align: left;
      width: 100%;
    }

    .composer__skill-row:hover:not(:disabled) {
      background: color-mix(in srgb, var(--color-accent) 10%, transparent);
    }

    .composer__skill-row--selected {
      background: color-mix(in srgb, var(--color-accent) 14%, transparent);
      font-weight: 600;
    }

    .composer__skill-row-main {
      display: flex;
      flex-direction: column;
      gap: 0.125rem;
      min-width: 0;
    }

    .composer__skill-subtitle {
      color: var(--color-text-muted);
      font-size: 0.625rem;
      font-weight: 500;
    }

    .composer__skill-current {
      color: var(--color-text-muted);
      flex-shrink: 0;
      font-size: 0.625rem;
      font-weight: 600;
      text-transform: uppercase;
    }

    .composer__skill-name {
      min-width: 0;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .composer__skill-added {
      color: var(--color-text-muted);
      flex-shrink: 0;
      font-size: 0.625rem;
      font-weight: 600;
      text-transform: uppercase;
    }

    .composer__gallery-popover {
      background: var(--color-surface-elevated, var(--color-surface-base));
      border: 1px solid var(--color-border-base);
      border-radius: 0.5rem;
      bottom: calc(100% + 0.375rem);
      box-shadow: 0 0.5rem 1.5rem color-mix(in srgb, black 20%, transparent);
      left: 0;
      max-height: min(20rem, 50vh);
      max-width: min(22rem, 92vw);
      min-width: 14rem;
      overflow: auto;
      padding: 0.5rem;
      position: absolute;
      z-index: 55;
    }

    .composer__gallery-grid {
      display: grid;
      gap: 0.35rem;
      grid-template-columns: repeat(auto-fill, minmax(3.25rem, 1fr));
    }

    .composer__gallery-thumb {
      aspect-ratio: 1;
      background: var(--color-surface-muted);
      border: 1px solid var(--color-border-base);
      border-radius: 0.375rem;
      cursor: pointer;
      overflow: hidden;
      padding: 0;
      width: 100%;
    }

    .composer__gallery-thumb:disabled {
      cursor: wait;
      opacity: 0.55;
    }

    .composer__gallery-thumb img {
      display: block;
      height: 100%;
      object-fit: cover;
      width: 100%;
    }

    .composer__gallery-status {
      color: var(--color-text-muted);
      font-size: 0.75rem;
      margin: 0;
      padding: 0.5rem;
    }

    .composer__gallery-status--inline {
      padding-top: 0.25rem;
      text-align: center;
    }

    .composer__menu-emoji {
      display: inline-flex;
      font-size: 0.875rem;
      line-height: 1;
      width: 1.125rem;
      justify-content: center;
    }

    .composer__plus-menu button {
      align-items: center;
      background: transparent;
      border: 0;
      border-radius: 0.375rem;
      color: var(--color-text-primary);
      cursor: pointer;
      display: flex;
      font-size: 0.75rem;
      gap: 0.5rem;
      padding: 0.4375rem 0.625rem;
      text-align: left;
    }

    .composer__plus-menu button:hover:not(:disabled),
    .composer__plus-menu button:focus-visible {
      background: color-mix(in srgb, var(--color-accent) 12%, transparent);
    }

    button:disabled {
      cursor: not-allowed;
      opacity: 0.5;
    }

    .composer__menu {
      bottom: calc(100% - 1rem);
      left: 50%;
      max-width: 26rem;
      position: absolute;
      transform: translateX(-50%);
      width: min(100%, 26rem, calc(100% - 2rem));
    }

    .composer__quota {
      color: var(--color-danger);
      font-size: 0.875rem;
      margin: 0;
      margin-inline: auto;
      max-width: min(100%, 120rem);
      width: 100%;
    }

    .composer__length {
      color: var(--color-text-muted);
      display: flex;
      font-size: 0.75rem;
      gap: 0.5rem;
      justify-content: space-between;
      margin-inline: auto;
      max-width: min(100%, 120rem);
      width: 100%;
    }

    .composer__length--warning {
      color: var(--color-warning, #d97706);
    }

    .composer__length--error {
      color: var(--color-danger);
    }

    .composer__archived {
      background: color-mix(in srgb, var(--color-text-muted) 8%, transparent);
      border-radius: 0.5rem;
      color: var(--color-text-muted);
      font-size: 0.8125rem;
      margin: 0;
      margin-inline: auto;
      max-width: min(100%, 120rem);
      padding: 0.5rem 0.75rem;
      width: 100%;
    }

    .composer:has(.composer__archived) .composer__body {
      opacity: 0.72;
    }

    .composer__retry-bar {
      align-items: center;
      background: color-mix(in srgb, var(--color-danger) 8%, var(--color-surface-base));
      border: 1px solid color-mix(in srgb, var(--color-danger) 28%, var(--color-border-base));
      border-radius: 0.5rem;
      color: var(--color-text-secondary);
      display: flex;
      flex-wrap: wrap;
      font-size: 0.75rem;
      gap: 0.5rem;
      justify-content: space-between;
      margin-inline: auto;
      max-width: min(100%, 120rem);
      padding: 0.375rem 0.625rem;
      width: 100%;
    }

    .composer__retry-text {
      flex: 1;
      min-width: 10rem;
    }

    .composer__retry-btn {
      border-radius: 999px;
      border: 1px solid var(--color-border-base);
      background: var(--color-surface-base);
      color: var(--color-text-primary);
      flex-shrink: 0;
      font-size: 0.6875rem;
      font-weight: 600;
      padding: 0.25rem 0.75rem;
    }

    .composer__drag {
      align-items: center;
      background: color-mix(in srgb, var(--color-accent) 16%, transparent);
      border: 2px dashed var(--color-accent);
      border-radius: 1.25rem;
      display: flex;
      inset: 0.5rem;
      justify-content: center;
      position: absolute;
      z-index: 2;
    }

    @media (max-width: 1023px) {
      .composer {
        padding-right: 1rem;
      }

      textarea {
        font-size: 1rem;
      }

      .composer__skill-search {
        font-size: 1rem;
      }
    }

    @media (max-width: 767px) {
      .composer__send,
      .composer__stop {
        min-height: 2.75rem;
        min-width: 2.75rem;
      }

      .composer__plus {
        min-height: 2.75rem;
        min-width: 2.75rem;
      }

      .composer__plus-menu {
        position: fixed;
        bottom: 0;
        left: 0;
        right: 0;
        top: auto;
        border-radius: 0.875rem 0.875rem 0 0;
        min-width: unset;
        max-height: 50vh;
        display: grid;
        grid-template-columns: 1fr 1fr;
        gap: 4px;
        padding: 8px 6px calc(env(safe-area-inset-bottom, 0px) + 14px);
      }

      .composer__plus-menu button {
        flex-direction: column;
        gap: 5px;
        padding: 14px 8px;
        font-size: 12px;
        border-radius: 8px;
        justify-content: center;
      }
    }
  `],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ChatComposerComponent {
  readonly pendingAttachmentKey = pendingAttachmentKey;

  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly destroyRef = inject(DestroyRef);
  private readonly ritualService = inject(RitualService);
  private readonly moodService = inject(MoodService);
  readonly imageGallery = inject(ImageGalleryService);
  readonly modeSingular = MODE_SINGULAR;
  readonly modeSingularPlural = MODE_PLURAL;
  readonly draft = input('');
  readonly attachments = input<readonly PendingFileAttachment[]>([]);
  readonly chatId = input<string | null>(null);
  readonly pendingRituals = input<readonly Ritual[]>([]);
  readonly models = input<readonly Model[]>([]);
  readonly selectedModelId = input<string | null>(null);
  readonly selectedPersonalityName = input<string | null>(null);
  readonly disabled = input(false);
  /** When false while {@link disabled} is true, user can still change model (e.g. switch to a free-tier model). */
  readonly modelPickerDisabled = input(false);
  readonly threadArchived = input(false);
  readonly composerPlaceholder = input<string | null>(null);
  readonly quotaMessage = input<string | null>(null);
  readonly generationRetryTarget = input<ChatMessage | null>(null);
  readonly isGenerating = input(false);
  readonly personalityId = input<string | null>(null);
  readonly activeMoodId = input<string | null>(null);
  readonly isAutoMood = input(true);

  readonly draftChange = output<string>();
  readonly send = output<string>();
  readonly filesSelected = output<File[]>();
  readonly pendingRitualsChange = output<readonly Ritual[]>();
  readonly commandSelected = output<SlashCommand>();
  readonly modelSelected = output<Model>();
  readonly personaButtonClicked = output<void>();
  readonly retryGeneration = output<void>();
  readonly stop = output<void>();
  readonly galleryAttachment = output<FileAttachment>();
  readonly attachmentRemoved = output<string>();
  readonly modeSelected = output<string | null>();

  readonly textareaPlaceholder = computed(() => {
    const custom = this.composerPlaceholder()?.trim();
    if (custom) {
      return custom;
    }
    const name = this.selectedPersonalityName()?.trim() || '';
    return name
      ? `Message ${name}... (type / for skills)`
      : 'Message... (type / for skills)';
  });
  readonly personaButtonLabel = computed(() => this.selectedPersonalityName() || 'Pick personality');
  readonly personaButtonAriaLabel = computed(() => {
    const name = this.selectedPersonalityName();
    return name ? `Change personality (currently ${name})` : 'Pick a personality';
  });

  readonly fileInput = viewChild<ElementRef<HTMLInputElement>>('fileInput');
  readonly textareaRef = viewChild<ElementRef<HTMLTextAreaElement>>('composerTextarea');
  readonly slashOpen = signal(false);
  readonly slashQuery = signal('');
  private readonly slashMenu = viewChild<SlashMenuComponent>('slashMenu');
  readonly isDragOver = signal(false);
  readonly plusOpen = signal(false);
  readonly emojiOpen = signal(false);
  readonly skillPickerOpen = signal(false);
  readonly skillLoading = signal(false);
  readonly skillLoadError = signal<string | null>(null);
  readonly skillOptions = signal<Ritual[]>([]);
  readonly skillFilter = signal('');
  readonly galleryOpen = signal(false);
  readonly galleryLoading = signal(false);
  readonly galleryBusy = signal(false);
  readonly galleryImages = signal<FileAttachment[]>([]);
  readonly modePickerOpen = signal(false);
  readonly modeLoading = signal(false);
  readonly modeLoadError = signal<string | null>(null);
  readonly modeOptions = signal<Mood[]>([]);
  readonly modeFilter = signal('');
  readonly inputExpanded = signal(false);
  readonly inputManualHeight = signal<number | null>(null);
  /**
   * True on devices whose primary input is a soft keyboard (phones/tablets):
   * `(pointer: coarse) and (hover: none)`. Touchscreen laptops report a fine
   * primary pointer + hover, so they stay on the desktop Enter-to-send path.
   * On soft-keyboard devices Enter inserts a newline and the Send button submits.
   */
  readonly softKeyboard = signal(detectSoftKeyboard());
  private textareaResizeRaf: number | null = null;
  private activeResizeDrag: {
    removeMove: (fn: (e: MouseEvent | TouchEvent) => void) => void;
    removeUp: (fn: () => void) => void;
    onMove: (e: MouseEvent | TouchEvent) => void;
    onUp: () => void;
  } | null = null;
  private readonly modePickerLoads = new Subject<void>();
  readonly hardLimit = TEXT_LIMIT_HARD_MAX;
  readonly hardLimitLabel = TEXT_LIMIT_HARD_MAX.toLocaleString();
  readonly warningLimitLabel = TEXT_LIMIT_WARNING_THRESHOLD.toLocaleString();
  readonly hasUploadingAttachments = computed(() => this.attachments().some(attachment => attachment.isUploading));
  readonly composerDisabled = computed(() => this.disabled() || this.threadArchived());
  readonly characterCount = computed(() => this.draft().length);
  readonly characterCountLabel = computed(() => this.characterCount().toLocaleString());
  readonly isOverLimit = computed(() => this.characterCount() > TEXT_LIMIT_HARD_MAX);
  readonly isNearLimit = computed(
    () => this.characterCount() >= TEXT_LIMIT_WARNING_THRESHOLD && !this.isOverLimit(),
  );
  readonly shouldShowLengthHint = computed(
    () => this.characterCount() >= CHAT_LENGTH_HINT_THRESHOLD || this.isNearLimit() || this.isOverLimit(),
  );
  readonly limitWarningOpen = signal(false);
  private readonly limitWarningAcknowledged = signal(false);
  readonly filteredModeOptions = computed(() => {
    const pid = this.personalityId();
    const q = this.modeFilter().trim().toLowerCase();
    let opts = this.modeOptions();
    if (pid) {
      opts = opts.filter(m => (m.personality_ids ?? []).includes(pid));
    }
    if (!q) return opts;
    return opts.filter(m => m.name.toLowerCase().includes(q));
  });
  readonly isAutoMode = computed(() => this.isAutoMood());
  readonly activeMode = computed(() =>
    this.modeOptions().find(m => m.id === this.activeMoodId()) ?? null,
  );
  readonly modeMenuLabel = computed(() => {
    if (!this.personalityId()) {
      return this.modeSingular;
    }
    if (this.isAutoMood()) {
      const name = this.activeMode()?.name;
      return name ? `${this.modeSingular} · Auto (${name})` : `${this.modeSingular} · Auto`;
    }
    const name = this.activeMode()?.name;
    return name ? `${this.modeSingular} · ${name}` : this.modeSingular;
  });
  readonly modeMenuAriaLabel = computed(() => {
    if (!this.personalityId()) {
      return `Choose ${this.modeSingular.toLowerCase()}`;
    }
    if (this.isAutoMood()) {
      const name = this.activeMode()?.name;
      return name
        ? `${this.modeSingular} set to Auto. Currently ${name}.`
        : `${this.modeSingular} set to Auto.`;
    }
    const name = this.activeMode()?.name ?? 'a specific mode';
    return `${this.modeSingular} locked to ${name}.`;
  });
  readonly filteredSkillOptions = computed(() => {
    const q = this.skillFilter().trim().toLowerCase();
    const opts = this.skillOptions();
    if (!q) return opts;
    return opts.filter(r => r.name.toLowerCase().includes(q));
  });

  readonly filteredCommands = computed(() => {
    const query = this.slashQuery();
    return query ? matchCommands(query, SLASH_COMMANDS) : [...SLASH_COMMANDS];
  });

  constructor() {
    effect(() => {
      this.draft();
      this.inputExpanded();
      this.scheduleTextareaResize();
    });

    effect(() => {
      const chatId = this.chatId();
      const personalityId = this.personalityId();
      if (chatId && personalityId) {
        this.modePickerLoads.next();
      }
    });

    this.modePickerLoads
      .pipe(
        switchMap(() => {
          this.modeLoading.set(true);
          this.modeLoadError.set(null);
          return this.moodService.listMoods(1, 100).pipe(
            map(res => ({ results: res.results ?? [], error: null as string | null })),
            catchError(() =>
              of({
                results: [] as Mood[],
                error: `Could not load ${MODE_PLURAL.toLowerCase()}.`,
              }),
            ),
            finalize(() => this.modeLoading.set(false)),
          );
        }),
        takeUntilDestroyed(this.destroyRef),
      )
      .subscribe(({ results, error }) => {
        this.modeOptions.set(results);
        this.modeLoadError.set(error);
      });

    this.destroyRef.onDestroy(() => this.teardownResizeDrag());
  }

  onSkillFilterInput(event: Event): void {
    this.skillFilter.set((event.target as HTMLInputElement).value);
  }

  onModeFilterInput(event: Event): void {
    this.modeFilter.set((event.target as HTMLInputElement).value);
  }

  onInput(event: Event): void {
    const value = (event.target as HTMLTextAreaElement).value;
    this.draftChange.emit(value);
    this.limitWarningAcknowledged.set(false);
    if (this.limitWarningOpen()) {
      this.limitWarningOpen.set(false);
    }
    const parsed = parseSlash(value);
    this.slashOpen.set(parsed.command !== null);
    this.slashQuery.set(parsed.command ?? '');
    this.scheduleTextareaResize();
  }

  onKeydown(event: KeyboardEvent): void {
    // On soft-keyboard devices Enter is a newline; submitting happens via the
    // Send button. This avoids needing a Shift key those devices don't have.
    const enterSends = event.key === 'Enter' && !event.shiftKey && !this.softKeyboard();
    if (this.slashOpen()) {
      if (event.key === 'Escape') {
        event.preventDefault();
        this.slashOpen.set(false);
        return;
      }
      if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
        event.preventDefault();
        this.slashMenu()?.onKeydown(event);
        return;
      }
      if (enterSends) {
        event.preventDefault();
        if (this.slashMenu()) {
          this.slashMenu()!.selectHighlighted();
        } else {
          this.tryRunSlashOnlyDraft();
        }
        return;
      }
    }
    if (enterSends) {
      event.preventDefault();
      if (this.tryRunSlashOnlyDraft()) {
        return;
      }
      this.submitDraft();
    }
  }

  onSubmit(event: Event): void {
    event.preventDefault();
    this.submitDraft();
  }

  runCommand(command: SlashCommand): void {
    this.clearSlashFromDraft();
    this.slashOpen.set(false);
    switch (command.id) {
      case 'attach':
        this.openFilePicker();
        return;
      case 'emoji':
        this.openEmojiPicker();
        return;
      case 'skill':
        this.openSkillPicker();
        return;
      case 'gallery':
        this.openGalleryPicker();
        return;
      case 'personality':
        this.closeAuxiliaryPopovers();
        this.personaButtonClicked.emit();
        return;
      case 'mode':
        this.openModePicker();
        return;
      default:
        this.commandSelected.emit(command);
    }
  }

  private clearSlashFromDraft(): void {
    if (parseSlash(this.draft()).command === null) return;
    this.draftChange.emit('');
  }

  private closeAuxiliaryPopovers(): void {
    this.plusOpen.set(false);
    this.emojiOpen.set(false);
    this.skillPickerOpen.set(false);
    this.galleryOpen.set(false);
    this.modePickerOpen.set(false);
  }

  togglePlusMenu(): void {
    const submenuOpen =
      this.emojiOpen() ||
      this.skillPickerOpen() ||
      this.modePickerOpen() ||
      this.galleryOpen();

    if (submenuOpen) {
      this.closeAuxiliaryPopovers();
      return;
    }

    this.plusOpen.update(v => !v);
  }

  openGalleryFromMenu(): void {
    this.plusOpen.set(false);
    this.emojiOpen.set(false);
    this.skillPickerOpen.set(false);
    this.modePickerOpen.set(false);
    this.openGalleryPicker();
  }

  openGalleryPicker(): void {
    this.slashOpen.set(false);
    this.emojiOpen.set(false);
    this.skillPickerOpen.set(false);
    this.modePickerOpen.set(false);
    this.galleryOpen.set(true);
    this.galleryLoading.set(true);
    this.galleryBusy.set(false);
    this.galleryImages.set([]);
    this.imageGallery
      .listImages(1, 30)
      .pipe(
        takeUntilDestroyed(this.destroyRef),
        finalize(() => this.galleryLoading.set(false)),
      )
      .subscribe({
        next: res => this.galleryImages.set(res.results ?? []),
        error: () => this.galleryImages.set([]),
      });
  }

  selectGalleryImage(image: FileAttachment): void {
    if (this.galleryBusy()) return;
    this.galleryBusy.set(true);
    this.imageGallery
      .referenceImage(image.id)
      .pipe(
        takeUntilDestroyed(this.destroyRef),
        finalize(() => this.galleryBusy.set(false)),
      )
      .subscribe({
        next: attachment => {
          this.galleryOpen.set(false);
          this.galleryAttachment.emit(attachment);
        },
        error: () => {},
      });
  }

  openEmojiPicker(): void {
    this.plusOpen.set(false);
    this.skillPickerOpen.set(false);
    this.galleryOpen.set(false);
    this.modePickerOpen.set(false);
    this.emojiOpen.set(true);
  }

  openModePicker(): void {
    this.plusOpen.set(false);
    this.emojiOpen.set(false);
    this.skillPickerOpen.set(false);
    this.galleryOpen.set(false);
    this.slashOpen.set(false);
    this.modePickerOpen.set(true);
    this.modeFilter.set('');
    if (!this.chatId()) {
      this.modeLoadError.set('Open a chat to pick a mode.');
      this.modeOptions.set([]);
      this.modeLoading.set(false);
      return;
    }
    this.modePickerLoads.next();
  }

  pickMode(mood: Mood): void {
    this.modeSelected.emit(mood.id);
    this.modePickerOpen.set(false);
  }

  pickAutoMode(): void {
    this.modeSelected.emit(null);
    this.modePickerOpen.set(false);
  }

  openSkillPicker(): void {
    this.plusOpen.set(false);
    this.emojiOpen.set(false);
    this.galleryOpen.set(false);
    this.modePickerOpen.set(false);
    this.slashOpen.set(false);
    this.skillPickerOpen.set(true);
    this.skillFilter.set('');
    const cid = this.chatId();
    if (!cid) {
      this.skillLoadError.set('Open a chat to pick skills.');
      this.skillOptions.set([]);
      this.skillLoading.set(false);
      return;
    }
    this.skillLoading.set(true);
    this.skillLoadError.set(null);
    forkJoin({
      chat: this.ritualService.getAvailableRituals(cid, 1, 100),
      system: this.ritualService.listSystemRituals(),
    })
      .pipe(
        takeUntilDestroyed(this.destroyRef),
        finalize(() => this.skillLoading.set(false)),
      )
      .subscribe({
        next: ({ chat, system }) => {
          this.skillOptions.set(mergeRitualOptions(chat.results ?? [], system));
          this.skillLoadError.set(null);
        },
        error: () => {
          this.skillOptions.set([]);
          this.skillLoadError.set('Could not load skills.');
        },
      });
  }

  pickSkill(ritual: Ritual): void {
    if (this.disabled() || this.isRitualPending(ritual.id)) {
      return;
    }
    this.pendingRitualsChange.emit([...this.pendingRituals(), ritual]);
    this.skillPickerOpen.set(false);
  }

  removePendingRitual(id: string): void {
    this.pendingRitualsChange.emit(this.pendingRituals().filter(r => r.id !== id));
  }

  removeAttachment(key: string): void {
    this.attachmentRemoved.emit(key);
  }

  isRitualPending(id: string): boolean {
    return this.pendingRituals().some(r => r.id === id);
  }

  openFilePicker(): void {
    this.closeAuxiliaryPopovers();
    this.fileInput()?.nativeElement.click();
  }

  onEmojiSelected(event: EmojiEvent): void {
    const ch = emojiCharFromData(event.emoji);
    if (!ch) return;
    this.insertAtCursor(ch);
  }

  private insertAtCursor(insert: string): void {
    const ta = this.textareaRef()?.nativeElement;
    const value = this.draft();
    if (!ta) {
      this.draftChange.emit(value + insert);
      return;
    }
    const start = ta.selectionStart ?? value.length;
    const end = ta.selectionEnd ?? value.length;
    ta.focus();
    if (typeof document.execCommand === 'function' && document.execCommand('insertText', false, insert)) {
      this.draftChange.emit(ta.value);
      const pos = start + insert.length;
      setTimeout(() => {
        ta.focus();
        ta.setSelectionRange(pos, pos);
      }, 0);
      return;
    }
    const next = value.slice(0, start) + insert + value.slice(end);
    ta.value = next;
    this.draftChange.emit(next);
    const pos = start + insert.length;
    setTimeout(() => {
      ta.focus();
      ta.setSelectionRange(pos, pos);
    }, 0);
  }

  onFileInput(event: Event): void {
    const input = event.target as HTMLInputElement;
    const files = Array.from(input.files ?? []).filter(isAllowedFile);
    input.value = '';
    this.filesSelected.emit(files);
  }

  onDragOver(event: DragEvent): void {
    event.preventDefault();
    this.isDragOver.set(true);
  }

  onDrop(event: DragEvent): void {
    event.preventDefault();
    this.isDragOver.set(false);
    const files = Array.from(event.dataTransfer?.files ?? []).filter(isAllowedFile);
    this.filesSelected.emit(files);
  }

  onPaste(event: ClipboardEvent): void {
    const clipboard = event.clipboardData;
    if (!clipboard) return;

    const files: File[] = [];
    for (const item of Array.from(clipboard.items ?? [])) {
      if (item.kind === 'file') {
        const file = item.getAsFile();
        if (file) {
          files.push(ensurePastedFileName(file));
        }
      }
    }
    if (files.length > 0) {
      event.preventDefault();
      const allowed = files.filter(isAllowedFile);
      if (allowed.length > 0) {
        this.filesSelected.emit(allowed);
      }
    }
  }

  @HostListener('window:resize')
  onWindowResize(): void {
    this.scheduleTextareaResize();
  }

  @HostListener('document:click', ['$event'])
  onDocumentClick(event: Event): void {
    const target = event.target;
    if (!(target instanceof Node)) return;

    // Slash menu clicks open a popover in the same tick; ignore them here so we
    // do not immediately close the popover we just opened.
    if (target instanceof Element && target.closest('.slash-menu')) {
      return;
    }

    if (this.emojiOpen() && target instanceof Element) {
      const inPopover = target.closest('.composer__emoji-popover');
      const inPlusAnchor = target.closest('.composer__plus-anchor');
      if (!inPopover && !inPlusAnchor) {
        this.emojiOpen.set(false);
      }
    }

    if (this.skillPickerOpen() && target instanceof Element) {
      const inSkill = target.closest('.composer__skill-popover');
      const inPlusAnchor = target.closest('.composer__plus-anchor');
      if (!inSkill && !inPlusAnchor) {
        this.skillPickerOpen.set(false);
      }
    }

    if (this.galleryOpen() && target instanceof Element) {
      const inGallery = target.closest('.composer__gallery-popover');
      const inPlusAnchor = target.closest('.composer__plus-anchor');
      if (!inGallery && !inPlusAnchor) {
        this.galleryOpen.set(false);
      }
    }

    if (this.modePickerOpen() && target instanceof Element) {
      const inMode = target.closest('.composer__mode-popover');
      const inPlusAnchor = target.closest('.composer__plus-anchor');
      if (!inMode && !inPlusAnchor) {
        this.modePickerOpen.set(false);
      }
    }

    if (this.host.nativeElement.contains(target)) return;
    this.closeAuxiliaryPopovers();
    this.slashOpen.set(false);
  }

  private tryRunSlashOnlyDraft(): boolean {
    const trimmed = this.draft().trim();
    const parsed = parseSlash(trimmed);
    if (parsed.command === null || parsed.args.length > 0) {
      return false;
    }
    const command = resolveSlashCommand(parsed.command, SLASH_COMMANDS);
    if (!command) {
      return false;
    }
    this.runCommand(command);
    return true;
  }

  private submitDraft(): void {
    const text = this.draft().trim();
    if (!text || this.isGenerating() || this.composerDisabled() || this.hasUploadingAttachments()) return;
    if (this.isOverLimit()) {
      return;
    }
    if (this.isNearLimit() && !this.limitWarningAcknowledged()) {
      this.limitWarningOpen.set(true);
      return;
    }
    if (this.tryRunSlashOnlyDraft()) {
      return;
    }
    const wrap = this.host.nativeElement.querySelector('.composer__unified-wrap') as HTMLElement;
    if (wrap) wrap.style.height = '';
    this.inputManualHeight.set(null);
    this.inputExpanded.set(false);
    this.send.emit(text);
  }

  focusTextarea(): void {
    this.textareaRef()?.nativeElement.focus();
  }

  onResizerMouseDown(event: MouseEvent): void {
    this.startResizeDrag(event.clientY,
      (onMove) => document.addEventListener('mousemove', onMove),
      (onMove) => document.removeEventListener('mousemove', onMove),
      (onUp) => document.addEventListener('mouseup', onUp),
      (onUp) => document.removeEventListener('mouseup', onUp),
    );
    event.preventDefault();
    event.stopPropagation();
  }

  onResizerTouchStart(event: TouchEvent): void {
    const touch = event.touches[0];
    if (!touch) return;
    this.startResizeDrag(touch.clientY,
      (onMove) => document.addEventListener('touchmove', onMove as unknown as EventListener, { passive: false }),
      (onMove) => document.removeEventListener('touchmove', onMove as unknown as EventListener),
      (onUp) => document.addEventListener('touchend', onUp as unknown as EventListener),
      (onUp) => document.removeEventListener('touchend', onUp as unknown as EventListener),
    );
    event.preventDefault();
    event.stopPropagation();
  }

  private startResizeDrag(
    startClientY: number,
    addMove: (fn: (e: MouseEvent | TouchEvent) => void) => void,
    removeMove: (fn: (e: MouseEvent | TouchEvent) => void) => void,
    addUp: (fn: () => void) => void,
    removeUp: (fn: () => void) => void,
  ): void {
    const wrap = this.host.nativeElement.querySelector('.composer__unified-wrap') as HTMLElement;
    if (!wrap) return;
    const startH = wrap.offsetHeight;
    const maxH = Math.floor(window.innerHeight * COMPOSER_EXPANDED_VIEWPORT_RATIO);
    const minH = 83;
    let moved = false;

    const onMove = (e: MouseEvent | TouchEvent) => {
      const clientY = e instanceof MouseEvent ? e.clientY : (e as TouchEvent).touches[0]?.clientY ?? startClientY;
      const dy = startClientY - clientY;
      if (Math.abs(dy) > 4) moved = true;
      if (!moved) return;
      const newH = Math.max(minH, Math.min(startH + dy, maxH));
      this.inputManualHeight.set(newH);
      wrap.style.height = newH + 'px';
      const ta = this.textareaRef()?.nativeElement;
      if (ta) { ta.style.flex = '1'; ta.style.overflowY = 'auto'; }
      if (e instanceof TouchEvent) e.preventDefault();
    };

    const onUp = () => {
      const wasMoved = moved;
      this.teardownResizeDrag();
      if (!wasMoved) {
        // Click: toggle between maximized and line-cap auto-size
        this.inputManualHeight.set(null);
        wrap.style.height = '';
        const ta = this.textareaRef()?.nativeElement;
        if (this.inputExpanded()) {
          // Was maximized → shrink to line-cap auto-size
          this.inputExpanded.set(false);
          if (ta) ta.style.flex = '';
          this.scheduleTextareaResize();
        } else {
          // Was auto-size → maximize
          this.inputExpanded.set(true);
          wrap.style.height = maxH + 'px';
          if (ta) { ta.style.flex = '1'; ta.style.overflowY = 'auto'; }
        }
      }
    };

    this.teardownResizeDrag();
    this.activeResizeDrag = { removeMove, removeUp, onMove, onUp };
    addMove(onMove);
    addUp(onUp);
  }

  private teardownResizeDrag(): void {
    const drag = this.activeResizeDrag;
    if (!drag) return;
    drag.removeMove(drag.onMove);
    drag.removeUp(drag.onUp);
    this.activeResizeDrag = null;
  }

  private scheduleTextareaResize(): void {
    if (typeof window === 'undefined') return;
    if (this.textareaResizeRaf !== null) {
      cancelAnimationFrame(this.textareaResizeRaf);
    }
    this.textareaResizeRaf = requestAnimationFrame(() => {
      this.textareaResizeRaf = null;
      this.resizeTextarea();
    });
  }

  private lineCapHeight(): number {
    const textarea = this.textareaRef()?.nativeElement;
    if (!textarea) return 220;
    const styles = window.getComputedStyle(textarea);
    const fontSize = this.parsePx(styles.fontSize, 13);
    const lineHeightVal = styles.lineHeight;
    const lineH = lineHeightVal === 'normal'
      ? fontSize * 1.6
      : this.parsePx(lineHeightVal, fontSize * 1.6);
    const paddingTop = this.parsePx(styles.paddingTop, 10);
    const paddingBottom = this.parsePx(styles.paddingBottom, 6);
    const isMobile = window.innerWidth <= COMPOSER_MOBILE_BREAKPOINT;
    const maxRows = isMobile ? COMPOSER_MOBILE_MAX_ROWS : COMPOSER_DESKTOP_MAX_ROWS;
    return Math.ceil(maxRows * lineH + paddingTop + paddingBottom);
  }

  private resizeTextarea(): void {
    const textarea = this.textareaRef()?.nativeElement;
    if (!textarea) return;

    if (this.inputManualHeight() !== null || this.inputExpanded()) {
      textarea.style.flex = '1';
      textarea.style.overflowY = 'auto';
      return;
    }

    const styles = window.getComputedStyle(textarea);
    const minHeight = this.parsePx(styles.minHeight, 36);
    const maxHeight = this.lineCapHeight();

    textarea.style.flex = '';
    textarea.style.height = 'auto';
    const contentHeight = textarea.scrollHeight;
    const nextHeight = Math.max(minHeight, Math.min(contentHeight, maxHeight));
    textarea.style.height = `${nextHeight}px`;
    textarea.style.overflowY = contentHeight > maxHeight ? 'auto' : 'hidden';
  }

  private parsePx(value: string | null, fallback: number): number {
    const parsed = Number.parseFloat(value ?? '');
    return Number.isFinite(parsed) ? parsed : fallback;
  }

  confirmLimitWarningAndSend(): void {
    this.limitWarningAcknowledged.set(true);
    this.limitWarningOpen.set(false);
    this.submitDraft();
  }
}

function mergeRitualOptions(chatResults: Ritual[], system: Ritual[]): Ritual[] {
  const map = new Map<string, Ritual>();
  for (const r of chatResults) {
    map.set(r.id, r);
  }
  for (const r of system) {
    map.set(r.id, r);
  }
  return [...map.values()].sort((a, b) => a.name.localeCompare(b.name));
}

function isAllowedFile(file: File): boolean {
  return file.size <= MAX_FILE_SIZE_BYTES && isMimeOrExtensionAllowedForChatUpload(file);
}

function detectSoftKeyboard(): boolean {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
    return false;
  }
  return window.matchMedia('(pointer: coarse) and (hover: none)').matches;
}

/** Clipboard files (especially screenshots) often arrive without a filename;
 * give them one so the extension allowlist and attachment chips have something
 * sensible to show. Real files copied from the OS keep their original name. */
function ensurePastedFileName(file: File): File {
  if (file.name && file.name.trim()) {
    return file;
  }
  const subtype = file.type.split('/')[1] || '';
  const ext = subtype ? `.${subtype}` : '';
  return new File([file], `pasted-file-${Date.now()}${ext}`, {
    type: file.type,
    lastModified: file.lastModified,
  });
}

function emojiCharFromData(emoji: EmojiData): string {
  if (emoji.native) {
    return emoji.native;
  }
  const unified = emoji.unified;
  if (!unified) {
    return '';
  }
  try {
    return unified
      .split('-')
      .map(hex => String.fromCodePoint(parseInt(hex, 16)))
      .join('');
  } catch {
    return '';
  }
}
