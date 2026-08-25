---
description: 
globs: web/**/*
alwaysApply: false
---
# Personal Assistant UI Guidelines

## Project Overview
The Personal Assistant UI is built with Angular and follows modern best practices for scalable frontend applications. It interfaces with the Personal Assistant API to provide a user-friendly interface for content creation and management.

## Project Structure

```
src/
├── app/
│   ├── core/                   # Core modules, services, and components
│   │   ├── components/         # Shared components (navbar, layout)
│   │   ├── guards/             # Route guards
│   │   ├── interceptors/       # HTTP interceptors
│   │   ├── models/             # Data models
│   │   └── services/           # Core services
│   ├── features/               # Feature modules
│   │   ├── auth/               # Authentication feature
│   │   │   └── components/     # Login, register, profile components
│   │   └── dashboard/          # Dashboard feature
│   ├── app.ts                  # Root component
│   ├── app.html                # Root component template
│   ├── app.scss                # Root component styles
│   ├── app.routes.ts           # Application routes
│   └── app.config.ts           # Application configuration
├── assets/                     # Static assets
└── environments/               # Environment configuration
```

## Code Organization Guidelines

### Component Structure
Components must be split into separate files for TypeScript, HTML, and SCSS:

```
component-name/
├── component-name.component.ts
├── component-name.component.html
├── component-name.component.scss
└── component-name.component.spec.ts
```

### Component Definition
All components should be standalone and use the following structure:

```typescript
import { Component, OnInit } from '@angular/core';
import { CommonModule } from '@angular/common';

@Component({
  selector: 'app-example',
  standalone: true,
  imports: [CommonModule],
  templateUrl: './example.component.html',
  styleUrls: ['./example.component.scss']
})
export class ExampleComponent implements OnInit {
  // Component properties
  
  constructor() {}
  
  ngOnInit(): void {
    // Initialization logic
  }
  
  // Component methods
}
```

### File Naming Conventions
- Use kebab-case for file names: `user-profile.component.ts`
- Use camelCase for TypeScript variables, properties, and methods
- Use PascalCase for class names: `UserProfileComponent`
- Use kebab-case for selectors: `<app-user-profile>`

## Styling Guidelines

### SCSS Structure
- Use SCSS for all styling
- Component-specific styles should be in the component's `.scss` file
- Global styles should be in `src/styles.scss`
- Use Tailwind CSS utility classes for common styling needs
- Use the `:host` selector to style the component's host element
- Use BEM methodology for custom CSS classes

### Tailwind CSS
Tailwind CSS is used for utility-first styling. Flowbite components are available for more complex UI elements.

Example:
```html
<div class="flex items-center justify-between p-4 bg-white rounded-md shadow-sm">
  <h2 class="text-lg font-semibold text-gray-800">Title</h2>
  <button class="px-4 py-2 text-white bg-indigo-600 rounded-md hover:bg-indigo-700">
    Action
  </button>
</div>
```

## Service Guidelines

### Service Structure
Services should be organized in the appropriate directory based on their scope:
- Core services in `src/app/core/services/`
- Feature-specific services in `src/app/features/[feature]/services/`

### Service Definition
```typescript
import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Observable } from 'rxjs';
import { environment } from '@environments/environment';

@Injectable({
  providedIn: 'root'
})
export class ExampleService {
  private http = inject(HttpClient);
  private apiUrl = `${environment.apiUrl}/examples`;
  
  getAll(): Observable<Example[]> {
    return this.http.get<Example[]>(this.apiUrl);
  }
  
  getById(id: string): Observable<Example> {
    return this.http.get<Example>(`${this.apiUrl}/${id}`);
  }
  
  create(example: Example): Observable<Example> {
    return this.http.post<Example>(this.apiUrl, example);
  }
  
  update(id: string, example: Example): Observable<Example> {
    return this.http.put<Example>(`${this.apiUrl}/${id}`, example);
  }
  
  delete(id: string): Observable<void> {
    return this.http.delete<void>(`${this.apiUrl}/${id}`);
  }
}
```

## Model Guidelines

### Model Definition
Models should be defined as TypeScript interfaces in `src/app/core/models/` or in feature-specific `models` directories.

```typescript
export interface User {
  id: string;
  username: string;
  email: string;
  first_name?: string;
  last_name?: string;
  created_at: string;
  updated_at: string;
}
```

## Routing Guidelines

### Route Definition
Routes should be defined in `app.routes.ts` for main routes and in feature-specific modules for feature routes.

```typescript
import { Routes } from '@angular/router';
import { authGuard } from './core/guards/auth.guard';

export const routes: Routes = [
  {
    path: '',
    redirectTo: 'dashboard',
    pathMatch: 'full'
  },
  {
    path: 'auth',
    component: AuthLayoutComponent,
    children: [
      {
        path: 'login',
        loadComponent: () => import('./features/auth/components/login/login.component')
          .then(m => m.LoginComponent)
      }
    ]
  },
  {
    path: '',
    component: AppLayoutComponent,
    canActivate: [authGuard],
    children: [
      {
        path: 'dashboard',
        loadComponent: () => import('./features/dashboard/dashboard.component')
          .then(m => m.DashboardComponent)
      }
    ]
  }
];
```

## Authentication & Authorization

### Auth Guard
Route guards should be used to protect routes that require authentication:

```typescript
import { CanActivateFn, Router } from '@angular/router';
import { inject } from '@angular/core';
import { AuthService } from '../services/auth.service';

export const authGuard: CanActivateFn = (route, state) => {
  const authService = inject(AuthService);
  const router = inject(Router);
  
  if (authService.isLoggedIn) {
    return true;
  }
  
  router.navigate(['/auth/login'], { queryParams: { returnUrl: state.url } });
  return false;
};
```

### HTTP Interceptor
HTTP interceptors should be used to add authentication tokens to requests:

```typescript
import { HttpInterceptorFn } from '@angular/common/http';
import { inject } from '@angular/core';
import { AuthService } from '../services/auth.service';

export const authInterceptor: HttpInterceptorFn = (req, next) => {
  const authService = inject(AuthService);
  const accessToken = authService.getAccessToken();
  
  if (accessToken) {
    const authReq = req.clone({
      headers: req.headers.set('Authorization', `Bearer ${accessToken}`)
    });
    return next(authReq);
  }
  
  return next(req);
};
```

## Error Handling

### HTTP Error Handling
HTTP errors should be handled using RxJS operators:

```typescript
import { catchError, throwError } from 'rxjs';
import { HttpErrorResponse } from '@angular/common/http';

getResource(): Observable<Resource> {
  return this.http.get<Resource>(`${this.apiUrl}/resources`)
    .pipe(
      catchError(this.handleError)
    );
}

private handleError(error: HttpErrorResponse) {
  let errorMessage = 'An error occurred';
  
  if (error.error instanceof ErrorEvent) {
    // Client-side error
    errorMessage = `Error: ${error.error.message}`;
  } else {
    // Server-side error
    errorMessage = `Error Code: ${error.status}\nMessage: ${error.message}`;
  }
  
  console.error(errorMessage);
  return throwError(() => new Error(errorMessage));
}
```

## Environment Configuration

### Environment Files
Environment-specific configuration should be defined in environment files:

```typescript
// environment.ts (development)
export const environment = {
  production: false,
  apiUrl: 'http://localhost:8080/api'
};
```

## Unit Testing Guidelines

### Component Testing
Components should be tested using Angular's testing utilities:

```typescript
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { RouterTestingModule } from '@angular/router/testing';
import { Component } from './component';

describe('Component', () => {
  let component: Component;
  let fixture: ComponentFixture<Component>;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [
        RouterTestingModule,
        Component
      ]
    }).compileComponents();
    
    fixture = TestBed.createComponent(Component);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  it('should create', () => {
    expect(component).toBeTruthy();
  });
  
  it('should render title', () => {
    const title = 'Test Title';
    component.title = title;
    fixture.detectChanges();
    const compiled = fixture.nativeElement as HTMLElement;
    expect(compiled.querySelector('h1')?.textContent).toContain(title);
  });
});
```

### Service Testing
Services should be tested using Angular's testing utilities and HttpClientTestingModule:

```typescript
import { TestBed } from '@angular/core/testing';
import { HttpClientTestingModule, HttpTestingController } from '@angular/common/http/testing';
import { ExampleService } from './example.service';
import { Example } from '../models/example.model';

describe('ExampleService', () => {
  let service: ExampleService;
  let httpMock: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      imports: [HttpClientTestingModule],
      providers: [ExampleService]
    });
    
    service = TestBed.inject(ExampleService);
    httpMock = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    httpMock.verify();
  });

  it('should be created', () => {
    expect(service).toBeTruthy();
  });
  
  it('should return examples', () => {
    const mockExamples: Example[] = [
      { id: '1', name: 'Example 1' },
      { id: '2', name: 'Example 2' }
    ];
    
    service.getAll().subscribe(examples => {
      expect(examples.length).toBe(2);
      expect(examples).toEqual(mockExamples);
    });
    
    const req = httpMock.expectOne(`${service['apiUrl']}`);
    expect(req.request.method).toBe('GET');
    req.flush(mockExamples);
  });
});
```

## Performance Guidelines

### Change Detection
Use OnPush change detection strategy for improved performance:

```typescript
@Component({
  selector: 'app-example',
  templateUrl: './example.component.html',
  styleUrls: ['./example.component.scss'],
  changeDetection: ChangeDetectionStrategy.OnPush
})
```

### Lazy Loading
Use lazy loading for feature modules to reduce initial load time:

```typescript
{
  path: 'feature',
  loadChildren: () => import('./features/feature/feature.routes')
    .then(m => m.FEATURE_ROUTES)
}
```

### Memoization
Use the `@memoize` decorator or RxJS `shareReplay` for expensive computations:

```typescript
import { shareReplay } from 'rxjs/operators';

getData(): Observable<Data[]> {
  return this.http.get<Data[]>(this.apiUrl).pipe(
    shareReplay(1)
  );
}
```

## Accessibility Guidelines

### ARIA Attributes
Use ARIA attributes to improve accessibility:

```html
<button 
  aria-label="Close menu" 
  [attr.aria-expanded]="isMenuOpen"
>
  <span class="sr-only">Close</span>
  <svg>...</svg>
</button>
```

### Keyboard Navigation
Ensure keyboard navigation works for all interactive elements:

```html
<div 
  role="button"
  tabindex="0"
  (click)="onClick()"
  (keydown.enter)="onClick()"
  (keydown.space)="onClick()"
>
  Click me
</div>
```

## Internationalization (i18n)

### Text Extraction
Use Angular's i18n markers for text extraction:

```html
<h1 i18n="@@homeTitle">Welcome Back!</h1>
<p i18n="@@homeDescription">Your personal assistant AI agent</p>
```

## Deployment Guidelines

### Build Commands
Use the appropriate build command for the target environment:

```bash
# Development
npm run build

# Test
npm run build:test

# Production
npm run build:prod
```

## Security Guidelines

### Content Security Policy
Implement a strict Content Security Policy:

```typescript
// In app.config.ts
{
  providers: [
    {
      provide: APP_INITIALIZER,
      useFactory: () => {
        // Set CSP headers
      },
      multi: true
    }
  ]
}
```

### XSS Prevention
Use Angular's built-in sanitization for user-generated content:

```html
<div [innerHTML]="sanitizer.bypassSecurityTrustHtml(userContent)"></div>
```

### CSRF Protection
Implement CSRF protection for forms:

```typescript
// In app.config.ts
{
  providers: [
    {
      provide: HTTP_INTERCEPTORS,
      useClass: CsrfInterceptor,
      multi: true
    }
  ]
}
```

## Additional Resources

- [Angular Documentation](mdc:https:/angular.dev)
- [Tailwind CSS Documentation](mdc:https:/tailwindcss.com/docs)
- [Flowbite Documentation](mdc:https:/flowbite.com/docs/getting-started/introduction)
- [RxJS Documentation](mdc:https:/rxjs.dev/guide/overview)

## Flowbite Component Guidelines

Flowbite provides pre-built components based on Tailwind CSS. This section covers how to use these components effectively in our Angular application.

### Component Integration

Initialize Flowbite in the root component to ensure proper JavaScript functionality:

```typescript
// app.ts
import { Component, OnInit } from '@angular/core';
import { initFlowbite } from 'flowbite';

@Component({
  // ...
})
export class AppComponent implements OnInit {
  ngOnInit(): void {
    initFlowbite();
  }
}
```

### Common Components and Usage Examples

#### Navbar

Use the Flowbite navbar component for application navigation:

```html
<!-- Example navbar implementation -->
<nav class="bg-white border-gray-200 px-4 lg:px-6 py-2.5 dark:bg-gray-800">
  <div class="flex flex-wrap justify-between items-center">
    <div class="flex items-center">
      <a href="/" class="flex items-center">
        <img src="assets/logo.svg" class="mr-3 h-6 sm:h-9" alt="Personal Assistant Logo" />
        <span class="self-center text-xl font-semibold whitespace-nowrap dark:text-white">Personal Assistant</span>
      </a>
    </div>
    
    <!-- Mobile menu button -->
    <button (click)="toggleMobileMenu()" type="button" class="inline-flex items-center p-2 ml-1 text-sm text-gray-500 rounded-lg lg:hidden hover:bg-gray-100 focus:outline-none focus:ring-2 focus:ring-gray-200 dark:text-gray-400 dark:hover:bg-gray-700 dark:focus:ring-gray-600" [attr.aria-expanded]="isMobileMenuOpen()">
      <span class="sr-only">Open main menu</span>
      <svg class="w-6 h-6" fill="currentColor" viewBox="0 0 20 20" xmlns="http://www.w3.org/2000/svg">
        <path *ngIf="!isMobileMenuOpen()" fill-rule="evenodd" d="M3 5a1 1 0 011-1h12a1 1 0 110 2H4a1 1 0 01-1-1zM3 10a1 1 0 011-1h12a1 1 0 110 2H4a1 1 0 01-1-1zM3 15a1 1 0 011-1h12a1 1 0 110 2H4a1 1 0 01-1-1z" clip-rule="evenodd"></path>
        <path *ngIf="isMobileMenuOpen()" fill-rule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clip-rule="evenodd"></path>
      </svg>
    </button>
    
    <!-- Menu items -->
    <div class="hidden lg:flex w-full lg:w-auto lg:order-1" [ngClass]="{'hidden': !isMobileMenuOpen(), 'block': isMobileMenuOpen()}" id="mobile-menu">
      <ul class="flex flex-col mt-4 font-medium lg:flex-row lg:space-x-8 lg:mt-0">
        <li>
          <a routerLink="/dashboard" routerLinkActive="text-primary-700" class="block py-2 pr-4 pl-3 text-gray-700 border-b border-gray-100 hover:bg-gray-50 lg:hover:bg-transparent lg:border-0 lg:hover:text-primary-700 lg:p-0 dark:text-gray-400 lg:dark:hover:text-white dark:hover:bg-gray-700 dark:hover:text-white lg:dark:hover:bg-transparent dark:border-gray-700">Dashboard</a>
        </li>
        <!-- Add more menu items here -->
      </ul>
    </div>
  </div>
</nav>
```

#### Modal

Use modals for displaying forms or information without navigating away:

```html
<!-- Modal trigger button -->
<button (click)="openModal()" class="block text-white bg-blue-700 hover:bg-blue-800 focus:ring-4 focus:ring-blue-300 font-medium rounded-lg text-sm px-5 py-2.5 text-center dark:bg-blue-600 dark:hover:bg-blue-700 dark:focus:ring-blue-800" type="button">
  Open Modal
</button>

<!-- Modal -->
<div [ngClass]="{'hidden': !isModalOpen, 'flex': isModalOpen}" class="fixed top-0 left-0 right-0 z-50 w-full p-4 overflow-x-hidden overflow-y-auto md:inset-0 h-[calc(100%-1rem)] max-h-full justify-center items-center">
  <div class="relative w-full max-w-2xl max-h-full">
    <div class="relative bg-white rounded-lg shadow dark:bg-gray-700">
      <!-- Modal header -->
      <div class="flex items-start justify-between p-4 border-b rounded-t dark:border-gray-600">
        <h3 class="text-xl font-semibold text-gray-900 dark:text-white">
          Modal Title
        </h3>
        <button (click)="closeModal()" type="button" class="text-gray-400 bg-transparent hover:bg-gray-200 hover:text-gray-900 rounded-lg text-sm w-8 h-8 ml-auto inline-flex justify-center items-center dark:hover:bg-gray-600 dark:hover:text-white">
          <svg class="w-3 h-3" aria-hidden="true" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 14 14">
            <path stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="m1 1 6 6m0 0 6 6M7 7l6-6M7 7l-6 6"/>
          </svg>
          <span class="sr-only">Close modal</span>
        </button>
      </div>
      <!-- Modal body -->
      <div class="p-6 space-y-6">
        <p class="text-base leading-relaxed text-gray-500 dark:text-gray-400">
          Modal content goes here
        </p>
      </div>
      <!-- Modal footer -->
      <div class="flex items-center p-6 space-x-2 border-t border-gray-200 rounded-b dark:border-gray-600">
        <button (click)="saveChanges()" type="button" class="text-white bg-blue-700 hover:bg-blue-800 focus:ring-4 focus:outline-none focus:ring-blue-300 font-medium rounded-lg text-sm px-5 py-2.5 text-center dark:bg-blue-600 dark:hover:bg-blue-700 dark:focus:ring-blue-800">Save changes</button>
        <button (click)="closeModal()" type="button" class="text-gray-500 bg-white hover:bg-gray-100 focus:ring-4 focus:outline-none focus:ring-blue-300 rounded-lg border border-gray-200 text-sm font-medium px-5 py-2.5 hover:text-gray-900 focus:z-10 dark:bg-gray-700 dark:text-gray-300 dark:border-gray-500 dark:hover:text-white dark:hover:bg-gray-600 dark:focus:ring-gray-600">Cancel</button>
      </div>
    </div>
  </div>
</div>

<!-- Modal backdrop -->
<div *ngIf="isModalOpen" class="bg-gray-900 bg-opacity-50 dark:bg-opacity-80 fixed inset-0 z-40"></div>
```

```typescript
// Component class
export class ExampleModalComponent {
  isModalOpen = false;

  openModal(): void {
    this.isModalOpen = true;
    document.body.classList.add('overflow-hidden');
  }

  closeModal(): void {
    this.isModalOpen = false;
    document.body.classList.remove('overflow-hidden');
  }

  saveChanges(): void {
    // Handle save logic
    this.closeModal();
  }
}
```

#### Forms

Use Flowbite form components for consistent styling:

```html
<form (ngSubmit)="onSubmit()" [formGroup]="exampleForm" class="space-y-6">
  <div>
    <label for="email" class="block mb-2 text-sm font-medium text-gray-900 dark:text-white">Email</label>
    <input type="email" id="email" formControlName="email" class="bg-gray-50 border border-gray-300 text-gray-900 text-sm rounded-lg focus:ring-blue-500 focus:border-blue-500 block w-full p-2.5 dark:bg-gray-700 dark:border-gray-600 dark:placeholder-gray-400 dark:text-white dark:focus:ring-blue-500 dark:focus:border-blue-500" placeholder="name@example.com" required>
    <div *ngIf="exampleForm.get('email')?.invalid && exampleForm.get('email')?.touched" class="mt-2 text-sm text-red-600 dark:text-red-500">
      <span *ngIf="exampleForm.get('email')?.hasError('required')">Email is required</span>
      <span *ngIf="exampleForm.get('email')?.hasError('email')">Please enter a valid email address</span>
    </div>
  </div>
  <div>
    <label for="password" class="block mb-2 text-sm font-medium text-gray-900 dark:text-white">Password</label>
    <input type="password" id="password" formControlName="password" class="bg-gray-50 border border-gray-300 text-gray-900 text-sm rounded-lg focus:ring-blue-500 focus:border-blue-500 block w-full p-2.5 dark:bg-gray-700 dark:border-gray-600 dark:placeholder-gray-400 dark:text-white dark:focus:ring-blue-500 dark:focus:border-blue-500" required>
    <div *ngIf="exampleForm.get('password')?.invalid && exampleForm.get('password')?.touched" class="mt-2 text-sm text-red-600 dark:text-red-500">
      <span *ngIf="exampleForm.get('password')?.hasError('required')">Password is required</span>
      <span *ngIf="exampleForm.get('password')?.hasError('minlength')">Password must be at least 8 characters</span>
    </div>
  </div>
  <div class="flex items-start">
    <div class="flex items-center h-5">
      <input id="remember" type="checkbox" formControlName="remember" class="w-4 h-4 border border-gray-300 rounded bg-gray-50 focus:ring-3 focus:ring-blue-300 dark:bg-gray-700 dark:border-gray-600 dark:focus:ring-blue-600 dark:ring-offset-gray-800 dark:focus:ring-offset-gray-800">
    </div>
    <label for="remember" class="ml-2 text-sm font-medium text-gray-900 dark:text-gray-300">Remember me</label>
  </div>
  <button type="submit" [disabled]="exampleForm.invalid" class="w-full text-white bg-blue-700 hover:bg-blue-800 focus:ring-4 focus:outline-none focus:ring-blue-300 font-medium rounded-lg text-sm px-5 py-2.5 text-center dark:bg-blue-600 dark:hover:bg-blue-700 dark:focus:ring-blue-800" [ngClass]="{'opacity-50 cursor-not-allowed': exampleForm.invalid}">Submit</button>
</form>
```

```typescript
import { Component } from '@angular/core';
import { FormBuilder, FormGroup, Validators } from '@angular/forms';

@Component({
  // ...
})
export class ExampleFormComponent {
  exampleForm: FormGroup;

  constructor(private fb: FormBuilder) {
    this.exampleForm = this.fb.group({
      email: ['', [Validators.required, Validators.email]],
      password: ['', [Validators.required, Validators.minLength(8)]],
      remember: [false]
    });
  }

  onSubmit(): void {
    if (this.exampleForm.valid) {
      // Process form submission
    }
  }
}
```

#### Alerts

Use alerts to provide feedback to users:

```html
<!-- Success alert -->
<div id="alert-success" class="flex items-center p-4 mb-4 text-green-800 rounded-lg bg-green-50 dark:bg-gray-800 dark:text-green-400" role="alert">
  <svg class="flex-shrink-0 w-4 h-4" aria-hidden="true" xmlns="http://www.w3.org/2000/svg" fill="currentColor" viewBox="0 0 20 20">
    <path d="M10 .5a9.5 9.5 0 1 0 9.5 9.5A9.51 9.51 0 0 0 10 .5Zm3.707 8.207-4 4a1 1 0 0 1-1.414 0l-2-2a1 1 0 0 1 1.414-1.414L9 10.586l3.293-3.293a1 1 0 0 1 1.414 1.414Z"/>
  </svg>
  <span class="sr-only">Success</span>
  <div class="ml-3 text-sm font-medium">
    Operation completed successfully.
  </div>
  <button (click)="closeAlert('alert-success')" type="button" class="ml-auto -mx-1.5 -my-1.5 bg-green-50 text-green-500 rounded-lg focus:ring-2 focus:ring-green-400 p-1.5 hover:bg-green-200 inline-flex items-center justify-center h-8 w-8 dark:bg-gray-800 dark:text-green-400 dark:hover:bg-gray-700">
    <span class="sr-only">Close</span>
    <svg class="w-3 h-3" aria-hidden="true" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 14 14">
      <path stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="m1 1 6 6m0 0 6 6M7 7l6-6M7 7l-6 6"/>
    </svg>
  </button>
</div>

<!-- Error alert -->
<div id="alert-error" class="flex items-center p-4 mb-4 text-red-800 rounded-lg bg-red-50 dark:bg-gray-800 dark:text-red-400" role="alert">
  <svg class="flex-shrink-0 w-4 h-4" aria-hidden="true" xmlns="http://www.w3.org/2000/svg" fill="currentColor" viewBox="0 0 20 20">
    <path d="M10 .5a9.5 9.5 0 1 0 9.5 9.5A9.51 9.51 0 0 0 10 .5ZM10 15a1 1 0 1 1 0-2 1 1 0 0 1 0 2Zm1-4a1 1 0 0 1-2 0V6a1 1 0 0 1 2 0v5Z"/>
  </svg>
  <span class="sr-only">Error</span>
  <div class="ml-3 text-sm font-medium">
    An error occurred while processing your request.
  </div>
  <button (click)="closeAlert('alert-error')" type="button" class="ml-auto -mx-1.5 -my-1.5 bg-red-50 text-red-500 rounded-lg focus:ring-2 focus:ring-red-400 p-1.5 hover:bg-red-200 inline-flex items-center justify-center h-8 w-8 dark:bg-gray-800 dark:text-red-400 dark:hover:bg-gray-700">
    <span class="sr-only">Close</span>
    <svg class="w-3 h-3" aria-hidden="true" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 14 14">
      <path stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="m1 1 6 6m0 0 6 6M7 7l6-6M7 7l-6 6"/>
    </svg>
  </button>
</div>
```

```typescript
export class AlertsComponent {
  closeAlert(alertId: string): void {
    const alert = document.getElementById(alertId);
    if (alert) {
      alert.classList.add('hidden');
    }
  }
}
```

#### Card

Use cards to display content in a clean, organized manner:

```html
<div class="max-w-sm p-6 bg-white border border-gray-200 rounded-lg shadow dark:bg-gray-800 dark:border-gray-700">
  <a href="#">
    <h5 class="mb-2 text-2xl font-bold tracking-tight text-gray-900 dark:text-white">Content Title</h5>
  </a>
  <p class="mb-3 font-normal text-gray-700 dark:text-gray-400">Card content description goes here...</p>
  <a href="#" class="inline-flex items-center px-3 py-2 text-sm font-medium text-center text-white bg-blue-700 rounded-lg hover:bg-blue-800 focus:ring-4 focus:outline-none focus:ring-blue-300 dark:bg-blue-600 dark:hover:bg-blue-700 dark:focus:ring-blue-800">
    Read more
    <svg class="w-3.5 h-3.5 ml-2" aria-hidden="true" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 14 10">
      <path stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M1 5h12m0 0L9 1m4 4L9 9"/>
    </svg>
  </a>
</div>
```

### Dynamic Component Initialization

For components that require JavaScript initialization, make sure to call `initFlowbite()` after dynamic content is added:

```typescript
import { Component, OnInit, AfterViewInit } from '@angular/core';
import { initFlowbite } from 'flowbite';

@Component({
  // ...
})
export class DynamicComponent implements OnInit, AfterViewInit {
  dynamicContent: boolean = false;

  ngOnInit(): void {
    // Initial setup
  }

  ngAfterViewInit(): void {
    // Initialize Flowbite components
    initFlowbite();
  }

  loadDynamicContent(): void {
    this.dynamicContent = true;
    
    // Re-initialize Flowbite after content is loaded
    setTimeout(() => {
      initFlowbite();
    }, 0);
  }
}
```

### Best Practices for Flowbite Components

1. **Consistent Styling**:
   - Maintain a consistent color scheme by using Tailwind's color classes
   - Create a shared component for common UI elements like buttons, inputs, etc.

2. **Accessibility**:
   - Ensure all Flowbite components have proper ARIA attributes
   - Include focus states for keyboard navigation
   - Use semantic HTML elements when implementing Flowbite components

3. **Responsive Design**:
   - Test all Flowbite components on different screen sizes
   - Use Tailwind's responsive prefixes (`sm:`, `md:`, `lg:`, etc.) consistently

4. **Dark Mode Support**:
   - Include dark mode classes (`dark:bg-gray-800`, etc.) for all components
   - Test components in both light and dark modes

5. **Component Composition**:
   - Break down complex Flowbite components into smaller Angular components
   - Use Angular's `@Input()` and `@Output()` decorators for component communication
