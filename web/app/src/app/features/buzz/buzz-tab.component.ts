import { ChangeDetectionStrategy, Component, OnInit, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { firstValueFrom } from 'rxjs';

import { BuzzService } from './buzz.service';
import { BuzzAgentComponent } from './buzz-agent.component';
import { BuzzTabState } from './buzz-tab.state';

/**
 * Integrations → Buzz: give a persona its own Buzz identity, join it to communities, and connect
 * their channels to relay threads. Talks to /api/buzz (internal/buzzplugin). The parts (identity,
 * community, channels, binding) share {@link BuzzTabState}.
 */
@Component({
  selector: 'app-buzz-tab',
  standalone: true,
  imports: [FormsModule, BuzzAgentComponent],
  providers: [BuzzTabState],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="space-y-6" data-testid="buzz-tab">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white">Buzz</h2>
      <p class="max-w-3xl text-sm text-gray-600 dark:text-gray-400">
        Buzz is a team chat where people and agents share channels. Your persona joins a Buzz community as its own member, with its own key:
        mention it in a connected channel and it answers there; ask it in the app to post a reply and it does. Each connected channel has a
        relay thread here that keeps the conversation together.
      </p>
      <p class="max-w-3xl text-sm text-gray-600 dark:text-gray-400" data-testid="buzz-relay-defaults">
        A new relay thread is sandboxed: it reads only itself (none of your memories, other conversations, files or scratchpad), and what
        the persona learns there stays in that thread. The tools that act beyond the conversation start switched off. What the persona
        learns is saved as <strong>External</strong> memories, marked unverified.
      </p>

      @if (state.error(); as message) {
        <div
          class="rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-800 dark:border-red-800/60 dark:bg-red-900/20 dark:text-red-300"
          role="alert"
          data-testid="buzz-error"
        >
          {{ message }}
        </div>
      } @else if (state.notice(); as message) {
        <div
          class="rounded-lg border border-green-200 bg-green-50 p-3 text-sm text-green-800 dark:border-green-800/60 dark:bg-green-900/20 dark:text-green-300"
          role="status"
          data-testid="buzz-notice"
        >
          {{ message }}
        </div>
      }

      <section class="rounded-lg border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-800">
        <h3 class="text-sm font-semibold text-gray-900 dark:text-white">Connect a persona</h3>
        <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">
          The persona gets a Buzz key of its own. What Iff keeps the key; you share its public key (npub) so communities can let it in.
        </p>
        <div class="mt-3 flex max-w-xl flex-wrap gap-3">
          <select
            class="min-w-0 flex-1 rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-900 dark:text-white"
            [ngModel]="newPersonaId()"
            (ngModelChange)="newPersonaId.set($event)"
            aria-label="Persona"
            data-testid="buzz-persona-select"
          >
            <option value="">Choose a persona…</option>
            @for (p of availablePersonas(); track p.id) {
              <option [value]="p.id">{{ p.name }}</option>
            }
          </select>
          <button
            type="button"
            class="rounded-lg bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:cursor-not-allowed disabled:opacity-50"
            [disabled]="!newPersonaId() || state.busy()"
            (click)="connectPersona()"
            data-testid="buzz-connect-persona"
          >
            Connect to Buzz
          </button>
        </div>
      </section>

      @if (state.loading()) {
        <div class="text-sm text-gray-600 dark:text-gray-400">Loading…</div>
      } @else if (state.agents().length === 0) {
        <div class="text-sm text-gray-600 dark:text-gray-400">No personas on Buzz yet.</div>
      }
      @for (a of state.agents(); track a.id) {
        <app-buzz-agent [agent]="a" />
      }
    </div>
  `,
})
export class BuzzTabComponent implements OnInit {
  private readonly buzz = inject(BuzzService);
  readonly state = inject(BuzzTabState);

  readonly newPersonaId = signal('');

  /** Personas without a Buzz identity (one per persona). */
  readonly availablePersonas = computed(() => {
    const taken = new Set(this.state.agents().map(a => a.personality_id));
    return this.state.personas().filter(p => !taken.has(p.id));
  });

  ngOnInit(): void {
    void this.state.load();
  }

  async connectPersona(): Promise<void> {
    const ok = await this.state.run(
      async () => this.state.addAgent(await firstValueFrom(this.buzz.createAgent(this.newPersonaId()))),
      'Could not connect the persona to Buzz.',
    );
    if (ok) this.newPersonaId.set('');
  }
}
