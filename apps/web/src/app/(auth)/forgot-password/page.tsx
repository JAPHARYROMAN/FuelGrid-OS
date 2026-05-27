import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@fuelgrid/ui';

export default function ForgotPasswordPage() {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Forgot password</CardTitle>
        <CardDescription>
          A real reset-request form lands in Stage 9. Until then, run{' '}
          <code className="rounded bg-muted px-1.5 py-0.5 text-xs">make seed</code> to recreate the
          demo user.
        </CardDescription>
      </CardHeader>
      <CardContent />
    </Card>
  );
}
