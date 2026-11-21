import { ChangeDetectionStrategy, Component, OnInit, inject, signal } from '@angular/core';
import { firstValueFrom } from 'rxjs';

import { Model, SpeechVoice } from '../../../core/models/model.model';
import { ModelService } from '../../../core/services/model.service';
import { ProviderConnectionsService } from '../../../core/services/provider-connections.service';
import { apiErrorMessage } from '../../../core/utils/api-error.helpers';
import { CheckboxComponent } from '../../../shared/ui/checkbox/checkbox.component';

/** One voice as the account edits it: whether it's offered, and its name. */
interface VoiceRow {
  id: string;
  name: string;
  offered: boolean;
}

interface ModelRow {
  model: Model;
  voices: VoiceRow[];
  /** A model added by id has no known list: voice ids are typed. */
  typed: boolean;
  dirty: boolean;
}

/** The rows for a model: every voice it has, those it offers ticked, with the names given. */
export function voiceRows(model: Model): VoiceRow[] {
  const offered = new Map((model.speech_voices ?? []).map(v => [v.id, v.name]));
  const all = model.available_voices?.length ? model.available_voices : (model.speech_voices ?? []);
  const rows = all.map(v => ({ id: v.id, name: offered.get(v.id) ?? v.name, offered: offered.has(v.id) }));
  for (const [id, name] of offered) {
    if (!rows.some(r => r.id === id)) rows.push({ id, name, offered: true });
  }
  return rows;
}

/**
 * The voices the account's own voice models offer (ADR 0x052 §4): narrow them (hide voices nobody
 * should pick) and rename them. Personalities pick from what is offered. Models from this server's
 * catalog are the server's to set; shown only where the account has its own.
 *
 * Self-contained so any build's settings modal can place it.
 */
@Component({
  selector: 'app-offered-voices',
  standalone: true,
  imports: [CheckboxComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (rows().length) {
      <fieldset class="voices" data-testid="offered-voices">
        <legend>Voices</legend>
        <small>The voices your voice models offer to personalities. Untick the ones nobody should pick, and rename any.</small>
        @for (row of rows(); track row.model.id) {
          <div class="voices__model" [attr.data-testid]="'offered-voices-' + row.model.name">
            <span class="voices__title"
              >{{ row.model.display_name }}
              @if (row.model.connection_name) {
                · {{ row.model.connection_name }}
              }
            </span>
            <ul>
              @for (v of row.voices; track v.id) {
                <li>
                  <ui-checkbox [checked]="v.offered" [ariaLabel]="'Offer ' + v.id" (change)="toggle(row, v, $any($event.target).checked)" />
                  <input
                    type="text"
                    maxlength="100"
                    [value]="v.name"
                    [attr.aria-label]="'Name for ' + v.id"
                    (input)="rename(row, v, $any($event.target).value)"
                  />
                  @if (v.name !== v.id) {
                    <span class="voices__id">{{ v.id }}</span>
                  }
                </li>
              }
            </ul>
            @if (row.typed) {
              <input
                #newVoice
                type="text"
                class="voices__add"
                placeholder="Add a voice id"
                aria-label="Add a voice id"
                (keydown.enter)="addVoice(row, newVoice.value); newVoice.value = ''"
              />
            }
            @if (row.dirty) {
              <button type="button" data-testid="offered-voices-save" [disabled]="saving() === row.model.id" (click)="save(row)">
                {{ saving() === row.model.id ? 'Saving…' : 'Save voices' }}
              </button>
            }
          </div>
        }
        @if (error()) {
          <p class="voices__error" role="alert">{{ error() }}</p>
        }
      </fieldset>
    }
  `,
  styles: `
    .voices {
      border: 1px solid var(--border);
      border-radius: 0.6rem;
      display: flex;
      flex-direction: column;
      gap: 0.6rem;
      margin: 0;
      padding: 0.75rem 1rem 1rem;
    }
    legend {
      color: var(--text-primary);
      font-weight: 600;
      padding: 0 0.25rem;
    }
    small,
    .voices__id {
      color: var(--text-secondary);
      font-size: 0.75rem;
    }
    .voices__title {
      color: var(--text-primary);
      font-size: 0.85rem;
      font-weight: 600;
    }
    ul {
      display: grid;
      gap: 0.25rem;
      grid-template-columns: repeat(auto-fill, minmax(14rem, 1fr));
      list-style: none;
      margin: 0.4rem 0;
      padding: 0;
    }
    li {
      align-items: center;
      display: flex;
      gap: 0.35rem;
    }
    input[type='text'] {
      background: var(--bg-input, var(--bg-primary));
      border: 1px solid var(--border);
      border-radius: 0.3rem;
      color: var(--text-primary);
      font: inherit;
      font-size: 0.8rem;
      min-width: 0;
      padding: 0.15rem 0.35rem;
    }
    .voices__error {
      color: var(--color-danger);
      font-size: 0.8rem;
      margin: 0;
    }
  `,
})
export class OfferedVoicesComponent implements OnInit {
  private readonly modelService = inject(ModelService);
  private readonly connections = inject(ProviderConnectionsService);

  readonly rows = signal<ModelRow[]>([]);
  readonly saving = signal<string | null>(null);
  readonly error = signal('');

  async ngOnInit(): Promise<void> {
    try {
      const models = await firstValueFrom(this.modelService.getAudioModels('speech'));
      this.rows.set(
        models
          .filter(m => m.connection_id)
          .map(model => ({ model, voices: voiceRows(model), typed: !model.available_voices?.length, dirty: false })),
      );
    } catch (err) {
      this.error.set(apiErrorMessage(err, "Couldn't load your voice models."));
    }
  }

  toggle(row: ModelRow, voice: VoiceRow, offered: boolean): void {
    this.patch(row, voices => voices.map(v => (v.id === voice.id ? { ...v, offered } : v)));
  }

  rename(row: ModelRow, voice: VoiceRow, name: string): void {
    this.patch(row, voices => voices.map(v => (v.id === voice.id ? { ...v, name } : v)));
  }

  addVoice(row: ModelRow, id: string): void {
    const voiceId = id.trim();
    if (!voiceId || row.voices.some(v => v.id === voiceId)) return;
    this.patch(row, voices => [...voices, { id: voiceId, name: voiceId, offered: true }]);
  }

  async save(row: ModelRow): Promise<void> {
    const voices: SpeechVoice[] = row.voices.filter(v => v.offered).map(v => ({ id: v.id, name: v.name.trim() || v.id }));
    if (voices.length === 0) {
      this.error.set('A voice model needs at least one voice.');
      return;
    }
    this.saving.set(row.model.id);
    this.error.set('');
    try {
      await firstValueFrom(
        this.connections.setModels(row.model.connection_id!, [
          { id: row.model.name, enabled: true, display_name: row.model.display_name, voices },
        ]),
      );
      this.rows.update(rows =>
        rows.map(r => (r.model.id === row.model.id ? { ...r, model: { ...r.model, speech_voices: voices }, dirty: false } : r)),
      );
    } catch (err) {
      this.error.set(apiErrorMessage(err, "Couldn't save the voices."));
    } finally {
      this.saving.set(null);
    }
  }

  private patch(row: ModelRow, change: (voices: VoiceRow[]) => VoiceRow[]): void {
    this.rows.update(rows => rows.map(r => (r.model.id === row.model.id ? { ...r, voices: change(r.voices), dirty: true } : r)));
  }
}
