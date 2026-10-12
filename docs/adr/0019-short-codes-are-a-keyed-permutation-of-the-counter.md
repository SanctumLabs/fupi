---
status: accepted
---

# Short codes are a keyed permutation of the sequence counter, so they cannot be enumerated

The short code generator claims blocks of `short_code_counter` (migration 000001, `INCREMENT BY 1000`) so that API instances issue unique codes with one database round trip per thousand codes and no retries. It first encoded the raw counter in base62 (`000001`, `000002`, …), which let anyone walk `GET /000001`, `/000002`, … and collect every target ever shortened, and told them how many links exist (#358).

**The counter is permuted before it is encoded.** Each counter value goes through a keyed bijection on `[0, 62^7)` and the result is encoded as exactly 7 base62 characters (`app/internal/adapters/postgres/short_code_permutation.go`). The bijection is a balanced Feistel network over 42 bits (21-bit halves) with HMAC-SHA256 as the round function, restricted to the domain by cycle walking: an output at or above 62^7 is permuted again until it lands inside, which keeps the restriction a bijection. Unique counter values therefore still give unique codes, with no collision check and no retry, and the block allocator is unchanged.

**Ten rounds, not four.** An attacker can create links back to back and get codes for counter values they know are consecutive, which is close to known-plaintext. Over a domain this small the textbook four rounds only resist a few thousand such observations; ten rounds push that bound towards the 2^21 the half size allows. The goal is that a code cannot be predicted from the codes someone has seen, not secrecy of the link targets themselves: a link is still public to whoever has its code.

**The key is `SHORT_CODE_KEY` and it never changes.** A different key maps later counter values onto codes that an earlier key may already have issued, so rotating it would collide. Outside development and test the API refuses to start with the development default or with a key shorter than 32 characters, like `AUTH_SECRET`. A leaked key does not break uniqueness; it makes codes predictable again, and the remedy is a new code length with a new key (below), not a rotation.

**Codes issued before this change keep working.** They are 6 characters long and every new code is 7, so the two sets cannot collide, and stored codes are looked up as stored.

**Exhaustion.** 62^7 is about 3.5 × 10¹² codes. When `short_code_counter` reaches it the generator returns an error instead of wrapping. The plan then is a second permutation over `[0, 62^8)` with its own key for 8-character codes: those cannot collide with the 7-character ones, again by length.

## Considered options

- **Random codes with retry on a unique violation** — no key to manage, but it drops the allocator's no-collision guarantee and adds a retry path to every create, with retries growing as the space fills.
- **The counter plus a random suffix** — unique and unpredictable in the suffix, but the prefix still leaks order and volume, and codes get longer.
- **A format-preserving encryption standard (NIST FF1/FF3-1)** — the rigorous version of the same idea, but it needs a third-party library for a domain where an HMAC Feistel with enough rounds meets the goal.
- **Rate limiting redirects instead** — worth having anyway, but it slows enumeration down rather than preventing it.
