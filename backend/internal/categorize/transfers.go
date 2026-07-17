package categorize

import (
	"math"
	"sort"
	"strings"
	"time"
)

const Transfers = "Transfers"

// TransferWindow is how far apart the two sides of a transfer may settle.
const TransferWindow = 3 * 24 * time.Hour

// TxnRef is the slice of a transaction the transfer matcher needs.
type TxnRef struct {
	ID       string
	UploadID string
	Date     time.Time
	Amount   float64
	Credit   bool
	Category string
	Merchant string
}

// RuledByUser reports whether a user rule decided this transaction's
// category. Such transactions are excluded from pair matching so the
// heuristic never overrides an explicit user decision.
func (t TxnRef) RuledByUser(rules []Rule) bool {
	for _, r := range rules {
		if r.Pattern != "" && strings.Contains(t.Merchant, r.Pattern) && r.Category != Transfers {
			return true
		}
	}
	return false
}

// MatchTransfers pairs a debit with a credit of the same amount from a
// different statement dated within TransferWindow — money leaving one of the
// user's accounts and arriving in another. Same-statement pairs are skipped
// because a debit and matching credit on one account is usually a refund.
// Each transaction joins at most one pair, closest dates first. Returns the
// IDs of paired transactions not already categorized as Transfers.
func MatchTransfers(txns []TxnRef) []string {
	byAmount := map[int64][]TxnRef{}
	for _, t := range txns {
		cents := int64(math.Round(t.Amount * 100))
		byAmount[cents] = append(byAmount[cents], t)
	}

	var ids []string
	for _, group := range byAmount {
		var debits, credits []TxnRef
		for _, t := range group {
			if t.Credit {
				credits = append(credits, t)
			} else {
				debits = append(debits, t)
			}
		}
		if len(debits) == 0 || len(credits) == 0 {
			continue
		}

		type pair struct {
			d, c int
			gap  time.Duration
		}
		var pairs []pair
		for i, d := range debits {
			for j, c := range credits {
				if d.UploadID == c.UploadID {
					continue
				}
				gap := d.Date.Sub(c.Date).Abs()
				if gap <= TransferWindow {
					pairs = append(pairs, pair{i, j, gap})
				}
			}
		}
		sort.Slice(pairs, func(a, b int) bool {
			if pairs[a].gap != pairs[b].gap {
				return pairs[a].gap < pairs[b].gap
			}
			// Deterministic order among equal gaps.
			if pairs[a].d != pairs[b].d {
				return pairs[a].d < pairs[b].d
			}
			return pairs[a].c < pairs[b].c
		})

		usedD, usedC := map[int]bool{}, map[int]bool{}
		for _, p := range pairs {
			if usedD[p.d] || usedC[p.c] {
				continue
			}
			usedD[p.d], usedC[p.c] = true, true
			for _, t := range [2]TxnRef{debits[p.d], credits[p.c]} {
				if t.Category != Transfers {
					ids = append(ids, t.ID)
				}
			}
		}
	}
	return ids
}
