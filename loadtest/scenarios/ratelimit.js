// Checks the rate limiter against the redirect group: one VU sends GROUP_MAX + EXTRA requests for an unknown short code
// (404 when allowed, 429 when limited; no DB hit beyond the absent-cache lookup) and checks how many got through. The
// API must run with the matching limit (RATE_LIMIT_REDIRECT_MAX and _WINDOW) and a fresh bucket: a second run inside
// the window finds the bucket empty and fails "allowed >= GROUP_MAX", which is how the script shows it detects one.
//
//   make loadtest.app.up MODE=single LOADTEST_LIMITS="... RATE_LIMIT_REDIRECT_MAX=200 RATE_LIMIT_REDIRECT_WINDOW=3600"
//   make loadtest.ratelimit GROUP_MAX=200 GROUP_WINDOW_S=3600
import http from 'k6/http';
import { check } from 'k6';
import { BASE_URL } from '../lib/config.js';
import { rateLimited } from '../lib/metrics.js';

const GROUP_MAX = parseInt(__ENV.GROUP_MAX || '100000', 10);
const GROUP_WINDOW_S = parseInt(__ENV.GROUP_WINDOW_S || '60', 10);
const EXTRA = parseInt(__ENV.EXTRA || '20', 10);

export const options = {
  scenarios: {
    ratelimit: {
      executor: 'per-vu-iterations',
      vus: 1,
      iterations: 1,
      maxDuration: '30m',
    },
  },
  thresholds: {
    checks: ['rate==1'],
  },
};

// 404 and 429 are the two answers the redirect group gives for an unknown code, so neither counts as a failed request.
const expected = http.expectedStatuses(404, 429);

// header reads a response header whatever its capitalisation.
function header(res, name) {
  const wanted = name.toLowerCase();
  const key = Object.keys(res.headers).find((k) => k.toLowerCase() === wanted);
  return key === undefined ? undefined : res.headers[key];
}

export default function () {
  let allowed = 0;
  let limited = 0;
  let incomplete = 0;
  const started = Date.now();

  for (let i = 0; i < GROUP_MAX + EXTRA; i++) {
    const res = http.get(`${BASE_URL}/zzzzzzzz`, { redirects: 0, responseCallback: expected, tags: { name: 'GET /{code} (ratelimit)' } });
    if (res.status === 404) {
      allowed++;
    } else if (res.status === 429) {
      limited++;
      rateLimited.add(1);
      if (!header(res, 'Retry-After') || !header(res, 'X-RateLimit-Limit')) {
        incomplete++;
      }
    }
  }

  const elapsed = (Date.now() - started) / 1000;
  const refilled = Math.ceil((elapsed * GROUP_MAX) / GROUP_WINDOW_S);
  console.log(`sent ${GROUP_MAX + EXTRA} in ${elapsed.toFixed(1)}s: allowed ${allowed}, limited ${limited}, refill allowance ${refilled}`);

  check(null, {
    'allowed >= GROUP_MAX': () => allowed >= GROUP_MAX,
    'allowed <= GROUP_MAX + refill during the run': () => allowed <= GROUP_MAX + refilled,
    'at least one 429': () => limited >= 1,
    'every 429 has Retry-After and X-RateLimit-Limit': () => incomplete === 0,
  });
}
