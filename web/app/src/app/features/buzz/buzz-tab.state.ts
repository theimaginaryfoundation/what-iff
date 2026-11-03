import { Injectable, inject, signal } from '@angular/core';
import { firstValueFrom } from 'rxjs';

import { Personality } from '../../core/models/personality.model';
import { PersonalityService } from '../../core/services/personality.service';
import { apiErrorMessage } from '../../core/utils/api-error.helpers';
import { BuzzAgent, BuzzBinding, BuzzCommunity, BuzzJoinPolicy, BuzzService, termsRequired } from './buzz.service';

/**
 * What the Integrations → Buzz tab shows, shared by its parts (identity cards, communities,
 * bindings): the persona identities with their communities, the channel bindings, and the one
 * error line at the top of the tab. Provided by BuzzTabComponent, so it lives as long as the tab.
 */
@Injectable()
export class BuzzTabState {
  private readonly buzz = inject(BuzzService);
  private readonly personalities = inject(PersonalityService);

  readonly loading = signal(true);
  readonly busy = signal(false);
  readonly error = signal<string | null>(null);
  /** A short note about the last action that is not an error (profile published to 2 of 3, say). */
  readonly notice = signal<string | null>(null);
  readonly agents = signal<BuzzAgent[]>([]);
  readonly bindings = signal<BuzzBinding[]>([]);
  readonly personas = signal<Personality[]>([]);

  async load(): Promise<void> {
    this.personalities.listPersonalities(1, 100).subscribe({
      next: page => this.personas.set(page.results ?? []),
      error: () => this.personas.set([]),
    });
    this.loading.set(true);
    try {
      const [agents, bindings] = await Promise.all([firstValueFrom(this.buzz.listAgents()), firstValueFrom(this.buzz.listBindings())]);
      this.agents.set(agents);
      this.bindings.set(bindings);
    } catch (e) {
      this.fail(e, 'Could not load your Buzz identities.');
    } finally {
      this.loading.set(false);
    }
  }

  bindingsFor(communityId: string): BuzzBinding[] {
    return this.bindings().filter(b => b.community_id === communityId);
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

  /**
   * {@link run} for a request that may claim an invite: a refusal because the community's join
   * terms have not been accepted for the persona is not an error, and returns the terms to show.
   */
  async runClaim(fn: () => Promise<void>, fallback: string): Promise<{ ok: boolean; terms: BuzzJoinPolicy | null }> {
    // Cast, so the assignment inside the callback is not narrowed away.
    let terms = null as BuzzJoinPolicy | null;
    const ok = await this.run(async () => {
      try {
        await fn();
      } catch (e) {
        terms = termsRequired(e);
        if (!terms) throw e;
      }
    }, fallback);
    return { ok: ok && !terms, terms };
  }

  fail(e: unknown, fallback: string): void {
    this.error.set(apiErrorMessage(e, fallback));
  }

  addAgent(agent: BuzzAgent): void {
    this.agents.update(list => [...list, agent]);
  }

  /** Replaces an agent's own fields, keeping the communities already shown unless the server sent them. */
  replaceAgent(agent: BuzzAgent): void {
    this.agents.update(list => list.map(a => (a.id === agent.id ? { ...agent, communities: agent.communities ?? a.communities } : a)));
  }

  removeAgent(id: string): void {
    const communities = new Set(
      this.agents()
        .find(a => a.id === id)
        ?.communities.map(c => c.id) ?? [],
    );
    this.agents.update(list => list.filter(a => a.id !== id));
    this.bindings.update(list => list.filter(b => !communities.has(b.community_id)));
  }

  /** Adds or replaces a community on its agent. */
  putCommunity(community: BuzzCommunity): void {
    this.agents.update(list =>
      list.map(a => {
        if (a.id !== community.agent_id) return a;
        const has = a.communities.some(c => c.id === community.id);
        return {
          ...a,
          communities: has ? a.communities.map(c => (c.id === community.id ? community : c)) : [...a.communities, community],
        };
      }),
    );
  }

  removeCommunity(community: BuzzCommunity): void {
    this.agents.update(list =>
      list.map(a => (a.id === community.agent_id ? { ...a, communities: a.communities.filter(c => c.id !== community.id) } : a)),
    );
    this.bindings.update(list => list.filter(b => b.community_id !== community.id));
  }

  /** Adds or replaces a binding. */
  putBinding(binding: BuzzBinding): void {
    this.bindings.update(list =>
      list.some(b => b.id === binding.id) ? list.map(b => (b.id === binding.id ? binding : b)) : [...list, binding],
    );
  }

  removeBinding(id: string): void {
    this.bindings.update(list => list.filter(b => b.id !== id));
  }
}
