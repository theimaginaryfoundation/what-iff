
import { ChangeDetectionStrategy, Component, computed, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';

import { Ritual } from '../../../core/models/ritual.model';
import { HotkeyInputComponent } from '../../../core/components/hotkey-input/hotkey-input.component';
import { RitualSelectOption } from './ritual-filter-bar.component';

let nextFormId = 0;

export interface RitualFormSave {
  name: string;
  description: string;
  content: string;
  hotkeys?: string;
  personality_id?: string | null;
  /** Replaces the linked MCP servers. Omitted when unchanged on edit so links made elsewhere survive. */
  mcp_server_ids?: string[];
}

@Component({
  selector: 'app-ritual-form',
  standalone: true,
  imports: [FormsModule, HotkeyInputComponent],
  templateUrl: './ritual-form.component.html',
  styleUrl: './ritual-form.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class RitualFormComponent {
  readonly ritual = input<Ritual | null>(null);
  readonly personalities = input<RitualSelectOption[]>([]);
  /** Connectors (MCP servers) the skill can turn on when called. */
  readonly mcpServers = input<RitualSelectOption[]>([]);
  readonly saving = input(false);
  readonly deleting = input(false);
  readonly isSystem = input(false);
  readonly creating = input(false);
  readonly error = input<string | null>(null);

  readonly cancel = output<void>();
  readonly save = output<RitualFormSave>();

  private readonly formId = `ritual-form-${++nextFormId}`;

  readonly name = signal('');
  readonly description = signal('');
  readonly content = signal('');
  readonly hotkeys = signal('');
  readonly personalityId = signal('');
  readonly mcpServerIds = signal<string[]>([]);

  readonly canSave = computed(
    () => this.name().trim().length > 0 && this.description().trim().length > 0 && this.content().trim().length > 0,
  );

  /** True when the form differs from the ritual it was hydrated from (empty for create). */
  readonly hasUnsavedEdits = computed(() => {
    const original = this.ritual();
    return (
      this.name() !== (original?.name ?? '') ||
      this.description() !== (original?.description ?? '') ||
      this.content() !== (original?.content ?? '') ||
      this.hotkeys() !== (original?.hotkeys ?? '') ||
      this.personalityId() !== (original?.personality_id ?? '') ||
      this.mcpIdsChanged()
    );
  });

  /** True when the picked MCP servers differ from the ones on the ritual (order-insensitive). */
  private readonly mcpIdsChanged = computed(() => {
    const current = new Set(this.mcpServerIds());
    const original = new Set(this.ritual()?.mcp_server_ids ?? []);
    return current.size !== original.size || [...current].some(id => !original.has(id));
  });

  ngOnChanges(): void {
    const ritual = this.ritual();
    if (!ritual) {
      this.name.set('');
      this.description.set('');
      this.content.set('');
      this.hotkeys.set('');
      this.personalityId.set('');
      this.mcpServerIds.set([]);
      return;
    }
    this.name.set(ritual.name);
    this.description.set(ritual.description);
    this.content.set(ritual.content);
    this.hotkeys.set(ritual.hotkeys ?? '');
    this.personalityId.set(ritual.personality_id ?? '');
    this.mcpServerIds.set([...(ritual.mcp_server_ids ?? [])]);
  }

  isMcpSelected(id: string): boolean {
    return this.mcpServerIds().includes(id);
  }

  toggleMcp(id: string, checked: boolean): void {
    const next = new Set(this.mcpServerIds());
    if (checked) {
      next.add(id);
    } else {
      next.delete(id);
    }
    this.mcpServerIds.set([...next]);
  }

  submit(): void {
    if (!this.canSave()) return;
    this.save.emit({
      name: this.name().trim(),
      description: this.description().trim(),
      content: this.content().trim(),
      hotkeys: this.hotkeys().trim() || undefined,
      personality_id: this.personalityId().trim() || null,
      // The API replaces links when this is present and leaves them alone when omitted, so only
      // send it when the user changed the picks (or on create) to avoid clobbering Tools-page links.
      ...(this.creating() || this.mcpIdsChanged() ? { mcp_server_ids: [...this.mcpServerIds()] } : {}),
    });
  }

  /** Unique id for a field's hint so aria-describedby stays valid if two forms render at once. */
  hintId(field: string): string {
    return `${this.formId}-${field}-hint`;
  }
}
