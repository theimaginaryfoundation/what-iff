import { ChangeDetectionStrategy, Component, OnInit, computed, inject, input, output, signal } from '@angular/core';
import { firstValueFrom } from 'rxjs';

import { VoiceCostOutletComponent } from '../../../extensions/voice-cost-outlet.component';
import { Model } from '../../../core/models/model.model';
import { Personality } from '../../../core/models/personality.model';
import { PersonalityVoice, voicePreviewSample } from '../../../core/models/speech.model';
import { ModelService } from '../../../core/services/model.service';
import { SpeechPlayerService } from '../../../core/services/speech-player.service';
import { SpeechService } from '../../../core/services/speech.service';
import { apiErrorMessage } from '../../../core/utils/api-error.helpers';
import { modelTierCompactLabel, modelTierRank } from '../../../core/utils/model-display.helpers';

const MAX_STYLE = 500;
const MAX_PREVIEW = 300;

/**
 * Voice models lowest grade first, then by name (ADR 0x052 §4): a voice chosen for the first time
 * is the first, so whoever configures voice decides what a person gets without thinking about cost.
 * Models with no grade come last.
 */
export function orderVoiceModels(models: readonly Model[]): Model[] {
  return [...models].sort((a, b) => {
    const ra = modelTierRank(a.subscription_tier) ?? Number.MAX_SAFE_INTEGER;
    const rb = modelTierRank(b.subscription_tier) ?? Number.MAX_SAFE_INTEGER;
    return ra - rb || a.display_name.localeCompare(b.display_name);
  });
}

/**
 * A personality's voice (ADR 0x052 §5): a voice model, one of its voices, and how the personality
 * sounds, with a preview of the unsaved choice. Saved on its own route, so nothing else on the page
 * can clear it.
 */
@Component({
  selector: 'app-personality-voice',
  standalone: true,
  imports: [VoiceCostOutletComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <section class="voice" aria-labelledby="personality-voice-heading" data-testid="personality-voice">
      <h2 id="personality-voice-heading">Voice</h2>
      @if (loaded() && models().length === 0) {
        <p class="voice__note" data-testid="voice-none">
          No voice models yet. Enable one on a provider in Settings, such as Gemini 3.8 Flash TTS or Qwen Audio 3.1 TTS.
        </p>
      } @else if (!editing()) {
        @if (personality().voice; as v) {
          <p class="voice__summary" data-testid="voice-summary">
            {{ voiceLabel(v) }}
            @if (unusable()) {
              <span class="voice__warn" role="status">This voice isn't available any more; choose another.</span>
            }
          </p>
          <div class="voice__actions">
            <button type="button" class="voice__button" (click)="edit()">Change voice</button>
            <button type="button" class="voice__button" data-testid="voice-remove" [disabled]="saving()" (click)="remove()">Remove</button>
          </div>
        } @else {
          <p class="voice__note">Give {{ personality().name }} a voice to hear replies read aloud.</p>
          <button
            type="button"
            class="voice__button voice__button--primary"
            data-testid="voice-add"
            [disabled]="!loaded()"
            (click)="edit()"
          >
            Add a voice
          </button>
        }
      } @else {
        <label class="voice__field">
          <span>Voice model</span>
          <span class="voice__row">
            <select data-testid="voice-model" [value]="modelId()" (change)="pickModel($any($event.target).value)">
              @for (m of models(); track m.id) {
                <option [value]="m.id" [selected]="m.id === modelId()">{{ modelLabel(m) }}</option>
              }
            </select>
            <app-voice-cost-outlet [model]="model()" place="picker" />
          </span>
        </label>
        <label class="voice__field">
          <span>Voice</span>
          <select data-testid="voice-voice" [value]="voiceId()" (change)="voiceId.set($any($event.target).value)">
            @for (v of model()?.speech_voices ?? []; track v.id) {
              <option [value]="v.id" [selected]="v.id === voiceId()">{{ v.name }}</option>
            }
          </select>
        </label>
        @if (model()?.speech_style) {
          <label class="voice__field">
            <span>How {{ personality().name }} sounds</span>
            <textarea
              data-testid="voice-style"
              rows="2"
              [attr.maxlength]="maxStyle"
              placeholder="Warm and unhurried, a dry Scottish lilt; smiles through the jokes."
              [value]="style()"
              (input)="style.set($any($event.target).value)"
            ></textarea>
            <small>{{ style().length }}/{{ maxStyle }}</small>
          </label>
        }
        <div class="voice__field">
          <span>Preview</span>
          <textarea
            data-testid="voice-sample"
            rows="2"
            [attr.maxlength]="maxPreview"
            [value]="sample()"
            (input)="sample.set($any($event.target).value)"
          ></textarea>
          <span class="voice__row">
            <button
              type="button"
              class="voice__button"
              data-testid="voice-preview"
              [disabled]="previewing() || !sample().trim()"
              (click)="preview()"
            >
              {{ previewing() ? 'Loading…' : 'Play preview' }}
            </button>
            <app-voice-cost-outlet [model]="model()" place="preview" [characters]="sample().length" />
          </span>
        </div>
        <div class="voice__actions">
          <button
            type="button"
            class="voice__button voice__button--primary"
            data-testid="voice-save"
            [disabled]="saving() || !voiceId()"
            (click)="save()"
          >
            {{ saving() ? 'Saving…' : 'Save voice' }}
          </button>
          <button type="button" class="voice__button" (click)="cancel()">Cancel</button>
        </div>
      }
      @if (error()) {
        <p class="voice__note voice__note--error" role="alert">{{ error() }}</p>
      }
    </section>
  `,
  styles: `
    .voice {
      background: var(--color-surface-card);
      border: 1px solid var(--color-border-default);
      border-radius: 1rem;
      display: flex;
      flex-direction: column;
      gap: 0.6rem;
      padding: 1rem;
    }
    h2 {
      color: var(--color-text-primary);
      font-size: 1rem;
      font-weight: 600;
      margin: 0;
    }
    .voice__note,
    .voice__summary,
    small {
      color: var(--color-text-secondary);
      font-size: 0.85rem;
      margin: 0;
    }
    .voice__summary {
      color: var(--color-text-primary);
    }
    .voice__warn,
    .voice__note--error {
      color: var(--color-danger);
      display: block;
    }
    .voice__field {
      display: flex;
      flex-direction: column;
      gap: 0.3rem;
    }
    .voice__field > span:first-child {
      color: var(--color-text-primary);
      font-size: 0.85rem;
    }
    .voice__row {
      align-items: center;
      display: flex;
      gap: 0.5rem;
    }
    select,
    textarea {
      background: var(--color-surface-input, var(--color-surface-card));
      border: 1px solid var(--color-border-default);
      border-radius: 0.4rem;
      color: var(--color-text-primary);
      font: inherit;
      padding: 0.35rem 0.5rem;
    }
    .voice__actions {
      display: flex;
      gap: 0.5rem;
    }
    .voice__button {
      background: none;
      border: 1px solid var(--color-border-default);
      border-radius: 0.4rem;
      color: var(--color-text-primary);
      cursor: pointer;
      font: inherit;
      font-size: 0.85rem;
      padding: 0.3rem 0.7rem;
    }
    .voice__button--primary {
      background: var(--color-accent);
      border-color: var(--color-accent);
      color: white;
    }
    .voice__button:disabled {
      cursor: default;
      opacity: 0.55;
    }
  `,
})
export class PersonalityVoiceComponent implements OnInit {
  private readonly modelService = inject(ModelService);
  private readonly speech = inject(SpeechService);
  private readonly player = inject(SpeechPlayerService);

  readonly personality = input.required<Personality>();
  /** The personality after its voice was saved or removed. */
  readonly changed = output<Personality>();

  readonly maxStyle = MAX_STYLE;
  readonly maxPreview = MAX_PREVIEW;
  readonly models = signal<Model[]>([]);
  readonly loaded = signal(false);
  readonly editing = signal(false);
  readonly saving = signal(false);
  readonly previewing = signal(false);
  readonly error = signal('');

  readonly modelId = signal('');
  readonly voiceId = signal('');
  readonly style = signal('');
  readonly sample = signal('');

  readonly model = computed(() => this.models().find(m => m.id === this.modelId()) ?? null);
  /** The saved voice can't be used: its model is gone or no longer offers it. */
  readonly unusable = computed(() => {
    const v = this.personality().voice;
    if (!v || !this.loaded()) return false;
    const m = this.models().find(x => x.id === v.model_id);
    return !m || !(m.speech_voices ?? []).some(x => x.id === v.voice);
  });

  async ngOnInit(): Promise<void> {
    try {
      this.models.set(orderVoiceModels(await firstValueFrom(this.modelService.getAudioModels('speech'))));
    } catch (err) {
      this.error.set(apiErrorMessage(err, "Couldn't load voice models."));
    } finally {
      this.loaded.set(true);
    }
  }

  modelLabel(m: Model): string {
    const tier = modelTierRank(m.subscription_tier) != null ? ` · ${modelTierCompactLabel(m.subscription_tier)}` : '';
    const conn = m.connection_name ? ` · ${m.connection_name}` : '';
    return `${m.display_name}${conn}${tier}`;
  }

  voiceLabel(v: PersonalityVoice): string {
    const m = this.models().find(x => x.id === v.model_id);
    const name = m?.speech_voices?.find(x => x.id === v.voice)?.name ?? v.voice;
    return m ? `${name} · ${m.display_name}` : name;
  }

  /** Opens the fields: the saved voice, or the first model's first voice for a new one. */
  edit(): void {
    const v = this.personality().voice;
    const usable = v && !this.unusable();
    const first = this.models()[0];
    this.modelId.set(usable ? v.model_id : (first?.id ?? ''));
    this.voiceId.set(usable ? v.voice : (first?.speech_voices?.[0]?.id ?? ''));
    this.style.set(v?.style ?? '');
    this.sample.set(voicePreviewSample(this.personality().name));
    this.error.set('');
    this.editing.set(true);
  }

  /** A new model keeps the voice if it has one by that id, else takes its first. */
  pickModel(id: string): void {
    this.modelId.set(id);
    const voices = this.model()?.speech_voices ?? [];
    if (!voices.some(v => v.id === this.voiceId())) {
      this.voiceId.set(voices[0]?.id ?? '');
    }
  }

  private draft(): PersonalityVoice {
    return { model_id: this.modelId(), voice: this.voiceId(), style: this.model()?.speech_style ? this.style().trim() : '' };
  }

  async preview(): Promise<void> {
    this.previewing.set(true);
    this.error.set('');
    try {
      const audio = await firstValueFrom(this.speech.preview(this.draft(), this.sample().trim()));
      await this.player.playAudio(audio);
    } catch (err) {
      this.error.set(apiErrorMessage(err, "Couldn't play the preview."));
    } finally {
      this.previewing.set(false);
    }
  }

  async save(): Promise<void> {
    this.saving.set(true);
    this.error.set('');
    try {
      this.changed.emit(await firstValueFrom(this.speech.setVoice(this.personality().id, this.draft())));
      this.editing.set(false);
    } catch (err) {
      this.error.set(apiErrorMessage(err, "Couldn't save the voice."));
    } finally {
      this.saving.set(false);
    }
  }

  async remove(): Promise<void> {
    this.saving.set(true);
    this.error.set('');
    try {
      this.changed.emit(await firstValueFrom(this.speech.clearVoice(this.personality().id)));
    } catch (err) {
      this.error.set(apiErrorMessage(err, "Couldn't remove the voice."));
    } finally {
      this.saving.set(false);
    }
  }

  cancel(): void {
    this.player.stop();
    this.editing.set(false);
    this.error.set('');
  }
}
