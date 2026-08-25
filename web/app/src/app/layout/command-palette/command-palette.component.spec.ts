import type { MockedObject } from "vitest";
import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection, signal } from '@angular/core';
import { Router, provideRouter } from '@angular/router';

import { ChatIconComponent } from '../../shared/ui/icons/icons';
import { CommandPaletteService } from '../../core/services/command-palette.service';
import { CommandPaletteComponent } from './command-palette.component';
import { SearchSection } from '../../core/models/search.model';

describe('CommandPaletteComponent', () => {
    let visible: ReturnType<typeof signal<boolean>>;
    let query: ReturnType<typeof signal<string>>;
    let sections: ReturnType<typeof signal<readonly SearchSection[]>>;
    let commandResults: ReturnType<typeof signal<any[]>>;
    let serviceSpy: MockedObject<CommandPaletteService> & {
        visible: any;
        query: any;
        sections: any;
        loading: any;
        commandResults: any;
    };
    let routerSpy: Pick<MockedObject<Router>, 'navigateByUrl'>;

    beforeEach(async () => {
        visible = signal(true);
        query = signal('');
        sections = signal<readonly SearchSection[]>([]);
        commandResults = signal<any[]>([]);

        serviceSpy = {
            open: vi.fn().mockName("CommandPaletteService.open"),
            close: vi.fn().mockName("CommandPaletteService.close"),
            toggle: vi.fn().mockName("CommandPaletteService.toggle"),
            setQuery: vi.fn().mockName("CommandPaletteService.setQuery"),
            register: vi.fn().mockName("CommandPaletteService.register"),
            registerCommand: vi.fn().mockName("CommandPaletteService.registerCommand"),
            runCommand: vi.fn().mockName("CommandPaletteService.runCommand"),
            visible,
            query,
            sections,
            loading: signal(false),
            commandResults
        } as any;

        routerSpy = {
            navigateByUrl: vi.fn().mockName("Router.navigateByUrl")
        } as unknown as Pick<MockedObject<Router>, 'navigateByUrl'>;

        await TestBed.configureTestingModule({
            imports: [CommandPaletteComponent],
            providers: [
                provideZonelessChangeDetection(),
                provideRouter([]),
                { provide: CommandPaletteService, useValue: serviceSpy },
                { provide: Router, useValue: routerSpy },
            ],
        }).compileComponents();
    });

    function chatSection(label: string): SearchSection {
        return {
            type: 'chat',
            results: [
                {
                    id: 'r1',
                    label,
                    description: 'Chat',
                    route: '/chat',
                    icon_type: 'chat',
                    score: 80,
                },
            ],
        };
    }

    function create() {
        const fixture = TestBed.createComponent(CommandPaletteComponent);
        fixture.detectChanges();
        return fixture;
    }

    it('renders nothing when service.visible() is false', () => {
        visible.set(false);
        const fixture = create();
        expect(fixture.nativeElement.querySelector('.cmd-palette')).toBeNull();
    });

    it('renders the dialog with role and label when visible', () => {
        const fixture = create();
        const dialog = fixture.nativeElement.querySelector('.cmd-palette');
        expect(dialog).not.toBeNull();
        expect(dialog.getAttribute('role')).toBe('dialog');
        expect(dialog.getAttribute('aria-modal')).toBe('true');
        expect(dialog.getAttribute('aria-label')).toBe('Command palette');
    });

    it('forwards typed value to service.setQuery', () => {
        const fixture = create();
        const input = fixture.nativeElement.querySelector('input.cmd-palette__input') as HTMLInputElement;
        input.value = 'atlas';
        input.dispatchEvent(new Event('input', { bubbles: true }));
        expect(serviceSpy.setQuery).toHaveBeenCalledWith('atlas');
    });

    it('Esc closes the palette', () => {
        const fixture = create();
        const backdrop = fixture.nativeElement.querySelector('.cmd-palette-backdrop') as HTMLElement;
        backdrop.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
        expect(serviceSpy.close).toHaveBeenCalled();
    });

    it('Arrow keys move selectedIndex (wrap-around)', async () => {
        sections.set([chatSection('first'), { type: 'memory', results: [{ id: 'm', label: 'mem', description: '', route: '/memory/m', icon_type: 'memory', score: 50 }] }]);
        const fixture = create();
        await new Promise(r => setTimeout(r, 0));
        const cmp = fixture.componentInstance;
        expect(cmp.selectedIndex()).toBe(0);

        fixture.nativeElement
            .querySelector('.cmd-palette-backdrop')
            .dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
        expect(cmp.selectedIndex()).toBe(1);

        fixture.nativeElement
            .querySelector('.cmd-palette-backdrop')
            .dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown', bubbles: true }));
        expect(cmp.selectedIndex()).toBe(0);

        fixture.nativeElement
            .querySelector('.cmd-palette-backdrop')
            .dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true }));
        expect(cmp.selectedIndex()).toBe(1);
    });

    it('Enter activates the selected row -> navigateByUrl + close for results', async () => {
        sections.set([chatSection('atlas roadmap')]);
        const fixture = create();
        await new Promise(r => setTimeout(r, 0));

        fixture.nativeElement
            .querySelector('.cmd-palette-backdrop')
            .dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));

        expect(routerSpy.navigateByUrl).toHaveBeenCalledWith('/chat');
        expect(serviceSpy.close).toHaveBeenCalled();
    });

    it('Enter activates a static command via runCommand', async () => {
        commandResults.set([
            { id: 'new-chat', label: 'New chat', icon: ChatIconComponent, run: () => { } },
        ]);
        const fixture = create();
        await new Promise(r => setTimeout(r, 0));

        fixture.nativeElement
            .querySelector('.cmd-palette-backdrop')
            .dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));

        expect(serviceSpy.runCommand).toHaveBeenCalledWith('new-chat');
    });

    it('clicking the backdrop closes the palette', () => {
        const fixture = create();
        const backdrop = fixture.nativeElement.querySelector('.cmd-palette-backdrop') as HTMLElement;
        backdrop.click();
        expect(serviceSpy.close).toHaveBeenCalled();
    });

    it('clicking inside the dialog does not close', () => {
        const fixture = create();
        const dialog = fixture.nativeElement.querySelector('.cmd-palette') as HTMLElement;
        dialog.click();
        expect(serviceSpy.close).not.toHaveBeenCalled();
    });

    it('exposes aria-activedescendant pointing at the active row', async () => {
        sections.set([chatSection('atlas roadmap')]);
        const fixture = create();
        await new Promise(r => setTimeout(r, 0));
        const input = fixture.nativeElement.querySelector('input.cmd-palette__input') as HTMLInputElement;
        expect(input.getAttribute('aria-activedescendant')).toBeTruthy();
    });
});
