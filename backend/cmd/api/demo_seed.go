package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/sgiraz/homelog/internal/database"
	"github.com/sgiraz/homelog/internal/middleware"
	"github.com/sgiraz/homelog/internal/models"
)

// demoMonths is how much history the public demo shows.
const demoMonths = 12

// demoAnalyticsSite is the GoatCounter site code the public demo reports to,
// from DEMO_GOATCOUNTER_SITE. Empty (the default) means no analytics at all,
// so a fork's demo never reports visitors to someone else's dashboard.
func demoAnalyticsSite() string {
	if !database.IsDemoMode() {
		return ""
	}
	return strings.TrimSpace(os.Getenv("DEMO_GOATCOUNTER_SITE"))
}

// seedThaiDemo builds the public demo dataset: a Thai household ("บ้านสุขใจ")
// that has used HomeLog for a year. The two accounts, their settings and the
// property are written directly (the shared demo password is shorter than the
// registration minimum); everything else — expenses, splits, bills and their
// auto-expenses, readings, projects, settlements and a long-term debt — goes
// through the real API handlers in-process, so balances and allocations are
// produced by the same code visitors exercise. It mirrors
// scripts/seed-sample-data.mjs, which does the same over HTTP.
func seedThaiDemo(db *gorm.DB) error {
	// The handlers log every step; an hourly reset would flood the log.
	prev := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(prev)

	if err := database.SeedDefaultCategories(db); err != nil {
		return err
	}

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	start := time.Date(today.Year(), today.Month()-demoMonths, 1, 0, 0, 0, 0, time.Local)

	// --- Accounts, property, members ---------------------------------------
	hash, err := bcrypt.GenerateFromPassword([]byte(database.DemoPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	somchai := models.User{Email: database.DemoEmail, PasswordHash: string(hash), Name: "สมชาย", Role: "admin", IsActive: true}
	if err := db.Create(&somchai).Error; err != nil {
		return err
	}
	// Somying cannot sign in (random, discarded password); she exists so the
	// demo shows a real two-person household with notifications and balances.
	otherHash, _ := bcrypt.GenerateFromPassword([]byte(fmt.Sprintf("%d-%d", rand.Int63(), time.Now().UnixNano())), bcrypt.MinCost)
	somying := models.User{Email: "somying@demo.homelog.app", PasswordHash: string(otherHash), Name: "สมหญิง", Role: "user", IsActive: true}
	if err := db.Create(&somying).Error; err != nil {
		return err
	}
	for _, u := range []models.User{somchai, somying} {
		if err := db.Create(&models.UserSettings{
			UserID: u.ID, Language: "th", Currency: "THB", Theme: "auto", DateFormat: "DD/MM/YYYY",
			EmailNotifications: true, InAppNotifications: true, NotifyJoinRequests: true, NotifySharedExpenses: true,
			BillDueAlertDays: 3, ReadingReminderDays: 7, AnomalyThreshold: 5.0, OnboardingCompleted: true,
		}).Error; err != nil {
			return err
		}
	}

	const address = "99/9 หมู่บ้านสุขใจ ถนนรัตนาธิเบศร์ นนทบุรี 11000"
	home := models.Property{UserID: somchai.ID, Name: "บ้านสุขใจ", Address: address, Type: "owned",
		StartDate: start.AddDate(-2, 0, 0), IsCurrent: true, Residents: 3}
	if err := db.Create(&home).Error; err != nil {
		return err
	}
	if err := db.Create(&models.HouseholdSettings{PropertyID: home.ID, SplitMode: true, DefaultSplitType: "equal"}).Error; err != nil {
		return err
	}
	mA := models.HouseholdMember{PropertyID: home.ID, UserID: &somchai.ID, Name: "สมชาย", Role: "admin"}
	mB := models.HouseholdMember{PropertyID: home.ID, UserID: &somying.ID, Name: "สมหญิง", Role: "member"}
	kid := models.HouseholdMember{PropertyID: home.ID, Name: "น้องต้นกล้า", Role: "ลูก", IsVirtual: true}
	for _, m := range []*models.HouseholdMember{&mA, &mB, &kid} {
		if err := db.Create(m).Error; err != nil {
			return err
		}
	}
	for _, pair := range [][2]uint{{somchai.ID, mB.ID}, {somying.ID, mA.ID}} {
		db.Model(&models.UserSettings{}).Where("user_id = ?", pair[0]).
			Update("default_split_with_member_ids", fmt.Sprintf("[%d]", pair[1]))
	}

	// --- In-process API ----------------------------------------------------
	api := gin.New()
	registerAPIRoutes(api, db)
	tokA, err := demoAccessToken(somchai)
	if err != nil {
		return err
	}
	tokB, err := demoAccessToken(somying)
	if err != nil {
		return err
	}
	call := func(method, path, token string, body any) (map[string]any, error) {
		var buf io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			buf = bytes.NewReader(b)
		}
		req := httptest.NewRequest(method, "/api/v1"+path, buf)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, req)
		if rec.Code < 200 || rec.Code >= 300 {
			return nil, fmt.Errorf("%s %s → %d: %s", method, path, rec.Code, rec.Body.String())
		}
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out, nil
	}
	idOf := func(m map[string]any, nested string) uint {
		if n, ok := m[nested].(map[string]any); ok {
			m = n
		}
		if v, ok := m["id"].(float64); ok {
			return uint(v)
		}
		return 0
	}

	// Category ids by slug (subcategory slugs resolve to their parent too).
	type catRef struct{ cat, sub uint }
	cats := map[string]catRef{}
	var categories []models.Category
	db.Preload("Subcategories").Where("user_id IS NULL").Find(&categories)
	for _, c := range categories {
		cats[c.Slug] = catRef{cat: c.ID}
		for _, s := range c.Subcategories {
			cats[s.Slug] = catRef{cat: c.ID, sub: s.ID}
		}
	}
	cat := func(slug string, body map[string]any) {
		ref, ok := cats[slug]
		if !ok {
			ref = cats[strings.SplitN(slug, "_", 2)[0]]
		}
		body["category_id"] = ref.cat
		if ref.sub != 0 {
			body["subcategory_id"] = ref.sub
		}
	}

	// --- Timeline ----------------------------------------------------------
	rng := rand.New(rand.NewSource(20240101))
	pick := func(xs ...string) string { return xs[rng.Intn(len(xs))] }
	money := func(lo, hi, step float64) float64 { return math.Round((lo+rng.Float64()*(hi-lo))/step) * step }
	ymd := func(t time.Time) string { return t.Format("2006-01-02") }
	iso := func(t time.Time) string { return t.Format("2006-01-02") + "T00:00:00Z" }

	type event struct {
		date time.Time
		run  func() error
	}
	var events []event
	at := func(d time.Time, run func() error) {
		if !d.After(today) {
			events = append(events, event{d, run})
		}
	}

	type person struct {
		token         string
		member, other uint
	}
	who := map[string]person{"A": {tokA, mA.ID, mB.ID}, "B": {tokB, mB.ID, mA.ID}}
	projectIDs := map[string]uint{}
	type opt struct {
		personal bool
		project  string
	}
	expense := func(d time.Time, payer, slug string, amount float64, desc string, o opt) {
		at(d, func() error {
			p := who[payer]
			body := map[string]any{
				"amount": amount, "description": desc, "date": ymd(d), "property_id": home.ID,
				"paid_by_member_id": p.member, "is_split": !o.personal,
			}
			if !o.personal {
				body["split_with_member_ids"] = []uint{p.other}
			}
			if o.project != "" {
				body["project_id"] = projectIDs[o.project]
			}
			cat(slug, body)
			_, err := call("POST", "/expenses", p.token, body)
			return err
		})
	}
	split := opt{}
	personal := opt{personal: true}

	// Utilities.
	utilIDs := map[string]uint{}
	utilSpecs := []struct {
		key, typ, provider, code string
		metered                  bool
		recurring                float64
		unit                     string
	}{
		{"elec", "electricity", "การไฟฟ้านครหลวง (MEA)", "012345678901", true, 0, "month"},
		{"water", "water", "การประปานครหลวง (MWA)", "87654321", true, 0, "month"},
		{"net", "internet", "AIS Fibre", "AIS-8823-4410", false, 642, "month"},
		{"mortgage", "mutuo", "ธนาคารกสิกรไทย", "KB-HL-556677", false, 14500, "month"},
		{"insurance", "insurance", "ทิพยประกันภัย", "FIRE-2291", false, 2400, "year"},
	}
	for _, u := range utilSpecs {
		body := map[string]any{
			"property_id": home.ID, "type": u.typ, "provider": u.provider, "customer_code": u.code,
			"address": address, "start_date": iso(start), "is_active": true, "is_metered": u.metered,
			"billing_interval": 1, "billing_unit": u.unit, "paid_by_member_id": mA.ID,
			"default_category_id": cats["home_utilities"].cat, "currency": "THB",
		}
		if u.recurring > 0 {
			body["recurring_amount"] = u.recurring
		}
		out, err := call("POST", "/utilities", tokA, body)
		if err != nil {
			return err
		}
		utilIDs[u.key] = idOf(out, "utility")
	}

	// Projects, relative to today.
	monthStart := func(back int) time.Time {
		return time.Date(today.Year(), today.Month()-time.Month(back), 1, 0, 0, 0, 0, time.Local)
	}
	monthEnd := func(back int) time.Time { return monthStart(back-1).AddDate(0, 0, -1) }
	projects := []struct {
		key, name, icon, desc, status string
		budget                        float64
		from, to                      int
	}{
		{"songkran", "เที่ยวเชียงใหม่ช่วงสงกรานต์", "🏔️", "พาครอบครัวไปเล่นน้ำสงกรานต์และเที่ยวดอยสุเทพ", "completed", 25000, 7, 6},
		{"birthday", "งานวันเกิดน้องต้นกล้า", "🎂", "จัดงานวันเกิด 6 ขวบที่บ้าน", "completed", 8000, 4, 3},
		{"roof", "ซ่อมหลังคาก่อนหน้าฝน", "🏠", "เปลี่ยนแผ่นกระเบื้องที่แตกและทำกันซึม", "active", 30000, 1, -2},
	}
	for _, p := range projects {
		out, err := call("POST", "/projects", tokA, map[string]any{
			"property_id": home.ID, "name": p.name, "icon": p.icon, "description": p.desc, "budget": p.budget,
			"start_date": ymd(monthStart(p.from)), "end_date": ymd(monthEnd(p.to)), "status": p.status,
			"members": []map[string]any{{"user_id": somying.ID, "role": "owner"}},
		})
		if err != nil {
			return err
		}
		projectIDs[p.key] = idOf(out, "project")
	}

	// Starting meter readings, a month before the first bill.
	firstRead := time.Date(start.Year(), start.Month(), 25, 0, 0, 0, 0, time.Local).AddDate(0, 0, -30)
	elecMeter, waterMeter := 18250.0, 1320.0
	if _, err := call("POST", fmt.Sprintf("/utilities/%d/readings", utilIDs["elec"]), tokA, map[string]any{"reading_date": iso(firstRead), "value": elecMeter}); err != nil {
		return err
	}
	if _, err := call("POST", fmt.Sprintf("/utilities/%d/readings", utilIDs["water"]), tokA, map[string]any{"reading_date": iso(firstRead), "value": waterMeter}); err != nil {
		return err
	}

	for m := 0; m <= demoMonths; m++ {
		first := time.Date(start.Year(), start.Month()+time.Month(m), 1, 0, 0, 0, 0, time.Local)
		daysIn := first.AddDate(0, 1, -1).Day()
		day := func(d int) time.Time { return first.AddDate(0, 0, min(d, daysIn)-1) }
		rday := func() time.Time { return day(1 + rng.Intn(27)) }
		month := first.Month()
		hot := month >= time.March && month <= time.May
		monthsAgo := demoMonths - m
		ab := func(pA float64) string {
			if rng.Float64() < pA {
				return "A"
			}
			return "B"
		}

		for _, d := range []int{3, 10, 17, 24} {
			payer := "B"
			if d%2 == 1 {
				payer = "A"
			}
			expense(day(d+rng.Intn(2)), payer, "food_groceries", money(1200, 3200, 5),
				pick("ซื้อของเข้าบ้าน Lotus's", "ซื้อของที่ Big C", "ตุนของที่ Makro", "ซื้อของ Tops Market"), split)
		}
		for i := 0; i < 3; i++ {
			expense(rday(), ab(0.5), "food_groceries", money(150, 600, 5), pick("ตลาดสดหน้าหมู่บ้าน", "ผักผลไม้ตลาดนัด", "หมู ไก่ ไข่ ตลาดสด"), split)
		}
		for i := 0; i < 3; i++ {
			expense(rday(), ab(0.6), "food_restaurants", money(350, 1800, 10),
				pick("ชาบูชิ", "MK สุกี้", "ส้มตำไก่ย่างเจ๊แดง", "ข้าวมันไก่ประตูน้ำ", "ร้านอาหารญี่ปุ่นฟูจิ", "หมูกระทะ"), split)
		}
		for i := 0; i < 3; i++ {
			o := split
			if rng.Float64() < 0.5 {
				o = personal
			}
			expense(rday(), ab(0.5), "food_delivery", money(120, 550, 5), pick("สั่ง Grab Food", "สั่ง LINE MAN", "สั่ง Foodpanda"), o)
		}
		for i := 0; i < 4; i++ {
			expense(rday(), ab(0.5), "food_cafes", money(55, 180, 5), pick("กาแฟ Café Amazon", "Starbucks", "ชานมไข่มุก", "กาแฟร้านประจำ"), personal)
		}
		for _, p := range []string{"A", "B"} {
			for i := 0; i < 2; i++ {
				expense(day(2+rng.Intn(26)), p, "transport_fuel", money(900, 1600, 10), pick("เติมน้ำมัน ปตท.", "เติมน้ำมัน บางจาก", "เติมน้ำมัน Shell"), personal)
			}
		}
		expense(day(12), "A", "transport_parking_tolls", money(200, 450, 5), "ค่าทางด่วน Easy Pass", personal)
		if m%6 == 2 {
			expense(day(20), "A", "transport_car_maintenance", money(2500, 5500, 50), "เข้าศูนย์เช็กระยะรถ", personal)
		}
		if month == time.May || month == time.November {
			expense(day(8), "A", "family_school", 28500, "ค่าเทอมโรงเรียนน้องต้นกล้า", split)
		}
		expense(day(14), "B", "family_toys", money(250, 1200, 10), pick("ของเล่นน้องต้นกล้า", "หนังสือนิทาน", "ตัวต่อเลโก้"), split)
		if m%3 == 1 {
			expense(day(9), "B", "family_kids_clothing", money(600, 2200, 10), "เสื้อผ้าน้องต้นกล้า", split)
		}
		if m%4 == 0 {
			expense(day(18), "B", "family_health", money(450, 2500, 10), pick("หาหมอเด็ก คลินิก", "ฉีดวัคซีนน้องต้นกล้า", "ร้านขายยา"), split)
		}
		expense(day(6), "B", "clothing_hairdresser", money(300, 1200, 10), "ทำผม", personal)
		if m%2 == 1 {
			expense(day(21), "A", "clothing_hairdresser", 150, "ตัดผม", personal)
		}
		expense(day(11), ab(0.5), "clothing_adults", money(400, 2500, 10), pick("ช้อป Shopee", "สั่ง Lazada", "Uniqlo"), personal)
		expense(day(15), "B", "clothing_personal_products", money(200, 900, 10), "ของใช้ส่วนตัว Watsons", personal)
		expense(day(5), "A", "entertainment_streaming", 419, "Netflix รายเดือน", split)
		expense(day(5), "B", "entertainment_streaming", 149, "Spotify Family", split)
		if m%3 == 0 {
			expense(day(16), "A", "home_ordinary_maintenance", money(350, 2800, 10), pick("ล้างแอร์ 2 เครื่อง", "ช่างซ่อมก๊อกน้ำ", "ซื้อหลอดไฟและอุปกรณ์", "ตัดหญ้าหน้าบ้าน"), split)
		}
		if m%5 == 3 {
			expense(day(25), "A", "technology_devices", money(890, 4500, 10), pick("หูฟังใหม่", "เราเตอร์ Wi‑Fi", "เมาส์และคีย์บอร์ด"), personal)
		}
		if m%2 == 0 {
			expense(day(27), ab(0.5), "gifts_births", money(300, 1000, 100), pick("ทำบุญวัด", "ใส่ซองงานแต่งเพื่อน", "ใส่ซองงานบวช"), personal)
		}

		// Monthly meter readings and bills, paid ~10 days after the reading.
		kwh := math.Round(330 + rng.Float64()*120)
		if hot {
			kwh = math.Round(520 + rng.Float64()*160)
		}
		m3 := math.Round(18 + rng.Float64()*10)
		readDay := day(25)
		prevRead := readDay.AddDate(0, 0, -30)
		elecStart, waterStart := elecMeter, waterMeter
		elecMeter += kwh
		waterMeter += m3
		elecNow, waterNow := elecMeter, waterMeter
		bill := func(key string, amount float64, extra map[string]any) {
			paid := readDay.AddDate(0, 0, 10+rng.Intn(4))
			isPaid := !paid.After(today) && monthsAgo > 0
			at(readDay, func() error {
				body := map[string]any{
					"bill_number": fmt.Sprintf("%s-%s", strings.ToUpper(key), readDay.Format("20060102")),
					"issue_date":  iso(readDay.AddDate(0, 0, 2)), "period_start": iso(prevRead), "period_end": iso(readDay),
					"due_date": iso(readDay.AddDate(0, 0, 15)), "amount_total": amount, "is_paid": isPaid,
				}
				if isPaid {
					body["paid_date"] = ymd(paid)
				}
				for k, v := range extra {
					body[k] = v
				}
				_, err := call("POST", fmt.Sprintf("/utilities/%d/bills", utilIDs[key]), tokA, body)
				return err
			})
		}
		at(readDay, func() error {
			if _, err := call("POST", fmt.Sprintf("/utilities/%d/readings", utilIDs["elec"]), tokA, map[string]any{"reading_date": iso(readDay), "value": elecNow}); err != nil {
				return err
			}
			_, err := call("POST", fmt.Sprintf("/utilities/%d/readings", utilIDs["water"]), tokA, map[string]any{"reading_date": iso(readDay), "value": waterNow})
			return err
		})
		bill("elec", math.Round(kwh*4.18+38.22), map[string]any{"reading_start_value": elecStart, "reading_end_value": elecNow, "provider_reading": elecNow, "consumption_total": kwh})
		bill("water", math.Round(m3*10.5+30), map[string]any{"reading_start_value": waterStart, "reading_end_value": waterNow, "provider_reading": waterNow, "consumption_total": m3})
		bill("net", 642, nil)
		bill("mortgage", 14500, nil)
		if month == time.January {
			bill("insurance", 2400, nil)
		}

		// Settle up early next month (the current month stays open).
		if monthsAgo >= 1 {
			settle := first.AddDate(0, 1, 2)
			label := fmt.Sprintf("เคลียร์ยอดเดือน %s", thaiMonthYear(first))
			at(settle, func() error {
				bal, err := call("GET", fmt.Sprintf("/properties/%d/balance?other_member_id=%d", home.ID, mB.ID), tokA, nil)
				if err != nil {
					return err
				}
				b, _ := bal["balance"].(float64)
				amount := math.Round(math.Abs(b)*100) / 100
				if amount < 1 {
					return nil
				}
				from, to, tok := mB.ID, mA.ID, tokB
				if b < 0 {
					from, to, tok = mA.ID, mB.ID, tokA
				}
				_, err = call("POST", "/settlements", tok, map[string]any{
					"property_id": home.ID, "from_member_id": from, "to_member_id": to, "amount": amount,
					"date": ymd(settle), "payment_method": pick("bank_transfer", "bank_transfer", "cash"), "note": label,
				})
				return err
			})
		}
	}

	// Project spending.
	pd := func(back, d int) time.Time { return monthStart(back).AddDate(0, 0, d-1) }
	for _, e := range []struct {
		back, d       int
		payer, slug   string
		amount        float64
		desc, project string
	}{
		{7, 2, "B", "entertainment_travel", 6800, "ตั๋วเครื่องบินไปเชียงใหม่", "songkran"},
		{6, 12, "A", "entertainment_travel", 7400, "โรงแรมนิมมาน 3 คืน", "songkran"},
		{6, 14, "B", "food_restaurants", 2350, "ข้าวซอยและขันโตก", "songkran"},
		{6, 15, "A", "transport_public", 1800, "เช่ารถแดงเที่ยวดอยสุเทพ", "songkran"},
		{4, 10, "B", "gifts_birthdays", 1850, "เค้กวันเกิด", "birthday"},
		{3, 1, "A", "gifts_birthdays", 3200, "จักรยานของขวัญ", "birthday"},
		{3, 2, "B", "food_restaurants", 2400, "อาหารจัดเลี้ยงเพื่อนน้อง", "birthday"},
		{1, 6, "A", "home_major_works", 9500, "กระเบื้องหลังคาและวัสดุกันซึม", "roof"},
		{0, 2, "A", "home_major_works", 6000, "ค่าแรงช่างงวดแรก", "roof"},
	} {
		expense(pd(e.back, e.d), e.payer, e.slug, e.amount, e.desc, opt{project: e.project})
	}

	// Long-term debt: Somying paid the car down payment; Somchai repays his
	// half at 5,000 a month, outside the running balance.
	var debtID uint
	debtDay := pd(10, 15)
	at(debtDay, func() error {
		body := map[string]any{
			"amount": 120000, "description": "เงินดาวน์รถยนต์คันใหม่", "date": ymd(debtDay), "property_id": home.ID,
			"paid_by_member_id": mB.ID, "is_split": true, "split_with_member_ids": []uint{mA.ID},
		}
		cat("transport", body)
		out, err := call("POST", "/expenses", tokB, body)
		if err != nil {
			return err
		}
		debtID = idOf(out, "expense")
		_, err = call("PATCH", fmt.Sprintf("/expenses/%d/long-term-debt", debtID), tokB, map[string]any{"is_long_term_debt": true})
		return err
	})
	for k := 1; k < 10; k++ {
		d := pd(10-k, 28)
		note := fmt.Sprintf("ผ่อนเงินดาวน์รถ งวดที่ %d", k)
		at(d, func() error {
			_, err := call("POST", "/settlements", tokA, map[string]any{
				"property_id": home.ID, "from_member_id": mA.ID, "to_member_id": mB.ID, "amount": 5000,
				"date": ymd(d), "payment_method": "bank_transfer", "note": note, "target_expense_id": debtID,
			})
			return err
		})
	}

	sort.SliceStable(events, func(i, j int) bool { return events[i].date.Before(events[j].date) })
	for _, ev := range events {
		if err := ev.run(); err != nil {
			return fmt.Errorf("demo seed %s: %w", ev.date.Format("2006-01-02"), err)
		}
	}

	log.SetOutput(prev)
	log.Printf("✅ Thai demo dataset seeded: %d records over %d months (login %s)", len(events), demoMonths, database.DemoEmail)
	return nil
}

// demoAccessToken signs a short-lived access token for the in-process seeder.
func demoAccessToken(u models.User) (string, error) {
	claims := middleware.JWTClaims{
		UserID: u.ID, Email: u.Email, Role: u.Role,
		TokenType: middleware.TokenTypeAccess, TokenVersion: u.TokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(os.Getenv("JWT_SECRET")))
}

var thaiMonths = [...]string{"มกราคม", "กุมภาพันธ์", "มีนาคม", "เมษายน", "พฤษภาคม", "มิถุนายน",
	"กรกฎาคม", "สิงหาคม", "กันยายน", "ตุลาคม", "พฤศจิกายน", "ธันวาคม"}

// thaiMonthYear formats a month in Thai with the Buddhist-era year, e.g.
// "มีนาคม 2569".
func thaiMonthYear(t time.Time) string {
	return fmt.Sprintf("%s %d", thaiMonths[t.Month()-1], t.Year()+543)
}
