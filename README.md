# Midway Quest

เกมพิกเซลอาร์ตเมืองจำลองของพอร์ทัล **Midway** (โมดูล KPlay) สอนเรื่อง security, ESG และสร้างทีมเวิร์กผ่านการเล่นทุกวัน

## โครงสร้าง
| โฟลเดอร์ | เนื้อหา |
|---|---|
| `game/` | ตัวเกมทั้งหมดในไฟล์เดียว `index.html` เปิดในเบราว์เซอร์ได้ทันที |
| `tools/` | smoke test แบบ headless (Node.js) |
| `api/` | backend Go Fiber + PostgreSQL + Redis สำหรับล็อกอิน KID และเศรษฐกิจฝั่งเซิร์ฟเวอร์ |
| `docs/` | backlog และเอกสารประกอบ |

## เริ่มใช้งาน
```bash
# เปิดเกม
open game/index.html            # หรือ npx serve game

# ทดสอบ
cd tools && npm install && npm run smoke

# backend (ดูรายละเอียดใน api/README.md)
cd api && cp .env.example .env && docker compose up -d
```

## Deploy
มี GitHub Actions (`.github/workflows/pages.yml`) สำหรับรัน smoke test แล้ว deploy โฟลเดอร์ `game/` ขึ้น GitHub Pages อัตโนมัติเมื่อ push เข้า `main`
(เปิดใช้ที่ Settings → Pages → Source: GitHub Actions) ถ้าเป็น repo ภายในองค์กร ให้ปรับไป deploy เข้าพอร์ทัล Midway แทน

## ทำงานต่อกับ Claude Code
อ่าน `CLAUDE.md` ก่อนทุกครั้ง มีแผนผังโค้ด จุดต่อขยาย และข้อผิดพลาดที่เคยเจอ
