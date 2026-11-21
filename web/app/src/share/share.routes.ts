import { Routes } from '@angular/router';

export const shareRoutes: Routes = [
  { path: 's/:key', loadComponent: () => import('./shared-page.component').then(m => m.SharedPageComponent) },
  { path: '**', loadComponent: () => import('./share-not-found.component').then(m => m.ShareNotFoundComponent) },
];
