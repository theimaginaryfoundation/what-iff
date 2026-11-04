import { ChangeDetectionStrategy, Component, computed, effect, inject, untracked } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';

import { ProviderKeysService } from '../../core/services/provider-keys.service';
import { UserPreferencesService } from '../../core/services/user-preferences.service';
import { hasDefaultModel } from '../../core/utils/default-model';
import { ModalComponent } from '../../shared/ui/modal/modal.component';
import { ProfileSettingsModalService } from '../profile-settings/profile-settings-modal.service';

/**
 * The first-run prompt: an account that brings its own provider keys and has
 * none yet cannot use anything, so this asks for one and opens Profile &
 * Settings on the Providers tab. With a key but no model to chat with yet (on
 * an install without a model catalog, ADR 0x027), it asks the account to
 * choose its models, opening the chooser. Either way it returns whenever
 * settings close without the step done, the same way the personality welcome
 * holds until one exists.
 */
@Component({
  selector: 'app-provider-key-prompt',
  standalone: true,
  imports: [ModalComponent],
  templateUrl: './provider-key-prompt.component.html',
  styleUrl: './provider-key-prompt.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ProviderKeyPromptComponent {
  private readonly keys = inject(ProviderKeysService);
  private readonly preferences = inject(UserPreferencesService);
  private readonly settings = inject(ProfileSettingsModalService);

  private readonly prefs = toSignal(this.preferences.preferences$, { initialValue: null });

  /** A key, but no model: the account brings its own keys, has one, and has no default model yet. */
  readonly needsFirstModel = computed(() => {
    const access = this.keys.access();
    const prefs = this.prefs();
    return access?.keys_from === 'account' && access.providers.length > 0 && prefs !== null && !hasDefaultModel(prefs);
  });

  /** Which step the account is on: 0 the key, 1 the models. */
  readonly step = computed(() => (this.keys.needsFirstKey() ? 0 : 1));

  readonly open = computed(() => (this.keys.needsFirstKey() || this.needsFirstModel()) && !this.settings.openState());

  readonly steps = [
    { title: 'Add your OpenAI key', body: 'Billed to your own OpenAI account. Other providers are optional.' },
    { title: 'Choose your models', body: 'From OpenAI, or any other provider you add.' },
    { title: 'Create a personality', body: 'Who you talk to: a voice, expertise and look of your own.' },
    { title: 'Start chatting', body: 'It remembers you across conversations.' },
  ];

  constructor() {
    // At start, and whenever settings close: they may have added a key or chosen models.
    effect(() => {
      if (!this.settings.openState()) {
        untracked(() => this.refresh());
      }
    });
  }

  addKey(): void {
    this.settings.open('providers');
  }

  chooseModels(): void {
    this.settings.openModelChooser('openai');
  }

  private refresh(): void {
    this.keys.refreshAccess().subscribe();
    this.preferences.getUserPreferences().subscribe({ error: () => undefined });
  }
}
