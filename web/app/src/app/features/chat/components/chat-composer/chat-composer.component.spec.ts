import type { MockedObject } from "vitest";
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { of } from 'rxjs';

import { ImageGalleryService } from '../../../../core/services/image-gallery.service';
import { RitualService } from '../../../../core/services/ritual.service';
import { MoodService } from '../../../../core/services/mood.service';
import { ChatComposerComponent } from './chat-composer.component';
import { Mood } from '../../../../core/models/mood.model';
import { TEXT_LIMIT_HARD_MAX, TEXT_LIMIT_WARNING_THRESHOLD, } from '../../../../core/constants/text-limits.constants';

const TEST_MODES: Mood[] = [
    {
        id: 'mode-writing',
        name: 'Writing',
        description: 'Drafting mode',
        prompt_snippet: 'Be warm.',
        image_ids: [],
        ritual_ids: [],
        personality_ids: ['persona-1'],
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
    },
    {
        id: 'mode-review',
        name: 'Code Review',
        description: 'Review mode',
        prompt_snippet: 'Be strict.',
        image_ids: [],
        ritual_ids: [],
        personality_ids: ['persona-1'],
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
    },
];

describe('ChatComposerComponent', () => {
    type RitualServiceMock = Pick<MockedObject<RitualService>, 'getAvailableRituals' | 'listSystemRituals'>;
    type ImageGalleryServiceMock = Pick<MockedObject<ImageGalleryService>, 'listImages' | 'referenceImage' | 'getImageUrl'>;
    type MoodServiceMock = Pick<MockedObject<MoodService>, 'listMoods'>;

    let fixture: ComponentFixture<ChatComposerComponent>;
    let ritualService: RitualServiceMock;
    let imageGallery: ImageGalleryServiceMock;
    let moodService: MoodServiceMock;

    beforeEach(async () => {
        ritualService = {
            getAvailableRituals: vi.fn().mockName("RitualService.getAvailableRituals"),
            listSystemRituals: vi.fn().mockName("RitualService.listSystemRituals")
        } as unknown as RitualServiceMock;
        ritualService.getAvailableRituals.mockReturnValue(of({ results: [{ id: 'r1', name: 'Alpha', description: '', content: '', hotkeys: '', personality_id: null, created_at: '', updated_at: '' }], total_count: 1, page: 1 }));
        ritualService.listSystemRituals.mockReturnValue(of([]));

        imageGallery = {
            listImages: vi.fn().mockName("ImageGalleryService.listImages"),
            referenceImage: vi.fn().mockName("ImageGalleryService.referenceImage"),
            getImageUrl: vi.fn().mockName("ImageGalleryService.getImageUrl")
        } as unknown as ImageGalleryServiceMock;
        imageGallery.listImages.mockReturnValue(of({ results: [], total_count: 0, page: 1, page_size: 30 }));
        imageGallery.referenceImage.mockReturnValue(of({
            id: 'ref-1',
            user_id: 'u-1',
            name: 'ref.png',
            file_type: 'image/png',
            created_at: '2026-01-01T00:00:00Z',
        }));
        imageGallery.getImageUrl.mockReturnValue('/api/image-gallery/x?size=thumbnail');

        moodService = {
            listMoods: vi.fn().mockName("MoodService.listMoods")
        } as unknown as MoodServiceMock;
        moodService.listMoods.mockReturnValue(of({ results: TEST_MODES, total_count: TEST_MODES.length, page: 1 }));

        await TestBed.configureTestingModule({
            imports: [ChatComposerComponent],
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                { provide: RitualService, useValue: ritualService },
                { provide: ImageGalleryService, useValue: imageGallery },
                { provide: MoodService, useValue: moodService },
            ],
        }).compileComponents();

        fixture = TestBed.createComponent(ChatComposerComponent);
        fixture.componentRef.setInput('draft', 'hello');
        fixture.componentRef.setInput('chatId', 'chat-1');
        fixture.componentRef.setInput('personalityId', 'persona-1');
        fixture.componentInstance.draftChange.subscribe(value => {
            fixture.componentRef.setInput('draft', value);
            fixture.detectChanges();
        });
        fixture.detectChanges();
    });

    it('sends on Enter', () => {
        const spy = vi.fn().mockName('send');
        fixture.componentInstance.send.subscribe(spy);

        fixture.componentInstance.onKeydown(new KeyboardEvent('keydown', { key: 'Enter' }));

        expect(spy).toHaveBeenCalledWith('hello');
    });

    it('keeps Shift+Enter as a newline action', () => {
        const spy = vi.fn().mockName('send');
        fixture.componentInstance.send.subscribe(spy);

        fixture.componentInstance.onKeydown(new KeyboardEvent('keydown', { key: 'Enter', shiftKey: true }));

        expect(spy).not.toHaveBeenCalled();
    });

    it('treats Enter as a newline (not send) on soft-keyboard devices', () => {
        const spy = vi.fn().mockName('send');
        fixture.componentInstance.send.subscribe(spy);
        fixture.componentInstance.softKeyboard.set(true);

        const event = new KeyboardEvent('keydown', { key: 'Enter' });
        const prevent = vi.spyOn(event, 'preventDefault').mockReturnValue(undefined);
        fixture.componentInstance.onKeydown(event);

        expect(spy).not.toHaveBeenCalled();
        expect(prevent).not.toHaveBeenCalled();
    });

    it('attaches images pasted into the composer', () => {
        const filesSpy = vi.fn().mockName('filesSelected');
        fixture.componentInstance.filesSelected.subscribe(filesSpy);

        const image = new File(['x'], 'screenshot.png', { type: 'image/png' });
        const event = {
            clipboardData: {
                items: [{ kind: 'file', type: 'image/png', getAsFile: () => image }],
            },
            preventDefault: vi.fn().mockName('preventDefault'),
        } as unknown as ClipboardEvent;

        fixture.componentInstance.onPaste(event);

        expect(event.preventDefault).toHaveBeenCalled();
        const emitted = vi.mocked(filesSpy).mock.lastCall![0] as File[];
        expect(emitted.length).toBe(1);
        expect(emitted[0].type).toBe('image/png');
    });

    it('attaches non-image files pasted into the composer', () => {
        const filesSpy = vi.fn().mockName('filesSelected');
        fixture.componentInstance.filesSelected.subscribe(filesSpy);

        const pdf = new File(['x'], 'report.pdf', { type: 'application/pdf' });
        const event = {
            clipboardData: {
                items: [{ kind: 'file', type: 'application/pdf', getAsFile: () => pdf }],
            },
            preventDefault: vi.fn().mockName('preventDefault'),
        } as unknown as ClipboardEvent;

        fixture.componentInstance.onPaste(event);

        expect(event.preventDefault).toHaveBeenCalled();
        const emitted = vi.mocked(filesSpy).mock.lastCall![0] as File[];
        expect(emitted.length).toBe(1);
        expect(emitted[0].name).toBe('report.pdf');
    });

    it('ignores non-file clipboard content on paste', () => {
        const filesSpy = vi.fn().mockName('filesSelected');
        fixture.componentInstance.filesSelected.subscribe(filesSpy);

        const event = {
            clipboardData: {
                items: [{ kind: 'string', type: 'text/plain', getAsFile: () => null }],
                getData: () => '',
            },
            preventDefault: vi.fn().mockName('preventDefault'),
        } as unknown as ClipboardEvent;

        fixture.componentInstance.onPaste(event);

        expect(event.preventDefault).not.toHaveBeenCalled();
        expect(filesSpy).not.toHaveBeenCalled();
    });

    it('names pasted files that arrive without a filename', () => {
        const filesSpy = vi.fn().mockName('filesSelected');
        fixture.componentInstance.filesSelected.subscribe(filesSpy);

        const image = new File(['x'], '', { type: 'image/png' });
        const event = {
            clipboardData: {
                items: [{ kind: 'file', type: 'image/png', getAsFile: () => image }],
            },
            preventDefault: vi.fn().mockName('preventDefault'),
        } as unknown as ClipboardEvent;

        fixture.componentInstance.onPaste(event);

        const emitted = vi.mocked(filesSpy).mock.lastCall![0] as File[];
        expect(emitted[0].name).toMatch(/^pasted-file-\d+\.png$/);
    });

    it('does not send when thread is archived', () => {
        const spy = vi.fn().mockName('send');
        fixture.componentInstance.send.subscribe(spy);
        fixture.componentRef.setInput('threadArchived', true);
        fixture.detectChanges();

        fixture.componentInstance.onKeydown(new KeyboardEvent('keydown', { key: 'Enter' }));

        expect(spy).not.toHaveBeenCalled();
        expect(fixture.nativeElement.textContent).toContain('archived');
    });

    it('does not send while attachments are uploading', () => {
        const spy = vi.fn().mockName('send');
        fixture.componentInstance.send.subscribe(spy);
        fixture.componentRef.setInput('attachments', [
            { file: new File(['hello'], 'notes.txt', { type: 'text/plain' }), isUploading: true },
        ]);
        fixture.detectChanges();

        fixture.componentInstance.onKeydown(new KeyboardEvent('keydown', { key: 'Enter' }));

        expect(spy).not.toHaveBeenCalled();
    });

    it('shows stop button while generating', () => {
        fixture.componentRef.setInput('isGenerating', true);
        fixture.detectChanges();

        expect(fixture.nativeElement.querySelector('.composer__stop')).toBeTruthy();
        expect(fixture.nativeElement.querySelector('.composer__send')).toBeFalsy();
    });

    it('emits stop when stop button is clicked', () => {
        fixture.componentRef.setInput('isGenerating', true);
        fixture.detectChanges();
        const stopSpy = vi.fn().mockName('stop');
        fixture.componentInstance.stop.subscribe(stopSpy);

        const button = fixture.nativeElement.querySelector('.composer__stop') as HTMLButtonElement;
        button.click();

        expect(stopSpy).toHaveBeenCalled();
    });

    it('blocks send when draft exceeds hard limit', () => {
        const sendSpy = vi.fn().mockName('send');
        fixture.componentInstance.send.subscribe(sendSpy);
        fixture.componentRef.setInput('draft', 'x'.repeat(TEXT_LIMIT_HARD_MAX + 1));
        fixture.detectChanges();

        fixture.componentInstance.onKeydown(new KeyboardEvent('keydown', { key: 'Enter' }));

        expect(sendSpy).not.toHaveBeenCalled();
        expect(fixture.nativeElement.textContent).toContain('Message exceeds the');
    });

    it('warns before sending near-limit drafts and sends after confirmation', () => {
        const sendSpy = vi.fn().mockName('send');
        fixture.componentInstance.send.subscribe(sendSpy);
        fixture.componentRef.setInput('draft', 'x'.repeat(TEXT_LIMIT_WARNING_THRESHOLD));
        fixture.detectChanges();

        fixture.componentInstance.onKeydown(new KeyboardEvent('keydown', { key: 'Enter' }));
        expect(sendSpy).not.toHaveBeenCalled();
        expect(fixture.componentInstance.limitWarningOpen()).toBe(true);

        fixture.componentInstance.confirmLimitWarningAndSend();
        expect(sendSpy).toHaveBeenCalledWith('x'.repeat(TEXT_LIMIT_WARNING_THRESHOLD));
    });

    it('opens slash commands when input starts with slash', () => {
        const event = { target: { value: '/rit' } } as unknown as Event;

        fixture.componentInstance.onInput(event);

        expect(fixture.componentInstance.slashOpen()).toBe(true);
        expect(fixture.componentInstance.filteredCommands().map(c => c.id)).toEqual(['skill']);
    });

    it('opens skill picker from plus menu and loads rituals', () => {
        const plus = fixture.nativeElement.querySelector('.composer__plus') as HTMLButtonElement;
        plus.click();
        fixture.detectChanges();

        expect(fixture.nativeElement.textContent).toContain('Skill');

        const menuButtons = fixture.nativeElement.querySelectorAll('.composer__plus-menu button');
        const skill = Array.from(menuButtons).find((b): b is HTMLButtonElement => b instanceof HTMLButtonElement && !!b.textContent?.includes('Skill'));
        expect(skill).toBeTruthy();
        skill!.click();
        fixture.detectChanges();

        expect(fixture.componentInstance.skillPickerOpen()).toBe(true);
        expect(fixture.componentInstance.plusOpen()).toBe(false);
        expect(ritualService.getAvailableRituals).toHaveBeenCalledWith('chat-1', 1, 100);
        expect(ritualService.listSystemRituals).toHaveBeenCalled();
    });

    it('closes plus and slash menus when clicking outside the composer', () => {
        fixture.componentInstance.plusOpen.set(true);
        fixture.componentInstance.slashOpen.set(true);
        fixture.detectChanges();

        document.body.dispatchEvent(new MouseEvent('click', { bubbles: true }));
        fixture.detectChanges();

        expect(fixture.componentInstance.plusOpen()).toBe(false);
        expect(fixture.componentInstance.slashOpen()).toBe(false);
    });

    it('closes an open submenu when plus is clicked without reopening the plus menu', () => {
        fixture.componentInstance.openEmojiPicker();
        fixture.detectChanges();
        expect(fixture.componentInstance.emojiOpen()).toBe(true);

        const plus = fixture.nativeElement.querySelector('.composer__plus') as HTMLButtonElement;
        plus.click();
        fixture.detectChanges();

        expect(fixture.componentInstance.emojiOpen()).toBe(false);
        expect(fixture.componentInstance.plusOpen()).toBe(false);
    });

    it('opens emoji picker when a slash command row is clicked', () => {
        fixture.componentInstance.onInput({ target: { value: '/emoji' } } as unknown as Event);
        fixture.detectChanges();

        const slashRow = fixture.nativeElement.querySelector('.slash-menu__row') as HTMLButtonElement;
        expect(slashRow).toBeTruthy();
        slashRow.click();
        fixture.detectChanges();

        expect(fixture.componentInstance.emojiOpen()).toBe(true);
        expect(fixture.componentInstance.draft()).toBe('');
    });

    it('runs a slash-only draft on Enter instead of sending', () => {
        const sendSpy = vi.fn().mockName('send');
        fixture.componentInstance.send.subscribe(sendSpy);
        fixture.componentInstance.onInput({ target: { value: '/emoji' } } as unknown as Event);
        fixture.detectChanges();

        fixture.componentInstance.onKeydown(new KeyboardEvent('keydown', { key: 'Enter' }));

        expect(sendSpy).not.toHaveBeenCalled();
        expect(fixture.componentInstance.emojiOpen()).toBe(true);
        expect(fixture.componentInstance.draft()).toBe('');
    });

    it('emits attachmentRemoved with a stable key when dismiss is clicked', () => {
        const removed = vi.fn().mockName('attachmentRemoved');
        fixture.componentRef.setInput('attachments', [
            {
                clientKey: 'pending-1',
                file: new File(['x'], 'notes.txt', { type: 'text/plain' }),
                isUploading: false,
            },
        ]);
        fixture.componentInstance.attachmentRemoved.subscribe(removed);
        fixture.detectChanges();

        const removeBtn = fixture.nativeElement.querySelector('.composer__attachment-remove') as HTMLButtonElement;
        removeBtn.click();

        expect(removed).toHaveBeenCalledWith('pending-1');
    });

    it('loads gallery images when Add from Gallery is chosen', () => {
        const plus = fixture.nativeElement.querySelector('.composer__plus') as HTMLButtonElement;
        plus.click();
        fixture.detectChanges();

        const galleryBtn = Array.from(fixture.nativeElement.querySelectorAll('.composer__plus-menu button')).find((b): b is HTMLButtonElement => b instanceof HTMLButtonElement && !!b.textContent?.includes('Add from Gallery'));
        expect(galleryBtn).toBeTruthy();
        galleryBtn!.click();
        fixture.detectChanges();

        expect(imageGallery.listImages).toHaveBeenCalledWith(1, 30);
        expect(fixture.componentInstance.galleryOpen()).toBe(true);
    });

    it('toggles composer expand state when the resizer is clicked', () => {
        const resizer = fixture.nativeElement.querySelector('.composer__resizer') as HTMLButtonElement;
        expect(resizer).toBeTruthy();
        expect(fixture.componentInstance.inputExpanded()).toBe(false);
        expect(resizer.classList.contains('composer__resizer--active')).toBe(false);

        fixture.componentInstance.onResizerMouseDown(new MouseEvent('mousedown', { clientY: 100 }));
        document.dispatchEvent(new MouseEvent('mouseup'));
        fixture.detectChanges();

        expect(fixture.componentInstance.inputExpanded()).toBe(true);
        expect(resizer.classList.contains('composer__resizer--active')).toBe(true);

        fixture.componentInstance.onResizerMouseDown(new MouseEvent('mousedown', { clientY: 100 }));
        document.dispatchEvent(new MouseEvent('mouseup'));
        fixture.detectChanges();

        expect(fixture.componentInstance.inputExpanded()).toBe(false);
        expect(resizer.classList.contains('composer__resizer--active')).toBe(false);
    });

    it('sets manual height when the resizer is dragged', () => {
        const wrap = fixture.nativeElement.querySelector('.composer__unified-wrap') as HTMLElement;
        Object.defineProperty(wrap, 'offsetHeight', { value: 100, configurable: true });

        fixture.componentInstance.onResizerMouseDown(new MouseEvent('mousedown', { clientY: 200 }));
        document.dispatchEvent(new MouseEvent('mousemove', { clientY: 180 }));
        document.dispatchEvent(new MouseEvent('mouseup'));
        fixture.detectChanges();

        expect(fixture.componentInstance.inputManualHeight()).toBe(120);
        expect(wrap.style.height).toBe('120px');
    });

    it('removes resize drag listeners on destroy', () => {
        const wrap = fixture.nativeElement.querySelector('.composer__unified-wrap') as HTMLElement;
        Object.defineProperty(wrap, 'offsetHeight', { value: 100, configurable: true });

        fixture.componentInstance.onResizerMouseDown(new MouseEvent('mousedown', { clientY: 200 }));
        document.dispatchEvent(new MouseEvent('mousemove', { clientY: 180 }));

        const setSpy = vi.spyOn(fixture.componentInstance.inputManualHeight, 'set');
        fixture.destroy();
        setSpy.mockClear();

        document.dispatchEvent(new MouseEvent('mousemove', { clientY: 160 }));
        expect(setSpy).not.toHaveBeenCalled();
    });

    it('shows auto mode label with current effective mode in plus menu', () => {
        fixture.componentRef.setInput('isAutoMood', true);
        fixture.componentRef.setInput('activeMoodId', 'mode-writing');
        fixture.detectChanges();

        expect(fixture.componentInstance.modeMenuLabel()).toBe('Mode · Auto (Writing)');
        expect(moodService.listMoods).toHaveBeenCalled();
    });

    it('shows locked mode label in plus menu', () => {
        fixture.componentRef.setInput('isAutoMood', false);
        fixture.componentRef.setInput('activeMoodId', 'mode-review');
        fixture.detectChanges();

        expect(fixture.componentInstance.modeMenuLabel()).toBe('Mode · Code Review');
    });

    it('marks auto row selected and shows current badge on effective mode in popover', () => {
        fixture.componentRef.setInput('isAutoMood', true);
        fixture.componentRef.setInput('activeMoodId', 'mode-writing');
        fixture.detectChanges();

        fixture.componentInstance.openModePicker();
        fixture.detectChanges();

        const rows = Array.from(fixture.nativeElement.querySelectorAll('.composer__mode-popover .composer__skill-row')) as HTMLButtonElement[];
        const autoRow = rows[0];
        const writingRow = rows.find(row => {
            const name = row.querySelector('.composer__skill-name');
            return name?.textContent?.trim() === 'Writing';
        });

        expect(autoRow?.classList.contains('composer__skill-row--selected')).toBe(true);
        expect(autoRow?.getAttribute('aria-selected')).toBe('true');
        expect(autoRow?.textContent).toContain('Currently: Writing');
        expect(writingRow?.classList.contains('composer__skill-row--selected')).toBe(false);
        expect(writingRow?.textContent).toContain('Current');
    });

    it('marks locked mode row selected in popover', () => {
        fixture.componentRef.setInput('isAutoMood', false);
        fixture.componentRef.setInput('activeMoodId', 'mode-review');
        fixture.detectChanges();

        fixture.componentInstance.openModePicker();
        fixture.detectChanges();

        const rows = Array.from(fixture.nativeElement.querySelectorAll('.composer__mode-popover .composer__skill-row')) as HTMLButtonElement[];
        const autoRow = rows[0];
        const reviewRow = rows.find(row => {
            const name = row.querySelector('.composer__skill-name');
            return name?.textContent?.trim() === 'Code Review';
        });

        expect(autoRow?.classList.contains('composer__skill-row--selected')).toBe(false);
        expect(autoRow?.getAttribute('aria-selected')).toBe('false');
        expect(reviewRow?.classList.contains('composer__skill-row--selected')).toBe(true);
        expect(reviewRow?.getAttribute('aria-selected')).toBe('true');
    });
    it('renders thread reference chips with overflow and emits remove/clear', () => {
        const threads = ['A', 'B', 'C', 'D', 'E', 'F', 'G'].map(name => ({
            id: `t-${name}`, user_id: 'u-1', name: `Thread ${name}`, created_at: '', updated_at: '',
        }));
        fixture.componentRef.setInput('threadReferences', threads);
        fixture.detectChanges();
        const root: HTMLElement = fixture.nativeElement;

        expect(root.querySelector('.composer__thread-refs-label')?.textContent).toContain('7 threads added');
        expect(root.querySelectorAll('.composer__thread-chip').length).toBe(7);
        expect(root.querySelectorAll('.composer__thread-chip--hide-mobile').length).toBe(4);
        expect(root.querySelectorAll('.composer__thread-chip--hide-desktop').length).toBe(2);
        expect(root.querySelector('.composer__thread-more--mobile')?.textContent).toContain('+4');
        expect(root.querySelector('.composer__thread-more--desktop')?.textContent).toContain('+2');

        const removed = vi.fn().mockName('threadReferenceRemoved');
        const cleared = vi.fn().mockName('threadReferencesCleared');
        fixture.componentInstance.threadReferenceRemoved.subscribe(removed);
        fixture.componentInstance.threadReferencesCleared.subscribe(cleared);
        (root.querySelector('.composer__thread-chip .composer__skill-chip-remove') as HTMLButtonElement).click();
        (root.querySelector('.composer__thread-refs-clear') as HTMLButtonElement).click();

        expect(removed).toHaveBeenCalledWith('t-A');
        expect(cleared).toHaveBeenCalled();
    });

    it('shows a singular label and no chips row when there are no thread references', () => {
        fixture.componentRef.setInput('threadReferences', [
            { id: 't-1', user_id: 'u-1', name: 'Only one', created_at: '', updated_at: '' },
        ]);
        fixture.detectChanges();
        expect(fixture.nativeElement.querySelector('.composer__thread-refs-label')?.textContent).toContain('1 thread added');

        fixture.componentRef.setInput('threadReferences', []);
        fixture.detectChanges();
        expect(fixture.nativeElement.querySelector('.composer__thread-refs')).toBeNull();
    });
    describe('thread picker', () => {
        const options = [
            { id: 'active', user_id: 'u', name: 'Active chat', created_at: '', updated_at: '' },
            { id: 't-1', user_id: 'u', name: 'Travel Planning', is_favorite: true, created_at: '', updated_at: '' },
            { id: 't-2', user_id: 'u', name: 'Cooking', created_at: '', updated_at: '' },
            { id: 't-3', user_id: 'u', name: 'Old one', archived: true, created_at: '', updated_at: '' },
        ];

        beforeEach(() => {
            fixture.componentRef.setInput('chatId', 'active');
            fixture.componentRef.setInput('threadOptions', options);
            fixture.detectChanges();
        });

        it('lists other active threads only and filters by search and starred', () => {
            fixture.componentInstance.openThreadPicker();
            fixture.detectChanges();
            const root: HTMLElement = fixture.nativeElement;
            const names = () => [...root.querySelectorAll('.composer__thread-popover .composer__thread-name')].map(n => n.textContent?.trim());

            expect(names()).toEqual(['Travel Planning', 'Cooking']);

            fixture.componentInstance.threadStarredOnly.set(true);
            fixture.detectChanges();
            expect(names()).toEqual(['Travel Planning']);

            fixture.componentInstance.threadStarredOnly.set(false);
            fixture.componentInstance.threadFilter.set('cook');
            fixture.detectChanges();
            expect(names()).toEqual(['Cooking']);
        });

        it('renders count header, personality avatar, age and a star for starred threads', () => {
            fixture.componentRef.setInput('personalities', [
                { id: 'p-1', name: 'Lola Tarsier', accent_color: '#10B981' },
            ]);
            fixture.componentRef.setInput('threadOptions', [
                { id: 't-1', user_id: 'u', name: 'Travel Planning', is_favorite: true, personality_id: 'p-1',
                  created_at: '', updated_at: '', last_message_time: new Date(Date.now() - 3 * 24 * 3_600_000).toISOString() },
                { id: 't-2', user_id: 'u', name: 'Cooking', personality_name: 'Mooey', created_at: '', updated_at: '' },
            ]);
            fixture.componentInstance.openThreadPicker();
            fixture.detectChanges();
            const root: HTMLElement = fixture.nativeElement;
            const rows = root.querySelectorAll('.composer__thread-popover .composer__thread-row');

            expect(root.querySelector('.composer__thread-count')?.textContent).toContain('0 selected');
            expect(rows[0].querySelector('.composer__thread-avatar')?.textContent?.trim()).toBe('LT');
            expect(rows[0].querySelector('.composer__thread-age')?.textContent).toContain('3 days ago');
            expect(rows[0].querySelector('.composer__thread-star')).not.toBeNull();
            expect(rows[1].querySelector('.composer__thread-avatar')?.textContent?.trim()).toBe('MO');
            expect(rows[1].querySelector('.composer__thread-star')).toBeNull();
        });

        it('sits directly under the composer body so it matches the composer width', () => {
            fixture.componentInstance.openThreadPicker();
            fixture.detectChanges();

            const popover = fixture.nativeElement.querySelector('.composer__thread-popover') as HTMLElement;
            expect(popover.parentElement?.classList.contains('composer__body')).toBe(true);
        });

        it('emits a toggle when a thread is picked and marks referenced rows', () => {
            const toggled = vi.fn().mockName('threadReferenceToggled');
            fixture.componentInstance.threadReferenceToggled.subscribe(toggled);
            fixture.componentRef.setInput('threadReferences', [options[2]]);
            fixture.componentInstance.openThreadPicker();
            fixture.detectChanges();
            const root: HTMLElement = fixture.nativeElement;
            const rows = root.querySelectorAll<HTMLButtonElement>('.composer__thread-popover .composer__thread-row');

            expect(rows[1].getAttribute('aria-selected')).toBe('true');
            rows[0].click();

            expect(toggled).toHaveBeenCalledWith(options[1]);
        });

        it('disables Done until a thread is selected and Cancel clears the references', () => {
            const cleared = vi.fn().mockName('threadReferencesCleared');
            fixture.componentInstance.threadReferencesCleared.subscribe(cleared);
            fixture.componentInstance.openThreadPicker();
            fixture.detectChanges();
            const root: HTMLElement = fixture.nativeElement;
            expect((root.querySelector('.composer__thread-done') as HTMLButtonElement).disabled).toBe(true);

            fixture.componentRef.setInput('threadReferences', [options[1]]);
            fixture.detectChanges();
            expect((root.querySelector('.composer__thread-done') as HTMLButtonElement).disabled).toBe(false);

            (root.querySelector('.composer__thread-cancel') as HTMLButtonElement).click();
            fixture.detectChanges();

            expect(cleared).toHaveBeenCalled();
            expect(root.querySelector('.composer__thread-popover')).toBeNull();
        });

        it('opens from the /thread slash command', () => {
            fixture.componentInstance.runCommand({ id: 'thread', label: 'Thread' });
            fixture.detectChanges();

            expect(fixture.nativeElement.querySelector('.composer__thread-popover')).not.toBeNull();
        });
    });
    describe('thread drag and drop', () => {
        const dragEvent = (type: string, types: string[], data: Record<string, string> = {}): DragEvent => {
            const event = new Event(type, { bubbles: true, cancelable: true }) as DragEvent;
            Object.defineProperty(event, 'dataTransfer', {
                value: { types, getData: (key: string) => data[key] ?? '', files: [], dropEffect: 'none' },
            });
            return event;
        };

        beforeEach(() => {
            fixture.componentRef.setInput('chatId', 'active');
            fixture.componentRef.setInput('threadOptions', [
                { id: 't-1', user_id: 'u', name: 'Travel Planning', created_at: '', updated_at: '' },
                { id: 'active', user_id: 'u', name: 'Active chat', created_at: '', updated_at: '' },
            ]);
            fixture.detectChanges();
        });

        it('highlights the composer while a thread is dragged over it', () => {
            const form: HTMLElement = fixture.nativeElement.querySelector('form.composer');
            form.dispatchEvent(dragEvent('dragover', ['text/thread-id']));
            fixture.detectChanges();

            expect(form.classList.contains('composer--thread-drop')).toBe(true);
            expect(fixture.nativeElement.textContent).toContain('Drop to add thread');
            expect(fixture.componentInstance.isDragOver()).toBe(false);

            form.dispatchEvent(dragEvent('dragleave', ['text/thread-id']));
            fixture.detectChanges();
            expect(form.classList.contains('composer--thread-drop')).toBe(false);
        });

        it('emits a toggle when a thread is dropped, ignoring the active thread and unknown ids', () => {
            const toggled = vi.fn().mockName('threadReferenceToggled');
            fixture.componentInstance.threadReferenceToggled.subscribe(toggled);
            const form: HTMLElement = fixture.nativeElement.querySelector('form.composer');

            form.dispatchEvent(dragEvent('drop', ['text/thread-id'], { 'text/thread-id': 't-1' }));
            form.dispatchEvent(dragEvent('drop', ['text/thread-id'], { 'text/thread-id': 'active' }));
            form.dispatchEvent(dragEvent('drop', ['text/thread-id'], { 'text/thread-id': 'missing' }));

            expect(toggled).toHaveBeenCalledTimes(1);
            expect(toggled.mock.calls[0][0].id).toBe('t-1');
        });

        it('does not remove an already-attached thread on drop and shows a timed notice', () => {
            vi.useFakeTimers();
            try {
                const toggled = vi.fn().mockName('threadReferenceToggled');
                fixture.componentInstance.threadReferenceToggled.subscribe(toggled);
                fixture.componentRef.setInput('threadReferences', [
                    { id: 't-1', user_id: 'u', name: 'Travel Planning', created_at: '', updated_at: '' },
                ]);
                fixture.detectChanges();
                const form: HTMLElement = fixture.nativeElement.querySelector('form.composer');

                form.dispatchEvent(dragEvent('drop', ['text/thread-id'], { 'text/thread-id': 't-1' }));
                fixture.detectChanges();

                expect(toggled).not.toHaveBeenCalled();
                expect(fixture.nativeElement.querySelector('.composer__thread-notice')?.textContent).toContain('Thread already added.');

                vi.advanceTimersByTime(2000);
                fixture.detectChanges();
                expect(fixture.nativeElement.querySelector('.composer__thread-notice')).toBeNull();
            } finally {
                vi.useRealTimers();
            }
        });

        it('keeps the file drop path for non-thread drags', () => {
            const form: HTMLElement = fixture.nativeElement.querySelector('form.composer');
            form.dispatchEvent(dragEvent('dragover', ['Files']));

            expect(fixture.componentInstance.isDragOver()).toBe(true);
            expect(fixture.componentInstance.isThreadDragOver()).toBe(false);
        });
    });
});
