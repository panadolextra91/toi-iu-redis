# 🧠 Giải thích 4 chế độ Cache — phiên bản "Bà Ngoại Cũng Hiểu"

> *Giả sử mẹ là 1 quán cơm, và mỗi món ăn cần nấu 200 giây (giống DB latency).*

---

## 🏠 Mô tả "quán cơm" của mẹ

| Vai trò | Trong code | Trong đời thật |
|---|---|---|
| `FakeDB` | Fake database | **Nhà bếp** — nấu ăn thật sự |
| `Redis` | Cache | **Tủ lạnh** — trữ đồ ăn đã nấu sẵn |
| `Pool = 10` | DB connections | **10 bếp gas** — mỗi lần nấu 1 món |
| `TTL = 3s` | Cache hết hạn | **Đồ ăn trong tủ lạnh** hết hạn sau 3 giây |
| `Singleflight` | Request coalescing | **1 người đi lấy đồ** rồi chia cho cả nhà |

---

## 🍽 Mode 1: `/nocache` — "Không Tủ Lạnh"

```
Khách gọi món → Bếp nấu → Trả món ăn
```

**Chuyện gì xảy ra:**

- Mỗi khách gọi 1 món → bếp phải nấu từ đầu
- 200 khách gọi cùng lúc → **10 bếp gas không đủ** → 190 người phải **xếp hàng đợi**
- Ai đợi cũng mất ~200 giây

**Số liệu:**

| Metric | Giá trị |
|---|---|
| 🍳 Bếp nấu | **200 lần** |
| 😓 Khách đợi | **~190 người** |
| ⏱ Thời gian trung bình | **~2 giây** |

> **Tóm lại:** *"Ai cũng phải chờ vì tất cả đổ xô vào bếp."*

---

## 🧊 Mode 2: `/cache` — "Có Tủ Lạnh, Hạn Sử Dụng 30 Giây"

```
Khách gọi món → Kiểm tra tủ lạnh
                     ├─ Có sẵn → Lấy ra ngay ✅
                     └─ Chưa có → Bếp nấu → Bỏ vào tủ lạnh
```

**Chuyện gì xảy ra:**

- Khách đầu tiên → tủ lạnh trống → bếp nấu → **bỏ vào tủ lạnh**
- 199 khách sau → tủ lạnh có sẵn → **lấy ra ngay, không cần nấu**
- Tủ lạnh hết hạn sau 30 giây

**Số liệu:**

| Metric | Giá trị |
|---|---|
| 🍳 Bếp nấu | **1 lần duy nhất!** |
| 😓 Khách đợi | **0 người** |
| ⏱ Thời gian trung bình | **< 5 mili-giây** (nhanh như lấy đồ từ tủ lạnh) |

> **Tóm lại:** *"Chỉ cần 1 lần nấu, 199 lần sau lấy ở tủ lạnh ra là xong."*

---

## 🧊❄️ Mode 3: `/cache-herd` — "Tủ Lạnh Hết Điện, Mọi Người Đổ Xô Vào Bếp"

```
Khách gọi món → Kiểm tra tủ lạnh
                     ├─ Có sẵn → Lấy ra ngay ✅
                     └─ Hết hạn → MỌI NGƯỜI đều chạy vào bếp 🔥
```

**Chuyện gì xảy ra:**

- Ban đầu tủ lạnh có đồ → 200 khách lấy ra ngon lành
- Sau 3 giây, **tủ lạnh hết hạn** (như hết điện)
- 200 khách mới ập vào → tủ lạnh trống → **200 người đổ xô vào bếp cùng lúc**
- Lại có 190 người xếp hàng đợi

**Số liệu:**

| Metric | Giá trị |
|---|---|
| 🍳 Bếp nấu | **~200 lần** (gần như ai cũng nấu!) |
| 😓 Khách đợi | **~190 người** |
| ⏱ Thời gian P99 | **~4 giây** |

> **Tóm lại:** *"Tủ lạnh tắt 3 giây thôi mà cả làng đổ xô vào bếp, kẹt xe kinh khủng."*

> ⚠️ Đây gọi là **Thundering Herd** (Bầy đàn thảnh thơi) — khi cache hết hạn, tất cả request đồng thời kích hoạt bếp.

---

## 🧊❄️👮 Mode 4: `/cache-protected` — "Có Tủ Lạnh + 1 Người Đại Diện"

```
Khách gọi món → Kiểm tra tủ lạnh
                     ├─ Có sẵn → Lấy ra ngay ✅
                     └─ Hết hạn → 1 người đại diện đi nấu, cả làng CHỜ
```

**Chuyện gì xảy ra:**

- Tủ lạnh hết hạn sau 3 giây
- **Thay vì 200 người đổ xô vào bếp:**
  - 1 người được chọn đi nấu
  - 199 người còn lại **ngồi yên chờ** người đó nấu xong
  - Người đó nấu xong → chia cho cả làng + bỏ vào tủ lạnh

**Số liệu:**

| Metric | Giá trị |
|---|---|
| 🍳 Bếp nấu | **1 lần duy nhất!** |
| 😓 Khách đợi | **0 người** (nhưng ai cũng phải đợi người đại diện) |
| 👥 Singleflight shared | **199 người** (đợi người đại diện) |
| ⏱ Thời gian | **~200ms** (mọi người đợi 1 người nấu, không kẹt xe) |

> **Tóm lại:** *"Chỉ 1 người đi nấu, cả làng đợi rồi chia nhau ăn. Không kẹt xe, ai cũng được món."*

---

## 📊 Bảng So Sánh "Dễ Hiểu"

|  | 🤦 Không tủ lạnh | 🧊 Tủ lạnh 30s | 🧊❄️ Tủ lạnh 3s (hết điện) | 🧊❄️👮 + 1 người đại diện |
|---|---|---|---|---|
| 🍳 **Bếp nấu** | 200 lần | 1 lần | ~200 lần | **1 lần** |
| 😓 **Người đợi** | ~190 người | 0 người | ~190 người | 0 người |
| ⏱ **Tốc độ** | ~2 giây | < 5ms | ~4 giây | **~200ms** |
| ️ **Đánh giá** | ❌ Kẹt xe | ✅ OK | ❌ Kẹt xe | ✅ **OK** |

---

## 🎯 Tóm lại 1 câu mỗi mode

| Mode | Tóm tắt 1 câu |
|---|---|
| **NoCache** | *"Không có tủ lạnh, ai cũng phải chờ bếp."* |
| **CacheAside** | *"Có tủ lạnh, lần đầu nấu rồi lưu lại, lần sau lấy nhanh."* |
| **CacheHerd** | *"Tủ lạnh hết hạn là cả làng đổ xô vào bếp, kẹt xe."* |
| **CacheProtected** | *"Có tủ lạnh + 1 người đại diện đi nấu, cả làng đợi rồi chia nhau."* |

---

## 🧪 4 Kịch bản test — "Phim" minh họa

### 🎬 Scene 1: NoCache — "Ngày Tất Bật"

> 200 khách cùng gọi món → 10 bếp không đủ → 190 người đứng chờ → mệt không à.

### 🎬 Scene 2: CacheAside — "Ngày Bình Thường"

> Sáng đầu 1 khách gọi → bếp nấu → bỏ tủ lạnh → 199 khách còn lại lấy nhanh từ tủ → xong.

### 🎬 Scene 3: CacheHerd — "Tủ Lạnh Hết Điện"

> Tủ lạnh chạy 3 giây → tắt → 200 khách ập vào bếp cùng lúc → kẹt xe kinh khủng như Scene 1.

### 🎬 Scene 4: CacheProtected — "Có Trật Tự"

> Tủ lạnh chạy 3 giây → tắt → 1 người được cử đi nấu → 199 người ngồi chờ → xong → cả làng đi về.
