package categorize

import (
	"sort"
	"testing"
	"time"
)

func day(d int) time.Time {
	return time.Date(2026, 6, d, 0, 0, 0, 0, time.UTC)
}

func TestMatchTransfers(t *testing.T) {
	cases := []struct {
		name string
		txns []TxnRef
		want []string
	}{
		{
			name: "pairs equal amounts across uploads",
			txns: []TxnRef{
				{ID: "out", UploadID: "u1", Date: day(30), Amount: 2500, Credit: false},
				{ID: "in", UploadID: "u2", Date: day(30), Amount: 2500, Credit: true},
			},
			want: []string{"in", "out"},
		},
		{
			name: "ignores same-upload pair (refund)",
			txns: []TxnRef{
				{ID: "buy", UploadID: "u1", Date: day(10), Amount: 50, Credit: false},
				{ID: "refund", UploadID: "u1", Date: day(12), Amount: 50, Credit: true},
			},
			want: nil,
		},
		{
			name: "ignores pair outside the date window",
			txns: []TxnRef{
				{ID: "out", UploadID: "u1", Date: day(1), Amount: 100, Credit: false},
				{ID: "in", UploadID: "u2", Date: day(10), Amount: 100, Credit: true},
			},
			want: nil,
		},
		{
			name: "ignores different amounts",
			txns: []TxnRef{
				{ID: "out", UploadID: "u1", Date: day(30), Amount: 1350, Credit: false},
				{ID: "in", UploadID: "u2", Date: day(30), Amount: 2500, Credit: true},
			},
			want: nil,
		},
		{
			name: "closest date wins and each side pairs once",
			txns: []TxnRef{
				{ID: "in", UploadID: "u2", Date: day(15), Amount: 200, Credit: true},
				{ID: "far", UploadID: "u1", Date: day(13), Amount: 200, Credit: false},
				{ID: "near", UploadID: "u1", Date: day(15), Amount: 200, Credit: false},
			},
			want: []string{"in", "near"},
		},
		{
			name: "already-marked transfers are not re-reported",
			txns: []TxnRef{
				{ID: "out", UploadID: "u1", Date: day(30), Amount: 2500, Credit: false, Category: Transfers},
				{ID: "in", UploadID: "u2", Date: day(30), Amount: 2500, Credit: true, Category: "Income"},
			},
			want: []string{"in"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := MatchTransfers(c.txns)
			sort.Strings(got)
			sort.Strings(c.want)
			if len(got) != len(c.want) {
				t.Fatalf("MatchTransfers = %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("MatchTransfers = %v, want %v", got, c.want)
				}
			}
		})
	}
}
