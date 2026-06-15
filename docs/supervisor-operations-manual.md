# FuelGrid OS — Supervisor Operations Manual

_This manual was generated for FuelGrid OS. It explains, in simple steps, how a fuel-station supervisor runs the day using the system. The app changes over time, so please keep this manual updated when buttons, screens, or rules change._

---

## 1. Welcome — Who this is for

This manual is for the **supervisor** at a fuel station.

A supervisor is the person in charge of a work shift. Your job is to make sure the right people are working the right pumps, that the meter numbers (the fuel readings) are correct, and that the cash collected matches the fuel that was sold. You check the work, you fix mistakes, and you give the final "this shift is good" approval at the end.

You use **two tools**, and they work together:

- **The main app** — this runs on a **computer or a tablet**. This is your **supervisor cockpit**. This is where _you_ work. You check things, you approve things, you fix mistakes here.
- **The mobile app** — this runs on a **phone**. This is the **attendant tool**. The attendant (the person at the pump) uses this.

The simple idea is this:

> The **attendant records on the phone**. The **supervisor checks and approves on the computer**.

The attendant types in the pump numbers and the cash on their phone. Those numbers travel into your computer. You look at them, and you say yes (approve), or you fix them, or you send them back. Nothing becomes final until **you approve it**.

You do almost all of your work on the **computer**, not the phone. (More on this in Section 8.)

---

## 2. Words you need to know

Read this little list once. Each word is explained in plain language. You can come back here any time.

**About the work**

- **Shift** — one work period. Like the morning team from 6am to 2pm, or the evening team from 2pm to 10pm.
- **Operating day** — the whole working day at the station. It holds all the shifts for that day. It must be opened before any shift can start, and closed at the end.
- **Slot** — which part of the day a shift is for: **morning** or **evening**.
- **Attendant** — the worker at the pump who serves customers and records the numbers on the phone.
- **Substitute** — a fill-in worker who is not on the normal team for that day, but works the shift in place of someone who is away.
- **Roster / rostered** — the work schedule that says which team works which day. A "rostered" person is one the schedule says should be working.

**About pumps and readings**

- **Nozzle / Pump** — the hose and handle a customer uses to fill their tank. One pump can have more than one nozzle. In the app a nozzle is named like "**Pump 1 · Nozzle 2**". In a picker (a drop-down list) this may be shortened to "**P1·N2**", which is short for **Pump 1, Nozzle 2**.
- **Tank code** — the short name of the fuel tank that a nozzle draws its fuel from. Your station sets these names. You do not need to change them.
- **Meter reading** — the number on the pump that counts total litres ever sold through that nozzle. It only goes up.
- **Opening reading** — the meter number at the **start** of the shift.
- **Closing reading** — the meter number at the **end** of the shift.
- **Litres sold** — closing reading minus opening reading. This is how much fuel was sold in the shift.
- **Nozzle assignment** — saying who works which nozzle. Each attendant is responsible for the fuel and money on the nozzles assigned to them.

**About money**

- **Collection** — the money the attendant collected during the shift (cash, mobile money, card, and credit).
- **Tender type** — the way money was paid: cash, mobile money, card, or credit. (You may just think of it as "the way the money came in.")
- **Shortage** — when the money handed over is **less** than expected. Money is missing.
- **Excess** — when the money handed over is **more** than expected. There is extra money.
- **Variance** — how much more or less the money is than expected. A shortage or an excess is a variance.
- **Revenue** — the money the station earns from selling fuel. **Revenue recognized** means the app now counts a shift's fuel sales as real, final money in the station's records.

**About checking and deciding**

- **Verify** — to check a reading and decide if it is correct.
- **Approve** — to say "this is correct and final."
- **Verdict** — your decision. When the app asks for a "verdict," it just means: choose what you have decided to do.
- **Exception** — an open problem the app is holding onto, like a rejected or flagged reading. A shift cannot be approved while there are open exceptions.
- **Void** — to cancel a sale. As a supervisor you can only **request** a void; a higher manager must approve it.
- **Incident** — a problem someone reports, like a broken pump, a safety worry, or a payment fault. Incidents are tracked until they are fixed.
- **Reconciliation** — checking that the fuel counted in the tank matches what the records say should be there.
- **Handover** — when one shift ends and the next shift starts. The ending shift's closing numbers become the next shift's opening numbers.
- **Separation of duties** — a safety rule: the person who **recorded** a number is **not allowed** to also be the person who **approves** it. This stops cheating. The app will block you if you try.
- **Audit log / audited** — a record the app keeps of who did what and when. It cannot be erased. "Audited" means the app saved a permanent note of your action.

**About using the computer**

- **Web browser** — the program you use to open websites, like Chrome.
- **Sidebar** — the menu on the left side of the screen.
- **QR code** — a square barcode you scan with your phone camera to open or install something.
- **Hover** — rest your mouse pointer on a button without clicking it.
- **Tooltip** — the small message that pops up when you hover over a button (often telling you why it is greyed out).
- **Badge** — a small coloured label on the screen that shows a status, like **Checked in** or **Approved**.
- **Filter** — to show only some items instead of all of them.
- **Feed** — your running list of messages (your notifications).

### A note about words in curly braces { }

In a few places this manual shows words inside curly braces, like **{slot}**, **{shift name}**, **{next status}**, or **{time}**. The braces are **not** real. They mean the app fills in the real word there. For example, **{slot}** becomes **morning** or **evening**, and **{time}** becomes the real time. You will never see the braces on the screen.

---

## 3. Your daily duties — a simple checklist

Here is your day, from start to finish. Don't worry — every step is explained in detail later.

1. **Log in** to the main app on your computer or tablet.
2. Open the **Operations** screen and **pick your station**.
3. **Open the operating day** (the whole working day) if it is not open yet.
4. Check the **scheduled team** for the slot, then **open the shift** (morning or evening).
5. **Assign attendants to nozzles** (say who works which pump).
6. Let the attendants **check in** and start work on their phones.
7. When the shift ends, the attendants send in their **closing readings**.
8. **Verify the closing readings** — approve them, correct them, reject them, or flag them.
9. The attendant sends in the **collection** (the cash).
10. **Confirm the cash** you received, and write down any shortage or excess.
11. **Approve the shift** when everything is green.
12. **Open the next shift** (the handover).
13. At the end of all shifts, **close the day** and **lock the day**.
14. Along the way: handle problems, check your reports, and read your notifications.

Print this list and keep it near you until it feels easy.

---

## 4. Getting started

### How to log in

1. Open the main app on your **computer or tablet** in a **web browser** (the program you use to open websites, like Chrome).
2. Type your **username** and **password**.
3. Click **Sign in**.

Why this matters: the app knows who you are. Some buttons only show for supervisors. The app also keeps a record of every action you take.

### Where things are

On the **left side** of the screen there is a menu (we call it the **sidebar**). These are the areas you will use most:

- **Operations** — your main daily screen. This is where shifts live.
- **Teams & Roster** — where teams and the rotation are set up. (Note: this menu item is named "Teams & Roster", but a warning message on the Operations screen may call the same area "Teams & Rotation". They mean the same place.)
- **Sales** — where you can request to cancel (void) a sale.
- **Reconciliation** — where tank counts are checked.
- **Incidents** — where you handle problems and issues.
- **Notifications** — your message feed (your running list of messages, opened with the bell).
- **Reports** — daily reports you should look at.

Why this matters: knowing where things are saves you time during a busy shift.

### A note about buttons you do not see

Some buttons only appear if you have permission to use them. If you cannot see a button this manual mentions, it may be that your station gives that job to a higher manager. That is normal. The app also double-checks every action, so if something is not allowed, you will see a clear message.

---

## 5. The shift, step by step (the heart of the manual)

This is the most important section. We walk the **whole life of a shift**, from start to end.

At each step there are **two blocks**:

- **On the phone (attendant)** — what your attendant does on their phone.
- **On the computer (you, the supervisor)** — what you do in the main app.

Remember the rule: the attendant **records**, you **check and approve**.

---

### Step 1 — Open the operating day

The **operating day** is the whole working day at the station. It must be open before any shift can run.

**On the computer (you):**

1. Click **Operations** in the left menu.
2. At the top right, use the **Station** picker to choose your station.
3. If you see **"No active operating day"**, that means today is not open yet.
4. Type today's date in the **Operating day date** box.
5. Click **Open operating day**. (While it works, the button says "Opening…".)

_Why this matters:_ shifts cannot start until the day is open. Think of it like unlocking the station for the day.

**On the phone (attendant):**

- Nothing yet. If the attendant looks at their phone, it says their team covers today's shift and to **wait for the supervisor to open the shift**. There is no button for them to press.

---

### Step 2 — Check the team and open the shift

Each station has **three teams** that take turns. Each day, **two teams work** (one in the morning slot, one in the evening slot) and one team rests. The app already knows whose turn it is.

**On the computer (you):**

1. On the **Operations** screen, look at the **scheduled team** line. It says something like: _"Scheduled team for morning: **Team A** · 3 member(s) · John, Mary, Peter."_
2. If instead you see a warning — _"No team scheduled for {slot}. Configure teams + the rotation anchor under Teams & Rotation before opening a shift"_ — it means the teams or the turn-taking are not set up yet. (Here **{slot}** will show the real word, **morning** or **evening**. The "**rotation anchor**" is the setting that tells the app where the team turn-taking starts.) You must fix this under **Teams & Roster** first. If you do not know how to set this up, ask whoever set up your station — you may not be able to fix it yourself. The **Open shift** button stays off until a team appears.
3. In the **New shift name** box, type a simple name like `Morning`.
4. In the slot picker, choose **Morning** or **Evening**.
5. Click **Open shift**. (It says "Opening…" while it works.)

_Why this matters:_ opening the shift turns it on. The app automatically adds the team's attendants to the shift, so you do not type their names one by one.

**On the phone (attendant):**

- The moment you open the shift, the attendant's phone changes. Their screen now shows a **Check in** button.

---

### Step 3 — Assign attendants to nozzles

Now you say **who works which nozzle**. This is important because each attendant is responsible for the fuel and money on their own nozzles.

**On the computer (you):** there are two places to do this.

**Place A — on the shift card (Operations screen):**

1. Find the shift card. Look for the **"Nozzle assignment"** section (it shows while the shift is open).
2. In the **Nozzle** picker (drop-down list), choose a nozzle (for example "P1·N2 · tank code"). Here "**P1·N2**" is short for **Pump 1, Nozzle 2**, and the **tank code** is the short name of the fuel tank that nozzle draws from (your station sets these names).
3. In the **Attendant** picker, choose the attendant.
4. Click **Assign**. (It says "Assigning…" while it works.)
5. To remove an assignment, click the small **rubbish-bin picture (delete)** next to it.

**Place B — on the shift review page (for substitutes):**

If someone is filling in who is **not** on the normal team (a substitute), use this place.

1. Open the shift's **Review** page (see Step 5 for how).
2. Find the **"Attendant assignment"** panel.
3. Pick a **Nozzle** and an **Attendant**. A substitute's name will show "(substitute)" next to it.
4. Click **Assign**.
5. To remove someone, click **Unassign**. A box appears asking **"Unassign this nozzle?"** — click **Unassign** again to confirm, or **Cancel** to stop.

_Why this matters:_ if a nozzle has no attendant, the attendant cannot start. Assigning the nozzle is what unlocks their work.

**On the phone (attendant):**

- The attendant gets a notification: _"Nozzle assigned to you — review and confirm it on My Shift."_
- They press **Confirm nozzles** on their phone to accept the nozzles. (You do **not** confirm this — the attendant does.)

---

### Step 4 — The attendant checks in

**On the phone (attendant):**

1. The attendant presses **Check in** on their phone.

**On the computer (you):**

1. Open the shift **Review** page (see Step 5).
2. Look at the **Attendance** panel. Each person shows a badge: **Checked in**, **Checked out**, or **Not checked in**.
3. You only **watch** here. There is no button for you to press. If someone is **Not checked in**, find out why.

_Why this matters:_ check-in tells you who actually showed up for work.

---

### Step 5 — Open the shift Review page

The **Review** page is where you do most of your checking. Open it like this:

1. On the **Operations** screen, find the shift card.
2. Click the shift **name** (it is a link), or click the **Review** button.
3. You are now on the shift review page. The title says "**{shift name} · review**" — where **{shift name}** shows your shift's real name, like **Morning · review**.
4. To go back, click the **Operations** button at the top right.

From top to bottom you will see:

- **Shift summary** — the key facts about the shift.
- **Attendance** — who has checked in.
- **Attendant assignment** (shows only while the shift is open) — who works which nozzle.
- **Closing readings to verify** — the readings waiting for you to check.
- **Collection receipt** (shows after the shift is closed) — where you confirm the cash.
- **Approval readiness** checklist (shows after the shift is closed) — the green ticks you need before approving.
- **Timeline** — the history of what happened during the shift.

---

### Step 6 — Opening readings and working (mostly the attendant)

**On the phone (attendant):**

1. The attendant presses **Verify opening readings**.
2. The phone shows each nozzle with the **expected** opening number already filled in (this comes from the last shift's approved closing). The attendant confirms or types the real meter number, then presses **Confirm and save**.
3. If they type a number **below** the expected opening, the phone blocks it (a meter only goes up) and offers **Report issue to supervisor**.
4. After that, the phone shows **Enter closing readings** and the attendant works the shift, serving customers.

**On the computer (you):**

- Nothing to do here. You wait. Opening readings are the attendant's job. Your verification job is for the **closing** readings, which come next.

---

### Step 7 — Closing readings come in, and you VERIFY them

When the shift ends, the attendant types the **closing** meter numbers on the phone. Then those numbers wait for **you** to check.

**On the phone (attendant):**

1. The attendant presses **Enter closing readings** and types each closing number, then **Confirm and save**.
2. The phone then says: _"Closing readings submitted. Wait for your supervisor to verify them."_ It checks for updates every 30 seconds.

**On the computer (you):** go to the shift Review page and find the **"Closing readings verification"** queue.

Each row shows: the nozzle (like "Pump 1 · Nozzle 2 · Petrol"), the attendant's name, the opening number and the submitted closing number, the litres sold, and a **status badge**.

You have **four choices**. Pick the right one for each reading.

**Important rule (separation of duties):** you **cannot** verify a reading **you yourself recorded**. If you try, the app blocks you with a message about "separation of duties." This is normal and is there to keep things honest.

#### Choice A — Approve all (the fast way, when everything looks right)

1. Click **Approve all (N)** at the top right of the card. (N is the number of readings waiting.)
2. A box appears: **"Approve all pending readings?"**
3. Click **Approve all as submitted**.

This says all the readings are correct exactly as the attendant typed them. Use this when nothing looks wrong.

#### Choice B — Correct one reading (a figure is wrong)

Use this when **one** number is wrong but you know the right number.

1. On that reading's row, click **Correct…**.
2. A box opens: **"Correct closing reading."**
3. In **Verified reading**, type the correct number.
   - It must be numbers only (like `1500` or `1500.25`).
   - It cannot have more decimals than the meter allows.
   - It **cannot be below the opening reading** (a meter only goes up).
4. In **Reason (required)**, type why. For example: `pump display misread by attendant`.
5. Click **Verify with correction**.

The original number is kept as history. Your corrected number becomes the final, approved number. The row will show "Submitted X → approved Y — reason."

_Why a reason matters:_ it keeps a clear record of why the number changed.

#### Choice C — Reject one reading (send it back)

Use this when the number looks wrong and you want the **attendant to type it again**.

1. On that reading's row, click **Reject…**.
2. A box opens: **"Reject closing reading."**
3. In **Reason (required)**, type why. For example: `photo does not match the meter — please re-capture`.
4. Click **Reject reading**.

The reading goes back to the attendant's phone to be re-entered. **The shift cannot be approved** until the attendant resubmits and you verify the new number. The badge shows **Rejected**.

#### Choice D — Flag one reading for investigation

Use this when something looks very wrong (maybe tampered) and you want to hold it for a closer look.

1. On that reading's row, click **Flag for investigation…**.
2. A box opens: **"Flag reading for investigation."**
3. In **Reason (required)**, type why. For example: `figure looks tampered — escalating to the manager`.
4. Click **Flag reading**.

The badge shows **Flagged for investigation**. The shift **cannot** be approved while a flag is open. Only you can clear it — by correcting the reading or approving it once you are satisfied. The attendant **cannot** clear a flag.

> **Tip:** If a reading is already rejected or flagged and you later decide it is fine, you will see an **Approve as submitted** button on that row to clear the hold.

---

### Step 8 — Collections come in, and you CONFIRM the cash

After the readings are verified, the attendant sends in the **collection** — the money. Then you confirm the cash you actually received.

**On the phone (attendant):**

1. The attendant presses **Submit collections**.
2. The phone shows the **expected** collection and a breakdown by type: **cash**, **mobile money**, **card**, and **credit**. It also shows any shortage or excess.
3. The attendant confirms and sends it.
4. The phone then says: _"Collections submitted. Wait for your supervisor to confirm receipt."_

**On the computer (you):** on the shift Review page, find the **"Collection receipt"** panel. (This only shows after the shift is closed.)

You will see:

- **Expected collection** — how much money the app expects, based on the fuel sold.
- **Attendant submitted** — how much the attendant said they collected.
- The four ways money comes in: **Cash, Mobile money, Card, Credit**.
- **Variance vs expected** — how much more or less the money is than expected (a shortage or an excess).
- Any note the attendant left.

Now count the real cash in your hand and record it:

1. In **Received total**, type the amount you actually received. (Up to 2 decimals, like `450000.00`.)
   - If your number is **different** from the expected number, a warning shows the difference, and a **reason becomes required**.
2. Choose a **Verdict** (this just means your decision — pick one):
   - **Confirm the cash received** — the money is fine, you accept it.
   - **Reject — send back to the attendant to resubmit** — there is a problem; the attendant must fix and resend. A reason is required.
   - **Flag for investigation — hold the cash review** — hold it for a closer look. A reason is required.
3. If a reason is required (or you want to add one), type it in the **Reason** box. You can also add a **Comment**.
4. Click the button that matches your verdict:
   - **Confirm receipt**, or
   - **Reject handover**, or
   - **Flag handover**.

_What the result means:_

- If your received amount **equals** the expected amount, the result shows **"Received — balanced."** Everything matches.
- If your received amount is **different**, the result shows **"Approved with difference."** This is your recorded shortage or excess, with your reason.
- If you rejected or flagged it, the result shows **Rejected** or **Flagged for investigation**.

**Separation of duties again:** you **cannot** confirm a cash submission **you yourself made**. The app will block you. This keeps the money honest.

_Why this matters:_ confirming the cash is how shortages and excesses get recorded properly, with a reason, so the books are correct.

**Example:** Expected collection is **TZS 500,000**. The attendant hands you **TZS 480,000** in cash. You type `480000` in **Received total**. The app warns you are **TZS 20,000 short**. You type a reason like `attendant gave wrong change to two customers`, choose **Confirm the cash received**, and click **Confirm receipt**. The result shows **"Approved with difference"** with a 20,000 shortage. The record is now clear and honest.

---

### Step 9 — Approve the shift

This is the final "yes" for the shift. You can only do it when everything is ready.

**On the computer (you):** on the shift Review page, find the **"Approval readiness"** panel. It is a checklist. Each item shows a **green tick** if it is OK, or a **warning** if it is not.

The three checks are:

1. **Readings** — all readings verified, none rejected, none flagged.
2. **Collection** — cash submitted and the receipt confirmed (received or approved-with-difference).
3. **Exceptions** — no open problems (exceptions) left.

When all three are green:

1. Click **Approve shift**. (It says "Approving…" while it works.)
2. The panel then shows: _"Shift approved {time} — revenue recognized from the verified readings."_ (Here **{time}** is the real time it was approved.) "**Revenue recognized**" means the app now counts this shift's fuel sales as real, final money in the station's records.

If the **Approve shift** button is greyed out, **hover** over it (rest your mouse pointer on it without clicking). A small message (a **tooltip**) will pop up saying **"Complete the checklist first."** That means one of the three checks is not green yet.

**Blocked messages explained.** If you try to approve too early, the app shows a clear message. Here is what each one means and how to fix it:

- _"… readings rejected — the attendant must resubmit the closing reading(s), then re-verify before approving."_ → A reading was sent back. Wait for the attendant to resend, then verify it (Step 7).
- _"… readings flagged for investigation — resolve the flag(s) (correct and re-verify) before approving."_ → A reading is flagged. You must correct it or approve it to clear the flag (Step 7, Choice D).
- _"… still awaiting verification — verify them above, then approve."_ → Some readings are not checked yet. Go verify them (Step 7).
- _"The collection has not been confirmed — record the cash receipt above, then approve."_ → You have not confirmed the cash. Go do Step 8.

_Why this matters:_ approving the shift makes the sales count as real, final money in the station's records. The checks make sure nothing is missing or wrong before that happens.

---

### Step 10 — The next shift unlocks (handover)

The **handover** is the link between one shift and the next. The next shift's opening numbers come from this shift's approved closing numbers. So you must **approve this shift before you open the next one**.

**Normal way:**

1. Approve the shift you just finished (Step 9).
2. Open the next shift normally (Step 2).

**If you try to open the next shift too early:**

You will see a warning box: **"Shift handover incomplete — approve the previous shift before opening a new one."** It lists the shift(s) blocking you, as links. Click a link to go approve that shift first.

**Override (only if you are allowed):** If you have approval rights, you may force it. In the warning box, type a reason in **Handover override reason** and click **Override and open**. The app keeps a permanent record of this, so only use it when you really must. If you do not have approval rights, you will see a message that the override needs the approval permission — in that case, ask a manager.

_Why this matters:_ if you skip the handover, the next shift's opening numbers could be wrong, and the fuel count breaks.

---

### Step 11 — Close the day and lock the day

At the end of all shifts:

1. On the **Operations** screen, click **Close day**.
   - This button is off while any shift is still open. If you hover over it (rest your mouse pointer on it), the small pop-up message (the **tooltip**) says **"Close open shifts first."** So finish your shifts first.
2. Once the day is closed, you can click **Lock day**.
   - This button is off until every shift is **approved**. Its tooltip (pop-up message) says **"Approve shifts first."**
   - There is also a **Reopen** button if you need to open the day again.

_Why this matters:_ closing and locking the day finishes the station's record for that day so the numbers are safe and final.

---

## 6. Handling problems

Things go wrong sometimes. Here is how to handle the common ones. Stay calm — the app is built to help you fix mistakes safely.

### A wrong reading

- If you know the right number, use **Correct…** (Step 7, Choice B). Type the right number and a reason.
- If you want the attendant to redo it, use **Reject…** (Step 7, Choice C).

### A rejected reading and resubmission

1. You click **Reject…** and give a reason.
2. The attendant gets a message on the phone: _"Closing reading rejected — please re-capture it."_ with your reason.
3. The attendant types the number again on the phone and presses **Resubmit closing reading**. (This works while the shift is still open.)
4. The new number comes back to your queue. **Verify it again** (Step 7).
5. The shift stays blocked from approval until this is done.

> **Note:** If the shift is already closed, the attendant cannot resubmit. In that case **you** must correct the reading yourself with **Correct…**.

### A cash shortage or excess

1. Count the real cash.
2. Type it in **Received total** (Step 8).
3. The app shows the difference. Type a clear **Reason**.
4. Choose **Confirm the cash received** and click **Confirm receipt**.
5. The result shows **"Approved with difference"** — your shortage or excess is now recorded with the reason.

If the difference is large or looks like theft, use **Reject handover** or **Flag handover** instead, and tell your manager.

### An attendant reports an issue (pump, safety, etc.)

1. The attendant presses **Report an issue** on their phone, picks a type (pump, nozzle, meter, payment, safety, or other), an urgency, and a description, then presses **Send to supervisor**.
2. You get a **notification**: _"Incident opened — check the incidents queue."_ (A critical issue may also send an email.)
3. Go to **Incidents** in the left menu.
4. Find the incident. Read it.
5. Click the **Mark** button to move it forward one step at a time: open → investigating → resolved → closed. The button names the next step, so it will read **Mark investigating**, then **Mark resolved**, then **Mark closed**.
6. You can also click **Open incident** yourself to log a problem you noticed.

_Why this matters:_ logging issues keeps a record and makes sure problems get fixed, not forgotten.

### An attendant did not show up / a substitute is needed

1. On the **Attendance** panel you will see **Not checked in** for that person.
2. To bring in a substitute, open the shift **Review** page and use the **"Attendant assignment"** panel (Step 3, Place B).
3. Pick the nozzle and the substitute (their name shows "(substitute)"), then click **Assign**.
4. The substitute confirms the nozzle on their own phone.

_Why this matters:_ every nozzle needs a responsible attendant, even when the rostered person (the one the schedule says should be working) is absent.

---

## 7. Your other duties beyond shifts

Besides running shifts, you have a few more jobs. Some jobs are **not** yours — those go to a higher manager. This section tells you which is which.

### Things you CAN do

- **Request to void (cancel) a sale.** Go to **Sales**, open the sale, and use **Request void** with a reason. _Note: you can only **request** it. A higher manager must approve the void._
- **Handle incidents.** Open, investigate, and close issues in the **Incidents** screen (see Section 6).
- **Manage tank reconciliations.** Check tank counts in the **Reconciliation** screen.
- **Read your reports** (see below).
- **Read and manage notifications** (the bell).

### Things you usually CANNOT do (these need a station manager or higher)

These buttons may not appear for you, and that is correct:

- **Change prices** — a station manager or regional manager does this in **Pricing**. You can only **view** prices.
- **Stock adjustments** (add or remove litres in a tank) — a manager requests and approves these. You can only **view** them.
- **Opening stock** entry and approval — a manager does this.
- **Approve a sale void** — a regional manager (or finance) approves; you only request.
- **Expenses** — handled by finance, not by supervisors.
- **Audit log** (the permanent record of who did what) — only senior roles see this.

If you need one of these done, **contact your manager**.

### Reports you should check each day

Go to **Reports** in the left menu. As a supervisor you can **view** these:

- **Daily Station Close** — the summary of the day's shifts.
- **Sales** — what was sold.
- **Profitability** — revenue and litres (the margin value is hidden from supervisors).
- **Inventory Reconciliation** — tank counts.
- **Risk & Loss** — fuel loss in litres.
- **Attendance** — who worked and when.

_Note:_ you can **view** these reports, but you usually **cannot export** them (download buttons need a manager). Reports like **Cash Reconciliation** and the finance/executive reports are for managers, not supervisors.

_Why check reports:_ they help you spot problems early — like a pump losing fuel, or an attendant with many shortages.

### Notifications

- Click **Notifications** (the bell) to see your feed (your running list of messages).
- Use **All** or **Unread** to filter (to filter means to show only some of the messages, not all), and **Mark all read** to clear them.
- Your feed includes shift closes, incidents, approvals, and alerts.
- In **Notification settings** you can turn categories on or off and set **quiet hours** (a start and end time when you are not disturbed).

### What needs a higher manager

Send these up the chain: price changes, stock adjustments, approving sale voids, expenses, anything flagged as serious or possible theft, and anything you do not have a button for.

---

## 8. About the mobile app (so you understand your team)

You do **not** normally use the phone app. **You work on the computer.** But it helps to understand what your attendant sees, so you can guide them.

### What the attendant does on the phone

The attendant's phone shows **one big button at a time** — the next thing to do. The journey is:

1. **Wait** for you to open the shift.
2. **Check in.**
3. **Confirm nozzles** (accept the nozzles you assigned).
4. **Verify opening readings** (confirm or type the start meter numbers).
5. Work the shift (**Enter closing readings** at the end).
6. **Wait** while you verify the readings.
7. **Submit collections** (the cash).
8. **Wait** while you confirm the cash.
9. **Finish shift** and **check out**.

Whenever the phone says "**wait for your supervisor**," it is waiting for **you** to act on the computer. The phone checks for updates every 30 seconds.

The attendant can also press **Report an issue** at any time to raise a problem (then **Send to supervisor**), which reaches you in **Incidents**.

### How the attendant installs the app

On the **login page** of the system there is a **QR code** for attendants (the install helper). A QR code is a square barcode you scan with your phone camera. The attendant points their phone camera at the QR code to install the app. After that, the app sits on their phone like any other app.

### It works even with bad internet

The phone app is built for places with weak signal. If the internet is bad, the attendant can still record numbers. The app **saves** the information on the phone and **sends it later** when the signal comes back. So the attendant is never stuck.

### Do you (supervisor) use the phone?

**No.** As a supervisor you do your work on the **main app on a computer or tablet**. The verify, confirm, assign, and approve buttons live there, not on the phone. The one screen that looks the same on both is the **notifications feed** — both you and the attendant can read messages there.

---

## 9. Quick reference card (one page)

Keep this near you. It is the short version of everything above.

### Your daily checklist

1. **(you)** Log in → **Operations** → pick **Station**.
2. **(you)** **Open operating day** (if needed).
3. **(you)** Check **scheduled team** → **Open shift** (Morning/Evening).
4. **(you)** **Assign** attendants to nozzles.
5. **(attendant)** Attendants **check in** and work (on their phones).
6. **(attendant → you)** Closing readings arrive → **you verify** them.
7. **(attendant → you)** Cash arrives → **you confirm** it (record shortage/excess + reason).
8. **(you)** **Approve shift** (when all green).
9. **(you)** **Open next shift** (handover).
10. **(you)** **Close day**, then **Lock day**.

### Reading statuses — what each means

- **Pending verification** — waiting for you to check it.
- **Approved** — you accepted it as typed.
- **Corrected** — you changed it and gave a reason; your number is final.
- **Rejected** — sent back to the attendant to redo. _Blocks approval._
- **Flagged for investigation** — held for a closer look. _Blocks approval. Only you clear it._

### Cash receipt results — what each means

- **Received — balanced** — money matches expected.
- **Approved with difference** — money differs; your shortage/excess is recorded with a reason.
- **Rejected** — sent back to the attendant to resubmit.
- **Flagged for investigation** — held for a closer look.

### Blocked messages — what to do

- **"Complete the checklist first"** → one of the three checks (Readings / Collection / Exceptions) is not green yet.
- **"… readings rejected …"** → attendant must resubmit; then re-verify.
- **"… readings flagged …"** → correct and re-verify to clear the flag.
- **"… still awaiting verification …"** → verify the remaining readings.
- **"The collection has not been confirmed …"** → confirm the cash receipt first.
- **"Shift handover incomplete …"** → approve the previous shift before opening a new one.
- **"Close open shifts first"** → finish all shifts before closing the day.
- **"Approve shifts first"** → approve all shifts before locking the day.
- **"separation of duties …"** → you cannot verify or confirm something you recorded yourself. Ask another supervisor to do it.

### Who to call for what

- **Price change** → station manager.
- **Stock adjustment / opening stock** → station manager.
- **Approve a sale void** → regional manager (you only request).
- **Expenses** → finance.
- **Possible theft / serious issue** → your manager (and flag it in the app).
- **Reports export / finance reports** → station or regional manager.

---

_End of manual. Please keep this document updated as the FuelGrid OS app changes — screen names, button labels, and rules may be improved over time._
