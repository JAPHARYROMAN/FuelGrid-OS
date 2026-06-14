-- Reverse 0111_executive_report_live: remove the Executive Business Report row
-- and revert the Executive category to its 0105 seed (partial, still pointing at
-- /reports/executive). The station-comparison row under the category is left
-- intact (it predates this migration).
DELETE FROM reports
WHERE tenant_id IS NULL AND key = 'executive';

UPDATE report_categories
SET availability = 'partial',
    target_route = '/reports/executive'
WHERE tenant_id IS NULL AND key = 'executive';
