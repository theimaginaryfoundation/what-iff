import { DOCUMENT } from '@angular/common';
import { Injectable, OnDestroy, inject } from '@angular/core';

/** Modifier requirements for a registered shortcut. */
export interface ShortcutDefinition {
  /** Single character key (case-insensitive) or `KeyboardEvent.key` value. */
  readonly key: string;
  /** True when the shortcut requires `Cmd` (macOS) or `Ctrl` (other OSes). */
  readonly metaOrCtrl?: boolean;
  /** True when the shortcut requires the Shift modifier. */
  readonly shift?: boolean;
  /** True when the shortcut requires the Alt/Option modifier. */
  readonly alt?: boolean;
  /**
   * Whether to fire the shortcut when the user is typing in an input/textarea
   * /contenteditable element. Defaults to `false` so typing letters never
   * accidentally triggers a global handler. Opt in for shortcuts that should
   * always work (e.g. Cmd+K to open the palette while focusing the chat
   * composer).
   */
  readonly allowInInputs?: boolean;
}

/** Disposer returned by `register()`. Idempotent. */
export type ShortcutHandle = () => void;

/**
 * Owns a single document-level `keydown` listener and dispatches to registered
 * shortcuts. Components should never add their own document-level listeners
 * for global keyboard handling — register here so we can audit, deduplicate,
 * and cleanly skip while typing.
 */
@Injectable({ providedIn: 'root' })
export class KeyboardShortcutService implements OnDestroy {
  private readonly document = inject(DOCUMENT);

  private readonly registrations = new Map<
    number,
    { definition: ShortcutDefinition; handler: (event: KeyboardEvent) => void }
  >();
  private nextId = 1;
  private readonly listener = (event: Event) => this.handle(event as KeyboardEvent);
  private listenerAttached = false;

  register(
    definition: ShortcutDefinition,
    handler: (event: KeyboardEvent) => void,
  ): ShortcutHandle {
    const id = this.nextId++;
    this.registrations.set(id, { definition, handler });
    this.ensureListener();
    return () => {
      this.registrations.delete(id);
      if (this.registrations.size === 0) {
        this.detachListener();
      }
    };
  }

  ngOnDestroy(): void {
    this.detachListener();
    this.registrations.clear();
  }

  private ensureListener(): void {
    if (this.listenerAttached) return;
    this.document.addEventListener('keydown', this.listener);
    this.listenerAttached = true;
  }

  private detachListener(): void {
    if (!this.listenerAttached) return;
    this.document.removeEventListener('keydown', this.listener);
    this.listenerAttached = false;
  }

  private handle(event: KeyboardEvent): void {
    for (const { definition, handler } of this.registrations.values()) {
      if (!matchesDefinition(event, definition)) continue;
      if (!definition.allowInInputs && isTypingTarget(event.target)) continue;
      handler(event);
    }
  }
}

function matchesDefinition(event: KeyboardEvent, def: ShortcutDefinition): boolean {
  if (event.key.toLowerCase() !== def.key.toLowerCase()) return false;
  const metaOrCtrl = event.metaKey || event.ctrlKey;
  if (!!def.metaOrCtrl !== metaOrCtrl) return false;
  if (!!def.shift !== event.shiftKey) return false;
  if (!!def.alt !== event.altKey) return false;
  return true;
}

function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  const tag = target.tagName;
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true;
  if (target.isContentEditable) return true;
  return false;
}
