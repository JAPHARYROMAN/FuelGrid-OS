import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@fuelgrid/ui';

export default function MfaPage() {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Multi-factor authentication</CardTitle>
        <CardDescription>
          MFA enroll + verify UI lands in Stage 9. For now, the MFA code field appears on the login
          form once the API responds with{' '}
          <code className="rounded bg-muted px-1.5 py-0.5 text-xs">mfa_required</code>.
        </CardDescription>
      </CardHeader>
      <CardContent />
    </Card>
  );
}
