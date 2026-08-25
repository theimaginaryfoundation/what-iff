import { Component, inject, OnDestroy, OnInit, ChangeDetectionStrategy } from '@angular/core';

import { ActivatedRoute } from '@angular/router';
import { Subscription } from 'rxjs';

import { ModeEditModalComponent } from './components/mode-edit-modal.component';
import { ModeGridComponent } from './components/mode-grid.component';
import { MoodListFacade } from './services/mood-list.facade';

@Component({
  selector: 'app-mood-list',
  standalone: true,
  imports: [ModeGridComponent, ModeEditModalComponent],
  providers: [MoodListFacade],
  templateUrl: './mood-list.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  styleUrls: ['./mood-list.component.scss'],
})
export class MoodListComponent implements OnInit, OnDestroy {
  readonly vm = inject(MoodListFacade);
  private route = inject(ActivatedRoute);
  private readonly subscriptions = new Subscription();

  ngOnInit(): void {
    this.vm.initialize();
    this.subscriptions.add(
      this.route.paramMap.subscribe(params => {
        this.vm.handleRouteMoodId(params.get('id'));
      }),
    );
  }

  ngOnDestroy(): void {
    this.subscriptions.unsubscribe();
    this.vm.destroy();
  }
}
