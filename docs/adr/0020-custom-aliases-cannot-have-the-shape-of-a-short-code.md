---
status: accepted
---

# Custom aliases cannot have the shape of a short code

Short codes and custom aliases share one redirect namespace, `GET /:code`, but each is unique only within its own column (`idx_urls_short_code`, `idx_urls_custom_alias`). `Resolve` tries the short code first and the alias second. So an alias that equals a code generated later stops resolving, and its visitors land on someone else's target (#359).

**The two sets are made disjoint by shape.** A short code is 6 to 10 letters and digits (`ShortCode`). The 6-character codes were issued before ADR-0019, the 7-character ones are issued now, and 8 or more is the exhaustion plan. `url.Create` therefore rejects a new custom alias that `NewShortCode` would accept, with `ErrCustomAliasLikeShortCode` (400). Aliases of 3 to 5 or 11 to 12 characters, or any alias containing a dash, can never equal a code. The rule reuses `NewShortCode` itself, so it follows the code shape if that ever changes.

**Only new aliases are checked.** The rule lives in `Create`, not in `NewCustomAlias`, which also hydrates stored rows: an alias saved before this rule still loads and still resolves unless a generated code takes its value. To list those aliases:

```sql
SELECT id, user_id, custom_alias FROM urls
WHERE custom_alias ~ '^[A-Za-z0-9]{6,10}$' AND deleted_at IS NULL;
```

A 6-character one can never be hit, because new codes are 7 characters. A 7-character one can be hit only when the keyed permutation lands on that value; the chance is about one in 62^7 per code issued for each such alias. They are left in place. If any exist in production, ask their owners to recreate them with a dash.

## Considered options

- **Reject only the exact current length (7)** — it leaves the 6-character codes already issued and the 8-character codes of the exhaustion plan unprotected.
- **One table for codes and aliases with a single unique index** — the strongest guarantee, but a migration and a change to every read and write path to fix what a domain rule fixes.
- **Check the other column at write time, and have the generator skip values used as aliases** — racy across instances without locking, and it adds a read to every create.
- **Serve aliases under their own prefix (`/a/:alias`)** — removes the overlap but changes the public URL shape that users choose an alias for.
