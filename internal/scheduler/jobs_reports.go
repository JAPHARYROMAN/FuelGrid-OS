package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/japharyroman/fuelgrid-os/internal/accounting"
	"github.com/japharyroman/fuelgrid-os/internal/database"
	"github.com/japharyroman/fuelgrid-os/internal/email"
)

// ReportDeps groups everything the canned scheduled-email jobs need: the owner
// pool (the jobs run cross-tenant, like every other scheduler job), the
// transactional email Sender, the accounting repo (to source the company P&L),
// and the static recipient/send-hour configuration. When Recipients is empty
// or the Sender is the console driver the jobs are a deliberate safe no-op —
// they compose nothing and send nothing — matching the env-gated SMTP pattern.
type ReportDeps struct {
	Pool       *database.Pool
	Email      email.Sender
	Accounting *accounting.Repo
	Recipients []string
	// SendHour is the hour-of-day (0–23, server local time) at or after which a
	// once-per-period digest is allowed to send. The interval ticker fires every
	// SCHEDULER_REPORT_DIGEST_INTERVAL; the body only sends when the clock has
	// reached SendHour AND the ledger shows no successful send yet this
	// day/month, so a sub-day interval collapses to exactly one send per period.
	SendHour int
	Logger   *slog.Logger
	// now is injected for tests; nil means time.Now.
	now func() time.Time
}

func (d ReportDeps) clock() time.Time {
	if d.now != nil {
		return d.now()
	}
	return time.Now()
}

func (d ReportDeps) log() *slog.Logger {
	if d.Logger != nil {
		return d.Logger
	}
	return slog.Default()
}

// configured reports whether the digest jobs have somewhere to deliver. With no
// recipients (or a nil/console sender) every send would be discarded, so the
// jobs short-circuit to a no-op rather than doing work whose output goes nowhere.
func (d ReportDeps) configured() bool {
	if len(d.Recipients) == 0 || d.Email == nil {
		return false
	}
	// The console driver intentionally drops mail (dev/CI). Treat it as
	// "unconfigured" so the digest is a true no-op until real SMTP is wired,
	// exactly like the rest of the env-gated email surface.
	return d.Email.Driver() != "console"
}

// dailyDigestJob returns the once-per-calendar-day station-close digest. It
// composes, per active tenant and station, a concise plain-text summary of the
// most recent business day (sales, litres, cash variance, open exceptions,
// stock variance) and emails it to the configured recipients. The interval
// ticker may fire several times a day; a ledger guard (a prior successful run
// today) plus the SendHour gate collapse that to exactly one send per day.
func dailyDigestJob(d ReportDeps) JobFunc {
	const jobName = "report_daily_close_digest"
	return func(ctx context.Context) (string, error) {
		if !d.configured() {
			return "skipped: no recipients or SMTP unconfigured", nil
		}
		now := d.clock()
		if now.Hour() < d.SendHour {
			return fmt.Sprintf("skipped: before send hour %02d:00 (now %02d:00)", d.SendHour, now.Hour()), nil
		}
		sent, err := d.sentSince(ctx, jobName, startOfDay(now))
		if err != nil {
			return "", fmt.Errorf("check last send: %w", err)
		}
		if sent {
			return "skipped: already sent today", nil
		}

		lines, stations, err := d.dailyDigestBody(ctx)
		if err != nil {
			return "", err
		}
		subject := fmt.Sprintf("FuelGrid daily station-close digest — %s", now.Format("2006-01-02"))
		body := "Daily station-close digest for " + now.Format("Monday, 02 Jan 2006") + "\n\n" + lines
		if err := d.broadcast(ctx, subject, body); err != nil {
			return "", err
		}
		return fmt.Sprintf("sent daily digest stations=%d recipients=%d", stations, len(d.Recipients)), nil
	}
}

// monthlyPnLJob returns the once-per-calendar-month company P&L summary. It
// emails the gross/net revenue and expenses for the prior completed month (or
// the current month-to-date when run early) to the configured recipients. Like
// the daily job, the SendHour gate and a ledger guard (a prior successful run
// this month) collapse repeated ticks to a single monthly send.
func monthlyPnLJob(d ReportDeps) JobFunc {
	const jobName = "report_monthly_pnl"
	return func(ctx context.Context) (string, error) {
		if !d.configured() {
			return "skipped: no recipients or SMTP unconfigured", nil
		}
		now := d.clock()
		if now.Hour() < d.SendHour {
			return fmt.Sprintf("skipped: before send hour %02d:00 (now %02d:00)", d.SendHour, now.Hour()), nil
		}
		sent, err := d.sentSince(ctx, jobName, startOfMonth(now))
		if err != nil {
			return "", fmt.Errorf("check last send: %w", err)
		}
		if sent {
			return "skipped: already sent this month", nil
		}

		// Report the prior completed calendar month: when the ticker first crosses
		// the send hour on day 1 the prior month is fully closed, which is the
		// figure operators expect in a "monthly P&L" mail.
		from, to := priorMonth(now)
		body, tenants, err := d.monthlyPnLBody(ctx, from, to)
		if err != nil {
			return "", err
		}
		subject := fmt.Sprintf("FuelGrid monthly P&L — %s", from.Format("January 2006"))
		header := fmt.Sprintf("Monthly profit & loss for %s (%s to %s)\n\n",
			from.Format("January 2006"), from.Format("2006-01-02"), to.Format("2006-01-02"))
		if err := d.broadcast(ctx, subject, header+body); err != nil {
			return "", err
		}
		return fmt.Sprintf("sent monthly P&L tenants=%d recipients=%d period=%s", tenants, len(d.Recipients), from.Format("2006-01")), nil
	}
}

// dailyDigestBody assembles the per-station summary block for every active
// tenant's most recent business day. It reads the rolled-up revenue_days row
// (gross/net/cash variance), the day's metered litres, the open-exception
// count, and the day's net stock adjustment (the dip-vs-book reconciliation
// movements) — all as exact decimal strings computed in SQL (never Go float64).
// Returns the rendered text and the number of station-days included.
func (d ReportDeps) dailyDigestBody(ctx context.Context) (string, int, error) {
	// One cross-tenant query: for each active tenant's stations, take the latest
	// business date present in revenue_days and roll up the digest figures. Each
	// numeric is cast ::text so money/litres stay exact decimal strings.
	rows, err := d.Pool.Query(ctx, `
		WITH latest AS (
		    SELECT rd.tenant_id, rd.station_id, MAX(rd.business_date) AS business_date
		    FROM revenue_days rd
		    JOIN tenants t ON t.id = rd.tenant_id AND t.status = 'active'
		    GROUP BY rd.tenant_id, rd.station_id
		)
		SELECT
		    t.name AS tenant_name,
		    s.name AS station_name,
		    rd.business_date,
		    rd.gross_revenue::text,
		    rd.net_revenue::text,
		    rd.cash_variance::text,
		    COALESCE((
		        SELECT SUM(sa.litres)
		        FROM sales sa
		        WHERE sa.tenant_id = rd.tenant_id
		          AND sa.station_id = rd.station_id
		          AND sa.operating_day_id = rd.operating_day_id
		    ), 0)::text AS litres_sold,
		    COALESCE((
		        SELECT COUNT(*)
		        FROM shift_exceptions se
		        JOIN shifts sh ON sh.id = se.shift_id AND sh.tenant_id = se.tenant_id
		        WHERE se.tenant_id = rd.tenant_id
		          AND sh.operating_day_id = rd.operating_day_id
		          AND se.status = 'open'
		    ), 0) AS open_exceptions,
		    COALESCE((
		        SELECT SUM(sm.litres)
		        FROM stock_movements sm
		        JOIN tanks tk ON tk.id = sm.tank_id AND tk.tenant_id = sm.tenant_id
		        WHERE sm.tenant_id = rd.tenant_id
		          AND tk.station_id = rd.station_id
		          AND sm.movement_type = 'adjustment'
		          AND sm.status = 'posted'
		          AND sm.created_at::date = rd.business_date
		    ), 0)::text AS stock_variance_litres
		FROM latest l
		JOIN revenue_days rd ON rd.tenant_id = l.tenant_id
		    AND rd.station_id = l.station_id AND rd.business_date = l.business_date
		JOIN tenants t ON t.id = rd.tenant_id
		JOIN stations s ON s.id = rd.station_id AND s.tenant_id = rd.tenant_id
		ORDER BY t.name, s.name
	`)
	if err != nil {
		return "", 0, fmt.Errorf("query daily digest: %w", err)
	}
	defer rows.Close()

	var b strings.Builder
	var count int
	var curTenant string
	for rows.Next() {
		var (
			tenantName, stationName               string
			businessDate                          time.Time
			gross, net, cashVar, litres, stockVar string
			openExceptions                        int
		)
		if err := rows.Scan(&tenantName, &stationName, &businessDate,
			&gross, &net, &cashVar, &litres, &openExceptions, &stockVar); err != nil {
			return "", 0, fmt.Errorf("scan daily digest row: %w", err)
		}
		if tenantName != curTenant {
			if curTenant != "" {
				b.WriteString("\n")
			}
			b.WriteString("== " + tenantName + " ==\n")
			curTenant = tenantName
		}
		fmt.Fprintf(&b,
			"  %s (%s)\n"+
				"    Gross revenue : %s\n"+
				"    Net revenue   : %s\n"+
				"    Litres sold   : %s\n"+
				"    Cash variance : %s\n"+
				"    Stock variance: %s litres\n"+
				"    Open exceptions: %d\n",
			stationName, businessDate.Format("2006-01-02"),
			gross, net, litres, cashVar, stockVar, openExceptions)
		count++
	}
	if err := rows.Err(); err != nil {
		return "", 0, fmt.Errorf("iterate daily digest rows: %w", err)
	}
	if count == 0 {
		b.WriteString("No station-close data available yet.\n")
	}
	return b.String(), count, nil
}

// monthlyPnLBody renders the company profit & loss for every active tenant over
// [from, to], reusing the existing accounting.IncomeStatement read so the
// figures match what the finance pages report. Returns the rendered text and
// the number of tenants included.
func (d ReportDeps) monthlyPnLBody(ctx context.Context, from, to time.Time) (string, int, error) {
	tenants, err := d.activeTenants(ctx)
	if err != nil {
		return "", 0, fmt.Errorf("list tenants: %w", err)
	}
	var b strings.Builder
	var count int
	for _, t := range tenants {
		is, err := d.Accounting.IncomeStatement(ctx, t.id, from, to)
		if err != nil {
			// Per-tenant isolation: a tenant whose chart isn't set up shouldn't
			// block the rest of the company P&L mail.
			d.log().Warn("report_monthly_pnl: income statement", "tenant_id", t.id, "error", err)
			continue
		}
		fmt.Fprintf(&b,
			"== %s ==\n"+
				"    Revenue (gross): %s\n"+
				"    Expenses       : %s\n"+
				"    Net profit     : %s\n\n",
			t.name, is.Revenue, is.Expenses, is.NetProfit)
		count++
	}
	if count == 0 {
		b.WriteString("No P&L data available for this period.\n")
	}
	return b.String(), count, nil
}

type tenantRow struct {
	id   uuid.UUID
	name string
}

// activeTenants lists active tenants (id + name) for the report bodies.
func (d ReportDeps) activeTenants(ctx context.Context) ([]tenantRow, error) {
	rows, err := d.Pool.Query(ctx, `SELECT id, name FROM tenants WHERE status = 'active' ORDER BY name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []tenantRow
	for rows.Next() {
		var r tenantRow
		if err := rows.Scan(&r.id, &r.name); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// broadcast sends one message per recipient, best-effort: a per-recipient
// failure is logged but the job still reports success (email is best-effort at
// this boundary, like every other send in the codebase) — except that if EVERY
// recipient fails we surface an error so the run is marked failed and retried
// next tick (and, crucially, the ledger guard does not suppress the next
// attempt).
func (d ReportDeps) broadcast(ctx context.Context, subject, body string) error {
	var failures int
	for _, to := range d.Recipients {
		if err := d.Email.Send(ctx, email.Message{To: to, Subject: subject, Body: body}); err != nil {
			failures++
			d.log().Warn("report digest: send failed", "to", to, "subject", subject, "error", err)
		}
	}
	if failures == len(d.Recipients) {
		return fmt.Errorf("all %d recipients failed", failures)
	}
	return nil
}

// sentSince reports whether the named job has already run (or is mid-run) within
// the current period — the once-per-period guard. A sub-day interval ticker
// fires many times, but only the first tick past the send hour finds no prior
// run and actually sends.
//
// IDEMPOTENCY / DOUBLE-DELIVERY: the guard matches status IN ('success',
// 'running'), NOT just 'success'. The ledger row is written 'running' at the
// start of the tick (scheduler.ledgerStart), the body sends every email, and
// only THEN is the row flipped to 'success' (scheduler.ledgerFinish). If a
// replica sends the whole digest and crashes (OOM/SIGKILL/deploy) before the
// 'success' write, the row is stuck 'running'. Matching 'success' only would let
// the next tick re-send the entire financial digest to every recipient. By also
// treating an in-period 'running' row as "already handled" we fail closed: a
// crash after sending suppresses the re-send for the rest of the period (the next
// period sends normally). This trades a possible single missed digest (if the
// crash happened BEFORE any send) for never double-delivering financial data —
// the correct bias for an at-most-once digest. A 'failure' row is intentionally
// NOT matched, so a run where every recipient failed still retries next tick.
//
// A deployment without the job_runs table (migration 0079 not applied) degrades
// to "never sent" — the SendHour gate alone then bounds sends to once per (hour
// the ticker fires), so the operator still isn't spammed every tick.
func (d ReportDeps) sentSince(ctx context.Context, jobName string, since time.Time) (bool, error) {
	// Exclude this tick's own 'running' row (scheduler.execute inserts it before
	// the body runs and threads its id on the context). Without this, matching
	// 'running' would make the job find the row it just wrote and skip itself,
	// so nothing would ever send. selfID is the empty string when no ledger row
	// was threaded (table missing / insert failed); $3 = '' then excludes nothing.
	selfID, _ := runIDFromContext(ctx)
	var exists bool
	err := d.Pool.QueryRow(ctx, `
		SELECT EXISTS (
		    SELECT 1 FROM job_runs
		    WHERE job_name = $1
		      AND status IN ('success', 'running')
		      AND started_at >= $2
		      AND ($3 = '' OR id <> $3::uuid)
		)`, jobName, since, selfID).Scan(&exists)
	if err != nil {
		if isMissingLedger(err) {
			return false, nil
		}
		return false, err
	}
	return exists, nil
}

// startOfDay truncates t to local midnight.
func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// startOfMonth truncates t to the first instant of its calendar month.
func startOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
}

// priorMonth returns the [first, last-instant] bounds of the calendar month
// immediately before t's month, used as the P&L reporting period.
func priorMonth(t time.Time) (from, to time.Time) {
	from = startOfMonth(t).AddDate(0, -1, 0)
	to = startOfMonth(t).Add(-time.Second)
	return from, to
}
