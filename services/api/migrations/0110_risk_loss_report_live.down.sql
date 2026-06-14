-- Reverse 0110_risk_loss_report_live: remove the Risk & Loss intelligence report
-- row and revert the Risk & Loss category to its 0105 seed (live, pointing at the
-- original fuel-loss route).
DELETE FROM reports
WHERE tenant_id IS NULL AND key = 'risk-loss';

UPDATE report_categories
SET availability = 'live',
    target_route = '/reports/fuel-loss'
WHERE tenant_id IS NULL AND key = 'risk-loss';
