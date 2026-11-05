import { AsyncPipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, OnInit, computed, inject, signal } from '@angular/core';
import { RouterLink, RouterLinkActive } from '@angular/router';

import { AgentJobService } from '../../core/services/agent-job.service';
import { AgentJob } from '../../core/models/agent-job.model';
import { Personality } from '../../core/models/personality.model';
import { PersonalityService } from '../../core/services/personality.service';
import { AuthImagePipe } from '../../core/pipes/auth-image.pipe';
import { UserIconComponent } from '../../shared/ui/icons/icons';
import { ImageGalleryService } from '../../core/services/image-gallery.service';
import { personalityCoverUrl } from '../../features/personality/helpers/cover-image.helpers';
import { personalityAccent, personalityAccentSurface } from '../../features/personality/helpers/personality-vm.helpers';
import { thumbnailCircleToCirclePreviewTransform } from '../../shared/ui/avatar/avatar-thumbnail.helpers';
import { pickRecentJobs, toSidebarJobRow } from './sidebar-recent-jobs.helpers';

/** Jobs are fetched in one page, then the most recently run are picked client-side (the API sorts by creation). */
const FETCH_LIMIT = 50;
/** Same portrait crop as the thread rows in AppSidebarComponent, so a personality looks identical in both lists. */
const PORTRAIT_SOURCE_WIDTH = 200;
const PORTRAIT_SOURCE_HEIGHT = 267;
const AVATAR_SIZE = 30;

/**
 * "Recent jobs" block in the left sidebar: up to ten scheduled jobs, newest run first, each as a
 * personality avatar, job title and a "Last run …" line, separated by dividers like the thread list.
 * Selecting one opens its detail page. Shown only on the Jobs pages (see AppSidebarComponent).
 */
@Component({
  selector: 'app-sidebar-recent-jobs',
  standalone: true,
  imports: [RouterLink, RouterLinkActive, AsyncPipe, AuthImagePipe, UserIconComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <section class="jobs" aria-label="Recent jobs" data-testid="recent-jobs">
      <header class="jobs__header">Recent jobs</header>
      @if (rows().length === 0) {
        <p class="jobs__empty">Jobs you create show up here.</p>
      } @else {
        <ul class="jobs__list">
          @for (row of rows(); track row.id) {
            <li class="jobs__item">
              <a
                class="jobs__row"
                [routerLink]="['/agent-jobs', row.id]"
                routerLinkActive="jobs__row--active"
                ariaCurrentWhenActive="page"
                data-testid="recent-job-row"
              >
                @let avatar = avatarFor(row.personalityId, row.title);
                <span class="jobs__avatar" aria-hidden="true" [style.background]="avatar.surface" [style.color]="avatar.accent">
                  @if (avatar.coverUrl && (avatar.coverUrl | authImage | async); as src) {
                    @if (avatar.transform; as transform) {
                      <span class="jobs__avatar-frame" [style.transform]="transform">
                        <img class="jobs__avatar-frame-image" [src]="src" alt="" />
                      </span>
                    } @else {
                      <img class="jobs__avatar-image" [src]="src" alt="" />
                    }
                  } @else {
                    <ui-user-icon [size]="14" />
                  }
                </span>
                <span class="jobs__text">
                  <span class="jobs__name">{{ row.title }}</span>
                  <span class="jobs__sr-sep">. </span>
                  <span class="jobs__last-run">{{ row.lastRun }}</span>
                </span>
              </a>
            </li>
          }
        </ul>
      }
    </section>
  `,
  styles: [
    `
      :host {
        display: flex;
        flex-direction: column;
        flex: 1;
        min-height: 0;
      }
      .jobs {
        display: flex;
        flex-direction: column;
        flex: 1;
        min-height: 0;
        padding: 6px 10px;
      }
      .jobs__header {
        color: var(--text-secondary);
        font-size: 10px;
        font-weight: 700;
        letter-spacing: 0.06em;
        text-transform: uppercase;
        padding-bottom: 6px;
      }
      .jobs__empty {
        color: var(--text-muted);
        font-size: 11px;
        margin: 0;
      }
      .jobs__list {
        list-style: none;
        margin: 0;
        padding: 0;
        overflow-y: auto;
        min-height: 0;
      }
      .jobs__item + .jobs__item {
        border-top: 1px solid var(--border);
      }
      .jobs__row {
        display: flex;
        align-items: center;
        gap: 8px;
        padding: 7px 6px;
        border-radius: 7px;
        text-decoration: none;
        min-width: 0;
      }
      .jobs__row:hover,
      .jobs__row--active {
        background: var(--accent-bg);
      }
      .jobs__avatar {
        width: 30px;
        height: 30px;
        border-radius: 999px;
        overflow: hidden;
        position: relative;
        border: 1px solid var(--border);
        display: inline-flex;
        align-items: center;
        justify-content: center;
        flex-shrink: 0;
      }
      .jobs__avatar-image {
        width: 100%;
        height: 100%;
        object-fit: cover;
        object-position: center top;
      }
      .jobs__avatar-frame {
        width: 12.5rem;
        height: 16.6875rem;
        position: absolute;
        left: 0;
        top: 0;
        transform-origin: top left;
      }
      .jobs__avatar-frame-image {
        width: 100%;
        height: 100%;
        object-fit: cover;
        object-position: center top;
      }
      .jobs__text {
        display: flex;
        flex-direction: column;
        gap: 1px;
        min-width: 0;
      }
      .jobs__name {
        color: var(--text-secondary);
        font-size: 11px;
        font-weight: 600;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
      }
      /* Pause between the name and the last run for screen readers; invisible on screen. */
      .jobs__sr-sep {
        position: absolute;
        width: 1px;
        height: 1px;
        overflow: hidden;
        clip: rect(0 0 0 0);
        white-space: nowrap;
      }
      /* Secondary (not muted) text: muted is 3.67:1 at this size, below the 4.5:1 minimum. */
      .jobs__last-run {
        color: var(--text-secondary);
        font-size: 10px;
      }
      .jobs__row:focus-visible {
        outline: 2px solid var(--accent);
        outline-offset: 1px;
      }
    `,
  ],
})
export class SidebarRecentJobsComponent implements OnInit {
  private readonly jobsApi = inject(AgentJobService);
  private readonly personalityApi = inject(PersonalityService);
  private readonly imageGallery = inject(ImageGalleryService);

  private readonly jobs = signal<AgentJob[]>([]);
  private readonly personas = signal<Map<string, Personality>>(new Map());
  readonly rows = computed(() => pickRecentJobs(this.jobs()).map(toSidebarJobRow));

  ngOnInit(): void {
    this.jobsApi.listAgentJobs(1, FETCH_LIMIT).subscribe({
      next: response => this.jobs.set(response.results ?? []),
      error: () => this.jobs.set([]),
    });
    this.personalityApi.listPersonalities(1, 100).subscribe({
      next: response => this.personas.set(new Map((response.results ?? []).map(p => [p.id, p] as const))),
      error: () => this.personas.set(new Map()),
    });
  }

  avatarFor(personalityId: string | null, fallbackName: string) {
    const persona = personalityId ? (this.personas().get(personalityId) ?? null) : null;
    const accent = personalityAccent(persona ?? { id: personalityId ?? '', name: fallbackName, accent_color: null });
    return {
      coverUrl: persona ? personalityCoverUrl(persona, [], this.imageGallery.getImageUrl.bind(this.imageGallery)) : null,
      accent,
      surface: personalityAccentSurface(accent),
      transform: thumbnailCircleToCirclePreviewTransform(persona?.thumbnail_circle, PORTRAIT_SOURCE_WIDTH, PORTRAIT_SOURCE_HEIGHT, AVATAR_SIZE),
    };
  }
}
