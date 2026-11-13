import { ChangeDetectionStrategy, Component, computed, effect, inject, signal, untracked, viewChild } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';

import { UtilityModels } from '../../core/models/utility-model.model';
import { PersonalityPresenceService } from '../../core/services/personality-presence.service';
import { ProviderKeysService } from '../../core/services/provider-keys.service';
import { UserPreferencesService } from '../../core/services/user-preferences.service';
import { UtilityModelsService } from '../../core/services/utility-models.service';
import { roleNotSet } from '../../core/utils/background-models';
import { hasDefaultModel } from '../../core/utils/default-model';
import { ModalComponent } from '../../shared/ui/modal/modal.component';
import { ProfileSettingsModalService } from '../profile-settings/profile-settings-modal.service';
import { BackgroundModelsStepComponent } from './background-models-step.component';

/**
 * The first-run prompt: an account that brings its own provider keys and has
 * none yet cannot use anything, so this asks for one and opens Profile &
 * Settings on the Providers tab. With a key but no model to chat with yet (on
 * an install without a model catalog, ADR 0x027), it asks the account to
 * choose its models, opening the chooser. Then, while any background role has
 * no model chosen at any level, it asks for those here (ADR 0x027 section 6):
 * during onboarding, and once for an account that is past it, after the server
 * stopped choosing background models itself. Each step returns whenever
 * settings close without it done, the same way the personality welcome holds
 * until one exists.
 */
@Component({
  selector: 'app-provider-key-prompt',
  standalone: true,
  imports: [ModalComponent, BackgroundModelsStepComponent],
  templateUrl: './provider-key-prompt.component.html',
  styleUrl: './provider-key-prompt.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ProviderKeyPromptComponent {
  private readonly keys = inject(ProviderKeysService);
  private readonly preferences = inject(UserPreferencesService);
  private readonly settings = inject(ProfileSettingsModalService);
  private readonly utilityModels = inject(UtilityModelsService);
  private readonly presence = inject(PersonalityPresenceService);

  private readonly prefs = toSignal(this.preferences.preferences$, { initialValue: null });
  /** Every background role's choices, once read; null until then, or where they can't be read. */
  readonly utility = signal<UtilityModels | null>(null);
  /** Whether the account is past onboarding: it has a personality. */
  readonly pastOnboarding = signal(false);
  private readonly backgroundStep = viewChild(BackgroundModelsStepComponent);
  /** Whether the background step is saving, so its button waits. */
  readonly backgroundSaving = computed(() => this.backgroundStep()?.saving() ?? true);

  /** A key, but no model: the account brings its own keys, has one, and has no default model yet. */
  readonly needsFirstModel = computed(() => {
    const access = this.keys.access();
    const prefs = this.prefs();
    return access?.keys_from === 'account' && access.providers.length > 0 && prefs !== null && !hasDefaultModel(prefs);
  });

  /**
   * A default model, but a background role with nothing chosen at any level: where accounts
   * choose, which is where they bring their own keys.
   */
  readonly needsBackgroundModels = computed(() => {
    const access = this.keys.access();
    const utility = this.utility();
    return (
      access?.keys_from === 'account' &&
      access.providers.length > 0 &&
      hasDefaultModel(this.prefs()) &&
      !!utility?.accounts_choose &&
      utility.roles.some(roleNotSet)
    );
  });

  /** The account's default chat model, which the background step starts each role on. */
  readonly defaultModelId = computed(() => this.prefs()?.default_model_id ?? '');

  /** Which step the account is on: 0 the key, 1 the models, 2 the background models. */
  readonly step = computed(() => (this.keys.needsFirstKey() ? 0 : this.needsFirstModel() ? 1 : 2));

  readonly open = computed(
    () => (this.keys.needsFirstKey() || this.needsFirstModel() || this.needsBackgroundModels()) && !this.settings.openState(),
  );

  readonly steps = [
    { title: 'Add your OpenAI key', body: 'Billed to your own OpenAI account. Other providers are optional.' },
    { title: 'Choose your models', body: 'From OpenAI, or any other provider you add.' },
    { title: 'Choose background models', body: 'What saves memories and does the work behind a chat.' },
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

  saveBackgroundModels(): void {
    void this.backgroundStep()?.save();
  }

  /** The background step saved every role. */
  backgroundModelsSaved(utility: UtilityModels): void {
    this.utility.set(utility);
  }

  private refresh(): void {
    this.keys.refreshAccess().subscribe();
    this.preferences.getUserPreferences().subscribe({ error: () => undefined });
    // A failed read asks nothing: the step is a courtesy over a working app, and chats show
    // what a role with nothing chosen keeps from running.
    this.utilityModels.get().subscribe({ next: u => this.utility.set(u), error: () => this.utility.set(null) });
    this.presence.hasPersonality().subscribe({ next: has => this.pastOnboarding.set(has), error: () => this.pastOnboarding.set(false) });
  }
}
