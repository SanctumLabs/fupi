import { seed } from './auth.js';

const SKEW = parseFloat(__ENV.ZIPF_S || '1.0');

// Cumulative Zipf weights over the seeded urls, so a few codes are hot and most are cold, as real traffic is.
const cumulative = [];
let total = 0;
for (let rank = 1; rank <= seed.urls.length; rank++) {
  total += 1 / Math.pow(rank, SKEW);
  cumulative.push(total);
}

// pickUrl returns a seeded url, Zipf-weighted by its position in the seed file.
export function pickUrl() {
  const target = Math.random() * total;
  let lo = 0;
  let hi = cumulative.length - 1;
  while (lo < hi) {
    const mid = (lo + hi) >> 1;
    if (cumulative[mid] < target) {
      lo = mid + 1;
    } else {
      hi = mid;
    }
  }
  return seed.urls[lo];
}

// expiresInDays formats a timestamp the API accepts (RFC 3339, UTC).
export function expiresInDays(days) {
  return new Date(Date.now() + days * 24 * 3600 * 1000).toISOString().replace(/\.\d+Z$/, 'Z');
}
