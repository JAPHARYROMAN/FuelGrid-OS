# FuelGrid OS — Mobile Attendant App Product Requirements Document

**Product:** FuelGrid OS
**Module:** Mobile Attendant App
**Document Type:** Product Requirements Document
**Purpose:** Define the full product requirements for the simplified mobile application used by pump attendants during daily station operations.
**AI Scope:** No AI features required for this module. Intelligence should come from workflow rules, validations, supervisor approvals, audit logs, and automated calculations.

## 1. Product Overview

The FuelGrid OS Mobile Attendant App is a simplified mobile application designed for pump attendants working at fuel stations. It gives attendants only the tools they need to participate in daily shift operations, confirm attendance, verify assigned pumps and nozzles, confirm opening readings, submit closing readings, submit collections, receive supervisor feedback, and complete shift handover.

The mobile app is not a full version of FuelGrid OS. It is a focused field operations tool. It must be accessible, easy to use, fast, and reliable in real fuel station environments.

The app must support attendants with different levels of digital experience. It should use clear language, large controls, strong contrast, step-by-step flows, and minimal navigation.

The core product principle is:

> The attendant app should be simple enough for daily pump work, but strong enough to protect every litre, every shilling, every shift, and every handover.

## 2. Product Goals

### 2.1 Business Goals

The Mobile Attendant App should help the business:

- Confirm that assigned attendants are physically present for work.
- Link each attendant to a specific shift.
- Link each attendant to assigned pumps, nozzles, and products.
- Ensure opening meter readings are verified before shift work begins.
- Ensure closing meter readings are submitted at the end of each shift.
- Ensure supervisors verify submitted readings before collections are finalized.
- Calculate litres sold using approved meter readings.
- Calculate expected collections using company-controlled prices.
- Capture submitted collections and supervisor-confirmed received collections.
- Track shortages, excesses, corrections, approvals, and reasons.
- Prevent the next shift from opening until the previous shift is properly verified.
- Preserve traceability for attendance, readings, collections, corrections, and handovers.

### 2.2 User Goals

The attendant should be able to:

- Log in easily.
- See the current assigned shift.
- Confirm attendance.
- View assigned pump and nozzles.
- Verify opening readings.
- Open the shift when all requirements are met.
- Submit closing meter readings.
- Receive supervisor approval or correction status.
- View expected collections after verified readings.
- Submit collections.
- Know whether the shift is completed.
- Receive clear instructions when something requires supervisor attention.

### 2.3 System Goals

The system should:

- Enforce shift workflow rules.
- Preserve original attendant submissions.
- Preserve supervisor corrections separately from attendant submissions.
- Use approved readings for final calculations.
- Prevent unauthorized price changes.
- Ensure all critical actions are audited.
- Support offline entry where possible.
- Sync offline data safely.
- Prevent duplicate or conflicting submissions.
- Feed data into the main FuelGrid OS modules.

## 3. Product Scope

### 3.1 In Scope

The Mobile Attendant App includes:

- Authentication
- Shift visibility
- Attendance confirmation
- Pump and nozzle assignment visibility
- Opening meter reading verification
- Shift opening
- Active shift status
- Closing meter reading submission
- Supervisor review status
- Collections calculation display
- Collections submission
- Supervisor collection confirmation status
- Notifications
- Issue reporting
- Offline support for critical field actions
- Accessibility features
- Multilingual interface support
- Audit event creation

### 3.2 Out of Scope

The Mobile Attendant App should not include:

- Company-wide dashboards
- Profit and loss reports
- Supplier cost visibility
- Price editing
- User management
- Role management
- Station configuration
- Product configuration
- Full reports module
- Audit log browsing
- Finance management
- Procurement management
- Customer credit management
- Fleet administration
- System settings
- AI assistant functionality

## 4. Target Users

### 4.1 Pump Attendant

The primary user of the mobile app.

Responsibilities:

- Check in for assigned shift.
- Confirm pump and nozzle assignment.
- Verify opening meter readings.
- Open the shift when allowed.
- Submit closing readings.
- Submit collections.
- Respond to supervisor feedback.
- Complete shift handover.

Access level:

- Own shift only
- Own pump assignment only
- Own submitted data
- Own supervisor feedback
- Own collection submission

### 4.2 Supervisor

The supervisor does not primarily use the attendant app, but the attendant app depends on supervisor actions from the main FuelGrid OS supervisor interface.

Supervisor responsibilities connected to the app:

- Assign attendants to pumps and nozzles.
- Verify opening readiness.
- Review closing readings.
- Approve, reject, or correct submitted readings.
- Confirm collections received.
- Record shortages, excesses, and reasons.
- Finalize shift handover.
- Unlock the next shift.

### 4.3 Station Manager

The station manager may view outputs from the attendant workflow in the main FuelGrid OS.

Station manager responsibilities connected to the app:

- Monitor attendance.
- Monitor shift progress.
- Review exceptions.
- Review shortages and excesses.
- Approve escalated issues.
- Use submitted data in daily close.

## 5. User Experience Principles

### 5.1 Simplicity

The attendant app must be simple and task-based. It should not expose unnecessary menus or complex dashboards.

### 5.2 Guided Workflow

The app should guide the attendant from one required step to the next.

The main flow should be:

```text
Login
→ View Shift
→ Check In
→ Confirm Assignment
→ Verify Opening Readings
→ Open Shift
→ Submit Closing Readings
→ Wait for Supervisor Verification
→ Submit Collections
→ Wait for Supervisor Collection Confirmation
→ Complete Shift
```

### 5.3 Accessibility

The app must be usable by attendants with different digital skill levels and in field conditions.

Accessibility requirements include:

- Large buttons
- Large numeric inputs
- High contrast
- Clear labels
- Simple text
- Minimal screens
- Icons with text labels
- Strong error messages
- Readable typography
- Touch-friendly layout
- Outdoor visibility support
- Multilingual support
- Clear workflow status
- Error prevention before submission

### 5.4 Field Readiness

The app should work in real station environments where users may face:

- Bright sunlight
- Noise
- Dust
- Fast-paced shift changes
- Unstable internet
- Shared working areas
- Time pressure
- Limited technical confidence

### 5.5 Traceability

The app interface should remain simple, but the backend record must be complete. Every critical user action should be stored, timestamped, and auditable.

## 6. Mobile App Modules

The Mobile Attendant App should contain the following limited modules.

### 6.1 Login

Allows the attendant to authenticate.

Required capabilities:

- Login with approved credential method.
- Session creation.
- Session expiration handling.
- Secure logout.
- Password reset support if enabled.
- Optional PIN or biometric unlock after login.
- Language selection.

### 6.2 My Shift

Shows the attendant's assigned shift and current workflow state.

Required capabilities:

- Display current shift.
- Display station.
- Display shift time.
- Display attendance status.
- Display pump assignment status.
- Display shift readiness status.
- Display next required action.

### 6.3 Attendance / Roll Call

Allows the attendant to confirm presence at work.

Required capabilities:

- Check in.
- Check out where required.
- Capture check-in timestamp.
- Capture device/session information.
- Optional location verification.
- Show attendance status.
- Prevent duplicate check-in.
- Notify supervisor of check-in.

### 6.4 My Pump and Nozzles

Shows pump and nozzle assignment made by the supervisor.

Required capabilities:

- Display assigned pump.
- Display assigned nozzles.
- Display product per nozzle.
- Display system opening readings.
- Display assignment status.
- Allow attendant to confirm assignment.
- Notify attendant when assignment changes.

### 6.5 Opening Meter Verification

Allows the attendant to verify physical opening readings against system readings.

Required capabilities:

- Show system opening reading per nozzle.
- Allow attendant to confirm physical match.
- Allow entry of physical reading if confirmation requires entry.
- Validate reading format.
- Validate reading direction.
- Detect mismatch.
- Block shift opening if readings are not approved.
- Send mismatch to supervisor review.
- Store attendant confirmation.

### 6.6 Shift Opening

Allows the attendant to open the assigned shift only after all requirements are satisfied.

Required capabilities:

- Validate attendance.
- Validate pump/nozzle assignment.
- Validate previous shift closure.
- Validate opening readings.
- Validate station operating day status.
- Validate active prices.
- Allow shift opening only when all conditions pass.
- Record shift opening event.
- Lock opening readings after shift opening.

### 6.7 Active Shift

Shows basic shift status during work.

Required capabilities:

- Display active shift status.
- Display assigned pump and nozzles.
- Display supervisor messages.
- Display issue reporting action.
- Display sync status.
- Avoid exposing unnecessary financial or management screens.

### 6.8 Closing Meter Submission

Allows the attendant to enter end-of-shift physical meter readings.

Required capabilities:

- Display opening reading per nozzle.
- Capture closing reading per nozzle.
- Validate closing reading.
- Calculate litres sold per nozzle.
- Prevent negative litre values.
- Flag abnormal readings.
- Allow notes where required.
- Submit readings to the main system.
- Lock attendant submission after submission unless correction is requested.

### 6.9 Supervisor Review Status

Shows the status of submitted readings.

Required capabilities:

- Display pending review status.
- Display approved status.
- Display correction required status.
- Display supervisor-corrected status.
- Display rejected status.
- Display investigation flag status.
- Notify attendant when supervisor acts.
- Show final approved values after supervisor verification.
- Preserve original attendant values in backend audit records.

### 6.10 Collections

Allows the attendant to view expected collections and submit collected amounts.

Required capabilities:

- Display expected collection amount after readings are verified.
- Display litres sold by product.
- Display calculation basis.
- Capture submitted collection amount.
- Support collection breakdown where enabled.
- Calculate difference.
- Require reason for shortage or excess.
- Submit collection record.
- Show supervisor receipt status.

### 6.11 Notifications

Keeps attendant informed about workflow changes.

Required capabilities:

- Assignment notification.
- Opening approval notification.
- Reading correction notification.
- Reading approval notification.
- Collection confirmation notification.
- Shift completion notification.
- Supervisor message notification.
- Sync failure notification.
- Required action notification.

### 6.12 Report Issue

Allows attendants to report operational problems.

Required capabilities:

- Report pump issue.
- Report nozzle issue.
- Report meter issue.
- Report payment issue.
- Report safety issue.
- Add note.
- Attach photo where enabled.
- Notify supervisor.

## 7. Functional Requirements

### 7.1 Authentication Requirements

The system must allow attendants to securely log into the mobile app.

Requirements:

- The app must authenticate attendants against FuelGrid OS identity services.
- The app must only show data for the authenticated attendant.
- Sessions must expire according to company security policy.
- The app must support secure logout.
- The app must prevent access after logout.
- The app must handle expired sessions clearly.
- The app must not expose another attendant's shift data.

### 7.2 Shift Visibility Requirements

The app must display only shifts assigned to the attendant or shifts the attendant is permitted to join.

Requirements:

- Show active assigned shift.
- Show shift time.
- Show station.
- Show attendance status.
- Show assignment status.
- Show workflow progress.
- Hide unrelated station or company shifts.
- Prevent opening a shift that has not been assigned or approved.

### 7.3 Attendance Requirements

The app must support attendance confirmation.

Requirements:

- The attendant must check in before opening a shift.
- The system must record check-in time.
- The system must link attendance to shift, station, and user.
- The system must prevent duplicate attendance records for the same shift unless policy allows corrections.
- The supervisor must be able to see attendance status.
- Late check-ins must be flagged according to shift rules.
- Absence must be visible to supervisor if an assigned attendant does not check in.
- Attendance actions must create audit events.

### 7.4 Pump and Nozzle Assignment Requirements

The supervisor must assign pumps and nozzles through the supervisor system. The attendant app displays the assignment.

Requirements:

- Assignment must include pump.
- Assignment must include nozzle or nozzles.
- Assignment must include product per nozzle.
- Assignment must include system opening readings.
- Assignment must be linked to shift and attendant.
- Assignment changes must notify the attendant.
- The attendant must confirm the assignment before opening the shift.
- The attendant must not assign themselves to pumps or nozzles unless company policy allows self-selection.
- The attendant must not change product mappings.

### 7.5 Opening Reading Requirements

The attendant must verify opening readings before opening the shift.

Requirements:

- Opening readings must come from the previous approved closing readings or supervisor-approved starting values.
- The app must display expected opening readings.
- The attendant must confirm physical readings.
- The app must detect mismatch.
- The app must prevent shift opening if required readings are missing.
- The app must prevent shift opening if readings are outside allowed tolerance and not supervisor-approved.
- The app must send mismatches to supervisor review.
- The app must store attendant confirmation.
- Opening reading verification must create audit records.

### 7.6 Shift Opening Requirements

The app must allow shift opening only when all required preconditions are satisfied.

Required preconditions:

- Attendant is authenticated.
- Attendant is assigned to the shift.
- Attendant has checked in.
- Pump and nozzle assignment exists.
- Previous shift is closed or supervisor-resolved.
- Opening readings are verified or approved.
- Station operating day is open.
- Product prices are active.
- No blocking supervisor red flag exists.

The system must:

- Record who opened the shift.
- Record opening time.
- Lock opening readings.
- Set the attendant's shift status to active.
- Notify supervisor that the attendant shift has opened.

### 7.7 Closing Reading Requirements

At the end of the shift, the attendant must submit closing readings.

Requirements:

- The app must show opening reading per nozzle.
- The attendant must enter closing reading per nozzle.
- The system must validate that closing reading is not lower than opening reading unless supervisor correction policy handles special cases.
- The system must calculate litres sold.
- The system must flag abnormal readings.
- The system must require all assigned nozzles to be completed.
- The system must allow notes where required.
- The system must submit readings to supervisor review.
- The system must preserve original attendant-submitted readings.
- The system must prevent final collections submission until readings are supervisor-approved or supervisor-corrected.

### 7.8 Supervisor Verification Requirements

Supervisor verification happens outside the attendant app, but the attendant app must reflect the result.

Requirements:

- Supervisor must be able to approve readings.
- Supervisor must be able to reject readings.
- Supervisor must be able to request correction.
- Supervisor must be able to enter verified readings.
- Supervisor must be able to add a reason for correction.
- Supervisor must be able to flag suspicious readings.
- The attendant app must notify the attendant of supervisor decision.
- The final approved reading must be shown to the attendant.
- The attendant's original submitted reading must remain stored.
- The system must maintain a full correction history.

### 7.9 Collections Calculation Requirements

The system must calculate expected collections after readings are approved.

Requirements:

- Use approved opening and closing readings.
- Calculate litres sold per nozzle.
- Group litres by product where needed.
- Use company-controlled price per product.
- Prevent attendants from editing prices.
- Calculate expected amount.
- Support payment method breakdown where enabled.
- Display expected collection to attendant.
- Keep calculation traceable to readings and prices.

### 7.10 Collections Submission Requirements

The attendant must submit collections after expected amount is calculated.

Requirements:

- The app must display expected collection.
- The attendant must enter submitted collection.
- The app must calculate difference.
- The app must require reason if submitted collection does not match expected collection.
- The app must allow collection breakdown if enabled.
- The app must submit collections to supervisor.
- The app must lock submission after submission unless correction is requested.
- The app must notify attendant when supervisor confirms receipt.

### 7.11 Supervisor Collection Confirmation Requirements

Supervisor confirms actual collections received through supervisor system.

Requirements:

- Supervisor must view expected collection.
- Supervisor must view attendant-submitted collection.
- Supervisor must enter or confirm received collection.
- System must calculate shortage or excess.
- Supervisor must record reason for shortage or excess.
- Supervisor must be able to approve with difference.
- Supervisor must be able to reject collection submission.
- Supervisor must be able to flag collection for investigation.
- The attendant app must display final collection status.

### 7.12 Shift Completion Requirements

The attendant shift should be completed only after required checks pass.

Requirements:

- Closing readings submitted.
- Supervisor reading verification completed.
- Collections submitted.
- Supervisor collection confirmation completed.
- Shortage or excess recorded where applicable.
- Required reasons captured.
- Shift status finalized.
- Attendant notified of completion.
- Next shift unlocking logic triggered where applicable.

### 7.13 Shift Handover Requirements

The next shift must depend on the previous shift's verified closure.

Requirements:

- Previous shift closing readings must be verified.
- Final approved readings must become next shift opening readings.
- Collections must be submitted and supervisor-handled.
- Shift close must be finalized before next shift opens unless supervisor override policy allows exception.
- Any override must be audited.
- The system must prevent broken handover chains.

## 8. Workflow Requirements

### 8.1 Full Attendant Workflow

```text
Attendant logs in
→ App loads assigned shift
→ Attendant checks in
→ Supervisor assigns pump and nozzles
→ Attendant confirms assignment
→ Attendant verifies opening readings
→ System validates readiness
→ Attendant opens shift
→ Attendant works active shift
→ Attendant submits closing readings
→ Supervisor verifies readings
→ System calculates expected collections
→ Attendant submits collections
→ Supervisor confirms collections
→ System records shortage or excess if any
→ Shift is finalized
→ Next shift is unlocked
```

### 8.2 Attendance Workflow

```text
Login
→ View assigned shift
→ Confirm attendance
→ System records check-in
→ Supervisor sees attendance status
```

### 8.3 Opening Shift Workflow

```text
Attendance confirmed
→ Assignment confirmed
→ Opening readings verified
→ Previous shift verified
→ Active prices confirmed
→ Open shift
→ Opening readings locked
```

### 8.4 Closing Reading Workflow

```text
Open closing screen
→ Enter closing readings
→ System validates readings
→ System calculates litres sold
→ Submit readings
→ Supervisor review starts
```

### 8.5 Supervisor Correction Workflow

```text
Attendant submits readings
→ Supervisor reviews
→ Supervisor approves or corrects
→ System stores attendant values
→ System stores supervisor values
→ Final approved values are applied
→ Attendant is notified
```

### 8.6 Collections Workflow

```text
Readings approved
→ Expected collection calculated
→ Attendant enters submitted collection
→ Difference calculated
→ Reason captured if needed
→ Supervisor receives collection record
→ Supervisor confirms received amount
→ Shortage or excess stored
→ Shift collection finalized
```

### 8.7 Next Shift Unlock Workflow

```text
Previous shift readings approved
→ Previous shift collections handled
→ Previous shift finalized
→ Final readings become next opening readings
→ Next shift attendants verify readings
→ Next shift can open
```

## 9. Status Requirements

### 9.1 Attendance Statuses

```text
Not Checked In
Checked In
Late
Absent
Reassigned
Checked Out
```

### 9.2 Assignment Statuses

```text
Pending Assignment
Assigned
Confirmed
Changed
Cancelled
```

### 9.3 Opening Reading Statuses

```text
Pending Verification
Verified
Mismatch
Supervisor Review Required
Supervisor Approved
Blocked
```

### 9.4 Shift Statuses

```text
Not Started
Ready to Open
Open
Closing Readings Pending
Supervisor Review Pending
Collections Pending
Supervisor Collection Pending
Completed
Blocked
Flagged
```

### 9.5 Reading Review Statuses

```text
Pending Supervisor Review
Approved
Correction Required
Supervisor Corrected
Rejected
Flagged for Investigation
```

### 9.6 Collection Statuses

```text
Not Available
Pending Submission
Submitted
Received
Partially Received
Shortage
Excess
Approved with Difference
Rejected
Flagged
Completed
```

### 9.7 Sync Statuses

```text
Online
Offline
Syncing
Synced
Sync Failed
Conflict Pending
```

## 10. Data Requirements

### 10.1 Attendance Data

The system must store:

```text
attendance_id, tenant_id, station_id, shift_id, attendant_id,
check_in_time, check_out_time, attendance_status, device_id,
session_id, location_data, created_at, updated_at
```

### 10.2 Pump Assignment Data

The system must store:

```text
assignment_id, tenant_id, station_id, shift_id, attendant_id, pump_id,
assigned_by, assignment_status, assigned_at, confirmed_at, created_at, updated_at
```

### 10.3 Nozzle Assignment Data

The system must store:

```text
nozzle_assignment_id, assignment_id, nozzle_id, product_id, tank_id,
opening_reading_id, assignment_status, created_at, updated_at
```

### 10.4 Opening Reading Confirmation Data

The system must store:

```text
confirmation_id, tenant_id, station_id, shift_id, attendant_id, pump_id,
nozzle_id, system_opening_reading, attendant_confirmed_reading, status,
difference, submitted_at, supervisor_review_status, created_at, updated_at
```

### 10.5 Closing Reading Submission Data

The system must store:

```text
submission_id, tenant_id, station_id, shift_id, attendant_id, pump_id,
nozzle_id, opening_reading, closing_reading_submitted, litres_sold_submitted,
submission_status, submitted_at, notes, created_at, updated_at
```

### 10.6 Supervisor Verified Reading Data

The system must store:

```text
verified_reading_id, tenant_id, station_id, shift_id, attendant_id, pump_id,
nozzle_id, attendant_submission_id, attendant_submitted_reading,
supervisor_verified_reading, final_approved_reading, difference, review_status,
reviewed_by, reviewed_at, reason, created_at, updated_at
```

### 10.7 Collection Submission Data

The system must store:

```text
collection_submission_id, tenant_id, station_id, shift_id, attendant_id,
expected_amount, submitted_amount, difference, reason, payment_breakdown,
submission_status, submitted_at, created_at, updated_at
```

### 10.8 Collection Receipt Data

The system must store:

```text
collection_receipt_id, tenant_id, station_id, shift_id, attendant_id,
supervisor_id, expected_amount, attendant_submitted_amount,
supervisor_received_amount, difference, receipt_status, reason,
supervisor_comment, received_at, created_at, updated_at
```

### 10.9 Shift Completion Data

The system must store:

```text
shift_completion_id, tenant_id, station_id, shift_id, attendant_id,
readings_status, collections_status, shortage_amount, excess_amount,
completion_status, completed_at, created_at, updated_at
```

## 11. Permissions and Access Control

### 11.1 Attendant Permissions

Attendants should be allowed to:

```text
View own shift
Check in to assigned shift
View own pump/nozzle assignment
Confirm own assignment
Verify own opening readings
Open own shift when allowed
Submit own closing readings
View supervisor reading status
View expected collections for own shift
Submit own collections
View own collection status
Report issue
Receive notifications
```

### 11.2 Attendant Restrictions

Attendants must not be allowed to:

```text
Edit product prices
Assign pumps or nozzles
Approve readings
Correct final readings
Confirm received collections
View other attendants' sensitive data
View company-wide reports
View profit margins
View supplier costs
Manage users
Change system settings
Bypass supervisor approvals
Delete submitted records
```

### 11.3 Supervisor Permissions Required by Workflow

Supervisors must be allowed to:

```text
Assign attendants to pumps/nozzles
View attendance
Review opening reading mismatches
Approve closing readings
Correct closing readings
Reject readings
Flag readings
Confirm collections
Record shortages/excesses
Finalize shift handover
Unlock next shift
```

## 12. Pricing and Calculation Rules

### 12.1 Product Price Control

Fuel prices must be controlled centrally.

Requirements:

- Prices must be created and edited only by authorized company users.
- Attendants must not edit prices.
- Collection calculations must use the active approved price.
- Price used for calculation must be stored with the collection record.
- Price changes must not retroactively alter already finalized shift calculations.
- Any price override must be permission-controlled and audited.

### 12.2 Litres Calculation

The system must calculate litres sold using:

```text
Litres Sold = Approved Closing Reading - Approved Opening Reading
```

Requirements:

- Use approved readings.
- Prevent negative results unless supervisor-approved correction policy allows exception.
- Use decimal-safe arithmetic.
- Preserve raw and approved readings.

### 12.3 Expected Collection Calculation

The system must calculate expected collection using:

```text
Expected Collection = Litres Sold × Approved Product Price
```

Requirements:

- Use final approved readings.
- Use system-controlled prices.
- Support multiple products per attendant.
- Support multiple nozzles per attendant.
- Support future payment method breakdown.
- Preserve calculation source data.

### 12.4 Difference Calculation

The system must calculate:

```text
Difference = Supervisor Received Amount - Expected Collection
```

or, before supervisor receipt:

```text
Difference = Attendant Submitted Amount - Expected Collection
```

Requirements:

- Negative difference represents shortage.
- Positive difference represents excess.
- Zero difference represents balanced collection.
- Any non-zero difference must require a reason according to policy.

## 13. Notifications Requirements

The mobile app must notify attendants about workflow changes.

Notification types:

```text
Shift assigned, Pump/nozzle assigned, Assignment changed, Attendance confirmed,
Opening verification approved, Opening verification blocked, Shift opened,
Closing readings submitted, Closing readings approved, Closing readings corrected,
Closing readings rejected, Collections available, Collections submitted,
Collections received, Shortage recorded, Excess recorded, Shift completed,
Supervisor message received, Sync failed, Conflict requires attention
```

Notification requirements:

- Notifications must be clear and action-oriented.
- Notifications must be linked to the relevant shift.
- Critical notifications must remain visible until acknowledged where required.
- Offline notifications should sync when connection returns.

## 14. Offline Requirements

The app must support critical workflows when internet is unstable.

### 14.1 Offline-Capable Actions

The system should support offline capture for:

```text
Attendance check-in
Opening reading confirmation
Closing reading submission
Collection submission
Issue reporting
Photo attachment capture
```

### 14.2 Sync Requirements

The app must:

- Store offline actions locally.
- Mark unsynced actions clearly.
- Sync actions when internet returns.
- Prevent duplicate submissions through idempotency keys.
- Show sync status.
- Show sync failure.
- Preserve user-entered data during failed sync.
- Send conflicts to supervisor or resolution workflow where required.

### 14.3 Conflict Handling

Conflict scenarios must preserve all submitted data.

The system must not silently discard:

- Offline reading submissions
- Offline collection submissions
- Offline issue reports
- Offline attendance records

If a conflict occurs, the system must:

- Mark conflict status.
- Preserve local submission.
- Preserve server record.
- Require supervisor or system resolution.
- Create audit records.

## 15. Accessibility Requirements

### 15.1 Visual Accessibility

The app must support:

- High contrast interface.
- Large text.
- Large buttons.
- Clear color usage.
- Non-color-only status indicators.
- Readable typography.
- Touch-friendly spacing.
- Outdoor visibility mode.
- Dark mode and light mode.

### 15.2 Language Accessibility

The app should support multilingual UI.

Minimum recommended languages:

```text
English
Swahili
```

Language requirements:

- Language selector should be easy to access.
- Critical workflow labels must be translated.
- Error messages must be translated.
- Notifications must be translated.
- Numeric and currency formatting must match company locale settings.

### 15.3 Cognitive Accessibility

The app must reduce confusion by using:

- Step-by-step screens.
- Clear next action.
- Minimal menu depth.
- Plain language.
- Confirmation before final submission.
- Clear success state.
- Clear blocked state.
- Clear supervisor review state.

### 15.4 Error Prevention

The app must prevent common mistakes.

Validation should detect:

- Missing required readings.
- Closing reading lower than opening reading.
- Invalid numeric input.
- Extremely abnormal reading difference.
- Collection amount missing.
- Difference without reason.
- Attempt to open shift before readiness.
- Attempt to submit collections before reading approval.
- Duplicate submission.

## 16. Security Requirements

The app must follow FuelGrid OS security standards.

Requirements:

- Secure authentication.
- Bearer/session token protection.
- Secure local storage.
- Encrypted offline storage where possible.
- Device identification.
- Session expiration.
- Logout support.
- No sensitive company data exposed unnecessarily.
- No price editing by attendants.
- No hidden supervisor bypass.
- Server-side authorization for every action.
- Audit logging for every sensitive action.

## 17. Audit Requirements

Every critical action must create an audit event.

Required audit events:

```text
Attendant logged in, Attendant checked in, Attendant checked out,
Supervisor assigned pump/nozzles, Attendant confirmed assignment,
Attendant confirmed opening reading, Opening reading mismatch detected,
Attendant opened shift, Attendant submitted closing reading,
Supervisor approved reading, Supervisor corrected reading,
Supervisor rejected reading, Supervisor flagged reading,
Attendant submitted collections, Supervisor confirmed collections,
Shortage recorded, Excess recorded, Supervisor approved collection with difference,
Shift finalized, Next shift unlocked, Offline action synced, Offline conflict detected
```

Audit records must include:

```text
actor_id, tenant_id, station_id, shift_id, action, entity_type, entity_id,
previous_value, new_value, reason, device_id, ip_address, timestamp, correlation_id
```

## 18. Integration With Main FuelGrid OS

The Mobile Attendant App must integrate with the main system modules.

### 18.1 Operations Module

Feeds: attendance, shift status, shift opening, shift completion, handover status.

### 18.2 Pump and Nozzle Module

Feeds: pump assignment, nozzle assignment, product assignment, meter readings.

### 18.3 Inventory Module

Feeds: litres sold from approved readings, stock movement calculations, shift-based depletion.

### 18.4 Revenue Module

Feeds: expected collections, collection submissions, payment method breakdown where enabled.

### 18.5 Reconciliation Module

Feeds: shift readings, approved reading data, cash variance data, handover values.

### 18.6 Risk Module

Feeds: reading mismatches, repeated shortages, abnormal litres sold, supervisor corrections, shift delays, collection differences.

### 18.7 Audit Module

Feeds: all critical actions, corrections, approvals, rejections, overrides, offline conflicts.

## 19. Reporting Impact

The workflow should feed the following reports:

```text
Attendance Report, Shift Report, Attendant Performance Report,
Pump Sales Report, Nozzle Sales Report, Cash Collection Report,
Cash Shortage Report, Cash Excess Report, Reading Correction Report,
Supervisor Approval Report, Daily Station Close Report, Fuel Loss Report,
Risk Report, Audit Report
```

Reports should be able to show: attendant attendance; assigned pumps/nozzles; submitted readings; supervisor verified readings; differences between submitted and verified readings; collections expected/submitted/received; shortage or excess; reasons; supervisor comments; shift completion status.

## 20. UI Screen Requirements

### 20.1 Login Screen

Credential input; login action; language selector; password reset access where enabled; clear error messages; secure session handling.

### 20.2 My Shift Screen

Current shift status; station name; shift time; attendance status; assignment status; next required action; supervisor messages.

### 20.3 Attendance Screen

Check-in action; check-out action where enabled; attendance status; shift team visibility where permitted; confirmation after check-in.

### 20.4 Pump Assignment Screen

Assigned pump; assigned nozzles; product per nozzle; system opening readings; assignment confirmation; assignment change notification.

### 20.5 Opening Reading Screen

Opening reading per nozzle; physical confirmation input; match/mismatch status; validation; supervisor review status; open shift readiness state.

### 20.6 Active Shift Screen

Active status; assigned pump and nozzles; shift start time; supervisor messages; report issue action; sync state.

### 20.7 Closing Reading Screen

Opening reading per nozzle; closing reading input per nozzle; auto-calculated litres sold; validation; notes where needed; submit action.

### 20.8 Supervisor Review Status Screen

Reading review status; supervisor decision; final approved values; correction reason where applicable; next required action.

### 20.9 Collections Screen

Expected collection amount; collection input; difference calculation; reason input when required; submit action; supervisor receipt status.

### 20.10 Shift Complete Screen

Reading approval status; collection confirmation status; shift completion status; check-out action where enabled; final completion confirmation.

## 21. Non-Functional Requirements

### 21.1 Performance

Load quickly on common Android devices; minimize data usage; cache assigned shift data; submit actions quickly; keep forms responsive; avoid heavy dashboards.

### 21.2 Reliability

Preserve user input during connection failure; prevent duplicate submissions; retry failed sync; clearly show submission status; recover gracefully from app restart.

### 21.3 Scalability

Support many attendants per station; many stations per tenant; multiple shifts per day; multiple pump/nozzle assignments; high daily reading and collection volume.

### 21.4 Maintainability

Use the shared API SDK; shared design system where possible; typed validation; clear workflow state machine; reusable form components; testable business rules.

## 22. API Requirements

The Mobile Attendant App requires API support for:

```text
Authentication, Current user profile, Assigned shift,
Attendance check-in/check-out, Pump/nozzle assignment,
Opening reading confirmation, Shift opening, Closing reading submission,
Supervisor review status, Expected collections, Collection submission,
Collection status, Notifications, Issue reporting, Offline sync
```

Suggested endpoint groups:

```text
/api/v1/mobile/me
/api/v1/mobile/shifts/current
/api/v1/mobile/shifts/{shift_id}/attendance
/api/v1/mobile/shifts/{shift_id}/assignment
/api/v1/mobile/shifts/{shift_id}/opening-readings
/api/v1/mobile/shifts/{shift_id}/open
/api/v1/mobile/shifts/{shift_id}/closing-readings
/api/v1/mobile/shifts/{shift_id}/review-status
/api/v1/mobile/shifts/{shift_id}/collections
/api/v1/mobile/notifications
/api/v1/mobile/issues
/api/v1/mobile/sync
```

All endpoints must enforce: tenant scoping; user identity; shift assignment; role permissions; station access; idempotency for submissions; audit logging for sensitive actions.

## 23. Validation Requirements

The system must validate:

```text
User is authenticated
User is assigned to shift
Shift belongs to tenant
Shift belongs to station
Attendance exists before opening
Pump/nozzle assignment exists before opening
Opening readings are verified before opening
Previous shift is closed or supervisor-resolved
Closing readings are complete before submission
Closing readings are valid
Collections are not submitted before reading approval
Submitted collections have required reason when different
Supervisor confirmation exists before final shift completion
```

## 24. Testing Requirements

### 24.1 Unit Tests

Reading calculations; collection calculations; status transitions; validation rules; difference calculations; required reason rules.

### 24.2 API Tests

Attendance submission; opening reading confirmation; shift opening; closing reading submission; collections submission; unauthorized access; cross-tenant access prevention; duplicate submission prevention; permission enforcement.

### 24.3 Offline Tests

Offline attendance capture; offline reading capture; offline collection capture; sync retry; duplicate prevention; conflict detection.

### 24.4 UI Tests

Login flow; check-in flow; assignment confirmation flow; opening reading flow; closing reading flow; collection submission flow; error states; loading states; offline states.

### 24.5 Accessibility Tests

Large touch targets; high contrast; screen readability; language switching; error message clarity; keyboard/input handling where relevant.

## 25. Acceptance Criteria

The Mobile Attendant App is acceptable when:

- Attendants can log in securely.
- Attendants can view only their assigned shift.
- Attendants can check in.
- Supervisors can assign pumps and nozzles.
- Attendants can view and confirm assignments.
- Attendants can verify opening readings.
- Shift opening is blocked until required conditions pass.
- Attendants can submit closing readings.
- Supervisors can approve, reject, or correct readings.
- Original attendant submissions are preserved.
- Final supervisor-approved readings are applied.
- Expected collections are calculated from approved readings and company prices.
- Attendants can submit collections.
- Supervisors can confirm received collections.
- Shortages and excesses are recorded.
- Reasons are required where policy requires them.
- Next shift opening depends on previous shift verification.
- Offline capture and sync work for critical actions.
- The app is accessible and field-friendly.
- All critical actions create audit events.
- Reports and daily close can consume the generated data.

## 26. Implementation Phases

**Phase 1 — Mobile Workflow Foundation:** mobile authentication; current shift screen; attendance check-in; assignment display; basic notifications.

**Phase 2 — Opening Shift Flow:** pump/nozzle assignment confirmation; opening reading verification; shift readiness validation; open shift action; supervisor visibility.

**Phase 3 — Closing Reading Flow:** closing reading input; litres sold calculation; submission to supervisor; supervisor review status display.

**Phase 4 — Collections Flow:** expected collection calculation; collection submission; difference calculation; reason capture; supervisor receipt status.

**Phase 5 — Handover Enforcement:** previous shift dependency; final approved readings as next opening readings; next shift unlock logic; shift completion state.

**Phase 6 — Offline and Accessibility Hardening:** offline queue; sync engine; conflict states; high contrast mode; large text mode; multilingual support; full accessibility review.

**Phase 7 — Reporting and Audit Integration:** attendance report integration; shift report integration; cash collection report integration; reading correction report integration; risk signal integration; audit event review.

## 27. Risks and Considerations

### 27.1 Device Sharing

Some attendants may share devices. The app must enforce logout, session expiration, and user identity clearly.

### 27.2 Internet Instability

Offline workflows must be designed carefully to avoid duplicate or conflicting records.

### 27.3 Supervisor Overwrite Behavior

Supervisor corrections must not erase original attendant submissions. Final values can be applied, but original data must remain auditable.

### 27.4 User Training

The workflow must be simple enough that attendants can learn it quickly.

### 27.5 Price Integrity

Prices must remain centrally controlled and protected from attendant edits.

### 27.6 Shift Handover Blocking

Blocking next shift opening improves control but may create operational pressure. Supervisor override policies must be clearly defined and audited.

## 28. Final Product Definition

The FuelGrid OS Mobile Attendant App is a simplified, accessible, mobile-first field operations application for pump attendants. It allows attendants to log in, confirm attendance, view assigned shifts, verify assigned pumps and nozzles, confirm opening readings, open shifts, submit closing readings, receive supervisor verification, view expected collections, submit collections, and complete shift handover.

The app must remain simple for attendants while creating complete backend records for attendance, assignments, readings, collections, corrections, approvals, shortages, excesses, audit logs, and reports.

The supervisor remains the operational control point for assignments, reading verification, corrections, collection confirmation, and shift handover approval.

The final product standard is:

> The mobile attendant experience must be simple, accessible, and fast on the surface, while FuelGrid OS remains strict, auditable, and financially accurate underneath.

---

**Delivery decision (owner, 2026-06-11):** the Mobile Attendant App ships as a mobile-first PWA _extension of the main app_ (apps/web), installed by attendants by scanning a **QR code shown on the main app's landing/login page**. Any main-app/backend changes the PRD requires are in-scope; the integration between the attendant experience and the main platform must be seamless and the workflow logic flawless. See [mobile-attendant-build-plan.md](mobile-attendant-build-plan.md) for the gap analysis and build sequencing.
