import { Component, inject, signal, ChangeDetectionStrategy } from '@angular/core';

import { FormBuilder, FormGroup, Validators, ReactiveFormsModule } from '@angular/forms';
import { Router, ActivatedRoute } from '@angular/router';
import { AuthService } from '../../../../core/services/auth.service';
import { UserLoginRequest } from '../../../../core/models/user.model';
import { EyeIconComponent, EyeOffIconComponent } from '../../../../shared/ui/icons';

/**
 * Local username/password sign-in against the built-in account store.
 */
@Component({
  selector: 'app-login',
  standalone: true,
  imports: [ReactiveFormsModule, EyeIconComponent, EyeOffIconComponent],
  templateUrl: './login.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  styleUrls: ['./login.component.scss']
})
export class LoginComponent {
  private fb: FormBuilder = inject(FormBuilder);
  private authService: AuthService = inject(AuthService);
  private router: Router = inject(Router);
  private route: ActivatedRoute = inject(ActivatedRoute);

  loginForm: FormGroup = this.fb.group({
    username: ['', [Validators.required]],
    password: ['', [Validators.required, Validators.minLength(8)]],
    remember: [false]
  });

  isLoading = signal(false);
  errorMessage = signal<string | null>(null);
  /** When true the password field renders as plain text; default stays masked. */
  passwordVisible = signal(false);

  togglePasswordVisibility(): void {
    this.passwordVisible.update(visible => !visible);
  }

  async onSubmit(): Promise<void> {
    if (!this.loginForm.valid) {
      return;
    }

    this.isLoading.set(true);
    this.errorMessage.set(null);

    const loginData: UserLoginRequest = {
      username: this.loginForm.value.username,
      password: this.loginForm.value.password
    };

    try {
      await this.authService.login(loginData, 'local');
      const returnUrl = this.route.snapshot.queryParams['returnUrl'] || '/chat';
      this.router.navigate([returnUrl]);
    } catch (error: any) {
      this.errorMessage.set(error?.message || 'Login failed. Please try again.');
    } finally {
      this.isLoading.set(false);
    }
  }

  navigateToRegister(): void {
    this.router.navigate(['/auth/register']);
  }
}
