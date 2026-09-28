#!/usr/bin/env node
// Fills a running HomeLog instance with a realistic Thai household that looks
// like it has been using the app for a long time — for demos, screenshots and
// manual/agent testing (see TESTING.md). NEVER run it against an instance with
// real data you care about: it creates accounts and hundreds of records.
//
// Everything goes through the public REST API, exactly as the web app would,
// so splits, settlements, notifications, bill → expense sync and the search
// index are produced by the real business logic rather than faked in the DB.
//
// Usage (Node 18+, no dependencies):
//   node scripts/seed-sample-data.mjs                       # 18 months, localhost:8080
//   node scripts/seed-sample-data.mjs --months 6 --base http://localhost:8080
//
// Without Node installed, run it through Docker instead:
//   docker run --rm -v "$(pwd -W 2>/dev/null || pwd)/scripts:/s" node:24-alpine \
//     node /s/seed-sample-data.mjs --base http://host.docker.internal:8080
//
// The server allows ~100 requests/minute per IP, so the script paces itself;
// a full 18-month run takes roughly 8–10 minutes. Browsing the app from the
// same machine while it runs can hit that limit too.

const args = Object.fromEntries(
  process.argv.slice(2).reduce((acc, a, i, all) => {
    if (a.startsWith('--')) acc.push([a.slice(2), all[i + 1]?.startsWith('--') ? 'true' : all[i + 1] ?? 'true'])
    return acc
  }, []),
)
const BASE = (args.base || 'http://localhost:8080').replace(/\/$/, '') + '/api/v1'
const MONTHS = Math.max(1, Math.min(36, parseInt(args.months || '18', 10)))
const PASSWORD = args.password || 'HomeLog2024!'
const FORCE = args.force === 'true'

const PEOPLE = {
  somchai: { email: 'somchai@example.com', name: 'สมชาย' },
  somying: { email: 'somying@example.com', name: 'สมหญิง' },
}
const HOME = { name: 'บ้านสุขใจ', address: '99/9 หมู่บ้านสุขใจ ถนนรัตนาธิเบศร์ นนทบุรี 11000' }

// ---------------------------------------------------------------- utilities

// Deterministic PRNG so every run produces the same household.
let seed = 20240101
function rand() {
  seed = (seed * 1664525 + 1013904223) % 4294967296
  return seed / 4294967296
}
const pick = (arr) => arr[Math.floor(rand() * arr.length)]
const between = (lo, hi) => lo + rand() * (hi - lo)
const money = (lo, hi, step = 1) => Math.round(between(lo, hi) / step) * step

const pad = (n) => String(n).padStart(2, '0')
const ymd = (d) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
const iso = (d) => `${ymd(d)}T00:00:00Z`
const addDays = (d, n) => new Date(d.getFullYear(), d.getMonth(), d.getDate() + n)
const today = new Date()
today.setHours(0, 0, 0, 0)

// Pace requests under the server's 100/min/IP limit and retry on 429.
const MIN_GAP_MS = 660
let lastRequest = 0
let requestCount = 0
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

async function api(method, path, { token, body, ok = [200, 201, 204] } = {}) {
  for (let attempt = 0; attempt < 6; attempt++) {
    const wait = lastRequest + MIN_GAP_MS - Date.now()
    if (wait > 0) await sleep(wait)
    lastRequest = Date.now()
    requestCount++
    const res = await fetch(BASE + path, {
      method,
      headers: {
        'Content-Type': 'application/json',
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
    if (res.status === 429) {
      process.stdout.write('\n⏳ rate limited — waiting 60s...\n')
      await sleep(61_000)
      continue
    }
    const text = await res.text()
    let data = null
    try {
      data = text ? JSON.parse(text) : null
    } catch {
      data = text
    }
    if (!ok.includes(res.status)) {
      const err = new Error(`${method} ${path} → ${res.status}: ${typeof data === 'string' ? data : JSON.stringify(data)}`)
      err.status = res.status
      err.data = data
      throw err
    }
    return data
  }
  throw new Error(`${method} ${path}: still rate limited after several retries`)
}

let progressTotal = 0
let progressDone = 0
function tick(label) {
  progressDone++
  const pct = progressTotal ? Math.floor((progressDone / progressTotal) * 100) : 0
  process.stdout.write(`\r  ${pct.toString().padStart(3)}%  ${progressDone}/${progressTotal}  ${label.padEnd(40).slice(0, 40)}`)
}

// ------------------------------------------------------------------ accounts

async function loginOrRegister(person) {
  try {
    const data = await api('POST', '/auth/login', { body: { email: person.email, password: PASSWORD } })
    return { token: data.token, user: data.user, created: false }
  } catch (e) {
    if (e.status !== 401) throw e
  }
  const data = await api('POST', '/auth/register', {
    body: { email: person.email, password: PASSWORD, name: person.name, language: 'th' },
  })
  return { token: data.token, user: data.user, created: true }
}

async function finishOnboarding(token) {
  await api('PUT', '/settings', {
    token,
    body: {
      language: 'th', currency: 'THB', date_format: 'DD/MM/YYYY', onboarding_completed: true,
      // On by default for accounts created since this was fixed; set
      // explicitly so older servers produce notifications too.
      notify_join_requests: true, notify_shared_expenses: true,
    },
  })
}

// ---------------------------------------------------------------------- main

async function main() {
  console.log(`🌱 HomeLog sample data → ${BASE}  (${MONTHS} months)`)

  const health = await fetch(BASE.replace(/\/api\/v1$/, '') + '/health').catch(() => null)
  if (!health?.ok) throw new Error(`HomeLog is not reachable at ${BASE} — is it running?`)

  // --- Accounts & household -------------------------------------------------
  const A = await loginOrRegister(PEOPLE.somchai)
  const B = await loginOrRegister(PEOPLE.somying)

  let props = await api('GET', '/properties', { token: A.token })
  let home = props.find((p) => p.name === HOME.name)
  if (home && !FORCE) {
    const existing = await api('GET', `/expenses?property_id=${home.id}&limit=1`, { token: A.token })
    const count = Array.isArray(existing) ? existing.length : existing?.total ?? existing?.expenses?.length ?? 0
    if (count > 0) {
      console.log(`\nℹ️  "${HOME.name}" already has data — nothing to do. Use --force to add more anyway.`)
      printLogins()
      return
    }
  }
  if (!home) {
    // The very first account on an instance gets a default property; reuse it.
    home = props.length === 1 && A.created ? props[0] : null
    if (home) {
      home = await api('PUT', `/properties/${home.id}`, {
        token: A.token,
        body: { name: HOME.name, address: HOME.address, residents: 3 },
      })
    } else {
      home = await api('POST', '/properties', {
        token: A.token,
        body: { name: HOME.name, address: HOME.address, type: 'owned', is_current: true, residents: 3 },
      })
    }
  }
  home = home.property || home
  await finishOnboarding(A.token)

  // Somying asks to join; Somchai approves.
  let members = await api('GET', `/properties/${home.id}/members`, { token: A.token })
  if (!members.some((m) => m.user_id === B.user.id)) {
    await api('POST', '/join-requests', { token: B.token, body: { property_id: home.id }, ok: [200, 201, 409] })
    const requests = await api('GET', '/join-requests', { token: A.token })
    const list = Array.isArray(requests) ? requests : requests.requests || requests.join_requests || []
    const req = list.find((r) => (r.user_id === B.user.id || r.user?.id === B.user.id) && r.status === 'pending')
    if (!req) throw new Error('join request from somying not found')
    await api('PATCH', `/join-requests/${req.id}`, { token: A.token, body: { status: 'approved' } })
    members = await api('GET', `/properties/${home.id}/members`, { token: A.token })
  }
  await finishOnboarding(B.token)
  if (!members.some((m) => m.is_virtual && m.name === 'น้องต้นกล้า')) {
    await api('POST', `/properties/${home.id}/members`, { token: A.token, body: { name: 'น้องต้นกล้า', role: 'ลูก' } })
    members = await api('GET', `/properties/${home.id}/members`, { token: A.token })
  }
  const mA = members.find((m) => m.user_id === A.user.id)
  const mB = members.find((m) => m.user_id === B.user.id)
  await api('PUT', `/properties/${home.id}/settings`, { token: A.token, body: { split_mode: true } })
  for (const t of [A.token, B.token]) {
    const other = t === A.token ? mB.id : mA.id
    await api('PUT', '/settings', { token: t, body: { default_split_with_member_ids: JSON.stringify([other]) } })
  }

  // --- Categories ------------------------------------------------------------
  const cats = await api('GET', '/categories', { token: A.token })
  const bySlug = {}
  for (const c of cats) {
    bySlug[c.slug] = { category_id: c.id }
    for (const s of c.subcategories || []) bySlug[s.slug] = { category_id: c.id, subcategory_id: s.id }
  }
  const cat = (slug) => {
    const hit = bySlug[slug] || bySlug[slug.split('_')[0]]
    if (!hit) throw new Error(`category ${slug} not found`)
    return hit
  }

  // --- Timeline --------------------------------------------------------------
  const start = new Date(today.getFullYear(), today.getMonth() - MONTHS, 1)
  const events = [] // { date, run: async () => {}, label }
  const at = (date, label, run) => {
    if (date <= today) events.push({ date, label, run })
  }

  const who = { A: { token: A.token, member: mA, other: mB }, B: { token: B.token, member: mB, other: mA } }
  const expense = (date, payer, slug, amount, description, { split = true, project } = {}) =>
    at(date, description, () =>
      api('POST', '/expenses', {
        token: who[payer].token,
        body: {
          amount,
          description,
          date: ymd(date),
          property_id: home.id,
          paid_by_member_id: who[payer].member.id,
          is_split: split,
          split_with_member_ids: split ? [who[payer].other.id] : [],
          ...(project ? { project_id: project() } : {}),
          ...cat(slug),
        },
      }),
    )

  // Projects are created up-front (their start dates are in the past) and
  // referenced lazily by the expenses that belong to them.
  const projectIds = {}
  const projects = [
    { key: 'kitchen', name: 'รีโนเวทห้องครัว', icon: '🍳', budget: 85000, from: 15, to: 12, status: 'completed',
      description: 'เปลี่ยนเคาน์เตอร์ ตู้ลอย และเตาใหม่ทั้งชุด' },
    { key: 'songkran', name: 'เที่ยวเชียงใหม่ช่วงสงกรานต์', icon: '🏔️', budget: 25000, from: 7, to: 6, status: 'completed',
      description: 'พาครอบครัวไปเล่นน้ำสงกรานต์และเที่ยวดอยสุเทพ' },
    { key: 'birthday', name: 'งานวันเกิดน้องต้นกล้า', icon: '🎂', budget: 8000, from: 4, to: 3, status: 'completed',
      description: 'จัดงานวันเกิด 6 ขวบที่บ้าน' },
    { key: 'roof', name: 'ซ่อมหลังคาก่อนหน้าฝน', icon: '🏠', budget: 30000, from: 1, to: -2, status: 'active',
      description: 'เปลี่ยนแผ่นกระเบื้องที่แตกและทำกันซึม' },
  ].filter((p) => p.from <= MONTHS)
  const monthStart = (back) => new Date(today.getFullYear(), today.getMonth() - back, 1)
  const projectRef = (key) => () => projectIds[key]

  // --- Utilities -------------------------------------------------------------
  const utilities = {}
  const utilitySpecs = [
    { key: 'elec', type: 'electricity', provider: 'การไฟฟ้านครหลวง (MEA)', customer_code: '012345678901', is_metered: true },
    { key: 'water', type: 'water', provider: 'การประปานครหลวง (MWA)', customer_code: '87654321', is_metered: true },
    { key: 'net', type: 'internet', provider: 'AIS Fibre', customer_code: 'AIS-8823-4410', is_metered: false, recurring_amount: 642 },
    { key: 'mortgage', type: 'mutuo', provider: 'ธนาคารกสิกรไทย', customer_code: 'KB-HL-556677', is_metered: false, recurring_amount: 14500 },
    { key: 'insurance', type: 'insurance', provider: 'ทิพยประกันภัย', customer_code: 'FIRE-2291', is_metered: false, recurring_amount: 2400,
      billing_unit: 'year' },
  ]

  // Month-by-month household life.
  let elecMeter = 18250
  let waterMeter = 1320
  for (let m = 0; m <= MONTHS; m++) {
    const first = new Date(start.getFullYear(), start.getMonth() + m, 1)
    const daysInMonth = new Date(first.getFullYear(), first.getMonth() + 1, 0).getDate()
    const day = (d) => new Date(first.getFullYear(), first.getMonth(), Math.min(d, daysInMonth))
    const month = first.getMonth() // 0 = January
    const hot = month >= 2 && month <= 4 // March–May: air-con season
    const monthsAgo = MONTHS - m

    // Groceries: weekly big shop, alternating who pays, plus the fresh market.
    for (const d of [3, 10, 17, 24]) {
      expense(day(d + Math.floor(rand() * 2)), d % 2 ? 'A' : 'B', 'food_groceries', money(1200, 3200, 5),
        pick(['ซื้อของเข้าบ้าน Lotus\'s', 'ซื้อของที่ Big C', 'ตุนของที่ Makro', 'ซื้อของ Tops Market']))
    }
    for (let i = 0; i < 3; i++) {
      expense(day(1 + Math.floor(rand() * 27)), rand() < 0.5 ? 'A' : 'B', 'food_groceries', money(150, 600, 5),
        pick(['ตลาดสดหน้าหมู่บ้าน', 'ผักผลไม้ตลาดนัด', 'หมู ไก่ ไข่ ตลาดสด']))
    }
    // Eating out together and food delivery.
    for (let i = 0; i < 3; i++) {
      expense(day(1 + Math.floor(rand() * 27)), rand() < 0.6 ? 'A' : 'B', 'food_restaurants', money(350, 1800, 10),
        pick(['ชาบูชิ', 'MK สุกี้', 'ส้มตำไก่ย่างเจ๊แดง', 'ข้าวมันไก่ประตูน้ำ', 'ร้านอาหารญี่ปุ่นฟูจิ', 'หมูกระทะ']))
    }
    for (let i = 0; i < 3; i++) {
      const payer = rand() < 0.5 ? 'A' : 'B'
      expense(day(1 + Math.floor(rand() * 27)), payer, 'food_delivery', money(120, 550, 5),
        pick(['สั่ง Grab Food', 'สั่ง LINE MAN', 'สั่ง Foodpanda']), { split: rand() < 0.5 })
    }
    for (let i = 0; i < 4; i++) {
      const payer = rand() < 0.5 ? 'A' : 'B'
      expense(day(1 + Math.floor(rand() * 27)), payer, 'food_cafes', money(55, 180, 5),
        pick(['กาแฟ Café Amazon', 'Starbucks', 'ชานมไข่มุก', 'กาแฟร้านประจำ']), { split: false })
    }
    // Transport: each drives their own car; fuel is personal.
    for (const p of ['A', 'B']) {
      for (let i = 0; i < 2; i++) {
        expense(day(2 + Math.floor(rand() * 26)), p, 'transport_fuel', money(900, 1600, 10),
          pick(['เติมน้ำมัน ปตท.', 'เติมน้ำมัน บางจาก', 'เติมน้ำมัน Shell']), { split: false })
      }
    }
    expense(day(12), 'A', 'transport_parking_tolls', money(200, 450, 5), 'ค่าทางด่วน Easy Pass', { split: false })
    if (m % 6 === 2) expense(day(20), 'A', 'transport_car_maintenance', money(2500, 5500, 50), 'เข้าศูนย์เช็กระยะรถ', { split: false })
    // Kids.
    if (month === 4 || month === 10) expense(day(8), 'A', 'family_school', 28500, 'ค่าเทอมโรงเรียนน้องต้นกล้า')
    expense(day(14), 'B', 'family_toys', money(250, 1200, 10), pick(['ของเล่นน้องต้นกล้า', 'หนังสือนิทาน', 'ตัวต่อเลโก้']))
    if (m % 3 === 1) expense(day(9), 'B', 'family_kids_clothing', money(600, 2200, 10), 'เสื้อผ้าน้องต้นกล้า')
    if (m % 4 === 0) expense(day(18), 'B', 'family_health', money(450, 2500, 10), pick(['หาหมอเด็ก คลินิก', 'ฉีดวัคซีนน้องต้นกล้า', 'ร้านขายยา']))
    // Personal.
    expense(day(6), 'B', 'clothing_hairdresser', money(300, 1200, 10), 'ทำผม', { split: false })
    if (m % 2) expense(day(21), 'A', 'clothing_hairdresser', 150, 'ตัดผม', { split: false })
    expense(day(11), rand() < 0.5 ? 'A' : 'B', 'clothing_adults', money(400, 2500, 10), pick(['ช้อป Shopee', 'สั่ง Lazada', 'Uniqlo']), { split: false })
    expense(day(15), 'B', 'clothing_personal_products', money(200, 900, 10), 'ของใช้ส่วนตัว Watsons', { split: false })
    // Subscriptions and home.
    expense(day(5), 'A', 'entertainment_streaming', 419, 'Netflix รายเดือน')
    expense(day(5), 'B', 'entertainment_streaming', 149, 'Spotify Family')
    if (m % 3 === 0) expense(day(16), 'A', 'home_ordinary_maintenance', money(350, 2800, 10),
      pick(['ล้างแอร์ 2 เครื่อง', 'ช่างซ่อมก๊อกน้ำ', 'ซื้อหลอดไฟและอุปกรณ์', 'ตัดหญ้าหน้าบ้าน']))
    if (m % 5 === 3) expense(day(25), 'A', 'technology_devices', money(890, 4500, 10), pick(['หูฟังใหม่', 'เราเตอร์ Wi‑Fi', 'เมาส์และคีย์บอร์ด']), { split: false })
    if (m % 2 === 0) expense(day(27), rand() < 0.5 ? 'A' : 'B', 'gifts_births', money(300, 1000, 100), pick(['ทำบุญวัด', 'ใส่ซองงานแต่งเพื่อน', 'ใส่ซองงานบวช']), { split: false })
    if (month === 1) expense(day(14), 'A', 'gifts_valentines', money(900, 2500, 10), 'ดอกไม้และดินเนอร์วาเลนไทน์', { split: false })

    // Utilities: monthly readings and bills (paid a few days before the due date).
    const kwh = Math.round(hot ? between(520, 680) : between(330, 450))
    const m3 = Math.round(between(18, 28))
    const readDay = day(25)
    const prevRead = addDays(readDay, -30)
    const elecStart = elecMeter
    const waterStart = waterMeter
    elecMeter += kwh
    waterMeter += m3
    const bill = (key, amount, extra = {}) => {
      const due = addDays(readDay, 15)
      const paid = addDays(readDay, 10 + Math.floor(rand() * 4))
      const isPaid = paid <= today && monthsAgo > 0
      at(readDay, `บิล ${key}`, () =>
        api('POST', `/utilities/${utilities[key]}/bills`, {
          token: A.token,
          body: {
            bill_number: `${key.toUpperCase()}-${ymd(readDay).replace(/-/g, '')}`,
            issue_date: iso(addDays(readDay, 2)),
            period_start: iso(prevRead),
            period_end: iso(readDay),
            due_date: iso(due),
            amount_total: amount,
            is_paid: isPaid,
            ...(isPaid ? { paid_date: ymd(paid) } : {}),
            ...extra,
          },
        }),
      )
    }
    at(readDay, 'จดมิเตอร์ไฟ', () =>
      api('POST', `/utilities/${utilities.elec}/readings`, { token: A.token, body: { reading_date: iso(readDay), value: elecMeter, source: 'manual' } }))
    at(readDay, 'จดมิเตอร์น้ำ', () =>
      api('POST', `/utilities/${utilities.water}/readings`, { token: A.token, body: { reading_date: iso(readDay), value: waterMeter, source: 'manual' } }))
    bill('elec', Math.round(kwh * 4.18 + 38.22), {
      reading_start_value: elecStart, reading_end_value: elecMeter, provider_reading: elecMeter, consumption_total: kwh,
    })
    bill('water', Math.round(m3 * 10.5 + 30), {
      reading_start_value: waterStart, reading_end_value: waterMeter, provider_reading: waterMeter, consumption_total: m3,
    })
    bill('net', 642)
    bill('mortgage', 14500)
    if (month === 0) bill('insurance', 2400)

    // Settle up early next month: whoever owes pays the whole running balance.
    if (monthsAgo >= 1) {
      const settleDay = new Date(first.getFullYear(), first.getMonth() + 1, 3)
      at(settleDay, 'เคลียร์ยอดประจำเดือน', async () => {
        const bal = await api('GET', `/properties/${home.id}/balance?other_member_id=${mB.id}`, { token: A.token })
        const amount = Math.round(Math.abs(bal.balance || 0) * 100) / 100
        if (amount < 1) return
        // balance > 0: Somying owes Somchai; < 0: the other way round.
        const [from, to, token] = bal.balance > 0 ? [mB, mA, B.token] : [mA, mB, A.token]
        await api('POST', '/settlements', {
          token,
          body: {
            property_id: home.id, from_member_id: from.id, to_member_id: to.id, amount,
            date: ymd(settleDay), payment_method: pick(['bank_transfer', 'bank_transfer', 'cash']),
            note: `เคลียร์ยอดเดือน ${first.toLocaleDateString('th-TH', { month: 'long', year: 'numeric' })}`,
          },
        })
      })
    }
  }

  // Project spending.
  const projDay = (back, d) => new Date(today.getFullYear(), today.getMonth() - back, d)
  if (projects.some((p) => p.key === 'kitchen')) {
    expense(projDay(15, 5), 'A', 'home_major_works', 12000, 'มัดจำช่างทำครัว', { project: projectRef('kitchen') })
    expense(projDay(14, 12), 'A', 'home_furniture', 38500, 'ท็อปหินและตู้ครัวบิวท์อิน', { project: projectRef('kitchen') })
    expense(projDay(13, 20), 'B', 'home_appliances', 16900, 'เตาและเครื่องดูดควัน', { project: projectRef('kitchen') })
    expense(projDay(12, 8), 'A', 'home_major_works', 14000, 'ค่าแรงงวดสุดท้าย', { project: projectRef('kitchen') })
  }
  if (projects.some((p) => p.key === 'songkran')) {
    expense(projDay(7, 2), 'B', 'entertainment_travel', 6800, 'ตั๋วเครื่องบินไปเชียงใหม่', { project: projectRef('songkran') })
    expense(projDay(6, 12), 'A', 'entertainment_travel', 7400, 'โรงแรมนิมมาน 3 คืน', { project: projectRef('songkran') })
    expense(projDay(6, 14), 'B', 'food_restaurants', 2350, 'ข้าวซอยและขันโตก', { project: projectRef('songkran') })
    expense(projDay(6, 15), 'A', 'transport_public', 1800, 'เช่ารถแดงเที่ยวดอยสุเทพ', { project: projectRef('songkran') })
  }
  if (projects.some((p) => p.key === 'birthday')) {
    expense(projDay(4, 10), 'B', 'gifts_birthdays', 1850, 'เค้กวันเกิด', { project: projectRef('birthday') })
    expense(projDay(3, 1), 'A', 'gifts_birthdays', 3200, 'จักรยานของขวัญ', { project: projectRef('birthday') })
    expense(projDay(3, 2), 'B', 'food_restaurants', 2400, 'อาหารจัดเลี้ยงเพื่อนน้อง', { project: projectRef('birthday') })
  }
  if (projects.some((p) => p.key === 'roof')) {
    expense(projDay(1, 6), 'A', 'home_major_works', 9500, 'กระเบื้องหลังคาและวัสดุกันซึม', { project: projectRef('roof') })
    expense(projDay(0, 2), 'A', 'home_major_works', 6000, 'ค่าแรงช่างงวดแรก', { project: projectRef('roof') })
  }

  // A long-term debt: Somying paid the car down payment; Somchai repays half
  // of it monthly, outside the running balance.
  let debtExpenseId = null
  const debtMonthsAgo = Math.min(12, MONTHS)
  const debtDay = projDay(debtMonthsAgo, 15)
  at(debtDay, 'เงินดาวน์รถยนต์', async () => {
    const e = await api('POST', '/expenses', {
      token: B.token,
      body: {
        amount: 120000, description: 'เงินดาวน์รถยนต์คันใหม่', date: ymd(debtDay), property_id: home.id,
        paid_by_member_id: mB.id, is_split: true, split_with_member_ids: [mA.id], ...cat('transport'),
      },
    })
    debtExpenseId = (e.expense || e).id
    await api('PATCH', `/expenses/${debtExpenseId}/long-term-debt`, { token: B.token, body: { is_long_term_debt: true } })
  })
  for (let k = 1; k < debtMonthsAgo; k++) {
    const d = projDay(debtMonthsAgo - k, 28)
    at(d, 'ผ่อนเงินดาวน์รถ', () =>
      api('POST', '/settlements', {
        token: A.token,
        body: {
          property_id: home.id, from_member_id: mA.id, to_member_id: mB.id, amount: 5000, date: ymd(d),
          payment_method: 'bank_transfer', note: `ผ่อนเงินดาวน์รถ งวดที่ ${k}`, target_expense_id: debtExpenseId,
        },
      }))
  }

  // --- Run -------------------------------------------------------------------
  events.sort((a, b) => a.date - b.date)
  progressTotal = events.length + projects.length + utilitySpecs.length
  const estMin = Math.ceil((progressTotal * 1.2 * MIN_GAP_MS) / 60000)
  console.log(`📦 ${events.length} records to create — about ${estMin} minutes\n`)

  for (const p of projects) {
    const created = await api('POST', '/projects', {
      token: A.token,
      body: {
        property_id: home.id, name: p.name, icon: p.icon, description: p.description, budget: p.budget,
        start_date: ymd(monthStart(p.from)), end_date: ymd(new Date(today.getFullYear(), today.getMonth() - p.to + 1, 0)),
        status: p.status, members: [{ user_id: B.user.id, role: 'owner' }],
      },
    })
    projectIds[p.key] = (created.project || created).id
    tick(p.name)
  }
  for (const u of utilitySpecs) {
    const created = await api('POST', '/utilities', {
      token: A.token,
      body: {
        property_id: home.id, type: u.type, provider: u.provider, customer_code: u.customer_code,
        address: HOME.address, start_date: iso(start), is_active: true, is_metered: u.is_metered,
        ...(u.recurring_amount ? { recurring_amount: u.recurring_amount } : {}),
        billing_interval: 1, billing_unit: u.billing_unit || 'month', paid_by_member_id: mA.id,
        default_category_id: cat('home_utilities').category_id, currency: 'THB',
      },
    })
    utilities[u.key] = (created.utility || created).id
    tick(u.provider)
  }
  // Starting meter readings, one month before the first bill.
  await api('POST', `/utilities/${utilities.elec}/readings`, { token: A.token, body: { reading_date: iso(addDays(new Date(start.getFullYear(), start.getMonth(), 25), -30)), value: 18250 } })
  await api('POST', `/utilities/${utilities.water}/readings`, { token: A.token, body: { reading_date: iso(addDays(new Date(start.getFullYear(), start.getMonth(), 25), -30)), value: 1320 } })

  let failures = 0
  for (const ev of events) {
    try {
      await ev.run()
    } catch (e) {
      failures++
      process.stdout.write(`\n⚠️  ${ymd(ev.date)} ${ev.label}: ${e.message}\n`)
      if (failures > 20) throw new Error('too many failures — stopping')
    }
    tick(`${ymd(ev.date)} ${ev.label}`)
  }

  console.log(`\n\n✅ Done: ${events.length - failures} records created, ${failures} failed, ${requestCount} API calls.`)
  printLogins()
}

function printLogins() {
  console.log('\nSign in at the app with:')
  for (const p of Object.values(PEOPLE)) console.log(`  ${p.name.padEnd(8)} ${p.email}  /  ${PASSWORD}`)
}

main().catch((e) => {
  console.error(`\n❌ ${e.message}`)
  process.exit(1)
})
