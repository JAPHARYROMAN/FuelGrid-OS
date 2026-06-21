import { Suspense } from 'react';

import { AttendantInstall } from '@/components/auth/attendant-install';
import { LoginForm } from '@/components/auth/login-form';
import { ManualDownload } from '@/components/auth/manual-download';

export default function LoginPage() {
  return (
    <>
      <Suspense>
        <LoginForm />
      </Suspense>
      <AttendantInstall />
      <ManualDownload />
    </>
  );
}
