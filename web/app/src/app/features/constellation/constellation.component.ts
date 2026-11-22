import { ChangeDetectionStrategy, Component, ElementRef, computed, inject, signal, viewChild } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { RouterLink } from '@angular/router';
import { ButtonComponent } from '../../shared/ui/button/button.component';
import { IconButtonComponent } from '../../shared/ui/icon-button/icon-button.component';
import { ModalComponent } from '../../shared/ui/modal/modal.component';
import { OverlayComponent } from '../../shared/ui/overlay/overlay.component';
import { XIconComponent } from '../../shared/ui/icons/icons';
import { LEARNING_GUIDES, LEARNING_OBJECTIVES } from './constellation.data';
import { ConstellationMapComponent } from './constellation-map.component';
import { ConstellationProgressService } from './constellation-progress.service';
import { objectiveReady } from './constellation-progress';
import { ConstellationService } from './constellation.service';

@Component({
  selector: 'app-constellation',
  standalone: true,
  imports: [
    FormsModule,
    RouterLink,
    ButtonComponent,
    IconButtonComponent,
    ModalComponent,
    OverlayComponent,
    XIconComponent,
    ConstellationMapComponent,
  ],
  templateUrl: './constellation.component.html',
  styleUrl: './constellation.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ConstellationComponent {
  readonly entry = inject(ConstellationService);
  readonly learning = inject(ConstellationProgressService);
  readonly guides = LEARNING_GUIDES;
  readonly nodes = LEARNING_OBJECTIVES;
  readonly selectedId = signal<string | null>(null);
  readonly selected = computed(() => this.nodes.find(node => node.id === this.selectedId()));
  readonly selectedReady = computed(() => {
    const selected = this.selected();
    return selected ? objectiveReady(selected, this.learning.completed()) : false;
  });
  readonly selectedGuides = computed(() => this.guides.filter(guide => this.selected()?.guides.includes(guide.slug)));
  readonly guideLibraryOpen = signal(false);
  readonly feedback = signal('');
  readonly map = viewChild(ConstellationMapComponent);
  readonly heading = viewChild<ElementRef<HTMLElement>>('objectiveHeading');
  note = '';

  select(id: string): void {
    if (!this.nodes.some(node => node.id === id)) return;
    this.selectedId.set(id);
    this.note = this.learning.progress()[id]?.note ?? '';
    this.feedback.set('');
    setTimeout(() => this.heading()?.nativeElement.focus({ preventScroll: true }), 0);
  }

  closeObjective(): void {
    const previous = this.selectedId();
    this.selectedId.set(null);
    if (previous) this.map()?.focusNode(previous);
  }

  dismiss(): void {
    if (this.guideLibraryOpen()) this.guideLibraryOpen.set(false);
    else if (this.selectedId()) this.closeObjective();
    else this.entry.close();
  }

  complete(): void {
    const id = this.selectedId();
    if (!id) return;
    const completed = this.learning.complete(id, this.note);
    this.feedback.set(
      completed ? 'Objective complete. The next milestone is ready.' : 'Describe the result you checked in at least 12 characters.',
    );
  }

  nextObjective(): void {
    const next = this.nodes.find(node => !this.learning.completed().has(node.id) && objectiveReady(node, this.learning.completed()));
    if (next) {
      this.map()?.centerOn(next.id);
      this.select(next.id);
    }
  }

  origin(): void {
    this.closeObjective();
    this.map()?.recenter();
  }
  titleFor(id: string): string {
    return this.nodes.find(node => node.id === id)?.title ?? id;
  }
  guideUrl(slug: string): string {
    return `https://whatiff.chat/guides/${slug}.html`;
  }
  zoomPercent(): number {
    return Math.round((this.map()?.zoom() ?? 0.95) * 100);
  }
}
