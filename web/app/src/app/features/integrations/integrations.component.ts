import { Component, OnInit, inject, signal, ChangeDetectionStrategy, DestroyRef } from '@angular/core';
import { CommonModule, NgComponentOutlet } from '@angular/common';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Router } from '@angular/router';
import { AccessGate } from '../../core/services/access-gate';
import { IntegrationTab, IntegrationTabSource } from '../../core/services/integration-tab-source';
import { IntegrationsConnectorsTabComponent } from './integrations-connectors-tab.component';
import { IntegrationsWebhooksTabComponent } from './integrations-webhooks-tab.component';
import { HelpHintComponent } from '../../shared/ui/help-hint/help-hint.component';
import { TooltipDirective } from '../../shared/ui/tooltip/tooltip.directive';

@Component({
  selector: 'app-integrations',
  standalone: true,
  imports: [CommonModule, NgComponentOutlet, IntegrationsConnectorsTabComponent, IntegrationsWebhooksTabComponent, HelpHintComponent, TooltipDirective],
  templateUrl: './integrations.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  styleUrls: ['./integrations.component.scss']
})
export class IntegrationsComponent implements OnInit {
  private router = inject(Router);
  private accessGate = inject(AccessGate);
  private tabSource = inject(IntegrationTabSource);
  private destroyRef = inject(DestroyRef);

  /** 'connectors', 'webhooks', or the id of a contributed tab. */
  activeTab = signal<string>('connectors');
  /** True when access-gated features (connectors) are available. */
  hasAccess = signal(false);
  /** Tabs contributed by another build (none by default). */
  extraTabs = signal<IntegrationTab[]>([]);

  ngOnInit(): void {
    this.accessGate.hasAccess().subscribe({
      next: (ok) => {
        this.hasAccess.set(ok);
        if (ok) this.activeTab.set('connectors');
      },
      error: () => {
        this.hasAccess.set(false);
      }
    });
    this.tabSource
      .tabs()
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (tabs) => {
          this.extraTabs.set(tabs);
          // A contributed tab can disappear (entitlements change); fall back rather than show nothing.
          const builtIn = this.activeTab() === 'connectors' || this.activeTab() === 'webhooks';
          if (!builtIn && !tabs.some((t) => t.id === this.activeTab())) this.activeTab.set('connectors');
        },
        error: () => this.extraTabs.set([]),
      });
  }

  setActiveTab(tab: string): void {
    this.activeTab.set(tab);
  }
}
