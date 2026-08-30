import { HttpClient } from '@angular/common/http';
import { CommonModule } from '@angular/common';
import { ChangeDetectionStrategy, Component, OnInit, inject, signal } from '@angular/core';
import { environment } from '../../../environments/environment';
import { AccessGate } from '../../core/services/access-gate';
import { IntegrationsConnectorsTabComponent } from './integrations-connectors-tab.component';
import { IntegrationsWebhooksTabComponent } from './integrations-webhooks-tab.component';

interface ToolMeta {
  name: string;
  description: string;
}

type IntegrationsTab = 'tools' | 'connectors' | 'webhooks';

@Component({
  selector: 'app-integrations',
  standalone: true,
  imports: [CommonModule, IntegrationsConnectorsTabComponent, IntegrationsWebhooksTabComponent],
  templateUrl: './integrations.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  styleUrls: ['./integrations.component.scss']
})
export class IntegrationsComponent implements OnInit {
  private readonly http = inject(HttpClient);
  private readonly accessGate = inject(AccessGate);

  activeTab = signal<IntegrationsTab>('tools');
  /** True when access-gated features (connectors) are available. */
  hasAccess = signal(false);
  tools = signal<readonly ToolMeta[]>([]);
  toolsLoading = signal(true);
  toolsError = signal(false);

  ngOnInit(): void {
    this.http.get<ToolMeta[]>(`${environment.apiUrl}/tools`).subscribe({
      next: tools => {
        this.tools.set(tools ?? []);
        this.toolsLoading.set(false);
      },
      error: () => {
        this.tools.set([]);
        this.toolsError.set(true);
        this.toolsLoading.set(false);
      }
    });

    this.accessGate.hasAccess().subscribe({
      next: ok => this.hasAccess.set(ok),
      error: () => this.hasAccess.set(false)
    });
  }

  setActiveTab(tab: IntegrationsTab): void {
    if ((tab === 'connectors' || tab === 'webhooks') && !this.hasAccess()) return;
    this.activeTab.set(tab);
  }
}
