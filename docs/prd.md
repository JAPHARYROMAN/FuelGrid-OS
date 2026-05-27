# FuelGrid OS — Full Product Requirements Document

## Version

**Document:** Full Product Requirements Document
**Product:** FuelGrid OS
**Category:** Fuel Operations Operating System
**Status:** Master PRD Draft

---

# 1. Executive Summary

FuelGrid OS is a full-fledged fuel business operating system designed for single stations, multi-station chains, depots, fleets, distributors, and enterprise fuel organizations. It unifies fuel inventory, tank and pump operations, shift management, sales, payments, finance, procurement, supplier management, customer credit, fleet fueling, risk detection, AI intelligence, reporting, audit, mobile operations, offline workflows, and hardware integrations into one command platform.

The system is not intended to be a small fuel management application. It is designed as the central operating layer for a fuel business, where every liter of fuel and every unit of money is traceable, auditable, and intelligently managed.

The product should serve a wide range of organizations, from a single fuel station to a national fuel chain with multiple regions, stations, depots, fleets, suppliers, and enterprise integrations.

---

# 2. Product Vision

## 2.1 Vision Statement

FuelGrid OS is the modern operating system for fuel businesses, controlling every liter, every transaction, every station, and every decision from one intelligent platform.

## 2.2 Mission

To give fuel businesses complete visibility, control, accountability, and intelligence across their operations, from fuel procurement and delivery to pump sales, cash reconciliation, customer credit, fleet fueling, risk detection, and executive decision-making.

## 2.3 Core Promise

FuelGrid OS helps fuel businesses answer the most important operational and financial questions:

* Where is every liter of fuel?
* Who received it?
* Who sold it?
* Which tank did it enter?
* Which pump dispensed it?
* Which attendant handled it?
* How much money should have been collected?
* How much money was actually submitted?
* Which stations are profitable?
* Which stations are losing fuel?
* Which customers owe money?
* Which deliveries have discrepancies?
* Which staff members show repeated shortages?
* Which tanks are at risk of stockout?
* What should management investigate today?

## 2.4 Product Philosophy

FuelGrid OS should behave like a financial ledger for physical fuel operations.

Every major business event should create a traceable record:

* Fuel received
* Fuel transferred
* Fuel sold
* Fuel adjusted
* Fuel lost
* Cash collected
* Cash submitted
* Cash deposited
* Credit issued
* Invoice generated
* Payment received
* Price changed
* Shift opened
* Shift closed
* Reading edited
* Approval granted
* Alert triggered
* Report generated

The master principle is:

**Every liter and every shilling must be traceable.**

---

# 3. Product Positioning

## 3.1 Product Category

FuelGrid OS belongs to a new product category:

**Fuel Operations OS**

It combines:

* Fuel Management System
* Station Management System
* Inventory Control System
* Sales and Payments System
* Finance and Reconciliation System
* Fleet Fuel Management System
* Depot Operations System
* Fraud and Risk Intelligence Platform
* Executive Analytics Platform
* Hardware Integration Platform

## 3.2 Positioning Statement

FuelGrid OS is a full-fledged fuel business operating system for single stations, multi-station chains, depots, fleet operators, distributors, and enterprise fuel organizations. It unifies station operations, fuel inventory, tank and pump management, sales, payments, finance, procurement, customer credit, fleet fueling, risk detection, AI intelligence, reporting, audit, and hardware integrations into one premium command platform.

## 3.3 Short Product Description

FuelGrid OS controls every liter, every transaction, every station, and every decision from one intelligent platform.

## 3.4 Tagline

**The operating system for modern fuel businesses.**

---

# 4. Target Customers

## 4.1 Independent Single Stations

Single fuel stations need a simple but powerful system to run daily operations, manage shifts, reconcile fuel, track sales, control cash, and produce reports.

### Needs

* Station dashboard
* Shift management
* Pump meter readings
* Tank readings
* Fuel deliveries
* Daily sales
* Cash reconciliation
* Expenses
* Credit customers
* Daily reports
* Simple alerts

### Main Job to Be Done

“I want to know whether today’s fuel, sales, and cash are correct.”

## 4.2 Multi-Station Chains

Fuel station chains need centralized control across many stations, regions, staff, tanks, pumps, suppliers, customers, and finance teams.

### Needs

* Multi-station command center
* Central pricing
* Regional management
* Station comparison
* Consolidated reports
* Fuel loss monitoring
* Central procurement
* Approval workflows
* Staff accountability
* Finance reconciliation

### Main Job to Be Done

“I want to control all my stations from one place.”

## 4.3 Fuel Depots and Bulk Storage Operators

Fuel depots need bulk storage, truck loading, stock ledger, receiving, dispatch, and reconciliation workflows.

### Needs

* Bulk tank management
* Truck loading
* Truck receiving
* Depot stock ledger
* Dispatch notes
* Supplier records
* Loading reconciliation
* Delivery variance
* Large-volume audit trails

### Main Job to Be Done

“I want to know exactly what entered, what left, and what remains.”

## 4.4 Fleet Operators

Fleet operators need to control vehicle fueling, driver behavior, fuel limits, odometer readings, and fuel consumption.

### Needs

* Vehicle fueling
* Driver tracking
* Fuel cards
* QR/RFID authorization
* Odometer capture
* Fuel consumption analytics
* Abuse detection
* Department allocation
* Project allocation

### Main Job to Be Done

“I want to control fuel consumption across my vehicles.”

## 4.5 Enterprise Fuel Organizations

Large enterprises need advanced integrations, custom workflows, AI intelligence, strong audit, forecasting, and multi-company support.

### Needs

* Multi-company structures
* Advanced role-based access control
* Hardware integrations
* ERP/accounting integrations
* AI analytics
* Compliance reporting
* Custom workflows
* API access
* Advanced audit
* Forecasting

### Main Job to Be Done

“I want a complete fuel operations command system.”

---

# 5. User Roles and Permissions

FuelGrid OS must be deeply role-based. Each role should have a tailored experience, permissions, dashboards, and workflows.

## 5.1 Attendant

### Responsibilities

* Operate assigned pump
* Record meter readings
* Record payment breakdowns where applicable
* Submit cash
* Submit shift notes
* Report incidents

### Interface

The attendant interface should be extremely simple:

* My Shift
* My Pump
* Opening Reading
* Closing Reading
* Expected Cash
* Submit Cash
* Shift Status
* Simple Alerts

### Restrictions

Attendants should not access:

* Company profit
* Other stations
* Fuel margins
* Price controls
* Audit logs
* User management
* System settings

## 5.2 Supervisor

### Responsibilities

* Assign attendants
* Open shifts
* Close shifts
* Verify readings
* Approve cash submissions
* Handle simple incidents

### Interface

* Active shifts
* Pump assignments
* Submitted readings
* Expected vs submitted cash
* Shift exceptions
* Pending approvals

## 5.3 Station Manager

### Responsibilities

* Manage daily station operations
* Receive fuel deliveries
* Review tank stock
* Approve shifts
* Close operating day
* Manage expenses
* Investigate variances
* Submit daily reports

### Interface

* Station dashboard
* Tanks
* Pumps
* Shifts
* Deliveries
* Sales
* Expenses
* Cash reconciliation
* Station reports
* Station alerts

## 5.4 Regional Manager

### Responsibilities

* Oversee multiple stations
* Compare station performance
* Investigate losses
* Approve exceptions
* Monitor stock levels
* Review station managers

### Interface

* Regional dashboard
* Station ranking
* Loss alerts
* Stockout risks
* Finance exceptions
* Performance trends

## 5.5 Finance Officer

### Responsibilities

* Reconcile revenue
* Track bank deposits
* Manage invoices
* Manage customer balances
* Manage supplier bills
* Review expenses
* Generate financial reports

### Interface

* Sales revenue
* Payment methods
* Cash submissions
* Deposits
* Customer invoices
* Supplier invoices
* Expenses
* Profit/loss
* Aging reports

## 5.6 Procurement Officer

### Responsibilities

* Monitor fuel demand
* Create purchase orders
* Manage suppliers
* Track delivery status
* Plan replenishment

### Interface

* Stock levels
* Forecasts
* Supplier records
* Purchase orders
* Delivery schedules
* Reorder recommendations

## 5.7 Auditor

### Responsibilities

* Review system integrity
* Investigate suspicious actions
* Export audit reports
* Trace record history

### Interface

* Audit logs
* Edit history
* Approval history
* Deleted or voided records
* Sensitive actions
* Risk events
* Compliance reports

### Restrictions

Auditors should usually be read-only and should not edit operational records unless explicitly granted special permissions.

## 5.8 Owner / CEO / Executive

### Responsibilities

* Monitor the whole business
* Review performance
* Identify losses
* Track profitability
* Make strategic decisions

### Interface

* Executive command center
* Network sales
* Stock position
* Profit/loss
* Station ranking
* Loss alerts
* Forecasts
* Credit exposure
* Management summaries
* AI insights

## 5.9 System Administrator

### Responsibilities

* Configure company structure
* Manage users
* Manage roles
* Set permissions
* Configure integrations
* Manage security settings

### Interface

* Users
* Roles
* Permissions
* Stations
* System settings
* Integrations
* API keys
* Audit settings

---

# 6. Product Editions and Operating Modes

FuelGrid OS should be one unified platform with different operating modes.

## 6.1 FuelGrid Station OS

For single stations.

### Includes

* Station dashboard
* Tank management
* Pump management
* Shift management
* Sales tracking
* Delivery receiving
* Cash reconciliation
* Expenses
* Daily close
* Reports
* Basic alerts

## 6.2 FuelGrid Chain OS

For multi-station operators.

### Includes Station OS plus

* Multi-station dashboard
* Regional management
* Central pricing
* Central procurement
* Station comparison
* Consolidated reports
* Approval workflows
* Network alerts
* Branch-level finance

## 6.3 FuelGrid Enterprise OS

For large operators.

### Includes Chain OS plus

* Advanced analytics
* AI assistant
* Hardware integrations
* ERP integrations
* Custom workflows
* Advanced audit
* API/webhooks
* Compliance reporting
* Forecasting engine
* Enterprise security

## 6.4 FuelGrid Fleet OS

For fleet-heavy businesses.

### Includes

* Vehicle fueling
* Driver authorization
* Fuel cards
* QR/RFID access
* Odometer capture
* Fuel consumption reports
* Route/project allocation
* Fleet abuse detection

## 6.5 FuelGrid Depot OS

For depots and bulk storage businesses.

### Includes

* Bulk tank management
* Truck loading
* Truck receiving
* Dispatch notes
* Depot stock ledger
* Loading reconciliation
* Depot delivery reports
* Bulk inventory controls

---

# 7. Product Scope

FuelGrid OS consists of 12 major operating layers.

1. Identity and Access OS
2. Company, Region and Station OS
3. Product, Tank and Pump OS
4. Shift and Station Operations OS
5. Fuel Inventory and Stock Ledger OS
6. Delivery, Procurement and Supplier OS
7. Sales, Payments and Revenue OS
8. Finance and Accounting Control OS
9. Customer Credit and Fleet Fuel OS
10. Risk, Fraud and Intelligence OS
11. AI Assistant and Executive Intelligence OS
12. Integrations, Hardware and API OS

---

# 8. Functional Requirements

## 8.1 Identity and Access OS

### Objective

Control who can access the system, what they can see, what actions they can perform, and which stations or companies they can manage.

### Requirements

#### User Accounts

The system must allow administrators to:

* Create users
* Edit users
* Deactivate users
* Assign users to companies
* Assign users to stations
* Assign users to regions
* Assign roles
* Require password resets
* View user activity
* Track user devices

#### Roles

The system must support default roles:

* Attendant
* Supervisor
* Station Manager
* Regional Manager
* Finance Officer
* Procurement Officer
* Auditor
* Executive
* System Administrator

The system must allow custom roles.

#### Permissions

The system must support fine-grained permissions including:

* Can view station dashboard
* Can open shift
* Can close shift
* Can approve shift
* Can edit meter reading
* Can approve stock adjustment
* Can change fuel price
* Can view profit margin
* Can manage credit customers
* Can export reports
* Can manage integrations
* Can view audit logs
* Can create users
* Can override credit limit
* Can delete records
* Can lock reporting period

#### Authentication

The system must support:

* Email/password login
* Secure password hashing
* Session management
* Two-factor authentication
* Password reset
* Device tracking
* Session timeout
* Optional single sign-on for enterprise customers

#### Access Boundaries

The system must enforce:

* Tenant isolation
* Company-level isolation
* Station-level access control
* Region-level access control
* Role-based module visibility
* Permission-based action control

---

## 8.2 Company, Region and Station OS

### Objective

Model the business hierarchy from tenant to company, region, station, tanks, pumps, nozzles, shifts, and transactions.

### Requirements

#### Tenant Management

The system must support multiple tenants. Each tenant must be isolated from other tenants.

#### Company Management

Administrators must be able to configure:

* Company name
* Legal name
* Registration number
* Tax information
* Contact information
* Currency
* Time zone
* Logo
* Business settings

#### Region Management

The system must support regions/zones for multi-station businesses.

Each region should include:

* Region name
* Region manager
* Stations assigned
* Performance dashboard
* Region-level reports

#### Station Management

Each station should include:

* Station name
* Station code
* Location
* Address
* Contact person
* Operating hours
* Assigned region
* Active products
* Tanks
* Pumps
* Staff
* Station status
* Licenses/documents

#### Hierarchy Navigation

The system must support drill-down navigation:

Tenant → Company → Region → Station → Tank → Pump → Nozzle → Shift → Transaction

---

## 8.3 Product, Tank and Pump OS

### Objective

Model all fuel products, tanks, pumps, nozzles, and physical dispensing infrastructure.

### Product Requirements

The system must support products such as:

* PMS / Petrol
* AGO / Diesel
* Kerosene
* LPG
* Lubricants
* AdBlue
* Shop items, optional

Each product must include:

* Product name
* Product code
* Category
* Unit of measure
* Default price
* Tax configuration
* Density/temperature settings
* Loss tolerance
* Active/inactive status

### Tank Requirements

Each tank must include:

* Tank name
* Station
* Product
* Capacity
* Safe minimum level
* Safe maximum level
* Dead stock level
* Current volume
* Opening stock
* Closing stock
* Dip chart
* Sensor mapping
* Calibration information
* Water level tracking
* Temperature tracking
* Status

### Pump Requirements

Each pump must include:

* Pump number
* Station
* Status
* Manufacturer/model
* Serial number
* Assigned tank
* Nozzles
* Meter readings
* Maintenance status
* Last calibration date

### Nozzle Requirements

Each nozzle must include:

* Nozzle number
* Pump
* Product
* Tank source
* Opening meter reading
* Closing meter reading
* Liters dispensed
* Price per liter
* Sales value
* Assigned attendant

---

## 8.4 Shift and Station Operations OS

### Objective

Allow stations to run daily operations through structured shift and daily close workflows.

### Shift Requirements

The system must support:

* Shift templates
* Shift opening
* Shift closing
* Attendant assignments
* Pump assignments
* Opening meter readings
* Closing meter readings
* Opening tank readings
* Closing tank readings
* Cash submission
* Supervisor approval
* Shift notes
* Shift incidents
* Shift exceptions

### Daily Operations Workflow

The system must support the following workflow:

1. Open operating day
2. Open shift
3. Assign attendants
4. Record opening meter readings
5. Record opening tank dips
6. Sell fuel
7. Receive deliveries
8. Record expenses
9. Close shift
10. Submit cash
11. Supervisor approval
12. Close day
13. Reconcile stock
14. Reconcile cash
15. Generate daily report
16. Lock day

### Shift Close Output

At shift close, the system must calculate:

* Total liters sold
* Expected cash
* Cash submitted
* Mobile money total
* Card payment total
* Credit sales total
* Expenses during shift
* Shortage/excess
* Pump variances
* Approval status

---

## 8.5 Fuel Inventory and Stock Ledger OS

### Objective

Maintain a traceable, auditable stock ledger for every fuel movement.

### Stock Movement Types

The system must support:

* Opening stock
* Delivery received
* Pump sale
* Manual adjustment
* Transfer in
* Transfer out
* Tank correction
* Evaporation/loss
* Calibration correction
* Stock write-off
* Closing stock

### Stock Formula

The system must calculate:

Expected Closing Stock = Opening Stock + Deliveries + Transfers In - Sales - Transfers Out - Adjustments

Then:

Variance = Actual Closing Stock - Expected Closing Stock

### Inventory Requirements

The system must support:

* Real-time stock ledger
* Tank stock
* Product stock
* Station stock
* Company-wide stock
* Stock movement history
* Delivery stock posting
* Sales stock depletion
* Manual adjustments
* Adjustment approvals
* Tolerance rules
* Stock reconciliation
* Variance analysis
* Runout prediction
* Reorder recommendations

### Ledger Requirement

The stock ledger must be immutable. Corrections must be made through adjustment entries rather than silent edits.

---

## 8.6 Delivery, Procurement and Supplier OS

### Objective

Manage fuel purchasing, supplier relationships, deliveries, receiving, and delivery reconciliation.

### Supplier Requirements

The system must support:

* Supplier profiles
* Depot locations
* Supplier contacts
* Supply contracts
* Price agreements
* Payment terms
* Supplier performance
* Supplier invoice history

### Purchase Order Requirements

The system must support:

* Purchase order creation
* Product selection
* Quantity requested
* Supplier assignment
* Station/depot destination
* Expected delivery date
* Approval workflow
* Purchase status
* Cost tracking

### Delivery Requirements

Each delivery must include:

* Delivery note number
* Supplier
* Truck number
* Driver
* Product
* Loaded quantity
* Delivered quantity
* Receiving station
* Receiving tank
* Before-delivery dip
* After-delivery dip
* Temperature-adjusted quantity
* Document attachments
* Delivery variance
* Approval status
* Supplier invoice link

### Delivery Workflow

The system must support:

1. Create purchase order
2. Approve purchase order
3. Confirm supplier loading
4. Dispatch truck
5. Receive truck at station
6. Record before-delivery tank dip
7. Discharge fuel
8. Record after-delivery tank dip
9. Upload delivery note
10. Calculate received quantity
11. Compare ordered, loaded, and received quantity
12. Approve or flag delivery
13. Update stock ledger
14. Create supplier payable

---

## 8.7 Sales, Payments and Revenue OS

### Objective

Capture and reconcile all revenue from fuel sales, credit sales, fleet sales, and related sales.

### Sale Types

The system must support:

* Pump sale
* Cash sale
* Mobile money sale
* Card sale
* Credit sale
* Voucher sale
* Fleet account sale
* Internal company vehicle fueling
* Lubricant sale
* Shop sale
* Bulk sale

### Payment Types

The system must support:

* Cash
* Mobile money
* Card
* Bank transfer
* Credit account
* Voucher
* Fuel card
* QR authorization
* RFID authorization
* Mixed payment

### Sales Requirements

The system must support:

* Pump sales calculation
* Manual sale entry
* POS integration
* Receipt generation
* Invoice generation
* Daily sales summary
* Sales by product
* Sales by station
* Sales by attendant
* Sales by payment method
* Sales by customer
* Sales by shift
* Void/correction workflow

### Revenue Reconciliation

The system must compare:

* Expected revenue
* Submitted cash
* Mobile money settlement
* Card settlement
* Credit sales
* Bank deposits
* Expenses deducted
* Shortage/excess

---

## 8.8 Finance and Accounting Control OS

### Objective

Transform operations into financial control and reporting.

### Finance Requirements

The system must support:

* Cash reconciliation
* Bank deposit tracking
* Mobile money settlement tracking
* Card settlement tracking
* Credit customer balances
* Customer invoices
* Supplier bills
* Expenses
* Petty cash
* Staff shortages
* Revenue reports
* Cost of goods sold
* Gross margin
* Net margin
* Profit/loss by station
* Profit/loss by product
* Profit/loss by region
* Tax reports
* Accounting exports

### Financial Objects

The system must support:

* Cash submission
* Bank deposit
* Customer invoice
* Supplier bill
* Expense
* Payment receipt
* Credit note
* Debit note
* Journal export
* Settlement batch

### Daily Finance Close

The system must calculate:

* Total sales
* Cash sales
* Mobile money sales
* Card sales
* Credit sales
* Expenses
* Expected deposit
* Actual deposit
* Shortage/excess
* Approved adjustments
* Final daily revenue

---

## 8.9 Customer Credit and Fleet Fuel OS

### Objective

Manage credit customers, vehicle fueling, driver authorization, fuel cards, and fleet consumption.

### Customer Requirements

The system must support:

* Customer profiles
* Customer contacts
* Credit limits
* Payment terms
* Customer stations
* Approved products
* Approved vehicles
* Approved drivers
* Customer balances
* Invoices
* Statements
* Payments
* Aging reports
* Account suspension

### Fleet Requirements

The system must support:

* Vehicle profiles
* Driver profiles
* Fuel limits
* Daily/monthly limits
* Odometer capture
* Fuel authorization
* QR code fueling
* RFID card fueling
* Fuel card management
* Consumption analysis
* Abnormal fuel usage alerts
* Department/project allocation

### Credit Fueling Workflow

The system must support:

1. Identify customer
2. Verify account status
3. Verify credit limit
4. Verify vehicle
5. Verify driver
6. Capture odometer
7. Authorize product
8. Dispense fuel
9. Record transaction
10. Update customer balance
11. Add to invoice
12. Monitor aging

---

## 8.10 Risk, Fraud and Intelligence OS

### Objective

Detect losses, fraud, abnormal behavior, suspicious changes, and operational risks.

### Risk Areas

The system must monitor:

* Fuel loss
* Cash shortage
* Delivery shortage
* Pump manipulation
* Tank leakage
* Suspicious edits
* Backdated entries
* Unusual credit sales
* Customer limit abuse
* Attendant shortage patterns
* Repeated voids
* Unauthorized price changes
* Stock adjustments

### Risk Engine Requirements

The system must support:

* Anomaly detection
* Rule-based alerts
* Risk scoring
* Variance classification
* Suspicious activity detection
* Staff behavior analytics
* Station risk ranking
* Delivery discrepancy detection
* Customer risk scoring
* Automated recommendations
* Investigation workflows

### Example Risk Alert

Critical Alert

Station: Mikocheni
Product: PMS
Issue: Abnormal negative variance
Estimated loss: 1,240 L
Estimated value: TZS 3,968,000

Pattern:

* Losses occurred during 5 of the last 7 evening shifts.
* Pump 03 appears in 68% of related variance events.
* The same attendant was assigned during 4 incidents.

Recommended action:

1. Recalibrate Pump 03.
2. Audit evening shift cash submissions.
3. Confirm tank dip readings.
4. Require supervisor approval for future evening closes.

---

## 8.11 AI Assistant and Executive Intelligence OS

### Objective

Provide a permission-aware AI assistant that helps users understand operations, explain variances, generate reports, investigate risk, and make decisions.

### AI Assistant Requirements

Users should be able to ask:

* Why did PMS losses increase this week?
* Which stations need fuel tomorrow?
* Which attendants had shortages this month?
* Which customers are overdue?
* Show stations with declining sales.
* Generate a monthly performance report.
* Explain yesterday’s tank variance.
* Which station is most profitable?
* Which delivery had the biggest discrepancy?
* What should I investigate today?

### AI Data Access

The AI assistant should access, based on user permissions:

* Sales data
* Stock data
* Tank data
* Pump data
* Shift data
* Delivery data
* Finance data
* Customer data
* Fleet data
* Audit logs
* Alerts
* Reports
* Forecasts

### AI Output

The AI should produce:

* Plain-language explanations
* Operational summaries
* Executive summaries
* Investigation recommendations
* Report drafts
* Forecast explanations
* Risk summaries
* Station comparisons

### AI Governance Rules

The AI must:

* Respect user permissions
* Never expose restricted financial data
* Cite internal system records where possible
* Avoid unsupported claims
* Explain uncertainty
* Recommend actions, not perform unauthorized actions
* Log AI queries for audit

---

## 8.12 Integrations, Hardware and API OS

### Objective

Connect FuelGrid OS to hardware, financial systems, communication providers, and external enterprise software.

### Hardware Integrations

The system should support integrations with:

* Pump controllers
* Automatic tank gauges
* Tank sensors
* POS systems
* Receipt printers
* QR scanners
* RFID readers
* Fuel card terminals
* Weighbridge systems
* GPS/fleet devices

### Financial Integrations

The system should support:

* Mobile money providers
* Banks
* Card processors
* Accounting systems
* ERP systems
* Tax/fiscal systems
* Payment gateways

### Communication Integrations

The system should support:

* SMS
* Email
* WhatsApp
* Push notifications
* Telegram, optional

### Developer Platform

The system should support:

* REST API
* gRPC API
* Webhooks
* API keys
* OAuth applications
* Integration logs
* Webhook retry system
* Rate limits
* Sandbox environment

### Integration Architecture

The system should use an adapter-based architecture:

* Integration Core
* Pump Adapter
* Tank Gauge Adapter
* Mobile Money Adapter
* Accounting Adapter
* Bank Adapter
* POS Adapter

---

# 9. Main Workflows

## 9.1 Station Daily Close Workflow

1. Open operating day
2. Open first shift
3. Assign attendants
4. Record opening pump readings
5. Record opening tank readings
6. Operate sales
7. Receive deliveries, if any
8. Record expenses, if any
9. Close shift
10. Submit attendant cash
11. Approve or reject shift
12. Open next shift
13. Repeat
14. Close final shift
15. Record closing tank readings
16. Reconcile stock
17. Reconcile revenue
18. Review alerts
19. Approve daily close
20. Lock day
21. Generate daily report

## 9.2 Pump Sales Workflow

1. Assign pump to attendant
2. Capture opening meter
3. Capture closing meter
4. Calculate liters sold
5. Apply product price
6. Calculate expected revenue
7. Split by payment method
8. Compare expected vs submitted
9. Flag shortage/excess
10. Post to stock ledger
11. Post to revenue ledger

## 9.3 Fuel Delivery Workflow

1. Create purchase order
2. Approve purchase order
3. Supplier loads fuel
4. Truck dispatched
5. Station receives truck
6. Record before-delivery tank dip
7. Discharge fuel
8. Record after-delivery tank dip
9. Attach documents
10. Compare ordered, loaded, and received quantities
11. Approve or dispute delivery
12. Update stock ledger
13. Create supplier bill
14. Generate delivery variance report

## 9.4 Stock Reconciliation Workflow

1. Get opening stock
2. Add deliveries
3. Subtract sales
4. Apply transfers/adjustments
5. Calculate expected closing stock
6. Capture actual closing stock
7. Calculate variance
8. Compare against tolerance
9. Classify variance
10. Create alert if abnormal
11. Require approval if material
12. Lock reconciliation

## 9.5 Cash Reconciliation Workflow

1. Calculate expected cash
2. Record cash submitted
3. Record mobile money payments
4. Record card payments
5. Record credit sales
6. Record expenses
7. Calculate shortage/excess
8. Supervisor review
9. Finance approval
10. Bank deposit matching
11. Lock revenue day

## 9.6 Credit Customer Workflow

1. Create customer account
2. Set credit limit
3. Add vehicles/drivers
4. Set product restrictions
5. Customer fuels vehicle
6. Verify authorization
7. Record transaction
8. Update balance
9. Generate invoice
10. Receive payment
11. Update aging report
12. Suspend if overdue or over limit

## 9.7 Investigation Workflow

1. Alert triggered
2. Assign investigator
3. Review related shifts
4. Review pump readings
5. Review tank readings
6. Review attendants
7. Review delivery records
8. Add investigation notes
9. Attach evidence
10. Mark root cause
11. Apply corrective action
12. Close case

---

# 10. Data Model Requirements

## 10.1 Identity and Tenant Entities

* tenants
* companies
* regions
* stations
* users
* roles
* permissions
* user_roles
* role_permissions
* user_station_access
* sessions
* devices

## 10.2 Fuel Infrastructure Entities

* products
* tanks
* tank_calibration_charts
* tank_readings
* tank_sensor_readings
* pumps
* nozzles
* pump_meter_readings
* pump_calibrations

## 10.3 Operations Entities

* operating_days
* shifts
* shift_assignments
* shift_pumps
* shift_cash_submissions
* shift_approvals
* daily_closes
* incidents
* maintenance_requests

## 10.4 Inventory Entities

* stock_movements
* stock_reconciliations
* stock_adjustments
* stock_transfers
* delivery_receipts
* delivery_variances
* inventory_snapshots

## 10.5 Procurement Entities

* suppliers
* supplier_contracts
* purchase_orders
* purchase_order_items
* deliveries
* delivery_documents
* supplier_bills
* supplier_payments

## 10.6 Sales and Payments Entities

* sales
* sale_items
* payments
* payment_methods
* cash_collections
* mobile_money_transactions
* card_transactions
* bank_deposits
* settlement_batches
* voids
* refunds

## 10.7 Customer and Fleet Entities

* customers
* customer_accounts
* customer_contacts
* customer_vehicles
* drivers
* fuel_authorizations
* fuel_cards
* fleet_transactions
* customer_invoices
* customer_invoice_items
* customer_payments
* customer_statements

## 10.8 Finance Entities

* expenses
* expense_categories
* petty_cash_accounts
* cash_reconciliations
* financial_periods
* profit_loss_snapshots
* tax_reports
* journal_exports

## 10.9 Risk, Alerts and Audit Entities

* alerts
* alert_rules
* risk_events
* risk_scores
* investigations
* investigation_notes
* audit_logs
* approval_requests
* approval_steps
* record_locks

## 10.10 AI, Reporting and Integration Entities

* reports
* report_templates
* scheduled_reports
* ai_queries
* ai_insight_snapshots
* forecast_snapshots
* integrations
* integration_events
* api_keys
* webhooks
* webhook_deliveries
* sync_events
* offline_devices

---

# 11. UI/UX Requirements

## 11.1 Design Objective

FuelGrid OS must look and feel like a premium command center, not traditional enterprise software.

The interface should be:

* Beautiful
* Calm
* Fast
* Clear
* Executive-grade
* Simple for attendants
* Powerful for managers
* Detailed for finance and auditors

## 11.2 Visual Direction

The product should combine:

* Apple-level simplicity
* Tesla-style dashboard confidence
* Stripe-like financial clarity
* Linear-style workflow polish
* Bloomberg-level operational depth

## 11.3 Color System

Recommended palette:

* Primary: Deep Navy
* Secondary: Slate / Charcoal
* Accent: Electric Blue
* Success: Emerald
* Warning: Amber
* Danger: Red
* Info: Cyan
* PMS/Petrol: Orange or Red
* Diesel: Blue or Green
* Kerosene: Purple or Gray
* Neutral: Zinc / Slate

## 11.4 Layout System

The main web layout should include:

* Left Sidebar
* Top Command Bar
* Main Workspace
* Right Insight Panel
* Global Command Palette
* Notification Center

## 11.5 Main Navigation

The full navigation should include:

* Command Center
* Stations
* Tanks
* Pumps
* Shifts
* Sales
* Inventory
* Deliveries
* Customers
* Fleet
* Finance
* Procurement
* Reports
* Alerts
* AI Assistant
* Audit
* Integrations
* Settings

Navigation must be role-aware. Attendants should not see enterprise-level modules.

## 11.6 Executive Command Center

The executive dashboard must show:

* Total revenue
* Total liters sold
* Gross margin
* Net margin
* Current stock
* Stockout risk
* Fuel losses
* Cash reconciliation status
* Credit exposure
* Station ranking
* Regional performance
* Critical alerts
* Forecasts
* AI executive summary

## 11.7 Station Dashboard

The station dashboard must show:

* Today’s sales
* Liters sold
* Current tank levels
* Open shifts
* Pending approvals
* Expected cash
* Submitted cash
* Expenses
* Deliveries
* Variance
* Alerts
* Daily close status

## 11.8 Tank View

The tank view must show:

* Tank visual level
* Capacity
* Current volume
* Safe minimum
* Safe maximum
* Ullage
* Opening stock
* Deliveries
* Sales depletion
* Expected closing
* Actual closing
* Variance
* Water level
* Temperature
* Runout prediction
* Reading history

## 11.9 Pump View

The pump view must show:

* Pump status
* Nozzle status
* Assigned product
* Assigned tank
* Opening meter
* Closing meter
* Liters sold
* Expected revenue
* Assigned attendant
* Calibration status
* Maintenance status

## 11.10 Shift View

The shift view must show:

* Shift timeline
* Assigned attendants
* Assigned pumps
* Opening readings
* Closing readings
* Expected cash
* Cash submitted
* Payment breakdown
* Shortage/excess
* Supervisor approval
* Shift notes
* Exceptions

## 11.11 Delivery View

The delivery view must show:

* Purchase order
* Supplier
* Truck
* Driver
* Product
* Loaded quantity
* Delivered quantity
* Receiving tank
* Before dip
* After dip
* Variance
* Documents
* Approval status
* Supplier bill status

## 11.12 Finance Dashboard

The finance dashboard must show:

* Revenue
* Cash position
* Deposits
* Mobile money settlements
* Card settlements
* Credit sales
* Expenses
* Supplier bills
* Customer balances
* Profit/loss
* Aging report
* Finance exceptions

## 11.13 Risk Center

The risk center must show:

* Critical alerts
* Risk score by station
* Fuel loss trends
* Cash shortage trends
* Suspicious edits
* Delivery discrepancies
* Attendant risk patterns
* Customer credit risk
* Open investigations
* Closed investigations

## 11.14 AI Assistant Interface

The AI assistant must include:

* Chat interface
* Suggested questions
* Data-backed answers
* Charts/tables when needed
* Source references
* Recommended actions
* Report generation
* Investigation mode

---

# 12. Mobile Requirements

## 12.1 Objective

FuelGrid OS must provide a mobile-first field operations experience.

## 12.2 Mobile Users

* Attendants
* Supervisors
* Station managers
* Delivery receivers
* Auditors
* Fleet drivers, optional

## 12.3 Mobile Features

The mobile app must support:

* Open shift
* View assigned pump
* Enter meter readings
* Enter tank dips
* Submit cash
* Capture delivery note photo
* Record expenses
* Approve shift
* Receive alerts
* Scan QR/RFID
* Authorize fleet fueling
* Work offline
* Sync later

## 12.4 Mobile Design Principles

The mobile UI must use:

* Big buttons
* Large numbers
* Minimal typing
* Clear status
* Fast loading
* Offline support
* Step-by-step workflows

---

# 13. Offline-First Requirements

## 13.1 Objective

FuelGrid OS must continue operating when stations have unstable internet.

## 13.2 Offline Features

The system must support offline:

* Opening shifts
* Entering meter readings
* Entering tank readings
* Recording expenses
* Receiving deliveries
* Capturing photos
* Submitting cash
* Closing shifts
* Queueing approvals
* Syncing later

## 13.3 Sync Requirements

The system must support:

* Local encrypted storage
* Background sync
* Conflict detection
* Conflict resolution UI
* Offline audit trail
* Device identity
* Sync status indicator
* Retry system
* Server-side validation

## 13.4 Conflict Resolution

If two users edit the same reading, the system must:

* Preserve both attempts
* Show who entered what
* Require supervisor resolution
* Create audit log
* Apply approved value only

---

# 14. Reporting Requirements

## 14.1 Daily Reports

The system must generate:

* Daily sales report
* Daily stock report
* Daily shift report
* Daily cash report
* Daily tank variance report
* Daily delivery report
* Daily expense report
* Daily close report

## 14.2 Monthly Reports

The system must generate:

* Monthly sales report
* Monthly station performance
* Monthly fuel loss report
* Monthly customer credit report
* Monthly supplier report
* Monthly profit/loss report
* Monthly staff performance
* Monthly procurement report

## 14.3 Executive Reports

The system must generate:

* Network performance report
* Station ranking report
* Regional comparison report
* Fuel loss intelligence report
* Forecasting report
* Profitability report
* Credit exposure report
* Risk report

## 14.4 Audit Reports

The system must generate:

* Record edit history
* Deleted/voided records
* Price changes
* Stock adjustments
* Approval history
* User activity report
* Suspicious actions

## 14.5 Export Formats

Reports must export to:

* PDF
* Excel
* CSV
* Email
* Dashboard link
* Scheduled report
* API export

---

# 15. Security Requirements

## 15.1 Core Security

The system must support:

* Role-based access control
* Permission-based actions
* Two-factor authentication
* Password policies
* Device tracking
* Session expiration
* IP logging
* Audit logs
* Record locking
* Approval workflows
* Encrypted data at rest
* Encrypted data in transit
* Secure API keys
* Webhook signing
* Backup and restore
* Least privilege access

## 15.2 Sensitive Actions Requiring Audit

The following actions must always create audit records:

* Price change
* Stock adjustment
* Meter reading edit
* Tank reading edit
* Cash submission edit
* Shift reopening
* Daily close reversal
* Credit limit override
* Customer suspension removal
* Supplier bill edit
* User permission change
* Integration setting change
* Record deletion

## 15.3 Record Locking

After a day is closed:

* Records must be locked
* Changes must require reopening approval
* Corrections must create adjustment records
* Original records must remain visible
* Audit trail must remain permanent

---

# 16. Technical Requirements

## 16.1 Frontend Stack

Recommended frontend stack:

* Next.js
* React
* TypeScript
* Tailwind CSS
* shadcn/ui
* TanStack Query
* TanStack Table
* Zustand
* React Hook Form
* Zod
* Recharts
* Framer Motion
* PWA support

## 16.2 Backend Stack

Recommended backend stack:

* Go
* gRPC
* REST gateway
* PostgreSQL
* Redis
* Kafka or NATS
* ClickHouse
* Object Storage
* OpenTelemetry
* Docker
* Kubernetes-ready architecture

## 16.3 Database Strategy

* PostgreSQL for core transactional data
* Redis for cache, sessions, queues, and locks
* ClickHouse for analytics, events, and high-volume reporting
* Object Storage for documents, photos, and receipts
* Event Store for audit and immutable business actions

## 16.4 Service Architecture

The system should be designed around these domains:

* identity-service
* tenant-service
* station-service
* product-service
* tank-service
* pump-service
* shift-service
* sales-service
* inventory-service
* delivery-service
* procurement-service
* finance-service
* customer-service
* fleet-service
* pricing-service
* alert-service
* risk-service
* forecasting-service
* reporting-service
* audit-service
* notification-service
* integration-service
* document-service
* ai-assistant-service
* offline-sync-service

The first implementation may be a modular monolith with strict service boundaries, but the architecture must allow future service extraction.

---

# 17. Event-Driven Requirements

FuelGrid OS must be event-driven.

## 17.1 Required Events

The system should emit events for:

* ShiftOpened
* ShiftClosed
* MeterReadingRecorded
* TankReadingRecorded
* DeliveryReceived
* StockMovementCreated
* StockReconciled
* CashSubmitted
* CashReconciled
* PriceChanged
* CreditSaleRecorded
* InvoiceGenerated
* PaymentReceived
* AlertTriggered
* ApprovalRequested
* ApprovalGranted
* ApprovalRejected
* RecordEdited
* DailyCloseCompleted

## 17.2 Event Benefits

Events support:

* Auditability
* Reporting
* Real-time alerts
* AI insight generation
* Integration webhooks
* Historical replay
* Fraud detection

---

# 18. Forecasting Requirements

## 18.1 Objective

FuelGrid OS must predict future fuel demand, stockout risk, and replenishment needs.

## 18.2 Forecasting Features

The system must support:

* Demand forecasting
* Stockout prediction
* Reorder recommendation
* Delivery scheduling
* Sales trend forecasting
* Seasonal demand analysis
* Station performance forecast
* Customer consumption forecast

## 18.3 Forecast Output

Forecasts should include:

* Product
* Station
* Current stock
* Predicted runout date/time
* Recommended order quantity
* Recommended delivery window
* Confidence level
* Explanation

---

# 19. Alert and Notification Requirements

## 19.1 Alert Types

The system must support alerts for:

* Low stock
* Stockout risk
* Fuel variance
* Cash shortage
* Delivery shortage
* Suspicious edit
* Shift not closed
* Daily close overdue
* Credit limit exceeded
* Customer overdue
* Pump offline
* Tank sensor warning
* Price change approval needed
* Supplier delivery delayed

## 19.2 Notification Channels

The system should support:

* In-app
* Email
* SMS
* WhatsApp
* Push notification
* Webhook

## 19.3 Alert Severity

Alert severity levels:

* Info
* Low
* Medium
* High
* Critical

## 19.4 Alert Lifecycle

Alert statuses:

* Created
* Acknowledged
* Assigned
* Investigating
* Resolved
* Dismissed
* Escalated

---

# 20. Audit and Compliance Requirements

## 20.1 Audit Log Fields

Audit logs must capture:

* Actor
* Action
* Entity
* Previous value
* New value
* Timestamp
* Device
* IP address
* Location, if available
* Reason
* Approval reference

## 20.2 Compliance Features

The system must support:

* Daily close locking
* Financial period locking
* Exportable audit reports
* User activity reports
* Approval logs
* Tax reports
* Document attachments
* Data retention settings

---

# 21. API and Webhook Requirements

## 21.1 API Requirements

The system must provide APIs for:

* Users
* Companies
* Stations
* Products
* Tanks
* Pumps
* Shifts
* Sales
* Payments
* Deliveries
* Inventory
* Customers
* Fleet
* Finance
* Reports
* Alerts
* Audit
* Integrations

## 21.2 Webhook Events

The system should support webhook events such as:

* sale.created
* delivery.received
* stock.low
* shift.closed
* invoice.generated
* payment.received
* alert.critical
* stock.reconciled
* price.changed

## 21.3 API Security

The system must support:

* API keys
* OAuth for enterprise integrations
* Rate limiting
* Webhook signing
* Access scopes
* Integration audit logs

---

# 22. Non-Functional Requirements

## 22.1 Performance

The system should:

* Load dashboards quickly
* Support real-time updates where needed
* Handle high-volume transaction data
* Support many stations per tenant
* Support analytics without slowing operations

## 22.2 Reliability

The system should:

* Prevent data loss
* Support retries for failed events
* Support backups
* Support disaster recovery
* Handle offline sync safely

## 22.3 Scalability

The system should support growth from:

* 1 station to hundreds of stations
* Dozens of users to thousands of users
* Manual entry to hardware-integrated operations
* Basic reports to advanced analytics

## 22.4 Maintainability

The system should use:

* Clear domain boundaries
* Strong typing
* Clean APIs
* Automated tests
* Versioned migrations
* Observability
* Documentation

## 22.5 Observability

The system should support:

* Structured logs
* Metrics
* Distributed tracing
* Error tracking
* Audit dashboards
* Integration health dashboards

---

# 23. Implementation Phases

These are full system construction phases, not MVP phases.

## Phase 1: Platform Foundation

* Monorepo setup
* Design system
* App shell
* Authentication
* Multi-tenancy
* Company setup
* Region setup
* Station setup
* User management
* RBAC permissions
* Audit foundation
* API foundation
* Database migrations
* Core dashboard shell

## Phase 2: Fuel Infrastructure Core

* Product management
* Tank management
* Pump management
* Nozzle management
* Tank-to-pump mapping
* Calibration data
* Station infrastructure dashboard
* Basic tank and pump status

## Phase 3: Station Operations Core

* Operating day
* Shift opening
* Shift closing
* Attendant assignment
* Pump assignment
* Opening meter readings
* Closing meter readings
* Opening tank readings
* Closing tank readings
* Supervisor approvals
* Daily close workflow
* Station dashboard

## Phase 4: Inventory and Reconciliation Engine

* Stock ledger
* Stock movement events
* Delivery stock posting
* Sales stock depletion
* Tank reconciliation
* Product reconciliation
* Variance calculations
* Tolerance rules
* Stock adjustment approvals
* Stock reports

## Phase 5: Delivery and Procurement OS

* Supplier management
* Purchase orders
* Purchase approvals
* Delivery receiving
* Truck/driver details
* Before/after dip workflow
* Delivery documents
* Delivery variance
* Supplier invoice creation
* Procurement dashboard

## Phase 6: Sales, Payments and Revenue OS

* Pump sales
* Manual sales
* Payment methods
* Cash sales
* Mobile money sales
* Card sales
* Credit sales
* Sales summaries
* Payment breakdown
* Revenue reports
* Void/correction workflow

## Phase 7: Finance and Accounting Control

* Cash reconciliation
* Bank deposits
* Expenses
* Petty cash
* Customer invoices
* Supplier bills
* Customer payments
* Supplier payments
* Profit/loss reports
* Aging reports
* Accounting exports

## Phase 8: Customer Credit and Fleet Fuel OS

* Customer profiles
* Credit limits
* Customer vehicles
* Drivers
* Fuel authorization
* Fuel cards
* QR/RFID workflows
* Odometer capture
* Fleet consumption reports
* Customer statements
* Credit risk alerts

## Phase 9: Chain and Enterprise Command

* Executive command center
* Regional dashboards
* Station ranking
* Central pricing
* Central procurement
* Network inventory
* Consolidated finance
* Multi-station reports
* Enterprise approvals

## Phase 10: Risk, Fraud and Intelligence

* Rule engine
* Alert rules
* Fuel loss detection
* Cash shortage detection
* Delivery discrepancy detection
* Suspicious edit detection
* Attendant risk patterns
* Station risk scores
* Investigation workflows
* Recommended actions

## Phase 11: Forecasting and Automation

* Demand forecasting
* Stockout prediction
* Reorder recommendations
* Automated alerts
* Scheduled reports
* Procurement planning
* Rule-based automation
* Escalation workflows

## Phase 12: AI Assistant

* AI chat interface
* Permission-aware queries
* Variance explanation
* Executive summaries
* Report generation
* Investigation assistant
* Forecast explanation
* Natural language analytics

## Phase 13: Hardware and External Integrations

* Pump controller adapters
* Tank gauge adapters
* POS integrations
* Mobile money integrations
* Bank integrations
* Accounting integrations
* ERP exports
* API keys
* Webhooks
* Integration logs

## Phase 14: Mobile and Offline OS

* Mobile attendant app
* Mobile supervisor app
* Offline storage
* Offline shift workflows
* Offline readings
* Offline delivery capture
* Sync engine
* Conflict resolution
* Device audit logs

## Phase 15: Enterprise Hardening

* Security hardening
* Observability
* OpenTelemetry
* Backups
* Disaster recovery
* Performance optimization
* Load testing
* Compliance reports
* Admin tools
* Data retention
* Record locking
* Advanced audit

---

# 24. Acceptance Criteria

FuelGrid OS is successful when the following outcomes are achieved.

## 24.1 Operational Acceptance

* A station can run its full operating day inside the system.
* A shift can be opened, managed, closed, approved, and audited.
* A station manager can complete daily close with stock and cash reconciliation.
* A multi-station owner can monitor all stations from one dashboard.

## 24.2 Inventory Acceptance

* Every fuel movement creates a stock ledger entry.
* Every delivery is reconciled.
* Every stock variance is calculated.
* Every material adjustment requires approval.
* Every closed period is locked.

## 24.3 Financial Acceptance

* Every sale connects to a payment method.
* Every cash submission is checked against expected revenue.
* Every credit customer has a balance.
* Every station can generate profit/loss reports.
* Finance can track deposits, settlements, expenses, and invoices.

## 24.4 Risk Acceptance

* The system detects abnormal fuel losses.
* The system detects cash shortages.
* The system flags suspicious edits.
* The system supports investigation workflows.
* The system generates risk scores for stations and users.

## 24.5 AI Acceptance

* Users can ask natural language questions.
* AI answers respect user permissions.
* AI explanations are based on system data.
* AI can explain variances, summarize performance, and recommend investigations.

## 24.6 Design Acceptance

* Attendants find the app simple.
* Station managers find it operationally useful.
* Finance users find it reliable.
* Auditors find it trustworthy.
* Executives find it beautiful and strategic.

---

# 25. Open Questions

The following decisions should be finalized during technical planning:

1. Which country-specific tax and compliance requirements should be supported first?
2. Which mobile money providers should be integrated first?
3. Which pump controller brands are most common in the target market?
4. Which automatic tank gauge systems should be supported first?
5. Should the first release support both cloud and on-premise deployments?
6. Should FuelGrid OS include built-in accounting or integrate with accounting systems first?
7. Should fleet fueling be core from day one or enabled as a module?
8. What exact offline conflict rules should be used for readings, deliveries, and cash?
9. Which reports are mandatory for the first production deployment?
10. What pricing model should be used: per station, per user, per module, or enterprise license?

---

# 26. Final Product Definition

FuelGrid OS is a full-fledged fuel operations operating system designed to manage the complete lifecycle of fuel businesses. It connects stations, tanks, pumps, shifts, deliveries, suppliers, customers, fleets, sales, payments, finance, inventory, risk, reporting, AI intelligence, and hardware integrations into one unified platform. It gives owners, managers, attendants, finance teams, auditors, and executives complete visibility and control over every liter of fuel and every unit of money.

Short version:

**FuelGrid OS is the intelligent command center for fuel businesses — controlling every liter, every transaction, every station, and every decision.**

One-line version:

**The modern operating system for fuel stations, chains, depots, fleets, and enterprise fuel operations.**
