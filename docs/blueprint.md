# **FuelGrid OS — Master Product Blueprint**

## **Full-Fledged Fuel Business Operating System**

---

# 1. Product Identity

## Product Name

## **FuelGrid OS**

### Tagline

**The operating system for modern fuel businesses.**

### Full Positioning Statement

**FuelGrid OS is a full-fledged fuel business operating system for single stations, multi-station chains, depots, fleet operators, distributors, and enterprise fuel organizations. It unifies station operations, fuel inventory, tank and pump management, sales, payments, finance, procurement, customer credit, fleet fueling, risk detection, rule-based intelligence, reporting, audit, and hardware integrations into one premium command platform.**

### Simple Positioning Statement

**FuelGrid OS controls every liter, every transaction, every station, and every decision from one intelligent platform.**

---

# 2. The Big Vision

FuelGrid OS should not be designed as a normal fuel management application.

It should be designed as the **central nervous system of a fuel business**.

It should answer the most important questions in fuel operations:

```text
Where is every liter of fuel?
Who sold it?
Who received it?
Who approved it?
How much money should we have?
How much money was actually submitted?
Which stations are profitable?
Which tanks are losing fuel?
Which attendants have shortages?
Which customers owe money?
Which deliveries have discrepancies?
Which stations need replenishment?
What should management investigate today?
```

The goal is to build a platform that is:

**Operational enough for attendants.**
**Powerful enough for station managers.**
**Financially serious enough for accountants.**
**Auditable enough for compliance teams.**
**Beautiful enough for executives.**
**Scalable enough for national chains.**

---

# 3. Product Category

FuelGrid OS sits at the intersection of several product categories:

```text
Fuel Management System
+ Station Management System
+ Inventory Control System
+ Finance & Reconciliation System
+ Fleet Fuel Management System
+ Depot Operations System
+ Fraud & Risk Intelligence Platform
+ Executive Analytics Platform
+ Hardware Integration Platform
```

The final category should be:

## **Fuel Operations OS**

or

## **Petroleum Retail & Distribution Operating System**

---

# 4. Core Operating Philosophy

FuelGrid OS should be built around one master principle:

## **Every liter and every shilling must be traceable.**

That means every important movement in the system must create a record:

```text
Fuel received
Fuel transferred
Fuel sold
Fuel adjusted
Fuel lost
Cash collected
Cash submitted
Cash deposited
Credit issued
Invoice generated
Payment received
Price changed
Shift opened
Shift closed
Reading edited
Approval granted
Alert triggered
Report generated
```

FuelGrid OS should behave like a **financial ledger for physical fuel operations**.

---

# 5. Target Users

FuelGrid OS should support multiple types of fuel businesses.

## 5.1 Single Gas Stations

For independent station owners and managers.

They need:

```text
Shift control
Pump readings
Tank readings
Deliveries
Daily sales
Cash reconciliation
Expenses
Credit customers
Daily reports
Simple alerts
```

Their main goal:

> “I want to know whether today’s fuel, sales, and cash are correct.”

---

## 5.2 Multi-Station Chains

For companies operating several fuel stations.

They need:

```text
Multi-station dashboard
Central pricing
Regional management
Station comparison
Consolidated reports
Fuel loss monitoring
Central procurement
Approval workflows
Staff accountability
Finance reconciliation
```

Their main goal:

> “I want to control all my stations from one place.”

---

## 5.3 Fuel Depots and Bulk Storage Operators

For companies storing or distributing large fuel volumes.

They need:

```text
Bulk tank management
Truck loading
Truck receiving
Depot stock ledger
Dispatch notes
Supplier records
Loading reconciliation
Delivery variance
Large-volume audit trails
```

Their main goal:

> “I want to know exactly what entered, what left, and what remains.”

---

## 5.4 Fleet Operators

For transport, logistics, mining, construction, agriculture, and corporate fleets.

They need:

```text
Vehicle fueling
Driver tracking
Fuel cards
QR/RFID authorization
Odometer capture
Fuel consumption analytics
Abuse detection
Department allocation
Project allocation
```

Their main goal:

> “I want to control fuel consumption across my vehicles.”

---

## 5.5 Enterprise Fuel Organizations

For large businesses with stations, depots, fleets, suppliers, and complex finance.

They need:

```text
Multi-company structures
Advanced RBAC
Hardware integrations
ERP/accounting integrations
Automated analytics
Compliance reporting
Custom workflows
API access
Advanced audit
Forecasting
```

Their main goal:

> “I want a complete fuel operations command system.”

---

# 6. User Roles

FuelGrid OS should be deeply role-based. Every role should see only what matters to them.

---

## 6.1 Attendant

### Main Responsibilities

```text
Operate assigned pump
Record meter readings
Record sales/payment information
Submit cash
Submit shift notes
Report incidents
```

### Should See

```text
My shift
My pump
Opening reading
Closing reading
Expected cash
Submit cash
Shift status
Simple alerts
```

### Should Not See

```text
Company profit
Other station data
Fuel margins
Price change controls
Audit logs
User management
```

---

## 6.2 Supervisor

### Main Responsibilities

```text
Assign attendants
Open shifts
Close shifts
Verify readings
Approve cash submissions
Handle simple incidents
```

### Should See

```text
Active shifts
Pump assignments
Submitted readings
Expected vs submitted cash
Shift exceptions
Pending approvals
```

---

## 6.3 Station Manager

### Main Responsibilities

```text
Manage daily station operations
Receive deliveries
Review tank stock
Approve shifts
Close day
Manage expenses
Investigate variances
Submit daily report
```

### Should See

```text
Station dashboard
Tanks
Pumps
Shifts
Deliveries
Sales
Expenses
Cash reconciliation
Station reports
Station alerts
```

---

## 6.4 Regional Manager

### Main Responsibilities

```text
Oversee multiple stations
Compare performance
Investigate losses
Approve exceptions
Monitor stock levels
Review station managers
```

### Should See

```text
Regional dashboard
Station ranking
Loss alerts
Stockout risks
Finance exceptions
Performance trends
```

---

## 6.5 Finance Officer

### Main Responsibilities

```text
Reconcile revenue
Track bank deposits
Manage invoices
Manage customer balances
Manage supplier bills
Review expenses
Generate financial reports
```

### Should See

```text
Sales revenue
Payment methods
Cash submissions
Deposits
Customer invoices
Supplier invoices
Expenses
Profit/loss
Aging reports
```

---

## 6.6 Procurement Officer

### Main Responsibilities

```text
Monitor fuel demand
Create purchase orders
Manage suppliers
Track delivery status
Plan replenishment
```

### Should See

```text
Stock levels
Forecasts
Supplier records
Purchase orders
Delivery schedules
Reorder recommendations
```

---

## 6.7 Auditor

### Main Responsibilities

```text
Review system integrity
Investigate suspicious actions
Export audit reports
Trace record history
```

### Should See

```text
Audit logs
Edit history
Approval history
Deleted/voided records
Sensitive actions
Risk events
Compliance reports
```

### Should Usually Not Do

```text
Change operational records
Approve shifts
Edit sales
Edit stock
```

---

## 6.8 Owner / CEO / Executive

### Main Responsibilities

```text
Monitor the whole business
Review performance
Identify losses
Track profitability
Make strategic decisions
```

### Should See

```text
Executive command center
Network sales
Stock position
Profit/loss
Station ranking
Loss alerts
Forecasts
Credit exposure
Management summaries
Deterministic insights
```

---

## 6.9 System Administrator

### Main Responsibilities

```text
Configure company structure
Manage users
Manage roles
Set permissions
Configure integrations
Manage security settings
```

### Should See

```text
Users
Roles
Permissions
Stations
System settings
Integrations
API keys
Audit settings
```

---

# 7. Product Editions

FuelGrid OS should be one platform with different operating modes.

---

## 7.1 FuelGrid Station OS

For single stations.

Includes:

```text
Station dashboard
Tank management
Pump management
Shift management
Sales tracking
Delivery receiving
Cash reconciliation
Expenses
Daily close
Reports
Basic alerts
```

---

## 7.2 FuelGrid Chain OS

For multi-station operators.

Includes Station OS plus:

```text
Multi-station dashboard
Regional management
Central pricing
Central procurement
Station comparison
Consolidated reports
Approval workflows
Network alerts
Branch-level finance
```

---

## 7.3 FuelGrid Enterprise OS

For large operators.

Includes Chain OS plus:

```text
Advanced analytics
Automation Engine
Hardware integrations
ERP integrations
Custom workflows
Advanced audit
API/webhooks
Compliance reporting
Forecasting engine
Enterprise security
```

---

## 7.4 FuelGrid Fleet OS

For fleet-heavy businesses.

Includes:

```text
Vehicle fueling
Driver authorization
Fuel cards
QR/RFID access
Odometer capture
Fuel consumption reports
Route/project allocation
Fleet abuse detection
```

---

## 7.5 FuelGrid Depot OS

For depots and bulk storage businesses.

Includes:

```text
Bulk tank management
Truck loading
Truck receiving
Dispatch notes
Depot stock ledger
Loading reconciliation
Depot delivery reports
Bulk inventory controls
```

---

# 8. The 12 Operating Layers of FuelGrid OS

FuelGrid OS should be organized into 12 major layers.

---

# Layer 1: Identity & Access OS

This layer controls who can access what.

## Features

```text
User accounts
Roles
Permissions
Teams
Branches
Regions
Multi-company support
Two-factor authentication
Session management
Device management
Password policies
Permission templates
Approval authority levels
```

## Important Capabilities

The system should support fine-grained permissions such as:

```text
Can view station dashboard
Can open shift
Can close shift
Can approve shift
Can edit meter reading
Can approve stock adjustment
Can change fuel price
Can view profit margin
Can manage credit customers
Can export reports
Can manage integrations
Can view audit logs
Can create users
Can override credit limit
Can delete records
Can lock reporting period
```

---

# Layer 2: Company, Region & Station OS

This layer models the business structure.

## Features

```text
Company setup
Multiple companies
Regions/zones
Stations/branches
Station groups
Station profile
Station operating hours
Station contacts
Station licenses
Station status
Station hierarchy
Station performance dashboard
```

## Hierarchy Model

```text
Tenant
→ Company
→ Region
→ Station
→ Tank
→ Pump
→ Nozzle
→ Shift
→ Transaction
```

This hierarchy allows executives to zoom from a business-wide view down to a single transaction.

---

# Layer 3: Product, Tank & Pump OS

This layer models the physical fuel infrastructure.

## Product Management

Products may include:

```text
PMS / Petrol
AGO / Diesel
Kerosene
LPG
Lubricants
AdBlue
Shop items, optional
```

For each product:

```text
Product name
Product code
Category
Unit of measure
Default price
Tax configuration
Density/temperature settings
Loss tolerance
Active/inactive status
```

## Tank Management

Each tank should have:

```text
Tank name
Station
Product
Capacity
Safe minimum level
Safe maximum level
Dead stock level
Current volume
Opening stock
Closing stock
Dip chart
Sensor mapping
Calibration information
Water level tracking
Temperature tracking
Status
```

## Pump Management

Each pump should have:

```text
Pump number
Station
Status
Manufacturer/model
Serial number
Assigned tank
Nozzles
Meter readings
Maintenance status
Last calibration date
```

## Nozzle Management

Each nozzle should have:

```text
Nozzle number
Pump
Product
Tank source
Opening meter reading
Closing meter reading
Liters dispensed
Price per liter
Sales value
Assigned attendant
```

---

# Layer 4: Shift & Station Operations OS

This layer controls daily activity.

## Shift Features

```text
Shift templates
Shift opening
Shift closing
Attendant assignments
Pump assignments
Opening meter readings
Closing meter readings
Opening tank readings
Closing tank readings
Cash submission
Supervisor approval
Shift notes
Shift incidents
Shift exceptions
```

## Daily Operations Workflow

```text
Open Day
→ Open Shift
→ Assign Attendants
→ Record Opening Meter Readings
→ Record Opening Tank Dips
→ Sell Fuel
→ Receive Deliveries
→ Record Expenses
→ Close Shift
→ Submit Cash
→ Supervisor Approval
→ Close Day
→ Reconcile Stock
→ Reconcile Cash
→ Generate Daily Report
→ Lock Day
```

## Shift Close Output

At shift close, the system should calculate:

```text
Total liters sold
Expected cash
Cash submitted
Mobile money total
Card payment total
Credit sales total
Expenses during shift
Shortage/excess
Pump variances
Approval status
```

---

# Layer 5: Fuel Inventory & Stock Ledger OS

This is one of the most important layers.

It controls the physical stock ledger.

## Stock Movement Types

```text
Opening stock
Delivery received
Pump sale
Manual adjustment
Transfer in
Transfer out
Tank correction
Evaporation/loss
Calibration correction
Stock write-off
Closing stock
```

## Stock Formula

```text
Expected Closing Stock =
Opening Stock + Deliveries + Transfers In - Sales - Transfers Out - Adjustments
```

Then:

```text
Variance =
Actual Closing Stock - Expected Closing Stock
```

## Inventory Features

```text
Real-time stock ledger
Tank stock
Product stock
Station stock
Company-wide stock
Stock movement history
Delivery stock posting
Sales stock depletion
Manual adjustments
Adjustment approvals
Tolerance rules
Stock reconciliation
Variance analysis
Runout prediction
Reorder recommendations
```

## Important Principle

The stock ledger should be immutable.

Instead of silently editing old stock records, the system should create correction entries.

---

# Layer 6: Delivery, Procurement & Supplier OS

This layer controls fuel purchasing and receiving.

## Supplier Management

```text
Supplier profiles
Depot locations
Supplier contacts
Supply contracts
Price agreements
Payment terms
Supplier performance
Supplier invoice history
```

## Purchase Order Features

```text
Purchase order creation
Product selection
Quantity requested
Supplier assignment
Station/depot destination
Expected delivery date
Approval workflow
Purchase status
Cost tracking
```

## Delivery Features

```text
Delivery note number
Supplier
Truck number
Driver
Product
Loaded quantity
Delivered quantity
Receiving station
Receiving tank
Before-delivery dip
After-delivery dip
Temperature-adjusted quantity
Document attachments
Delivery variance
Approval status
Supplier invoice link
```

## Delivery Workflow

```text
Create Purchase Order
→ Approve Purchase Order
→ Confirm Loading
→ Dispatch Truck
→ Arrive at Station
→ Record Before-Dip
→ Discharge Fuel
→ Record After-Dip
→ Upload Delivery Note
→ Calculate Received Quantity
→ Compare Loaded vs Received
→ Approve or Flag Delivery
→ Update Stock
→ Create Supplier Payable
```

---

# Layer 7: Sales, Payments & Revenue OS

This layer manages all money earned from fuel and related sales.

## Sale Types

```text
Pump sale
Cash sale
Mobile money sale
Card sale
Credit sale
Voucher sale
Fleet account sale
Internal company vehicle fueling
Lubricant sale
Shop sale
Bulk sale
```

## Payment Types

```text
Cash
Mobile money
Card
Bank transfer
Credit account
Voucher
Fuel card
QR authorization
RFID authorization
Mixed payment
```

## Sales Features

```text
Pump sales calculation
Manual sale entry
POS integration
Receipt generation
Invoice generation
Daily sales summary
Sales by product
Sales by station
Sales by attendant
Sales by payment method
Sales by customer
Sales by shift
Void/correction workflow
```

## Revenue Reconciliation

The system should compare:

```text
Expected revenue
Submitted cash
Mobile money settlement
Card settlement
Credit sales
Bank deposits
Expenses deducted
Shortage/excess
```

---

# Layer 8: Finance & Accounting Control OS

This layer turns operations into financial control.

## Finance Features

```text
Cash reconciliation
Bank deposit tracking
Mobile money settlement tracking
Card settlement tracking
Credit customer balances
Customer invoices
Supplier bills
Expenses
Petty cash
Staff shortages
Revenue reports
Cost of goods sold
Gross margin
Net margin
Profit/loss by station
Profit/loss by product
Profit/loss by region
Tax reports
Accounting exports
```

## Financial Objects

```text
Cash submission
Bank deposit
Customer invoice
Supplier bill
Expense
Payment receipt
Credit note
Debit note
Journal export
Settlement batch
```

## Daily Finance Close

```text
Total sales
Cash sales
Mobile money sales
Card sales
Credit sales
Expenses
Expected deposit
Actual deposit
Shortage/excess
Approved adjustments
Final daily revenue
```

---

# Layer 9: Customer Credit & Fleet Fuel OS

This layer manages business customers and fleet fueling.

## Customer Management

```text
Customer profiles
Customer contacts
Credit limits
Payment terms
Customer stations
Approved products
Approved vehicles
Approved drivers
Customer balances
Invoices
Statements
Payments
Aging reports
Account suspension
```

## Fleet Features

```text
Vehicle profiles
Driver profiles
Fuel limits
Daily/monthly limits
Odometer capture
Fuel authorization
QR code fueling
RFID card fueling
Fuel card management
Consumption analysis
Abnormal fuel usage alerts
Department/project allocation
```

## Credit Fueling Workflow

```text
Identify Customer
→ Verify Account Status
→ Verify Credit Limit
→ Verify Vehicle
→ Verify Driver
→ Capture Odometer
→ Authorize Product
→ Dispense Fuel
→ Record Transaction
→ Update Customer Balance
→ Add to Invoice
→ Monitor Aging
```

---

# Layer 10: Risk, Fraud & Intelligence OS

This is the premium brain of FuelGrid OS.

## Risk Areas

```text
Fuel loss
Cash shortage
Delivery shortage
Pump manipulation
Tank leakage
Suspicious edits
Backdated entries
Unusual credit sales
Customer limit abuse
Attendant shortage patterns
Repeated voids
Unauthorized price changes
Stock adjustments
```

## Risk Engine Features

```text
Anomaly detection
Rule-based alerts
Risk scoring
Variance classification
Suspicious activity detection
Staff behavior analytics
Station risk ranking
Delivery discrepancy detection
Customer risk scoring
Automated recommendations
Investigation workflows
```

## Example Risk Alert

```text
Critical Alert

Station: Mikocheni
Product: PMS
Issue: Abnormal negative variance
Estimated loss: 1,240 L
Estimated value: TZS 3,968,000

Pattern:
Losses occurred during 5 of the last 7 evening shifts.
Pump 03 appears in 68% of related variance events.
The same attendant was assigned during 4 incidents.

Recommended action:
1. Recalibrate Pump 03.
2. Audit evening shift cash submissions.
3. Confirm tank dip readings.
4. Require supervisor approval for future evening closes.
```

---

# Layer 11: Automation Engine & Executive Intelligence

The Automation Engine — a deterministic Rules & Insights Engine — should be a native layer, not a side feature. It answers questions by running reporting rules and forecast formulas over system data, never by guessing.

## Automation Engine Capabilities

Users should be able to run insight queries such as:

```text
Why did PMS losses increase this week?
Which stations need fuel tomorrow?
Which attendants had shortages this month?
Which customers are overdue?
Show stations with declining sales.
Generate a monthly performance report.
Explain yesterday’s tank variance.
Which station is most profitable?
Which delivery had the biggest discrepancy?
What should I investigate today?
```

## Rules & Insights Engine Should Access

```text
Sales data
Stock data
Tank data
Pump data
Shift data
Delivery data
Finance data
Customer data
Fleet data
Audit logs
Alerts
Reports
Forecasts
```

## Rules & Insights Engine Should Produce

```text
Plain-language explanations
Operational summaries
Executive summaries
Investigation recommendations
Report drafts
Forecast explanations
Risk summaries
Station comparisons
```

## Rules & Insights Engine Must Be Evidence-Based

The Rules & Insights Engine must never bypass permissions and must be evidence-based: every deterministic insight should be traceable to system data.

It should say things like:

```text
This conclusion is based on:
- Tank reconciliation for May 24–26
- Pump 03 meter readings
- Evening shift cash submissions
- Delivery records for PMS
```

---

# Layer 12: Integrations, Hardware & API OS

This layer connects FuelGrid OS to the physical and financial world.

## Hardware Integrations

```text
Pump controllers
Automatic tank gauges
Tank sensors
POS systems
Receipt printers
QR scanners
RFID readers
Fuel card terminals
Weighbridge systems
GPS/fleet devices
```

## Financial Integrations

```text
Mobile money providers
Banks
Card processors
Accounting systems
ERP systems
Tax/fiscal systems
Payment gateways
```

## Communication Integrations

```text
SMS
Email
WhatsApp
Push notifications
Telegram, optional
```

## Developer Platform

```text
REST API
gRPC API
Webhooks
API keys
OAuth applications
Integration logs
Webhook retry system
Rate limits
Sandbox environment
```

## Integration Design Pattern

Use adapters.

```text
Integration Core
→ Pump Adapter
→ Tank Gauge Adapter
→ Mobile Money Adapter
→ Accounting Adapter
→ Bank Adapter
→ POS Adapter
```

This keeps the OS flexible.

---

# 9. Full Module List

FuelGrid OS should eventually include these modules:

```text
01. Authentication & Identity
02. User Management
03. Role & Permission Management
04. Company Management
05. Region/Zonal Management
06. Station Management
07. Product Management
08. Tank Management
09. Pump Management
10. Nozzle Management
11. Shift Management
12. Attendant Management
13. Supervisor Approval
14. Meter Reading Management
15. Tank Dip Management
16. Sensor Reading Management
17. Delivery Management
18. Supplier Management
19. Purchase Order Management
20. Procurement Planning
21. Stock Movement Ledger
22. Stock Reconciliation
23. Variance Analysis
24. Fuel Loss Detection
25. Sales Management
26. Payment Management
27. Cash Reconciliation
28. Mobile Money Reconciliation
29. Card Settlement Reconciliation
30. Credit Customer Management
31. Customer Vehicle Management
32. Driver Management
33. Fleet Fuel Management
34. Voucher Management
35. Fuel Card Management
36. Invoice Management
37. Expense Management
38. Bank Deposit Management
39. Supplier Invoice Management
40. Profit/Loss Reporting
41. Tax/Compliance Reporting
42. Audit Trail
43. Alerts & Notifications
44. Rules & Insights Engine
45. Forecasting Engine
46. Rule Engine
47. Risk Scoring Engine
48. Station Ranking
49. Executive Dashboard
50. Report Builder
51. Document Management
52. Maintenance Management
53. Incident Management
54. API Management
55. Webhook Management
56. Hardware Integration Management
57. Offline Sync Engine
58. Mobile Operations App
59. Admin Configuration Center
60. Data Import/Export
```

---

# 10. Core Workflows

## 10.1 Station Daily Close Workflow

```text
Open operating day
→ Open first shift
→ Assign attendants
→ Record opening pump readings
→ Record opening tank readings
→ Operate sales
→ Receive deliveries, if any
→ Record expenses, if any
→ Close shift
→ Submit attendant cash
→ Approve/reject shift
→ Open next shift
→ Repeat
→ Close final shift
→ Record closing tank readings
→ Reconcile stock
→ Reconcile revenue
→ Review alerts
→ Approve daily close
→ Lock day
→ Generate daily report
```

---

## 10.2 Pump Sales Workflow

```text
Assign pump to attendant
→ Capture opening meter
→ Capture closing meter
→ Calculate liters sold
→ Apply product price
→ Calculate expected revenue
→ Split by payment method
→ Compare expected vs submitted
→ Flag shortage/excess
→ Post to stock ledger
→ Post to revenue ledger
```

---

## 10.3 Fuel Delivery Workflow

```text
Create purchase order
→ Approve purchase order
→ Supplier loads fuel
→ Truck dispatched
→ Station receives truck
→ Record before-delivery tank dip
→ Discharge fuel
→ Record after-delivery tank dip
→ Attach documents
→ Compare ordered, loaded, and received quantities
→ Approve or dispute delivery
→ Update stock ledger
→ Create supplier bill
→ Generate delivery variance report
```

---

## 10.4 Stock Reconciliation Workflow

```text
Get opening stock
→ Add deliveries
→ Subtract sales
→ Apply transfers/adjustments
→ Calculate expected closing stock
→ Capture actual closing stock
→ Calculate variance
→ Compare against tolerance
→ Classify variance
→ Create alert if abnormal
→ Require approval if material
→ Lock reconciliation
```

---

## 10.5 Cash Reconciliation Workflow

```text
Calculate expected cash
→ Record cash submitted
→ Record mobile money payments
→ Record card payments
→ Record credit sales
→ Record expenses
→ Calculate shortage/excess
→ Supervisor review
→ Finance approval
→ Bank deposit matching
→ Lock revenue day
```

---

## 10.6 Credit Customer Workflow

```text
Create customer account
→ Set credit limit
→ Add vehicles/drivers
→ Set product restrictions
→ Customer fuels vehicle
→ Verify authorization
→ Record transaction
→ Update balance
→ Generate invoice
→ Receive payment
→ Update aging report
→ Suspend if overdue/over limit
```

---

## 10.7 Investigation Workflow

```text
Alert triggered
→ Assign investigator
→ Review related shifts
→ Review pump readings
→ Review tank readings
→ Review attendants
→ Review delivery records
→ Add investigation notes
→ Attach evidence
→ Mark root cause
→ Apply corrective action
→ Close case
```

---

# 11. Data Model Blueprint

This is a high-level entity model.

## 11.1 Identity & Tenant Entities

```text
tenants
companies
regions
stations
users
roles
permissions
user_roles
role_permissions
user_station_access
sessions
devices
```

---

## 11.2 Fuel Infrastructure Entities

```text
products
tanks
tank_calibration_charts
tank_readings
tank_sensor_readings
pumps
nozzles
pump_meter_readings
pump_calibrations
```

---

## 11.3 Operations Entities

```text
operating_days
shifts
shift_assignments
shift_pumps
shift_cash_submissions
shift_approvals
daily_closes
incidents
maintenance_requests
```

---

## 11.4 Inventory Entities

```text
stock_movements
stock_reconciliations
stock_adjustments
stock_transfers
delivery_receipts
delivery_variances
inventory_snapshots
```

---

## 11.5 Procurement Entities

```text
suppliers
supplier_contracts
purchase_orders
purchase_order_items
deliveries
delivery_documents
supplier_bills
supplier_payments
```

---

## 11.6 Sales & Payments Entities

```text
sales
sale_items
payments
payment_methods
cash_collections
mobile_money_transactions
card_transactions
bank_deposits
settlement_batches
voids
refunds
```

---

## 11.7 Customer & Fleet Entities

```text
customers
customer_accounts
customer_contacts
customer_vehicles
drivers
fuel_authorizations
fuel_cards
fleet_transactions
customer_invoices
customer_invoice_items
customer_payments
customer_statements
```

---

## 11.8 Finance Entities

```text
expenses
expense_categories
petty_cash_accounts
cash_reconciliations
financial_periods
profit_loss_snapshots
tax_reports
journal_exports
```

---

## 11.9 Risk, Alerts & Audit Entities

```text
alerts
alert_rules
risk_events
risk_scores
investigations
investigation_notes
audit_logs
approval_requests
approval_steps
record_locks
```

---

## 11.10 Automation, Reporting & Integration Entities

```text
reports
report_templates
scheduled_reports
insight_queries
insight_snapshots
forecast_snapshots
integrations
integration_events
api_keys
webhooks
webhook_deliveries
sync_events
offline_devices
```

---

# 12. UI/UX Blueprint

FuelGrid OS must look and feel premium.

The design should avoid the usual problems of enterprise software:

```text
Too many tables
Too much clutter
Too many small buttons
Ugly dashboards
Confusing workflows
No clear hierarchy
Poor mobile experience
```

Instead, FuelGrid OS should feel like a **modern command center**.

---

## 12.1 Visual Style

The visual direction should combine:

```text
Apple-level simplicity
Tesla-style dashboard confidence
Stripe-like financial clarity
Linear-style workflow polish
Bloomberg-level operational depth
```

## 12.2 Design Mood

```text
Premium
Calm
Confident
Modern
Operational
Sharp
Trustworthy
Executive-grade
```

## 12.3 Suggested Color System

```text
Primary: Deep Navy
Secondary: Slate / Charcoal
Accent: Electric Blue
Success: Emerald
Warning: Amber
Danger: Red
Info: Cyan
Petrol/PMS: Orange or Red
Diesel: Blue or Green
Kerosene: Purple or Gray
Neutral: Zinc / Slate
```

## 12.4 Layout System

### Main Layout

```text
Left Sidebar
Top Command Bar
Main Workspace
Right Insight Panel
Global Command Palette
Notification Center
```

### Left Sidebar

```text
Command Center
Stations
Tanks
Pumps
Shifts
Sales
Inventory
Deliveries
Customers
Fleet
Finance
Procurement
Reports
Alerts
Automation
Audit
Integrations
Settings
```

### Top Bar

```text
Global search
Date range
Station selector
Region selector
Quick create
Notifications
User profile
```

### Right Insight Panel

Contextual recommendations such as:

```text
Stockout warning
Variance explanation
Pending approval
Deterministic insight
Related records
Suggested action
```

---

# 13. Main Screens

## 13.1 Executive Command Center

For owners and executives.

Should show:

```text
Total revenue
Total liters sold
Gross margin
Net margin
Current stock
Stockout risk
Fuel losses
Cash reconciliation status
Credit exposure
Station ranking
Regional performance
Critical alerts
Forecasts
Rule-based executive summary
```

Example opening message:

```text
Good morning, Japhary.

Your network sold 184,200 liters today across 14 stations.
Revenue is up 8.4% compared to last Wednesday.
Three stations need replenishment within 48 hours.
One station has a critical PMS variance.
Cash reconciliation is 96.8% complete.
```

---

## 13.2 Station Dashboard

For station managers.

Should show:

```text
Today’s sales
Liters sold
Current tank levels
Open shifts
Pending approvals
Expected cash
Submitted cash
Expenses
Deliveries
Variance
Alerts
Daily close status
```

---

## 13.3 Tank View

Should show:

```text
Tank visual level
Capacity
Current volume
Safe minimum
Safe maximum
Ullage
Opening stock
Deliveries
Sales depletion
Expected closing
Actual closing
Variance
Water level
Temperature
Runout prediction
Reading history
```

---

## 13.4 Pump View

Should show:

```text
Pump status
Nozzle status
Assigned product
Assigned tank
Opening meter
Closing meter
Liters sold
Expected revenue
Assigned attendant
Calibration status
Maintenance status
```

---

## 13.5 Shift View

Should show:

```text
Shift timeline
Assigned attendants
Assigned pumps
Opening readings
Closing readings
Expected cash
Cash submitted
Payment breakdown
Shortage/excess
Supervisor approval
Shift notes
Exceptions
```

---

## 13.6 Delivery View

Should show:

```text
Purchase order
Supplier
Truck
Driver
Product
Loaded quantity
Delivered quantity
Receiving tank
Before dip
After dip
Variance
Documents
Approval status
Supplier bill status
```

---

## 13.7 Finance Dashboard

Should show:

```text
Revenue
Cash position
Deposits
Mobile money settlements
Card settlements
Credit sales
Expenses
Supplier bills
Customer balances
Profit/loss
Aging report
Finance exceptions
```

---

## 13.8 Risk Center

Should show:

```text
Critical alerts
Risk score by station
Fuel loss trends
Cash shortage trends
Suspicious edits
Delivery discrepancies
Attendant risk patterns
Customer credit risk
Open investigations
Closed investigations
```

---

## 13.9 Automation Engine

Should have:

```text
Insight query interface
Suggested questions
Data-backed answers
Charts/tables when needed
Source references
Recommended actions
Report generation
Investigation mode
```

---

# 14. Mobile Experience

FuelGrid OS should include a mobile-first field experience.

## Mobile App Users

```text
Attendants
Supervisors
Station managers
Delivery receivers
Auditors
Fleet drivers, optional
```

## Mobile Features

```text
Open shift
View assigned pump
Enter meter readings
Enter tank dips
Submit cash
Capture delivery note photo
Record expenses
Approve shift
Receive alerts
Scan QR/RFID
Authorize fleet fueling
Work offline
Sync later
```

## Mobile Design Principle

Mobile should be extremely simple:

```text
Big buttons
Large numbers
Minimal typing
Clear status
Fast loading
Offline support
Step-by-step workflows
```

---

# 15. Offline-First Blueprint

Offline support is essential.

FuelGrid OS should support offline operations for stations with unstable internet.

## Offline Features

```text
Open shifts
Enter meter readings
Enter tank readings
Record expenses
Receive deliveries
Capture photos
Submit cash
Close shifts
Queue approvals
Sync later
```

## Offline Sync Requirements

```text
Local encrypted storage
Background sync
Conflict detection
Conflict resolution UI
Offline audit trail
Device identity
Sync status indicator
Retry system
Server-side validation
```

## Conflict Example

If two users edit the same reading:

```text
System should preserve both attempts
Show who entered what
Require supervisor resolution
Create audit log
Apply approved value only
```

---

# 16. Reporting Blueprint

FuelGrid OS should include both fixed reports and a custom report builder.

## Daily Reports

```text
Daily sales report
Daily stock report
Daily shift report
Daily cash report
Daily tank variance report
Daily delivery report
Daily expense report
Daily close report
```

## Monthly Reports

```text
Monthly sales report
Monthly station performance
Monthly fuel loss report
Monthly customer credit report
Monthly supplier report
Monthly profit/loss report
Monthly staff performance
Monthly procurement report
```

## Executive Reports

```text
Network performance report
Station ranking report
Regional comparison report
Fuel loss intelligence report
Forecasting report
Profitability report
Credit exposure report
Risk report
```

## Audit Reports

```text
Record edit history
Deleted/voided records
Price changes
Stock adjustments
Approval history
User activity report
Suspicious actions
```

## Export Formats

```text
PDF
Excel
CSV
Email
Dashboard link
Scheduled report
API export
```

---

# 17. Security Blueprint

FuelGrid OS must be built like a serious financial system.

## Security Requirements

```text
Role-based access control
Permission-based actions
Two-factor authentication
Password policies
Device tracking
Session expiration
IP logging
Audit logs
Record locking
Approval workflows
Encrypted data at rest
Encrypted data in transit
Secure API keys
Webhook signing
Backup and restore
Least privilege access
```

## Sensitive Actions Requiring Audit

```text
Price change
Stock adjustment
Meter reading edit
Tank reading edit
Cash submission edit
Shift reopening
Daily close reversal
Credit limit override
Customer suspension removal
Supplier bill edit
User permission change
Integration setting change
Record deletion
```

---

# 18. Technical Architecture

## 18.1 Frontend Stack

Recommended frontend:

```text
Next.js
React
TypeScript
Tailwind CSS
shadcn/ui
TanStack Query
TanStack Table
Zustand
React Hook Form
Zod
Recharts
Framer Motion
PWA support
```

## Why This Stack

```text
Modern UI
Fast development
Excellent dashboards
Strong forms
Good data fetching
Reusable components
PWA/offline support
Beautiful frontend experience
```

---

## 18.2 Backend Stack

Recommended backend:

```text
Go
gRPC
REST gateway
PostgreSQL
Redis
Kafka or NATS
ClickHouse
Object Storage
OpenTelemetry
Docker
Kubernetes-ready architecture
```

## Why Go

```text
High performance
Strong concurrency
Reliable backend services
Excellent for APIs
Good for enterprise systems
Good for microservices
Long-term maintainability
```

---

## 18.3 Database Strategy

```text
PostgreSQL: core transactional data
Redis: cache, sessions, queues, locks
ClickHouse: analytics, events, high-volume reporting
Object Storage: documents, photos, receipts
Event Store: audit/events/immutable business actions
```

---

## 18.4 Service Architecture

FuelGrid OS can be designed as services:

```text
identity-service
tenant-service
station-service
product-service
tank-service
pump-service
shift-service
sales-service
inventory-service
delivery-service
procurement-service
finance-service
customer-service
fleet-service
pricing-service
alert-service
risk-service
forecasting-service
reporting-service
audit-service
notification-service
integration-service
document-service
automation-engine-service
offline-sync-service
```

However, the first implementation can still be a **modular monolith with strict service boundaries**. That gives speed now and scalability later.

---

# 19. Event-Driven Architecture

FuelGrid OS should be event-driven.

Every major action should emit an event.

## Example Events

```text
ShiftOpened
ShiftClosed
MeterReadingRecorded
TankReadingRecorded
DeliveryReceived
StockMovementCreated
StockReconciled
CashSubmitted
CashReconciled
PriceChanged
CreditSaleRecorded
InvoiceGenerated
PaymentReceived
AlertTriggered
ApprovalRequested
ApprovalGranted
ApprovalRejected
RecordEdited
DailyCloseCompleted
```

## Benefits

```text
Auditability
Better reporting
Real-time alerts
Deterministic insight generation
Integration webhooks
Historical replay
Fraud detection
```

---

# 20. Automation Engine Architecture

The Automation Engine — a deterministic Rules & Insights Engine — should be built safely and carefully.

## Automation Engine Components

```text
Insight query interface
Query interpreter
Permission-aware data access
Report generator
Insight generator
Risk explanation engine
Forecast explanation engine
Investigation assistant
```

## Rules & Insights Engine Rules

The Rules & Insights Engine must:

```text
Respect user permissions and never bypass them
Never expose restricted financial data
Cite system records internally
Avoid unsupported claims
Explain uncertainty
Recommend actions, not make unauthorized changes
Log insight queries for audit
```

## Example Insight Query

User asks:

```text
Why did diesel losses increase yesterday?
```

Engine process:

```text
Check user permission
Fetch diesel tank reconciliation
Fetch pump readings
Fetch deliveries
Fetch shift data
Fetch relevant audit logs
Compare historical average
Identify abnormal pattern
Generate plain-language explanation
Recommend next actions
```

---

# 21. Forecasting Engine

FuelGrid OS should include forecasting.

## Forecasting Features

```text
Demand forecasting
Stockout prediction
Reorder recommendation
Delivery scheduling
Sales trend forecasting
Seasonal demand analysis
Station performance forecast
Customer consumption forecast
```

## Forecast Output Example

```text
Diesel at Mbezi Station is projected to reach minimum level in 38 hours.

Recommended order: 22,000 L
Recommended delivery window: Tomorrow before 10:00
Confidence: High
Reason: Current stock, 14-day sales trend, weekend demand pattern
```

---

# 22. Alert & Notification System

## Alert Types

```text
Low stock
Stockout risk
Fuel variance
Cash shortage
Delivery shortage
Suspicious edit
Shift not closed
Daily close overdue
Credit limit exceeded
Customer overdue
Pump offline
Tank sensor warning
Price change approval needed
Supplier delivery delayed
```

## Notification Channels

```text
In-app
Email
SMS
WhatsApp
Push notification
Webhook
```

## Alert Severity

```text
Info
Low
Medium
High
Critical
```

## Alert Lifecycle

```text
Created
Acknowledged
Assigned
Investigating
Resolved
Dismissed
Escalated
```

---

# 23. Audit & Compliance Blueprint

FuelGrid OS should have a serious audit foundation.

## Audit Log Should Capture

```text
Actor
Action
Entity
Previous value
New value
Timestamp
Device
IP address
Location, if available
Reason
Approval reference
```

## Record Locking

After a day is closed:

```text
Records should be locked
Changes require reopening approval
Corrections should create adjustment records
Original records should remain visible
Audit trail must remain permanent
```

## Compliance-Ready Features

```text
Daily close locking
Financial period locking
Exportable audit reports
User activity reports
Approval logs
Tax reports
Document attachments
Data retention settings
```

---

# 24. Integration Blueprint

## Pump Controller Integration

Should support:

```text
Live meter readings
Transaction capture
Pump status
Nozzle status
Price updates
Pump lock/unlock, if supported
Error states
```

## Tank Gauge Integration

Should support:

```text
Real-time volume
Water level
Temperature
Ullage
Leak alerts
Sensor health
Historical readings
```

## Mobile Money Integration

Should support:

```text
Payment confirmation
Transaction reference
Settlement matching
Failed payments
Reversal tracking
Daily settlement reports
```

## Accounting Integration

Should support:

```text
Customer invoices
Supplier bills
Expenses
Payments
Journal exports
Chart of accounts mapping
Tax mapping
```

## API/Webhooks

External systems should be able to receive events such as:

```text
sale.created
delivery.received
stock.low
shift.closed
invoice.generated
payment.received
alert.critical
```

---

# 25. Implementation Roadmap for Full OS

Since this is a full-fledged OS, we should not call these MVP phases. These are **full system construction phases**.

---

## Phase 1: Platform Foundation

Build the base operating layer.

```text
Monorepo setup
Design system
App shell
Authentication
Multi-tenancy
Company setup
Region setup
Station setup
User management
RBAC permissions
Audit foundation
API foundation
Database migrations
Core dashboard shell
```

---

## Phase 2: Fuel Infrastructure Core

Build the physical model.

```text
Product management
Tank management
Pump management
Nozzle management
Tank-to-pump mapping
Calibration data
Station infrastructure dashboard
Basic tank and pump status
```

---

## Phase 3: Station Operations Core

Build daily station workflows.

```text
Operating day
Shift opening
Shift closing
Attendant assignment
Pump assignment
Opening meter readings
Closing meter readings
Opening tank readings
Closing tank readings
Supervisor approvals
Daily close workflow
Station dashboard
```

---

## Phase 4: Inventory & Reconciliation Engine

Build the stock brain.

```text
Stock ledger
Stock movement events
Delivery stock posting
Sales stock depletion
Tank reconciliation
Product reconciliation
Variance calculations
Tolerance rules
Stock adjustment approvals
Stock reports
```

---

## Phase 5: Delivery & Procurement OS

Build supplier and delivery workflows.

```text
Supplier management
Purchase orders
Purchase approvals
Delivery receiving
Truck/driver details
Before/after dip workflow
Delivery documents
Delivery variance
Supplier invoice creation
Procurement dashboard
```

---

## Phase 6: Sales, Payments & Revenue OS

Build revenue control.

```text
Pump sales
Manual sales
Payment methods
Cash sales
Mobile money sales
Card sales
Credit sales
Sales summaries
Payment breakdown
Revenue reports
Void/correction workflow
```

---

## Phase 7: Finance & Accounting Control

Build financial management.

```text
Cash reconciliation
Bank deposits
Expenses
Petty cash
Customer invoices
Supplier bills
Customer payments
Supplier payments
Profit/loss reports
Aging reports
Accounting exports
```

---

## Phase 8: Customer Credit & Fleet Fuel OS

Build credit and fleet capabilities.

```text
Customer profiles
Credit limits
Customer vehicles
Drivers
Fuel authorization
Fuel cards
QR/RFID workflows
Odometer capture
Fleet consumption reports
Customer statements
Credit risk alerts
```

---

## Phase 9: Chain & Enterprise Command

Build multi-station power.

```text
Executive command center
Regional dashboards
Station ranking
Central pricing
Central procurement
Network inventory
Consolidated finance
Multi-station reports
Enterprise approvals
```

---

## Phase 10: Risk, Fraud & Intelligence

Build the risk brain.

```text
Rule engine
Alert rules
Fuel loss detection
Cash shortage detection
Delivery discrepancy detection
Suspicious edit detection
Attendant risk patterns
Station risk scores
Investigation workflows
Recommended actions
```

---

## Phase 11: Forecasting & Automation

Build prediction and automation.

```text
Demand forecasting
Stockout prediction
Reorder recommendations
Automated alerts
Scheduled reports
Procurement planning
Rule-based automation
Escalation workflows
```

---

## Phase 12: Automation Engine

Build the Rules & Insights Engine interface.

```text
Insight query interface
Permission-aware queries
Variance explanation
Executive summaries
Report generation
Investigation assistant
Forecast explanation
Natural language analytics
```

---

## Phase 13: Hardware & External Integrations

Build the integration platform.

```text
Pump controller adapters
Tank gauge adapters
POS integrations
Mobile money integrations
Bank integrations
Accounting integrations
ERP exports
API keys
Webhooks
Integration logs
```

---

## Phase 14: Mobile & Offline OS

Build field operations.

```text
Mobile attendant app
Mobile supervisor app
Offline storage
Offline shift workflows
Offline readings
Offline delivery capture
Sync engine
Conflict resolution
Device audit logs
```

---

## Phase 15: Enterprise Hardening

Make it production-grade.

```text
Security hardening
Observability
OpenTelemetry
Backups
Disaster recovery
Performance optimization
Load testing
Compliance reports
Admin tools
Data retention
Record locking
Advanced audit
```

---

# 26. Success Criteria

FuelGrid OS is successful when it can do the following:

## Operational Success

```text
A station can run its full day inside the system.
A manager can close the day with confidence.
A chain owner can see all stations in one command center.
A finance officer can reconcile money without paper chaos.
```

## Inventory Success

```text
Every liter is traceable.
Every delivery is reconciled.
Every stock variance is explained.
Every adjustment is approved and audited.
```

## Financial Success

```text
Every sale connects to a payment.
Every cash submission is checked.
Every credit customer has a balance.
Every station has profit/loss visibility.
```

## Intelligence Success

```text
The system detects abnormal losses.
The system flags suspicious activity.
The system recommends actions.
The system forecasts stock needs.
The Automation Engine explains what is happening.
```

## Design Success

```text
Attendants find it simple.
Managers find it useful.
Executives find it beautiful.
Auditors find it trustworthy.
Enterprises find it scalable.
```

---

# 27. Final Product Definition

## Long Version

**FuelGrid OS is a full-fledged fuel operations operating system designed to manage the complete lifecycle of fuel businesses. It connects stations, tanks, pumps, shifts, deliveries, suppliers, customers, fleets, sales, payments, finance, inventory, risk, reporting, rule-based intelligence, and hardware integrations into one unified platform. It gives owners, managers, attendants, finance teams, auditors, and executives complete visibility and control over every liter of fuel and every unit of money.**

## Short Version

**FuelGrid OS is the intelligent command center for fuel businesses — controlling every liter, every transaction, every station, and every decision.**

## One-Line Version

**The modern operating system for fuel stations, chains, depots, fleets, and enterprise fuel operations.**
