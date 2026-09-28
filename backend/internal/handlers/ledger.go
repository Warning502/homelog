package handlers

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/sgiraz/homelog/internal/apierr"
	"github.com/sgiraz/homelog/internal/middleware"
	"github.com/sgiraz/homelog/internal/models"
)

// LedgerHandler serves the household's money accounts, incomes, transfers and
// the per-member spending summary. Every endpoint is scoped to one property
// and open to any of its members.
type LedgerHandler struct {
	db *gorm.DB
}

func NewLedgerHandler(db *gorm.DB) *LedgerHandler {
	return &LedgerHandler{db: db}
}

// IncomeCategories are the income kinds the UI offers.
var IncomeCategories = map[string]bool{
	"salary": true, "bonus": true, "side_income": true, "interest": true, "gift": true, "other": true,
}

// AccountWithBalance is an account plus its derived balance.
type AccountWithBalance struct {
	models.Account
	Balance float64 `json:"balance"`
}

// propertyFromParam parses :id and checks the caller is a member of it.
func (h *LedgerHandler) propertyFromParam(c *gin.Context) (uint, uint, bool) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		apierr.Fail(c, http.StatusUnauthorized, "not_authenticated", "You are not signed in")
		return 0, 0, false
	}
	pid, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		apierr.Fail(c, http.StatusBadRequest, "invalid_property_id", "Invalid property id")
		return 0, 0, false
	}
	if !requirePropertyMember(c, h.db, userID, uint(pid)) {
		return 0, 0, false
	}
	return userID, uint(pid), true
}

// accountInProperty reports whether accountID is an account of propertyID.
func accountInProperty(db *gorm.DB, accountID, propertyID uint) bool {
	var n int64
	db.Model(&models.Account{}).Where("id = ? AND property_id = ?", accountID, propertyID).Count(&n)
	return n > 0
}

// memberInProperty reports whether memberID is a household member of propertyID.
func memberInProperty(db *gorm.DB, memberID, propertyID uint) bool {
	var n int64
	db.Model(&models.HouseholdMember{}).Where("id = ? AND property_id = ?", memberID, propertyID).Count(&n)
	return n > 0
}

// accountBalances derives the balance of every account of a property.
func accountBalances(db *gorm.DB, propertyID uint, accounts []models.Account) map[uint]float64 {
	bal := make(map[uint]float64, len(accounts))
	ids := make([]uint, 0, len(accounts))
	for _, a := range accounts {
		bal[a.ID] = a.OpeningBalance
		ids = append(ids, a.ID)
	}
	if len(ids) == 0 {
		return bal
	}
	type row struct {
		ID    uint
		Total float64
	}
	add := func(rows []row, sign float64) {
		for _, r := range rows {
			bal[r.ID] += sign * r.Total
		}
	}
	var rows []row
	db.Model(&models.MoneyTransaction{}).Select("account_id AS id, SUM(amount) AS total").
		Where("property_id = ? AND type = ? AND account_id IN ?", propertyID, "income", ids).Group("account_id").Scan(&rows)
	add(rows, 1)
	rows = nil
	db.Model(&models.MoneyTransaction{}).Select("account_id AS id, SUM(amount) AS total").
		Where("property_id = ? AND type = ? AND account_id IN ?", propertyID, "transfer", ids).Group("account_id").Scan(&rows)
	add(rows, -1)
	rows = nil
	db.Model(&models.MoneyTransaction{}).Select("to_account_id AS id, SUM(amount) AS total").
		Where("property_id = ? AND type = ? AND to_account_id IN ?", propertyID, "transfer", ids).Group("to_account_id").Scan(&rows)
	add(rows, 1)
	rows = nil
	db.Model(&models.Expense{}).Select("account_id AS id, SUM(amount) AS total").
		Where("account_id IN ?", ids).Group("account_id").Scan(&rows)
	add(rows, -1)
	for id, v := range bal {
		bal[id] = float64(int64(v*100+sign(v)*0.5)) / 100
	}
	return bal
}

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

// ---- Accounts ------------------------------------------------------------

type accountInput struct {
	Name           *string  `json:"name"`
	Bank           *string  `json:"bank"`
	Type           *string  `json:"type"`
	AccountNumber  *string  `json:"account_number"`
	OpeningBalance *float64 `json:"opening_balance"`
	OwnerMemberID  *uint    `json:"owner_member_id"` // 0 clears
	Color          *string  `json:"color"`
	IsArchived     *bool    `json:"is_archived"`
}

// apply validates and copies the provided fields onto a.
func (h *LedgerHandler) apply(c *gin.Context, propertyID uint, in accountInput, a *models.Account) bool {
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			apierr.Fail(c, http.StatusBadRequest, "account_name_required", "An account name is required")
			return false
		}
		a.Name = name
	}
	if in.Bank != nil {
		a.Bank = strings.TrimSpace(*in.Bank)
	}
	if in.Type != nil {
		if !models.AccountTypes[*in.Type] {
			apierr.Fail(c, http.StatusBadRequest, "invalid_account_type", "Invalid account type")
			return false
		}
		a.Type = *in.Type
	}
	if in.AccountNumber != nil {
		a.AccountNumber = strings.TrimSpace(*in.AccountNumber)
	}
	if in.OpeningBalance != nil {
		a.OpeningBalance = *in.OpeningBalance
	}
	if in.OwnerMemberID != nil {
		if *in.OwnerMemberID == 0 {
			a.OwnerMemberID = nil
		} else if !memberInProperty(h.db, *in.OwnerMemberID, propertyID) {
			apierr.Fail(c, http.StatusBadRequest, "invalid_member", "That person is not a member of this household")
			return false
		} else {
			id := *in.OwnerMemberID
			a.OwnerMemberID = &id
		}
	}
	if in.Color != nil {
		a.Color = *in.Color
	}
	if in.IsArchived != nil {
		a.IsArchived = *in.IsArchived
	}
	return true
}

// ListAccounts - GET /properties/:id/accounts
func (h *LedgerHandler) ListAccounts(c *gin.Context) {
	_, pid, ok := h.propertyFromParam(c)
	if !ok {
		return
	}
	var accounts []models.Account
	h.db.Where("property_id = ?", pid).Order("is_archived, id").Find(&accounts)
	bal := accountBalances(h.db, pid, accounts)
	out := make([]AccountWithBalance, 0, len(accounts))
	for _, a := range accounts {
		out = append(out, AccountWithBalance{Account: a, Balance: bal[a.ID]})
	}
	c.JSON(http.StatusOK, out)
}

// CreateAccount - POST /properties/:id/accounts
func (h *LedgerHandler) CreateAccount(c *gin.Context) {
	_, pid, ok := h.propertyFromParam(c)
	if !ok {
		return
	}
	var in accountInput
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.Fail(c, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if in.Name == nil {
		apierr.Fail(c, http.StatusBadRequest, "account_name_required", "An account name is required")
		return
	}
	a := models.Account{PropertyID: pid, Type: "savings"}
	if !h.apply(c, pid, in, &a) {
		return
	}
	if err := h.db.Create(&a).Error; err != nil {
		apierr.Fail(c, http.StatusInternalServerError, "server_error", "Failed to create account")
		return
	}
	c.JSON(http.StatusCreated, AccountWithBalance{Account: a, Balance: a.OpeningBalance})
}

// accountForUser loads an account the caller may manage.
func (h *LedgerHandler) accountForUser(c *gin.Context) (*models.Account, bool) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		apierr.Fail(c, http.StatusUnauthorized, "not_authenticated", "You are not signed in")
		return nil, false
	}
	var a models.Account
	if err := h.db.First(&a, c.Param("id")).Error; err != nil || !isPropertyMember(h.db, userID, a.PropertyID) {
		apierr.Fail(c, http.StatusNotFound, "account_not_found", "Account not found")
		return nil, false
	}
	return &a, true
}

// UpdateAccount - PUT /accounts/:id
func (h *LedgerHandler) UpdateAccount(c *gin.Context) {
	a, ok := h.accountForUser(c)
	if !ok {
		return
	}
	var in accountInput
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.Fail(c, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if !h.apply(c, a.PropertyID, in, a) {
		return
	}
	if err := h.db.Save(a).Error; err != nil {
		apierr.Fail(c, http.StatusInternalServerError, "server_error", "Failed to update account")
		return
	}
	bal := accountBalances(h.db, a.PropertyID, []models.Account{*a})
	c.JSON(http.StatusOK, AccountWithBalance{Account: *a, Balance: bal[a.ID]})
}

// DeleteAccount - DELETE /accounts/:id. Refused while any income, transfer or
// expense still points at it; archive the account instead.
func (h *LedgerHandler) DeleteAccount(c *gin.Context) {
	a, ok := h.accountForUser(c)
	if !ok {
		return
	}
	var n int64
	h.db.Model(&models.MoneyTransaction{}).Where("account_id = ? OR to_account_id = ?", a.ID, a.ID).Count(&n)
	var e int64
	h.db.Model(&models.Expense{}).Where("account_id = ?", a.ID).Count(&e)
	if n+e > 0 {
		apierr.Fail(c, http.StatusConflict, "account_in_use", "This account has transactions. Archive it instead of deleting it.")
		return
	}
	if err := h.db.Delete(a).Error; err != nil {
		apierr.Fail(c, http.StatusInternalServerError, "server_error", "Failed to delete account")
		return
	}
	c.Status(http.StatusNoContent)
}

// ---- Incomes & transfers ---------------------------------------------------

type transactionInput struct {
	Type        string  `json:"type" binding:"required"`
	AccountID   uint    `json:"account_id" binding:"required"`
	ToAccountID *uint   `json:"to_account_id"`
	Amount      float64 `json:"amount" binding:"required"`
	Date        string  `json:"date" binding:"required"`
	Description string  `json:"description"`
	Category    string  `json:"category"`
	ForMemberID *uint   `json:"for_member_id"`
}

// build validates in and returns the transaction it describes.
func (h *LedgerHandler) build(c *gin.Context, userID, pid uint, in transactionInput) (*models.MoneyTransaction, bool) {
	if in.Type != "income" && in.Type != "transfer" {
		apierr.Fail(c, http.StatusBadRequest, "invalid_transaction_type", "Type must be income or transfer")
		return nil, false
	}
	if in.Amount <= 0 {
		apierr.Fail(c, http.StatusBadRequest, "amount_must_be_positive", "The amount must be greater than zero")
		return nil, false
	}
	date, err := time.Parse("2006-01-02", in.Date)
	if err != nil {
		apierr.Fail(c, http.StatusBadRequest, "invalid_date", "Invalid date format")
		return nil, false
	}
	if !accountInProperty(h.db, in.AccountID, pid) {
		apierr.Fail(c, http.StatusBadRequest, "invalid_account", "That account does not belong to this household")
		return nil, false
	}
	t := &models.MoneyTransaction{
		PropertyID: pid, UserID: userID, Type: in.Type, AccountID: in.AccountID,
		Amount: in.Amount, Date: date, Description: strings.TrimSpace(in.Description),
	}
	if in.Type == "transfer" {
		if in.ToAccountID == nil || !accountInProperty(h.db, *in.ToAccountID, pid) {
			apierr.Fail(c, http.StatusBadRequest, "invalid_account", "That account does not belong to this household")
			return nil, false
		}
		if *in.ToAccountID == in.AccountID {
			apierr.Fail(c, http.StatusBadRequest, "transfer_same_account", "Choose two different accounts for a transfer")
			return nil, false
		}
		to := *in.ToAccountID
		t.ToAccountID = &to
		return t, true
	}
	if in.Category != "" && !IncomeCategories[in.Category] {
		apierr.Fail(c, http.StatusBadRequest, "invalid_category", "Invalid category")
		return nil, false
	}
	t.Category = in.Category
	if in.ForMemberID != nil && *in.ForMemberID != 0 {
		if !memberInProperty(h.db, *in.ForMemberID, pid) {
			apierr.Fail(c, http.StatusBadRequest, "invalid_member", "That person is not a member of this household")
			return nil, false
		}
		m := *in.ForMemberID
		t.ForMemberID = &m
	}
	return t, true
}

// ListTransactions - GET /properties/:id/transactions?month=YYYY-MM&type=income|transfer
func (h *LedgerHandler) ListTransactions(c *gin.Context) {
	_, pid, ok := h.propertyFromParam(c)
	if !ok {
		return
	}
	q := h.db.Where("property_id = ?", pid)
	if t := c.Query("type"); t != "" {
		q = q.Where("type = ?", t)
	}
	if m := c.Query("month"); m != "" {
		from, to, ok := monthRange(m)
		if !ok {
			apierr.Fail(c, http.StatusBadRequest, "invalid_date", "Invalid month, expected YYYY-MM")
			return
		}
		q = q.Where("date >= ? AND date < ?", from, to)
	}
	var out []models.MoneyTransaction
	q.Order("date DESC, id DESC").Limit(1000).Find(&out)
	c.JSON(http.StatusOK, out)
}

// CreateTransaction - POST /properties/:id/transactions
func (h *LedgerHandler) CreateTransaction(c *gin.Context) {
	userID, pid, ok := h.propertyFromParam(c)
	if !ok {
		return
	}
	var in transactionInput
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.Fail(c, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	t, ok := h.build(c, userID, pid, in)
	if !ok {
		return
	}
	if err := h.db.Create(t).Error; err != nil {
		apierr.Fail(c, http.StatusInternalServerError, "server_error", "Failed to save the transaction")
		return
	}
	c.JSON(http.StatusCreated, t)
}

func (h *LedgerHandler) transactionForUser(c *gin.Context) (*models.MoneyTransaction, uint, bool) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		apierr.Fail(c, http.StatusUnauthorized, "not_authenticated", "You are not signed in")
		return nil, 0, false
	}
	var t models.MoneyTransaction
	if err := h.db.First(&t, c.Param("id")).Error; err != nil || !isPropertyMember(h.db, userID, t.PropertyID) {
		apierr.Fail(c, http.StatusNotFound, "transaction_not_found", "Transaction not found")
		return nil, 0, false
	}
	return &t, userID, true
}

// UpdateTransaction - PUT /transactions/:id (full replacement)
func (h *LedgerHandler) UpdateTransaction(c *gin.Context) {
	old, userID, ok := h.transactionForUser(c)
	if !ok {
		return
	}
	var in transactionInput
	if err := c.ShouldBindJSON(&in); err != nil {
		apierr.Fail(c, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	t, ok := h.build(c, userID, old.PropertyID, in)
	if !ok {
		return
	}
	t.ID, t.CreatedAt, t.UserID = old.ID, old.CreatedAt, old.UserID
	if err := h.db.Save(t).Error; err != nil {
		apierr.Fail(c, http.StatusInternalServerError, "server_error", "Failed to save the transaction")
		return
	}
	c.JSON(http.StatusOK, t)
}

// DeleteTransaction - DELETE /transactions/:id
func (h *LedgerHandler) DeleteTransaction(c *gin.Context) {
	t, _, ok := h.transactionForUser(c)
	if !ok {
		return
	}
	if err := h.db.Delete(t).Error; err != nil {
		apierr.Fail(c, http.StatusInternalServerError, "server_error", "Failed to delete the transaction")
		return
	}
	c.Status(http.StatusNoContent)
}

// ---- Summary ---------------------------------------------------------------

// monthRange turns "YYYY-MM" into [first day, first day of next month).
func monthRange(m string) (time.Time, time.Time, bool) {
	from, err := time.Parse("2006-01", m)
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	return from, from.AddDate(0, 1, 0), true
}

// MemberSpending is how much was spent on one member (0 = shared household).
type MemberSpending struct {
	MemberID   uint               `json:"member_id"`
	Total      float64            `json:"total"`
	Categories []CategorySpending `json:"categories"`
}

// CategorySpending is one category's share of a member's spending.
type CategorySpending struct {
	CategoryID uint    `json:"category_id"`
	Total      float64 `json:"total"`
}

// Summary - GET /properties/:id/ledger/summary?month=YYYY-MM
// Income and spending for the month, and spending per member by category.
func (h *LedgerHandler) Summary(c *gin.Context) {
	_, pid, ok := h.propertyFromParam(c)
	if !ok {
		return
	}
	month := c.DefaultQuery("month", time.Now().Format("2006-01"))
	from, to, ok := monthRange(month)
	if !ok {
		apierr.Fail(c, http.StatusBadRequest, "invalid_date", "Invalid month, expected YYYY-MM")
		return
	}

	var income float64
	h.db.Model(&models.MoneyTransaction{}).Select("COALESCE(SUM(amount), 0)").
		Where("property_id = ? AND type = ? AND date >= ? AND date < ?", pid, "income", from, to).Scan(&income)

	type row struct {
		ForMemberID *uint
		CategoryID  uint
		Total       float64
	}
	var rows []row
	h.db.Model(&models.Expense{}).Select("for_member_id, category_id, SUM(amount) AS total").
		Where("property_id = ? AND date >= ? AND date < ? AND is_long_term_debt = ?", pid, from, to, false).
		Group("for_member_id, category_id").Scan(&rows)

	byMember := map[uint]*MemberSpending{}
	var expense float64
	for _, r := range rows {
		var mid uint
		if r.ForMemberID != nil {
			mid = *r.ForMemberID
		}
		ms := byMember[mid]
		if ms == nil {
			ms = &MemberSpending{MemberID: mid}
			byMember[mid] = ms
		}
		ms.Total += r.Total
		ms.Categories = append(ms.Categories, CategorySpending{CategoryID: r.CategoryID, Total: r.Total})
		expense += r.Total
	}
	members := make([]MemberSpending, 0, len(byMember))
	for _, ms := range byMember {
		sort.Slice(ms.Categories, func(i, j int) bool { return ms.Categories[i].Total > ms.Categories[j].Total })
		members = append(members, *ms)
	}
	sort.Slice(members, func(i, j int) bool { return members[i].Total > members[j].Total })

	c.JSON(http.StatusOK, gin.H{
		"month":     month,
		"income":    income,
		"expense":   expense,
		"by_member": members,
	})
}
