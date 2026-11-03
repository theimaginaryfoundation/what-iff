import { Injectable, inject, signal } from '@angular/core';
import { firstValueFrom } from 'rxjs';

import { Personality } from '../../core/models/personality.model';
import { PersonalityService } from '../../core/services/personality.service';
import { apiErrorMessage } from '../../core/utils/api-error.helpers';
import { SlackApp, SlackBinding, SlackService } from './slack.service';

/**
 * What the Integrations → Slack tab shows, shared by its parts (connect form, app cards,
 * channels, bindings): the persona apps, the channel bindings, the personas, and the one error
 * line at the top of the tab. Provided by SlackTabComponent, so it lives as long as the tab.
 */
@Injectable()
export class SlackTabState {
  private readonly slack = inject(SlackService);
  private readonly personalities = inject(PersonalityService);

  readonly loading = signal(true);
  readonly busy = signal(false);
  readonly error = signal<string | null>(null);
  /** A short note about the last action that is not an error ("Connected to Acme", say). */
  readonly notice = signal<string | null>(null);
  readonly apps = signal<SlackApp[]>([]);
  readonly bindings = signal<SlackBinding[]>([]);
  readonly personas = signal<Personality[]>([]);

  async load(): Promise<void> {
    this.personalities.listPersonalities(1, 100).subscribe({
      next: page => this.personas.set(page.results ?? []),
      error: () => this.personas.set([]),
    });
    this.loading.set(true);
    try {
      const [apps, bindings] = await Promise.all([firstValueFrom(this.slack.listApps()), firstValueFrom(this.slack.listBindings())]);
      this.apps.set(apps);
      this.bindings.set(bindings);
    } catch (e) {
      this.fail(e, 'Could not load your Slack apps.');
    } finally {
      this.loading.set(false);
    }
  }

  personaName(personalityId: string): string {
    return this.personas().find(p => p.id === personalityId)?.name ?? 'A persona';
  }

  bindingsFor(appId: string): SlackBinding[] {
    return this.bindings().filter(b => b.app_id === appId);
  }

  /** Runs one action with the tab busy, showing its failure (or `fallback`) at the top of the tab. */
  async run(fn: () => Promise<void>, fallback: string): Promise<boolean> {
    this.busy.set(true);
    this.error.set(null);
    this.notice.set(null);
    try {
      await fn();
      return true;
    } catch (e) {
      this.fail(e, fallback);
      return false;
    } finally {
      this.busy.set(false);
    }
  }

  fail(e: unknown, fallback: string): void {
    this.error.set(apiErrorMessage(e, fallback));
  }

  /** Adds or replaces an app. */
  putApp(app: SlackApp): void {
    this.apps.update(list => (list.some(a => a.id === app.id) ? list.map(a => (a.id === app.id ? app : a)) : [...list, app]));
  }

  /** Removes an app and, as the server does, its bindings. */
  removeApp(id: string): void {
    this.apps.update(list => list.filter(a => a.id !== id));
    this.bindings.update(list => list.filter(b => b.app_id !== id));
  }

  /** Adds or replaces a binding. */
  putBinding(binding: SlackBinding): void {
    this.bindings.update(list =>
      list.some(b => b.id === binding.id) ? list.map(b => (b.id === binding.id ? binding : b)) : [...list, binding],
    );
  }

  removeBinding(id: string): void {
    this.bindings.update(list => list.filter(b => b.id !== id));
  }
}
