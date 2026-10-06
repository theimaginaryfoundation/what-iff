import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { of } from 'rxjs';

import { ChatService } from '../../../core/services/chat.service';
import { ConfirmationService } from '../../../core/services/confirmation.service';
import { ImageDetailModalComponent } from './image-detail-modal.component';
import { GalleryTileVm } from '../helpers/gallery-vm.helpers';

const sampleTile: GalleryTileVm = {
    id: 'img-1',
    name: 'cute_fox.png',
    createdAt: '2026-01-01T00:00:00Z',
    personalityId: null,
    personalityName: null,
    personalityNames: [],
    source: 'uploaded',
    sourceLabel: 'Imported',
    folder: '',
    isFile: false,
  badge: '',
  thumbnailUrl: '/thumb',
    fullUrl: '/full',
};

describe('ImageDetailModalComponent', () => {
    let fixture: ComponentFixture<ImageDetailModalComponent>;
    let component: ImageDetailModalComponent;

    beforeEach(async () => {
        await TestBed.configureTestingModule({
            imports: [ImageDetailModalComponent],
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                {
                    provide: ChatService,
                    useValue: {
                        listChats: () => of({ results: [{ id: 'chat-1', name: 'Thread A' }] }),
                    },
                },
                {
                    provide: ConfirmationService,
                    useValue: {
                        alert: () => Promise.resolve(),
                    },
                },
            ],
        }).compileComponents();

        fixture = TestBed.createComponent(ImageDetailModalComponent);
        component = fixture.componentInstance;
        fixture.componentRef.setInput('open', true);
        fixture.componentRef.setInput('image', sampleTile);
        fixture.detectChanges();
    });

    describe('folder', () => {
        const folderRow = () => document.querySelector('.image-detail-modal__folder') as HTMLElement;

        it('says where the image is, and calls the top level the top level', () => {
            expect(component.folderName()).toBe('Top level');
            expect(folderRow().textContent).toContain('Top level');

            fixture.componentRef.setInput('image', { ...sampleTile, folder: 'charts/oura' });
            fixture.detectChanges();

            expect(component.folderName()).toBe('charts / oura');
            expect(folderRow().textContent).toContain('charts / oura');
        });

        it('offers Move to… as a link that asks to move this image', () => {
            const spy = vi.spyOn(component.moveToFolder, 'emit').mockReturnValue(undefined);
            const link = folderRow().querySelector('.image-detail-modal__folder-move') as HTMLButtonElement;

            expect(link.textContent?.trim()).toBe('Move to…');
            link.click();

            expect(spy).toHaveBeenCalledWith('img-1');
        });

        it('does nothing when there is no image', () => {
            const spy = vi.spyOn(component.moveToFolder, 'emit').mockReturnValue(undefined);
            fixture.componentRef.setInput('image', null);
            fixture.detectChanges();

            component.requestMove();

            expect(spy).not.toHaveBeenCalled();
        });
    });

    it('emits next when right arrow is pressed while open', () => {
        const spy = vi.spyOn(component.next, 'emit').mockReturnValue(undefined);

        component.onDocumentKeydown(new KeyboardEvent('keydown', { key: 'ArrowRight' }));

        expect(spy).toHaveBeenCalled();
    });

    it('does not emit navigation keys when closed', () => {
        const spy = vi.spyOn(component.previous, 'emit').mockReturnValue(undefined);
        fixture.componentRef.setInput('open', false);

        component.onDocumentKeydown(new KeyboardEvent('keydown', { key: 'ArrowLeft' }));

        expect(spy).not.toHaveBeenCalled();
    });

    it('emits rename with extension preserved', () => {
        const spy = vi.spyOn(component.rename, 'emit').mockReturnValue(undefined);
        component.startRename();
        component.renameDraft.set('fox');
        component.submitRename();
        expect(spy).toHaveBeenCalledWith({ id: 'img-1', name: 'fox.png' });
    });

    it('shows base name without extension in display title', () => {
        expect(component.displayTitle()).toBe('cute_fox');
    });

    it('emits startNewChat with image id', () => {
        const spy = vi.spyOn(component.startNewChat, 'emit').mockReturnValue(undefined);
        component.onStartNewChat();
        expect(spy).toHaveBeenCalledWith('img-1');
    });

    it('clears transient modal state when reopened', () => {
        component.isRenaming.set(true);
        component.renameDraft.set('stale name');
        component.threadPickerOpen.set(true);
        component.recentThreads.set([{ id: 'chat-old', name: 'Old Thread' } as any]);
        component.threadsLoading.set(true);
        component.threadsLoadError.set(true);

        fixture.componentRef.setInput('open', false);
        fixture.detectChanges();
        fixture.componentRef.setInput('open', true);
        fixture.detectChanges();

        expect(component.isRenaming()).toBe(false);
        expect(component.renameDraft()).toBe('');
        expect(component.threadPickerOpen()).toBe(false);
        expect(component.recentThreads()).toEqual([]);
        expect(component.threadsLoading()).toBe(false);
        expect(component.threadsLoadError()).toBe(false);
    });
});
