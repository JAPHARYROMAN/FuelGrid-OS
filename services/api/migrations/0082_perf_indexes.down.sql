-- 0082_perf_indexes.down.sql
-- Drop exactly the indexes added in 0082_perf_indexes.up.sql.

DROP INDEX IF EXISTS idx_fuel_auth_tenant_created;
DROP INDEX IF EXISTS idx_cpp_tenant_created;
DROP INDEX IF EXISTS idx_cpr_tenant_created;
DROP INDEX IF EXISTS idx_sto_tenant_created;
DROP INDEX IF EXISTS idx_shifts_station_opened;
DROP INDEX IF EXISTS idx_incidents_tenant_opened;
DROP INDEX IF EXISTS idx_companies_tenant_created;
DROP INDEX IF EXISTS idx_supplier_invoices_tenant_created;
DROP INDEX IF EXISTS idx_purchase_orders_tenant_created;
DROP INDEX IF EXISTS idx_accounting_exports_tenant_time;
DROP INDEX IF EXISTS idx_customer_invoices_tenant_date;
DROP INDEX IF EXISTS idx_customer_payments_tenant_date;
DROP INDEX IF EXISTS idx_supplier_payments_tenant_date;
DROP INDEX IF EXISTS idx_expenses_tenant_date;
DROP INDEX IF EXISTS idx_payables_tenant_due;
DROP INDEX IF EXISTS idx_journal_entries_tenant_number;
DROP INDEX IF EXISTS idx_audit_logs_tenant_time;
