import Link from 'next/link';

import {
  Button,
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@fuelgrid/ui';

/**
 * Standalone MFA challenge information page. The live second-factor prompt is
 * handled inline on the sign-in form (it reveals an authentication-code field
 * the moment the API answers `mfa_required`), so this page exists for direct
 * navigation and explains where the code comes from.
 */
export default function MfaPage() {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Two-factor authentication</CardTitle>
        <CardDescription>
          Your account is protected by an authenticator app. Sign in with your password, then enter
          the 6-digit code it shows.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <ul className="flex flex-col gap-2 text-sm text-muted-foreground">
          <li>
            Open your authenticator app (Google Authenticator, 1Password, Authy, …) and read the
            current code for FuelGrid OS.
          </li>
          <li>
            Lost your device? Use one of the one-time backup codes you saved when you enrolled —
            each works exactly once.
          </li>
          <li>Manage your second factor any time from your profile&rsquo;s Security section.</li>
        </ul>
      </CardContent>
      <CardFooter>
        <Button asChild>
          <Link href="/login">Back to sign in</Link>
        </Button>
      </CardFooter>
    </Card>
  );
}
