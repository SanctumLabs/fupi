// The public redirect at a constant arrival rate (open model: a slow server does not reduce the offered load).
// Codes are Zipf-weighted; MISS_RATIO of requests use a code that does not exist, to exercise the 404 path. The first
// request for each code is a cache miss; later ones are hits.
//
//   make loadtest.run SCENARIO=redirect K6_ARGS="-e RATE=50 -e DURATION=5m"
import http from 'k6/http';
import { check } from 'k6';
import { BASE_URL, maxDropped } from '../lib/config.js';
import { pickUrl } from '../lib/data.js';
import { rateLimited } from '../lib/metrics.js';

const MISS_RATIO = parseFloat(__ENV.MISS_RATIO || '0.05');

const RATE = parseInt(__ENV.RATE || '10', 10);
const DURATION = __ENV.DURATION || '1m';

export const options = {
  scenarios: {
    redirect: {
      executor: 'constant-arrival-rate',
      rate: RATE,
      timeUnit: '1s',
      duration: DURATION,
      preAllocatedVUs: parseInt(__ENV.PRE_VUS || '20', 10),
      maxVUs: parseInt(__ENV.MAX_VUS || '200', 10),
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.001'],
    http_req_duration: [`p(95)<${__ENV.P95_MS || 50}`],
    dropped_iterations: [`count<=${maxDropped(RATE, DURATION)}`],
    checks: ['rate>0.999'],
  },
};

// 8 characters: the redirect only serves 3-12 character single segments, anything else needs a token.
const known = http.expectedStatuses(302);
const unknown = http.expectedStatuses(404);

export default function () {
  if (Math.random() < MISS_RATIO) {
    const res = http.get(`${BASE_URL}/zz${String(Math.floor(Math.random() * 1e6)).padStart(6, '0')}`, { redirects: 0, responseCallback: unknown, tags: { name: 'GET /{code} (unknown)' } });
    check(res, { 'unknown code is 404': (r) => r.status === 404 });
    return;
  }

  const target = pickUrl();
  const res = http.get(`${BASE_URL}/${target.short_code}`, { redirects: 0, responseCallback: known, tags: { name: 'GET /{code}' } });
  if (res.status === 429) {
    rateLimited.add(1);
  }
  check(res, {
    'redirect is 302': (r) => r.status === 302,
    'redirect goes to the stored url': (r) => r.headers['Location'] === target.original_url,
  });
}
