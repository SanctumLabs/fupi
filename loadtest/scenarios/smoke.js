// Smoke test: one VU walks the redirect, the create path and a token refresh against seeded data. It proves the scripts
// and the seed are valid; it is not a measurement. Runs in CI.
import http from 'k6/http';
import { check } from 'k6';
import { API, BASE_URL, JSON_HEADERS, SEED_FILE, bearer } from '../lib/config.js';

const seed = JSON.parse(open(SEED_FILE));

export const options = {
  vus: 1,
  iterations: parseInt(__ENV.ITERATIONS || '5', 10),
  thresholds: {
    checks: ['rate==1'],
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<500'],
  },
};

// Seeded access tokens last 15 minutes, so trade each user's refresh token (valid for 24 hours) for a fresh one.
export function setup() {
  return seed.users.map((u) => {
    const res = http.post(`${API}/auth/oauth/token?grant_type=refresh_token&refresh_token=${u.refresh_token}`, null, { headers: JSON_HEADERS });
    if (res.status !== 200) {
      throw new Error(`cannot refresh the token of ${u.email} (status ${res.status}): re-run the seed`);
    }
    return { ...u, access_token: res.json('access_token') };
  });
}

export default function (users) {
  const target = seed.urls[__ITER % seed.urls.length];
  const redirect = http.get(`${BASE_URL}/${target.short_code}`, { redirects: 0 });
  check(redirect, {
    'redirect is 302': (r) => r.status === 302,
    'redirect goes to the stored url': (r) => r.headers['Location'] === target.original_url,
  });

  const missing = http.get(`${BASE_URL}/zzzzzzzz`, { redirects: 0, responseCallback: http.expectedStatuses(404) });
  check(missing, { 'unknown code is 404': (r) => r.status === 404 });

  const user = users[__VU % users.length];
  const expires = new Date(Date.now() + 7 * 24 * 3600 * 1000).toISOString().replace(/\.\d+Z$/, 'Z');
  const created = http.post(
    `${API}/urls`,
    JSON.stringify({ original_url: `https://example.com/smoke/${__VU}/${__ITER}/${Date.now()}`, expires_on: expires }),
    bearer(user.access_token),
  );
  check(created, { 'create is 201': (r) => r.status === 201 });

  const refreshed = http.post(`${API}/auth/oauth/token?grant_type=refresh_token&refresh_token=${user.refresh_token}`, null, { headers: JSON_HEADERS });
  check(refreshed, {
    'refresh is 200': (r) => r.status === 200,
    'refresh issues a new access token': (r) => !!r.json('access_token'),
  });
}
