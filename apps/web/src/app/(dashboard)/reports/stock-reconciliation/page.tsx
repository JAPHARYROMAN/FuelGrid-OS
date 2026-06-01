import { redirect } from 'next/navigation';

/**
 * The Phase-16 reconciliation report lives at /reports/inventory/reconciliation
 * (the signature waterfall view). Keep the old path working with a permanent
 * redirect so existing links / bookmarks land on the new view.
 */
export default function StockReconciliationRedirect() {
  redirect('/reports/inventory/reconciliation');
}
