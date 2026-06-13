/**
 * English dictionary for the Mobile Attendant App (Phase 6b, PRD §15.2).
 *
 * THIS FILE IS THE SOURCE OF TRUTH for the message shape: `Messages` is
 * `typeof en`, and every other locale (sw.ts) is declared `const sw: Messages`
 * — so a key present here but missing in a translation is a TYPE ERROR, not a
 * runtime fallback.
 *
 * Conventions:
 *   - Plain strings for static copy; functions for interpolated/plural copy
 *     (each locale handles its own word order and plural rules).
 *   - Keys are grouped per screen (home/opening/closing/…) plus shared
 *     `common`, `sync` (offline engine surfaces), `settings`, and `install`
 *     (the login page's attendant affordance).
 *   - Server prose (`user_message`, raw SdkError messages) is NEVER
 *     translated here — the UI renders it verbatim as an honest fallback.
 *   - Numbers/money/litres stay locale-formatted by the existing formatting
 *     utilities; dictionary functions receive already-formatted strings.
 */

/** Shift slot names, used inside slot-labelled sentences. */
const slotNames: Record<string, string> = {
  morning: 'morning',
  afternoon: 'afternoon',
  evening: 'evening',
  night: 'night',
};

const slotName = (slot: string): string => slotNames[slot] ?? slot;

export const en = {
  common: {
    myShift: 'My shift',
    backToMyShift: 'Back to my shift',
    tryAgain: 'Try again',
    couldNotLoadShift: "Couldn't load your shift",
    pumpNozzle: (pump: number, nozzle: number) => `Pump ${pump} · Nozzle ${nozzle}`,
    reason: (text: string) => `Reason: ${text}`,
    supervisorReason: (text: string) => `Supervisor reason: ${text}`,
    goBackAndEdit: 'Go back and edit',
    confirmAndSubmit: 'Confirm and submit',
    savedOnPhone: 'Saved on this phone — will sync when you are back online.',
    savedOnPhoneBadge: 'Saved on this phone — will sync',
  },

  shell: {
    appName: 'Attendant',
  },

  home: {
    offDuty: 'Off duty',
    onDutyToday: 'You are on duty today',
    /** Rendered inside a capitalize-styled badge: "morning shift". */
    slotShiftBadge: (slot: string) => `${slotName(slot)} shift`,
    /** The shift header label: "Morning shift". */
    slotShiftHeader: (slot: string) => {
      const name = slotName(slot);
      return `${name.charAt(0).toUpperCase()}${name.slice(1)} shift`;
    },
    openedAt: (time: string) => `opened ${time}`,
    checkAgain: 'Check again',
    /** Rendered inside a capitalize-styled badge: "Shift open". */
    shiftStatusBadge: (status: string) => `Shift ${status}`,
    attendanceNotCheckedIn: 'You are not checked in',
    attendanceCheckedIn: 'You are checked in',
    attendanceCheckedOut: 'You are checked out',
    attendanceSince: (time: string) => ` (since ${time})`,
    checkedInBanner: 'You are checked in.',
    stages: {
      check_in: 'Check in',
      confirm_assignment: 'Confirm nozzles',
      verify_opening_readings: 'Verify opening readings',
      working: 'Work the shift',
      submit_closing_readings: 'Submit closing readings',
      await_reading_verification: 'Supervisor verifies readings',
      submit_collections: 'Submit collections',
      await_collection_receipt: 'Supervisor confirms cash',
      complete: 'Shift complete',
    },
    shiftProgress: 'Shift progress',
    srStageDone: ' — done',
    srStageCurrent: ' — current step',
    nozzlesVerifiedCount: (n: number, total: number) => `${n} of ${total} nozzles verified`,
    verifiedCount: (n: number, total: number) => `${n} of ${total} verified`,
    myNozzles: 'My nozzles',
    noNozzlesYet:
      'No nozzles assigned to you yet. Your supervisor assigns them after you check in.',
    confirmedBadge: 'Confirmed',
    confirmedWaitingSync: 'Confirmed — waiting to sync',
    awaitingConfirmation: 'Awaiting your confirmation',
    openReading: (value: string) => `Open ${value}`,
    closeReading: (value: string) => `Close ${value}`,
    /** Rendered inside a capitalize-styled badge: "Reading approved". */
    readingStatusBadge: (status: string) => `Reading ${status}`,
    collections: 'Collections',
    expected: 'Expected',
    submitted: 'Submitted',
    received: 'Received',
    receiptStatusBadge: (status: string) => status.replaceAll('_', ' '),
    ctaCheckIn: 'Check in',
    ctaCheckedInQueued: 'Checked in — waiting to sync',
    ctaConfirmNozzles: 'Confirm my nozzles',
    ctaConfirmedQueued: 'Confirmed — waiting to sync',
    ctaVerifyOpenings: 'Verify opening readings',
    ctaEnterClosings: 'Enter closing readings',
    ctaFinishClosings: 'Finish closing readings',
    ctaViewReviewStatus: 'View review status',
    ctaSubmitCollections: 'Submit collections',
    ctaViewCollectionStatus: 'View collection status',
    ctaFinishShift: 'Finish your shift',
    errCheckIn: 'Could not check in. Try again.',
    errConfirm: 'Could not confirm the assignment. Try again.',
    // Home header bell + report-issue entry point.
    notificationsLink: 'Notifications',
    /** Accessible label for the bell when there are unread items. */
    bellUnread: (n: number) => `Notifications, ${n} unread`,
    bellNoUnread: 'Notifications',
    reportIssue: 'Report an issue',
  },

  notifications: {
    title: 'Notifications',
    subtitle: 'Updates from your supervisor about this shift.',
    markAllRead: 'Mark all as read',
    markRead: 'Mark as read',
    markedRead: 'Read',
    unread: 'Unread',
    emptyTitle: 'No notifications yet',
    emptyBody: 'Updates about your assignment, readings, cash and shift show up here.',
    errLoadTitle: "Couldn't load your notifications",
    loadMore: 'Show more',
    // Severity badge labels (text always carries the meaning, colour reinforces).
    severityInfo: 'Info',
    severitySuccess: 'Done',
    severityWarning: 'Attention',
    severityCritical: 'Urgent',
  },

  report: {
    title: 'Report an issue',
    subtitle: 'Tell your supervisor about a problem at your pump. They are notified right away.',
    typeLabel: 'What is the problem?',
    // Issue-type picker labels, mapped to the API enum values.
    types: {
      pump: 'Pump',
      nozzle: 'Nozzle',
      meter: 'Meter',
      payment: 'Payment',
      safety: 'Safety',
      other: 'Something else',
    },
    typeHints: {
      pump: 'The pump is not working properly',
      nozzle: 'A nozzle is faulty or leaking',
      meter: 'The meter reading looks wrong',
      payment: 'A problem taking payment',
      safety: 'A safety or spill concern',
      other: 'Any other problem',
    },
    urgencyLabel: 'How urgent is it?',
    urgencyLow: 'Low',
    urgencyMedium: 'Normal',
    urgencyHigh: 'Urgent',
    descriptionLabel: 'Describe the problem',
    descriptionPlaceholder: 'What happened? Add anything your supervisor should know.',
    descriptionMissing: 'Add a short description before you send.',
    typeMissing: 'Choose what the problem is first.',
    submitButton: 'Send to supervisor',
    confirmTitle: 'Send this to your supervisor?',
    confirmType: 'Problem',
    confirmUrgency: 'Urgency',
    confirmSend: 'Send now',
    onceNote: 'Your supervisor is notified as soon as this is sent.',
    sentTitle: 'Sent to your supervisor',
    sentBody: 'Your supervisor has been notified and will follow up.',
    queuedTitle: 'Saved on this phone',
    queuedBody: 'It will be sent to your supervisor when you are back online.',
    backHome: 'Back to my shift',
    reportAnother: 'Report another issue',
    errNoShiftTitle: 'You are not on a shift',
    errNoShiftBody:
      'Issue reports are linked to your current shift. Check in to a shift first, or call your supervisor.',
    errGeneric: 'Could not send the report. Check your connection and try again.',
    toastSentTitle: 'Issue reported',
    toastSentBody: 'Your supervisor has been notified.',
    toastQueuedTitle: 'Issue saved on this phone',
    toastQueuedBody: 'It will be sent when you are back online.',
  },

  opening: {
    title: 'Opening readings',
    progress: (verified: number, total: number) =>
      `${verified} of ${total} nozzles verified. Compare each meter with the expected figure and save.`,
    expectedOpening: 'Expected opening',
    noPreviousReading: 'No previous reading',
    recordedBadge: 'Recorded',
    meterLabel: (places: number) =>
      `Meter reading (${places} decimal${places === 1 ? '' : 's'} max)`,
    statusMatched: 'Matched — same as the expected opening.',
    statusHigher: (difference: string) =>
      `Higher than expected by ${difference}. You can save it, but tell your supervisor if this looks wrong.`,
    statusLowerPrefix: 'Lower than expected. ',
    lowerBlocked:
      "Reading is lower than the previous shift's approved closing. Call your supervisor.",
    statusNoExpected: 'No previous reading for this nozzle — enter the meter as you see it.',
    statusScale: (places: number) =>
      `Too many decimals — this meter records at most ${places} decimal${places === 1 ? '' : 's'}.`,
    statusInvalid: 'Enter numbers only, like 1500 or 1500.25.',
    statusEmpty: 'Enter the reading shown on the meter.',
    notSaved: (message: string) => `Not saved: ${message}`,
    issueReported: 'Issue reported. Your supervisor has been notified.',
    issueQueued:
      'Issue saved on this phone — it will reach your supervisor when you are back online.',
    reportIssue: 'Report issue to supervisor',
    cannotReport:
      'You cannot file the report from here yet — call your supervisor to resolve this before the shift can continue.',
    confirmTitle: 'Confirm your readings',
    lockNote: 'Saved readings are locked once your shift opens — corrections need your supervisor.',
    confirmAndSave: 'Confirm and save',
    saveButton: 'Save opening readings',
    allRecordedTitle: 'All opening readings are recorded',
    allRecordedBody: (total: number) =>
      `${total} of ${total} nozzles verified. You are set for this shift.`,
    queuedNote: (n: number) =>
      `${n} reading${n === 1 ? ' is' : 's are'} saved on this phone and will sync when you are back online.`,
    emptyTitle: 'Nothing to verify right now',
    emptyNoShift:
      'You are not on a shift. Opening readings are captured at the start of your shift.',
    emptyNotOpen: 'Your shift is no longer open, so opening readings can no longer be captured.',
    emptyNoAssignments:
      'No nozzles are assigned to you yet. Your supervisor assigns them after you check in.',
    errExpectedTitle: "Couldn't load the expected readings",
    partialSummary: (saved: number, total: number) =>
      `Saved ${saved} of ${total} readings. Fix the nozzles marked below and try again.`,
    toastQueuedTitle: 'Opening readings saved on this phone',
    toastQueuedBody: 'They will sync when you are back online.',
    toastSavedTitle: 'Opening readings saved',
    toastSavedBody: 'All your nozzles are verified.',
    errAlreadyRecorded: 'An opening reading was already recorded for this nozzle.',
    errGeneric: 'Could not save this reading. Check your connection and try again.',
    errReportTitle: 'Could not report the issue',
    errReportBody: 'Try again or call your supervisor.',
  },

  closing: {
    title: 'Closing readings',
    progress: (submitted: number, total: number) =>
      `${submitted} of ${total} nozzles submitted. Enter the closing meter on each nozzle — litres sold are calculated for you.`,
    openingReading: 'Opening reading',
    notRecorded: 'Not recorded',
    closingReading: 'Closing reading',
    litresSold: 'Litres sold',
    litresValue: (litres: string) => `${litres} L`,
    lowerBlocked: 'Closing reading cannot be lower than opening reading.',
    noOpening:
      'No opening reading was recorded for this nozzle, so its closing cannot be validated. Verify the opening reading first.',
    meterLabel: (places: number) =>
      `Closing meter reading (${places} decimal${places === 1 ? '' : 's'} max)`,
    statusOk: (litres: string) => `Litres sold: ${litres} L`,
    statusHigh: (litres: string) =>
      `Litres sold: ${litres} L — this looks unusually high. Double-check the meter; you can still submit it.`,
    statusScale: (places: number) =>
      `Too many decimals — this meter records at most ${places} decimal${places === 1 ? '' : 's'}.`,
    statusInvalid: 'Enter numbers only, like 1500 or 1500.25.',
    statusEmpty: 'Enter the closing reading shown on the meter.',
    notSaved: (message: string) => `Not saved: ${message}`,
    badgeApproved: 'Approved by supervisor',
    badgeCorrected: 'Corrected by supervisor',
    badgeRejected: 'Rejected by supervisor',
    badgePending: 'Submitted — pending supervisor review',
    confirmTitle: 'Confirm your closing readings',
    /** "You are submitting {n} readings totalling {litres span} litres — confirm." */
    confirmSummaryPrefix: (n: number) =>
      `You are submitting ${n} reading${n === 1 ? '' : 's'} totalling `,
    confirmSummarySuffix: ' litres — confirm.',
    litresSoldShort: (litres: string) => `${litres} L sold`,
    lockNote:
      'Submitted readings are locked — only your supervisor can correct them during review.',
    submitButton: 'Submit closing readings',
    allSubmittedTitle: 'All closing readings are submitted',
    allSubmittedBody: (total: number, shiftClosed: boolean) =>
      `${total} of ${total} nozzles submitted${shiftClosed ? ' and the shift is closed' : ''}. Your supervisor reviews them next.`,
    queuedNote: (n: number) =>
      `${n} reading${n === 1 ? ' is' : 's are'} saved on this phone and will sync when you are back online.`,
    shiftNotOpenTitle: 'Your shift is no longer open',
    shiftNotOpenBody:
      'Closing readings can no longer be captured. Talk to your supervisor about the missing nozzles.',
    emptyTitle: 'Nothing to close right now',
    emptyNoShift: 'You are not on a shift. Closing readings are captured at the end of your shift.',
    emptyNoAssignments: 'No nozzles are assigned to you yet, so there is nothing to close.',
    partialSummary: (saved: number, total: number) =>
      `Saved ${saved} of ${total} readings. Fix the nozzles marked below and try again.`,
    toastQueuedTitle: 'Closing readings saved on this phone',
    toastQueuedBody: 'They will sync when you are back online.',
    toastSubmittedTitle: 'Closing readings submitted',
    toastSubmittedBody: 'Your supervisor will now review and verify them.',
    errAlreadySubmitted:
      'A closing reading was already submitted for this nozzle — it is pending supervisor review.',
    errAlreadyRecorded: 'A closing reading was already recorded for this nozzle.',
    errGeneric: 'Could not save this reading. Check your connection and try again.',
    viewReviewStatus: 'View review status',
  },

  collections: {
    title: 'Collections',
    subtitle:
      'Hand in everything you collected this shift. Amounts are checked against the meters.',
    errLoadTitle: "Couldn't load your collections",
    emptyTitle: 'No collections right now',
    emptyNoShift: 'You are not on a shift. Collections are submitted after your shift closes.',
    preCloseBody:
      'Your expected collection is available after the shift closes. Finish your closing readings and wait for your supervisor to close the shift.',
    awaitVerification:
      'Your supervisor is still verifying your closing readings. Submit your collections once the expected amount is final.',
    expectedCollection: 'Expected collection',
    totalExpected: 'Total expected',
    litresTimesPrice: (litres: string, price: string) => `${litres} L × ${price}`,
    tenderCash: 'Cash',
    tenderMobileMoney: 'Mobile money',
    tenderCard: 'Card',
    tenderCredit: 'Credit',
    formTitle: 'Submit your collections',
    tenderInvalid: 'Enter a money amount like 250000 or 250000.50 (no minus sign).',
    submittedTotal: 'Submitted total',
    expected: 'Expected',
    balanced: 'Balanced — your total matches the expected collection.',
    shortage: (amount: string) => `Shortage of ${amount} — you are handing in less than expected.`,
    excess: (amount: string) => `Excess of ${amount} — you are handing in more than expected.`,
    reasonLabel: 'Reason for the difference (required)',
    reasonPlaceholderZero: 'Explain why you are submitting nothing',
    reasonPlaceholderDiff: 'Explain why your total does not match the expected amount',
    reasonMissing: 'Add a short reason before you can submit.',
    submitButton: 'Submit collections',
    confirmTitle: 'Confirm your collections',
    /** "You are submitting {total} against an expected {expected} — difference {diff} — confirm." */
    confirmPart1: 'You are submitting ',
    confirmPart2: ' against an expected ',
    confirmPart3: ' — difference ',
    confirmPart4: ' — confirm.',
    onceNote:
      'You can submit collections only once for this shift. After this, only your supervisor handles changes.',
    yourSubmission: 'Your submission',
    receiptWaiting: 'Submitted — waiting for your supervisor to confirm receipt.',
    supervisorReceipt: 'Supervisor receipt',
    receivedRow: 'Received',
    differenceRow: 'Difference',
    rejectedAlert: 'Your collection was rejected. See your supervisor.',
    comment: (text: string) => `Comment: ${text}`,
    badgeReceived: 'Received',
    badgeApprovedWithDifference: 'Approved with difference',
    badgeRejected: 'Rejected',
    errVarianceReason:
      'Your total does not match the expected amount — add a reason explaining the difference.',
    errAlreadySubmitted: 'Collections were already submitted for this shift.',
    errGeneric: 'Could not submit your collections. Check your connection and try again.',
    toastQueuedTitle: 'Collections saved on this phone',
    toastQueuedBody: 'They will sync when you are back online.',
    toastSubmittedTitle: 'Collections submitted',
    toastSubmittedBody: 'Your supervisor will now confirm the cash they receive from you.',
  },

  review: {
    title: 'Reading review status',
    progress: (verified: number, total: number) =>
      `${verified} of ${total} readings verified by your supervisor.`,
    emptyTitle: 'No readings to review',
    emptyNoShift: 'You are not on a shift, so there are no submitted readings to track.',
    emptyNoAssignments: 'No nozzles are assigned to you on this shift.',
    badgeNotSubmitted: 'Not submitted yet',
    badgeApproved: 'Approved',
    badgeCorrected: 'Corrected by supervisor',
    badgeRejected: 'Rejected',
    badgeFlagged: 'Flagged for investigation',
    badgePending: 'Pending supervisor review',
    submitPrompt: "Submit this nozzle's closing reading to start the review.",
    youSubmitted: 'You submitted',
    supervisorApproved: 'Supervisor approved',
    difference: 'Difference',
    approvedReading: 'Approved reading',
    reasonLabel: 'Reason:',
    notReviewedYet: 'Your supervisor has not reviewed this reading yet.',
    finishClosings: 'Finish closing readings',
    // A rejected reading is sent back to the attendant to re-capture.
    rejectedTitle: 'Your supervisor rejected this reading',
    rejectedHelp: 'Take a fresh closing reading for this nozzle and submit it again.',
    resubmitCta: 'Resubmit your closing reading',
    // A flagged reading is under supervisor investigation — no attendant action.
    flaggedHelp: 'Your supervisor is investigating this reading. No action is needed from you yet.',
  },

  complete: {
    emptyTitle: 'No shift to complete',
    emptyBody: 'You are not on a shift right now.',
    notCompleteTitle: 'Your shift is not complete yet',
    doneTitle: 'Shift complete — well done!',
    readings: 'Readings',
    verifiedBadge: 'Verified',
    readingsVerified: (n: number, total: number) =>
      `${n} of ${total} closing readings verified by your supervisor.`,
    viewReadingDetails: 'View reading details',
    collections: 'Collections',
    badgeApprovedWithDifference: 'Approved with difference',
    badgeReceived: 'Received',
    badgeSubmitted: 'Submitted',
    expected: 'Expected',
    youSubmitted: 'You submitted',
    supervisorReceived: 'Supervisor received',
    difference: 'Difference',
    viewCollectionDetails: 'View collection details',
    checkOut: 'Check out',
    checkedOutQueued: 'Checked out — saved on this phone, will sync when you are back online.',
    youAreCheckedOut: 'You are checked out',
    errCheckOut: 'Could not check out. Try again.',
    toastQueuedTitle: 'Check-out saved on this phone',
    toastQueuedBody: 'It will sync when you are back online.',
    toastDoneTitle: 'Checked out',
    toastDoneBody: 'Thanks for your shift — see you next time.',
  },

  sync: {
    chipOffline: 'Offline',
    chipOfflineWaiting: (n: number) => `Offline — ${n} to sync`,
    chipSyncing: 'Syncing…',
    chipAuth: 'Sign in to finish syncing',
    chipConflict: 'Needs attention',
    chipFailed: 'Sync failed',
    chipWaiting: (n: number) => `${n} waiting to sync`,
    chipSynced: 'All changes synced',
    chipOnline: 'Online',
    offlineHint:
      'You are offline — showing the last synced info. Anything you submit is saved on this phone and will sync when you are back online.',
    sheetTitle: 'Sync details',
    sheetClose: 'Close sync details',
    authNote:
      'Your session expired before everything synced. Sign in again to finish syncing — nothing has been lost.',
    emptyQueue: 'Nothing waiting to sync. Everything you submitted reached the server.',
    savedAt: (when: string) => `Saved ${when}`,
    statusPending: 'Waiting to sync',
    statusSyncing: 'Syncing…',
    statusSynced: 'Synced',
    statusFailed: 'Failed',
    statusConflict: 'Needs supervisor attention',
    tryAgain: 'Try again',
    discard: 'Discard',
    discardConfirm: 'Tap again to discard',
    syncNow: 'Sync now',
    updateReady: 'App updated — tap to reload',
    // Queued-action labels, rendered at display time from action_type +
    // payload (the queue stores codes/payloads, never display prose).
    actionCheckIn: 'Check in',
    actionCheckOut: 'Check out',
    actionConfirmAssignment: (pump: number, nozzle: number) =>
      `Confirm pump ${pump} · nozzle ${nozzle}`,
    actionConfirmAssignmentGeneric: 'Confirm nozzle assignment',
    actionOpeningReading: (reading: string, pump: number, nozzle: number) =>
      `Opening reading ${reading} — pump ${pump} · nozzle ${nozzle}`,
    actionOpeningReadingGeneric: (reading: string) => `Opening reading ${reading}`,
    actionClosingReading: (reading: string, pump: number, nozzle: number) =>
      `Closing reading ${reading} — pump ${pump} · nozzle ${nozzle}`,
    actionClosingReadingGeneric: (reading: string) => `Closing reading ${reading}`,
    actionCollection: 'Submit collections',
    /** "Report a {pump} issue" — the issue type comes from report.types. */
    actionReportIssue: (issueType: string) => `Report a ${issueType.toLowerCase()} issue`,
    // Client-generated queue error messages, stored as CODES in the queue
    // records and rendered here (raw server prose has no code and is shown
    // verbatim as the fallback).
    errOpeningBelowExpected:
      "Reading is lower than the previous shift's approved closing. Call your supervisor.",
    errAssignmentChanged:
      'Your nozzle assignment changed while you were offline. Check your assignment and confirm it again.',
    errReadingConflict: (readingType: 'opening' | 'closing', serverValue?: string) =>
      serverValue != null
        ? `The server already has a different ${readingType} reading (${serverValue}) for this nozzle. Your figure is kept here — show it to your supervisor.`
        : `The server reported this ${readingType} reading as already submitted but it is not visible on your shift. Your figure is kept here — show it to your supervisor.`,
    errCollectionConflict: (serverTotal: string) =>
      `Collections were already submitted for this shift with a different total (${serverTotal}). Your amounts are kept here — show them to your supervisor.`,
    errVerifyUnavailable: 'Could not verify with the server — it will be retried.',
    errNoActiveShift:
      'You are no longer on a shift, so this issue could not be sent. Check in to a shift first, or call your supervisor.',
    errIssueInvalid: 'This issue report could not be sent. Edit it and try again, or discard it.',
  },

  settings: {
    title: 'Display & language',
    close: 'Close settings',
    language: 'Language',
    english: 'English',
    swahili: 'Kiswahili',
    textSize: 'Text size',
    textNormal: 'Normal',
    textLarge: 'Large',
    contrast: 'Contrast',
    contrastNormal: 'Normal',
    contrastHigh: 'High',
    done: 'Done',
  },

  install: {
    prompt: 'Pump attendant? Install the app',
    loadingCode: 'Loading code…',
    qrTitle: 'Open the attendant app',
    /** "{1}Add to Home Screen{2}" — split around the emphasised phrase. */
    scanInstruction1: "Scan with your phone camera, sign in, then use your browser's ",
    addToHomeScreen: 'Add to Home Screen',
    scanInstruction2: ' to install.',
    languageLabel: 'Language',
  },
};

/**
 * The message shape every locale must satisfy. `sw.ts` declares
 * `const sw: Messages`, so a missing or extra key fails `pnpm typecheck`.
 */
export type Messages = typeof en;
