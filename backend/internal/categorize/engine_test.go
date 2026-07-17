package categorize

import "testing"

func TestCategorize(t *testing.T) {
	engine := NewEngine([]Rule{{Pattern: "ACME COWORKING", Category: "Housing"}})

	cases := []struct {
		desc string
		want string
	}{
		{"TESCO STORES 3297", "Groceries"},
		{"AMZN MKTPLACE*0442", "Shopping"},
		{"Netflix.com", "Subscriptions"},
		{"UBER *TRIP HELP.UBER.COM", "Transport"},
		{"STARBUKS COFFEE 122", "Dining"}, // fuzzy: one edit from STARBUCKS
		{"ACME Coworking Ltd", "Housing"}, // user rule wins
		{"ZZZ UNKNOWN VENDOR", Uncategorized},
		{"TRANSFER TO SAVINGS", Transfers},
		{"WEST AR", Uncategorized}, // must not fuzzy-match BEST BUY
		{"West D T ALLAN WEST", Uncategorized},
	}
	for _, c := range cases {
		if got := engine.Categorize(c.desc); got != c.want {
			t.Errorf("Categorize(%q) = %q, want %q", c.desc, got, c.want)
		}
	}
}

func TestCategorizeShortKeywordsDontMatchInsideWords(t *testing.T) {
	engine := NewEngine(nil)
	for i := 0; i < 100; i++ {
		if got := engine.Categorize("Netflix.com"); got != "Subscriptions" {
			t.Fatalf("Categorize(Netflix.com) = %q, want %q", got, "Subscriptions")
		}
	}
}

func TestNormalize(t *testing.T) {
	if got := Normalize("AMZN*Mktplace-0442  "); got != "AMZN MKTPLACE 0442" {
		t.Errorf("Normalize = %q", got)
	}
}
