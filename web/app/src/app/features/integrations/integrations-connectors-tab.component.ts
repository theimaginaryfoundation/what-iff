import { Component, OnInit, computed, inject, signal, ChangeDetectionStrategy } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
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
  formAuthentication = signal('');
  formClearAuthentication = signal(false);
  formDefaultEnabled = signal(false);
  rituals = signal<Ritual[]>([]);
  selectedRitualIds = signal<string[]>([]);
  testResult = signal<TestMCPServerConnectionResponse | null>(null);

  canSave = computed(() => {
    return this.formName().trim() !== '' &&
      this.formDescription().trim() !== '' &&
      this.formServerURL().trim() !== '' &&
      !this.isSaving();
  });

  canTest = computed(() => {
    return this.formServerURL().trim() !== '' && !this.isTesting() && !this.isSaving();
  });

  ngOnInit(): void {
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
    this.formAuthentication.set('');
    this.formClearAuthentication.set(false);
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
        this.formAuthentication.set('');
        this.formClearAuthentication.set(false);
        this.formDefaultEnabled.set(full.default_enabled);
        this.selectedRitualIds.set(full.ritual_ids ? [...full.ritual_ids] : []);
        this.clearTestResult();
      },
      error: () => {
        this.editingServer.set(server);
        this.formName.set(server.name);
        this.formDescription.set(server.description);
        this.formServerURL.set(server.server_url);
        this.formAuthentication.set('');
        this.formClearAuthentication.set(false);
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
            authentication: this.formClearAuthentication() ? null : this.formAuthentication(),
            default_enabled: this.formDefaultEnabled(),
            ritual_ids: [...this.selectedRitualIds()]
          };
          return this.mcpServerService.updateMCPServer(editing.id, payload);
        })()
      : (() => {
          const payload: CreateMCPServerRequest = {
            name: this.formName().trim(),
            description: this.formDescription().trim(),
            server_url: this.formServerURL().trim(),
            authentication: this.formAuthentication(),
            default_enabled: this.formDefaultEnabled()
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

  private clearTestResult(): void {
    this.testResult.set(null);
  }

  private buildTestPayload(): TestMCPServerConnectionRequest {
    const editing = this.editingServer();
    const payload: TestMCPServerConnectionRequest = {
      server_url: this.formServerURL().trim()
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
