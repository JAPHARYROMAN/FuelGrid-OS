# FuelGrid OS — Full Technical Architecture Document

## Version

**Document:** Full Technical Architecture Document
**Product:** FuelGrid OS
**Category:** Fuel Operations Operating System
**Status:** Master Architecture Draft

---

# 1. Architecture Executive Summary

FuelGrid OS is a full-fledged fuel operations operating system designed to support single fuel stations, multi-station chains, depots, fleet operators, distributors, and enterprise fuel organizations.

The architecture must support operational workflows, financial-grade reconciliation, immutable audit trails, offline-capable station operations (planned — see §16; the web app today ships as an installable PWA, with the service worker and offline sync engine scheduled for Phase 14), high-volume analytics, hardware integrations, AI intelligence, multi-tenant security, real-time alerts, mobile workflows, and enterprise extensibility.

FuelGrid OS should be designed as a modular, event-driven platform. The first implementation can be delivered as a modular monolith with strict domain boundaries, but the architecture must be clean enough to evolve into distributed services as usage grows.

The platform should be built around one guiding principle:

**Every liter, every transaction, every user action, and every approval must be traceable.**

---

# 2. Architecture Goals

## 2.1 Primary Goals

The architecture must provide:

* Multi-tenant isolation
* Station, region, and company hierarchy
* Role-based and permission-based access control
* Traceable fuel inventory ledger
* Financial-grade revenue reconciliation
* Shift and daily close workflows
* Immutable audit logs
* Offline-capable station and mobile operations
* Event-driven alerts and integrations
* High-volume analytics and reporting
* Hardware integration layer
* AI assistant with permission-aware data access
* Enterprise security and observability

## 2.2 Non-Goals

The system should not be built as:

* A simple CRUD admin dashboard
* A single-station-only app
* A spreadsheet replacement
* A POS-only system
* A finance-only system
* A hardware-dependent platform
* A tightly coupled monolith with no domain boundaries

FuelGrid OS must be an extensible business operating system.

---

# 3. Recommended Technology Stack

## 3.1 Frontend Stack

Recommended stack:

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
* Service Workers
* IndexedDB for offline storage

## 3.2 Backend Stack

Recommended stack:

* Go
* gRPC for internal service APIs
* REST gateway for web/mobile clients
* PostgreSQL for transactional data
* Redis for cache, sessions, locks, and queues
* Kafka or NATS for event streaming
* ClickHouse for analytics and high-volume reporting
* Object Storage for documents, receipts, images, and attachments
* OpenTelemetry for traces, metrics, and logs
* Docker for packaging
* Kubernetes-ready deployment design

## 3.3 AI and Intelligence Stack

Recommended components:

* AI Gateway service
* Permission-aware query planner
* Data retrieval layer
* Report generation engine
* Risk explanation engine
* Forecast explanation engine
* Vector search, optional for documents and knowledge base
* Audit log for AI queries

## 3.4 Mobile Stack Options

Recommended options:

* React Native with TypeScript
* Expo for accelerated development, if suitable
* SQLite or MMKV/secure storage for offline data
* Background sync queue
* Push notifications
* QR/RFID integration support where device capabilities allow

Alternative:

* PWA-first mobile operations app using Next.js and browser storage

---

# 4. High-Level System Architecture

FuelGrid OS should be organized into the following major layers:

1. Client Layer
2. API Gateway Layer
3. Domain Application Layer
4. Event and Messaging Layer
5. Data Persistence Layer
6. Analytics Layer
7. Integration Layer
8. AI Intelligence Layer
9. Observability and Security Layer

## 4.1 Architecture Diagram

```text
Web App / Mobile App / Admin Console / External Clients
                ↓
API Gateway / REST Gateway / BFF Layer
                ↓
Domain Application Layer
                ↓
Transactional Database / Event Bus / Cache / Object Storage
                ↓
Analytics Warehouse / Reporting Engine / AI Intelligence Layer
                ↓
External Integrations / Hardware Adapters / Webhooks
```

## 4.2 Client Applications

FuelGrid OS should support multiple client experiences:

* Executive web app
* Station manager web app
* Finance web app
* Auditor web app
* Admin console
* Mobile attendant app
* Mobile supervisor app
* Delivery receiving app
* API clients
* Hardware integration clients

## 4.3 API Gateway

The API gateway should provide:

* Authentication enforcement
* Tenant resolution
* Rate limiting
* Request validation
* API versioning
* REST endpoints for frontend/mobile apps
* gRPC translation where needed
* Request tracing
* Permission context injection

## 4.4 Domain Layer

The domain layer contains business logic for:

* Identity
* Tenant and company management
* Station operations
* Tanks and pumps
* Inventory ledger
* Sales
* Payments
* Deliveries
* Procurement
* Finance
* Customer credit
* Fleet fueling
* Risk
* Alerts
* Reporting
* AI
* Integrations
* Audit

## 4.5 Data Layer

The data layer includes:

* PostgreSQL for source-of-truth transactional data
* Redis for cache, locks, and temporary state
* Event bus for domain events
* ClickHouse for analytics
* Object storage for files and documents
* Local device storage for offline workflows

---

# 5. Architectural Style

## 5.1 Modular Monolith First, Service-Ready Design

FuelGrid OS should start as a modular monolith or tightly governed modular platform, not as premature microservices.

The codebase should be organized by domain modules with strict boundaries:

```text
/apps
  /web
  /mobile
  /admin
/services
  /api
/packages
  /ui
  /config
  /types
  /sdk
/internal
  /identity
  /tenant
  /station
  /tank
  /pump
  /shift
  /inventory
  /delivery
  /sales
  /finance
  /customer
  /fleet
  /risk
  /reporting
  /audit
  /integration
  /ai
```

Each domain should own:

* Models
* Commands
* Queries
* Policies
* Validators
* Events
* Repository interfaces
* Application services

## 5.2 Future Service Extraction

Domains should be designed so they can later become independent services:

* identity-service
* tenant-service
* station-service
* inventory-service
* sales-service
* finance-service
* risk-service
* reporting-service
* integration-service
* ai-assistant-service

Service extraction should happen only when required by scale, team structure, or deployment independence.

---

# 6. Core Domain Boundaries

## 6.1 Identity Domain

Owns:

* Users
* Roles
* Permissions
* Authentication
* Sessions
* Devices
* MFA
* Access policies

Does not own:

* Station operations
* Fuel inventory
* Financial data

## 6.2 Tenant Domain

Owns:

* Tenants
* Companies
* Regions
* Station hierarchy
* Tenant configuration
* Business settings

## 6.3 Station Domain

Owns:

* Stations
* Operating days
* Station status
* Staff station access
* Station-level configuration

## 6.4 Infrastructure Domain

Owns:

* Products
* Tanks
* Pumps
* Nozzles
* Calibration data
* Hardware mappings

## 6.5 Shift Domain

Owns:

* Shifts
* Shift assignments
* Opening readings
* Closing readings
* Shift close
* Shift approvals
* Shift exceptions

## 6.6 Inventory Domain

Owns:

* Stock ledger
* Stock movements
* Stock reconciliation
* Variance calculation
* Adjustments
* Transfers
* Inventory snapshots

## 6.7 Delivery and Procurement Domain

Owns:

* Suppliers
* Purchase orders
* Deliveries
* Delivery documents
* Supplier invoices linkage
* Delivery variance

## 6.8 Sales and Payments Domain

Owns:

* Sales
* Sale items
* Payment methods
* Pump sale calculations
* Credit sales
* Cash/card/mobile money splits
* Voids and corrections

## 6.9 Finance Domain

Owns:

* Cash reconciliation
* Bank deposits
* Expenses
* Petty cash
* Customer invoices
* Supplier bills
* Profit/loss snapshots
* Financial periods
* Accounting exports

## 6.10 Customer and Fleet Domain

Owns:

* Customers
* Customer accounts
* Credit limits
* Vehicles
* Drivers
* Fuel authorizations
* Fleet transactions
* Fuel cards

## 6.11 Risk Domain

Owns:

* Risk rules
* Risk scores
* Fraud detection
* Anomaly detection
* Investigation workflows
* Risk events

## 6.12 Alert and Notification Domain

Owns:

* Alert rules
* Alert lifecycle
* Notification routing
* Escalations
* Delivery channels

## 6.13 Reporting Domain

Owns:

* Report definitions
* Report templates
* Scheduled reports
* Export jobs
* Report permissions

## 6.14 Audit Domain

Owns:

* Audit logs
* Record history
* Approval history
* Record locking
* Sensitive action logging

## 6.15 Integration Domain

Owns:

* External connections
* API keys
* Webhooks
* Hardware adapters
* Integration events
* Retry handling

## 6.16 AI Domain

Owns:

* AI query handling
* Permission-aware data access
* AI answer generation
* AI query audit logs
* AI reports and summaries

---

# 7. Multi-Tenancy Architecture

## 7.1 Tenant Model

FuelGrid OS must support multiple tenants. A tenant may represent:

* A single station business
* A fuel chain
* A depot operator
* A fleet operator
* A holding company with multiple fuel companies

## 7.2 Tenant Isolation

Every tenant-owned table must include:

* tenant_id
* company_id where applicable
* station_id where applicable
* created_at
* updated_at
* created_by
* updated_by

## 7.3 Isolation Strategy

Recommended first strategy:

* Shared database
* Shared schema
* Strong tenant_id enforcement
* Row-level security where appropriate
* Application-level authorization policies

Enterprise future options:

* Dedicated database per large tenant
* Dedicated schema per enterprise tenant
* Hybrid deployment model

## 7.4 Tenant Resolution

Tenant can be resolved from:

* Auth token claims
* Subdomain
* Organization switcher
* API key scope
* Integration configuration

## 7.5 Tenant Safety Rules

The system must prevent:

* Cross-tenant data leakage
* Cross-tenant reporting
* Cross-tenant API access
* Cross-tenant AI retrieval
* Cross-tenant webhook delivery

---

# 8. Authentication and Authorization Architecture

## 8.1 Authentication

Supported authentication methods:

* Email/password
* MFA
* Password reset
* Enterprise SSO in future
* API key authentication for integrations
* OAuth for enterprise external apps

## 8.2 Session Model

Sessions should include:

* session_id
* user_id
* tenant_id
* device_id
* issued_at
* expires_at
* last_seen_at
* ip_address
* user_agent
* revoked_at

## 8.3 Authorization Model

FuelGrid OS should use layered authorization:

1. Tenant access
2. Company access
3. Region access
4. Station access
5. Role permissions
6. Object-level permissions
7. Workflow-state permissions

Example:

A station manager may have permission to approve shifts, but only for stations assigned to them, and only when the shift is in submitted status.

## 8.4 Permission Evaluation

Permission checks should include:

* actor
* action
* resource type
* resource id
* tenant id
* station id
* workflow state
* ownership/assignment context

## 8.5 Sensitive Action Protection

Sensitive actions must require:

* Explicit permission
* Reason capture
* Audit log
* Optional approval workflow
* Optional MFA challenge

Sensitive actions include:

* Price changes
* Stock adjustments
* Closed period changes
* Credit limit overrides
* User permission changes
* Integration credential changes
* Record deletion

---

# 9. Data Architecture

## 9.1 PostgreSQL as System of Record

PostgreSQL should store:

* Tenants
* Companies
* Regions
* Stations
* Users
* Roles
* Products
* Tanks
* Pumps
* Shifts
* Deliveries
* Sales
* Payments
* Inventory ledger
* Finance records
* Customers
* Fleet records
* Alerts
* Audit logs
* Integration configs

## 9.2 ClickHouse for Analytics

ClickHouse should store high-volume analytical data:

* Sales events
* Stock movement events
* Pump readings
* Tank sensor readings
* Risk events
* Alert events
* Audit event projections
* Reporting aggregates

## 9.3 Redis Usage

Redis should be used for:

* Session cache
* Permission cache
* Short-lived locks
* Rate limiting
* Background job queues, if not handled by another queue
* Idempotency keys
* Sync state markers
* Dashboard cache

## 9.4 Object Storage

Object storage should store:

* Delivery notes
* Receipts
* Expense photos
* Calibration documents
* Station licenses
* Exported reports
* Uploaded documents
* Investigation evidence

Each stored object should include metadata:

* tenant_id
* owner_entity_type
* owner_entity_id
* uploaded_by
* content_type
* checksum
* created_at

## 9.5 Event Store

FuelGrid OS should keep durable domain events.

Events should include:

* event_id
* tenant_id
* event_type
* aggregate_type
* aggregate_id
* actor_id
* payload
* metadata
* occurred_at
* correlation_id
* causation_id

---

# 10. Core Database Entity Groups

## 10.1 Identity Entities

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

## 10.2 Fuel Infrastructure Entities

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

## 10.3 Operations Entities

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

## 10.4 Inventory Entities

```text
stock_movements
stock_reconciliations
stock_adjustments
stock_transfers
delivery_receipts
delivery_variances
inventory_snapshots
```

## 10.5 Procurement Entities

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

## 10.6 Sales and Payments Entities

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

## 10.7 Customer and Fleet Entities

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

## 10.8 Finance Entities

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

## 10.9 Risk, Alert, and Audit Entities

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

## 10.10 AI, Reporting, and Integration Entities

```text
reports
report_templates
scheduled_reports
ai_queries
ai_insight_snapshots
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

# 11. Inventory Ledger Architecture

## 11.1 Ledger Principle

The inventory ledger must be append-only.

Existing stock movement records should not be silently edited. Corrections should create new adjustment records.

## 11.2 Stock Movement Record

A stock movement should include:

* movement_id
* tenant_id
* station_id
* tank_id
* product_id
* movement_type
* quantity
* unit
* source_type
* source_id
* reference_number
* occurred_at
* created_by
* approved_by, optional
* status
* metadata

## 11.3 Movement Types

* opening_stock
* delivery_received
* pump_sale
* transfer_in
* transfer_out
* manual_adjustment
* tank_correction
* evaporation_loss
* calibration_correction
* stock_writeoff
* closing_stock

## 11.4 Reconciliation Calculation

Expected closing stock:

```text
opening_stock + deliveries + transfers_in - sales - transfers_out - adjustments
```

Variance:

```text
actual_closing_stock - expected_closing_stock
```

## 11.5 Reconciliation Output

A reconciliation result should include:

* expected_closing_stock
* actual_closing_stock
* variance_quantity
* variance_percentage
* tolerance_status
* classification
* recommended_action
* approval_status
* locked_at

## 11.6 Inventory Integrity Rules

The system must prevent:

* Negative tank stock unless explicitly allowed by policy
* Delivery posting without receiving tank
* Pump sale posting without mapped tank
* Closing stock without required readings
* Stock adjustment without permission
* Editing locked reconciliation without approval

---

# 12. Financial Ledger and Reconciliation Architecture

## 12.1 Revenue Traceability

Every sale must connect to:

* Station
* Product
* Shift
* Pump/nozzle, where applicable
* Attendant, where applicable
* Payment method
* Revenue amount
* Stock movement

## 12.2 Cash Reconciliation

Cash reconciliation should compare:

* Expected cash
* Cash submitted
* Cash expenses
* Cash deposit
* Shortage/excess

## 12.3 Payment Settlement

The system should support settlement matching for:

* Mobile money
* Card transactions
* Bank deposits
* Credit customer payments

## 12.4 Financial Periods

Finance should be organized into periods:

* Daily close
* Monthly close
* Financial period close

Closed periods should be locked.

## 12.5 Corrections

Corrections after close should create:

* Adjustment record
* Approval request
* Audit log
* Optional journal export update

---

# 13. Event-Driven Architecture

## 13.1 Domain Events

The system should emit domain events whenever important business actions occur.

Required events include:

* UserCreated
* StationCreated
* TankCreated
* PumpCreated
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

## 13.2 Event Envelope

Each event should include:

* event_id
* event_type
* event_version
* tenant_id
* aggregate_type
* aggregate_id
* actor_id
* payload
* metadata
* occurred_at
* correlation_id
* causation_id

## 13.3 Event Consumers

Events should feed:

* Audit logs
* Alerts
* Risk engine
* Reporting projections
* Webhooks
* Notifications
* AI insight snapshots
* Analytics warehouse

## 13.4 Idempotency

Event consumers must be idempotent.

Each consumer should track processed event IDs to avoid duplicate processing.

## 13.5 Outbox Pattern

The system should use the outbox pattern to ensure database writes and event publication remain consistent.

Workflow:

1. Business transaction writes domain data.
2. Same transaction writes outbox event.
3. Background publisher reads outbox.
4. Publisher sends event to event bus.
5. Event is marked as published.

---

# 14. API Architecture

## 14.1 API Types

FuelGrid OS should expose:

* REST API for web/mobile clients
* gRPC APIs for internal services
* Webhooks for external systems
* Optional GraphQL/BFF layer for dashboard-heavy pages

## 14.2 API Versioning

APIs should be versioned:

```text
/api/v1/...
/api/v2/...
```

Breaking changes require new versions.

## 14.3 API Resource Groups

Main API groups:

```text
/auth
/users
/roles
/permissions
/tenants
/companies
/regions
/stations
/products
/tanks
/pumps
/nozzles
/shifts
/readings
/inventory
/deliveries
/purchase-orders
/sales
/payments
/finance
/customers
/fleet
/alerts
/risk
/reports
/audit
/integrations
/ai
```

## 14.4 API Standards

All APIs should support:

* Structured validation errors
* Pagination
* Filtering
* Sorting
* Idempotency keys for write operations
* Request IDs
* Tenant scoping
* Permission checks
* Audit metadata

## 14.5 Example API Patterns

### Create Shift

```text
POST /api/v1/shifts
```

### Close Shift

```text
POST /api/v1/shifts/{shift_id}/close
```

### Reconcile Stock

```text
POST /api/v1/inventory/reconciliations
```

### Submit Cash

```text
POST /api/v1/shifts/{shift_id}/cash-submissions
```

### Ask AI Assistant

```text
POST /api/v1/ai/query
```

---

# 15. Frontend Architecture

## 15.1 Application Structure

Recommended frontend structure:

```text
/apps/web
  /app
    /(auth)
    /(dashboard)
    /command-center
    /stations
    /tanks
    /pumps
    /shifts
    /inventory
    /deliveries
    /sales
    /finance
    /customers
    /fleet
    /risk
    /reports
    /audit
    /integrations
    /settings
  /components
  /features
  /lib
  /hooks
  /stores
  /schemas
  /styles
```

## 15.2 Feature-Based Organization

Each feature should include:

* API client
* UI components
* Page components
* Forms
* Tables
* Validation schemas
* Hooks
* Types
* Tests

Example:

```text
/features/shifts
  /api
  /components
  /forms
  /hooks
  /schemas
  /types
```

## 15.3 State Management

Use:

* TanStack Query for server state
* Zustand for local UI state
* React Hook Form for form state
* IndexedDB for offline data

## 15.4 Role-Aware UI

The UI must hide or disable features based on:

* Role
* Permission
* Station access
* Workflow state
* Tenant configuration

Permission checks should happen on both frontend and backend. Frontend checks are for UX only; backend checks are authoritative.

## 15.5 Design System

The design system should provide:

* App shell
* Sidebar
* Top bar
* Cards
* KPI cards
* Tables
* Data grids
* Charts
* Forms
* Drawers
* Modals
* Command palette
* Timeline components
* Approval components
* Alert components
* Tank visual components
* Pump visual components
* Status badges
* Empty states
* Loading states
* Error states

## 15.6 Dashboard Performance

Dashboards should use:

* Cached summary endpoints
* Incremental loading
* Skeleton states
* Server-side filtering
* Aggregated reporting tables
* Background refresh

---

# 16. Mobile and Offline Architecture

## 16.1 Offline-First Principle

Critical station workflows must work without internet.

Offline-supported workflows:

* Open shift
* Enter meter readings
* Enter tank readings
* Record expenses
* Receive deliveries
* Capture photos
* Submit cash
* Close shift
* Queue approvals

## 16.2 Local Storage

Mobile/PWA local storage should include:

* User session metadata
* Assigned station
* Assigned shifts
* Active pump assignments
* Offline form drafts
* Pending sync operations
* Local audit entries
* Attachments awaiting upload

Use encrypted local storage for sensitive data.

## 16.3 Sync Queue

Each offline action should create a sync queue item:

* local_id
* operation_type
* entity_type
* payload
* idempotency_key
* created_at
* retry_count
* status
* error_message

## 16.4 Sync Process

Sync workflow:

1. User performs offline action.
2. App stores local operation.
3. App updates local UI optimistically where safe.
4. Network returns.
5. Sync engine sends queued operations.
6. Server validates permissions and state.
7. Server accepts, rejects, or marks conflict.
8. App updates local state.
9. Conflicts are sent to resolution UI.

## 16.5 Conflict Handling

Conflicts must preserve all submitted data.

Conflict examples:

* Two closing meter readings submitted for same pump
* Delivery received twice
* Shift closed by supervisor while attendant was offline
* Cash submitted after daily close

Conflict states:

* no_conflict
* server_rejected
* manual_resolution_required
* accepted_with_adjustment

## 16.6 Offline Audit

Every offline action must record:

* user_id
* device_id
* local_timestamp
* operation
* payload checksum
* sync_timestamp
* server_result

---

# 17. Reporting and Analytics Architecture

## 17.1 Reporting Types

FuelGrid OS should support:

* Operational reports
* Inventory reports
* Sales reports
* Finance reports
* Customer reports
* Fleet reports
* Risk reports
* Audit reports
* Executive reports

## 17.2 Reporting Data Strategy

Use PostgreSQL for transactional reports requiring current truth.

Use ClickHouse/materialized views for:

* Historical analytics
* Large date ranges
* Trend analysis
* Station ranking
* Risk trends
* High-volume sensor data

## 17.3 Report Generation

Reports should support:

* Synchronous generation for small reports
* Async jobs for large reports
* Export to PDF
* Export to Excel
* Export to CSV
* Scheduled delivery
* Report templates

## 17.4 Report Security

Reports must enforce:

* Tenant scope
* Station scope
* Role permissions
* Financial data permissions
* Export permissions

---

# 18. Risk and Fraud Architecture

## 18.1 Risk Engine Inputs

The risk engine should consume:

* Stock reconciliations
* Tank variances
* Pump meter readings
* Cash reconciliations
* Delivery variances
* Audit logs
* Price changes
* Voids/corrections
* Credit sales
* Staff assignments

## 18.2 Rule-Based Detection

Initial risk detection can be rule-based:

* Variance exceeds tolerance
* Cash shortage exceeds threshold
* Repeated shortage by attendant
* Delivery received quantity below loaded quantity
* Backdated edit after close
* Credit limit exceeded
* Pump sale without matching stock movement

## 18.3 Risk Scoring

Risk score dimensions:

* Fuel variance risk
* Cash shortage risk
* Staff behavior risk
* Delivery risk
* Credit risk
* Audit/edit risk
* Hardware anomaly risk

Scores should be calculated by station, product, shift, attendant, customer, and supplier.

## 18.4 Investigation Workflow

Risk events can create investigation cases.

An investigation should include:

* Risk event
* Assigned investigator
* Related records
* Notes
* Attachments
* Root cause
* Corrective action
* Status
* Closure approval

---

# 19. AI Assistant Architecture

## 19.1 AI Gateway

All AI requests should go through an AI Gateway service.

The AI Gateway should:

* Authenticate user
* Resolve tenant and permissions
* Classify user intent
* Select allowed data tools
* Retrieve relevant data
* Generate answer
* Attach source references
* Log query and response metadata

## 19.2 Permission-Aware Retrieval

AI must never bypass normal permissions.

Before accessing data, AI must check:

* Tenant access
* Station access
* Module permission
* Financial data permission
* Audit data permission
* Customer data permission

## 19.3 AI Query Types

Supported query types:

* Operational summary
* Variance explanation
* Station comparison
* Forecast explanation
* Report generation
* Investigation assistant
* Credit customer summary
* Staff performance summary
* Executive briefing

## 19.4 AI Data Sources

AI can retrieve:

* Aggregated summaries
* Approved reports
* Reconciliation records
* Risk events
* Audit records, if permitted
* Forecast snapshots
* Customer balances, if permitted
* Station performance data

## 19.5 AI Safety Rules

AI must:

* Respect permissions
* Avoid exposing restricted data
* Avoid unsupported claims
* Explain uncertainty
* Recommend actions rather than directly making sensitive changes
* Log queries for audit
* Provide source records where possible

---

# 20. Integration Architecture

## 20.1 Adapter-Based Design

Integrations should be implemented using adapters.

Core structure:

```text
Integration Core
  → Pump Controller Adapter
  → Tank Gauge Adapter
  → POS Adapter
  → Mobile Money Adapter
  → Bank Adapter
  → Accounting Adapter
  → ERP Adapter
  → SMS/WhatsApp Adapter
```

## 20.2 Integration Core Responsibilities

The integration core should handle:

* Credentials
* Connection health
* Event mapping
* Retry logic
* Logging
* Rate limits
* Error handling
* Webhook delivery
* Data normalization

## 20.3 Hardware Integration Pattern

Hardware integrations should not directly write final business records without validation.

Flow:

1. Hardware sends raw event.
2. Integration adapter normalizes event.
3. System validates event.
4. Event is matched to station/tank/pump.
5. Domain service creates business record.
6. Audit log and event are emitted.

## 20.4 Pump Controller Integration

Should support:

* Live pump status
* Meter readings
* Transaction capture
* Nozzle status
* Price synchronization
* Error status

## 20.5 Tank Gauge Integration

Should support:

* Current volume
* Water level
* Temperature
* Ullage
* Sensor health
* Leak warnings
* Historical readings

## 20.6 Financial Integration

Financial integrations should support:

* Mobile money confirmations
* Card settlement imports
* Bank statement matching
* Accounting journal exports
* Customer invoice sync
* Supplier bill sync

---

# 21. Notification Architecture

## 21.1 Notification Channels

Supported channels:

* In-app
* Email
* SMS
* WhatsApp
* Push notification
* Webhook

## 21.2 Notification Pipeline

Flow:

1. Domain event occurs.
2. Rule engine evaluates notification rules.
3. Notification is created.
4. Recipient permissions are checked.
5. Message is rendered using template.
6. Channel adapter sends notification.
7. Delivery status is tracked.

## 21.3 Alert Escalation

Alerts can escalate based on:

* Severity
* Time unacknowledged
* Station risk score
* Financial value
* Repetition
* Role hierarchy

---

# 22. Security Architecture

## 22.1 Security Principles

FuelGrid OS must follow:

* Least privilege
* Defense in depth
* Secure by default
* Audit everything sensitive
* Encrypt sensitive data
* Tenant isolation
* Zero trust for integrations

## 22.2 Encryption

Required encryption:

* TLS for data in transit
* Database encryption at rest
* Object storage encryption
* Encrypted local offline storage
* Secure secrets management

## 22.3 Secrets Management

Secrets should never be stored in source code.

Use:

* Environment variables for local development
* Secrets manager in production
* Rotation policy for API keys
* Separate credentials per environment

## 22.4 API Security

APIs must include:

* Authentication
* Authorization
* Rate limiting
* Input validation
* Idempotency keys
* Request IDs
* Audit logging for sensitive actions

## 22.5 Webhook Security

Webhooks must support:

* Signing secrets
* Delivery retries
* Timestamp validation
* Replay protection
* Delivery logs

## 22.6 Audit Security

Audit logs should be append-only and protected from normal edits.

Only highly privileged system processes should write audit logs.

---

# 23. Deployment Architecture

## 23.1 Environments

Recommended environments:

* local
* development
* staging
* production
* sandbox for integrations

## 23.2 Containerization

All backend services should be containerized with Docker.

## 23.3 Production Deployment

Production should be Kubernetes-ready, even if initial deployment is simpler.

Production components:

* Web app hosting
* API service
* Worker service
* PostgreSQL
* Redis
* Event broker
* ClickHouse
* Object storage
* Monitoring stack
* Log aggregation

## 23.4 Background Workers

Workers should handle:

* Event publishing
* Report generation
* Notification sending
* Webhook delivery
* Offline sync processing
* Integration polling
* Forecast generation
* Risk scoring
* Export jobs

## 23.5 CI/CD

CI/CD should include:

* Linting
* Type checking
* Unit tests
* Integration tests
* Migration checks
* Security scanning
* Container build
* Deployment approvals

---

# 24. Observability Architecture

## 24.1 Logging

Use structured logs with:

* request_id
* correlation_id
* tenant_id
* user_id
* service
* operation
* status
* latency
* error

## 24.2 Metrics

Track metrics such as:

* API latency
* API error rate
* Database latency
* Event processing lag
* Sync failures
* Notification failures
* Webhook failures
* Report generation time
* Integration health

## 24.3 Tracing

Use OpenTelemetry to trace:

* API requests
* Database queries
* Event flows
* Background jobs
* Integration calls
* AI queries

## 24.4 Business Observability

Track business health metrics:

* Stations active
* Shifts open
* Daily closes pending
* Stockout risks
* Critical alerts
* Failed reconciliations
* Cash shortages
* Offline devices pending sync

---

# 25. Testing Architecture

## 25.1 Test Types

The system should include:

* Unit tests
* Integration tests
* API contract tests
* Permission tests
* Workflow tests
* Event consumer tests
* Offline sync tests
* Security tests
* Load tests
* End-to-end tests

## 25.2 Critical Test Scenarios

Must test:

* Tenant isolation
* Permission enforcement
* Shift open/close
* Daily close
* Stock reconciliation
* Cash reconciliation
* Delivery receiving
* Credit limit enforcement
* Offline conflict resolution
* Audit log creation
* Record locking
* Webhook delivery
* AI permission boundaries

## 25.3 Test Data

Test data should include:

* Single station tenant
* Multi-station tenant
* Multiple roles
* Multiple fuel products
* Deliveries
* Pump readings
* Tank readings
* Sales
* Cash shortages
* Credit customers
* Risk alerts

---

# 26. Performance and Scalability Requirements

## 26.1 Performance Targets

Recommended targets:

* Dashboard initial load under 3 seconds for normal data ranges
* API p95 latency under 500ms for common operations
* Report generation async for heavy reports
* Offline sync should handle retry safely
* Event processing should be near real-time for alerts

## 26.2 Scaling Strategy

Scale by:

* Read replicas for PostgreSQL
* Caching dashboard summaries
* Event-driven projections
* ClickHouse for analytics
* Background job workers
* Horizontal API scaling
* Integration worker scaling

## 26.3 Large Tenant Strategy

For large enterprise tenants, consider:

* Dedicated database
* Dedicated worker queues
* Dedicated analytics partitioning
* Custom retention policies
* Enterprise-specific integration capacity

---

# 27. Backup and Disaster Recovery

## 27.1 Backup Requirements

Backups must include:

* PostgreSQL backups
* Object storage backup/versioning
* ClickHouse backup strategy
* Configuration backup
* Secrets backup through secure provider

## 27.2 Recovery Objectives

Define:

* RPO: Recovery Point Objective
* RTO: Recovery Time Objective

Recommended targets for production:

* RPO: 15 minutes or better for core transactional data
* RTO: 4 hours or better for standard production recovery

## 27.3 Disaster Recovery Testing

The team should periodically test:

* Database restore
* Object restore
* Environment recreation
* Integration recovery
* Report regeneration from source data

---

# 28. Compliance and Data Retention

## 28.1 Retention Policies

The system should support configurable retention for:

* Audit logs
* Financial records
* Reports
* Sensor readings
* Integration logs
* AI query logs
* Attachments

## 28.2 Data Export

Tenants should be able to export:

* Reports
* Audit logs
* Transaction history
* Customer statements
* Inventory ledger
* Finance records

## 28.3 Data Deletion

Deletion must be controlled carefully.

For financial and operational records, prefer:

* Soft delete
* Void
* Correction
* Archive

rather than physical deletion.

---

# 29. Implementation Roadmap Alignment

The architecture supports these construction phases:

## Phase 1: Platform Foundation

* Monorepo setup
* Design system
* App shell
* Authentication
* Multi-tenancy
* Company/region/station setup
* User management
* RBAC
* Audit foundation
* API foundation
* Database migrations

## Phase 2: Fuel Infrastructure Core

* Product management
* Tank management
* Pump management
* Nozzle management
* Tank-to-pump mapping
* Calibration data

## Phase 3: Station Operations Core

* Operating day
* Shift opening
* Shift closing
* Attendant assignment
* Pump assignment
* Readings
* Approvals
* Daily close

## Phase 4: Inventory and Reconciliation Engine

* Stock ledger
* Movement events
* Sales depletion
* Delivery stock posting
* Variance calculation
* Adjustment approval

## Phase 5: Delivery and Procurement OS

* Suppliers
* Purchase orders
* Delivery receiving
* Delivery documents
* Supplier invoice linkage

## Phase 6: Sales, Payments and Revenue OS

* Sales
* Payment methods
* Cash/mobile/card/credit sales
* Revenue reports
* Voids/corrections

## Phase 7: Finance and Accounting Control

* Cash reconciliation
* Expenses
* Deposits
* Invoices
* Supplier bills
* Profit/loss

## Phase 8: Customer Credit and Fleet Fuel OS

* Customers
* Credit limits
* Vehicles
* Drivers
* Authorizations
* Fleet reports

## Phase 9: Chain and Enterprise Command

* Executive dashboard
* Regional dashboards
* Station ranking
* Central pricing
* Consolidated reporting

## Phase 10: Risk, Fraud and Intelligence

* Rule engine
* Risk scores
* Alerts
* Investigations
* Fraud detection

## Phase 11: Forecasting and Automation

* Demand forecasting
* Stockout prediction
* Reorder recommendations
* Scheduled reports

## Phase 12: AI Assistant

* Permission-aware AI
* Variance explanations
* Report generation
* Investigation assistant

## Phase 13: Hardware and External Integrations

* Pump adapters
* Tank adapters
* POS
* Mobile money
* Bank/accounting integrations

## Phase 14: Mobile and Offline OS

* Mobile workflows
* Offline storage
* Sync engine
* Conflict resolution

## Phase 15: Enterprise Hardening

* Security hardening
* Observability
* Backups
* Load testing
* Compliance

---

# 30. Architecture Acceptance Criteria

The architecture is acceptable when:

## 30.1 Domain Integrity

* Domains are clearly separated.
* Business rules live in domain/application layers.
* Data access does not bypass permissions.
* Cross-domain communication uses explicit interfaces or events.

## 30.2 Traceability

* Every sensitive action creates an audit log.
* Every stock movement is ledgered.
* Every sale connects to stock and payment data.
* Every event has a correlation ID.

## 30.3 Security

* Tenant isolation is enforced.
* Permissions are enforced backend-side.
* Sensitive actions require audit and optional approval.
* API keys and webhooks are secured.

## 30.4 Offline Reliability

* Critical workflows can be performed offline.
* Offline operations sync safely.
* Conflicts preserve all submitted data.
* Offline audit entries are retained.

## 30.5 Scalability

* Reporting can move to analytics storage.
* Domain modules can become services later.
* Workers can scale independently.
* Large tenants can receive dedicated infrastructure in future.

## 30.6 Operational Excellence

* Logs, metrics, and traces exist.
* Backups and recovery are planned.
* Events are idempotent.
* Background jobs are retryable.
* Integrations have health monitoring.

---

# 31. Final Architecture Definition

FuelGrid OS should be built as a modular, event-driven, multi-tenant fuel operations platform with financial-grade traceability, offline-capable station workflows, analytics-ready data pipelines, secure integrations, AI-powered operational intelligence, and enterprise-ready security.

The system should begin as a modular monolith with strict domain boundaries and evolve toward distributed services only when required by scale, enterprise needs, or operational complexity.

The final architecture must support the full mission of FuelGrid OS:

**to control every liter, every transaction, every station, and every decision from one intelligent platform.**
