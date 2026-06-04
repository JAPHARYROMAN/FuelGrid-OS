# FuelGrid OS Audit Events Matrix

**Purpose:** Define audit event codes and the minimum evidence each event must preserve.

Sensitive records that affect stock, cash, revenue, receivables, payables, reports, user access, or governance must be corrected through reversals, approvals, or controlled changes instead of destructive edits.

## Severity labels

- `Low`: Administrative or read/export event with limited operational impact.
- `Medium`: Operational change with station, stock, payment, or workflow impact.
- `High`: Financial, approval, access-control, reversal, or governance change.
- `Critical`: Closed-period, security, or cross-scope event requiring elevated retention and review.

## Retention labels

- `Standard`: Tenant audit retention policy.
- `Financial`: Financial retention policy, minimum 7 years unless a stricter policy applies.
- `Security`: Security retention policy, minimum 7 years unless a stricter policy applies.
- `Permanent`: Retain indefinitely unless legal retention policy explicitly permits archival.

## Matrix

| Event code | Entity type | Action | Reason required | Before snapshot | After snapshot | Severity | Retention |
|---|---|---|---|---|---|---|---|
| setup.step_completed | setup_step | Complete setup step | No | No | Yes | Low | Standard |
| company.created | company | Create company | No | No | Yes | Medium | Standard |
| company.updated | company | Update company | Conditional | Yes | Yes | Medium | Standard |
| company.suspended | company | Suspend company | Yes | Yes | Yes | High | Standard |
| company.closed | company | Close company | Yes | Yes | Yes | High | Permanent |
| region.created | region | Create region | No | No | Yes | Medium | Standard |
| region.updated | region | Update region | Conditional | Yes | Yes | Medium | Standard |
| region.deactivated | region | Deactivate region | Yes | Yes | Yes | Medium | Standard |
| station.created | station | Create station | No | No | Yes | Medium | Standard |
| station.updated | station | Update station or operating rules | Conditional | Yes | Yes | Medium | Standard |
| station.status_changed | station | Change station status | Yes | Yes | Yes | High | Standard |
| station.closed | station | Close station | Yes | Yes | Yes | High | Permanent |
| product.created | product | Create product | No | No | Yes | Medium | Standard |
| product.updated | product | Update product | Conditional | Yes | Yes | Medium | Standard |
| product.price_changed | product_price | Change product price | Yes | Yes | Yes | High | Financial |
| product.status_changed | product | Activate or deactivate product | Yes | Yes | Yes | Medium | Standard |
| tank.created | tank | Create tank | No | No | Yes | Medium | Standard |
| tank.capacity_changed | tank | Change tank capacity | Yes | Yes | Yes | High | Standard |
| tank.mapping_changed | tank | Change tank to product mapping | Yes | Yes | Yes | High | Standard |
| tank.status_changed | tank | Activate or deactivate tank | Yes | Yes | Yes | Medium | Standard |
| pump.created | pump | Create pump | No | No | Yes | Medium | Standard |
| pump.updated | pump | Update pump | Conditional | Yes | Yes | Medium | Standard |
| nozzle.created | nozzle | Create nozzle | No | No | Yes | Medium | Standard |
| nozzle.mapping_changed | nozzle | Change nozzle pump, tank, or product mapping | Yes | Yes | Yes | High | Standard |
| hardware.deactivated | hardware_component | Deactivate tank, pump, or nozzle | Yes | Yes | Yes | Medium | Standard |
| opening_stock.recorded | opening_stock | Record opening stock | Yes | No | Yes | High | Financial |
| opening_stock.corrected | opening_stock | Correct opening stock before approval | Yes | Yes | Yes | High | Financial |
| opening_stock.approved | opening_stock | Approve and lock opening stock | Yes | Yes | Yes | High | Financial |
| opening_stock.rejected | opening_stock | Reject a draft opening stock with a reason | Yes | Yes | Yes | High | Financial |
| user.invited | user | Invite user | No | No | Yes | Medium | Security |
| user.activated | user | Activate user | Conditional | Yes | Yes | Medium | Security |
| user.deactivated | user | Deactivate user | Yes | Yes | Yes | High | Security |
| user.role_changed | user | Assign or remove role | Yes | Yes | Yes | High | Security |
| user.station_access_changed | user_access | Change station, region, or company access | Yes | Yes | Yes | High | Security |
| user.session_revoked | user_session | Revoke user sessions | Yes | Yes | Yes | High | Security |
| user.mfa_reset | user | Reset MFA | Yes | Yes | Yes | High | Security |
| role.created | role | Create role | No | No | Yes | Medium | Security |
| role.updated | role | Update role metadata | Conditional | Yes | Yes | Medium | Security |
| role.permissions_changed | role | Change role permissions | Yes | Yes | Yes | High | Security |
| role.deactivated | role | Deactivate role | Yes | Yes | Yes | High | Security |
| permission.denied | permission_check | Deny sensitive action | No | No | Yes | Medium | Security |
| shift.opened | shift | Open shift | No | No | Yes | Medium | Financial |
| shift.closed | shift | Close shift | No | Yes | Yes | Medium | Financial |
| shift.submitted | shift | Submit shift for approval | No | Yes | Yes | Medium | Financial |
| shift.approved | shift | Approve shift | Conditional | Yes | Yes | High | Financial |
| shift.rejected | shift | Reject shift | Yes | Yes | Yes | High | Financial |
| shift.correction_requested | shift | Request shift correction | Yes | Yes | Yes | High | Financial |
| shift.locked | shift | Lock approved shift | No | Yes | Yes | High | Financial |
| meter_reading.captured | meter_reading | Capture meter reading | No | No | Yes | Medium | Financial |
| meter_reading.corrected | meter_reading | Correct meter reading | Yes | Yes | Yes | High | Financial |
| sale.created | sale | Create sale | No | No | Yes | High | Financial |
| sale.void_requested | sale_void | Request sale void | Yes | Yes | Yes | High | Financial |
| sale.void_approved | sale_void | Approve sale void | Yes | Yes | Yes | High | Financial |
| sale.void_rejected | sale_void | Reject sale void | Yes | Yes | Yes | High | Financial |
| sale.reversed | sale_reversal | Reverse sale effects | Yes | Yes | Yes | High | Financial |
| payment.created | payment | Create payment | No | No | Yes | High | Financial |
| payment.status_changed | payment | Change payment status | Conditional | Yes | Yes | High | Financial |
| payment.callback_received | payment_callback | Receive external payment callback | No | No | Yes | Medium | Financial |
| payment.reconciled | payment | Reconcile payment | Yes | Yes | Yes | High | Financial |
| delivery.expected_created | delivery | Create expected delivery | No | No | Yes | Medium | Financial |
| delivery.received | delivery_receipt | Receive delivery | No | Yes | Yes | High | Financial |
| delivery.receipt_approved | delivery_receipt | Approve delivery receipt | Conditional | Yes | Yes | High | Financial |
| delivery.variance_flagged | delivery_receipt | Flag delivery variance | No | Yes | Yes | High | Financial |
| tank_dip.captured | tank_dip | Capture tank dip | No | No | Yes | Medium | Financial |
| tank_dip.corrected | tank_dip | Correct tank dip | Yes | Yes | Yes | High | Financial |
| stock.reconciliation_submitted | stock_reconciliation | Submit stock reconciliation | No | Yes | Yes | High | Financial |
| stock.reconciliation_approved | stock_reconciliation | Approve stock reconciliation | Conditional | Yes | Yes | High | Financial |
| stock.adjustment_requested | stock_adjustment | Request stock adjustment | Yes | Yes | Yes | High | Financial |
| stock.adjustment_approved | stock_adjustment | Approve stock adjustment | Yes | Yes | Yes | High | Financial |
| stock.adjustment_rejected | stock_adjustment | Reject stock adjustment | Yes | Yes | Yes | High | Financial |
| stock.adjustment_posted | inventory_ledger | Post stock adjustment | Yes | Yes | Yes | High | Financial |
| stock.transfer_requested | stock_transfer | Request stock transfer | Yes | Yes | Yes | High | Financial |
| stock.transfer_dispatched | stock_transfer | Dispatch transfer | No | Yes | Yes | High | Financial |
| stock.transfer_received | stock_transfer | Receive transfer | No | Yes | Yes | High | Financial |
| stock.transfer_approved | stock_transfer | Approve transfer | Conditional | Yes | Yes | High | Financial |
| stock.transfer_variance_recorded | stock_transfer | Record transfer variance | Yes | Yes | Yes | High | Financial |
| customer.created | customer | Create customer | No | No | Yes | Medium | Financial |
| customer.updated | customer | Update customer | Conditional | Yes | Yes | Medium | Financial |
| customer.credit_limit_changed | customer | Change credit limit | Yes | Yes | Yes | High | Financial |
| customer.placed_on_hold | customer | Place customer on hold | Yes | Yes | Yes | High | Financial |
| customer.status_changed | customer | Activate or deactivate customer | Yes | Yes | Yes | Medium | Financial |
| credit_sale.created | receivable_transaction | Create credit sale | No | No | Yes | High | Financial |
| credit_sale.override_requested | credit_override | Request credit policy override | Yes | Yes | Yes | High | Financial |
| credit_sale.override_approved | credit_override | Approve credit policy override | Yes | Yes | Yes | High | Financial |
| invoice.generated | invoice | Generate invoice | No | No | Yes | Medium | Financial |
| invoice.sent | invoice | Send invoice | No | Yes | Yes | Low | Financial |
| payment.allocated | payment_allocation | Allocate customer payment | No | Yes | Yes | High | Financial |
| payment.allocation_reversed | payment_allocation | Reverse customer payment allocation | Yes | Yes | Yes | High | Financial |
| supplier.created | supplier | Create supplier | No | No | Yes | Medium | Financial |
| supplier.updated | supplier | Update supplier | Conditional | Yes | Yes | Medium | Financial |
| supplier.status_changed | supplier | Activate or deactivate supplier | Yes | Yes | Yes | Medium | Financial |
| purchase_order.created | purchase_order | Create purchase order | No | No | Yes | Medium | Financial |
| purchase_order.submitted | purchase_order | Submit purchase order for approval | No | Yes | Yes | Medium | Financial |
| purchase_order.approved | purchase_order | Approve purchase order | Conditional | Yes | Yes | High | Financial |
| purchase_order.cancelled | purchase_order | Cancel purchase order | Yes | Yes | Yes | High | Financial |
| purchase_order.closed | purchase_order | Close purchase order | Yes | Yes | Yes | Medium | Financial |
| supplier_invoice.recorded | supplier_invoice | Record supplier invoice | No | No | Yes | High | Financial |
| supplier_invoice.approved | supplier_invoice | Approve supplier invoice | Conditional | Yes | Yes | High | Financial |
| supplier_invoice.payment_scheduled | supplier_invoice | Schedule supplier payment | No | Yes | Yes | High | Financial |
| supplier_invoice.paid | supplier_invoice | Mark supplier invoice paid | No | Yes | Yes | High | Financial |
| expense.category_created | expense_category | Create expense category | No | No | Yes | Medium | Financial |
| expense.category_changed | expense_category | Change category, threshold, or mapping | Yes | Yes | Yes | High | Financial |
| expense.submitted | expense | Submit expense | Conditional | No | Yes | Medium | Financial |
| expense.approved | expense | Approve expense | Conditional | Yes | Yes | High | Financial |
| expense.rejected | expense | Reject expense | Yes | Yes | Yes | Medium | Financial |
| expense.posted | expense | Post expense | No | Yes | Yes | High | Financial |
| petty_cash.float_created | petty_cash_float | Create petty cash float | Yes | No | Yes | High | Financial |
| petty_cash.movement_recorded | petty_cash_movement | Record cash issue, return, or expense | Yes | Yes | Yes | High | Financial |
| petty_cash.reconciled | petty_cash_reconciliation | Reconcile petty cash | Conditional | Yes | Yes | High | Financial |
| petty_cash.period_closed | petty_cash_period | Close petty cash period | Yes | Yes | Yes | High | Financial |
| approval.policy_changed | approval_policy | Change governance policy | Yes | Yes | Yes | High | Permanent |
| approval.request_submitted | approval_request | Submit approval request | Conditional | No | Yes | Medium | Financial |
| approval.decision_recorded | approval_decision | Approve, reject, request changes, or cancel | Conditional | Yes | Yes | High | Financial |
| approval.escalated | approval_request | Escalate approval | Conditional | Yes | Yes | Medium | Financial |
| audit.exported | audit_export | Export audit log | Yes | No | Yes | High | Security |
| report.exported | report_export | Export report | No | No | Yes | Medium | Financial |
| report.scheduled | scheduled_report | Schedule report | No | No | Yes | Low | Standard |
| notification.preference_changed | notification_preference | Change notification preferences | No | Yes | Yes | Low | Standard |
| notification.acknowledged | notification | Acknowledge notification | No | Yes | Yes | Low | Standard |
| notification.dispatched | notification_dispatch | Dispatch notification | No | No | Yes | Low | Standard |
| risk.alert_created | risk_alert | Create risk alert | No | No | Yes | Medium | Standard |
| risk.alert_status_changed | risk_alert | Change alert status | Yes | Yes | Yes | Medium | Standard |
| risk.alert_resolved | risk_alert | Resolve alert | Yes | Yes | Yes | Medium | Standard |
| risk.alert_dismissed | risk_alert | Dismiss alert | Yes | Yes | Yes | Medium | Standard |
| insight.generated | insight | Persist deterministic insight | No | No | Yes | Low | Standard |
| sync.batch_processed | sync_batch | Process offline sync batch | No | No | Yes | Medium | Standard |
| sync.conflict_detected | sync_conflict | Detect offline sync conflict | No | No | Yes | Medium | Standard |
| mobile.entry_synced | mobile_entry | Sync mobile entry | No | No | Yes | Medium | Standard |
| device.registered | device | Register integration device | No | No | Yes | Medium | Security |
| integration.signature_rejected | integration_event | Reject bad integration signature | No | No | Yes | High | Security |
| integration.webhook_received | integration_event | Receive integration webhook | No | No | Yes | Medium | Security |
| enterprise.scope_changed | user_context | Switch enterprise context | No | Yes | Yes | Medium | Security |
| retention.policy_changed | retention_policy | Change retention policy | Yes | Yes | Yes | Critical | Permanent |
| retention.job_run | retention_job | Run retention job | No | No | Yes | Medium | Permanent |
| closed_period.change_requested | closed_period | Request closed-period change | Yes | Yes | Yes | Critical | Permanent |
| closed_period.change_approved | closed_period | Approve closed-period change | Yes | Yes | Yes | Critical | Permanent |
| observability.alert_triggered | observability_alert | Trigger operational health alert | No | No | Yes | Medium | Standard |
