import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideRouter } from '@angular/router';
import { Subject } from 'rxjs';

import { AgentJob } from '../../core/models/agent-job.model';
import { AgentJobService } from '../../core/services/agent-job.service';
import { ImageGalleryService } from '../../core/services/image-gallery.service';
import { PersonalityService } from '../../core/services/personality.service';
import { SidebarRecentJobsComponent } from './sidebar-recent-jobs.component';

describe('SidebarRecentJobsComponent', () => {
  let fixture: ComponentFixture<SidebarRecentJobsComponent>;
  let jobs$: Subject<{ results: AgentJob[] }>;
  let personas$: Subject<{ results: unknown[] }>;

  beforeEach(async () => {
    jobs$ = new Subject();
    personas$ = new Subject();
    await TestBed.configureTestingModule({
      imports: [SidebarRecentJobsComponent],
      providers: [
        provideZonelessChangeDetection(),
        provideRouter([]),
        { provide: AgentJobService, useValue: { listAgentJobs: vi.fn(() => jobs$) } },
        { provide: PersonalityService, useValue: { listPersonalities: vi.fn(() => personas$) } },
        { provide: ImageGalleryService, useValue: { getImageUrl: vi.fn(() => '') } },
      ],
    }).compileComponents();
    fixture = TestBed.createComponent(SidebarRecentJobsComponent);
    fixture.detectChanges();
  });

  const rows = () => Array.from(fixture.nativeElement.querySelectorAll('[data-testid="recent-job-row"]')) as HTMLElement[];

  it('shows the empty hint until jobs arrive, then a row per job linking to its detail', async () => {
    expect(fixture.nativeElement.textContent).toContain('Jobs you create show up here.');

    jobs$.next({
      results: [
        {
          id: 'j1',
          title: 'Morning digest',
          prompt: 'p',
          personality_id: null,
          created_at: '2026-11-01T00:00:00Z',
          last_run_at: null,
        } as unknown as AgentJob,
      ],
    });
    personas$.next({ results: [] });
    fixture.detectChanges();
    await fixture.whenStable();

    expect(rows().length).toBe(1);
    expect(rows()[0].textContent).toContain('Morning digest');
    expect(rows()[0].textContent).toContain('Not run yet');
    expect(rows()[0].getAttribute('href')).toBe('/agent-jobs/j1');
  });

  it('unsubscribes from both requests when destroyed', () => {
    expect(jobs$.observed).toBe(true);
    expect(personas$.observed).toBe(true);

    fixture.destroy();

    expect(jobs$.observed).toBe(false);
    expect(personas$.observed).toBe(false);
  });
});
