import { ChangeDetectionStrategy, Component, computed, effect, inject, input, output, signal, untracked } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { firstValueFrom } from 'rxjs';

import {
  ProviderConnected,
  ProviderConnection,
  ProviderFlavor,
  ProviderPreset,
  UpdateProviderConnection,
} from '../../../core/models/provider-connection.model';
import { ProviderConnectionsService } from '../../../core/services/provider-connections.service';
import { apiErrorMessage, hasErrorCode } from '../../../core/utils/api-error.helpers';
import { ModalComponent } from '../../../shared/ui';

/** The field a failure points at, so the form can highlight it. */
type FormField = 'preset' | 'name' | 'flavor' | 'address' | 'key' | 'password';

interface FormError {
  message: string;
  field?: FormField;
}

/**
 * The one form for adding or editing a provider connection (ADR 0x027). Picking
 * a provider fills in its details; Custom asks for them. Connecting lists the
 * provider's models first, and nothing is saved unless that works, so a failure
 * keeps everything typed and points at the field to fix.
 *
 * Opened with `connection` to edit it: the provider is fixed, and a new key or
 * address is checked the same way. A rename needs no password.
 */
@Component({
  selector: 'app-provider-form',
  standalone: true,
  imports: [FormsModule, ModalComponent],
  templateUrl: './provider-form.component.html',
  styleUrl: './provider-form.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ProviderFormComponent {
  private readonly connections = inject(ProviderConnectionsService);

  readonly open = input(false);
  readonly presets = input<ProviderPreset[]>([]);
  /** The connection to edit; null to add one. */
  readonly connection = input<ProviderConnection | null>(null);
  /** The provider to start an add on, if any. */
  readonly initialPreset = input<string | null>(null);

  /** Emitted once a connection was added or changed. */
  readonly saved = output<ProviderConnected>();
  readonly closed = output<void>();

  readonly presetId = signal('');
  readonly name = signal('');
  readonly flavor = signal<ProviderFlavor | ''>('');
  readonly address = signal('');
  readonly key = signal('');
  readonly password = signal('');
  readonly saving = signal(false);
  readonly error = signal<FormError | null>(null);
  /** Set once connected: what was found, shown before closing. */
  readonly result = signal<ProviderConnected | null>(null);

  readonly editing = computed(() => this.connection() !== null);

  readonly preset = computed<ProviderPreset | null>(() => this.presets().find(p => p.id === this.presetId()) ?? null);

  readonly isCustom = computed(() => this.presetId() === 'custom');

  /** The presets, grouped the way the form offers them. */
  readonly groups = computed(() => {
    const presets = this.presets();
    return [
      { label: 'Cloud', presets: presets.filter(p => !p.local && p.id !== 'custom') },
      { label: 'On this computer', presets: presets.filter(p => p.local) },
      { label: 'Anything else', presets: presets.filter(p => p.id === 'custom') },
    ].filter(g => g.presets.length > 0);
  });

  /** The address as it stands before any edit, to tell whether it changed. */
  private readonly originalAddress = computed(() => this.connection()?.base_url || this.preset()?.base_url || '');

  readonly addressChanged = computed(() => this.editing() && this.address().trim() !== this.originalAddress());

  readonly keyChanged = computed(() => this.key().trim() !== '');

  /** Adding, or pointing a connection at a new key or address, needs the owner's password; a rename does not. */
  readonly needsPassword = computed(() => !this.editing() || this.keyChanged() || this.addressChanged());

  /** Where to create a key, as the server's preset says. */
  readonly keyUrl = computed(() => this.preset()?.keys_url ?? null);

  readonly title = computed(() => {
    const connection = this.connection();
    return connection ? `Edit ${connection.name}` : 'Add a provider';
  });

  constructor() {
    // Each time the form opens, start from the connection being edited, or a
    // blank form on the provider asked for.
    effect(() => {
      if (!this.open()) {
        return;
      }
      const connection = this.connection();
      const initial = this.initialPreset();
      untracked(() => this.reset(connection, initial));
    });
  }

  private reset(connection: ProviderConnection | null, initial: string | null): void {
    this.presetId.set(connection?.preset ?? initial ?? '');
    this.name.set(connection?.name ?? '');
    this.flavor.set(connection?.flavor ?? '');
    this.key.set('');
    this.password.set('');
    this.error.set(null);
    this.result.set(null);
    this.saving.set(false);
    this.address.set(connection?.base_url || this.preset()?.base_url || '');
  }

  /** Picking a provider fills in its address; anything typed for another is dropped. */
  pickPreset(id: string): void {
    this.presetId.set(id);
    this.address.set(this.preset()?.base_url ?? '');
    this.flavor.set('');
    this.error.set(null);
  }

  fieldInvalid(field: FormField): boolean {
    return this.error()?.field === field;
  }

  async submit(): Promise<void> {
    const preset = this.preset();
    if (!preset) {
      this.error.set({ message: 'Choose a provider.', field: 'preset' });
      return;
    }
    const problem = this.check(preset);
    if (problem) {
      this.error.set(problem);
      return;
    }
    this.saving.set(true);
    this.error.set(null);
    try {
      const reconnecting = this.needsPassword();
      const connected = this.editing() ? await this.saveEdit() : await this.saveNew(preset);
      if (connected) {
        this.saved.emit(connected);
        // A rename reached nothing, so there is no result to show.
        if (reconnecting) {
          this.result.set(connected);
        } else {
          this.closed.emit();
        }
      }
    } catch (err) {
      this.error.set(this.explain(err, preset));
      if (hasErrorCode(err, 'reauthentication_required')) {
        this.password.set('');
      }
    } finally {
      this.saving.set(false);
    }
  }

  /** What is missing before anything is sent, if anything. */
  private check(preset: ProviderPreset): FormError | null {
    if (this.isCustom() && !this.editing()) {
      if (!this.name().trim()) return { message: 'Name this connection.', field: 'name' };
      if (!this.flavor()) return { message: 'Choose what the server speaks.', field: 'flavor' };
    }
    if (preset.address_editable && !this.address().trim()) {
      return { message: "Enter the server's address.", field: 'address' };
    }
    if (!this.editing() && preset.key_required && !this.key().trim()) {
      return { message: `Paste your ${preset.name} key.`, field: 'key' };
    }
    if (this.needsPassword() && !this.password()) {
      return { message: 'Enter your password to confirm.', field: 'password' };
    }
    return null;
  }

  private saveNew(preset: ProviderPreset): Promise<ProviderConnected> {
    return firstValueFrom(
      this.connections.create({
        preset: preset.id,
        name: this.name().trim() || undefined,
        flavor: this.isCustom() ? (this.flavor() as ProviderFlavor) : undefined,
        base_url: preset.address_editable ? this.address().trim() : undefined,
        key: this.key().trim() || undefined,
        current_password: this.password(),
      }),
    );
  }

  /** Sends only what changed. With nothing changed there is nothing to send. */
  private async saveEdit(): Promise<ProviderConnected | null> {
    const connection = this.connection()!;
    const body: UpdateProviderConnection = {};
    const name = this.name().trim();
    if (name && name !== connection.name) body.name = name;
    if (this.addressChanged()) body.base_url = this.address().trim();
    if (this.keyChanged()) body.key = this.key().trim();
    if (Object.keys(body).length === 0) {
      this.closed.emit();
      return null;
    }
    if (this.needsPassword()) body.current_password = this.password();
    return firstValueFrom(this.connections.update(connection.id, body));
  }

  /** Turns a failure into words, and the field to fix. */
  private explain(err: unknown, preset: ProviderPreset): FormError {
    const name = preset.name;
    if (hasErrorCode(err, 'reauthentication_required')) {
      return { message: "That password didn't match. Try again.", field: 'password' };
    }
    if (hasErrorCode(err, 'provider_key_rejected')) {
      return { message: `${name} didn't accept that key.`, field: 'key' };
    }
    if (hasErrorCode(err, 'provider_unreachable')) {
      return { message: this.unreachable(preset), field: preset.address_editable ? 'address' : undefined };
    }
    if (hasErrorCode(err, 'provider_incompatible')) {
      return preset.address_editable
        ? { message: "Something answered at that address, but not as this kind of server. Check the address and what it speaks.", field: 'address' }
        : { message: `${name} answered in a way WhatIff didn't expect. Try again later.` };
    }
    if (hasErrorCode(err, 'provider_unavailable')) {
      return { message: `${name} isn't answering right now. Try again in a moment.` };
    }
    if (hasErrorCode(err, 'custom_address_not_allowed')) {
      return { message: "This WhatIff server doesn't allow choosing a provider's address." };
    }
    if (hasErrorCode(err, 'provider_connection_exists')) {
      return { message: `You already have a ${name} connection. Edit that one instead.`, field: 'preset' };
    }
    return { message: apiErrorMessage(err, "Couldn't connect. Try again.") };
  }

  private unreachable(preset: ProviderPreset): string {
    const address = this.address().trim() || preset.base_url || '';
    if (preset.id === 'ollama') {
      return `Nothing answered at ${address}. Check that Ollama is running and listening beyond this computer (OLLAMA_HOST=0.0.0.0).`;
    }
    if (preset.id === 'lmstudio') {
      return `Nothing answered at ${address}. Check that LM Studio's server is running and serving on the local network.`;
    }
    if (preset.address_editable) {
      return `Nothing answered at ${address}. Check the address and that the server is running.`;
    }
    return `Couldn't reach ${preset.name}. Check your internet connection and try again.`;
  }

  close(): void {
    if (!this.saving()) {
      this.closed.emit();
    }
  }
}
