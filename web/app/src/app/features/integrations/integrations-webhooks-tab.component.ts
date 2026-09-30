import { Component, OnInit, inject, signal, ChangeDetectionStrategy } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { HelpHintComponent } from '../../shared/ui/help-hint/help-hint.component';
import { TooltipDirective } from '../../shared/ui/tooltip/tooltip.directive';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { WebhookTokenService } from '../../core/services/webhook-token.service';
import { WEBHOOK_SCOPE_OPTIONS, WebhookScope, WebhookToken } from '../../core/models/webhook-token.model';

@Component({
  selector: 'app-integrations-webhooks-tab',
  standalone: true,
  imports: [CommonModule, FormsModule, HelpHintComponent, TooltipDirective],
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

  /** The scopes a new token can be given. Posting is on by default, as tokens have always been; reading is opt-in. */
  readonly scopeOptions = WEBHOOK_SCOPE_OPTIONS;
  selectedScopes = signal<ReadonlySet<WebhookScope>>(new Set<WebhookScope>(['messages:write']));

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
    return this.webhookTokenName().trim() !== '' && this.selectedScopes().size > 0 && !this.webhookTokenSaving();
  }

  isScopeSelected(scope: WebhookScope): boolean {
    return this.selectedScopes().has(scope);
  }

  toggleScope(scope: WebhookScope, selected: boolean): void {
    const next = new Set(this.selectedScopes());
    if (selected) {
      next.add(scope);
    } else {
      next.delete(scope);
    }
    this.selectedScopes.set(next);
  }

  scopeLabel(scope: WebhookScope): string {
    return this.scopeOptions.find((option) => option.scope === scope)?.label ?? scope;
  }

  /** Tooltip for an active token: what it can do, in plain words. */
  activeTokenHint(token: WebhookToken): string {
    const parts = (token.scopes ?? []).map((scope) => this.scopeOptions.find((o) => o.scope === scope)?.description ?? scope);
    return parts.length > 0 ? parts.join(' ') : 'Active';
  }

  async createWebhookToken(): Promise<void> {
    if (!this.canCreateWebhookToken()) {
      return;
    }

    this.webhookTokenSaving.set(true);
    // Send scopes in a stable order so the request does not depend on click order.
    const scopes = this.scopeOptions.map((o) => o.scope).filter((scope) => this.selectedScopes().has(scope));
    this.webhookTokenService.createWebhookToken({ name: this.webhookTokenName().trim(), scopes }).subscribe({
      next: (response) => {
        this.webhookTokenSaving.set(false);
        this.webhookTokenName.set('');
        this.selectedScopes.set(new Set<WebhookScope>(['messages:write']));
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
