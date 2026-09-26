import { ChangeDetectionStrategy, Component, OnInit, inject, signal } from '@angular/core';
import { ActivatedRoute, Router } from '@angular/router';

import { MemoriesListTabComponent } from './memories-list-tab.component';
import { MemoryMergeHistoryPageComponent } from './memory-merge-history-page.component';
import { CompactionLogPageComponent } from './compaction-log-page.component';

export type MemoriesPageTab = 'memories' | 'merge-history' | 'compaction-log';

@Component({
  selector: 'app-memories-page',
  standalone: true,
  imports: [MemoriesListTabComponent, MemoryMergeHistoryPageComponent, CompactionLogPageComponent],
  templateUrl: './memories-page.component.html',
  styleUrl: './memories-page.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class MemoriesPageComponent implements OnInit {
  private readonly route = inject(ActivatedRoute);
  private readonly router = inject(Router);

  readonly activeTab = signal<MemoriesPageTab>('memories');

  ngOnInit(): void {
    this.route.queryParams.subscribe(params => {
      this.activeTab.set(normalizeTab(params['tab']));
    });
  }

  setActiveTab(tab: MemoriesPageTab): void {
    this.activeTab.set(tab);
    void this.router.navigate([], {
      relativeTo: this.route,
      queryParams: tab === 'memories' ? { tab: null } : { tab },
      queryParamsHandling: 'merge',
      replaceUrl: true,
    });
  }
}

function normalizeTab(raw: unknown): MemoriesPageTab {
  if (raw === 'merge-history' || raw === 'compaction-log') return raw;
  return 'memories';
}
