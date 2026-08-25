import { Routes } from '@angular/router';
import { authGuard, guestGuard } from './core/guards/auth.guard';
import { personalitySetupGuard } from './core/guards/personality-setup.guard';
import { AppLayoutComponent } from './layout/app-layout.component';
import { privateChildRoutes } from './extensions/private-routes';
import { authChildRoutes } from './extensions/auth-routes';

export const routes: Routes = [
  {
    path: 'auth',
    canActivate: [guestGuard],
    // Auth screens (local sign-in/registration), defined in the extensions
    // module and assigned as this route's children.
    children: authChildRoutes
  },
  {
    path: '',
    component: AppLayoutComponent,
    canActivateChild: [authGuard, personalitySetupGuard],
    children: [
      {
        path: '',
        redirectTo: 'chat',
        pathMatch: 'full'
      },
      {
        path: 'dashboard',
        loadComponent: () => import('./features/dashboard/dashboard.component')
          .then(m => m.DashboardComponent)
      },
      {
        path: 'chat',
        children: [
          {
            path: '',
            loadComponent: () => import('./features/chat/chat-page.component')
              .then(m => m.ChatPageComponent)
          },
          {
            path: ':id',
            loadComponent: () => import('./features/chat/chat-page.component')
              .then(m => m.ChatPageComponent)
          }
        ]
      },
      {
        path: 'memories',
        children: [
          {
            path: '',
            loadComponent: () => import('./features/memory/memories-page.component')
              .then(m => m.MemoriesPageComponent)
          },
          {
            path: 'merge-history',
            loadComponent: () => import('./features/memory/memory-merge-history-page.component')
              .then(m => m.MemoryMergeHistoryPageComponent)
          },
          {
            path: 'compaction-log',
            loadComponent: () => import('./features/memory/compaction-log-page.component')
              .then(m => m.CompactionLogPageComponent)
          },
          {
            path: ':id',
            loadComponent: () => import('./features/memory/memory-detail-page.component')
              .then(m => m.MemoryDetailPageComponent)
          }
        ]
      },
      {
        path: 'memory',
        redirectTo: '/memories',
        pathMatch: 'full',
      },
      {
        path: 'memory/:id',
        redirectTo: '/memories/:id',
      },
      {
        path: 'personality',
        children: [
          {
            path: '',
            loadComponent: () => import('./features/personality/personalities-page.component')
              .then(m => m.PersonalitiesPageComponent)
          },
          {
            path: 'getting-started',
            loadComponent: () => import('./features/personality/personality-getting-started/personality-getting-started.component')
              .then(m => m.PersonalityGettingStartedComponent)
          },
          {
            path: 'generate',
            loadComponent: () => import('./features/personality/personality-generate/personality-generate.component')
              .then(m => m.PersonalityGenerateComponent)
          },
          {
            path: ':id',
            loadComponent: () => import('./features/personality/detail/personality-detail-page.component')
              .then(m => m.PersonalityDetailPageComponent)
          }
        ]
      },
      {
        path: 'personalities',
        redirectTo: '/personality',
        pathMatch: 'full',
      },
      {
        path: 'skills',
        children: [
          {
            path: '',
            loadComponent: () => import('./features/ritual/rituals-page.component')
              .then(m => m.RitualsPageComponent)
          },
          {
            path: ':id',
            loadComponent: () => import('./features/ritual/ritual-detail-page.component')
              .then(m => m.RitualDetailPageComponent)
          }
        ]
      },
      {
        path: 'rituals',
        redirectTo: '/skills',
        pathMatch: 'full',
      },
      {
        path: 'rituals/:id',
        redirectTo: '/skills/:id',
      },
      {
        path: 'ritual',
        redirectTo: '/skills',
        pathMatch: 'full',
      },
      {
        path: 'ritual/:id',
        redirectTo: '/skills/:id',
      },
      {
        path: 'mode',
        children: [
          {
            path: '',
            loadComponent: () => import('./features/mood/mood-list.component')
              .then(m => m.MoodListComponent)
          },
          {
            path: ':id',
            loadComponent: () => import('./features/mood/mood-list.component')
              .then(m => m.MoodListComponent)
          }
        ]
      },
      {
        path: 'agent-jobs',
        children: [
          {
            path: '',
            loadComponent: () => import('./features/agent-job/jobs-page.component')
              .then(m => m.JobsPageComponent)
          },
          {
            path: ':id',
            loadComponent: () => import('./features/agent-job/job-detail-page.component')
              .then(m => m.JobDetailPageComponent)
          }
        ]
      },
      {
        path: 'profile',
        loadComponent: () => import('./features/profile/profile.component')
          .then(m => m.ProfileComponent)
      },
      // Privately-maintained routes (billing/subscription/usage, etc.) compose
      // in here from the overlay; empty in the open-source build.
      ...privateChildRoutes,
      {
        path: 'integrations',
        loadComponent: () => import('./features/integrations/integrations.component')
          .then(m => m.IntegrationsComponent)
      },
      {
        path: 'gallery',
        loadComponent: () => import('./features/gallery/gallery-page.component')
          .then(m => m.GalleryPageComponent)
      },
      {
        path: 'image-gallery',
        redirectTo: '/gallery',
        pathMatch: 'full'
      }
    ]
  },
  {
    path: '**',
    redirectTo: '/chat'
  }
];
