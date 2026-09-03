import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection, signal } from '@angular/core';

import { ModelPickerComponent } from './model-picker.component';
import { Model } from '../../../../core/models/model.model';
import { ModelFavoritesService } from '../../services/model-favorites.service';

/**
 * Signal-backed stand-in for ModelFavoritesService. The real service reads and writes
 * user preferences over HTTP, which a picker test has no business exercising; the picker
 * only cares that favoriteIds() is reactive and that toggling flips membership.
 */
class FakeModelFavoritesService {
    private readonly ids = signal<ReadonlySet<string>>(new Set<string>());
    readonly error = signal<string | null>(null);

    favoriteIds(): ReadonlySet<string> {
        return this.ids();
    }

    isFavorite(modelId: string): boolean {
        return this.ids().has(modelId);
    }

    toggle(modelId: string): void {
        const next = new Set(this.ids());
        if (!next.delete(modelId)) {
            next.add(modelId);
        }
        this.ids.set(next);
    }

    clearCache(): void {
        this.ids.set(new Set<string>());
    }
}

describe('ModelPickerComponent', () => {
    let fixture: ComponentFixture<ModelPickerComponent>;
    let favorites: FakeModelFavoritesService;
    const models: Model[] = [
        { id: 'm3', name: 'claude-sonnet-4-6', display_name: 'Claude Sonnet 4.6', description: 'Claude', provider: 'anthropic', tool_support: true, subscription_tier: 'high' },
        { id: 'm2', name: 'gpt-5.1', display_name: 'GPT-5.1', description: 'Two', provider: 'openai', tool_support: false, subscription_tier: 'high' },
        { id: 'm1', name: 'gpt-4o-mini', display_name: 'GPT-4o Mini', description: 'One', provider: 'openai', tool_support: true, subscription_tier: 'low' },
    ];

    beforeEach(async () => {
        await TestBed.configureTestingModule({
            imports: [ModelPickerComponent],
            providers: [
                provideZonelessChangeDetection(),
                { provide: ModelFavoritesService, useClass: FakeModelFavoritesService },
            ],
        }).compileComponents();

        favorites = TestBed.inject(ModelFavoritesService) as unknown as FakeModelFavoritesService;
        fixture = TestBed.createComponent(ModelPickerComponent);
        fixture.componentRef.setInput('models', models);
        fixture.componentRef.setInput('selectedId', 'm1');
        fixture.detectChanges();
    });

    it('renders current model and emits selection from vendor flow', () => {
        const spy = vi.fn().mockName('selected');
        fixture.componentInstance.selected.subscribe(spy);

        fixture.nativeElement.querySelector('.model-picker__trigger').click();
        fixture.detectChanges();

        const options = fixture.nativeElement.querySelectorAll('.model-picker__option');
        expect(options.length).toBeGreaterThan(0);
        expect(options[0].textContent).toContain('GPT-4o Mini');
        options[0].click();

        expect(spy).toHaveBeenCalledWith(models[2]);
    });

    it('shows fine-grained capabilities on each model option', () => {
        fixture.componentRef.setInput('models', [
            {
                ...models[2],
                capabilities: {
                    tool_calling: true,
                    vision: true,
                    mcp: true,
                    tools: ['recall', 'web_search'],
                },
            },
        ]);
        fixture.componentRef.setInput('selectedId', 'm1');
        fixture.detectChanges();

        fixture.nativeElement.querySelector('.model-picker__trigger').click();
        fixture.detectChanges();

        const badges = [...fixture.nativeElement.querySelectorAll('.model-picker__capability')]
            .map((node: Element) => node.textContent?.trim());
        expect(badges).toEqual(['Tools 2', 'Vision', 'MCP']);
        expect(fixture.nativeElement.querySelector('.model-picker__capabilities')?.getAttribute('aria-label'))
            .toBe('Capabilities: 2 tools, vision, MCP');
        expect(fixture.nativeElement.querySelector('.model-picker__capability')?.getAttribute('title'))
            .toBe('Available tools: recall, web_search');
    });

    it('shows vendor step first when no model is selected', () => {
        fixture.componentRef.setInput('selectedId', null);
        fixture.detectChanges();

        fixture.nativeElement.querySelector('.model-picker__trigger').click();
        fixture.detectChanges();

        const vendors = fixture.nativeElement.querySelectorAll('.model-picker__vendor');
        expect(vendors.length).toBe(2);
        expect(vendors[0].textContent).toContain('GPT');
        expect(vendors[1].textContent).toContain('Claude');
    });

    it('groups models by backend provider field instead of name heuristics', () => {
        fixture.componentRef.setInput('models', [
            ...models,
            {
                id: 'm4',
                name: 'gpt-looking-gemini',
                display_name: 'Gemini Pro',
                description: 'Google',
                provider: 'google',
                tool_support: true,
                subscription_tier: 'high',
            },
        ]);
        fixture.componentRef.setInput('selectedId', null);
        fixture.detectChanges();

        fixture.nativeElement.querySelector('.model-picker__trigger').click();
        fixture.detectChanges();

        const vendors = [...fixture.nativeElement.querySelectorAll('.model-picker__vendor')].map((node: Element) => node.textContent ?? '');
        expect(vendors.some(text => text.includes('Gemini'))).toBe(true);
        expect(vendors.some(text => text.includes('GPT'))).toBe(true);
    });

    it('does not open when disabled', () => {
        fixture.componentRef.setInput('disabled', true);
        fixture.detectChanges();

        fixture.nativeElement.querySelector('.model-picker__trigger').click();

        expect(fixture.componentInstance.open()).toBe(false);
    });

    it('closes when clicking outside the picker', () => {
        fixture.componentInstance.toggle();
        fixture.detectChanges();
        expect(fixture.componentInstance.open()).toBe(true);

        document.body.dispatchEvent(new MouseEvent('click', { bubbles: true }));
        fixture.detectChanges();

        expect(fixture.componentInstance.open()).toBe(false);
    });

    it('toggles a favorite from the star without selecting the model', () => {
        const spy = vi.fn().mockName('selected');
        fixture.componentInstance.selected.subscribe(spy);

        fixture.nativeElement.querySelector('.model-picker__trigger').click();
        fixture.detectChanges();

        const star: HTMLButtonElement = fixture.nativeElement.querySelector('.model-picker__star');
        star.click();
        fixture.detectChanges();

        expect(spy).not.toHaveBeenCalled();
        expect(favorites.isFavorite('m1')).toBe(true);
    });

    it('shows a favorites sync failure inside the panel, including on a later open', () => {
        // The panel is a dropdown, so a write that fails after it closes has nowhere to
        // appear at the time. The message lives on the service, so it surfaces next open.
        favorites.error.set('Could not save favorites. Please try again.');
        fixture.detectChanges();

        fixture.nativeElement.querySelector('.model-picker__trigger').click();
        fixture.detectChanges();

        const alert = fixture.nativeElement.querySelector('.model-picker__error');
        expect(alert).not.toBeNull();
        expect(alert.getAttribute('role')).toBe('alert');
        expect(alert.textContent.trim()).toBe('Could not save favorites. Please try again.');
    });

    it('shows no error notice when the last sync succeeded', () => {
        fixture.nativeElement.querySelector('.model-picker__trigger').click();
        fixture.detectChanges();

        expect(fixture.nativeElement.querySelector('.model-picker__error')).toBeNull();
    });

    it('opens in the favorites view and lists favorited models when there are any', () => {
        favorites.toggle('m2');
        fixture.detectChanges();

        fixture.nativeElement.querySelector('.model-picker__trigger').click();
        fixture.detectChanges();

        expect(fixture.componentInstance.viewMode()).toBe('favorites');
        const names = [...fixture.nativeElement.querySelectorAll('.model-picker__option-name')].map((n: Element) => n.textContent?.trim());
        expect(names).toEqual(['GPT-5.1']);
    });

    it('shows tier list then drills into models like vendor', () => {
        fixture.componentRef.setInput('selectedId', null);
        fixture.detectChanges();

        fixture.nativeElement.querySelector('.model-picker__trigger').click();
        fixture.detectChanges();

        fixture.componentInstance.setViewMode('tier');
        fixture.detectChanges();

        const tiers = [...fixture.nativeElement.querySelectorAll('.model-picker__vendor')].map((n: Element) => n.textContent?.trim() ?? '');
        expect(tiers.some(text => text.includes('Tier 1'))).toBe(true);
        expect(tiers.some(text => text.includes('Tier 3'))).toBe(true);

        const tier1 = [...fixture.nativeElement.querySelectorAll('.model-picker__vendor')].find((n: Element) => n.textContent?.includes('Tier 1')) as HTMLButtonElement;
        tier1.click();
        fixture.detectChanges();

        const names = [...fixture.nativeElement.querySelectorAll('.model-picker__option-name')].map((n: Element) => n.textContent?.trim());
        expect(names).toEqual(['GPT-4o Mini']);
        expect(fixture.nativeElement.querySelector('.model-picker__back')?.textContent).toContain('Tier 1');
    });

    it('drills to the selected model tier when opening the tier tab', () => {
        fixture.nativeElement.querySelector('.model-picker__trigger').click();
        fixture.detectChanges();

        fixture.componentInstance.setViewMode('tier');
        fixture.detectChanges();

        expect(fixture.componentInstance.tierStep()).toBe('model');
        expect(fixture.componentInstance.activeTier()).toBe(1);
        const names = [...fixture.nativeElement.querySelectorAll('.model-picker__option-name')].map((n: Element) => n.textContent?.trim());
        expect(names).toEqual(['GPT-4o Mini']);
    });
});
