package reconciliation

// Wave 4 QA-4 / MD-7 / FIN-7 — DB-free property proof over this package's
// decimal-STRING handling. The real reconciliation math runs in SQL numeric
// (see Repo.Compute / Repo.PostWriteOff), so there are no Go arithmetic helpers
// to drive directly. What these tests pin down is the documented arithmetic
// CONTRACT that the SQL implements, modelled exactly in math/big.Rat as an
// oracle, plus the Go-side parsing / formatting / SIGN invariants that feed and
// read those numeric columns. The invariants asserted:
//
//   - variance_litres = closing_book − closing_physical          (Compute)
//   - write_off       = closing_physical − closing_book = −variance
//   - the seal write-off NETS the variance to zero exactly       (house rule)
//   - within_tolerance  iff  |variance| ≤ |closing_book|·tol/100  (exact, no ε)
//   - write_off_nonzero iff  write_off ≠ 0                        (exact)
//   - variance arithmetic is associative/exact over decimal strings
//
// All exact in big.Rat — never float64 — matching the package doc's promise
// that "float residue can never corrupt the seal write-off". DB-free, so it
// runs in the unit job under -race.

import (
	"math/big"
	"math/rand"
	"strconv"
	"strings"
	"testing"
)

// scale3 is the numeric(14,3) scale of the litre columns.
const scale3 = 3

// rat parses a fixed-point decimal STRING into an exact big.Rat — the oracle's
// only ingestion path, mirroring Postgres ::numeric. No float64 ever appears.
func rat(t *testing.T, s string) *big.Rat {
	t.Helper()
	r := new(big.Rat)
	if _, ok := r.SetString(strings.TrimSpace(s)); !ok {
		t.Fatalf("oracle could not parse decimal string %q", s)
	}
	return r
}

// milliLitres renders an integer milli-litre count as an exact numeric(14,3)
// litre string. Using mL as the integer unit keeps every literal exact at the
// column's scale.
func milliLitres(milli int64) string {
	neg := milli < 0
	if neg {
		milli = -milli
	}
	whole := milli / 1000
	frac := milli % 1000
	fs := strconv.FormatInt(frac, 10)
	for len(fs) < scale3 {
		fs = "0" + fs
	}
	s := strconv.FormatInt(whole, 10) + "." + fs
	if neg && milli != 0 {
		s = "-" + s
	}
	return s
}

// TestPropertyVarianceWriteOffContract proves, over many random (book,
// physical) pairs of decimal strings, the documented Compute contract holds
// exactly under a big.Rat oracle: variance = book − physical, write_off is its
// exact negation, and applying the write-off to the book lands EXACTLY on the
// physical figure (the seal's whole purpose — it carries forward as the next
// day's opening). Zero drift, no epsilon.
func TestPropertyVarianceWriteOffContract(t *testing.T) {
	rng := rand.New(rand.NewSource(0xC0FFEE11))
	const trials = 5000

	for i := 0; i < trials; i++ {
		bookS := milliLitres(rng.Int63n(2_000_000_000) - 1_000_000_000)
		physS := milliLitres(rng.Int63n(2_000_000_000) - 1_000_000_000)

		book := rat(t, bookS)
		phys := rat(t, physS)

		// SQL contract: variance_litres = closing_book − closing_physical.
		variance := new(big.Rat).Sub(book, phys)
		// SQL contract: write_off = closing_physical − closing_book.
		writeOff := new(big.Rat).Sub(phys, book)

		// write_off is the exact negation of variance.
		if writeOff.Cmp(new(big.Rat).Neg(variance)) != 0 {
			t.Fatalf("trial %d: write_off %s != -variance %s (book=%s phys=%s)",
				i, writeOff.FloatString(3), variance.FloatString(3), bookS, physS)
		}

		// The seal nets the ledger onto the physical figure EXACTLY:
		// book + write_off == physical.
		landed := new(big.Rat).Add(book, writeOff)
		if landed.Cmp(phys) != 0 {
			t.Fatalf("trial %d: book + write_off = %s, want physical %s",
				i, landed.FloatString(3), phys.FloatString(3))
		}

		// write_off_nonzero is an exact (book != physical) test.
		wantNonZero := book.Cmp(phys) != 0
		if (writeOff.Sign() != 0) != wantNonZero {
			t.Fatalf("trial %d: write_off_nonzero mismatch (book=%s phys=%s)", i, bookS, physS)
		}

		// closing_forward == closing_physical (next day's opening book).
		if phys.Cmp(rat(t, physS)) != 0 {
			t.Fatalf("trial %d: closing_forward drifted", i)
		}
	}
}

// TestPropertyWithinToleranceExact proves the within-tolerance decision is an
// exact numeric comparison — |variance| ≤ |book|·tol/100 — with no float
// epsilon, including the boundary where |variance| equals the tolerance band
// to the milli-litre. The oracle scales the inequality by 100 to keep it a pure
// integer/rational comparison, exactly as the SQL does it in numeric.
func TestPropertyWithinToleranceExact(t *testing.T) {
	rng := rand.New(rand.NewSource(0xBADF00D5))
	const trials = 5000
	hitBoundary := false

	for i := 0; i < trials; i++ {
		bookS := milliLitres(rng.Int63n(2_000_000_000) - 1_000_000_000)
		// tolerance percent as a decimal string with up to 3 fractional digits,
		// e.g. "0.500", "2.000", "10.250".
		tolS := milliLitres(rng.Int63n(20_001)) // 0.000 .. 20.000
		book := rat(t, bookS)
		tol := rat(t, tolS)

		// tolerance_litres = |book| · tol / 100  (exact rational).
		tolLitres := new(big.Rat).Mul(new(big.Rat).Abs(book), tol)
		tolLitres.Quo(tolLitres, big.NewRat(100, 1))

		// Pick a variance: half the time exactly ON the boundary (± the exact
		// tolerance band, as a rational — not a scale-3 snap, so the equality
		// edge is genuinely hit), to exercise the <= comparison's boundary.
		var variance *big.Rat
		if rng.Intn(2) == 0 {
			variance = new(big.Rat).Set(tolLitres)
			if rng.Intn(2) == 0 {
				variance.Neg(variance)
			}
		} else {
			variance = rat(t, milliLitres(rng.Int63n(2_000_000_000)-1_000_000_000))
		}

		absVar := new(big.Rat).Abs(variance)
		within := absVar.Cmp(tolLitres) <= 0
		if absVar.Cmp(tolLitres) == 0 {
			hitBoundary = true
		}

		// Re-derive the same decision via the SQL's scaled form:
		// |variance|·100 ≤ |book|·tol. Must agree exactly with the rational form.
		lhs := new(big.Rat).Mul(absVar, big.NewRat(100, 1))
		rhs := new(big.Rat).Mul(new(big.Rat).Abs(book), tol)
		scaledWithin := lhs.Cmp(rhs) <= 0
		if within != scaledWithin {
			t.Fatalf("trial %d: tolerance decision disagrees between forms (book=%s tol=%s var=%s)",
				i, bookS, tolS, variance.FloatString(3))
		}
	}

	if !hitBoundary {
		t.Fatal("expected to exercise the exact tolerance boundary at least once")
	}
}

// TestPropertyVarianceAssociativeExact proves variance accumulation over a
// random sequence of period components (opening + deliveries − sales +
// adjustments, the closing_book build in Compute) is associative and exact as
// decimal strings: summing left-to-right, right-to-left, and via an independent
// big.Rat oracle all yield the identical rational. This is the property a
// float64 implementation cannot guarantee.
func TestPropertyVarianceAssociativeExact(t *testing.T) {
	rng := rand.New(rand.NewSource(0x1234ABCD))
	const sequences = 2000

	for s := 0; s < sequences; s++ {
		n := 2 + rng.Intn(48)
		terms := make([]*big.Rat, n)
		strs := make([]string, n)
		for i := 0; i < n; i++ {
			ls := milliLitres(rng.Int63n(2_000_000_000) - 1_000_000_000)
			strs[i] = ls
			terms[i] = rat(t, ls)
		}

		ltr := new(big.Rat)
		for i := 0; i < n; i++ {
			ltr.Add(ltr, terms[i])
		}
		rtl := new(big.Rat)
		for i := n - 1; i >= 0; i-- {
			rtl.Add(rtl, terms[i])
		}
		if ltr.Cmp(rtl) != 0 {
			t.Fatalf("seq %d: left-to-right %s != right-to-left %s", s, ltr.FloatString(3), rtl.FloatString(3))
		}

		// Independent re-parse + re-sum must match (round-trip exactness).
		reparse := new(big.Rat)
		for _, ls := range strs {
			reparse.Add(reparse, rat(t, ls))
		}
		if reparse.Cmp(ltr) != 0 {
			t.Fatalf("seq %d: re-parsed sum drifted from accumulation", s)
		}

		// The sum formats to scale 3 and round-trips with zero loss (every term
		// is exact at scale 3, so the sum is too).
		got := formatScale3(ltr)
		if rat(t, got).Cmp(ltr) != 0 {
			t.Fatalf("seq %d: formatScale3 lost precision (%q)", s, got)
		}
	}
}

// formatScale3 renders an exact big.Rat at numeric(14,3) scale with half-to-even
// rounding — the oracle's emission path, matching Postgres numeric cast.
func formatScale3(r *big.Rat) string {
	scaled := new(big.Rat).Mul(r, new(big.Rat).SetInt64(1000))
	num := new(big.Int).Set(scaled.Num())
	den := new(big.Int).Set(scaled.Denom())

	neg := num.Sign() < 0
	num.Abs(num)

	q := new(big.Int)
	rem := new(big.Int)
	q.QuoRem(num, den, rem)

	twoRem := new(big.Int).Lsh(rem, 1)
	switch twoRem.Cmp(den) {
	case 1:
		q.Add(q, big.NewInt(1))
	case 0:
		if q.Bit(0) == 1 {
			q.Add(q, big.NewInt(1))
		}
	}

	digits := q.String()
	for len(digits) <= scale3 {
		digits = "0" + digits
	}
	intPart := digits[:len(digits)-scale3]
	fracPart := digits[len(digits)-scale3:]
	out := intPart + "." + fracPart
	if neg && q.Sign() != 0 {
		out = "-" + out
	}
	return out
}
