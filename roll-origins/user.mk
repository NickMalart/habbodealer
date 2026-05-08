USERS NAME RESOLUTION REFERENCE (Shockwave)
=========================================

Goal
----
Resolve the trade partner name reliably when Incoming[104] TRADE_OPEN is received.

Key packets
-----------
1) Outgoing[61] G_USRS
   - Request the current room users list.
   - Raw payload example: "@}".

2) Incoming[28] USERS
   - Contains room-user entries.
   - Each entry includes a VL64 room index directly followed by username, then 0x02.
   - Important pattern from live packets:
     - cizM + Dubbo + 0x02
     - `m{M + MWebsedit + 0x02

3) Incoming[104] TRADE_OPEN
   - First field is trader room index in VL64.
   - Example: "`m{M" => 228276.

How matching works now
----------------------
1) On TRADE_OPEN, decode VL64 from packet start to get trader room index.
2) Immediately send G_USRS (and GETSPACENODEUSERS fallback) to refresh room users.
3) Parse USERS[28] by scanning raw bytes:
   - For each byte offset, test VL64 length.
   - Decode candidate room index.
   - Read subsequent ASCII username chars [A-Za-z0-9_-].
   - Require terminating 0x02 after the name.
4) Store map: roomIndex -> name.
5) Resolve trade partner using decoded room index from TRADE_OPEN.

Why old approach failed
-----------------------
- Token-only matching was unreliable:
  - USERS legacy 4-byte token near name is not guaranteed to be the same thing as TRADE_OPEN's first VL64 id field in all cases.
- Splitting USERS by 0x02 and reading tail fields missed real id/name pairs in some packet layouts.

Current reliability strategy
----------------------------
- Primary: TRADE_OPEN VL64 room index -> USERS[28] index map.
- Secondary: token fallback remains for compatibility.
- Additional robustness: short wait loop after trade-open to allow refreshed USERS packet to arrive.

Quick diagnostic checklist
--------------------------
1) Confirm this appears on trade-open:
   [TRADE_OPEN] decoded trader room index <n> from VL64
2) Confirm USERS logs include same roomIndex:
   [USERS28] ... name="<name>" roomIndex=<n>
3) Confirm resolution log:
   [TRADE_OPEN] resolved name "<name>" from room index <n>
4) If missing:
   - Verify Outgoing[61] G_USRS was sent.
   - Verify Incoming[28] was received after trade-open.

Notes
-----
- Header values used in this project:
  - 28: USERS (incoming)
  - 61: G_USRS (outgoing request)
  - 104: TRADE_OPEN (incoming)
