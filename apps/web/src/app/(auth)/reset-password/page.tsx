import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@fuelgrid/ui';

export default function ResetPasswordPage() {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Reset password</CardTitle>
        <CardDescription>
          Token + new-password form lands in Stage 9. The backend endpoint (
          <code className="rounded bg-muted px-1.5 py-0.5 text-xs">
            POST /api/v1/auth/password-reset/confirm
          </code>
          ) is already live.
        </CardDescription>
      </CardHeader>
      <CardContent />
    </Card>
  );
}
