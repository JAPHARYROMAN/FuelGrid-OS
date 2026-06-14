-- Reverse 0107_delivery_report_live: remove the Delivery report catalog row and
-- revert the Delivery category to its 0105 seed (live, pointing at the borrowed
-- station-close route).
DELETE FROM reports WHERE tenant_id IS NULL AND key = 'delivery';

UPDATE report_categories
SET availability = 'live',
    target_route = '/reports/station-close'
WHERE tenant_id IS NULL AND key = 'delivery';
