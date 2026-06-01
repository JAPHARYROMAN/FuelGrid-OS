import { redirect } from 'next/navigation';

/**
 * The Phase-16 daily close report lives at /reports/station-close. Keep the old
 * path working with a redirect so existing links / bookmarks land on the new
 * structured view.
 */
export default function DailyCloseRedirect() {
  redirect('/reports/station-close');
}
