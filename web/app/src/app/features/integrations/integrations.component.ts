import { Component, OnInit, inject, signal, ChangeDetectionStrategy } from '@angular/core';
import { CommonModule } from '@angular/common';
import { Router } from '@angular/router';
import { AccessGate } from '../../core/services/access-gate';
import { IntegrationsApiKeysTabComponent } from './integrations-api-keys-tab.component';
import { IntegrationsConnectorsTabComponent } from './integrations-connectors-tab.component';
import { IntegrationsModelsTabComponent } from './integrations-models-tab.component';
import { IntegrationsWebhooksTabComponent } from './integrations-webhooks-tab.component';

@Component({
  selector: 'app-integrations',
  standalone: true,
  imports: [
    CommonModule,
    IntegrationsApiKeysTabComponent,
    IntegrationsConnectorsTabComponent,
    IntegrationsModelsTabComponent,
    IntegrationsWebhooksTabComponent,
  ],
  templateUrl: './integrations.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  styleUrls: ['./integrations.component.scss'],
})
export class IntegrationsComponent implements OnInit {
  private router = inject(Router);
  private accessGate = inject(AccessGate);

  activeTab = signal<'api-keys' | 'models' | 'connectors' | 'webhooks'>('api-keys');
  /** True when access-gated features (connectors) are available. */
  hasAccess = signal(false);

  ngOnInit(): void {
    // The setup guard sends an account with no usable key here with
    // ?setup=api-key, so honour that over any other default.
    const wantsKeySetup = this.router.parseUrl(this.router.url).queryParams['setup'] === 'api-key';

    this.accessGate.hasAccess().subscribe({
      next: ok => {
        this.hasAccess.set(ok);
        if (ok && !wantsKeySetup) this.activeTab.set('connectors');
      },
      error: () => {
        this.hasAccess.set(false);
      },
    });
  }

  setActiveTab(tab: 'api-keys' | 'models' | 'connectors' | 'webhooks'): void {
    this.activeTab.set(tab);
  }
}
