import { Component, inject, signal, ChangeDetectionStrategy } from '@angular/core';

import { FormBuilder, FormGroup, Validators, ReactiveFormsModule } from '@angular/forms';
import { Router } from '@angular/router';
import { AuthService } from '../../../../core/services/auth.service';
import { UserRegisterRequest } from '../../../../core/models/user.model';

/**
 * Local account creation (username, email, password) against the built-in
 * account store.
 */
@Component({
  selector: 'app-register',
  standalone: true,
  imports: [ReactiveFormsModule],
  templateUrl: './register.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  styleUrls: ['./register.component.scss']
})
export class RegisterComponent {
  private fb: FormBuilder = inject(FormBuilder);
  private authService: AuthService = inject(AuthService);
  private router: Router = inject(Router);

  registerForm: FormGroup = this.fb.group({
    // Username bounds mirror the ent schema (MinLen(3).MaxLen(50)); without them
    // a too-short/long username reaches the backend and comes back as a generic
    // 500 instead of an inline validation message.
    username: ['', [Validators.required, Validators.minLength(3), Validators.maxLength(50)]],
    email: ['', [Validators.required, Validators.email]],
    password: ['', [Validators.required, Validators.minLength(8)]],
    confirmPassword: ['', [Validators.required]]
  }, { validators: this.passwordMatchValidator });

  isLoading = signal(false);
  errorMessage = signal<string | null>(null);

  // Reports the mismatch at the GROUP level only. Setting the error on the
  // confirmPassword control directly would leave it stuck when the user fixes
  // the mismatch by editing the password field instead — the group error would
  // clear but the control's error would not, keeping the form invalid with no
  // visible reason. The template reads registerForm.errors for the message.
  private passwordMatchValidator(form: FormGroup) {
    const password = form.get('password')?.value;
    const confirmPassword = form.get('confirmPassword')?.value;

    if (password && confirmPassword && password !== confirmPassword) {
      return { passwordMismatch: true };
    }

    return null;
  }

  async onSubmit(): Promise<void> {
    if (!this.registerForm.valid) {
      return;
    }

    this.isLoading.set(true);
    this.errorMessage.set(null);

    // No terms-of-service is collected here, so terms_accepted is omitted rather
    // than sent — the backend records acceptance only when it is provided.
    const registerData: UserRegisterRequest = {
      username: this.registerForm.value.username,
      email: this.registerForm.value.email,
      password: this.registerForm.value.password
    };

    try {
      await this.authService.register(registerData, 'local');
      const returnUrl = '/chat';
      this.router.navigate([returnUrl]);
    } catch (error: any) {
      this.errorMessage.set(error?.message || 'Registration failed. Please try again.');
    } finally {
      this.isLoading.set(false);
    }
  }

  navigateToLogin(): void {
    this.router.navigate(['/auth/login']);
  }
}
