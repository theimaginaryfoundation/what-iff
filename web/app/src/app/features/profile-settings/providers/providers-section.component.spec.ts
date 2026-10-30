import { HttpErrorResponse } from '@angular/common/http';
import { provideZonelessChangeDetection, signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { of, throwError } from 'rxjs';

import { Model } from '../../../core/models/model.model';
import { ProviderConnection, ProviderPreset } from '../../../core/models/provider-connection.model';
import { AuthService } from '../../../core/services/auth.service';
import { ConfirmationService } from '../../../core/services/confirmation.service';
import { ModelService } from '../../../core/services/model.service';
import { PersonalityService } from '../../../core/services/personality.service';
import { ProviderConnectionsService } from '../../../core/services/provider-connections.service';
import { ProviderKeysService } from '../../../core/services/provider-keys.service';
import { UserPreferencesService } from '../../../core/services/user-preferences.service';
import { NIL_UUID } from '../../../core/utils/default-model';
import { ProfileSettingsModalService } from '../profile-settings-modal.service';
import { ProvidersSectionComponent } from './providers-section.component';

function connection(overrides: Partial<ProviderConnection>): ProviderConnection {
  return {
    id: 'c-openai',
    preset: 'openai',
    name: 'OpenAI',
    hint: 'abcd',
    has_key: true,
    builtin: true,
    created_at: '2026-10-29T12:00:00Z',
    updated_at: '2026-10-29T12:00:00Z',
    ...overrides,
  };
}

const OPENAI = connection({});
const OLLAMA = connection({ id: 'c-ollama', preset: 'ollama', name: 'Ollama', hint: undefined, has_key: false, builtin: false, base_url: 'http://host.docker.internal:11434/v1' });

const PRESETS: ProviderPreset[] = [
  { id: 'openai', name: 'OpenAI', flavor: 'openai', base_url: 'https://api.openai.com/v1', address_editable: false, key_required: true, builtin: true, local: false },
  { id: 'ollama', name: 'Ollama', flavor: 'openai', base_url: 'http://localhost:11434/v1', address_editable: true, key_required: false, builtin: false, local: true },
  { id: 'custom', name: 'Custom', address_editable: true, key_required: false, builtin: false, local: false },
];

describe('ProvidersSectionComponent', () => {
  let connections: {
    list: ReturnType<typeof vi.fn>;
    presets: ReturnType<typeof vi.fn>;
    remove: ReturnType<typeof vi.fn>;
  };
  let keys: { refreshAccess: ReturnType<typeof vi.fn> };
  let personalities: { listPersonalities: ReturnType<typeof vi.fn> };
  let confirmation: { confirm: ReturnType<typeof vi.fn> };
  let router: { navigate: ReturnType<typeof vi.fn> };
  let modal: ProfileSettingsModalService;
  let modelService: { getModels: ReturnType<typeof vi.fn> };
  let preferences: { getUserPreferences: ReturnType<typeof vi.fn> };
  let currentUser: ReturnType<typeof signal<{ id: string } | null>>;

  async function render(): Promise<ComponentFixture<ProvidersSectionComponent>> {
    const fixture = TestBed.createComponent(ProvidersSectionComponent);
    fixture.detectChanges();
    await vi.waitFor(() => expect(fixture.componentInstance.loading()).toBe(false));
    await fixture.whenStable();
    fixture.detectChanges();
    return fixture;
  }

  function row(fixture: ComponentFixture<ProvidersSectionComponent>, preset: string): HTMLElement {
    return fixture.nativeElement.querySelector(`[data-preset="${preset}"]`);
  }

  function buttonIn(el: HTMLElement, label: string): HTMLButtonElement {
    const button = [...el.querySelectorAll('button')].find(b => b.textContent?.trim() === label);
    if (!button) throw new Error(`no ${label} button`);
    return button;
  }

  function byTestId(fixture: ComponentFixture<ProvidersSectionComponent>, id: string): HTMLElement | null {
    return fixture.nativeElement.querySelector(`[data-testid="${id}"]`);
  }

  beforeEach(async () => {
    localStorage.clear();
    connections = {
      list: vi.fn().mockReturnValue(of([])),
      presets: vi.fn().mockReturnValue(of(PRESETS)),
      remove: vi.fn().mockReturnValue(of(undefined)),
    };
    keys = { refreshAccess: vi.fn().mockReturnValue(of(null)) };
    personalities = { listPersonalities: vi.fn().mockReturnValue(of({ results: [{ id: 'p1' }], total_count: 1, page: 1 })) };
    confirmation = { confirm: vi.fn().mockResolvedValue(true) };
    router = { navigate: vi.fn() };
    currentUser = signal<{ id: string } | null>({ id: 'user-1' });
    modelService = { getModels: vi.fn().mockReturnValue(of([])) };
    preferences = { getUserPreferences: vi.fn().mockReturnValue(of({ default_model_id: 'm-default' })) };

    await TestBed.configureTestingModule({
      imports: [ProvidersSectionComponent],
      providers: [
        provideZonelessChangeDetection(),
        ProfileSettingsModalService,
        { provide: ProviderConnectionsService, useValue: connections },
        { provide: ProviderKeysService, useValue: keys },
        { provide: AuthService, useValue: { currentUser } },
        { provide: PersonalityService, useValue: personalities },
        { provide: ConfirmationService, useValue: confirmation },
        { provide: Router, useValue: router },
        { provide: ModelService, useValue: modelService },
        { provide: UserPreferencesService, useValue: preferences },
      ],
    }).compileComponents();
    modal = TestBed.inject(ProfileSettingsModalService);
  });

  afterEach(() => localStorage.clear());

  it('lists connections with their key hint, and marks OpenAI required and local servers', async () => {
    connections.list.mockReturnValue(of([OPENAI, OLLAMA]));
    const fixture = await render();

    const openai = row(fixture, 'openai');
    expect(openai.textContent).toContain('OpenAI');
    expect(openai.textContent).toContain('••••abcd');
    expect(openai.querySelector('.providers__badge')?.textContent?.trim()).toBe('Required');
    expect(openai.textContent).toContain('Memory, summaries and images');

    const ollama = row(fixture, 'ollama');
    expect(ollama.textContent).toContain('This computer');
    expect(ollama.textContent).toContain('host.docker.internal:11434');
    expect(ollama.textContent).toContain('No key');
  });

  it('asks for OpenAI while there is no OpenAI connection, and opens the form on it', async () => {
    connections.list.mockReturnValue(of([OLLAMA]));
    const fixture = await render();
    const callout = byTestId(fixture, 'providers-needs-openai')!;
    expect(callout).not.toBeNull();

    callout.querySelector('button')!.click();
    expect(fixture.componentInstance.formOpen()).toBe(true);
    expect(fixture.componentInstance.formPreset()).toBe('openai');
    expect(fixture.componentInstance.formConnection()).toBeNull();
  });

  it('does not ask for OpenAI once it is connected', async () => {
    connections.list.mockReturnValue(of([OPENAI]));
    const fixture = await render();
    expect(byTestId(fixture, 'providers-needs-openai')).toBeNull();
  });

  it('opens the form to add any provider, or to edit a connection', async () => {
    connections.list.mockReturnValue(of([OPENAI]));
    const fixture = await render();

    byTestId(fixture, 'providers-add')!.click();
    expect(fixture.componentInstance.formOpen()).toBe(true);
    expect(fixture.componentInstance.formPreset()).toBeNull();

    fixture.componentInstance.closeForm();
    buttonIn(row(fixture, 'openai'), 'Edit').click();
    expect(fixture.componentInstance.formConnection()).toEqual(OPENAI);
  });

  describe('models (ADR 0x027)', () => {
    const model = (id: string, connectionId: string): Model => ({ id, name: id, display_name: id, description: '', tool_support: true, connection_id: connectionId });

    it("counts each connection's models, and opens a connection's chooser", async () => {
      connections.list.mockReturnValue(of([OPENAI, OLLAMA]));
      modelService.getModels.mockReturnValue(of([model('a', 'c-openai'), model('b', 'c-openai'), model('catalog', '')]));
      const fixture = await render();

      expect(row(fixture, 'openai').textContent).toContain('2 models');
      // The account has a default model, so a connection without models is not urged.
      expect(row(fixture, 'ollama').textContent).toContain('no models');
      expect(row(fixture, 'ollama').textContent).not.toContain('no models yet');

      buttonIn(row(fixture, 'ollama'), 'Models').click();
      expect(fixture.componentInstance.chooserConnection()).toEqual(OLLAMA);
    });

    it('urges choosing models only while the account has none to chat with', async () => {
      connections.list.mockReturnValue(of([OPENAI]));
      preferences.getUserPreferences.mockReturnValue(of({ default_model_id: NIL_UUID }));
      const fixture = await render();
      expect(row(fixture, 'openai').textContent).toContain('no models yet');
      expect(buttonIn(row(fixture, 'openai'), 'Choose models')).toBeTruthy();
    });

    it('points onboarding at choosing models while the account has no default model', async () => {
      connections.list.mockReturnValue(of([OLLAMA, OPENAI]));
      preferences.getUserPreferences.mockReturnValue(of({ default_model_id: NIL_UUID }));
      const fixture = await render();

      const callout = byTestId(fixture, 'providers-needs-models')!;
      expect(callout.textContent).toContain('Choose OpenAI models');
      expect(byTestId(fixture, 'providers-next')).toBeNull();
      (callout.querySelector('button') as HTMLButtonElement).click();
      expect(fixture.componentInstance.chooserConnection()).toEqual(OPENAI);
    });

    it('does not point at models once the account has a default', async () => {
      connections.list.mockReturnValue(of([OPENAI]));
      const fixture = await render();
      expect(byTestId(fixture, 'providers-needs-models')).toBeNull();
    });

    it('opens the chooser another part of the app asked for, once', async () => {
      connections.list.mockReturnValue(of([OLLAMA, OPENAI]));
      modal.openModelChooser('openai');
      const fixture = await render();
      expect(fixture.componentInstance.chooserConnection()).toEqual(OPENAI);
      expect(modal.modelChooserRequest()).toBeNull();
    });

    it("goes from connecting straight to choosing the connection's models", async () => {
      connections.list.mockReturnValue(of([OLLAMA]));
      const fixture = await render();
      fixture.componentInstance.add();
      fixture.componentInstance.chooseModels(OLLAMA);
      expect(fixture.componentInstance.formOpen()).toBe(false);
      expect(fixture.componentInstance.chooserConnection()).toEqual(OLLAMA);
    });

    it('says how many models are enabled after saving, and recounts', async () => {
      connections.list.mockReturnValue(of([OLLAMA]));
      const fixture = await render();
      fixture.componentInstance.chooseModels(OLLAMA);
      modelService.getModels.mockClear();
      await fixture.componentInstance.onModelsSaved([{ id: 'qwen3:4b', enabled: true }]);
      expect(fixture.componentInstance.chooserConnection()).toBeNull();
      expect(fixture.componentInstance.notice()).toBe('Ollama: 1 model enabled.');
      expect(modelService.getModels).toHaveBeenCalled();
    });

    it('sends the account to the Profile tab to change its default', async () => {
      connections.list.mockReturnValue(of([OPENAI]));
      const fixture = await render();
      modal.open('providers');
      fixture.componentInstance.chooseModels(OPENAI);
      fixture.componentInstance.changeDefault();
      expect(fixture.componentInstance.chooserConnection()).toBeNull();
      expect(modal.activeTab()).toBe('profile');
    });
  });

  it('reloads and refreshes access after a connection is saved', async () => {
    const fixture = await render();
    connections.list.mockReturnValue(of([OPENAI]));

    await fixture.componentInstance.onSaved({ connection: OPENAI, models: [] });

    expect(connections.list).toHaveBeenCalledTimes(2);
    expect(keys.refreshAccess).toHaveBeenCalled();
    expect(fixture.componentInstance.notice()).toBe('OpenAI connected.');
  });

  it('removes a connection after confirming, warning that OpenAI is required', async () => {
    connections.list.mockReturnValue(of([OPENAI]));
    const fixture = await render();
    const [openaiRow] = fixture.componentInstance.rows();

    await fixture.componentInstance.remove(openaiRow);

    expect(confirmation.confirm.mock.calls[0][0].message).toContain('needs an OpenAI key');
    expect(connections.remove).toHaveBeenCalledWith('c-openai');
    expect(keys.refreshAccess).toHaveBeenCalled();
  });

  it('keeps a connection when removal is not confirmed', async () => {
    confirmation.confirm.mockResolvedValue(false);
    connections.list.mockReturnValue(of([OLLAMA]));
    const fixture = await render();

    await fixture.componentInstance.remove(fixture.componentInstance.rows()[0]);

    expect(connections.remove).not.toHaveBeenCalled();
  });

  it('says so when the list cannot load, with a retry', async () => {
    connections.list.mockReturnValue(throwError(() => new HttpErrorResponse({ status: 0 })));
    const fixture = await render();
    expect(fixture.nativeElement.querySelector('[role="alert"]')?.textContent).toContain('Could not load your providers.');
    expect(byTestId(fixture, 'providers-needs-openai')).toBeNull();
  });

  describe('the converted-keys notice', () => {
    beforeEach(() => {
      connections.list.mockReturnValue(of([connection({ source: 'converted' })]));
    });

    it('shows once to an account whose keys were converted, until dismissed', async () => {
      const fixture = await render();
      const notice = byTestId(fixture, 'providers-converted')!;
      expect(notice.textContent).toContain('Your saved API keys are now connections.');

      notice.querySelector('button')!.click();
      fixture.detectChanges();
      expect(byTestId(fixture, 'providers-converted')).toBeNull();

      const again = await render();
      expect(byTestId(again, 'providers-converted')).toBeNull();
    });

    it('hides when dismissed before the account is known, without remembering it', async () => {
      currentUser.set(null);
      const fixture = await render();
      byTestId(fixture, 'providers-converted')!.querySelector('button')!.click();
      fixture.detectChanges();

      expect(byTestId(fixture, 'providers-converted')).toBeNull();
      expect(localStorage.length).toBe(0);
    });

    it('is not shown to an account without converted keys', async () => {
      connections.list.mockReturnValue(of([OPENAI]));
      const fixture = await render();
      expect(byTestId(fixture, 'providers-converted')).toBeNull();
    });
  });

  describe('during onboarding', () => {
    beforeEach(() => {
      personalities.listPersonalities.mockReturnValue(of({ results: [], total_count: 0, page: 1 }));
    });

    it('points on to creating a personality once OpenAI is connected', async () => {
      connections.list.mockReturnValue(of([OPENAI]));
      const fixture = await render();
      const next = byTestId(fixture, 'providers-next')!;
      expect(next).not.toBeNull();

      modal.open('providers');
      next.querySelector('button')!.click();

      expect(modal.openState()).toBe(false);
      expect(router.navigate).toHaveBeenCalledWith(['/personality']);
    });

    it('does not point on before OpenAI is connected', async () => {
      const fixture = await render();
      expect(byTestId(fixture, 'providers-next')).toBeNull();
    });
  });

  it('does not point on for an account that already has a personality', async () => {
    connections.list.mockReturnValue(of([OPENAI]));
    const fixture = await render();
    expect(byTestId(fixture, 'providers-next')).toBeNull();
  });
});
