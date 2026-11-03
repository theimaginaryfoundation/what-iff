import { HttpErrorResponse } from '@angular/common/http';
import { Component, provideZonelessChangeDetection, signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { of, throwError } from 'rxjs';

import { ProviderConnected, ProviderConnection, ProviderPreset } from '../../../core/models/provider-connection.model';
import { ProviderConnectionsService } from '../../../core/services/provider-connections.service';
import { resetBodyScrollLockForTests } from '../../../shared/ui';
import { ProviderFormComponent } from './provider-form.component';

const PRESETS: ProviderPreset[] = [
  { id: 'openai', name: 'OpenAI', flavor: 'openai', base_url: 'https://api.openai.com/v1', address_editable: false, key_required: true, builtin: true, local: false },
  { id: 'openrouter', name: 'OpenRouter', flavor: 'openai', base_url: 'https://openrouter.ai/api/v1', address_editable: false, key_required: true, builtin: false, local: false, keys_url: 'https://openrouter.ai/keys' },
  { id: 'ollama', name: 'Ollama', flavor: 'openai', base_url: 'http://localhost:11434/v1', address_editable: true, key_required: false, builtin: false, local: true },
  { id: 'custom', name: 'Custom', address_editable: true, key_required: false, builtin: false, local: false },
];

const OLLAMA: ProviderConnection = {
  id: 'c-ollama',
  preset: 'ollama',
  name: 'Ollama',
  base_url: 'http://localhost:11434/v1',
  has_key: false,
  builtin: false,
  created_at: '2026-10-29T12:00:00Z',
  updated_at: '2026-10-29T12:00:00Z',
};

function connected(c: Partial<ProviderConnection>, models = 2): ProviderConnected {
  return { connection: { ...OLLAMA, ...c }, models: Array.from({ length: models }, (_, i) => ({ id: `m${i}` })) };
}

function apiError(status: number, code?: string): HttpErrorResponse {
  return new HttpErrorResponse({ status, error: code ? { code, message: 'server text' } : { message: 'server text' } });
}

@Component({
  standalone: true,
  imports: [ProviderFormComponent],
  template: `<app-provider-form
    [open]="open()"
    [presets]="presets"
    [connection]="connection()"
    [initialPreset]="initial()"
    (saved)="saved.push($event)"
    (closed)="closes = closes + 1"
  />`,
})
class HostComponent {
  presets = PRESETS;
  open = signal(true);
  connection = signal<ProviderConnection | null>(null);
  initial = signal<string | null>(null);
  saved: ProviderConnected[] = [];
  closes = 0;
}

describe('ProviderFormComponent', () => {
  let service: { create: ReturnType<typeof vi.fn>; update: ReturnType<typeof vi.fn> };
  let fixture: ComponentFixture<HostComponent>;
  let form: ProviderFormComponent;

  async function render(setup?: (host: HostComponent) => void): Promise<void> {
    fixture = TestBed.createComponent(HostComponent);
    setup?.(fixture.componentInstance);
    fixture.detectChanges();
    await fixture.whenStable();
    form = fixture.debugElement.children[0].componentInstance as ProviderFormComponent;
  }

  function fill(fields: { preset?: string; name?: string; flavor?: 'openai' | 'anthropic'; address?: string; key?: string; password?: string }): void {
    if (fields.preset) form.pickPreset(fields.preset);
    if (fields.name !== undefined) form.name.set(fields.name);
    if (fields.flavor) form.flavor.set(fields.flavor);
    if (fields.address !== undefined) form.address.set(fields.address);
    if (fields.key !== undefined) form.key.set(fields.key);
    if (fields.password !== undefined) form.password.set(fields.password);
  }

  beforeEach(async () => {
    resetBodyScrollLockForTests();
    service = { create: vi.fn(), update: vi.fn() };
    await TestBed.configureTestingModule({
      imports: [HostComponent],
      providers: [provideZonelessChangeDetection(), { provide: ProviderConnectionsService, useValue: service }],
    }).compileComponents();
  });

  afterEach(() => resetBodyScrollLockForTests());

  it('groups presets into cloud, this computer and anything else', async () => {
    await render();
    expect(form.groups().map(g => [g.label, g.presets.map(p => p.id)])).toEqual([
      ['Cloud', ['openai', 'openrouter']],
      ['On this computer', ['ollama']],
      ['Anything else', ['custom']],
    ]);
  });

  it("links to where to create a key, as the preset says", async () => {
    await render();
    form.pickPreset('openrouter');
    expect(form.keyUrl()).toBe('https://openrouter.ai/keys');
    form.pickPreset('ollama');
    expect(form.keyUrl()).toBeNull();
  });

  it('starts on the provider it was opened for', async () => {
    await render(h => h.initial.set('openai'));
    expect(form.presetId()).toBe('openai');
    expect(form.address()).toBe('https://api.openai.com/v1');
  });

  it('connects a cloud preset with its key and password, then shows what was found', async () => {
    service.create.mockReturnValue(of(connected({ preset: 'openai', name: 'OpenAI' }, 38)));
    await render();
    fill({ preset: 'openai', key: '  sk-test-1234  ', password: 'pw' });

    await form.submit();

    expect(service.create).toHaveBeenCalledWith({
      preset: 'openai',
      name: undefined,
      flavor: undefined,
      base_url: undefined,
      key: 'sk-test-1234',
      current_password: 'pw',
    });
    fixture.detectChanges();
    expect(fixture.nativeElement.querySelector('[data-testid="provider-form-result"]')?.textContent).toContain('Found 38 models');
    expect(fixture.componentInstance.saved).toHaveLength(1);
  });

  it('connects a local server at the address given, without a key', async () => {
    service.create.mockReturnValue(of(connected({})));
    await render();
    fill({ preset: 'ollama', address: 'http://localhost:11500/v1', password: 'pw' });

    await form.submit();

    expect(service.create.mock.calls[0][0]).toMatchObject({ preset: 'ollama', base_url: 'http://localhost:11500/v1', key: undefined });
  });

  it('asks Custom for a name and what it speaks before sending anything', async () => {
    await render();
    fill({ preset: 'custom', address: 'http://localhost:8000/v1', password: 'pw' });

    await form.submit();
    expect(form.error()?.field).toBe('name');

    fill({ name: 'Home GPU box' });
    await form.submit();
    expect(form.error()?.field).toBe('flavor');
    expect(service.create).not.toHaveBeenCalled();

    service.create.mockReturnValue(of(connected({ preset: 'custom', name: 'Home GPU box' })));
    fill({ flavor: 'anthropic' });
    await form.submit();
    expect(service.create.mock.calls[0][0]).toMatchObject({ preset: 'custom', name: 'Home GPU box', flavor: 'anthropic' });
  });

  it('needs the key and password before sending anything', async () => {
    await render();
    fill({ preset: 'openai' });
    await form.submit();
    expect(form.error()?.field).toBe('key');

    fill({ key: 'sk-test-1234' });
    await form.submit();
    expect(form.error()?.field).toBe('password');
    expect(service.create).not.toHaveBeenCalled();
  });

  describe('when connecting fails', () => {
    async function failWith(err: HttpErrorResponse, preset = 'ollama'): Promise<void> {
      service.create.mockReturnValue(throwError(() => err));
      await render();
      fill({ preset, address: preset === 'ollama' ? 'http://localhost:11434/v1' : undefined, key: preset === 'ollama' ? '' : 'sk-test-1234', password: 'pw' });
      await form.submit();
    }

    it('keeps everything typed', async () => {
      await failWith(apiError(422, 'provider_key_rejected'), 'openai');
      expect(form.key()).toBe('sk-test-1234');
      expect(form.password()).toBe('pw');
      expect(form.result()).toBeNull();
    });

    it('points at the key when the provider rejects it', async () => {
      await failWith(apiError(422, 'provider_key_rejected'), 'openai');
      expect(form.error()).toEqual({ message: "OpenAI didn't accept that key.", field: 'key' });
    });

    it('points at the address, with what to check, when nothing answers on this computer', async () => {
      await failWith(apiError(422, 'provider_unreachable'));
      expect(form.error()?.field).toBe('address');
      expect(form.error()?.message).toContain('OLLAMA_HOST=0.0.0.0');
    });

    it('says a cloud provider could not be reached without blaming an address', async () => {
      await failWith(apiError(422, 'provider_unreachable'), 'openai');
      expect(form.error()?.field).toBeUndefined();
      expect(form.error()?.message).toContain("Couldn't reach OpenAI");
    });

    it('points at the address when something else answered there', async () => {
      await failWith(apiError(422, 'provider_incompatible'));
      expect(form.error()?.field).toBe('address');
    });

    it('suggests trying again when the provider is failing', async () => {
      await failWith(apiError(422, 'provider_unavailable'), 'openai');
      expect(form.error()?.message).toContain('Try again in a moment');
    });

    it('clears only the password when it was wrong', async () => {
      await failWith(apiError(403, 'reauthentication_required'), 'openai');
      expect(form.error()?.field).toBe('password');
      expect(form.password()).toBe('');
      expect(form.key()).toBe('sk-test-1234');
    });

    it('says to edit the existing connection on a conflict', async () => {
      await failWith(apiError(409, 'provider_connection_exists'), 'openai');
      expect(form.error()?.message).toContain('Edit that one instead');
    });
  });

  describe('editing', () => {
    beforeEach(async () => {
      await render(h => h.connection.set(OLLAMA));
    });

    it('starts from the connection, with the provider fixed', () => {
      expect(form.title()).toBe('Edit Ollama');
      expect(form.presetId()).toBe('ollama');
      expect(form.address()).toBe('http://localhost:11434/v1');
      expect(fixture.nativeElement.querySelector('select[name="preset"]')).toBeNull();
    });

    it('renames without a password', async () => {
      service.update.mockReturnValue(of(connected({ name: 'Desk Ollama' }, 0)));
      fill({ name: 'Desk Ollama' });
      expect(form.needsPassword()).toBe(false);

      await form.submit();

      expect(service.update).toHaveBeenCalledWith('c-ollama', { name: 'Desk Ollama' });
      expect(fixture.componentInstance.saved).toHaveLength(1);
      expect(fixture.componentInstance.closes).toBe(1);
      expect(form.result()).toBeNull();
    });

    it('needs the password for a new address, and sends only what changed', async () => {
      service.update.mockReturnValue(of(connected({ base_url: 'http://localhost:11500/v1' })));
      fill({ address: 'http://localhost:11500/v1' });
      expect(form.needsPassword()).toBe(true);
      await form.submit();
      expect(form.error()?.field).toBe('password');

      fill({ password: 'pw' });
      await form.submit();
      expect(service.update).toHaveBeenCalledWith('c-ollama', { base_url: 'http://localhost:11500/v1', current_password: 'pw' });
    });

    it('closes without a request when nothing changed', async () => {
      await form.submit();
      expect(service.update).not.toHaveBeenCalled();
      expect(fixture.componentInstance.closes).toBe(1);
    });
  });
});
