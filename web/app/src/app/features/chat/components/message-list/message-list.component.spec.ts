import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideMarkdown } from 'ngx-markdown';

import { MessageListComponent } from './message-list.component';
import { GroupedItem } from '../../helpers/message-grouping.helpers';

async function flushDoubleRaf(): Promise<void> {
    await new Promise<void>(resolve => {
        requestAnimationFrame(() => {
            requestAnimationFrame(() => resolve());
        });
    });
}

describe('MessageListComponent', () => {
    let fixture: ComponentFixture<MessageListComponent>;

    beforeEach(async () => {
        await TestBed.configureTestingModule({
            imports: [MessageListComponent],
            providers: [provideZonelessChangeDetection(), provideMarkdown()],
        }).compileComponents();

        fixture = TestBed.createComponent(MessageListComponent);
    });

    it('renders grouped messages and exposes a polite log', () => {
        fixture.componentRef.setInput('groups', [messageGroup()]);
        fixture.detectChanges();

        const log = fixture.nativeElement.querySelector('[role="log"]') as HTMLElement;
        expect(log.getAttribute('aria-live')).toBe('polite');
        expect(fixture.nativeElement.querySelector('.message-list__lane')).not.toBeNull();
        expect(fixture.nativeElement.querySelector('app-message-group')).not.toBeNull();
    });

    it('renders model change dividers with a pill and guide lines', () => {
        fixture.componentRef.setInput('groups', [modelDivider()]);
        fixture.detectChanges();

        expect(fixture.nativeElement.querySelector('.message-list__divider-pill')?.textContent).toContain('Model changed from gpt-a to gpt-b');
        expect(fixture.nativeElement.querySelectorAll('.message-list__divider-line').length).toBe(2);
    });

    it('emits loadOlder when load more history is clicked', () => {
        const spy = vi.fn().mockName('loadOlder');
        fixture.componentInstance.loadOlder.subscribe(spy);
        fixture.componentRef.setInput('groups', [messageGroup()]);
        fixture.componentRef.setInput('hasMoreOlder', true);
        fixture.detectChanges();

        const button = fixture.nativeElement.querySelector('.message-list__history') as HTMLButtonElement;
        button.click();

        expect(spy).toHaveBeenCalled();
    });

    it('scrolls to the latest message when requested', () => {
        fixture.componentRef.setInput('groups', [messageGroup()]);
        fixture.detectChanges();
        fixture.componentInstance.isNearBottom.set(false);
        fixture.detectChanges();

        const log = fixture.nativeElement.querySelector('[role="log"]') as HTMLElement;
        const scrollSpy = vi.spyOn(log, 'scrollTo').mockReturnValue(undefined);
        Object.defineProperty(log, 'scrollHeight', { configurable: true, value: 1200 });

        const button = fixture.nativeElement.querySelector('.message-list__scroll') as HTMLButtonElement;
        button.click();

        expect(scrollSpy).toHaveBeenCalled();
        const scrollOptions = vi.mocked(scrollSpy).mock.lastCall![0] as unknown as ScrollToOptions;
        expect(scrollOptions).toEqual({ top: 1200, behavior: 'smooth' });
        expect(fixture.componentInstance.isNearBottom()).toBe(true);
    });

    it('positions an opened conversation at the bottom in the same render turn', () => {
        fixture.componentRef.setInput('conversationId', 'chat-1');
        fixture.componentRef.setInput('groups', []);
        fixture.detectChanges();
        const log = fixture.nativeElement.querySelector('[role="log"]') as HTMLElement;
        const scrollSpy = vi.spyOn(log, 'scrollTo').mockReturnValue(undefined);
        Object.defineProperty(log, 'scrollHeight', { configurable: true, value: 900 });

        fixture.componentRef.setInput('groups', [messageGroup()]);
        fixture.detectChanges();

        expect(scrollSpy).toHaveBeenCalledWith({ top: 900, behavior: 'auto' });
    });

    it('scrolls to the latest message after initial messages render', async () => {
        fixture.componentRef.setInput('conversationId', 'chat-1');
        fixture.componentRef.setInput('groups', []);
        fixture.detectChanges();
        const log = fixture.nativeElement.querySelector('[role="log"]') as HTMLElement;
        const scrollSpy = vi.spyOn(log, 'scrollTo').mockReturnValue(undefined);
        Object.defineProperty(log, 'scrollHeight', { configurable: true, value: 900 });

        fixture.componentRef.setInput('groups', [messageGroup()]);
        fixture.detectChanges();
        await flushDoubleRaf();

        expect(scrollSpy).toHaveBeenCalled();
        const scrollOptions = vi.mocked(scrollSpy).mock.lastCall![0] as unknown as ScrollToOptions;
        expect(scrollOptions).toEqual({ top: 900, behavior: 'auto' });
    });

    it('scrolls to latest again when switching conversations', async () => {
        fixture.componentRef.setInput('conversationId', 'chat-1');
        fixture.componentRef.setInput('groups', [messageGroup('m1')]);
        fixture.detectChanges();
        const log = fixture.nativeElement.querySelector('[role="log"]') as HTMLElement;
        const scrollSpy = vi.spyOn(log, 'scrollTo').mockReturnValue(undefined);
        Object.defineProperty(log, 'scrollHeight', { configurable: true, value: 900 });
        await flushDoubleRaf();
        scrollSpy.mockClear();

        fixture.componentRef.setInput('conversationId', 'chat-2');
        fixture.componentRef.setInput('groups', [messageGroup('m2')]);
        fixture.detectChanges();
        await flushDoubleRaf();

        expect(scrollSpy).toHaveBeenCalled();
        const scrollOptions = vi.mocked(scrollSpy).mock.lastCall![0] as unknown as ScrollToOptions;
        expect(scrollOptions).toEqual({ top: 900, behavior: 'auto' });
    });

    it('scrolls again when a new tail message group is appended while following bottom', async () => {
        fixture.componentRef.setInput('conversationId', 'chat-1');
        fixture.componentRef.setInput('groups', [messageGroup('a')]);
        fixture.detectChanges();
        const log = fixture.nativeElement.querySelector('[role="log"]') as HTMLElement;
        const scrollSpy = vi.spyOn(log, 'scrollTo').mockReturnValue(undefined);
        Object.defineProperty(log, 'scrollHeight', { configurable: true, value: 900 });
        await flushDoubleRaf();
        scrollSpy.mockClear();

        fixture.componentRef.setInput('groups', [messageGroup('a'), messageGroup('b')]);
        fixture.detectChanges();
        await flushDoubleRaf();

        expect(scrollSpy).toHaveBeenCalled();
    });

    it('does not auto-scroll on tail change after user scrolled away from bottom', async () => {
        fixture.componentRef.setInput('conversationId', 'chat-1');
        fixture.componentRef.setInput('groups', [messageGroup('a')]);
        fixture.detectChanges();
        const log = fixture.nativeElement.querySelector('[role="log"]') as HTMLElement;
        const scrollSpy = vi.spyOn(log, 'scrollTo').mockReturnValue(undefined);
        Object.defineProperty(log, 'scrollHeight', { configurable: true, value: 1000 });
        Object.defineProperty(log, 'scrollTop', { configurable: true, value: 0, writable: true });
        Object.defineProperty(log, 'clientHeight', { configurable: true, value: 400 });
        await flushDoubleRaf();
        scrollSpy.mockClear();

        fixture.componentInstance.onScroll({
            target: { scrollHeight: 1000, scrollTop: 0, clientHeight: 400 },
        } as unknown as Event);

        fixture.componentRef.setInput('groups', [messageGroup('a'), messageGroup('b')]);
        fixture.detectChanges();
        await flushDoubleRaf();

        expect(scrollSpy).not.toHaveBeenCalled();
    });

    it('stops following bottom as soon as user scrolls slightly up', async () => {
        fixture.componentRef.setInput('conversationId', 'chat-1');
        fixture.componentRef.setInput('groups', [messageGroup('a')]);
        fixture.detectChanges();
        const log = fixture.nativeElement.querySelector('[role="log"]') as HTMLElement;
        const scrollSpy = vi.spyOn(log, 'scrollTo').mockReturnValue(undefined);
        Object.defineProperty(log, 'scrollHeight', { configurable: true, value: 1000 });
        await flushDoubleRaf();
        scrollSpy.mockClear();

        fixture.componentInstance.onScroll({
            target: { scrollHeight: 1000, scrollTop: 590, clientHeight: 400 }, // distance 10px (not at bottom)
        } as unknown as Event);

        fixture.componentRef.setInput('groups', [messageGroup('a'), messageGroup('b')]);
        fixture.detectChanges();
        await flushDoubleRaf();

        expect(scrollSpy).not.toHaveBeenCalled();
    });
});

function messageGroup(id = 'm1'): GroupedItem {
    return {
        kind: 'message-group',
        origin: 'User',
        messages: [{
                id,
                chat_id: 'c1',
                message: 'Hello',
                origin: 'User',
                sent_at: '2024-01-01T00:00:00Z',
            }],
    };
}

function modelDivider(): GroupedItem {
    return {
        kind: 'model-change-divider',
        messageId: 'm1',
        previousModel: 'gpt-a',
        model: 'gpt-b',
    };
}
