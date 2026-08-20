import { cookies } from 'next/headers';
import { redirect } from 'next/navigation';

import { SESSION_COOKIE } from '@/lib/server/session-cookie';

/**
 * Root route is a thin redirector — authenticated users go to the
 * command center, the rest get the login screen. Resolve this on the server
 * from the real httpOnly session cookie so localStorage can never disagree
 * with the navigation decision.
 */
export default async function HomePage() {
  const session = (await cookies()).get(SESSION_COOKIE)?.value;
  redirect(session ? '/command-center' : '/login');
}
