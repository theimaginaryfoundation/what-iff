import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';

import { PersonalityCardComponent } from './personality-card.component';
import { Personality } from '../../../core/models/personality.model';

function makePersonality(overrides: Partial<Personality> = {}): Personality {
    return {
        id: 'p-1',
        name: 'Vera Calder',
        system_prompt: 'A spectral cartographer with a fondness for forgotten places.',
        auto_pin_memories: false,
        expressions_enabled: true,
        image_style: 'auto', cover_image_id: null,
        cover_image_url: null,
        created_at: '2026-04-26T00:00:00Z',
        updated_at: '2026-04-26T00:00:00Z',
        stats: { chat_count: 4, last_used_at: '2026-04-27T00:00:00Z' },
        ...overrides,
    };
}

describe('PersonalityCardComponent', () => {
    let fixture: ComponentFixture<PersonalityCardComponent>;
    let component: PersonalityCardComponent;

    beforeEach(async () => {
        await TestBed.configureTestingModule({
            imports: [PersonalityCardComponent],
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                provideHttpClientTesting(),
            ],
        }).compileComponents();

        fixture = TestBed.createComponent(PersonalityCardComponent);
        component = fixture.componentInstance;
    });

    function setInputs(personality: Personality, defaultId: string | null = null): void {
        fixture.componentRef.setInput('personality', personality);
        fixture.componentRef.setInput('defaultPersonalityId', defaultId);
        fixture.detectChanges();
    }

    it('shows the name and prompt preview', () => {
        setInputs(makePersonality());
        const text = fixture.nativeElement.textContent;
        expect(text).toContain('Vera Calder');
        expect(text).toContain('spectral cartographer');
    });

    it('shows usage badge based on stats', () => {
        setInputs(makePersonality());
        expect(fixture.nativeElement.textContent).toContain('4 threads');
    });

    it('shows "New thread" badge when chat_count is 0', () => {
        setInputs(makePersonality({ stats: { chat_count: 0, last_used_at: null } }));
        expect(fixture.nativeElement.textContent).toContain('New thread');
    });

    it('shows default badge when matching defaultPersonalityId', () => {
        setInputs(makePersonality(), 'p-1');
        expect(fixture.nativeElement.textContent.toLowerCase()).toContain('default');
    });

    it('emits edit when the card is clicked', () => {
        setInputs(makePersonality());
        const events: string[] = [];
        component.action.subscribe(a => events.push(a));
        const article = fixture.nativeElement.querySelector('.personality-card') as HTMLElement;
        article.click();
        expect(events).toEqual(['edit']);
    });

    it('emits other actions and stops propagation', () => {
        setInputs(makePersonality());
        const events: string[] = [];
        component.action.subscribe(a => events.push(a));
        const event = new MouseEvent('click', { bubbles: true });
        vi.spyOn(event, 'stopPropagation').mockReturnValue(undefined);
        vi.spyOn(event, 'preventDefault').mockReturnValue(undefined);
        component.onAction('edit', event);
        expect(events).toEqual(['edit']);
        expect(event.stopPropagation).toHaveBeenCalled();
        expect(event.preventDefault).toHaveBeenCalled();
    });
});
