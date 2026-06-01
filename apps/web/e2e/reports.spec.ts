import { test, expect } from '@playwright/test';

import { authedSession } from './helpers/journey';

/**
 * Reports write-journey (QA-7): trigger a CSV export and assert the request
 * fires. The Reports page fetches a Blob from the report endpoint and hands it
 * to a synthetic <a download> click. We mock the endpoint to return CSV and
 * assert (a) the network request was made and (b) the browser download event
 * fired with the expected filename.
 */

test.describe('reports', () => {
  test('CSV download fires the report request', async ({ page }) => {
    await authedSession(page);

    let revenueCalls = 0;
    await page.route('**/api/bff/api/v1/stations/*/reports/revenue.csv**', async (route) => {
      revenueCalls += 1;
      await route.fulfill({
        status: 200,
        headers: {
          'Content-Type': 'text/csv',
          'Content-Disposition': 'attachment; filename="revenue-DS1.csv"',
          'X-Request-Id': 'e2e-report',
        },
        body: 'business_date,gross\n2026-06-01,1000.00\n',
      });
    });

    await page.goto('/reports');
    await expect(page.getByRole('heading', { name: 'Reporting hub', exact: true })).toBeVisible();
    await expect(page.getByText('Sales Summary', { exact: true })).toBeVisible();

    // The first "CSV" button is the Sales Summary (revenue) export. The page
    // builds a synthetic <a download> + clicks it; capture the download event.
    const downloadPromise = page.waitForEvent('download');
    await page.getByRole('button', { name: 'CSV' }).first().click();

    const download = await downloadPromise;
    expect(download.suggestedFilename()).toBe('sales-DS1.csv');
    expect(revenueCalls).toBe(1);
  });
});
