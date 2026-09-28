# แผนทดสอบ HomeLog (สำหรับ AI agent / Antigravity)

เอกสารนี้เป็นขั้นตอนทดสอบระบบ HomeLog ทั้งหมดแบบ end-to-end ให้ agent ทำตามทีละข้อ
แล้วรายงานผลตามรูปแบบในหัวข้อ **"รูปแบบรายงานผล"** ท้ายไฟล์

> **กติกาสำหรับ agent**
> - ห้ามแก้โค้ดระหว่างทดสอบ หน้าที่คือ *ทดสอบและรายงาน* เท่านั้น
> - ทำทุกข้อตามลำดับ ถ้าข้อไหนไม่ผ่านให้บันทึกแล้วทำข้อถัดไปต่อ (ยกเว้นขั้นเตรียมระบบ ถ้าไม่ผ่านให้หยุดและรายงาน)
> - ทุกข้อที่ไม่ผ่าน ให้แนบ: คำสั่งที่รัน, ผลที่ได้จริง, ภาพหน้าจอ (ถ้าเป็นการทดสอบในเบราว์เซอร์) และ log จาก `docker compose logs --tail=50`
> - ข้อมูลที่ใช้ทดสอบเป็นข้อมูลปลอมทั้งหมด ห้ามใช้อีเมลหรือรหัสผ่านจริง

---

## 0. สภาพแวดล้อม

| รายการ | ค่า |
|---|---|
| ระบบปฏิบัติการ | Windows (Git Bash / MINGW64) หรือ Linux/macOS |
| เครื่องมือที่ต้องมี | Docker Desktop (มี `docker compose`), Git, `curl`, `openssl`, เบราว์เซอร์ Chrome/Edge |
| URL ของแอป | `http://localhost:8080` |
| โฟลเดอร์โปรเจกต์ | root ของ repo นี้ (มีไฟล์ `docker-compose.yml`) |

ตัวแปรที่ใช้ในคำสั่ง `curl` ด้านล่าง (รันใน Git Bash):

```bash
export BASE=http://localhost:8080/api/v1
export JAR=./test-cookies.txt     # ไฟล์เก็บ cookie ของ curl
```

---

## 1. เตรียมระบบ (ต้องผ่านทั้งหมดก่อนทดสอบข้ออื่น)

### 1.1 Build และรัน

```bash
git pull origin main
docker rm -f homelog 2>/dev/null || true          # ลบ container เก่าที่ชื่อซ้ำ (ถ้ามี)
[ -f .env ] || echo "JWT_SECRET=$(openssl rand -base64 32)" > .env
docker compose up -d --build
```

**ผลที่คาดหวัง**
- build สำเร็จ ไม่มีบรรทัด `ERROR` (โดยเฉพาะขั้น `npm run build` ต้องผ่าน)
- `docker compose ps` แสดง container `homelog` สถานะ `running` และภายใน ~1 นาทีเป็น `healthy`

### 1.2 Health check

```bash
curl -s http://localhost:8080/health
```

**คาดหวัง:** JSON ที่มี `"status":"healthy"`

### 1.3 เริ่มจากฐานข้อมูลว่าง (ทางเลือก)

ถ้าต้องการทดสอบแบบสะอาด ให้หยุดระบบแล้วย้ายฐานข้อมูลเก่าออกก่อน (**อย่าลบข้อมูลจริงของผู้ใช้** ให้ย้ายไปเก็บไว้แทน):

```bash
docker compose down
mv data data-backup-$(date +%Y%m%d%H%M%S)
docker compose up -d
```

---

## 2. ภาษาไทยและค่าเริ่มต้น

| # | ขั้นตอน | ผลที่คาดหวัง |
|---|---|---|
| 2.1 | เปิด `http://localhost:8080/login` ในเบราว์เซอร์ | หน้า login แสดงเป็นภาษาไทย: หัวข้อ "แอปจัดการค่าใช้จ่ายในบ้าน", ช่อง "อีเมล", "รหัสผ่าน", ปุ่ม "เข้าสู่ระบบ" |
| 2.2 | ตั้งภาษาเบราว์เซอร์เป็นภาษาที่ระบบไม่รองรับ (เช่น ญี่ปุ่น) หรือเปิดหน้าต่าง Incognito ใหม่ แล้วลบ `localStorage` key `locale` | หน้ายังแสดงเป็นภาษาไทย (ภาษาเริ่มต้นคือ `th`) |
| 2.3 | ตรวจทุกหน้าหลักหลัง login (หน้าหลัก, ค่าใช้จ่าย, บริการ, โปรเจกต์, ตั้งค่า) | ไม่มีข้อความที่เป็น key ดิบ เช่น `auth.login.title` หรือ `errors.xxx` และไม่มีข้อความภาษาอิตาลีหลงเหลือ |

---

## 3. สมัครสมาชิกและการตรวจข้อมูล

ทดสอบผ่านหน้าเว็บ (กด "ยังไม่มีบัญชี? สมัครสมาชิก") และยืนยันด้วย `curl`

| # | อีเมล | รหัสผ่าน | ชื่อ | ข้อความที่คาดหวัง (UI) | `error_code` ที่คาดหวัง (API) |
|---|---|---|---|---|---|
| 3.1 | `admin@admin` | `LongPassword1` | `ทดสอบ` | "กรุณากรอกอีเมลให้ถูกต้อง (เช่น name@example.com)" | `invalid_email` |
| 3.2 | `tester@example.com` | `short` | `ทดสอบ` | "รหัสผ่านใหม่ต้องมีอย่างน้อย 8 ตัวอักษร" (ตรวจฝั่งหน้าเว็บก่อนส่ง) | `password_too_short` |
| 3.3 | `tester@example.com` | `1234567` (7 ตัว) | `ทดสอบ` | เหมือน 3.2 | `password_too_short` |
| 3.4 | `tester@example.com` | `LongPassword1` | *(เว้นว่าง)* | เบราว์เซอร์ไม่ให้ส่งฟอร์ม (ช่องชื่อเป็น required) — ส่วน API ต้องตอบ "กรุณากรอกชื่อ" | `name_required` |
| 3.5 | `tester@example.com` | `LongPassword1` | `ทดสอบ` | สมัครสำเร็จ เข้าสู่ระบบอัตโนมัติ | HTTP `201` |
| 3.6 | `tester@example.com` (ซ้ำ) | `LongPassword1` | `ทดสอบ` | "อีเมลนี้ลงทะเบียนแล้ว" | `email_already_registered` |

ตัวอย่างคำสั่งยืนยันด้วย API:

```bash
curl -s -X POST $BASE/auth/register -H 'Content-Type: application/json' \
  -d '{"email":"admin@admin","password":"LongPassword1","name":"ทดสอบ","language":"th"}'
# คาดหวัง: "error_code":"invalid_email"
```

### 3.7 ค่าเริ่มต้นของบัญชีแรก

บัญชีแรกที่สมัคร (ข้อ 3.5) ต้องได้:

```bash
TOKEN=$(curl -s -c $JAR -X POST $BASE/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"tester@example.com","password":"LongPassword1"}' | sed -E 's/.*"token":"([^"]+)".*/\1/')

curl -s $BASE/properties -H "Authorization: Bearer $TOKEN"
curl -s $BASE/settings   -H "Authorization: Bearer $TOKEN"
```

**คาดหวัง**
- ที่พักแรกชื่อ `"บ้านหลัก"`
- settings มี `"language":"th"` และ `"currency":"THB"`
- ผู้ใช้คนแรกมี `"role":"admin"`

---

## 4. ความปลอดภัยของ session / token

### 4.1 Login ไม่ส่ง refresh token ใน body และตั้ง cookie แบบ HttpOnly

```bash
curl -s -D - -o body.json -X POST $BASE/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"tester@example.com","password":"LongPassword1"}' | grep -i set-cookie
cat body.json
```

**คาดหวัง**
- header มี `Set-Cookie: homelog_refresh=...; Path=/api/v1/auth; Max-Age=604800; HttpOnly; SameSite=Strict`
- `body.json` มีแค่ `token` และ `user` **ต้องไม่มี** `refresh_token`

### 4.2 ในเบราว์เซอร์ JavaScript อ่าน refresh token ไม่ได้

หลัง login ผ่านหน้าเว็บ เปิด DevTools → Console:

```js
document.cookie                 // คาดหวัง: "" (ว่าง)
Object.keys(localStorage)       // คาดหวัง: มีแค่ "token", "user" (และอาจมี "locale") — ต้องไม่มี "refreshToken"
```

DevTools → Application → Cookies → `http://localhost:8080` ต้องเห็น `homelog_refresh` ที่ติ๊ก **HttpOnly** และ SameSite = **Strict**

### 4.3 ใช้ refresh token แทน access token ไม่ได้

```bash
RT=$(grep homelog_refresh $JAR | awk '{print $7}')
curl -s -o /dev/null -w "%{http_code}\n" $BASE/properties -H "Authorization: Bearer $RT"
```

**คาดหวัง:** `401`

### 4.4 ใช้ access token ที่ `/auth/refresh` ไม่ได้

```bash
curl -s -X POST $BASE/auth/refresh -H 'Content-Type: application/json' -d "{\"refresh_token\":\"$TOKEN\"}"
```

**คาดหวัง:** HTTP 401 และ `"error_code":"invalid_refresh_token"`

### 4.5 Refresh ผ่าน cookie ได้

```bash
curl -s -b $JAR -c $JAR -o /dev/null -w "%{http_code}\n" -X POST $BASE/auth/refresh
```

**คาดหวัง:** `200` และ cookie ใน `$JAR` ถูกเปลี่ยนเป็นค่าใหม่

### 4.6 หน้าเว็บกู้ session เองเมื่อ access token หมดอายุ

1. login ผ่านหน้าเว็บ
2. Console: `localStorage.setItem('token', 'garbage')`
3. คลิกไปหน้า "ค่าใช้จ่าย" หรือกด reload

**คาดหวัง:** ยังอยู่ในระบบ ไม่ถูกส่งกลับหน้า login และ `localStorage.getItem('token')` ไม่ใช่ `'garbage'` แล้ว

### 4.7 Logout ลบ cookie

1. กดออกจากระบบในหน้าตั้งค่า
2. DevTools → Cookies

**คาดหวัง:** ไม่มี `homelog_refresh` แล้ว และ `localStorage` ไม่มี `token`/`user` แต่ key `locale` (ถ้ามี) ต้องยังอยู่

API:

```bash
curl -s -b $JAR -c $JAR -o /dev/null -w "%{http_code}\n" -X POST $BASE/auth/logout   # คาดหวัง 204
curl -s -b $JAR -X POST $BASE/auth/refresh                                            # คาดหวัง 401
```

### 4.8 เปลี่ยนรหัสผ่านแล้ว session อื่นถูกยกเลิก

1. เปิดเบราว์เซอร์ **A** (ปกติ) และ **B** (Incognito) แล้ว login บัญชีเดียวกันทั้งสองฝั่ง
2. ที่ **A**: ตั้งค่า → เปลี่ยนรหัสผ่าน จาก `LongPassword1` เป็น `NewPassword22`
3. ที่ **A**: ใช้งานต่อได้ตามปกติ (ต้องไม่ถูก logout)
4. ที่ **B**: Console `localStorage.setItem('token','garbage')` แล้ว reload (เพื่อบังคับให้ refresh)

**คาดหวัง:** **B** ถูกส่งกลับหน้า login ส่วน **A** ยังใช้งานได้

> หมายเหตุ: ถ้าไม่ทำขั้นที่ 4 access token เดิมของ B ยังใช้ได้อีกไม่เกิน 15 นาที ซึ่งเป็นพฤติกรรมที่ออกแบบไว้ ไม่ใช่ bug

### 4.9 รหัสผ่านใหม่สั้นกว่า 8 ตัวถูกปฏิเสธ

ที่หน้าตั้งค่า → เปลี่ยนรหัสผ่าน ใส่รหัสใหม่ `1234567`

**คาดหวัง:** ข้อความ "รหัสผ่านใหม่ต้องมีอย่างน้อย 8 ตัวอักษร" และรหัสผ่านไม่ถูกเปลี่ยน

### 4.10 ลืมรหัสผ่าน / reset

1. หน้า login → "ลืมรหัสผ่าน?" → ใส่ `tester@example.com`
2. ดู token จาก log:
   ```bash
   docker compose logs homelog | grep -A1 "PASSWORD RESET TOKEN"
   ```
3. ใช้ token นั้นตั้งรหัสผ่านใหม่ `ResetPassword33`

**คาดหวัง**
- ข้อความตอบกลับเหมือนกันไม่ว่าอีเมลจะมีในระบบหรือไม่ (ลองอีเมล `nobody@example.com` ด้วย)
- reset สำเร็จ login ด้วยรหัสใหม่ได้ และ refresh cookie เดิมทั้งหมดใช้ไม่ได้ (`/auth/refresh` ได้ 401)
- ใช้ reset token เดิมซ้ำเป็นครั้งที่สองไม่ได้

---

## 5. ฟังก์ชันหลักของแอป (smoke test)

ทำผ่านหน้าเว็บด้วยขนาดจอมือถือ (DevTools → Toggle device → 390×844) และจอคอมพิวเตอร์อย่างละรอบ

| # | ขั้นตอน | ผลที่คาดหวัง |
|---|---|---|
| 5.1 | ทำ onboarding ให้จบ (ถ้าระบบพาไปหน้า onboarding) | เข้าหน้าหลักได้ |
| 5.2 | เพิ่มค่าใช้จ่าย 3 รายการ หมวดต่างกัน สกุลเงิน THB | แสดงในรายการ ยอดรวมและสัญลักษณ์ `฿` ถูกต้อง |
| 5.3 | แก้ไข และลบค่าใช้จ่าย 1 รายการ | รายการอัปเดต/หายไปถูกต้อง |
| 5.4 | ตัวกรองวันที่ในหน้าค่าใช้จ่าย | ผลลัพธ์ตรงช่วงวันที่ |
| 5.5 | หน้าหลัก (dashboard) | กราฟและตัวเลขสรุปแสดงผล ไม่มี error |
| 5.6 | เพิ่มบริการ (เช่น ค่าไฟ) และบันทึกค่ามิเตอร์ 2 ครั้ง | แสดงการใช้งานระหว่างสองครั้ง |
| 5.7 | สร้างโปรเจกต์ และผูกค่าใช้จ่ายเข้าโปรเจกต์ | ยอดของโปรเจกต์อัปเดต |
| 5.8 | ค้นหา (ไอคอนแว่นขยาย) ด้วยคำจากรายการที่สร้าง | พบรายการนั้น |
| 5.9 | ตั้งค่า → เปลี่ยนภาษาเป็น English แล้วกลับเป็น ไทย | UI เปลี่ยนภาษาทันทีทั้งสองครั้ง |
| 5.10 | ตั้งค่า → ส่งออกข้อมูล (JSON) | ดาวน์โหลดไฟล์ได้และเปิดเป็น JSON ที่ถูกต้อง |
| 5.11 | เชิญ/เพิ่มสมาชิกในบ้าน แล้วสร้างค่าใช้จ่ายแบบหารกัน | ยอดคงเหลือระหว่างสมาชิกถูกต้อง |

---

## 6. ความคงทนของข้อมูล

```bash
docker compose restart
# รอจนสถานะ healthy แล้ว login ใหม่
docker compose down && docker compose up -d
```

**คาดหวัง:** ข้อมูลทั้งหมดจากข้อ 3–5 ยังอยู่ครบหลังทั้งสองกรณี (ข้อมูลเก็บใน `./data`)

---

## 7. (ทางเลือก) โหมดเดโม

รันแยกต่างหากด้วยพอร์ตอื่น เพื่อไม่ให้กระทบข้อมูลทดสอบ:

```bash
docker run -d --name homelog-demo -p 8081:8080 \
  -e JWT_SECRET=demo-test-secret -e DEMO_MODE=true homelog-th:latest
curl -s -X POST http://localhost:8081/api/v1/auth/register -H 'Content-Type: application/json' \
  -d '{"email":"x@example.com","password":"LongPassword1","name":"x"}'
docker rm -f homelog-demo
```

**คาดหวัง:** HTTP 403 และ `"error_code":"demo_mode_forbidden"` (ต้องไม่มีข้อความภาษาอิตาลี)

---

## 8. เก็บกวาดหลังทดสอบ

```bash
rm -f test-cookies.txt body.json
```

ถ้าในข้อ 1.3 ย้ายฐานข้อมูลจริงออกไป ให้ย้ายกลับ:

```bash
docker compose down
rm -rf data && mv data-backup-XXXXXXXXXXXXXX data   # ใช้ชื่อโฟลเดอร์ที่สร้างไว้
docker compose up -d
```

---

## รูปแบบรายงานผล

ให้ agent สรุปผลเป็นตารางนี้ แล้วตามด้วยรายละเอียดของข้อที่ไม่ผ่าน:

```markdown
## ผลการทดสอบ HomeLog — <วันที่>, commit <git rev-parse --short HEAD>

| ข้อ | ผล | หมายเหตุ |
|---|---|---|
| 1.1 | ✅ ผ่าน | |
| 3.1 | ❌ ไม่ผ่าน | ได้ error_code = invalid_registration แทน invalid_email |
| 7   | ⏭️ ข้าม | ไม่ได้ทดสอบโหมดเดโม |
...

### รายละเอียดข้อที่ไม่ผ่าน
#### 3.1
- คำสั่ง / ขั้นตอน: ...
- ผลที่คาดหวัง: ...
- ผลที่ได้จริง: ...
- หลักฐาน: (ภาพหน้าจอ / output / log)
```
