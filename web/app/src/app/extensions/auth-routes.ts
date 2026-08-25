import { Routes } from '@angular/router';

/**
 * Route definitions for the unauthenticated `/auth` area: local
 * username/password sign-in and account creation.
 *
 * Assigned directly as the guest-guarded `auth` route's `children` in
 * `app.routes.ts`.
 */
export const authChildRoutes: Routes = [
  {
    path: '',
    redirectTo: 'login',
    pathMatch: 'full'
  },
  {
    path: 'login',
    loadComponent: () => import('../features/auth/components/login/login.component')
      .then(m => m.LoginComponent)
  },
  {
    path: 'register',
    loadComponent: () => import('../features/auth/components/register/register.component')
      .then(m => m.RegisterComponent)
  }
];
