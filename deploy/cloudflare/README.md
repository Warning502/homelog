# เดโม HomeLog บน Cloudflare

โฟลเดอร์นี้ใช้ขึ้น HomeLog **โหมดเดโม** บน Cloudflare Containers ด้วย `wrangler` (CLI ของ Cloudflare)
ลูกค้าเข้าเล่นได้ตลอดเวลาผ่านลิงก์ `https://homelog-demo.<ชื่อบัญชี>.workers.dev` โดยไม่ต้องเปิดเครื่องของคุณทิ้งไว้

## ลูกค้าจะเห็นอะไร

- ครอบครัวตัวอย่าง **"บ้านสุขใจ"** ที่ใช้ระบบมา 12 เดือน เป็นภาษาไทยและเงินบาททั้งหมด:
  ค่าใช้จ่ายประจำวัน, ค่าไฟ/ค่าน้ำพร้อมเลขมิเตอร์, บิลผ่อนบ้าน, โปรเจกต์, การหารค่าใช้จ่ายกับคู่ชีวิต, หนี้ระยะยาว และการแจ้งเตือน
- เข้าด้วยปุ่ม **เข้าสู่เดโม** ในหน้า login หรือใช้ `demo@hbs-finance.com` / `demo`
- ลูกค้าทุกคนใช้บัญชีเดียวกัน ทุกอย่างกดเล่นได้ แต่ระบบห้ามเปลี่ยนรหัสผ่าน ห้ามลบบัญชี และห้ามสมัครสมาชิกใหม่
- ข้อมูลจะ **รีเซ็ตกลับเป็นค่าตั้งต้นทุก 1 ชั่วโมง** และทุกครั้งที่ระบบเริ่มทำงานใหม่ ลูกค้าจึงแก้หรือลบข้อมูลได้โดยไม่มีผลถาวร
- ถ้าไม่มีใครเข้า 30 นาที ระบบจะหลับเพื่อประหยัด คนแรกที่เข้ามาหลังจากนั้นจะรอเพิ่มอีกไม่กี่วินาที

> ห้ามใช้ชุดนี้เก็บข้อมูลจริง ข้อมูลใน container ไม่ถาวรและจะถูกล้างเสมอ

## สิ่งที่ต้องมี

1. **บัญชี Cloudflare ที่เปิดใช้ Workers แบบเสียเงิน (Workers Paid)** เพราะ Containers ไม่มีในแผนฟรี
   ตรวจราคาล่าสุดในหน้า Dashboard ก่อนเปิดใช้
2. **Docker Desktop ที่เปิดไว้** เพราะ `wrangler` จะ build image จาก `Dockerfile` ของ repo นี้บนเครื่องคุณก่อนอัปโหลด
3. **Node.js 18 ขึ้นไป**

## ขึ้นระบบครั้งแรก

เปิด Git Bash ที่โฟลเดอร์ของโปรเจกต์ แล้วรัน:

```bash
git pull origin main
cd deploy/cloudflare
npm install

# 1) ล็อกอิน Cloudflare (เปิดเบราว์เซอร์ให้กดอนุญาต)
npx wrangler login

# 2) ขึ้นระบบ — ครั้งแรกจะ build และอัปโหลด image ใช้เวลาหลายนาที
npx wrangler deploy

# 3) ตั้งกุญแจสำหรับเซ็น token (ทำครั้งเดียว) — วางค่าที่ได้จากคำสั่ง openssl
openssl rand -base64 32
npx wrangler secret put JWT_SECRET
```

เมื่อ `wrangler deploy` เสร็จ จะแสดงลิงก์ `https://homelog-demo.<ชื่อบัญชี>.workers.dev`
หลังขึ้นระบบครั้งแรก Cloudflare อาจใช้เวลาอีก 2–5 นาทีกว่า container จะพร้อม ถ้าเปิดแล้วยังไม่ขึ้นให้รอสักครู่แล้วลองใหม่

## ใช้โดเมนของตัวเอง (ไม่บังคับ)

Cloudflare Dashboard → **Workers & Pages** → `homelog-demo` → **Settings** → **Domains & Routes** → **Add** → **Custom domain**
แล้วใส่ เช่น `demo.yourdomain.com` (โดเมนต้องใช้ DNS ของ Cloudflare)

## อัปเดตเดโมหลังแก้โค้ด

```bash
git pull origin main
cd deploy/cloudflare
npx wrangler deploy
```

## ดู log / แก้ปัญหา

```bash
npx wrangler tail          # ดู log แบบสด
```

| อาการ | สาเหตุ / วิธีแก้ |
|---|---|
| หน้าเว็บขึ้นว่า `JWT_SECRET is not set` | ยังไม่ได้ทำขั้นที่ 3 ให้รัน `npx wrangler secret put JWT_SECRET` |
| `wrangler deploy` ขึ้นว่าเรียก Docker ไม่ได้ | เปิด Docker Desktop แล้วรอจนขึ้นสถานะ Running ก่อน |
| deploy แล้วแจ้งว่าต้องใช้แผนเสียเงิน | เปิด Workers Paid ในหน้า Dashboard |
| เปิดครั้งแรกช้า | ระบบกำลังตื่นจากการหลับและสร้างข้อมูลเดโมใหม่ รอไม่กี่วินาที |

## สถิติผู้เข้าชม (ไม่บังคับ)

ค่าเริ่มต้นคือ **ไม่เก็บสถิติใด ๆ** ถ้าต้องการนับจำนวนผู้เข้าชมแบบไม่ใช้ cookie ให้สมัคร [GoatCounter](https://www.goatcounter.com/)
แล้วใส่รหัสไซต์ของคุณที่ `DEMO_GOATCOUNTER_SITE` ใน `wrangler.jsonc` จากนั้น `npx wrangler deploy` ใหม่
