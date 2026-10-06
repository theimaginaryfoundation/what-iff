import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { Router } from '@angular/router';
import { provideMarkdown } from 'ngx-markdown';

import { MessageBubbleComponent } from './message-bubble.component';
import { ChatMessage } from '../../../../core/models/message.model';
import { CHAT_PENDING_ASSISTANT_MESSAGE_ID } from '../../chat.constants';
import { formatThreadReferences } from '../../helpers/thread-reference.helpers';

describe('MessageBubbleComponent', () => {
    let fixture: ComponentFixture<MessageBubbleComponent>;

    beforeEach(async () => {
        await TestBed.configureTestingModule({
            imports: [MessageBubbleComponent],
            providers: [provideZonelessChangeDetection(), provideHttpClient(withXhr()), provideHttpClientTesting(), provideMarkdown()],
        }).compileComponents();

        fixture = TestBed.createComponent(MessageBubbleComponent);
        fixture.componentRef.setInput('message', message('User'));
        fixture.componentRef.setInput('displayContent', 'Hello');
        fixture.detectChanges();
    });

    describe('attached-thread block in user messages', () => {
        const ID_A = '11111111-1111-4111-8111-111111111111';
        const ID_B = '22222222-2222-4222-8222-222222222222';
        const block = formatThreadReferences([
            { id: ID_A, name: 'Trip "plans"' },
            { id: ID_B, name: 'Budget' },
        ]);

        it('renders the leading reference lines as thread chips and the rest as the message', () => {
            fixture.componentRef.setInput('displayContent', `${block}What did we decide?`);
            fixture.detectChanges();
            const root: HTMLElement = fixture.nativeElement;
            const chips = [...root.querySelectorAll('.bubble__thread-ref')] as HTMLAnchorElement[];

            expect(chips.map(c => c.querySelector('.bubble__thread-ref-name')?.textContent)).toEqual(['Trip "plans"', 'Budget']);
            expect(chips[0].getAttribute('href')).toBe(`/chat/${ID_A}`);
            expect(fixture.componentInstance.bodyContent()).toBe('What did we decide?');
        });

        it('navigates in-app when a chip is clicked', () => {
            const navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
            fixture.componentRef.setInput('displayContent', `${block}Hi`);
            fixture.detectChanges();

            (fixture.nativeElement.querySelector('.bubble__thread-ref') as HTMLAnchorElement).click();

            expect(navigate).toHaveBeenCalledWith(['/chat', ID_A]);
        });

        it('leaves assistant messages and non-matching text untouched', () => {
            fixture.componentRef.setInput('message', message('Assistant'));
            fixture.componentRef.setInput('displayContent', `${block}Hi`);
            fixture.detectChanges();
            expect(fixture.nativeElement.querySelector('.bubble__thread-refs')).toBeNull();

            fixture.componentRef.setInput('message', message('User'));
            fixture.componentRef.setInput('displayContent', '[Referenced thread "x"] not really');
            fixture.detectChanges();
            expect(fixture.nativeElement.querySelector('.bubble__thread-refs')).toBeNull();
            expect(fixture.componentInstance.bodyContent()).toBe('[Referenced thread "x"] not really');
        });
    });

    describe('image attachment fidelity', () => {
        const attachments = [{
            id: 'img-1',
            user_id: 'u1',
            name: 'photo.png',
            file_type: 'image/png',
            created_at: '2024-01-01T00:00:00Z',
        }];

        it('loads the full-resolution image in user turns, not the 256px thumbnail', () => {
            const httpMock = TestBed.inject(HttpTestingController);
            fixture.componentRef.setInput('message', { ...message('User'), attachments });
            fixture.detectChanges();

            httpMock.expectNone(req => req.urlWithParams.includes('/image-gallery/img-1?size=thumbnail'));
            httpMock.expectOne(req => req.urlWithParams.includes('/image-gallery/img-1?size=full'))
                .flush(new Blob(['x'], { type: 'image/png' }));
            expect(fixture.nativeElement.querySelector('.message-images--full')).toBeTruthy();
            httpMock.verify();
        });

        it('loads the full-resolution image in assistant turns too, not the 256px thumbnail', () => {
            const httpMock = TestBed.inject(HttpTestingController);
            fixture.componentRef.setInput('message', { ...message('Assistant'), attachments });
            fixture.detectChanges();

            httpMock.expectNone(req => req.urlWithParams.includes('/image-gallery/img-1?size=thumbnail'));
            httpMock.expectOne(req => req.urlWithParams.includes('/image-gallery/img-1?size=full'))
                .flush(new Blob(['x'], { type: 'image/png' }));
            expect(fixture.nativeElement.querySelector('.message-images--full')).toBeTruthy();
            httpMock.verify();
        });
    });

    it('shows model/mood hint for assistant messages when present', () => {
        fixture.componentRef.setInput('message', {
            ...message('Assistant'),
            generation_model: 'claude-sonnet',
            generation_mood_name: 'Focus',
        });
        fixture.componentRef.setInput('displayContent', 'Hi');
        fixture.detectChanges();

        const hint = fixture.nativeElement.querySelector('.bubble__hint');
        expect(hint?.textContent?.trim()).toBe('[claude-sonnet:Focus]');
        expect(fixture.nativeElement.querySelector('.bubble__meta')?.classList).toContain('bubble__meta--assistant');
    });

    it('shows model reasoning in a collapsed disclosure for assistant messages only', () => {
        // No reasoning → no disclosure.
        fixture.componentRef.setInput('message', { ...message('Assistant') });
        fixture.detectChanges();
        expect(fixture.nativeElement.querySelector('.bubble__reasoning')).toBeNull();

        fixture.componentRef.setInput('message', {
            ...message('Assistant'),
            model_reasoning: '  First I weighed X.\n\nThen Y.  ',
        });
        fixture.detectChanges();
        const details = fixture.nativeElement.querySelector('details.bubble__reasoning') as HTMLDetailsElement;
        expect(details).not.toBeNull();
        expect(details.open).toBe(false);
        expect(details.querySelector('summary')?.textContent).toContain('Thought process');
        expect(details.querySelector('.bubble__reasoning-text')?.textContent).toBe('First I weighed X.\n\nThen Y.');
        // Sits above the reply body.
        expect(details.nextElementSibling?.classList).toContain('bubble__body');

        // User messages never show it, even if the field is somehow present.
        fixture.componentRef.setInput('message', { ...message('User'), model_reasoning: 'nope' });
        fixture.detectChanges();
        expect(fixture.nativeElement.querySelector('.bubble__reasoning')).toBeNull();
    });

    it('shows live reasoning open while thinking, then settles collapsed once the reply streams', () => {
        fixture.componentRef.setInput('message', { ...pendingAssistantMessage(), model_reasoning: 'Considering it' });
        fixture.componentRef.setInput('displayContent', '');
        fixture.detectChanges();

        const details = () => fixture.nativeElement.querySelector('details.bubble__reasoning') as HTMLDetailsElement;
        expect(details().open).toBe(true);
        expect(details().classList).toContain('bubble__reasoning--live');
        expect(details().querySelector('summary')?.textContent).toContain('Thinking…');
        // The typing dots stay: the reply itself hasn't started.
        expect(fixture.nativeElement.querySelector('.bubble__pending-dots')).not.toBeNull();

        fixture.componentRef.setInput('displayContent', 'Here is my answer');
        fixture.detectChanges();
        expect(details().open).toBe(false);
        expect(details().classList).not.toContain('bubble__reasoning--live');
        expect(details().querySelector('summary')?.textContent).toContain('Thought process');
    });

    it('labels the speaker and emits copy events', () => {
        const spy = vi.fn().mockName('copy');
        fixture.componentInstance.copy.subscribe(spy);

        const copyBtn = fixture.nativeElement.querySelector('.bubble__copy') as HTMLButtonElement;
        copyBtn.click();

        expect(fixture.nativeElement.querySelector('article').getAttribute('aria-label')).toBe('User message');
        expect(fixture.nativeElement.querySelector('article').classList).toContain('bubble--user');
        expect(spy).toHaveBeenCalled();
    });

    it('shows a Context button only for assistant messages with a breakdown, and emits it', () => {
        // No breakdown → no button.
        fixture.componentRef.setInput('message', { ...message('Assistant') });
        fixture.detectChanges();
        expect(fixture.nativeElement.querySelector('.bubble__context')).toBeNull();

        fixture.componentRef.setInput('message', {
            ...message('Assistant'),
            context_breakdown: {
                segments: [{ kind: 'history_turn', segments: 1, tokens: 100, cacheable: false }],
                total_tokens: 100,
                budget_tokens: 30000,
                captured_at: '2026-08-17T12:00:00Z',
            },
        });
        fixture.detectChanges();

        const spy = vi.fn().mockName('showContext');
        fixture.componentInstance.showContext.subscribe(spy);
        const btn = fixture.nativeElement.querySelector('.bubble__context') as HTMLButtonElement;
        expect(btn).toBeTruthy();
        btn.click();
        expect(spy).toHaveBeenCalled();
    });

    it('places Context before the model and personality hint', () => {
        fixture.componentRef.setInput('message', {
            ...message('Assistant'),
            generation_model: 'claude-sonnet',
            generation_mood_name: 'Focus',
            context_breakdown: {
                segments: [{ kind: 'history_turn', segments: 1, tokens: 100, cacheable: false }],
                total_tokens: 100,
                budget_tokens: 30000,
                captured_at: '2026-08-17T12:00:00Z',
            },
        });
        fixture.detectChanges();

        const labels = Array.from(fixture.nativeElement.querySelector('.bubble__meta-end')?.children ?? [], (element: Element) => element.textContent?.trim());
        expect(labels).toEqual(['Context', '[claude-sonnet:Focus]']);
    });

    it('shows skill name pills for user messages with rituals', () => {
        fixture.componentRef.setInput('message', {
            ...message('User'),
            rituals: [
                { id: 'r1', name: 'Morning brief', description: '', content: '', hotkeys: '', personality_id: null, created_at: '', updated_at: '' },
            ],
        });
        fixture.componentRef.setInput('displayContent', 'Hi');
        fixture.detectChanges();

        const pills = fixture.nativeElement.querySelectorAll('.bubble__skill-pill');
        expect(pills.length).toBe(1);
        expect(pills[0].textContent?.trim()).toBe('Morning brief');
    });

    it('shows the queued-behind-an-earlier-turn status instead of the dots', () => {
        fixture.componentRef.setInput('message', { ...pendingAssistantMessage(), pending_status: 'Waiting for the previous reply…' });
        fixture.componentRef.setInput('displayContent', '');
        fixture.detectChanges();

        expect(fixture.nativeElement.querySelector('.bubble__pending-status')?.textContent).toContain('Waiting for the previous reply…');
        expect(fixture.nativeElement.querySelector('.bubble__pending-dots')).toBeNull();
    });

    it('renders typing dots for the pending assistant placeholder without copy chrome', () => {
        fixture.componentRef.setInput('message', pendingAssistantMessage());
        fixture.componentRef.setInput('displayContent', '');
        fixture.detectChanges();

        expect(fixture.nativeElement.querySelector('article').getAttribute('aria-label')).toBe('Assistant is composing a reply');
        expect(fixture.nativeElement.querySelector('.bubble__pending-dots')).toBeTruthy();
        expect(fixture.nativeElement.querySelector('.bubble__copy')).toBeNull();
    });

    it('renders streamed placeholder content once available', () => {
        fixture.componentRef.setInput('message', pendingAssistantMessage());
        fixture.componentRef.setInput('displayContent', 'Partial streamed text');
        fixture.detectChanges();

        expect(fixture.nativeElement.querySelector('.bubble__pending-dots')).toBeNull();
        expect(fixture.componentInstance.showPendingDots()).toBe(false);
        expect(fixture.nativeElement.querySelector('app-message-content')).toBeTruthy();
    });
});

function message(origin: ChatMessage['origin']): ChatMessage {
    return {
        id: 'm1',
        chat_id: 'c1',
        message: 'Hello',
        origin,
        sent_at: '2024-01-01T00:00:00Z',
    };
}

function pendingAssistantMessage(): ChatMessage {
    return {
        id: CHAT_PENDING_ASSISTANT_MESSAGE_ID,
        chat_id: 'c1',
        message: '',
        origin: 'Assistant',
        sent_at: '1970-01-01T00:00:00.000Z',
        generation_personality: 'Kai',
        generation_expression_key: 'thinking',
    };
}
