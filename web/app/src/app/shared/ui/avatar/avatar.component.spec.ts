import { Component, ChangeDetectionStrategy } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { AvatarComponent } from './avatar.component';

@Component({
  standalone: true,
  imports: [AvatarComponent],
  changeDetection: ChangeDetectionStrategy.Eager,
  template:
    '<ui-avatar name="Filbolt Pottsworth" size="lg" accentColor="red" imageUrl="/portrait.png" [thumbnailCircle]="{ cx: 0.4, cy: 0.3, r: 0.25 }" />',
})
class AvatarHostComponent {}

describe('AvatarComponent', () => {
  let fixture: ComponentFixture<AvatarHostComponent>;

  beforeEach(async () => {
    await TestBed.configureTestingModule({ imports: [AvatarHostComponent], providers: [provideZonelessChangeDetection()] }).compileComponents();
    fixture = TestBed.createComponent(AvatarHostComponent);
    fixture.detectChanges();
  });

  it('renders image and applies thumbnail positioning styles', () => {
    const avatar = fixture.nativeElement.querySelector('.ui-avatar') as HTMLElement;
    const image = fixture.nativeElement.querySelector('img') as HTMLImageElement;
    expect(image).toBeTruthy();
    expect(image.style.objectPosition).toBe('40% 30%');
    expect(image.style.transform).toContain('scale(');
    expect(avatar.className).toContain('h-12');
  });
});
