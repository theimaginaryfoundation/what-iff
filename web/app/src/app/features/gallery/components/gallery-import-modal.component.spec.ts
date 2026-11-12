import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';

import { GalleryImportModalComponent } from './gallery-import-modal.component';

describe('GalleryImportModalComponent', () => {
  let fixture: ComponentFixture<GalleryImportModalComponent>;
  let component: GalleryImportModalComponent;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [GalleryImportModalComponent],
      providers: [provideZonelessChangeDetection(), provideHttpClient(withXhr())],
    }).compileComponents();

    fixture = TestBed.createComponent(GalleryImportModalComponent);
    component = fixture.componentInstance;
    fixture.componentRef.setInput('open', true);
    fixture.componentRef.setInput('personalities', [{ id: 'pers-1', name: 'Aster' }]);
    fixture.detectChanges();
  });

  it('emits file import request when valid', () => {
    const emitSpy = vi.spyOn(component.submitFile, 'emit').mockReturnValue(undefined);
    component.setScope('personality');
    component.personalityId.set('pers-1');
    component.title.set('My image');
    component.selectedFile.set(new File(['x'], 'image.png', { type: 'image/png' }));

    component.onSubmitFile();

    expect(emitSpy).toHaveBeenCalled();
  });

  it('takes diagrams and Visio drawings, and refuses what the upload does not take', () => {
    expect(component.accept).toContain('.drawio');
    expect(component.accept).toContain('.vsdx');
    const choose = (file: File) => component.onFileInput({ target: { files: [file] } } as unknown as Event);

    choose(new File(['<mxfile/>'], 'flow.drawio'));
    expect(component.selectedFile()?.name).toBe('flow.drawio');
    expect(component.title()).toBe('flow.drawio');

    choose(new File(['x'], 'tool.exe'));
    expect(component.selectedFile()).toBeNull();
    expect(component.error()).toContain('not a supported file type');
  });

  it('does not emit file import when required values are missing', () => {
    const emitSpy = vi.spyOn(component.submitFile, 'emit').mockReturnValue(undefined);

    component.onSubmitFile();

    expect(emitSpy).not.toHaveBeenCalled();
  });

  it('resets stale form state when reopened', () => {
    component.title.set('Old title');
    component.description.set('Old description');
    component.scope.set('personality');
    component.personalityId.set('pers-1');
    component.selectedFile.set(new File(['x'], 'old.png', { type: 'image/png' }));
    component.error.set('Old error');

    fixture.componentRef.setInput('open', false);
    fixture.detectChanges();
    fixture.componentRef.setInput('open', true);
    fixture.detectChanges();

    expect(component.title()).toBe('');
    expect(component.description()).toBe('');
    expect(component.scope()).toBe('global');
    expect(component.personalityId()).toBe('');
    expect(component.selectedFile()).toBeNull();
    expect(component.error()).toBeNull();
  });
});
