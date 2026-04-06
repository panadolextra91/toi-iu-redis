# 🧠 Cache là cái gì — nếu con iu hỏi

Quán cơm nhà mình lúc nào cũng đông nghẹt, khách vô kêu "cho anh dĩa rau thêm". Nếu không có cache thì nhân viên phải chạy tuốt ra chợ mua rau, rửa rau, xong mới bưng ra, khách đói chửi um sùm. Còn mà có cache là bà chủ quán rửa sẵn 1 rổ rau tươi xanh để ngay quầy, khách kêu là bốc đưa liền khỏi chờ.

Cache có thể đặt tại trình duyệt (client), máy chủ biên (CDN), proxy ngược (Nginx) hoặc ngay trong app (Redis/Memcached). CDN nói nôm na là 1 cái mạng lưới máy chủ toàn cầu dùng để phân phối mấy cái nội dung tĩnh như ảnh, video, JS/CSS từ vị trí gần user nhất, giúp giảm đáng kể latency — cũng là một dạng cache.

Trong hệ thống mình, mình dùng **Redis** để cache ngay trong app. Redis lưu data trên RAM, tốc độ phản hồi tính bằng **mili-giây**, nhanh như lấy đồ từ tủ lạnh vậy. Nhưng con iu ơi, tủ lạnh nhà mình có giới hạn thôi — đầy rồi thì phải dọn bớt, lâu không ai đụng tới thì xoá đi. Đó gọi là **Cache Eviction**, mẹ sẽ nói sau.

---

## 🏠 Đây là "quán cơm" của mình

| Trong code | Trong đời thật |
|---|---|
| Database (FakeDB) | **Nhà bếp** — nấu ăn thật sự, mỗi món tốn 200ms |
| Redis (cache) | **Tủ lạnh** — trữ đồ ăn đã nấu sẵn |
| Pool = 10 | **10 bếp gas** — mỗi lần nấu 1 món, hết bếp thì phải xếp hàng |
| TTL = 3s | **Đồ ăn trong tủ lạnh** hết hạn sau 3 giây |
| Singleflight | **1 người đại diện** đi nấu rồi chia cho cả nhà |

---

## 🍽 Mode 1: `/nocache` — "Không Tủ Lạnh"

```
Khách gọi món → Bếp nấu → Trả món ăn
```

Chuyện gì xảy ra: mỗi khách gọi 1 món là bếp phải nấu từ đầu. 200 khách gọi cùng lúc mà chỉ có 10 bếp gas thì 190 người phải xếp hàng đợi. Ai đợi cũng mất ~200ms. Nói chung là kẹt xe kinh khủng.

| Metric | Giá trị |
|---|---|
| 🍳 Bếp nấu | **200 lần** |
| 😓 Khách đợi | **~190 người** |
| ⏱ Thời gian trung bình | **~2 giây** |

> *"Ai cũng phải chờ vì tất cả đổ xô vào bếp."*

---

## 🧊 Mode 2: `/cache` — "Có Tủ Lạnh, Hạn Sử Dụng 30 Giây"

```
Khách gọi món → Kiểm tra tủ lạnh
                     ├─ Có sẵn → Lấy ra ngay ✅
                     └─ Chưa có → Bếp nấu → Bỏ vào tủ lạnh
```

Khách đầu tiên gọi thì tủ lạnh trống, phải chạy xuống bếp nấu, xong rồi bỏ vô tủ lạnh. 199 khách sau thì tủ lạnh có sẵn rồi, lấy ra ngay không cần nấu nữa. Đồ ăn trong tủ lạnh hết hạn sau 30 giây — nhưng 30 giây thì lâu lắm rồi, đa số khách đã được phục vụ hết.

| Metric | Giá trị |
|---|---|
| 🍳 Bếp nấu | **1 lần duy nhất!** |
| 😓 Khách đợi | **0 người** |
| ⏱ Thời gian trung bình | **< 5 mili-giây** (nhanh như lấy đồ từ tủ lạnh) |

> *"Chỉ cần 1 lần nấu, 199 lần sau lấy ở tủ lạnh ra là xong."*

---

## 🧊❄️ Mode 3: `/cache-herd` — "Tủ Lạnh Hết Hạn, Mọi Người Đổ Xô Vào Bếp"

```
Khách gọi món → Kiểm tra tủ lạnh
                     ├─ Có sẵn → Lấy ra ngay ✅
                     └─ Hết hạn → MỌI NGƯỜI đều chạy vào bếp 🔥
```

Đây là cái hay ho của mode này. Lúc đầu tủ lạnh có đồ, 200 khách lấy ra ngon lành. Nhưng sau 3 giây thì đồ ăn trong tủ lạnh hết hạn — bị xoá. Ngay cái tích tắc đó, 200 khách mới ập vào, thấy tủ lạnh trống là đổ xô hết vào bếp. Kết quả: 190 người lại xếp hàng, bếp nấu 200 lần, latency nhảy lên ~4 giây. Tủ lạnh 30 giây thì ngon rồi, chứ 3 giây thì gần như không cache gì.

| Metric | Giá trị |
|---|---|
| 🍳 Bếp nấu | **~200 lần** (gần như ai cũng nấu!) |
| 😓 Khách đợi | **~190 người** |
| ⏱ Thời gian P99 | **~4 giây** |

> *"Tủ lạnh tắt 3 giây thôi mà cả làng đổ xô vào bếp, kẹt xe kinh khủng."*

Đây gọi là **Thundering Herd** hay còn gọi là **Cache Stampede** — bầy đàn thảnh thơi. Khi cache hết hạn, tất cả request đồng thời kích hoạt bếp. Mẹ sẽ nói kỹ hơn ở phần dưới.

---

## 🎬 Kịch bản kinh điển: "iPhone 16 hết hạn"

Giả sử mình cache thông tin sản phẩm "iPhone 16" trong Redis với TTL là 60 phút. Đúng lúc phút thứ 60, cache hết hạn, bị xoá.

Thảm hoạ: ngay cái tích tắc đó, có **1 triệu user cùng bấm vô coi iPhone 16**. Tất cả đều chọc vô Redis để lấy cache nhưng thấy hết rồi (miss) → 1 triệu cái request đó **CÙNG LÚC đánh hội đồng** con DB để lấy data → DB sập ngay lập tức giống như 10 cái bếp ở trên mà bị dồn 1 triệu đứa đập vào.

Trong project này, mình mô phỏng y xì vậy: cache "benchmark-key" với TTL 3 giây, rồi cho 200 request đập vào đồng thời. Kết quả y như iPhone 16 — cache hết → cả làng đổ xô vào bếp.

---

## 💡 Các chiến lược cache — đọc thì thế nào, ghi thì thế nào

Cache không chỉ có đọc, mà còn **ghi** nữa. Có 3 chiêu phổ biến:

### 🖊 Chiêu 1: Cache-Aside (Lazy Loading)

> *App sẽ check cache trước, nếu có (hit — trúng) thì lấy ra xài luôn, nếu không có (miss — hụt) thì chạy xuống DB lấy, xong tiện tay nhét ngược vô cache để lần sau có mà xài.*

Ưu điểm: cache chỉ chứa data nào user cần. Hư cache thì vẫn còn DB gánh dù hơi chậm tí. Đây cũng là chiêu mình dùng trong 3 mode cache của project này.

### 🖊 Chiêu 2: Write-Through

> *Khi ghi data, nhân viên vừa bỏ đồ vào tủ lạnh, vừa chạy xuống bếp nấu luôn, cùng lúc.*

Ưu điểm: data trong cache luôn mới cứng (fresh), không sợ bị cũ (stale), tính nhất quán siêu cao. Nhược điểm: ghi hơi chậm xíu vì phải ghi ở 2 chỗ lận.

### 🖊 Chiêu 3: Write-Back

> *Khi ghi data, nhân viên bỏ đồ vào tủ lạnh trước rồi phản hồi liền cho user "oke đã nhận đơn", sau đó mới lặng lẽ chạy xuống bếp nấu từ từ, theo lô (batch) để tăng hiệu suất ghi.*

Ưu điểm: user vừa thấy được phản hồi nhanh, vừa đỡ chờ đợi ghi 2 chỗ như thằng write-through. Nhược điểm: nếu tủ lạnh chết trước khi kịp nấu → món bay mất.

Trong project này mình dùng **Cache-Aside** vì đây là chiêu phổ biến nhất và phù hợp nhất với kịch bản đọc nhiều hơn ghi.

---

## 🗑 Cache Eviction — "Dọn rác"

RAM có hạn, không thể lưu cả thế giới vô cache được. Đầy rồi thì mình phải xoá bớt. Thường xài thuật toán **LRU (Least Recently Used)** — thằng nào lâu quá không ai đụng tới thì xoá trước, món nào ế quá thì dọn nghỉ bán cho trống chỗ. Redis tự lo phần này, mình chỉ cần cấu hình `maxmemory` và `allkeys-lru` là xong.

---

## ⚠️ Cảnh báo: đừng tham lam mà cache 1 cục bự

Redis nhanh, nhưng cái chết người là khi mình **JSON.parse()** cái cục data lớn khi lấy ra hoặc **JSON.stringify()** khi nhét vào — đây là hàm đồng bộ (synchronous) và ăn CPU.

Sai lầm của anh em là cache nguyên 1 cục JSON bự chảng, ví dụ list 10,000 sản phẩm cỡ 5MB vô Redis. Hậu quả: lúc lấy ra, server phải đứng vài chục mili-giây để parse cái cục 5MB đó. Trong thời gian đó, **server bị treo, latency nhảy loạn**.

→ **Lời khuyên:** chia nhỏ data ra mà cache, đừng tham lam mà cache 1 cục bự. Trong project này mình chỉ cache 1 JSON string nhỏ (~100 bytes), nên không có vấn đề gì.

---

## 🧊❄️👮 Mode 4: `/cache-protected` — "Có Tủ Lạnh + 1 Người Đại Diện"

```
Khách gọi món → Kiểm tra tủ lạnh
                     ├─ Có sẵn → Lấy ra ngay ✅
                     └─ Hết hạn → 1 người đại diện đi nấu, cả làng CHỜ
```

Tủ lạnh hết hạn sau 3 giây. Nhưng lần này, thay vì 200 người đổ xô vào bếp, mình cử **1 người đại diện** đi vô bếp nấu. 199 người còn lại ngồi yên chờ người đó xong, rồi chia nhau ăn. Ai cũng được món, không kẹt xe, bếp chỉ nấu đúng 1 lần.

| Metric | Giá trị |
|---|---|
| 🍳 Bếp nấu | **1 lần duy nhất!** |
| 😓 Khách đợi | **0 người** (1 người đại diện vô bếp nhưng ai cũng phải đợi người đại diện) |
| 👥 Singleflight shared | **199 người** (đợi người đại diện) |
| ⏱ Thời gian | **~200ms** (mọi người đợi 1 người nấu, không kẹt xe) |

> *"Chỉ 1 người đi nấu, cả làng đợi rồi chia nhau ăn. Không kẹt xe, ai cũng được món."*

---

## 🔧 Các giải pháp chống Thundering Herd

Trong project này mình dùng **Singleflight** để gom request — đó là 1 cách. Ngoài ra còn 2 cách khác:

### 🔒 Cách 1: Locking (Khoá)

> *Khi thấy cache miss, chỉ cho phép **1 request duy nhất** được quyền vào DB lấy data và cập nhật lại cache. Thằng đầu tiên chiếm khoá đi nấu, 199 thằng còn lại bắt đứng chờ (ngủ 1 chút rồi thử lại). Trong Redis thì dùng lệnh `SETNX` để làm khoá.*

Ưu điểm: chắc chắn chỉ 1 thằng đập vào DB. Nhược điểm: 199 thằng kia phải chờ, tốn thời gian.

### 🎲 Cách 2: Probabilistic Early Expiration (Hết hạn ngẫu nhiên)

> *Đừng để tất cả cache hết hạn đúng cùng 1 thời điểm. Mình random cho nó hết hạn sớm hơn, ví dụ từ phút 55 tới 60. Như vậy sẽ có 1 thằng xui xui nào đó thấy hết hạn ở phút 55 và **tự thân đi load lại cache**, lúc đó chưa có đông người, DB vẫn thở.*

Ưu điểm: không ai phải chờ ai. Nhược điểm: random nên không đảm bảo 100%.

### ⚖️ So sánh

| Cách | Singleflight (project này) | Locking | Prob. Early Exp. |
|---|---|---|---|
| **Ai vào DB?** | 1 thằng | 1 thằng | 1 thằng |
| **Thằng khác chờ?** | ✅ Có, nhưng được kết quả ngay | ✅ Có, phải retry | ❌ Không, tự đi lấy |
| **Độ phức tạp** | Thấp (thư viện có sẵn) | Trung bình (cần SETNX) | Cao (cần tính toán) |

---

## 📊 Bảng So Sánh "Dễ Hiểu"

|  | 🤦 Không tủ lạnh | 🧊 Tủ lạnh 30s | 🧊❄️ Tủ lạnh 3s (hết hạn) | 🧊❄️👮 + 1 người đại diện |
|---|---|---|---|---|
| 🍳 **Bếp nấu** | 200 lần | 1 lần | ~200 lần | **1 lần** |
| 😓 **Người đợi** | ~190 người | 0 người | ~190 người | 0 người |
| ⏱ **Tốc độ** | ~2 giây | < 5ms | ~4 giây | **~200ms** |
| ️️ **Đánh giá** | ❌ Kẹt xe | ✅ OK | ❌ Kẹt xe | ✅ **OK** |

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

### 🎬 Scene 3: CacheHerd — "Tủ Lạnh Hết Hạn"

> Tủ lạnh chạy 3 giây → hết hạn → 200 khách ập vào bếp cùng lúc → kẹt xe kinh khủng như Scene 1.

### 🎬 Scene 4: CacheProtected — "Có Trật Tự"

> Tủ lạnh chạy 3 giây → hết hạn → 1 người được cử đi nấu → 199 người ngồi chờ → xong → cả làng đi về.
