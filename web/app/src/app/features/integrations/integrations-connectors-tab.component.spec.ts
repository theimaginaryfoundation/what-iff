import type { MockedObject } from 'vitest';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { of, throwError } from 'rxjs';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { MCPServerService } from '../../core/services/mcp-server.service';
import { RitualService } from '../../core/services/ritual.service';
import { IntegrationsConnectorsTabComponent } from './integrations-connectors-tab.component';

describe('IntegrationsConnectorsTabComponent', () => {
  let fixture: ComponentFixture<IntegrationsConnectorsTabComponent>;
  let component: IntegrationsConnectorsTabComponent;
  let mcpServerService: Pick<MockedObject<MCPServerService>, 'listMCPServers' | 'getMCPServer' | 'createMCPServer' | 'updateMCPServer' | 'deleteMCPServer' | 'testMCPServerConnection'>;

  beforeEach(async () => {
    mcpServerService = {
      listMCPServers: vi.fn().mockName('MCPServerService.listMCPServers'),
      getMCPServer: vi.fn().mockName('MCPServerService.getMCPServer'),
      createMCPServer: vi.fn().mockName('MCPServerService.createMCPServer'),
      updateMCPServer: vi.fn().mockName('MCPServerService.updateMCPServer'),
      deleteMCPServer: vi.fn().mockName('MCPServerService.deleteMCPServer'),
      testMCPServerConnection: vi.fn().mockName('MCPServerService.testMCPServerConnection')
    };
    mcpServerService.listMCPServers.mockReturnValue(of({ results: [], total_count: 0, page: 1 }));
    mcpServerService.testMCPServerConnection.mockReturnValue(of({ pass: true, tool_count: 2, message: 'ok' }));
    const ritualService: Pick<MockedObject<RitualService>, 'listRituals'> = {
      listRituals: vi.fn().mockName('RitualService.listRituals')
    };
    ritualService.listRituals.mockReturnValue(of({ results: [], total_count: 0, page: 1 }));

    const confirmationService: Pick<MockedObject<ConfirmationService>, 'confirm' | 'alert'> = {
      confirm: vi.fn().mockName('ConfirmationService.confirm'),
      alert: vi.fn().mockName('ConfirmationService.alert')
    };
    confirmationService.confirm.mockResolvedValue(true);
    confirmationService.alert.mockResolvedValue();

    await TestBed.configureTestingModule({
      imports: [IntegrationsConnectorsTabComponent],
      providers: [
        provideZonelessChangeDetection(),
        { provide: MCPServerService, useValue: mcpServerService },
        { provide: RitualService, useValue: ritualService },
        { provide: ConfirmationService, useValue: confirmationService }
      ]
    }).compileComponents();

    fixture = TestBed.createComponent(IntegrationsConnectorsTabComponent);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  it('tests connection with unsaved create form values', () => {
    component.formServerURL.set('https://example.com/mcp');
    component.formAuthentication.set('Bearer token');

    component.test();

    expect(mcpServerService.testMCPServerConnection).toHaveBeenCalledWith({
      server_url: 'https://example.com/mcp',
      authentication: 'Bearer token'
    });
    expect(component.testResult()?.pass).toBe(true);
    expect(component.testResult()?.tool_count).toBe(2);
  });

  it('uses connector_id fallback when editing and auth is omitted', () => {
    component.editingServer.set({
      id: '4f36536c-90da-4fc4-91eb-5bb8b0e085f3',
      user_id: '9f01f759-5ed8-421f-a40d-6535db3c4dfa',
      name: 'Tracker',
      description: 'desc',
      server_url: 'https://old.example.com/mcp',
      status: 'active',
      default_enabled: false,
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z'
    });
    component.formServerURL.set('https://new.example.com/mcp');
    component.formAuthentication.set('');
    component.formClearAuthentication.set(false);

    component.test();

    expect(mcpServerService.testMCPServerConnection).toHaveBeenCalledWith({
      server_url: 'https://new.example.com/mcp',
      connector_id: '4f36536c-90da-4fc4-91eb-5bb8b0e085f3'
    });
  });

  it('sends explicit null auth when token is being unset', () => {
    component.editingServer.set({
      id: 'dd57f6cf-37af-4e71-87bb-6a7f1d5b8979',
      user_id: '7d7df1e1-f73c-41ca-b749-9333899fd7f8',
      name: 'Mail',
      description: 'desc',
      server_url: 'https://mail.example.com/mcp',
      status: 'active',
      default_enabled: false,
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z'
    });
    component.formServerURL.set('https://mail.example.com/mcp');
    component.formClearAuthentication.set(true);

    component.test();

    expect(mcpServerService.testMCPServerConnection).toHaveBeenCalledWith({
      server_url: 'https://mail.example.com/mcp',
      connector_id: 'dd57f6cf-37af-4e71-87bb-6a7f1d5b8979',
      authentication: null
    });
  });

  it('maps test errors to a failed test result state', () => {
    mcpServerService.testMCPServerConnection.mockReturnValue(
      throwError(() => new Error('auth failed'))
    );
    component.formServerURL.set('https://bad.example.com/mcp');

    component.test();

    expect(component.testResult()).toEqual({
      pass: false,
      tool_count: 0,
      message: 'auth failed'
    });
  });
});
