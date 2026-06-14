import { redirect } from 'next/navigation';

/**
 * The Sales report now lives at /reports/sales (the signature §5.2 suite). The
 * old /reports/sales-summary path redirects there so any saved link or bookmark
 * resolves to the live report and the hub has no dead links.
 */
export default function SalesSummaryRedirect() {
  redirect('/reports/sales');
}
