import { HttpErrorResponse } from '@angular/common/http';
import { Component, provideZonelessChangeDetection, signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { of, throwError } from 'rxjs';

import { Model } from '../../../core/models/model.model';
import { ConnectionModel, ProviderConnection } from '../../../core/models/provider-connection.model';
import { ModelService } from '../../../core/services/model.service';
import { ProviderConnectionsService } from '../../../core/services/provider-connections.service';
import { UserPreferencesService } from '../../../core/services/user-preferences.service';
import { resetBodyScrollLockForTests } from '../../../shared/ui';
import { ChooserRow, ModelChooserComponent, choicesFrom, highlightParts, matchesWords } from './model-chooser.component';

const OPENROUTER: ProviderConnection = {
  id: 'c-or',
  preset: 'openrouter',
  name: 'OpenRouter',
  has_key: true,
  hint: 'a91f',
  builtin: false,
  created_at: '2026-10-30T12:00:00Z',
  updated_at: '2026-10-30T12:00:00Z',
};

const LISTED: ConnectionModel[] = [
  { id: 'anthropic/claude-sonnet-4.6', display_name: 'Claude Sonnet 4.6', tool_support: true, vision_support: true, enabled: true },
  { id: 'anthropic/claude-sonnet-5.5', display_name: 'Claude Sonnet 5.5', tool_support: true, vision_support: true, enabled: false },
  { id: 'meta-llama/llama-5-70b-instruct', display_name: 'Llama 5 70B Instruct', tool_support: true, vision_support: false, enabled: false },
  { id: 'mystery-model', enabled: false },
];

/** The account's model list: Sonnet 4.6 enabled on this connection, and the default. */
const MODELS: Model[] = [
  { id: 'row-sonnet', name: 'anthropic/claude-sonnet-4.6', display_name: 'Claude Sonnet 4.6', description: '', tool_support: true, connection_id: 'c-or' },
];

function apiError(status: number, code?: string): HttpErrorResponse {
  return new HttpErrorResponse({ status, error: code ? { code, message: 'server text' } : { message: 'server text' } });
}

@Component({
  standalone: true,
  imports: [ModelChooserComponent],
  template: `<app-model-chooser
    [open]="open()"
    [connection]="connection()"
    (saved)="saved.push($event)"
    (closed)="closes = closes + 1"
    (changeDefault)="changeDefaults = changeDefaults + 1"
  />`,
})
class HostComponent {
  open = signal(true);
  connection = signal<ProviderConnection | null>(OPENROUTER);
  saved: ConnectionModel[][] = [];
  closes = 0;
  changeDefaults = 0;
}

describe('ModelChooserComponent', () => {
  let connections: { models: ReturnType<typeof vi.fn>; setModels: ReturnType<typeof vi.fn> };
  let modelService: { getModels: ReturnType<typeof vi.fn> };
  let preferences: { getUserPreferences: ReturnType<typeof vi.fn> };
  let fixture: ComponentFixture<HostComponent>;
  let chooser: ModelChooserComponent;

  async function render(): Promise<void> {
    fixture = TestBed.createComponent(HostComponent);
    fixture.detectChanges();
    await fixture.whenStable();
    chooser = fixture.debugElement.children[0].componentInstance as ModelChooserComponent;
  }

  function row(id: string): ChooserRow {
    const found = chooser.rows().find(r => r.id === id);
    if (!found) throw new Error(`no row ${id}`);
    return found;
  }

  beforeEach(async () => {
    resetBodyScrollLockForTests();
    connections = { models: vi.fn(() => of(LISTED)), setModels: vi.fn(() => of([])) };
    modelService = { getModels: vi.fn(() => of(MODELS)) };
    preferences = { getUserPreferences: vi.fn(() => of({ default_model_id: 'row-sonnet' })) };
    await TestBed.configureTestingModule({
      imports: [HostComponent],
      providers: [
        provideZonelessChangeDetection(),
        { provide: ProviderConnectionsService, useValue: connections },
        { provide: ModelService, useValue: modelService },
        { provide: UserPreferencesService, useValue: preferences },
      ],
    }).compileComponents();
  });

  afterEach(() => resetBodyScrollLockForTests());

  it("lists the connection's models with what each takes, and marks the default", async () => {
    await render();
    expect(connections.models).toHaveBeenCalledWith('c-or');
    expect(chooser.rows().map(r => [r.name, r.enabled, r.tools, r.images])).toEqual([
      ['Claude Sonnet 4.6', true, true, true],
      ['Claude Sonnet 5.5', false, true, true],
      ['Llama 5 70B Instruct', false, true, false],
      ['mystery-model', false, undefined, undefined],
    ]);
    expect(chooser.defaultModelId()).toBe('anthropic/claude-sonnet-4.6');
    expect(chooser.enabledCount()).toBe(1);
  });

  describe('search', () => {
    it('matches every word, in any order, in the name or the id', async () => {
      await render();
      chooser.setQuery('son');
      expect(chooser.shown().map(r => r.name)).toEqual(['Claude Sonnet 4.6', 'Claude Sonnet 5.5']);
      chooser.setQuery('4.6 SON');
      expect(chooser.shown().map(r => r.name)).toEqual(['Claude Sonnet 4.6']);
      chooser.setQuery('meta 70');
      expect(chooser.shown().map(r => r.name)).toEqual(['Llama 5 70B Instruct']);
    });

    it('highlights matches without splitting the name it shows', async () => {
      await render();
      chooser.setQuery('son');
      fixture.detectChanges();
      const names = [...fixture.nativeElement.ownerDocument.querySelectorAll('.chooser__name')].map(
        (el: Element) => el.textContent?.replace(/\s+/g, ' ').trim(),
      );
      expect(names).toEqual(['Claude Sonnet 4.6, Default', 'Claude Sonnet 5.5']);
      const marks = [...fixture.nativeElement.ownerDocument.querySelectorAll('.chooser__name mark')].map((el: Element) => el.textContent);
      expect(marks).toEqual(['Son', 'Son']);
    });

    it('combines with the filters, and says which filter emptied the list', async () => {
      await render();
      chooser.setFilter('images');
      chooser.setQuery('llama');
      expect(chooser.shown()).toEqual([]);
      expect(chooser.emptyText()).toBe('No models that take images match “llama”.');

      chooser.showAll();
      expect(chooser.query()).toBe('');
      expect(chooser.filter()).toBe('all');
      expect(chooser.shown().length).toBe(4);
    });

    it('filters to the enabled models', async () => {
      await render();
      chooser.setFilter('enabled');
      expect(chooser.shown().map(r => r.name)).toEqual(['Claude Sonnet 4.6']);
    });

    it('clears on Escape before anything else closes', async () => {
      await render();
      chooser.setQuery('son');
      const event = new KeyboardEvent('keydown', { key: 'Escape', cancelable: true });
      const stop = vi.spyOn(event, 'stopPropagation');
      chooser.onSearchKeydown(event);
      expect(chooser.query()).toBe('');
      expect(stop).toHaveBeenCalled();
    });

    it('typing never asks the server', async () => {
      await render();
      chooser.setQuery('claude');
      chooser.setQuery('claude son');
      expect(connections.models).toHaveBeenCalledTimes(1);
    });
  });

  describe('saving', () => {
    it('sends only what changed, with capabilities where they are known', async () => {
      await render();
      chooser.toggle(row('meta-llama/llama-5-70b-instruct'), true);
      chooser.toggle(row('mystery-model'), true);
      chooser.toggleCapability(row('mystery-model'), 'tools');
      await chooser.save();

      expect(connections.setModels).toHaveBeenCalledWith('c-or', [
        { id: 'meta-llama/llama-5-70b-instruct', enabled: true, display_name: 'Llama 5 70B Instruct', tool_support: true, vision_support: false },
        { id: 'mystery-model', enabled: true, display_name: 'mystery-model', tool_support: true },
      ]);
      expect(fixture.componentInstance.saved.length).toBe(1);
    });

    it('refreshes the model picker and the default after saving', async () => {
      await render();
      modelService.getModels.mockClear();
      preferences.getUserPreferences.mockClear();
      chooser.toggle(row('anthropic/claude-sonnet-5.5'), true);
      await chooser.save();
      expect(modelService.getModels).toHaveBeenCalledTimes(1);
      expect(preferences.getUserPreferences).toHaveBeenCalledTimes(1);
    });

    it('closes without a request when nothing changed', async () => {
      await render();
      await chooser.save();
      expect(connections.setModels).not.toHaveBeenCalled();
      expect(fixture.componentInstance.closes).toBe(1);
    });

    it("says what went wrong when saving fails, and keeps the account's choices", async () => {
      connections.setModels.mockReturnValue(throwError(() => apiError(500)));
      await render();
      chooser.toggle(row('anthropic/claude-sonnet-5.5'), true);
      await chooser.save();
      expect(chooser.saveError()).not.toBe('');
      expect(row('anthropic/claude-sonnet-5.5').enabled).toBe(true);
      expect(fixture.componentInstance.saved).toEqual([]);
    });
  });

  describe('renaming', () => {
    function renameButton(name: string): HTMLButtonElement | null {
      return fixture.nativeElement.ownerDocument.querySelector(`button[aria-label="Rename ${name}"]`);
    }

    function field(): HTMLInputElement {
      return fixture.nativeElement.ownerDocument.querySelector('.chooser__rename') as HTMLInputElement;
    }

    it('renames an enabled model in place, and saves the new name', async () => {
      await render();
      fixture.detectChanges();
      expect(renameButton('Claude Sonnet 5.5')).toBeNull();
      renameButton('Claude Sonnet 4.6')!.click();
      fixture.detectChanges();
      expect(document.activeElement).toBe(field());
      field().value = '  Sonnet  ';
      field().dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }));
      fixture.detectChanges();

      expect(field()).toBeNull();
      expect(row('anthropic/claude-sonnet-4.6').name).toBe('Sonnet');
      expect(fixture.nativeElement.ownerDocument.body.textContent).toContain('anthropic/claude-sonnet-4.6');
      await chooser.save();
      expect(connections.setModels).toHaveBeenCalledWith('c-or', [
        { id: 'anthropic/claude-sonnet-4.6', enabled: true, display_name: 'Sonnet', tool_support: true, vision_support: true },
      ]);
    });

    it('keeps the name on Escape, without closing the dialog', async () => {
      await render();
      fixture.detectChanges();
      renameButton('Claude Sonnet 4.6')!.click();
      fixture.detectChanges();
      field().value = 'Something else';
      const escape = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true });
      field().dispatchEvent(escape);
      fixture.detectChanges();

      expect(escape.defaultPrevented).toBe(true);
      expect(field()).toBeNull();
      expect(row('anthropic/claude-sonnet-4.6').name).toBe('Claude Sonnet 4.6');
      expect(fixture.componentInstance.closes).toBe(0);
      expect(chooser.changes()).toEqual([]);
    });

    it('goes back to the id when the name is cleared', async () => {
      await render();
      chooser.startRename(row('anthropic/claude-sonnet-4.6'));
      chooser.commitRename(row('anthropic/claude-sonnet-4.6'), '   ');
      expect(row('anthropic/claude-sonnet-4.6').name).toBe('anthropic/claude-sonnet-4.6');
    });
  });

  describe('the default model', () => {
    it("can't be unticked, and says how to change it", async () => {
      await render();
      const box = document.createElement('input');
      box.type = 'checkbox';
      box.checked = false; // as the click left it
      chooser.toggle(row('anthropic/claude-sonnet-4.6'), false, box);
      expect(box.checked).toBe(true);
      expect(row('anthropic/claude-sonnet-4.6').enabled).toBe(true);
      expect(chooser.defaultBlocked()).toBe('Claude Sonnet 4.6');
      fixture.detectChanges();
      const message = fixture.nativeElement.ownerDocument.querySelector('[data-testid="model-chooser-default"]') as HTMLElement;
      expect(message.textContent).toContain('Claude Sonnet 4.6 is your default model');
      (message.querySelector('button') as HTMLButtonElement).click();
      expect(fixture.componentInstance.changeDefaults).toBe(1);
    });

    it("explains the server's refusal the same way", async () => {
      connections.setModels.mockReturnValue(throwError(() => apiError(409, 'default_model_in_use')));
      await render();
      chooser.toggle(row('anthropic/claude-sonnet-5.5'), true);
      await chooser.save();
      expect(chooser.defaultBlocked()).toBe('Claude Sonnet 4.6');
      expect(chooser.saveError()).toBe('');
    });
  });

  it('asks about capabilities nothing reported, once a model is enabled', async () => {
    await render();
    expect(chooser.unconfirmed()).toBe(false);
    chooser.toggle(row('mystery-model'), true);
    expect(chooser.unconfirmed()).toBe(true);
    chooser.toggleCapability(row('mystery-model'), 'tools');
    chooser.toggleCapability(row('mystery-model'), 'images');
    expect(chooser.unconfirmed()).toBe(false);
  });

  it("says when the provider can't be listed, and retries", async () => {
    connections.models.mockReturnValueOnce(throwError(() => apiError(422, 'provider_unreachable')));
    await render();
    expect(chooser.loadError()).not.toBe('');
    await chooser.load(OPENROUTER);
    expect(chooser.loadError()).toBe('');
    expect(chooser.rows().length).toBe(4);
  });
});

describe('model chooser helpers', () => {
  it('matches words case-insensitively against name and id', () => {
    expect(matchesWords({ id: 'qwen/qwen3.7-plus', name: 'Qwen 3.7 Plus' }, ['qwen', 'plus'])).toBe(true);
    expect(matchesWords({ id: 'qwen/qwen3.7-plus', name: 'Qwen 3.7 Plus' }, ['qwen', 'max'])).toBe(false);
  });

  it('highlights matches as text parts, never as HTML', () => {
    expect(highlightParts('Claude <Sonnet>', ['son'])).toEqual([
      { text: 'Claude <', match: false },
      { text: 'Son', match: true },
      { text: 'net>', match: false },
    ]);
    expect(highlightParts('a.b', ['.'])).toEqual([
      { text: 'a', match: false },
      { text: '.', match: true },
      { text: 'b', match: false },
    ]);
  });

  it("sends an enabled model's new name, and nothing for one renamed back", () => {
    const base = { id: 'm', enabled: true, tools: true, images: false };
    const initial = { name: 'Old', enabled: true, tools: true, images: false };
    expect(choicesFrom([{ ...base, name: 'New', initial }])).toEqual([
      { id: 'm', enabled: true, display_name: 'New', tool_support: true, vision_support: false },
    ]);
    expect(choicesFrom([{ ...base, name: 'Old', initial }])).toEqual([]);
  });

  it('turning a model off sends no capabilities', () => {
    const rows: ChooserRow[] = [
      { id: 'm', name: 'M', enabled: false, tools: true, images: false, initial: { name: 'M', enabled: true, tools: true, images: false } },
    ];
    expect(choicesFrom(rows)).toEqual([{ id: 'm', enabled: false }]);
  });
});
