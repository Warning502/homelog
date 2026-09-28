# เดโม HomeLog บน server ของตัวเอง (ผ่าน SSH)

ขึ้น HomeLog **โหมดเดโม** บน Linux server ของคุณ (VPS) ด้วยคำสั่งเดียวจากเครื่องของคุณ
ลูกค้าเข้าเล่นได้ตลอดเวลาที่ `http://IP-ของ-server`

## ขึ้นระบบ / อัปเดต

เปิด **Git Bash** (ไม่ใช่ PowerShell) ที่โฟลเดอร์โปรเจกต์ แล้วรัน:

```bash
git pull origin main
bash scripts/deploy-server.sh root@IP-ของ-server
```

พิมพ์รหัสผ่าน SSH เมื่อถูกถาม (ถามครั้งเดียว และสคริปต์ไม่เก็บรหัสผ่านไว้) สคริปต์จะทำให้เองทั้งหมด:

1. อัปโหลดโค้ดล่าสุดของ `main` ไปที่ `/opt/homelog-demo` บน server (server ไม่ต้องเข้าถึง GitHub)
2. ติดตั้ง Docker ถ้ายังไม่มี
3. สร้าง `.env` พร้อม `JWT_SECRET` แบบสุ่ม (ครั้งแรกครั้งเดียว)
4. build แล้วเปิดเดโมที่ port 80 (ครั้งแรกใช้เวลาหลายนาที) และเปิด port ใน `ufw` ถ้าเปิดใช้อยู่
5. รอจนระบบพร้อม แล้วแสดงลิงก์ `http://IP-ของ-server`

จะอัปเดตเดโมหลังแก้โค้ด ให้รันสองคำสั่งเดิมซ้ำ ข้อมูลเดโมจะถูกสร้างใหม่ทุกครั้ง

## ลูกค้าจะเห็นอะไร

- ครอบครัวตัวอย่าง "บ้านสุขใจ" ที่ใช้ระบบมา 12 เดือน เป็นภาษาไทยและเงินบาท
- เข้าด้วยปุ่ม **เข้าสู่เดโม** ในหน้า login (`demo@homelog.app` / `demo`)
- ข้อมูล **รีเซ็ตทุก 1 ชั่วโมง** ห้ามเปลี่ยนรหัสผ่าน ห้ามลบบัญชี และห้ามสมัครสมาชิกใหม่
- ไม่หลับเหมือนแผนฟรีของ Render ถ้า server เปิดอยู่ ลูกค้าเข้าได้ตลอด

## คำสั่งที่ใช้บน server (ถ้าจำเป็น)

```bash
ssh root@IP-ของ-server
cd /opt/homelog-demo
docker compose -f docker-compose.yml -f docker-compose.demo.yml logs -f      # ดู log
docker compose -f docker-compose.yml -f docker-compose.demo.yml restart      # รีสตาร์ต
docker compose -f docker-compose.yml -f docker-compose.demo.yml down         # ปิดเดโม
```

ถ้า port 80 ถูกใช้โดยโปรแกรมอื่นอยู่ ให้แก้ `HOST_PORT` ใน `/opt/homelog-demo/.env` เป็นเลขอื่น เช่น `8080` แล้วรันสคริปต์ซ้ำ

## ความปลอดภัย

- เดโมนี้เป็น HTTP ธรรมดา ถ้าต้องการ HTTPS และโดเมนของตัวเอง แนะนำให้ใช้ Caddy หรือ Cloudflare Tunnel ครอบไว้ด้านหน้า
- หลังขึ้นระบบแล้ว แนะนำให้ **เปลี่ยนรหัสผ่าน root** และเปลี่ยนไปใช้ SSH key แทนรหัสผ่าน
- **ห้ามใช้ server นี้เก็บข้อมูลจริงของ HomeLog** เพราะโหมดเดโมจะล้างข้อมูลทุกชั่วโมง
