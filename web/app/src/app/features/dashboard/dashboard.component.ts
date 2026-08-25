import { Component, inject, signal, OnInit, ChangeDetectionStrategy } from '@angular/core';
import { RouterLink } from '@angular/router';

import { AuthService } from '../../core/services/auth.service';
import { UsageStats, UsagePeriod } from '../../core/models/user.model';

@Component({
  selector: 'app-dashboard',
  standalone: true,
  imports: [RouterLink],
  templateUrl: './dashboard.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  styleUrls: ['./dashboard.component.scss']
})
export class DashboardComponent implements OnInit {
  private authService: AuthService = inject(AuthService);

  currentUser = this.authService.currentUser;
  usageStats = signal<UsageStats | null>(null);
  selectedPeriod = signal<UsagePeriod>('month');
  isLoadingStats = signal(false);
  statsError = signal<string | null>(null);

  readonly periodOptions: Array<{value: UsagePeriod, label: string}> = [
    { value: 'day', label: 'Last 24 Hours' },
    { value: 'week', label: 'Last 7 Days' },
    { value: 'month', label: 'Last 30 Days' }
  ];

  ngOnInit(): void {
    this.loadUsageStats();
  }

  onPeriodChange(event: Event): void {
    const target = event.target as HTMLSelectElement;
    const period = target.value as UsagePeriod;
    this.selectedPeriod.set(period);
    this.loadUsageStats();
  }

  loadUsageStats(): void {
    this.isLoadingStats.set(true);
    this.statsError.set(null);
    
    this.authService.getUserUsageStats(this.selectedPeriod()).subscribe({
      next: (stats) => {
        this.usageStats.set(stats);
        this.isLoadingStats.set(false);
      },
      error: (error) => {
        this.statsError.set('Failed to load usage statistics');
        this.isLoadingStats.set(false);
        console.error('Error loading usage stats:', error);
      }
    });
  }

  getUserDisplayName(): string {
    const user = this.currentUser();
    if (user?.first_name && user?.last_name) {
      return `${user.first_name} ${user.last_name}`;
    }
    if (user?.first_name) {
      return user.first_name;
    }
    return user?.username || 'User';
  }

  formatJoinDate(dateString: string): string {
    const date = new Date(dateString);
    return date.toLocaleDateString('en-US', { 
      year: 'numeric', 
      month: 'long', 
      day: 'numeric' 
    });
  }

  getUserInitial(): string {
    const user = this.currentUser();
    if (!user || !user.username) {
      return 'U';
    }
    return user.username.charAt(0).toUpperCase();
  }

  formatNumber(num: number): string {
    return new Intl.NumberFormat().format(num);
  }
}
