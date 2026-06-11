import { Suspense } from 'react';

import { AttendantInstall } from '@/components/auth/attendant-install';
import { LoginForm } from '@/components/auth/login-form';

export default function LoginPage() {
  return (
    <>
      <Suspense>
        <LoginForm />
      </Suspense>
      <AttendantInstall />
    </>
  );
}
