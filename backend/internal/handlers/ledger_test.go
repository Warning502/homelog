package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/sgiraz/homelog/internal/middleware"
	"github.com/sgiraz/homelog/internal/models"
	"github.com/sgiraz/homelog/internal/testutil"
)

// setupLedger reuses the money fixture (alice + bob + virtual carol in one
// property) and wires the ledger routes next to the expense routes.
func setupLedger(t *testing.T) (*moneyFixture, *gin.Engine) {
	t.Helper()
	f := setupMoneyFixture(t)
	l := NewLedgerHandler(f.db)
	exp := NewExpenseHandler(f.db)
	r := gin.New()
	p := r.Group("")
	p.Use(middleware.AuthRequired())
	p.GET("/properties/:id/accounts", l.ListAccounts)
	p.POST("/properties/:id/accounts", l.CreateAccount)
	p.GET("/properties/:id/transactions", l.ListTransactions)
	p.POST("/properties/:id/transactions", l.CreateTransaction)
	p.GET("/properties/:id/ledger/summary", l.Summary)
	p.PUT("/accounts/:id", l.UpdateAccount)
	p.DELETE("/accounts/:id", l.DeleteAccount)
	p.PUT("/transactions/:id", l.UpdateTransaction)
	p.DELETE("/transactions/:id", l.DeleteTransaction)
	p.POST("/expenses", exp.Create)
	p.PUT("/expenses/:id", exp.Update)
	return f, r
}

func decode[T any](t *testing.T, body []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return v
}

func createAccount(t *testing.T, r http.Handler, f *moneyFixture, body map[string]any) uint {
	t.Helper()
	rec := doJSON(t, r, http.MethodPost, fmt.Sprintf("/properties/%d/accounts", f.prop.ID), f.aliceTok, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create account: %d %s", rec.Code, rec.Body.String())
	}
	return decode[AccountWithBalance](t, rec.Body.Bytes()).ID
}

func balances(t *testing.T, r http.Handler, f *moneyFixture) map[uint]float64 {
	t.Helper()
	rec := doJSON(t, r, http.MethodGet, fmt.Sprintf("/properties/%d/accounts", f.prop.ID), f.bobTok, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list accounts: %d %s", rec.Code, rec.Body.String())
	}
	out := map[uint]float64{}
	for _, a := range decode[[]AccountWithBalance](t, rec.Body.Bytes()) {
		out[a.ID] = a.Balance
	}
	return out
}

func TestLedger_BalancesFollowIncomeTransferAndExpense(t *testing.T) {
	f, r := setupLedger(t)
	bank := createAccount(t, r, f, map[string]any{"name": "Salary", "bank": "KBank", "type": "savings", "account_number": "0823456789", "opening_balance": 1000})
	savings := createAccount(t, r, f, map[string]any{"name": "School fund", "type": "fixed"})

	today := time.Now().Format("2006-01-02")
	post := func(path string, body map[string]any) {
		t.Helper()
		rec := doJSON(t, r, http.MethodPost, path, f.aliceTok, body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("POST %s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
	tx := fmt.Sprintf("/properties/%d/transactions", f.prop.ID)
	post(tx, map[string]any{"type": "income", "account_id": bank, "amount": 50000, "date": today, "category": "salary", "for_member_id": f.mAlice.ID})
	post(tx, map[string]any{"type": "transfer", "account_id": bank, "to_account_id": savings, "amount": 10000, "date": today})
	post("/expenses", map[string]any{
		"amount": 4500, "description": "Diving course", "date": today, "category_id": f.casaCatID,
		"property_id": f.prop.ID, "paid_by_member_id": f.mAlice.ID, "account_id": bank, "for_member_id": f.mCarol.ID,
	})

	b := balances(t, r, f)
	if b[bank] != 1000+50000-10000-4500 || b[savings] != 10000 {
		t.Fatalf("balances = %v, want bank 36500 and savings 10000", b)
	}

	// Summary: the expense is attributed to Carol, the income is counted.
	rec := doJSON(t, r, http.MethodGet, fmt.Sprintf("/properties/%d/ledger/summary?month=%s", f.prop.ID, today[:7]), f.bobTok, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("summary: %d %s", rec.Code, rec.Body.String())
	}
	sum := decode[struct {
		Income   float64          `json:"income"`
		Expense  float64          `json:"expense"`
		ByMember []MemberSpending `json:"by_member"`
	}](t, rec.Body.Bytes())
	if sum.Income != 50000 || sum.Expense != 4500 || len(sum.ByMember) != 1 || sum.ByMember[0].MemberID != f.mCarol.ID {
		t.Fatalf("summary = %+v", sum)
	}
}

func TestLedger_RejectsBadReferences(t *testing.T) {
	f, r := setupLedger(t)
	a := createAccount(t, r, f, map[string]any{"name": "Cash", "type": "cash"})
	today := time.Now().Format("2006-01-02")
	tx := fmt.Sprintf("/properties/%d/transactions", f.prop.ID)

	cases := []struct {
		name string
		body map[string]any
		code string
	}{
		{"same account transfer", map[string]any{"type": "transfer", "account_id": a, "to_account_id": a, "amount": 1, "date": today}, "transfer_same_account"},
		{"unknown account", map[string]any{"type": "income", "account_id": 9999, "amount": 1, "date": today}, "invalid_account"},
		{"bad type", map[string]any{"type": "expense", "account_id": a, "amount": 1, "date": today}, "invalid_transaction_type"},
		{"member of nobody", map[string]any{"type": "income", "account_id": a, "amount": 1, "date": today, "for_member_id": 9999}, "invalid_member"},
	}
	for _, tc := range cases {
		rec := doJSON(t, r, http.MethodPost, tx, f.aliceTok, tc.body)
		got := decode[map[string]any](t, rec.Body.Bytes())["error_code"]
		if rec.Code != http.StatusBadRequest || got != tc.code {
			t.Errorf("%s: %d %v, want 400 %s", tc.name, rec.Code, got, tc.code)
		}
	}
}

func TestLedger_NonMemberIsForbidden(t *testing.T) {
	f, r := setupLedger(t)
	outsider := &models.User{Email: "eve@example.com", PasswordHash: "x", Name: "Eve", Role: "user", IsActive: true}
	mustCreate(t, f.db, outsider)
	tok := testutil.SignToken(t, outsider)
	for _, path := range []string{"accounts", "transactions", "ledger/summary"} {
		rec := doJSON(t, r, http.MethodGet, fmt.Sprintf("/properties/%d/%s", f.prop.ID, path), tok, nil)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", path, rec.Code)
		}
	}
	acct := createAccount(t, r, f, map[string]any{"name": "Private"})
	if rec := doJSON(t, r, http.MethodDelete, fmt.Sprintf("/accounts/%d", acct), tok, nil); rec.Code != http.StatusNotFound {
		t.Errorf("outsider delete: status %d, want 404", rec.Code)
	}
}

func TestLedger_AccountInUseCannotBeDeleted(t *testing.T) {
	f, r := setupLedger(t)
	a := createAccount(t, r, f, map[string]any{"name": "Card", "type": "credit_card"})
	rec := doJSON(t, r, http.MethodPost, fmt.Sprintf("/properties/%d/transactions", f.prop.ID), f.aliceTok,
		map[string]any{"type": "income", "account_id": a, "amount": 10, "date": time.Now().Format("2006-01-02")})
	if rec.Code != http.StatusCreated {
		t.Fatalf("income: %d", rec.Code)
	}
	rec = doJSON(t, r, http.MethodDelete, fmt.Sprintf("/accounts/%d", a), f.aliceTok, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete in-use account: %d, want 409", rec.Code)
	}
}

func TestExpense_AccountAndForMemberAreEditableAndClearable(t *testing.T) {
	f, r := setupLedger(t)
	a := createAccount(t, r, f, map[string]any{"name": "Bank"})
	rec := doJSON(t, r, http.MethodPost, "/expenses", f.aliceTok, map[string]any{
		"amount": 100, "description": "Fees", "date": time.Now().Format("2006-01-02"), "category_id": f.casaCatID,
		"property_id": f.prop.ID, "paid_by_member_id": f.mAlice.ID,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	id := uint(decode[map[string]any](t, rec.Body.Bytes())["id"].(float64))

	rec = doJSON(t, r, http.MethodPut, fmt.Sprintf("/expenses/%d", id), f.aliceTok, map[string]any{"account_id": a, "for_member_id": f.mCarol.ID})
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	var e models.Expense
	f.db.First(&e, id)
	if e.AccountID == nil || *e.AccountID != a || e.ForMemberID == nil || *e.ForMemberID != f.mCarol.ID {
		t.Fatalf("after update: account=%v for=%v", e.AccountID, e.ForMemberID)
	}

	rec = doJSON(t, r, http.MethodPut, fmt.Sprintf("/expenses/%d", id), f.aliceTok, map[string]any{"account_id": 0, "for_member_id": 0})
	if rec.Code != http.StatusOK {
		t.Fatalf("clear: %d %s", rec.Code, rec.Body.String())
	}
	e = models.Expense{}
	f.db.First(&e, id)
	if e.AccountID != nil || e.ForMemberID != nil {
		t.Fatalf("after clear: account=%v for=%v", e.AccountID, e.ForMemberID)
	}
}
