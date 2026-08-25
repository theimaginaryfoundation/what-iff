import { Amplify } from 'aws-amplify';
import { signIn, signOut, fetchAuthSession } from 'aws-amplify/auth';
import { cognitoUserPoolsTokenProvider } from 'aws-amplify/auth/cognito';
import { sharedInMemoryStorage } from 'aws-amplify/utils';

/**
 * Logs a pre-existing Cognito user in directly against the user pool, for
 * configs targeting a build with `authMode: 'cognito'`, where there is no
 * Go-issued password hash to authenticate against.
 *
 * Uses `aws-amplify/auth` - the same library the app's own login form goes
 * through (src/app/core/services/auth.service.ts) - so this path and the
 * browser path are the same client, not two implementations asserted to
 * behave alike. `signIn` with no `authFlowType` performs SRP
 * (`USER_SRP_AUTH`); plain-password `InitiateAuth` would only work if the app
 * client allows `ALLOW_USER_PASSWORD_AUTH`, which is not guaranteed, and the
 * AWS SDK v3 client has no SRP implementation at all.
 *
 * No AWS credentials of any kind are used or required - this is the same
 * unauthenticated, end-user-facing call a browser makes to sign in. `userPoolId`
 * and `clientId` are not secret (they already ship in any Cognito-mode built
 * frontend); only the account's own username/password are.
 */
export interface CognitoUserPoolConfig {
  userPoolId: string;
  clientId: string;
}

/**
 * Amplify holds pool config and session state per process, and a Playwright
 * worker is a process, so configuring once per worker is both necessary and
 * sufficient. Tracked by pool identity rather than a bare boolean so a run
 * that ever targets two pools reconfigures instead of silently authenticating
 * against the first one.
 *
 * `sharedInMemoryStorage` replaces Amplify's localStorage default, which does
 * not exist here: this is Node, not a browser. Tokens live only for the
 * lifetime of the worker, which is the right retention for a test process.
 */
let configuredPool: string | undefined;

function configureAmplify(pool: CognitoUserPoolConfig): void {
  const key = `${pool.userPoolId}/${pool.clientId}`;
  if (configuredPool === key) return;

  Amplify.configure({
    Auth: { Cognito: { userPoolId: pool.userPoolId, userPoolClientId: pool.clientId } },
  });
  cognitoUserPoolsTokenProvider.setKeyValueStorage(sharedInMemoryStorage);
  configuredPool = key;
}

/** The middleware validates the ID token specifically (internal/auth/cognito.go requires token_use "id"), not the access token. */
export async function loginWithCognito(pool: CognitoUserPoolConfig, username: string, password: string): Promise<string> {
  configureAmplify(pool);

  // `signIn` throws if this process already holds a session. Callers memoize a
  // single login per worker (fixtures/api.ts), so this only fires when
  // something authenticates twice - clearing first makes that a fresh login
  // rather than an "already signed in" error that reads like a Cognito fault.
  await signOut();

  const { nextStep } = await signIn({ username, password });

  // Anything other than DONE is a challenge a test run has no way to answer:
  // FORCE_CHANGE_PASSWORD (typical right after AdminCreateUser), MFA, or one
  // of the passwordless steps. Name the step so the message says which.
  if (nextStep.signInStep !== 'DONE') {
    throw new Error(
      `Cognito account "${username}" cannot complete a non-interactive login: it requires "${nextStep.signInStep}". ` +
        'Log in once through the real app to clear the challenge, then re-run.',
    );
  }

  const idToken = (await fetchAuthSession()).tokens?.idToken?.toString();
  if (!idToken) {
    throw new Error(`Cognito reported a successful sign-in for "${username}" but issued no ID token.`);
  }
  return idToken;
}
