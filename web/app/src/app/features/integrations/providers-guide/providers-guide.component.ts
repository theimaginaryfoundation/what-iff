import { ChangeDetectionStrategy, Component, OnInit, computed, inject, signal } from '@angular/core';
import { RouterLink } from '@angular/router';

import { ProviderUsage } from '../../../core/models/provider-usage.model';
import { ProviderUsageService } from '../../../core/services/provider-usage.service';
import { vendorLabel } from '../../../core/utils/provider-vendor';

/**
 * Answers "what is my key being spent on, and by which model".
 *
 * Everything shown here comes from the server, which builds it from the same
 * constants the calls use. Writing the list out here instead would create a
 * second copy to keep in sync — which is exactly how the architecture summary
 * came to name the wrong archival model for months.
 */
@Component({
  selector: 'app-providers-guide',
  standalone: true,
  imports: [RouterLink],
  templateUrl: './providers-guide.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
})
export class ProvidersGuideComponent implements OnInit {
  private usageService = inject(ProviderUsageService);

  usage = signal<ProviderUsage[]>([]);
  isLoading = signal(false);
  errorMessage = signal('');

  /** Required providers first; the rest keep server order. */
  ordered = computed(() => [...this.usage()].sort((a, b) => Number(b.required ?? false) - Number(a.required ?? false)));

  ngOnInit(): void {
    this.isLoading.set(true);
    this.usageService.list().subscribe({
      next: usage => {
        this.usage.set(usage);
        this.isLoading.set(false);
      },
      error: () => {
        this.errorMessage.set('Could not load what each provider is used for.');
        this.isLoading.set(false);
      },
    });
  }

  label(provider: string): string {
    return vendorLabel(provider);
  }
}
