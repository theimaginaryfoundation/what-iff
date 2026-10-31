import { ChangeDetectionStrategy, Component, computed, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';

import {
  estimateSkillTokens,
  RITUAL_BODY_MAX_BYTES,
  RITUAL_BODY_SOFT_TOKENS,
  RITUAL_DESCRIPTION_MAX,
  RITUAL_SHORT_FIELD_MAX,
  RITUAL_SLUG_MAX,
  Ritual,
  slugifySkillName,
} from '../../../core/models/ritual.model';
import { HotkeyInputComponent } from '../../../core/components/hotkey-input/hotkey-input.component';
import { RitualSelectOption } from './ritual-filter-bar.component';

let nextFormId = 0;

const SLUG_PATTERN = /^[a-z0-9]+(-[a-z0-9]+)*$/;

export interface RitualFormSave {
  name: string;
  description: string;
  content: string;
  hotkeys?: string;
  personality_id?: string | null;
  /** Replaces the linked MCP servers. Omitted when unchanged on edit so links made elsewhere survive. */
  mcp_server_ids?: string[];
  /** Agent Skills fields (ADR 0x035); the form always sends its current values. */
  slug: string;
  license: string;
  compatibility: string;
  allowed_tools: string[];
  disable_model_invocation: boolean;
  user_invocable: boolean;
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
  /** This deployment's tools, offered for `allowed_tools` (loaded with the skill). */
  readonly tools = input<RitualSelectOption[]>([]);
  readonly saving = input(false);
  readonly deleting = input(false);
  readonly creating = input(false);
  readonly error = input<string | null>(null);

  readonly cancel = output<void>();
  readonly save = output<RitualFormSave>();

  private readonly formId = `ritual-form-${++nextFormId}`;

  readonly descriptionMax = RITUAL_DESCRIPTION_MAX;
  readonly shortFieldMax = RITUAL_SHORT_FIELD_MAX;
  readonly slugMax = RITUAL_SLUG_MAX;
  readonly bodySoftTokens = RITUAL_BODY_SOFT_TOKENS;

  readonly name = signal('');
  /** The slug as typed; while untouched on a new skill, the name drives the slug instead. */
  private readonly slugInput = signal('');
  /** Once the user edits the slug (or the form holds an existing skill), the name no longer drives it. */
  private readonly slugTouched = signal(false);
  readonly slug = computed(() => {
    if (this.slugTouched()) return this.slugInput();
    const name = this.name().trim();
    return name ? slugifySkillName(name) : '';
  });
  readonly description = signal('');
  readonly content = signal('');
  readonly hotkeys = signal('');
  readonly personalityId = signal('');
  readonly mcpServerIds = signal<string[]>([]);
  readonly license = signal('');
  readonly compatibility = signal('');
  readonly allowedTools = signal<string[]>([]);
  readonly modelInvocable = signal(true);
  readonly userInvocable = signal(true);

  readonly slugValid = computed(() => {
    const slug = this.slug().trim();
    return slug.length > 0 && slug.length <= RITUAL_SLUG_MAX && SLUG_PATTERN.test(slug);
  });
  readonly descriptionTooLong = computed(() => this.description().length > RITUAL_DESCRIPTION_MAX);
  readonly bodyTokens = computed(() => estimateSkillTokens(this.content()));
  readonly bodyOverSoftLimit = computed(() => this.bodyTokens() > RITUAL_BODY_SOFT_TOKENS);
  readonly bodyTooLong = computed(() => new TextEncoder().encode(this.content()).length > RITUAL_BODY_MAX_BYTES);

  /** Allowed tools kept from an import that this deployment does not offer; shown, not editable. */
  readonly foreignAllowedTools = computed(() => {
    const known = new Set(this.tools().map(tool => tool.id));
    return this.allowedTools().filter(name => !known.has(name));
  });

  readonly canSave = computed(
    () =>
      this.name().trim().length > 0 &&
      this.description().trim().length > 0 &&
      this.content().trim().length > 0 &&
      this.slugValid() &&
      !this.descriptionTooLong() &&
      !this.bodyTooLong(),
  );

  /** True when the form differs from the ritual it was hydrated from (empty for create). */
  readonly hasUnsavedEdits = computed(() => {
    const original = this.ritual();
    return (
      this.name() !== (original?.name ?? '') ||
      this.slug() !== (original?.slug ?? '') ||
      this.description() !== (original?.description ?? '') ||
      this.content() !== (original?.content ?? '') ||
      this.hotkeys() !== (original?.hotkeys ?? '') ||
      this.personalityId() !== (original?.personality_id ?? '') ||
      this.license() !== (original?.license ?? '') ||
      this.compatibility() !== (original?.compatibility ?? '') ||
      this.modelInvocable() !== !(original?.disable_model_invocation ?? false) ||
      this.userInvocable() !== (original?.user_invocable ?? true) ||
      !sameSet(this.allowedTools(), original?.allowed_tools ?? []) ||
      this.mcpIdsChanged()
    );
  });

  /** True when the picked MCP servers differ from the ones on the ritual (order-insensitive). */
  private readonly mcpIdsChanged = computed(() => !sameSet(this.mcpServerIds(), this.ritual()?.mcp_server_ids ?? []));

  ngOnChanges(): void {
    const ritual = this.ritual();
    if (!ritual) {
      this.name.set('');
      this.slugInput.set('');
      this.slugTouched.set(false);
      this.description.set('');
      this.content.set('');
      this.hotkeys.set('');
      this.personalityId.set('');
      this.mcpServerIds.set([]);
      this.license.set('');
      this.compatibility.set('');
      this.allowedTools.set([]);
      this.modelInvocable.set(true);
      this.userInvocable.set(true);
      return;
    }
    this.name.set(ritual.name);
    // An existing skill keeps its slug when renamed: other harnesses and threads know it by that name.
    this.slugInput.set(ritual.slug ?? '');
    this.slugTouched.set(true);
    this.description.set(ritual.description);
    this.content.set(ritual.content);
    this.hotkeys.set(ritual.hotkeys ?? '');
    this.personalityId.set(ritual.personality_id ?? '');
    this.mcpServerIds.set([...(ritual.mcp_server_ids ?? [])]);
    this.license.set(ritual.license ?? '');
    this.compatibility.set(ritual.compatibility ?? '');
    this.allowedTools.set([...(ritual.allowed_tools ?? [])]);
    this.modelInvocable.set(!(ritual.disable_model_invocation ?? false));
    this.userInvocable.set(ritual.user_invocable ?? true);
  }

  onSlugChange(value: string): void {
    this.slugTouched.set(true);
    this.slugInput.set(value.toLowerCase());
  }

  isMcpSelected(id: string): boolean {
    return this.mcpServerIds().includes(id);
  }

  toggleMcp(id: string, checked: boolean): void {
    this.mcpServerIds.set(toggled(this.mcpServerIds(), id, checked));
  }

  isToolSelected(name: string): boolean {
    return this.allowedTools().includes(name);
  }

  toggleTool(name: string, checked: boolean): void {
    this.allowedTools.set(toggled(this.allowedTools(), name, checked));
  }

  submit(): void {
    if (!this.canSave()) return;
    this.save.emit({
      name: this.name().trim(),
      slug: this.slug().trim(),
      description: this.description().trim(),
      content: this.content().trim(),
      hotkeys: this.hotkeys().trim() || undefined,
      personality_id: this.personalityId().trim() || null,
      license: this.license().trim(),
      compatibility: this.compatibility().trim(),
      allowed_tools: [...this.allowedTools()],
      disable_model_invocation: !this.modelInvocable(),
      user_invocable: this.userInvocable(),
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

function toggled(list: string[], item: string, on: boolean): string[] {
  const next = new Set(list);
  if (on) {
    next.add(item);
  } else {
    next.delete(item);
  }
  return [...next];
}

function sameSet(a: string[], b: string[]): boolean {
  const current = new Set(a);
  const original = new Set(b);
  return current.size === original.size && [...current].every(item => original.has(item));
}
