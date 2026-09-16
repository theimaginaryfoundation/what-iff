import { Component, OnInit, inject, signal, ChangeDetectionStrategy } from '@angular/core';
import { CommonModule } from '@angular/common';
import { Router } from '@angular/router';
import { AccessGate } from '../../core/services/access-gate';
import { ProviderKeyService } from '../../core/services/provider-key.service';
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
  private providerKeys = inject(ProviderKeyService);

  activeTab = signal<'api-keys' | 'models' | 'connectors' | 'webhooks'>('api-keys');
  /** True when access-gated features (connectors) are available. */
  hasAccess = signal(false);
  /**
   * True when this account can supply provider keys of its own.
   *
   * Derived from the API rather than from a second swap point: the server
   * already reports whether each provider accepts a per-account key, so asking
   * it keeps one answer in one place. A parallel frontend seam would be a
   * second thing to bind, and the failure mode of forgetting is silent.
   */
  canSupplyKeys = signal(false);

  ngOnInit(): void {
    // The setup guard sends an account with no usable key here with
    // ?setup=api-key, so honour that over any other default.
    const wantsKeySetup = this.router.parseUrl(this.router.url).queryParams['setup'] === 'api-key';

    this.providerKeys.listStatuses().subscribe({
      next: statuses => {
        const any = statuses.some(s => s.supported);
        this.canSupplyKeys.set(any);
        // Nothing to configure here, so do not open on an empty tab.
        if (!any && this.activeTab() === 'api-keys') this.activeTab.set('models');
      },
      // Failing closed would hide the one screen that fixes a missing key.
      error: () => this.canSupplyKeys.set(true),
    });

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
