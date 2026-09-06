import { Component, OnInit, computed, inject, signal, ChangeDetectionStrategy } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { ActivatedRoute } from '@angular/router';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { MCPServerService } from '../../core/services/mcp-server.service';
import { RitualService } from '../../core/services/ritual.service';
import {
  MCPServer,
  CreateMCPServerRequest,
  UpdateMCPServerRequest,
  TestMCPServerConnectionRequest,
  TestMCPServerConnectionResponse
} from '../../core/models/mcp-server.model';
import { Ritual } from '../../core/models/ritual.model';

@Component({
  selector: 'app-integrations-connectors-tab',
  standalone: true,
  imports: [CommonModule, FormsModule],
  changeDetection: ChangeDetectionStrategy.Eager,
  templateUrl: './integrations-connectors-tab.component.html'
})
export class IntegrationsConnectorsTabComponent implements OnInit {
  private confirmationService = inject(ConfirmationService);
  private mcpServerService = inject(MCPServerService);
  private ritualService = inject(RitualService);
  private route = inject(ActivatedRoute);

  isLoading = signal(false);
  isSaving = signal(false);
  isTesting = signal(false);
  servers = signal<MCPServer[]>([]);
  totalCount = signal(0);
  search = signal('');
  currentPage = signal(1);
  pageSize = signal(10);
  editingServer = signal<MCPServer | null>(null);

  formName = signal('');
  formDescription = signal('');
  formServerURL = signal('');
  formAuthMode = signal<'header' | 'oauth'>('header');
  formAuthentication = signal('');
  formClearAuthentication = signal(false);
  formOAuthAuthURL = signal('');
  formOAuthTokenURL = signal('');
  formOAuthClientID = signal('');
  formOAuthClientSecret = signal('');
  formClearOAuthClientSecret = signal(false);
  formOAuthScopes = signal('');
  formOAuthPKCEPolicy = signal<'required' | 'supported' | 'not_supported'>('supported');
  formDefaultEnabled = signal(false);
  rituals = signal<Ritual[]>([]);
  selectedRitualIds = signal<string[]>([]);
  testResult = signal<TestMCPServerConnectionResponse | null>(null);
  oauthBanner = signal<{ success: boolean; message: string } | null>(null);

  canSave = computed(() => {
    return this.formName().trim() !== '' &&
      this.formDescription().trim() !== '' &&
      this.formServerURL().trim() !== '' &&
      !this.isSaving();
  });

  canTest = computed(() => {
    return this.formAuthMode() !== 'oauth' && this.formServerURL().trim() !== '' && !this.isTesting() && !this.isSaving();
  });

  ngOnInit(): void {
    const status = this.route.snapshot.queryParamMap.get('oauth_status');
    const message = this.route.snapshot.queryParamMap.get('oauth_message');
    if (status === 'success' || status === 'error') {
      this.oauthBanner.set({
        success: status === 'success',
        message: message?.trim() || (status === 'success' ? 'Connector authenticated successfully.' : 'Connector authentication failed.')
      });
    }
    this.loadServers();
    this.loadRituals();
  }

  loadRituals(): void {
    this.ritualService.listRituals(1, 200).subscribe({
      next: (res) => this.rituals.set(res.results),
      error: () => this.rituals.set([])
    });
  }

  loadServers(): void {
    this.isLoading.set(true);
    this.mcpServerService.listMCPServers(this.currentPage(), this.pageSize(), {
      search: this.search().trim() || undefined
    }).subscribe({
      next: (response) => {
        this.servers.set(response.results || []);
        this.totalCount.set(response.total_count || 0);
        this.isLoading.set(false);
      },
      error: () => {
        this.isLoading.set(false);
        this.servers.set([]);
        this.totalCount.set(0);
      }
    });
  }

  onSearch(): void {
    this.currentPage.set(1);
    this.loadServers();
  }

  onPageChange(page: number): void {
    this.currentPage.set(page);
    this.loadServers();
  }

  startCreate(): void {
    this.editingServer.set(null);
    this.formName.set('');
    this.formDescription.set('');
    this.formServerURL.set('');
    this.formAuthMode.set('header');
    this.formAuthentication.set('');
    this.formClearAuthentication.set(false);
    this.formOAuthAuthURL.set('');
    this.formOAuthTokenURL.set('');
    this.formOAuthClientID.set('');
    this.formOAuthClientSecret.set('');
    this.formClearOAuthClientSecret.set(false);
    this.formOAuthScopes.set('');
    this.formOAuthPKCEPolicy.set('supported');
    this.formDefaultEnabled.set(false);
    this.selectedRitualIds.set([]);
    this.clearTestResult();
  }

  startEdit(server: MCPServer): void {
    this.mcpServerService.getMCPServer(server.id).subscribe({
      next: (full) => {
        this.editingServer.set(full);
        this.formName.set(full.name);
        this.formDescription.set(full.description);
        this.formServerURL.set(full.server_url);
        this.formAuthMode.set((full.auth_mode || 'header') as 'header' | 'oauth');
        this.formAuthentication.set('');
        this.formClearAuthentication.set(false);
        this.formOAuthAuthURL.set(full.oauth_auth_url || '');
        this.formOAuthTokenURL.set(full.oauth_token_url || '');
        this.formOAuthClientID.set(full.oauth_client_id || '');
        this.formOAuthClientSecret.set('');
        this.formClearOAuthClientSecret.set(false);
        this.formOAuthScopes.set((full.oauth_scopes || []).join(' '));
        this.formOAuthPKCEPolicy.set((full.oauth_pkce_policy || 'supported') as 'required' | 'supported' | 'not_supported');
        this.formDefaultEnabled.set(full.default_enabled);
        this.selectedRitualIds.set(full.ritual_ids ? [...full.ritual_ids] : []);
        this.clearTestResult();
      },
      error: () => {
        this.editingServer.set(server);
        this.formName.set(server.name);
        this.formDescription.set(server.description);
        this.formServerURL.set(server.server_url);
        this.formAuthMode.set((server.auth_mode || 'header') as 'header' | 'oauth');
        this.formAuthentication.set('');
        this.formClearAuthentication.set(false);
        this.formOAuthAuthURL.set(server.oauth_auth_url || '');
        this.formOAuthTokenURL.set(server.oauth_token_url || '');
        this.formOAuthClientID.set(server.oauth_client_id || '');
        this.formOAuthClientSecret.set('');
        this.formClearOAuthClientSecret.set(false);
        this.formOAuthScopes.set((server.oauth_scopes || []).join(' '));
        this.formOAuthPKCEPolicy.set((server.oauth_pkce_policy || 'supported') as 'required' | 'supported' | 'not_supported');
        this.formDefaultEnabled.set(server.default_enabled);
        this.selectedRitualIds.set(server.ritual_ids ? [...server.ritual_ids] : []);
        this.clearTestResult();
      }
    });
  }

  isRitualSelected(ritualId: string): boolean {
    return this.selectedRitualIds().includes(ritualId);
  }

  toggleRitual(ritualId: string, checked: boolean): void {
    const next = new Set(this.selectedRitualIds());
    if (checked) {
      next.add(ritualId);
    } else {
      next.delete(ritualId);
    }
    this.selectedRitualIds.set([...next]);
  }

  onAuthenticationChange(value: string): void {
    this.formAuthentication.set(value);
    this.formClearAuthentication.set(false);
    this.clearTestResult();
  }

  unsetAuthenticationToken(): void {
    this.formAuthentication.set('');
    this.formClearAuthentication.set(true);
    this.clearTestResult();
  }

  unsetOAuthClientSecret(): void {
    this.formOAuthClientSecret.set('');
    this.formClearOAuthClientSecret.set(true);
    this.clearTestResult();
  }

  onFormValueChange(): void {
    this.clearTestResult();
  }

  test(): void {
    if (!this.canTest()) return;

    this.isTesting.set(true);
    this.clearTestResult();
    const payload = this.buildTestPayload();

    this.mcpServerService.testMCPServerConnection(payload).subscribe({
      next: (result) => {
        this.isTesting.set(false);
        this.testResult.set(result);
      },
      error: (error) => {
        this.isTesting.set(false);
        this.testResult.set({
          pass: false,
          tool_count: 0,
          message: error?.message || 'Connection test failed.'
        });
      }
    });
  }

  async save(): Promise<void> {
    if (!this.canSave()) return;

    this.isSaving.set(true);
    const editing = this.editingServer();
    const request$ = editing
      ? (() => {
          const payload: UpdateMCPServerRequest = {
            name: this.formName().trim(),
            description: this.formDescription().trim(),
            server_url: this.formServerURL().trim(),
            auth_mode: this.formAuthMode(),
            authentication: this.formClearAuthentication() ? null : this.formAuthentication(),
            default_enabled: this.formDefaultEnabled(),
            ritual_ids: [...this.selectedRitualIds()],
            oauth_auth_url: this.formOAuthAuthURL().trim() || undefined,
            oauth_token_url: this.formOAuthTokenURL().trim() || undefined,
            oauth_client_id: this.formOAuthClientID().trim() || undefined,
            oauth_client_secret: this.formClearOAuthClientSecret() ? null : (this.formOAuthClientSecret().trim() || undefined),
            oauth_scopes: this.formOAuthScopes().trim() === '' ? [] : this.formOAuthScopes().trim().split(/\s+/),
            oauth_pkce_policy: this.formOAuthPKCEPolicy()
          };
          return this.mcpServerService.updateMCPServer(editing.id, payload);
        })()
      : (() => {
          const payload: CreateMCPServerRequest = {
            name: this.formName().trim(),
            description: this.formDescription().trim(),
            server_url: this.formServerURL().trim(),
            auth_mode: this.formAuthMode(),
            authentication: this.formAuthentication(),
            default_enabled: this.formDefaultEnabled(),
            oauth_auth_url: this.formOAuthAuthURL().trim() || undefined,
            oauth_token_url: this.formOAuthTokenURL().trim() || undefined,
            oauth_client_id: this.formOAuthClientID().trim() || undefined,
            oauth_client_secret: this.formOAuthClientSecret().trim() || undefined,
            oauth_scopes: this.formOAuthScopes().trim() === '' ? [] : this.formOAuthScopes().trim().split(/\s+/),
            oauth_pkce_policy: this.formOAuthPKCEPolicy()
          };
          return this.mcpServerService.createMCPServer(payload);
        })();

    request$.subscribe({
      next: () => {
        this.isSaving.set(false);
        this.startCreate();
        this.loadServers();
      },
      error: async (error) => {
        this.isSaving.set(false);
        await this.confirmationService.alert({
          message: error?.message || 'Failed to save connector.',
          type: 'danger'
        });
      }
    });
  }

  async delete(server: MCPServer): Promise<void> {
    const confirmed = await this.confirmationService.confirm({
      title: 'Delete Connector',
      message: `Delete "${server.name}"? This cannot be undone.`,
      type: 'danger',
      confirmText: 'Delete',
      cancelText: 'Cancel'
    });
    if (!confirmed) return;

    this.mcpServerService.deleteMCPServer(server.id).subscribe({
      next: () => {
        this.loadServers();
      },
      error: async (error) => {
        await this.confirmationService.alert({
          message: error?.message || 'Failed to delete connector.',
          type: 'danger'
        });
      }
    });
  }

  getTotalPages(): number {
    return Math.max(1, Math.ceil(this.totalCount() / this.pageSize()));
  }

  trackByServerId(index: number, server: MCPServer): string {
    return server.id;
  }

  authActionLabel(server: MCPServer): string {
    if ((server.auth_mode || 'header') !== 'oauth') return '';
    return server.oauth_has_refresh_token || server.oauth_has_access_token ? 'Reauthenticate' : 'Authenticate';
  }

  authenticate(server: MCPServer): void {
    if ((server.auth_mode || 'header') !== 'oauth') return;
    this.mcpServerService.startOAuth(server.id, { redirect_after: window.location.href }).subscribe({
      next: (res) => {
        if (res.authorization_url) {
          window.location.href = res.authorization_url;
        }
      },
      error: async (error) => {
        await this.confirmationService.alert({
          message: error?.message || 'Failed to start connector authentication.',
          type: 'danger'
        });
      }
    });
  }

  private clearTestResult(): void {
    this.testResult.set(null);
  }

  private buildTestPayload(): TestMCPServerConnectionRequest {
    const editing = this.editingServer();
    const payload: TestMCPServerConnectionRequest = {
      server_url: this.formServerURL().trim(),
      auth_mode: this.formAuthMode()
    };

    if (editing) {
      payload.connector_id = editing.id;
    }

    if (this.formClearAuthentication()) {
      payload.authentication = null;
      return payload;
    }

    const auth = this.formAuthentication().trim();
    if (auth !== '') {
      payload.authentication = auth;
    }

    return payload;
  }
}
