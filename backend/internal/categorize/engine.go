// Package categorize assigns spending categories to transactions using
// user-defined rules first, then built-in merchant keywords, then fuzzy
// matching to absorb the abbreviations banks love ("AMZN MKTPLACE").
package categorize

import (
	"strings"
	"unicode"
)

const Uncategorized = "Uncategorized"

// Categories is the canonical list offered in the UI.
var Categories = []string{
	"Groceries", "Dining", "Entertainment", "Utilities", "Transport",
	"Shopping", "Health", "Housing", "Income", "Fees & Charges",
	"Subscriptions", "Travel", "Cash", Uncategorized,
}

// defaultKeywords maps a normalized merchant fragment to a category.
var defaultKeywords = map[string]string{
	// Groceries
	"TESCO": "Groceries", "SAINSBURY": "Groceries", "ASDA": "Groceries",
	"ALDI": "Groceries", "LIDL": "Groceries", "MORRISONS": "Groceries",
	"WAITROSE": "Groceries", "WHOLE FOODS": "Groceries", "KROGER": "Groceries",
	"WALMART": "Groceries", "SAFEWAY": "Groceries", "TRADER JOE": "Groceries",
	"COOP": "Groceries", "COSTCO": "Groceries",
	// Dining
	"MCDONALD": "Dining", "STARBUCKS": "Dining", "COSTA": "Dining",
	"PRET A MANGER": "Dining", "NANDOS": "Dining", "KFC": "Dining",
	"SUBWAY": "Dining", "DOMINOS": "Dining", "PIZZA": "Dining",
	"DELIVEROO": "Dining", "JUST EAT": "Dining", "UBER EATS": "Dining",
	"DOORDASH": "Dining", "GRUBHUB": "Dining", "GREGGS": "Dining",
	"RESTAURANT": "Dining", "CAFE": "Dining",
	// Entertainment
	"CINEMA": "Entertainment", "ODEON": "Entertainment", "VUE": "Entertainment",
	"AMC": "Entertainment", "TICKETMASTER": "Entertainment",
	"STEAM": "Entertainment", "PLAYSTATION": "Entertainment",
	"NINTENDO": "Entertainment", "XBOX": "Entertainment",
	// Subscriptions
	"NETFLIX": "Subscriptions", "SPOTIFY": "Subscriptions",
	"DISNEY": "Subscriptions", "PRIME VIDEO": "Subscriptions",
	"APPLE.COM/BILL": "Subscriptions", "AUDIBLE": "Subscriptions",
	"YOUTUBE PREMIUM": "Subscriptions", "HULU": "Subscriptions",
	"NOW TV": "Subscriptions",
	// Utilities
	"BRITISH GAS": "Utilities", "EDF": "Utilities", "OCTOPUS ENERGY": "Utilities",
	"EON": "Utilities", "THAMES WATER": "Utilities", "SEVERN TRENT": "Utilities",
	"BT GROUP": "Utilities", "VIRGIN MEDIA": "Utilities", "SKY": "Utilities",
	"VODAFONE": "Utilities", "O2": "Utilities", "EE LIMITED": "Utilities",
	"THREE": "Utilities", "VERIZON": "Utilities", "COMCAST": "Utilities",
	"AT&T": "Utilities", "PG&E": "Utilities",
	// Transport
	"UBER": "Transport", "LYFT": "Transport", "TFL": "Transport",
	"TRAINLINE": "Transport", "NATIONAL RAIL": "Transport",
	"SHELL": "Transport", "BP ": "Transport", "ESSO": "Transport",
	"TEXACO": "Transport", "CHEVRON": "Transport", "PARKING": "Transport",
	// Shopping
	"AMAZON": "Shopping", "AMZN": "Shopping", "EBAY": "Shopping",
	"ETSY": "Shopping", "ARGOS": "Shopping", "JOHN LEWIS": "Shopping",
	"IKEA": "Shopping", "TARGET": "Shopping", "H&M": "Shopping",
	"ZARA": "Shopping", "NEXT RETAIL": "Shopping", "PRIMARK": "Shopping",
	"CURRYS": "Shopping", "BEST BUY": "Shopping",
	// Health
	"BOOTS": "Health", "SUPERDRUG": "Health", "PHARMACY": "Health",
	"CVS": "Health", "WALGREENS": "Health", "GYM": "Health",
	"PUREGYM": "Health", "NUFFIELD": "Health", "DENTAL": "Health",
	// Housing
	"RENT": "Housing", "MORTGAGE": "Housing", "COUNCIL TAX": "Housing",
	// Income
	"SALARY": "Income", "PAYROLL": "Income", "HMRC": "Income",
	"DIVIDEND": "Income", "INTEREST PAID": "Income",
	// Fees
	"OVERDRAFT": "Fees & Charges", "BANK FEE": "Fees & Charges",
	"ATM FEE": "Fees & Charges", "INTEREST CHARGED": "Fees & Charges",
	"LATE FEE": "Fees & Charges",
	// Travel
	"AIRBNB": "Travel", "BOOKING.COM": "Travel", "EXPEDIA": "Travel",
	"RYANAIR": "Travel", "EASYJET": "Travel", "BRITISH AIRWAYS": "Travel",
	"HOTEL": "Travel", "HILTON": "Travel", "MARRIOTT": "Travel",
	// Cash
	"ATM WITHDRAWAL": "Cash", "CASH WITHDRAWAL": "Cash", "CASHPOINT": "Cash",
}

type Rule struct {
	Pattern  string
	Category string
}

type Engine struct {
	userRules []Rule // normalized patterns, checked before defaults
}

func NewEngine(userRules []Rule) *Engine {
	normalized := make([]Rule, 0, len(userRules))
	for _, r := range userRules {
		normalized = append(normalized, Rule{Pattern: Normalize(r.Pattern), Category: r.Category})
	}
	return &Engine{userRules: normalized}
}

// Normalize uppercases and collapses everything that is not a letter, digit,
// '&', '.', '/' into single spaces, so "AMZN*Mktplace-0442" → "AMZN MKTPLACE 0442".
func Normalize(s string) string {
	var b strings.Builder
	lastSpace := true
	for _, r := range strings.ToUpper(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '&' || r == '.' || r == '/':
			b.WriteRune(r)
			lastSpace = false
		default:
			if !lastSpace {
				b.WriteRune(' ')
				lastSpace = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// Categorize returns the category for a raw transaction description.
func (e *Engine) Categorize(description string) string {
	norm := Normalize(description)
	if norm == "" {
		return Uncategorized
	}
	// 1. User rules win.
	for _, r := range e.userRules {
		if r.Pattern != "" && strings.Contains(norm, r.Pattern) {
			return r.Category
		}
	}
	// 2. Built-in keyword table.
	for kw, cat := range defaultKeywords {
		if strings.Contains(norm, kw) {
			return cat
		}
	}
	// 3. Fuzzy: compare each description token against keyword tokens to
	// absorb minor misspellings and OCR noise ("STARBUKS", "TESC0").
	tokens := strings.Fields(norm)
	for kw, cat := range defaultKeywords {
		kwFirst := strings.Fields(kw)[0]
		if len(kwFirst) < 4 {
			continue // too short to fuzzy-match safely
		}
		for _, tok := range tokens {
			if len(tok) < 4 {
				continue
			}
			if levenshtein(tok, kwFirst) <= fuzzThreshold(kwFirst) {
				return cat
			}
		}
	}
	return Uncategorized
}

func fuzzThreshold(word string) int {
	if len(word) >= 8 {
		return 2
	}
	return 1
}

func levenshtein(a, b string) int {
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, min(curr[j-1]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}
