import { redirect } from 'next/navigation';

/**
 * Sales summary is folded into the Phase-16 Daily Station Close report (sales,
 * litres and margin with a recent-day trend). Redirect the old path to it so the
 * hub has no dead links.
 */
export default function SalesSummaryRedirect() {
  redirect('/reports/station-close');
}
