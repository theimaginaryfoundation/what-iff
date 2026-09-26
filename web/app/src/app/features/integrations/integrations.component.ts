import { Component, OnInit, inject, signal, ChangeDetectionStrategy } from '@angular/core';
import { CommonModule } from '@angular/common';
import { Router } from '@angular/router';
import { AccessGate } from '../../core/services/access-gate';
import { IntegrationsConnectorsTabComponent } from './integrations-connectors-tab.component';
import { IntegrationsWebhooksTabComponent } from './integrations-webhooks-tab.component';
import { HelpHintComponent } from '../../shared/ui/help-hint/help-hint.component';
import { TooltipDirective } from '../../shared/ui/tooltip/tooltip.directive';

@Component({
  selector: 'app-integrations',
  standalone: true,
  imports: [CommonModule, IntegrationsConnectorsTabComponent, IntegrationsWebhooksTabComponent, HelpHintComponent, TooltipDirective],
  templateUrl: './integrations.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  styleUrls: ['./integrations.component.scss']
})
export class IntegrationsComponent implements OnInit {
  private router = inject(Router);
  private accessGate = inject(AccessGate);

  activeTab = signal<'connectors' | 'webhooks'>('connectors');
  /** True when access-gated features (connectors) are available. */
  hasAccess = signal(false);

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
  }

  setActiveTab(tab: 'connectors' | 'webhooks'): void {
    this.activeTab.set(tab);
  }
}
