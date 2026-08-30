import { ComponentFixture, TestBed } from '@angular/core/testing';
import { SecurityContext, provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { SANITIZE, provideMarkdown } from 'ngx-markdown';

import { MessageContentComponent } from './message-content.component';

describe('MessageContentComponent', () => {
    let fixture: ComponentFixture<MessageContentComponent>;
    let httpMock: HttpTestingController;

    beforeEach(async () => {
        await TestBed.configureTestingModule({
            imports: [MessageContentComponent],
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                provideHttpClientTesting(),
                provideMarkdown({
                    sanitize: { provide: SANITIZE, useValue: SecurityContext.HTML },
                }),
            ],
        }).compileComponents();

        fixture = TestBed.createComponent(MessageContentComponent);
        httpMock = TestBed.inject(HttpTestingController);
    });

    afterEach(() => {
        vi.restoreAllMocks();
        httpMock.verify();
    });

    it('renders extracted images from message content', () => {
        fixture.componentRef.setInput('content', '![Alt](https://example.com/a.png)');
        fixture.detectChanges();

        const img = fixture.nativeElement.querySelector('img') as HTMLImageElement;
        expect(img.alt).toBe('Alt');
    });

    it('sanitizes hostile markdown html', async () => {
        fixture.componentRef.setInput('content', '<script>alert(1)</script><img src="x" onerror="alert(2)">');
        fixture.detectChanges();
        await fixture.whenStable();

        const html = fixture.nativeElement.innerHTML as string;
        expect(html).not.toContain('<script');
        expect(html).not.toContain('onerror');
    });

    it('renders image attachments as constrained preview cards', () => {
        fixture.componentRef.setInput('attachments', [
            imageAttachment('image-1', 'first.png'),
            imageAttachment('image-2', 'second.png'),
        ]);

        fixture.detectChanges();

        const cards = fixture.nativeElement.querySelectorAll('.message-images__card');
        expect(cards.length).toBe(2);
        expect(fixture.nativeElement.textContent).toContain('first.png');
        expect(fixture.nativeElement.textContent).toContain('second.png');

        httpMock.expectOne(req => req.urlWithParams.includes('/image-gallery/image-1?size=thumbnail'))
            .flush(new Blob(['one'], { type: 'image/png' }));
        httpMock.expectOne(req => req.urlWithParams.includes('/image-gallery/image-2?size=thumbnail'))
            .flush(new Blob(['two'], { type: 'image/png' }));
    });

    it('downloads the full image attachment from the preview card', () => {
        vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:download');
        vi.spyOn(URL, 'revokeObjectURL').mockReturnValue(undefined);
        const click = vi.fn().mockName('click');
        const anchor = document.createElement('a');
        const createElement = document.createElement.bind(document);
        vi.spyOn(anchor, 'click').mockImplementation(click);
        vi.spyOn(document, 'createElement').mockImplementation(((tagName: string) => {
            return tagName === 'a' ? anchor : createElement(tagName);
        }) as typeof document.createElement);

        fixture.componentRef.setInput('attachments', [imageAttachment('image-1', 'first.png')]);
        fixture.detectChanges();
        httpMock.expectOne(req => req.urlWithParams.includes('/image-gallery/image-1?size=thumbnail'))
            .flush(new Blob(['thumb'], { type: 'image/png' }));

        const button = fixture.nativeElement.querySelector('.message-images__download') as HTMLButtonElement;
        button.click();

        httpMock.expectOne(req => req.urlWithParams.includes('/image-gallery/image-1?size=full'))
            .flush(new Blob(['full'], { type: 'image/png' }));

        expect(anchor.download).toBe('first.png');
        expect(anchor.href).toBe('blob:download');
        expect(click).toHaveBeenCalled();
        expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:download');
    });

    it('downloads a non-image attachment from the pill click', () => {
        vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:file-download');
        vi.spyOn(URL, 'revokeObjectURL').mockReturnValue(undefined);
        const click = vi.fn().mockName('click');
        const anchor = document.createElement('a');
        const createElement = document.createElement.bind(document);
        vi.spyOn(anchor, 'click').mockImplementation(click);
        vi.spyOn(document, 'createElement').mockImplementation(((tagName: string) => {
            return tagName === 'a' ? anchor : createElement(tagName);
        }) as typeof document.createElement);

        fixture.componentRef.setInput('attachments', [fileAttachment('doc-1', 'notes.txt', 'text/plain')]);
        fixture.detectChanges();

        const button = fixture.nativeElement.querySelector('button.rounded-full') as HTMLButtonElement;
        button.click();

        httpMock.expectOne(req => req.urlWithParams.includes('/file-attachment/doc-1'))
            .flush(new Blob(['notes'], { type: 'text/plain' }));

        expect(anchor.download).toBe('notes.txt');
        expect(anchor.href).toBe('blob:file-download');
        expect(click).toHaveBeenCalled();
        expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:file-download');
    });

    it('treats uppercase image MIME types as image attachments', () => {
        fixture.componentRef.setInput('attachments', [fileAttachment('img-1', 'cover.PNG', 'IMAGE/PNG')]);
        fixture.detectChanges();

        const previewCards = fixture.nativeElement.querySelectorAll('.message-images__card');
        expect(previewCards.length).toBe(1);
        expect(fixture.nativeElement.querySelector('button.rounded-full')).toBeNull();

        httpMock.expectOne(req => req.urlWithParams.includes('/image-gallery/img-1?size=thumbnail'))
            .flush(new Blob(['thumb'], { type: 'image/png' }));
    });

    it('copies sanitized html and preserves list markers in plain text', () => {
        fixture.detectChanges();
        const setData = vi.fn().mockName('setData');
        const preventDefault = vi.fn().mockName('preventDefault');
        const fragment = document.createDocumentFragment();
        const wrapper = document.createElement('div');
        wrapper.innerHTML = '<ul class="x"><li style="background:#000">First</li><li>Second</li></ul>';
        while (wrapper.firstChild) {
            fragment.appendChild(wrapper.firstChild);
        }
        const selection = {
            isCollapsed: false,
            rangeCount: 1,
            getRangeAt: () => ({ cloneContents: () => fragment } as Range),
            toString: () => 'First\nSecond',
        } as unknown as Selection;
        vi.spyOn(window, 'getSelection').mockReturnValue(selection);
        const event = {
            clipboardData: { setData },
            preventDefault,
        } as unknown as ClipboardEvent;

        fixture.componentInstance.onCopy(event);

        expect(preventDefault).toHaveBeenCalled();
        const plainTextPayload = vi.mocked(setData).mock.calls.find(args => args[0] === 'text/plain')?.[1] as string | undefined;
        expect(plainTextPayload).toContain('• First');
        expect(plainTextPayload).toContain('• Second');
        expect(setData).toHaveBeenCalledWith('text/html', '<ul><li>First</li><li>Second</li></ul>');
    });

    it('preserves nesting and numbering in plain-text list copy', () => {
        fixture.detectChanges();
        const setData = vi.fn().mockName('setData');
        const preventDefault = vi.fn().mockName('preventDefault');
        const fragment = document.createDocumentFragment();
        const wrapper = document.createElement('div');
        wrapper.innerHTML = '<ol start="3"><li>Top<ul><li>Nested</li></ul></li><li>Next</li></ol>';
        while (wrapper.firstChild) {
            fragment.appendChild(wrapper.firstChild);
        }
        const selection = {
            isCollapsed: false,
            rangeCount: 1,
            getRangeAt: () => ({ cloneContents: () => fragment } as Range),
            toString: () => 'Top\nNested\nNext',
        } as unknown as Selection;
        vi.spyOn(window, 'getSelection').mockReturnValue(selection);
        const event = { clipboardData: { setData }, preventDefault } as unknown as ClipboardEvent;

        fixture.componentInstance.onCopy(event);

        const plainTextPayload = vi.mocked(setData).mock.calls.find(args => args[0] === 'text/plain')?.[1] as string | undefined;
        expect(plainTextPayload).toContain('3. Top');
        expect(plainTextPayload).toContain('  • Nested');
        expect(plainTextPayload).toContain('4. Next');
        expect(setData).toHaveBeenCalledWith('text/html', '<ol start="3"><li>Top<ul><li>Nested</li></ul></li><li>Next</li></ol>');
    });

    it('converts task-list checkboxes to plain text markers', () => {
        fixture.detectChanges();
        const setData = vi.fn().mockName('setData');
        const preventDefault = vi.fn().mockName('preventDefault');
        const fragment = document.createDocumentFragment();
        const wrapper = document.createElement('div');
        wrapper.innerHTML = '<ul><li><input type="checkbox" checked> Done</li><li><input type="checkbox"> Todo</li></ul>';
        while (wrapper.firstChild) {
            fragment.appendChild(wrapper.firstChild);
        }
        const selection = {
            isCollapsed: false,
            rangeCount: 1,
            getRangeAt: () => ({ cloneContents: () => fragment } as Range),
            toString: () => 'Done\nTodo',
        } as unknown as Selection;
        vi.spyOn(window, 'getSelection').mockReturnValue(selection);
        const event = { clipboardData: { setData }, preventDefault } as unknown as ClipboardEvent;

        fixture.componentInstance.onCopy(event);

        const plainTextPayload = vi.mocked(setData).mock.calls.find(args => args[0] === 'text/plain')?.[1] as string | undefined;
        const normalized = (plainTextPayload ?? '').replace(/[ \t]+/g, ' ');
        expect(normalized).toContain('• [x] Done');
        expect(normalized).toContain('• [ ] Todo');
    });
});

function imageAttachment(id: string, name: string) {
    return fileAttachment(id, name, 'image/png');
}

function fileAttachment(id: string, name: string, fileType: string) {
    return {
        id,
        user_id: 'user-1',
        name,
        file_type: fileType,
        created_at: '',
    };
}
