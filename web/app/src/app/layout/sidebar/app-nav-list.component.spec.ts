import { Component, ChangeDetectionStrategy } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideRouter } from '@angular/router';

import { ChatIconComponent, UsersIconComponent } from '../../shared/ui/icons/icons';
import { AppNavListComponent } from './app-nav-list.component';
import { NavItem } from './nav.helpers';

@Component({
    selector: 'noop',
    standalone: true,
    changeDetection: ChangeDetectionStrategy.Eager,
    template: '',
})
class NoopComponent {
}

describe('AppNavListComponent', () => {
    const items: ReadonlyArray<NavItem> = [
        { id: 'chat', label: 'Chat', route: '/chat', icon: ChatIconComponent },
        { id: 'people', label: 'Personalities', route: '/personality', icon: UsersIconComponent },
    ];

    beforeEach(async () => {
        await TestBed.configureTestingModule({
            imports: [AppNavListComponent],
            providers: [
                provideZonelessChangeDetection(),
                provideRouter([
                    { path: 'chat', component: NoopComponent },
                    { path: 'personality', component: NoopComponent },
                ]),
            ],
        }).compileComponents();
    });

    function createFixture(collapsed = false) {
        const fixture = TestBed.createComponent(AppNavListComponent);
        fixture.componentRef.setInput('items', items);
        fixture.componentRef.setInput('collapsed', collapsed);
        fixture.detectChanges();
        return fixture;
    }

    it('renders a list item for each nav entry', () => {
        const fixture = createFixture();
        const links = fixture.nativeElement.querySelectorAll('a.app-nav-list__link');
        expect(links.length).toBe(2);
    });

    it('shows labels when expanded and hides them when collapsed', () => {
        const fixture = createFixture(false);
        expect(fixture.nativeElement.querySelectorAll('.app-nav-list__label').length).toBe(2);

        fixture.componentRef.setInput('collapsed', true);
        fixture.detectChanges();
        expect(fixture.nativeElement.querySelectorAll('.app-nav-list__label').length).toBe(0);
    });

    it('applies the collapsed class to each link when collapsed', () => {
        const fixture = createFixture(true);
        const links = fixture.nativeElement.querySelectorAll('a.app-nav-list__link');
        for (const link of Array.from(links) as HTMLElement[]) {
            expect(link.classList.contains('app-nav-list__link--collapsed')).toBe(true);
            expect(link.getAttribute('aria-label')).toBeTruthy();
        }
    });

    it('emits select with the right item when a link is clicked', () => {
        const fixture = createFixture();
        const emitted: NavItem[] = [];
        fixture.componentInstance.select.subscribe((item: NavItem) => emitted.push(item));

        const link = fixture.nativeElement.querySelector('a.app-nav-list__link') as HTMLElement;
        link.click();
        expect(emitted.length).toBe(1);
        expect(emitted[0].id).toBe('chat');
    });
});
