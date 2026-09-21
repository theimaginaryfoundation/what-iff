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

    it('emits remove without jumping and keeps the menu open so several can be cleared', () => {
        const fixture = setup();
        const removed: MessageBookmark[] = [];
        const jumped: MessageBookmark[] = [];
        fixture.componentInstance.remove.subscribe(b => removed.push(b));
        fixture.componentInstance.jump.subscribe(b => jumped.push(b));
        openMenu(fixture);

        (fixture.nativeElement.querySelector('.tb__remove') as HTMLButtonElement).click();
        fixture.detectChanges();

        expect(removed).toEqual([bookmarks[0]]);
        expect(jumped).toEqual([]); // stopPropagation keeps the row's jump handler from firing
        expect(fixture.componentInstance.open()).toBe(true);
    });
});
