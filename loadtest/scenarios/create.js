// Creating short urls as authenticated users at a constant arrival rate. Each VU is pinned to a seeded user and
// refreshes its token before it expires (see lib/auth.js).
//
//   make loadtest.run SCENARIO=create K6_ARGS="-e RATE=2 -e DURATION=20m"
import http from 'k6/http';
import { check } from 'k6';
import { API, bearer } from '../lib/config.js';
import { accessToken } from '../lib/auth.js';
import { expiresInDays } from '../lib/data.js';
import { rateLimited, unauthorized } from '../lib/metrics.js';

export const options = {
  scenarios: {
    create: {
      executor: 'constant-arrival-rate',
      rate: parseInt(__ENV.RATE || '2', 10),
      timeUnit: '1s',
      duration: __ENV.DURATION || '1m',
      preAllocatedVUs: parseInt(__ENV.PRE_VUS || '10', 10),
      maxVUs: parseInt(__ENV.MAX_VUS || '100', 10),
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.001'],
    http_req_duration: ['p(95)<300'],
    dropped_iterations: ['count==0'],
    unauthorized: ['count==0'],
    checks: ['rate>0.999'],
  },
};

export default function () {
  const token = accessToken();
  if (token === null) {
    check(null, { 'token refresh succeeded': () => false });
    return;
  }

  const body = JSON.stringify({
    original_url: `https://example.com/load/${__VU}/${__ITER}/${Date.now()}`,
    expires_on: expiresInDays(30),
  });
  const res = http.post(`${API}/urls`, body, { ...bearer(token), tags: { name: 'POST /urls' } });
  if (res.status === 401) {
    unauthorized.add(1);
  }
  if (res.status === 429) {
    rateLimited.add(1);
  }
  check(res, {
    'create is 201': (r) => r.status === 201,
    'create returns a short code': (r) => r.status !== 201 || !!r.json('short_code'),
  });
}
