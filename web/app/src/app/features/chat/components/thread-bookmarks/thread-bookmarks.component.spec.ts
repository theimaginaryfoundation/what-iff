import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';

import { ThreadBookmarksComponent } from './thread-bookmarks.component';
import { MessageBookmark } from '../../../../core/models/message.model';

describe('ThreadBookmarksComponent', () => {
    const bookmarks: MessageBookmark[] = [
        { id: 'b-1', origin: 'User', snippet: 'First bookmark', sent_at: '2026-06-23T00:00:01Z' },
        { id: 'b-2', origin: 'Assistant', snippet: 'Second bookmark', sent_at: '2026-06-23T00:00:02Z' },
    ];

    function setup() {
        TestBed.configureTestingModule({
            providers: [provideZonelessChangeDetection()],
        });
        const fixture = TestBed.createComponent(ThreadBookmarksComponent);
        fixture.componentRef.setInput('bookmarks', bookmarks);
        fixture.detectChanges();
        return fixture;
    }

    function openMenu(fixture: ReturnType<typeof setup>) {
        const trigger = fixture.nativeElement.querySelector('.tb__btn') as HTMLButtonElement;
        trigger.click();
        fixture.detectChanges();
    }

    it('renders a remove control per bookmark once the menu is open', () => {
        const fixture = setup();
        openMenu(fixture);
        const removeButtons = fixture.nativeElement.querySelectorAll('.tb__remove');
        expect(removeButtons.length).toBe(bookmarks.length);
    });

    it('emits jump and closes the menu when a row is selected', () => {
        const fixture = setup();
        const jumped: MessageBookmark[] = [];
        fixture.componentInstance.jump.subscribe(b => jumped.push(b));
        openMenu(fixture);

        (fixture.nativeElement.querySelector('.tb__item') as HTMLButtonElement).click();
        fixture.detectChanges();

        expect(jumped).toEqual([bookmarks[0]]);
        expect(fixture.componentInstance.open()).toBe(false);
    });

    it('defers removal: un-starring marks the row pending without saving or jumping', () => {
        const fixture = setup();
        const committed: string[][] = [];
        const jumped: MessageBookmark[] = [];
        fixture.componentInstance.commitRemovals.subscribe(ids => committed.push(ids));
        fixture.componentInstance.jump.subscribe(b => jumped.push(b));
        openMenu(fixture);

        (fixture.nativeElement.querySelector('.tb__remove') as HTMLButtonElement).click();
        fixture.detectChanges();

        expect(fixture.componentInstance.isPending('b-1')).toBe(true);
        expect(committed).toEqual([]); // nothing saved yet
        expect(jumped).toEqual([]); // stopPropagation keeps the row's jump handler from firing
        expect(fixture.componentInstance.open()).toBe(true); // menu stays open
        // Row is marked pending (dimmed + hollow star) rather than gone.
        expect(fixture.nativeElement.querySelector('.tb__item--pending')).not.toBeNull();
    });

    it('re-clicking a pending removal restores it, so a mis-click is recoverable', () => {
        const fixture = setup();
        const committed: string[][] = [];
        fixture.componentInstance.commitRemovals.subscribe(ids => committed.push(ids));
        openMenu(fixture);
        const removeBtn = fixture.nativeElement.querySelector('.tb__remove') as HTMLButtonElement;

        removeBtn.click();
        fixture.detectChanges();
        expect(fixture.componentInstance.isPending('b-1')).toBe(true);

        removeBtn.click();
        fixture.detectChanges();
        expect(fixture.componentInstance.isPending('b-1')).toBe(false);

        fixture.componentInstance.close();
        expect(committed).toEqual([]); // restored before close → nothing removed
    });

    it('commits the still-pending removals once, when the menu closes', () => {
        const fixture = setup();
        const committed: string[][] = [];
        fixture.componentInstance.commitRemovals.subscribe(ids => committed.push(ids));
        openMenu(fixture);
        const removeButtons = fixture.nativeElement.querySelectorAll('.tb__remove');

        (removeButtons[0] as HTMLButtonElement).click();
        (removeButtons[1] as HTMLButtonElement).click();
        fixture.detectChanges();
        expect(committed).toEqual([]); // still nothing until close

        fixture.componentInstance.close();
        expect(committed).toEqual([['b-1', 'b-2']]);
    });
});
