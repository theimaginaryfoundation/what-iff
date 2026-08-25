import { CommonModule } from '@angular/common';
import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';
import { FormsModule } from '@angular/forms';

import { GalleryDateRange, GalleryFilters, GallerySourceFilter } from '../helpers/gallery-vm.helpers';

export interface GalleryPersonalityOption {
  id: string;
  name: string;
}

@Component({
  selector: 'app-gallery-filter-bar',
  standalone: true,
  imports: [CommonModule, FormsModule],
  templateUrl: './gallery-filter-bar.component.html',
  styleUrl: './gallery-filter-bar.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class GalleryFilterBarComponent {
  readonly filters = input.required<GalleryFilters>();
  readonly availableSources = input<GallerySourceFilter[]>(['all']);
  readonly personalities = input<GalleryPersonalityOption[]>([]);
  readonly loading = input(false);

  readonly filtersChanged = output<Partial<GalleryFilters>>();
  readonly resetFilters = output<void>();
  readonly openImport = output<void>();

  onQueryChange(query: string): void {
    this.filtersChanged.emit({ query });
  }

  onSourceChange(value: string): void {
    this.filtersChanged.emit({ source: (value as GallerySourceFilter) ?? 'all' });
  }

  onPersonalityChange(value: string): void {
    this.filtersChanged.emit({ personalityId: value || 'all' });
  }

  onDateRangeChange(value: string): void {
    this.filtersChanged.emit({ dateRange: (value as GalleryDateRange) ?? 'any' });
  }
}
