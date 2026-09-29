import type { MockedObject } from 'vitest';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { of, throwError } from 'rxjs';
import { ActivatedRoute, convertToParamMap } from '@angular/router';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { MCPServerService } from '../../core/services/mcp-server.service';
import { RitualService } from '../../core/services/ritual.service';
import { IntegrationsConnectorsTabComponent } from './integrations-connectors-tab.component';

describe('IntegrationsConnectorsTabComponent', () => {
  let fixture: ComponentFixture<IntegrationsConnectorsTabComponent>;
  let component: IntegrationsConnectorsTabComponent;
  let mcpServerService: Pick<MockedObject<MCPServerService>, 'listMCPServers' | 'getMCPServer' | 'createMCPServer' | 'updateMCPServer' | 'deleteMCPServer' | 'testMCPServerConnection' | 'startOAuth'>;
  let ritualService: Pick<MockedObject<RitualService>, 'listRituals'>;
  let confirmationService: Pick<MockedObject<ConfirmationService>, 'confirm' | 'alert'>;
  let routeParams: Record<string, string>;

  function makeServer(overrides: Partial<Parameters<typeof component.startEdit>[0]> = {}) {
    return {
      id: '4f36536c-90da-4fc4-91eb-5bb8b0e085f3',
      user_id: '9f01f759-5ed8-421f-a40d-6535db3c4dfa',
      name: 'Tracker',
      description: 'desc',
      server_url: 'https://old.example.com/mcp',
      auth_mode: 'header' as const,
      status: 'active',
      default_enabled: false,
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
      ...overrides
    };
  }

  beforeEach(async () => {
    routeParams = {};
    mcpServerService = {
      listMCPServers: vi.fn().mockName('MCPServerService.listMCPServers'),
      getMCPServer: vi.fn().mockName('MCPServerService.getMCPServer'),
      createMCPServer: vi.fn().mockName('MCPServerService.createMCPServer'),
      updateMCPServer: vi.fn().mockName('MCPServerService.updateMCPServer'),
      deleteMCPServer: vi.fn().mockName('MCPServerService.deleteMCPServer'),
      testMCPServerConnection: vi.fn().mockName('MCPServerService.testMCPServerConnection'),
      startOAuth: vi.fn().mockName('MCPServerService.startOAuth')
    };
    mcpServerService.listMCPServers.mockReturnValue(of({ results: [], total_count: 0, page: 1 }));
    mcpServerService.testMCPServerConnection.mockReturnValue(of({ pass: true, tool_count: 2, message: 'ok' }));
    ritualService = {
      listRituals: vi.fn().mockName('RitualService.listRituals')
    };
    ritualService.listRituals.mockReturnValue(of({ results: [], total_count: 0, page: 1 }));

    confirmationService = {
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
        { provide: ConfirmationService, useValue: confirmationService },
        {
          provide: ActivatedRoute,
          useValue: {
            get snapshot() {
              return { queryParamMap: convertToParamMap(routeParams) };
            }
          }
        }
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
      auth_mode: 'header',
      authentication: 'Bearer token'
    });
    expect(component.testResult()?.pass).toBe(true);
    expect(component.testResult()?.tool_count).toBe(2);
  });

  it('uses connector_id fallback when editing and auth is omitted', () => {
    component.editingServer.set(makeServer());
    component.formServerURL.set('https://new.example.com/mcp');
    component.formAuthentication.set('');
    component.formClearAuthentication.set(false);

    component.test();

    expect(mcpServerService.testMCPServerConnection).toHaveBeenCalledWith({
      server_url: 'https://new.example.com/mcp',
      auth_mode: 'header',
      connector_id: '4f36536c-90da-4fc4-91eb-5bb8b0e085f3'
    });
  });

  it('sends explicit null auth when token is being unset', () => {
    component.editingServer.set(makeServer({
      id: 'dd57f6cf-37af-4e71-87bb-6a7f1d5b8979',
      server_url: 'https://mail.example.com/mcp'
    }));
    component.formServerURL.set('https://mail.example.com/mcp');
    component.formClearAuthentication.set(true);

    component.test();

    expect(mcpServerService.testMCPServerConnection).toHaveBeenCalledWith({
      server_url: 'https://mail.example.com/mcp',
      auth_mode: 'header',
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

  it('disables test for oauth mode', () => {
    component.formAuthMode.set('oauth');
    component.formServerURL.set('https://example.com/mcp');
    component.test();
    expect(mcpServerService.testMCPServerConnection).not.toHaveBeenCalled();
  });

  it('shows oauth status banner from query params', () => {
    routeParams = { oauth_status: 'error', oauth_message: 'bad creds' };
    fixture = TestBed.createComponent(IntegrationsConnectorsTabComponent);
    component = fixture.componentInstance;
    fixture.detectChanges();
    expect(component.oauthBanner()).toEqual({ success: false, message: 'bad creds' });
  });

  it('falls back to default oauth success message when blank', () => {
    routeParams = { oauth_status: 'success', oauth_message: '   ' };
    fixture = TestBed.createComponent(IntegrationsConnectorsTabComponent);
    component = fixture.componentInstance;
    fixture.detectChanges();
    expect(component.oauthBanner()).toEqual({
      success: true,
      message: 'Connector authenticated successfully.'
    });
  });

  it('handles load servers error path', () => {
    mcpServerService.listMCPServers.mockReturnValueOnce(throwError(() => new Error('boom')));
    component.loadServers();
    expect(component.isLoading()).toBe(false);
    expect(component.servers()).toEqual([]);
    expect(component.totalCount()).toBe(0);
  });

  it('resets form values when starting create', () => {
    component.editingServer.set(makeServer());
    component.formName.set('x');
    component.formDescription.set('y');
    component.formServerURL.set('https://z');
    component.formAuthentication.set('token');
    component.formClearAuthentication.set(true);
    component.formOAuthClientSecret.set('secret');
    component.formClearOAuthClientSecret.set(true);
    component.testResult.set({ pass: true, tool_count: 1, message: 'ok' });
    component.selectedRitualIds.set(['r1']);

    component.startCreate();

    expect(component.editingServer()).toBeNull();
    expect(component.formName()).toBe('');
    expect(component.formAuthMode()).toBe('header');
    expect(component.formClearAuthentication()).toBe(false);
    expect(component.formClearOAuthClientSecret()).toBe(false);
    expect(component.selectedRitualIds()).toEqual([]);
    expect(component.testResult()).toBeNull();
  });

  it('prefers full server details when startEdit get succeeds', () => {
    mcpServerService.getMCPServer.mockReturnValueOnce(of(makeServer({
      auth_mode: 'oauth',
      oauth_auth_url: 'https://auth.example.com',
      oauth_token_url: 'https://token.example.com',
      oauth_client_id: 'id',
      oauth_scopes: ['openid', 'email'],
      oauth_pkce_policy: 'required',
      default_enabled: true,
      ritual_ids: ['r1']
    })));

    component.startEdit(makeServer({ name: 'summary' }));

    expect(component.editingServer()?.name).toBe('Tracker');
    expect(component.formAuthMode()).toBe('oauth');
    expect(component.formOAuthScopes()).toBe('openid email');
    expect(component.formOAuthPKCEPolicy()).toBe('required');
    expect(component.formDefaultEnabled()).toBe(true);
    expect(component.selectedRitualIds()).toEqual(['r1']);
  });

  it('falls back to summary server when get fails in startEdit', () => {
    mcpServerService.getMCPServer.mockReturnValueOnce(throwError(() => new Error('nope')));
    const summary = makeServer({ name: 'summary fallback' });
    component.startEdit(summary);
    expect(component.editingServer()?.name).toBe('summary fallback');
    expect(component.formName()).toBe('summary fallback');
  });

  it('toggles rituals and reports ritual selection', () => {
    component.toggleRitual('r1', true);
    component.toggleRitual('r2', true);
    component.toggleRitual('r1', false);
    expect(component.selectedRitualIds()).toEqual(['r2']);
    expect(component.isRitualSelected('r2')).toBe(true);
    expect(component.isRitualSelected('r1')).toBe(false);
  });

  it('clears test result on auth and form changes', () => {
    component.testResult.set({ pass: true, tool_count: 1, message: 'ok' });
    component.onAuthenticationChange('abc');
    expect(component.formAuthentication()).toBe('abc');
    expect(component.formClearAuthentication()).toBe(false);
    expect(component.testResult()).toBeNull();

    component.testResult.set({ pass: true, tool_count: 1, message: 'ok' });
    component.unsetAuthenticationToken();
    expect(component.formClearAuthentication()).toBe(true);
    expect(component.testResult()).toBeNull();

    component.formOAuthClientSecret.set('secret');
    component.testResult.set({ pass: true, tool_count: 1, message: 'ok' });
    component.unsetOAuthClientSecret();
    expect(component.formClearOAuthClientSecret()).toBe(true);
    expect(component.testResult()).toBeNull();

    component.testResult.set({ pass: true, tool_count: 1, message: 'ok' });
    component.onFormValueChange();
    expect(component.testResult()).toBeNull();
  });

  it('saves create payload and reloads list on success', async () => {
    mcpServerService.createMCPServer.mockReturnValueOnce(of(makeServer()));
    component.formName.set(' Name ');
    component.formDescription.set(' Desc ');
    component.formServerURL.set(' https://new.example.com/mcp ');
    component.formAuthentication.set('token');
    component.formDefaultEnabled.set(true);
    component.formOAuthScopes.set('openid profile');

    await component.save();

    expect(mcpServerService.createMCPServer).toHaveBeenCalledWith({
      name: 'Name',
      description: 'Desc',
      server_url: 'https://new.example.com/mcp',
      auth_mode: 'header',
      authentication: 'token',
      default_enabled: true,
      oauth_auth_url: undefined,
      oauth_token_url: undefined,
      oauth_client_id: undefined,
      oauth_client_secret: undefined,
      oauth_scopes: ['openid', 'profile'],
      oauth_pkce_policy: 'supported'
    });
    expect(component.formName()).toBe('');
    expect(component.isSaving()).toBe(false);
  });

  it('saves update payload with explicit null token clears', async () => {
    component.editingServer.set(makeServer({ id: 'edit-id', auth_mode: 'oauth' }));
    mcpServerService.updateMCPServer.mockReturnValueOnce(of(makeServer()));
    component.formName.set('n');
    component.formDescription.set('d');
    component.formServerURL.set('https://s.example.com/mcp');
    component.formAuthMode.set('oauth');
    component.formClearAuthentication.set(true);
    component.formClearOAuthClientSecret.set(true);
    component.formOAuthScopes.set('a b');
    component.selectedRitualIds.set(['r1', 'r2']);

    await component.save();

    expect(mcpServerService.updateMCPServer).toHaveBeenCalledWith('edit-id', expect.objectContaining({
      authentication: null,
      oauth_client_secret: null,
      ritual_ids: ['r1', 'r2'],
      oauth_scopes: ['a', 'b'],
      oauth_pkce_policy: 'supported'
    }));
    expect(component.isSaving()).toBe(false);
  });

  it('alerts when save fails', async () => {
    mcpServerService.createMCPServer.mockReturnValueOnce(throwError(() => new Error('save failed')));
    component.formName.set('n');
    component.formDescription.set('d');
    component.formServerURL.set('https://s.example.com/mcp');

    await component.save();
    await Promise.resolve();

    expect(confirmationService.alert).toHaveBeenCalled();
    expect(component.isSaving()).toBe(false);
  });

  it('does nothing when save is invalid', async () => {
    component.formName.set('');
    await component.save();
    expect(mcpServerService.createMCPServer).not.toHaveBeenCalled();
  });

  it('deletes after confirmation and reloads', async () => {
    mcpServerService.deleteMCPServer.mockReturnValueOnce(of(void 0));
    await component.delete(makeServer({ id: 'deadbeef' }));
    expect(confirmationService.confirm).toHaveBeenCalled();
    expect(mcpServerService.deleteMCPServer).toHaveBeenCalledWith('deadbeef');
  });

  it('does not delete when confirmation is cancelled', async () => {
    confirmationService.confirm.mockResolvedValueOnce(false);
    await component.delete(makeServer());
    expect(mcpServerService.deleteMCPServer).not.toHaveBeenCalled();
  });

  it('alerts when delete fails', async () => {
    mcpServerService.deleteMCPServer.mockReturnValueOnce(throwError(() => new Error('delete failed')));
    await component.delete(makeServer({ id: 'deadbeef' }));
    await Promise.resolve();
    expect(confirmationService.alert).toHaveBeenCalled();
  });

  it('calculates pages and labels auth actions', () => {
    component.totalCount.set(0);
    component.pageSize.set(10);
    expect(component.getTotalPages()).toBe(1);
    component.totalCount.set(25);
    expect(component.getTotalPages()).toBe(3);

    expect(component.authActionLabel(makeServer({ auth_mode: 'header' }))).toBe('');
    expect(component.authActionLabel(makeServer({ auth_mode: 'oauth', oauth_has_access_token: false, oauth_has_refresh_token: false }))).toBe('Authenticate');
    expect(component.authActionLabel(makeServer({ auth_mode: 'oauth', oauth_has_access_token: true }))).toBe('Reauthenticate');
  });

  it('starts oauth only for oauth connectors', () => {
    const originalHref = window.location.href;
    mcpServerService.startOAuth.mockReturnValueOnce(of({ authorization_url: '' }));
    component.authenticate(makeServer({ auth_mode: 'header' }));
    expect(mcpServerService.startOAuth).not.toHaveBeenCalled();

    component.authenticate(makeServer({ auth_mode: 'oauth', id: 'oauth-id' }));
    expect(mcpServerService.startOAuth).toHaveBeenCalledWith('oauth-id', { redirect_after: originalHref });
  });

  it('alerts when oauth start fails', async () => {
    mcpServerService.startOAuth.mockReturnValueOnce(throwError(() => new Error('oauth failed')));
    component.authenticate(makeServer({ auth_mode: 'oauth' }));
    await Promise.resolve();
    expect(confirmationService.alert).toHaveBeenCalled();
  });
});
