import type { Messages } from './en';

/**
 * Swahili (Kiswahili, Tanzanian usage) dictionary for the Mobile Attendant
 * App (Phase 6b, PRD §15.2).
 *
 * Typed as `Messages` (the shape of en.ts) so a key present in English but
 * missing here is a TYPE ERROR. Glossary kept consistent throughout:
 *
 *   shift = zamu            ·  supervisor = msimamizi
 *   pump = pampu            ·  nozzle = nozeli
 *   meter = mita            ·  (meter) reading = usomaji
 *   opening = (ya) kufungua ·  closing = (ya) kufunga
 *   collections = makusanyo ·  shortage = upungufu  ·  excess = ziada
 *   check in = ingia kazini ·  check out = toka kazini
 *   submit = wasilisha      ·  confirm = thibitisha  ·  save = hifadhi
 *   sync = tuma/kutumwa (plain "send to the server" framing — clearer for
 *   attendants than a coined technical term)
 */

/** Shift slot names. */
const slotNames: Record<string, string> = {
  morning: 'asubuhi',
  afternoon: 'mchana',
  evening: 'jioni',
  night: 'usiku',
};

const slotName = (slot: string): string => slotNames[slot] ?? slot;

/** Shift lifecycle status, as used in "Zamu …" badges. */
const shiftStatusNames: Record<string, string> = {
  open: 'imefunguliwa',
  closed: 'imefungwa',
  approved: 'imeidhinishwa',
  locked: 'imefungiwa',
};

/** Reading verification status, as used in "Usomaji …" badges. */
const readingStatusNames: Record<string, string> = {
  approved: 'umeidhinishwa',
  pending: 'unasubiri uhakiki',
  corrected: 'umesahihishwa',
  rejected: 'umekataliwa',
};

/** Collection receipt status names. */
const receiptStatusNames: Record<string, string> = {
  received: 'imepokelewa',
  approved_with_difference: 'imeidhinishwa na tofauti',
  rejected: 'imekataliwa',
};

export const sw: Messages = {
  common: {
    myShift: 'Zamu yangu',
    backToMyShift: 'Rudi kwenye zamu yangu',
    tryAgain: 'Jaribu tena',
    couldNotLoadShift: 'Imeshindikana kupakia zamu yako',
    pumpNozzle: (pump, nozzle) => `Pampu ${pump} · Nozeli ${nozzle}`,
    reason: (text) => `Sababu: ${text}`,
    supervisorReason: (text) => `Sababu ya msimamizi: ${text}`,
    goBackAndEdit: 'Rudi ukarekebishe',
    confirmAndSubmit: 'Thibitisha na uwasilishe',
    savedOnPhone: 'Imehifadhiwa kwenye simu hii — itatumwa mtandao ukirudi.',
    savedOnPhoneBadge: 'Imehifadhiwa kwenye simu — itatumwa',
  },

  shell: {
    appName: 'Mhudumu',
  },

  home: {
    offDuty: 'Huna zamu',
    onDutyToday: 'Una zamu leo',
    slotShiftBadge: (slot) => `zamu ya ${slotName(slot)}`,
    slotShiftHeader: (slot) => `Zamu ya ${slotName(slot)}`,
    openedAt: (time) => `ilifunguliwa ${time}`,
    checkAgain: 'Angalia tena',
    shiftStatusBadge: (status) => `Zamu ${shiftStatusNames[status] ?? status}`,
    attendanceNotCheckedIn: 'Hujaingia kazini',
    attendanceCheckedIn: 'Umeingia kazini',
    attendanceCheckedOut: 'Umetoka kazini',
    attendanceSince: (time) => ` (tangu ${time})`,
    checkedInBanner: 'Umeingia kazini.',
    stages: {
      check_in: 'Ingia kazini',
      confirm_assignment: 'Thibitisha nozeli',
      verify_opening_readings: 'Hakiki usomaji wa kufungua',
      working: 'Fanya kazi ya zamu',
      submit_closing_readings: 'Wasilisha usomaji wa kufunga',
      await_reading_verification: 'Msimamizi anahakiki usomaji',
      submit_collections: 'Wasilisha makusanyo',
      await_collection_receipt: 'Msimamizi anathibitisha pesa',
      complete: 'Zamu imekamilika',
    },
    shiftProgress: 'Maendeleo ya zamu',
    srStageDone: ' — imekamilika',
    srStageCurrent: ' — hatua ya sasa',
    nozzlesVerifiedCount: (n, total) => `Nozeli ${n} kati ya ${total} zimehakikiwa`,
    verifiedCount: (n, total) => `${n} kati ya ${total} zimehakikiwa`,
    myNozzles: 'Nozeli zangu',
    noNozzlesYet: 'Bado hujapangiwa nozeli. Msimamizi wako atakupangia baada ya kuingia kazini.',
    confirmedBadge: 'Imethibitishwa',
    confirmedWaitingSync: 'Imethibitishwa — inasubiri kutumwa',
    awaitingConfirmation: 'Inasubiri uthibitisho wako',
    openReading: (value) => `Kufungua ${value}`,
    closeReading: (value) => `Kufunga ${value}`,
    readingStatusBadge: (status) => `Usomaji ${readingStatusNames[status] ?? status}`,
    collections: 'Makusanyo',
    expected: 'Kinachotarajiwa',
    submitted: 'Kilichowasilishwa',
    received: 'Kilichopokelewa',
    receiptStatusBadge: (status) => receiptStatusNames[status] ?? status.replaceAll('_', ' '),
    ctaCheckIn: 'Ingia kazini',
    ctaCheckedInQueued: 'Umeingia — inasubiri kutumwa',
    ctaConfirmNozzles: 'Thibitisha nozeli zangu',
    ctaConfirmedQueued: 'Imethibitishwa — inasubiri kutumwa',
    ctaVerifyOpenings: 'Hakiki usomaji wa kufungua',
    ctaEnterClosings: 'Weka usomaji wa kufunga',
    ctaFinishClosings: 'Maliza usomaji wa kufunga',
    ctaViewReviewStatus: 'Angalia hali ya uhakiki',
    ctaSubmitCollections: 'Wasilisha makusanyo',
    ctaViewCollectionStatus: 'Angalia hali ya makusanyo',
    ctaFinishShift: 'Maliza zamu yako',
    errCheckIn: 'Imeshindikana kuingia kazini. Jaribu tena.',
    errConfirm: 'Imeshindikana kuthibitisha nozeli. Jaribu tena.',
    notificationsLink: 'Arifa',
    bellUnread: (n) => `Arifa, ${n} hazijasomwa`,
    bellNoUnread: 'Arifa',
    reportIssue: 'Ripoti tatizo',
  },

  notifications: {
    title: 'Arifa',
    subtitle: 'Taarifa kutoka kwa msimamizi wako kuhusu zamu hii.',
    markAllRead: 'Weka zote kuwa zimesomwa',
    markRead: 'Weka kuwa imesomwa',
    markedRead: 'Imesomwa',
    unread: 'Haijasomwa',
    emptyTitle: 'Bado hakuna arifa',
    emptyBody: 'Taarifa kuhusu mpangilio wako, usomaji, pesa na zamu zitaonekana hapa.',
    errLoadTitle: 'Imeshindikana kupakia arifa zako',
    loadMore: 'Onyesha zaidi',
    severityInfo: 'Taarifa',
    severitySuccess: 'Imekamilika',
    severityWarning: 'Angalia',
    severityCritical: 'Haraka',
  },

  report: {
    title: 'Ripoti tatizo',
    subtitle: 'Mwambie msimamizi wako kuhusu tatizo kwenye pampu yako. Anajulishwa mara moja.',
    typeLabel: 'Tatizo ni nini?',
    types: {
      pump: 'Pampu',
      nozzle: 'Nozeli',
      meter: 'Mita',
      payment: 'Malipo',
      safety: 'Usalama',
      other: 'Jambo lingine',
    },
    typeHints: {
      pump: 'Pampu haifanyi kazi vizuri',
      nozzle: 'Nozeli ina hitilafu au inavuja',
      meter: 'Usomaji wa mita unaonekana si sahihi',
      payment: 'Tatizo la kupokea malipo',
      safety: 'Wasiwasi wa usalama au kumwagika',
      other: 'Tatizo lingine lolote',
    },
    urgencyLabel: 'Ni la dharura kiasi gani?',
    urgencyLow: 'Chini',
    urgencyMedium: 'Kawaida',
    urgencyHigh: 'Dharura',
    descriptionLabel: 'Eleza tatizo',
    descriptionPlaceholder: 'Nini kilitokea? Ongeza chochote msimamizi wako anapaswa kujua.',
    descriptionMissing: 'Andika maelezo mafupi kabla ya kutuma.',
    typeMissing: 'Chagua tatizo ni nini kwanza.',
    submitButton: 'Tuma kwa msimamizi',
    confirmTitle: 'Tuma hii kwa msimamizi wako?',
    confirmType: 'Tatizo',
    confirmUrgency: 'Udharura',
    confirmSend: 'Tuma sasa',
    onceNote: 'Msimamizi wako anajulishwa mara tu hii inapotumwa.',
    sentTitle: 'Imetumwa kwa msimamizi wako',
    sentBody: 'Msimamizi wako amejulishwa na atafuatilia.',
    queuedTitle: 'Imehifadhiwa kwenye simu hii',
    queuedBody: 'Itatumwa kwa msimamizi wako mtandao ukirudi.',
    backHome: 'Rudi kwenye zamu yangu',
    reportAnother: 'Ripoti tatizo lingine',
    errNoShiftTitle: 'Huko kwenye zamu',
    errNoShiftBody:
      'Ripoti za matatizo zinaunganishwa na zamu yako ya sasa. Ingia kwenye zamu kwanza, au mpigie msimamizi wako.',
    errGeneric: 'Imeshindikana kutuma ripoti. Angalia mtandao wako kisha ujaribu tena.',
    toastSentTitle: 'Tatizo limeripotiwa',
    toastSentBody: 'Msimamizi wako amejulishwa.',
    toastQueuedTitle: 'Tatizo limehifadhiwa kwenye simu hii',
    toastQueuedBody: 'Litatumwa mtandao ukirudi.',
  },

  opening: {
    title: 'Usomaji wa kufungua',
    progress: (verified, total) =>
      `Nozeli ${verified} kati ya ${total} zimehakikiwa. Linganisha kila mita na kiwango kinachotarajiwa, kisha hifadhi.`,
    expectedOpening: 'Usomaji unaotarajiwa',
    noPreviousReading: 'Hakuna usomaji uliopita',
    recordedBadge: 'Umerekodiwa',
    meterLabel: (places) => `Usomaji wa mita (si zaidi ya desimali ${places})`,
    statusMatched: 'Umelingana — sawa na usomaji unaotarajiwa.',
    statusHigher: (difference) =>
      `Uko juu ya unaotarajiwa kwa ${difference}. Unaweza kuuhifadhi, lakini mwambie msimamizi wako kama unaona si sahihi.`,
    statusLowerPrefix: 'Uko chini ya unaotarajiwa. ',
    lowerBlocked:
      'Usomaji uko chini ya kufunga kulikoidhinishwa kwa zamu iliyopita. Mpigie msimamizi wako.',
    statusNoExpected:
      'Hakuna usomaji uliopita kwa nozeli hii — weka usomaji kama unavyouona kwenye mita.',
    statusScale: (places) =>
      `Desimali ni nyingi mno — mita hii inarekodi si zaidi ya desimali ${places}.`,
    statusInvalid: 'Weka namba tu, kama 1500 au 1500.25.',
    statusEmpty: 'Weka usomaji unaoonekana kwenye mita.',
    notSaved: (message) => `Haukuhifadhiwa: ${message}`,
    issueReported: 'Tatizo limeripotiwa. Msimamizi wako amejulishwa.',
    issueQueued:
      'Tatizo limehifadhiwa kwenye simu hii — litamfikia msimamizi wako mtandao ukirudi.',
    reportIssue: 'Ripoti tatizo kwa msimamizi',
    cannotReport:
      'Huwezi kuripoti kutoka hapa kwa sasa — mpigie msimamizi wako ili kutatua hili kabla zamu haijaendelea.',
    confirmTitle: 'Thibitisha usomaji wako',
    lockNote:
      'Usomaji uliohifadhiwa hufungwa zamu ikifunguliwa — masahihisho yanahitaji msimamizi wako.',
    confirmAndSave: 'Thibitisha na uhifadhi',
    saveButton: 'Hifadhi usomaji wa kufungua',
    allRecordedTitle: 'Usomaji wote wa kufungua umerekodiwa',
    allRecordedBody: (total) =>
      `Nozeli ${total} kati ya ${total} zimehakikiwa. Uko tayari kwa zamu hii.`,
    queuedNote: (n) =>
      n === 1
        ? `Usomaji 1 umehifadhiwa kwenye simu hii na utatumwa mtandao ukirudi.`
        : `Usomaji ${n} umehifadhiwa kwenye simu hii na utatumwa mtandao ukirudi.`,
    emptyTitle: 'Hakuna cha kuhakiki kwa sasa',
    emptyNoShift: 'Huko kwenye zamu. Usomaji wa kufungua huchukuliwa mwanzoni mwa zamu yako.',
    emptyNotOpen: 'Zamu yako imeshafungwa, kwa hivyo usomaji wa kufungua hauwezi kuchukuliwa tena.',
    emptyNoAssignments:
      'Bado hujapangiwa nozeli. Msimamizi wako atakupangia baada ya kuingia kazini.',
    errExpectedTitle: 'Imeshindikana kupakia usomaji unaotarajiwa',
    partialSummary: (saved, total) =>
      `Imehifadhi usomaji ${saved} kati ya ${total}. Rekebisha nozeli zilizowekwa alama hapa chini kisha ujaribu tena.`,
    toastQueuedTitle: 'Usomaji wa kufungua umehifadhiwa kwenye simu hii',
    toastQueuedBody: 'Utatumwa mtandao ukirudi.',
    toastSavedTitle: 'Usomaji wa kufungua umehifadhiwa',
    toastSavedBody: 'Nozeli zako zote zimehakikiwa.',
    errAlreadyRecorded: 'Usomaji wa kufungua ulisharekodiwa kwa nozeli hii.',
    errGeneric: 'Imeshindikana kuhifadhi usomaji huu. Angalia mtandao wako kisha ujaribu tena.',
    errReportTitle: 'Imeshindikana kuripoti tatizo',
    errReportBody: 'Jaribu tena au mpigie msimamizi wako.',
  },

  closing: {
    title: 'Usomaji wa kufunga',
    progress: (submitted, total) =>
      `Nozeli ${submitted} kati ya ${total} zimewasilishwa. Weka usomaji wa kufunga kwa kila nozeli — lita zilizouzwa zinakokotolewa kwa ajili yako.`,
    openingReading: 'Usomaji wa kufungua',
    notRecorded: 'Haujarekodiwa',
    closingReading: 'Usomaji wa kufunga',
    litresSold: 'Lita zilizouzwa',
    litresValue: (litres) => `Lita ${litres}`,
    lowerBlocked: 'Usomaji wa kufunga hauwezi kuwa chini ya usomaji wa kufungua.',
    noOpening:
      'Hakuna usomaji wa kufungua uliorekodiwa kwa nozeli hii, kwa hivyo kufunga kwake hakuwezi kuhakikiwa. Hakiki usomaji wa kufungua kwanza.',
    meterLabel: (places) => `Usomaji wa mita wa kufunga (si zaidi ya desimali ${places})`,
    statusOk: (litres) => `Lita zilizouzwa: ${litres}`,
    statusHigh: (litres) =>
      `Lita zilizouzwa: ${litres} — kiasi hiki kinaonekana kikubwa isivyo kawaida. Hakikisha mita; bado unaweza kuwasilisha.`,
    statusScale: (places) =>
      `Desimali ni nyingi mno — mita hii inarekodi si zaidi ya desimali ${places}.`,
    statusInvalid: 'Weka namba tu, kama 1500 au 1500.25.',
    statusEmpty: 'Weka usomaji wa kufunga unaoonekana kwenye mita.',
    notSaved: (message) => `Haukuhifadhiwa: ${message}`,
    badgeApproved: 'Umeidhinishwa na msimamizi',
    badgeCorrected: 'Umesahihishwa na msimamizi',
    badgeRejected: 'Umekataliwa na msimamizi',
    badgePending: 'Umewasilishwa — unasubiri uhakiki wa msimamizi',
    confirmTitle: 'Thibitisha usomaji wako wa kufunga',
    confirmSummaryPrefix: (n) => `Unawasilisha usomaji ${n} wenye jumla ya lita `,
    confirmSummarySuffix: ' — thibitisha.',
    litresSoldShort: (litres) => `Lita ${litres} zimeuzwa`,
    lockNote:
      'Usomaji uliowasilishwa umefungwa — msimamizi wako pekee ndiye anayeweza kuusahihisha wakati wa uhakiki.',
    submitButton: 'Wasilisha usomaji wa kufunga',
    allSubmittedTitle: 'Usomaji wote wa kufunga umewasilishwa',
    allSubmittedBody: (total, shiftClosed) =>
      `Nozeli ${total} kati ya ${total} zimewasilishwa${shiftClosed ? ' na zamu imefungwa' : ''}. Msimamizi wako atazihakiki sasa.`,
    queuedNote: (n) =>
      n === 1
        ? `Usomaji 1 umehifadhiwa kwenye simu hii na utatumwa mtandao ukirudi.`
        : `Usomaji ${n} umehifadhiwa kwenye simu hii na utatumwa mtandao ukirudi.`,
    shiftNotOpenTitle: 'Zamu yako imeshafungwa',
    shiftNotOpenBody:
      'Usomaji wa kufunga hauwezi kuchukuliwa tena. Ongea na msimamizi wako kuhusu nozeli zilizokosekana.',
    emptyTitle: 'Hakuna cha kufunga kwa sasa',
    emptyNoShift: 'Huko kwenye zamu. Usomaji wa kufunga huchukuliwa mwishoni mwa zamu yako.',
    emptyNoAssignments: 'Bado hujapangiwa nozeli, kwa hivyo hakuna cha kufunga.',
    partialSummary: (saved, total) =>
      `Imehifadhi usomaji ${saved} kati ya ${total}. Rekebisha nozeli zilizowekwa alama hapa chini kisha ujaribu tena.`,
    toastQueuedTitle: 'Usomaji wa kufunga umehifadhiwa kwenye simu hii',
    toastQueuedBody: 'Utatumwa mtandao ukirudi.',
    toastSubmittedTitle: 'Usomaji wa kufunga umewasilishwa',
    toastSubmittedBody: 'Msimamizi wako atauhakiki na kuuthibitisha sasa.',
    errAlreadySubmitted:
      'Usomaji wa kufunga ulishawasilishwa kwa nozeli hii — unasubiri uhakiki wa msimamizi.',
    errAlreadyRecorded: 'Usomaji wa kufunga ulisharekodiwa kwa nozeli hii.',
    errGeneric: 'Imeshindikana kuhifadhi usomaji huu. Angalia mtandao wako kisha ujaribu tena.',
    viewReviewStatus: 'Angalia hali ya uhakiki',
  },

  collections: {
    title: 'Makusanyo',
    subtitle: 'Kabidhi kila ulichokusanya zamu hii. Kiasi kinalinganishwa na mita.',
    errLoadTitle: 'Imeshindikana kupakia makusanyo yako',
    emptyTitle: 'Hakuna makusanyo kwa sasa',
    emptyNoShift: 'Huko kwenye zamu. Makusanyo huwasilishwa baada ya zamu yako kufungwa.',
    preCloseBody:
      'Makusanyo yanayotarajiwa yatapatikana baada ya zamu kufungwa. Maliza usomaji wako wa kufunga kisha subiri msimamizi wako afunge zamu.',
    awaitVerification:
      'Msimamizi wako bado anahakiki usomaji wako wa kufunga. Wasilisha makusanyo yako kiasi kinachotarajiwa kitakapokuwa cha mwisho.',
    expectedCollection: 'Makusanyo yanayotarajiwa',
    totalExpected: 'Jumla inayotarajiwa',
    litresTimesPrice: (litres, price) => `Lita ${litres} × ${price}`,
    tenderCash: 'Taslimu',
    tenderMobileMoney: 'Pesa za simu',
    tenderCard: 'Kadi',
    tenderCredit: 'Mkopo',
    formTitle: 'Wasilisha makusanyo yako',
    tenderInvalid: 'Weka kiasi cha pesa kama 250000 au 250000.50 (bila alama ya kutoa).',
    submittedTotal: 'Jumla iliyowasilishwa',
    expected: 'Kinachotarajiwa',
    balanced: 'Imelingana — jumla yako inalingana na makusanyo yanayotarajiwa.',
    shortage: (amount) => `Upungufu wa ${amount} — unakabidhi pungufu ya kinachotarajiwa.`,
    excess: (amount) => `Ziada ya ${amount} — unakabidhi zaidi ya kinachotarajiwa.`,
    reasonLabel: 'Sababu ya tofauti (inahitajika)',
    reasonPlaceholderZero: 'Eleza kwa nini hauwasilishi chochote',
    reasonPlaceholderDiff: 'Eleza kwa nini jumla yako hailingani na kiasi kinachotarajiwa',
    reasonMissing: 'Andika sababu fupi kabla ya kuwasilisha.',
    submitButton: 'Wasilisha makusanyo',
    confirmTitle: 'Thibitisha makusanyo yako',
    confirmPart1: 'Unawasilisha ',
    confirmPart2: ' wakati kinachotarajiwa ni ',
    confirmPart3: ' — tofauti ',
    confirmPart4: ' — thibitisha.',
    onceNote:
      'Unaweza kuwasilisha makusanyo mara moja tu kwa zamu hii. Baada ya hapo, msimamizi wako pekee ndiye anayeshughulikia mabadiliko.',
    yourSubmission: 'Mawasilisho yako',
    receiptWaiting: 'Yamewasilishwa — yanasubiri msimamizi wako athibitishe kupokea.',
    supervisorReceipt: 'Risiti ya msimamizi',
    receivedRow: 'Kilichopokelewa',
    differenceRow: 'Tofauti',
    rejectedAlert: 'Makusanyo yako yamekataliwa. Mwone msimamizi wako.',
    comment: (text) => `Maoni: ${text}`,
    badgeReceived: 'Imepokelewa',
    badgeApprovedWithDifference: 'Imeidhinishwa na tofauti',
    badgeRejected: 'Imekataliwa',
    errVarianceReason:
      'Jumla yako hailingani na kiasi kinachotarajiwa — ongeza sababu inayoeleza tofauti.',
    errAlreadySubmitted: 'Makusanyo yalishawasilishwa kwa zamu hii.',
    errGeneric:
      'Imeshindikana kuwasilisha makusanyo yako. Angalia mtandao wako kisha ujaribu tena.',
    toastQueuedTitle: 'Makusanyo yamehifadhiwa kwenye simu hii',
    toastQueuedBody: 'Yatatumwa mtandao ukirudi.',
    toastSubmittedTitle: 'Makusanyo yamewasilishwa',
    toastSubmittedBody: 'Msimamizi wako atathibitisha sasa pesa anazopokea kutoka kwako.',
  },

  review: {
    title: 'Hali ya uhakiki wa usomaji',
    progress: (verified, total) =>
      `Usomaji ${verified} kati ya ${total} umehakikiwa na msimamizi wako.`,
    emptyTitle: 'Hakuna usomaji wa kuhakikiwa',
    emptyNoShift: 'Huko kwenye zamu, kwa hivyo hakuna usomaji uliowasilishwa wa kufuatilia.',
    emptyNoAssignments: 'Hujapangiwa nozeli kwenye zamu hii.',
    badgeNotSubmitted: 'Haujawasilishwa bado',
    badgeApproved: 'Umeidhinishwa',
    badgeCorrected: 'Umesahihishwa na msimamizi',
    badgeRejected: 'Umekataliwa',
    badgeFlagged: 'Umewekewa alama ya uchunguzi',
    badgePending: 'Unasubiri uhakiki wa msimamizi',
    submitPrompt: 'Wasilisha usomaji wa kufunga wa nozeli hii ili uhakiki uanze.',
    youSubmitted: 'Uliwasilisha',
    supervisorApproved: 'Msimamizi aliidhinisha',
    difference: 'Tofauti',
    approvedReading: 'Usomaji ulioidhinishwa',
    reasonLabel: 'Sababu:',
    notReviewedYet: 'Msimamizi wako bado hajauhakiki usomaji huu.',
    finishClosings: 'Maliza usomaji wa kufunga',
    // Usomaji uliokataliwa hurudishwa kwa mhudumu ili asome upya.
    rejectedTitle: 'Msimamizi wako amekataa usomaji huu',
    rejectedHelp: 'Soma upya kipimo cha kufunga cha nozeli hii kisha uwasilishe tena.',
    resubmitCta: 'Wasilisha tena usomaji wako wa kufunga',
    // Usomaji uliowekewa alama uko kwenye uchunguzi wa msimamizi — hakuna hatua.
    flaggedHelp: 'Msimamizi wako anachunguza usomaji huu. Hakuna hatua inayohitajika kwako bado.',
  },

  complete: {
    emptyTitle: 'Hakuna zamu ya kukamilisha',
    emptyBody: 'Huko kwenye zamu kwa sasa.',
    notCompleteTitle: 'Zamu yako bado haijakamilika',
    doneTitle: 'Zamu imekamilika — hongera!',
    readings: 'Usomaji',
    verifiedBadge: 'Umehakikiwa',
    readingsVerified: (n, total) =>
      `Usomaji wa kufunga ${n} kati ya ${total} umehakikiwa na msimamizi wako.`,
    viewReadingDetails: 'Angalia maelezo ya usomaji',
    collections: 'Makusanyo',
    badgeApprovedWithDifference: 'Imeidhinishwa na tofauti',
    badgeReceived: 'Imepokelewa',
    badgeSubmitted: 'Imewasilishwa',
    expected: 'Kinachotarajiwa',
    youSubmitted: 'Uliwasilisha',
    supervisorReceived: 'Msimamizi alipokea',
    difference: 'Tofauti',
    viewCollectionDetails: 'Angalia maelezo ya makusanyo',
    checkOut: 'Toka kazini',
    checkedOutQueued: 'Umetoka kazini — imehifadhiwa kwenye simu hii, itatumwa mtandao ukirudi.',
    youAreCheckedOut: 'Umetoka kazini',
    errCheckOut: 'Imeshindikana kutoka kazini. Jaribu tena.',
    toastQueuedTitle: 'Kutoka kazini kumehifadhiwa kwenye simu hii',
    toastQueuedBody: 'Kutatumwa mtandao ukirudi.',
    toastDoneTitle: 'Umetoka kazini',
    toastDoneBody: 'Asante kwa zamu yako — tutaonana tena.',
  },

  sync: {
    chipOffline: 'Hakuna mtandao',
    chipOfflineWaiting: (n) => `Hakuna mtandao — ${n} zinasubiri`,
    chipSyncing: 'Inatuma…',
    chipAuth: 'Ingia tena ili kumaliza kutuma',
    chipConflict: 'Inahitaji kuangaliwa',
    chipFailed: 'Imeshindwa kutuma',
    chipWaiting: (n) => `${n} zinasubiri kutumwa`,
    chipSynced: 'Kila kitu kimetumwa',
    chipOnline: 'Mtandaoni',
    offlineHint:
      'Huna mtandao — unaona taarifa za mwisho zilizotumwa. Chochote utakachowasilisha kitahifadhiwa kwenye simu hii na kitatumwa mtandao ukirudi.',
    sheetTitle: 'Hali ya utumaji',
    sheetClose: 'Funga hali ya utumaji',
    authNote:
      'Kipindi chako kiliisha kabla kila kitu hakijatumwa. Ingia tena ili kumaliza kutuma — hakuna kilichopotea.',
    emptyQueue: 'Hakuna kinachosubiri kutumwa. Kila ulichowasilisha kimefika kwenye seva.',
    savedAt: (when) => `Imehifadhiwa ${when}`,
    statusPending: 'Inasubiri kutumwa',
    statusSyncing: 'Inatuma…',
    statusSynced: 'Imetumwa',
    statusFailed: 'Imeshindwa',
    statusConflict: 'Inahitaji msimamizi',
    tryAgain: 'Jaribu tena',
    discard: 'Futa',
    discardConfirm: 'Gusa tena ili kufuta',
    syncNow: 'Tuma sasa',
    updateReady: 'Programu imesasishwa — gusa ili kupakia upya',
    actionCheckIn: 'Ingia kazini',
    actionCheckOut: 'Toka kazini',
    actionConfirmAssignment: (pump, nozzle) => `Thibitisha pampu ${pump} · nozeli ${nozzle}`,
    actionConfirmAssignmentGeneric: 'Thibitisha mpangilio wa nozeli',
    actionOpeningReading: (reading, pump, nozzle) =>
      `Usomaji wa kufungua ${reading} — pampu ${pump} · nozeli ${nozzle}`,
    actionOpeningReadingGeneric: (reading) => `Usomaji wa kufungua ${reading}`,
    actionClosingReading: (reading, pump, nozzle) =>
      `Usomaji wa kufunga ${reading} — pampu ${pump} · nozeli ${nozzle}`,
    actionClosingReadingGeneric: (reading) => `Usomaji wa kufunga ${reading}`,
    actionCollection: 'Wasilisha makusanyo',
    actionReportIssue: (issueType) => `Ripoti tatizo la ${issueType.toLowerCase()}`,
    errOpeningBelowExpected:
      'Usomaji uko chini ya kufunga kulikoidhinishwa kwa zamu iliyopita. Mpigie msimamizi wako.',
    errAssignmentChanged:
      'Mpangilio wako wa nozeli ulibadilika ukiwa nje ya mtandao. Angalia mpangilio wako kisha uthibitishe tena.',
    errReadingConflict: (readingType, serverValue) => {
      const type = readingType === 'opening' ? 'kufungua' : 'kufunga';
      return serverValue != null
        ? `Seva tayari ina usomaji tofauti wa ${type} (${serverValue}) kwa nozeli hii. Kipimo chako kimehifadhiwa hapa — mwonyeshe msimamizi wako.`
        : `Seva ilisema usomaji huu wa ${type} ulishawasilishwa lakini hauonekani kwenye zamu yako. Kipimo chako kimehifadhiwa hapa — mwonyeshe msimamizi wako.`;
    },
    errCollectionConflict: (serverTotal) =>
      `Makusanyo yalishawasilishwa kwa zamu hii kwa jumla tofauti (${serverTotal}). Kiasi chako kimehifadhiwa hapa — mwonyeshe msimamizi wako.`,
    errVerifyUnavailable: 'Imeshindikana kuthibitisha na seva — itajaribiwa tena.',
    errNoActiveShift:
      'Huko tena kwenye zamu, kwa hivyo tatizo hili halikuweza kutumwa. Ingia kwenye zamu kwanza, au mpigie msimamizi wako.',
    errIssueInvalid: 'Ripoti hii ya tatizo haikuweza kutumwa. Irekebishe ujaribu tena, au uifute.',
  },

  settings: {
    title: 'Mwonekano na lugha',
    close: 'Funga mipangilio',
    language: 'Lugha',
    english: 'English',
    swahili: 'Kiswahili',
    textSize: 'Ukubwa wa maandishi',
    textNormal: 'Kawaida',
    textLarge: 'Makubwa',
    contrast: 'Utofauti wa rangi',
    contrastNormal: 'Kawaida',
    contrastHigh: 'Juu',
    done: 'Sawa',
  },

  install: {
    prompt: 'Mhudumu wa pampu? Sakinisha programu',
    loadingCode: 'Inapakia msimbo…',
    qrTitle: 'Fungua programu ya mhudumu',
    scanInstruction1: 'Skani kwa kamera ya simu yako, ingia, kisha tumia ',
    addToHomeScreen: 'Ongeza kwenye Skrini ya Mwanzo',
    scanInstruction2: ' ya kivinjari chako ili kusakinisha.',
    languageLabel: 'Lugha',
  },
};
