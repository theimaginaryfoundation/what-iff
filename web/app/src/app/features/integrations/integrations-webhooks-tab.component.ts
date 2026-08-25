import { Component, OnInit, inject, signal, ChangeDetectionStrategy } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { WebhookTokenService } from '../../core/services/webhook-token.service';
import { WebhookToken } from '../../core/models/webhook-token.model';

@Component({
  selector: 'app-integrations-webhooks-tab',
  standalone: true,
  imports: [CommonModule, FormsModule],
  changeDetection: ChangeDetectionStrategy.Eager,
  templateUrl: './integrations-webhooks-tab.component.html'
})
export class IntegrationsWebhooksTabComponent implements OnInit {
  private confirmationService = inject(ConfirmationService);
  private webhookTokenService = inject(WebhookTokenService);

  webhookTokens = signal<WebhookToken[]>([]);
  webhookTokensLoading = signal(false);
  webhookTokenName = signal('');
  webhookTokenSaving = signal(false);
  justCreatedToken = signal<string | null>(null);

  ngOnInit(): void {
    this.loadWebhookTokens();
  }

  loadWebhookTokens(): void {
    this.webhookTokensLoading.set(true);
    this.webhookTokenService.listWebhookTokens().subscribe({
      next: (tokens) => {
        this.webhookTokens.set(tokens || []);
        this.webhookTokensLoading.set(false);
      },
      error: () => {
        this.webhookTokens.set([]);
        this.webhookTokensLoading.set(false);
      }
    });
  }

  canCreateWebhookToken(): boolean {
    return this.webhookTokenName().trim() !== '' && !this.webhookTokenSaving();
  }

  async createWebhookToken(): Promise<void> {
    if (!this.canCreateWebhookToken()) {
      return;
    }

    this.webhookTokenSaving.set(true);
    this.webhookTokenService.createWebhookToken({ name: this.webhookTokenName().trim() }).subscribe({
      next: (response) => {
        this.webhookTokenSaving.set(false);
        this.webhookTokenName.set('');
        this.justCreatedToken.set(response.api_token);
        this.loadWebhookTokens();
      },
      error: async (error) => {
        this.webhookTokenSaving.set(false);
        await this.confirmationService.alert({
          message: error?.message || 'Failed to create webhook token.',
          type: 'danger'
        });
      }
    });
  }

  async revokeWebhookToken(token: WebhookToken): Promise<void> {
    const confirmed = await this.confirmationService.confirm({
      title: 'Revoke API Token',
      message: `Revoke "${token.name}"? Existing webhook clients using it will stop working.`,
      type: 'danger',
      confirmText: 'Revoke',
      cancelText: 'Cancel'
    });
    if (!confirmed) return;

    this.webhookTokenService.revokeWebhookToken(token.id).subscribe({
      next: () => {
        this.loadWebhookTokens();
      },
      error: async (error) => {
        await this.confirmationService.alert({
          message: error?.message || 'Failed to revoke webhook token.',
          type: 'danger'
        });
      }
    });
  }

  async copyJustCreatedToken(): Promise<void> {
    const token = this.justCreatedToken();
    if (!token) return;

    try {
      await navigator.clipboard.writeText(token);
      await this.confirmationService.alert({
        message: 'API token copied to clipboard.',
        type: 'success'
      });
    } catch {
      await this.confirmationService.alert({
        message: 'Unable to copy token automatically. Please copy it manually.',
        type: 'warning'
      });
    }
  }

  dismissJustCreatedToken(): void {
    this.justCreatedToken.set(null);
  }

  trackByWebhookTokenId(index: number, token: WebhookToken): string {
    return token.id;
  }
}
