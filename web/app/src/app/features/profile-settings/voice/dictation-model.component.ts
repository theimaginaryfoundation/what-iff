import { ChangeDetectionStrategy, Component, OnInit, computed, inject, signal } from '@angular/core';
import { firstValueFrom } from 'rxjs';

import { Model } from '../../../core/models/model.model';
import { UtilityModels, UtilityRoleModel } from '../../../core/models/utility-model.model';
import { ModelService } from '../../../core/services/model.service';
import { SpeechService } from '../../../core/services/speech.service';
import { UtilityModelsService } from '../../../core/services/utility-models.service';
import { apiErrorMessage } from '../../../core/utils/api-error.helpers';
import { OFF_VALUE, backgroundModelLabel } from '../../../core/utils/background-models';

/**
 * The account's dictation model (ADR 0x052 §4): the `transcription` utility role, chosen from the
 * transcription models on its connections, or Off. With nothing chosen the composer has no
 * microphone. Shown only where accounts choose their models, as background models are.
 *
 * Self-contained so any build's settings modal can place it.
 */
@Component({
  selector: 'app-dictation-model',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (utility()?.accounts_choose) {
      <fieldset class="voice" data-testid="dictation-model">
        <legend>Dictation</legend>
        <small>Talk instead of typing: what you say becomes text in the message box, for you to read and send.</small>
        @if (loaded() && models().length === 0 && !chosen()) {
          <p class="voice__note" data-testid="dictation-none">
            Enable a dictation model on one of your providers first, such as GPT Transcribe (OpenAI), Gemini 3.5 Transcribe (Google), Qwen3
            ASR (Qwen) or Voxtral Transcribe (Mistral).
          </p>
        } @else {
          <label class="voice__field">
            <span>Dictation model</span>
            <select data-testid="dictation-model-select" [disabled]="saving()" (change)="onChange($any($event.target))">
              @if (chosenUnlisted()) {
                <option [value]="chosen()" selected>Chosen model is unavailable</option>
              }
              <option [value]="offValue" [selected]="!chosen() || chosen() === offValue">Off: no microphone</option>
              @for (model of models(); track model.id) {
                <option [value]="model.id" [selected]="model.id === chosen()">{{ label(model) }}</option>
              }
            </select>
          </label>
        }
        @if (error()) {
          <p class="voice__note voice__note--error" role="alert">{{ error() }}</p>
        }
      </fieldset>
    }
  `,
  styles: `
    .voice {
      border: 1px solid var(--border);
      border-radius: 0.6rem;
      display: flex;
      flex-direction: column;
      gap: 0.5rem;
      margin: 0;
      padding: 0.75rem 1rem 1rem;
    }
    legend {
      color: var(--text-primary);
      font-weight: 600;
      padding: 0 0.25rem;
    }
    small,
    .voice__note {
      color: var(--text-secondary);
      font-size: 0.78rem;
      margin: 0;
    }
    .voice__note--error {
      color: var(--color-danger);
    }
    .voice__field {
      display: flex;
      flex-direction: column;
      gap: 0.3rem;
    }
    .voice__field span {
      color: var(--text-primary);
      font-size: 0.85rem;
    }
  `,
})
export class DictationModelComponent implements OnInit {
  private readonly utilityModels = inject(UtilityModelsService);
  private readonly modelService = inject(ModelService);
  private readonly speech = inject(SpeechService);

  readonly offValue = OFF_VALUE;
  readonly label = backgroundModelLabel;
  readonly utility = signal<UtilityModels | null>(null);
  readonly models = signal<Model[]>([]);
  readonly loaded = signal(false);
  readonly saving = signal(false);
  readonly error = signal('');

  private readonly role = computed<UtilityRoleModel | null>(
    () => this.utility()?.speech_roles?.find(r => r.role === 'transcription') ?? null,
  );
  /** The account's choice: a model id, OFF_VALUE, or '' when it chose nothing. */
  readonly chosen = computed(() => {
    const account = this.role()?.account;
    if (!account) return '';
    return account.off ? OFF_VALUE : (account.model_id ?? '');
  });
  readonly chosenUnlisted = computed(() => {
    const id = this.chosen();
    return this.loaded() && !!id && id !== OFF_VALUE && !this.models().some(m => m.id === id);
  });

  async ngOnInit(): Promise<void> {
    try {
      const [utility, models] = await Promise.all([
        firstValueFrom(this.utilityModels.get()),
        firstValueFrom(this.modelService.getAudioModels('transcription')),
      ]);
      this.utility.set(utility);
      this.models.set(models);
      this.loaded.set(true);
    } catch (err) {
      this.error.set(apiErrorMessage(err, "Couldn't load dictation models."));
    }
  }

  async onChange(select: HTMLSelectElement): Promise<void> {
    const value = select.value;
    if (value === this.chosen() || (value === OFF_VALUE && !this.chosen())) return;
    this.saving.set(true);
    this.error.set('');
    try {
      const next = await firstValueFrom(
        value === OFF_VALUE
          ? this.utilityModels.switchOff('transcription')
          : this.utilityModels.choose('transcription', { model_id: value }),
      );
      this.utility.set(next);
      await this.speech.loadDictation(true);
    } catch (err) {
      this.error.set(apiErrorMessage(err, "Couldn't save the dictation model."));
      select.value = this.chosen() || OFF_VALUE;
    } finally {
      this.saving.set(false);
    }
  }
}
