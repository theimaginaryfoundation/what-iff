import { Component, inject, ChangeDetectionStrategy } from '@angular/core';

import { Router } from '@angular/router';
@Component({
  selector: 'app-personality-getting-started',
  standalone: true,
  imports: [],
  templateUrl: './personality-getting-started.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  styleUrls: ['./personality-getting-started.component.scss'],
})
export class PersonalityGettingStartedComponent {
  private router = inject(Router);

  goToGenerate(): void {
    this.router.navigate(['/personality/generate']);
  }

  goToBuildOwn(): void {
    this.router.navigate(['/personality'], { queryParams: { create: '1' } });
  }
}
