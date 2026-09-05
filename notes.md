## 1. DB Connection Pool

`pool` is not a single DB connection — it's a pool (array) of reusable connections.

- We hold one `*pgxpool.Pool` reference in the server.
- On `Ping()` or any query/migration, the pool picks any idle connection.
- This avoids opening a new TCP connection per request and allows concurrent queries up to `MAX_CONNS`.

---

## 2. Prefer `strings.Contains` over hand-rolled loops

Don't write a custom loop with an anonymous closure just to check a substring. The stdlib version is cleaner and faster:

```go
// ❌ custom contains loop
// ✅ simple version
strings.Contains(s, substr)
```

---
## Twitter Snowflake, a distributed unique ID generator

This is the core formula for **Twitter Snowflake**, a distributed unique ID generator. It packs three distinct pieces of data into a single 64-bit integer using bitwise operations (`<<` shift and `|` OR).

---

### 1. Bit Layout Breakdown (64-bit Integer)

A generated 64-bit ID is partitioned into three distinct sections:

```
 1 Bit      41 Bits               10 Bits          12 Bits
┌──────┬───────────────────────┬───────────────┬───────────────┐
│ Unused│  Timestamp (Delta)    │    Node ID    │ Sequence No.  │
└──────┴───────────────────────┴───────────────┴───────────────┘

```

* **Timestamp (41 bits):** The number of milliseconds elapsed since the custom `epoch`.
* **Node ID (10 bits):** Identifies which specific server/machine generated the ID (supports up to $2^{10} = 1024$ nodes, from 0 to 1023).
* **Sequence (12 bits):** A local counter for IDs generated within the exact same millisecond on the same node (supports up to $2^{12} = 4096$ IDs/ms per node).

---

### 2. How the Bitwise Operations Work

* **`<<` (Bit Shift):** Shifts the numbers to their assigned bit positions so they don't overlap.
* `(ts - epoch) << 22`: Moves the timestamp 22 bits to the left (10 node bits + 12 sequence bits).
* `s.nodeID << 12`: Moves the Node ID 12 bits to the left (above the sequence bits).


* **`|` (Bitwise OR):** Combines the three shifted bit groups into a single 64-bit integer.

---

### 3. Why is `epoch` set to `1704067200000`?

`1704067200000` is the Unix timestamp in milliseconds for **January 1, 2024, 00:00:00 UTC**.

#### Why use a custom epoch instead of standard Unix epoch (1970)?

1. **Prevents Bit Overflow:** 41 bits can only store up to $2^{41} - 1 = 2,199,023,255,551$ milliseconds ($\approx 69.7$ years).
2. **Extends ID Lifetime:** If you measured time from 1970, 41 bits would have expired in 2039. By setting a custom epoch near project creation (2024), your system's ID generator will safely run for **~69 years from 2024 (until ~2093)** without overflowing.

---

### 4. What `-1 ^ (-1 << bits)` Means

This bitwise trick calculates the maximum value for a given number of bits without hardcoding numbers:

* `-1` in binary (two's complement) is all `1`s.
* `-1 << 10` shifts ten `0`s in from the right.
* `^` (XOR) flips the bottom 10 bits to `1`s, yielding `1111111111` in binary, which equals **`1023`**.

---
Without mu, two concurrent POST /shorten in same ms read same sequence → duplicate ID → DB UNIQUE collision. Mutex makes check-and-increment atomic.

Hence NextID() -> was made mutex protected. (thread safe)