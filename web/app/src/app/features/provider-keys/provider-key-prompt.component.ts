import { ChangeDetectionStrategy, Component, OnInit, computed, inject } from '@angular/core';

import { ProviderKeysService } from '../../core/services/provider-keys.service';
import { ModalComponent } from '../../shared/ui/modal/modal.component';
import { ProfileSettingsModalService } from '../profile-settings/profile-settings-modal.service';

/**
 * The first-run prompt: an account that brings its own provider keys and has
 * none yet cannot use anything, so this asks for one and opens Profile &
 * Settings on the Providers tab. It returns whenever settings close without a
 * key, the same way the personality welcome holds until one exists.
 */
@Component({
  selector: 'app-provider-key-prompt',
  standalone: true,
  imports: [ModalComponent],
  templateUrl: './provider-key-prompt.component.html',
  styleUrl: './provider-key-prompt.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ProviderKeyPromptComponent implements OnInit {
  private readonly keys = inject(ProviderKeysService);
  private readonly settings = inject(ProfileSettingsModalService);

  readonly open = computed(() => this.keys.needsFirstKey() && !this.settings.openState());

  readonly steps = [
    { title: 'Add your OpenAI key', body: 'Billed to your own OpenAI account. Other providers are optional.' },
    { title: 'Create a personality', body: 'Who you talk to: a voice, expertise and look of your own.' },
    { title: 'Start chatting', body: 'It remembers you across conversations.' },
  ];

  ngOnInit(): void {
    this.keys.refreshAccess().subscribe();
  }

  addKey(): void {
    this.settings.open('providers');
  }
}
