// Seeds load-test users and short urls from k6, for when only the k6 image is available. The Go seeder
// (loadtest/seed/main.go) does the same and takes the same settings. Both write the file the scenarios read.
//
//   k6 run -e BASE_URL=http://fupi-app:8085 -e USERS=5 -e URLS_PER_USER=20 loadtest/seed/seed.js
import http from 'k6/http';
import { sleep } from 'k6';
import { API, JSON_HEADERS, BASE_URL, SEED_FILE, bearer } from '../lib/config.js';

const USERS = parseInt(__ENV.USERS || '5', 10);
const URLS_PER_USER = parseInt(__ENV.URLS_PER_USER || '20', 10);
const PASSWORD = 'Loadtest-Passw0rd!';

export const options = { vus: 1, iterations: 1 };

// post waits out a 429 for as long as the server asks, so a rate limiter slows the seed down instead of failing it.
function post(path, payload, params, want) {
  for (let attempt = 0; attempt < 5; attempt++) {
    const res = http.post(`${API}${path}`, JSON.stringify(payload), params);
    if (res.status === 429) {
      sleep(Math.max(parseInt(res.headers['Retry-After'] || '1', 10), 1));
      continue;
    }
    if (res.status !== want) {
      throw new Error(`POST ${path}: status ${res.status}: ${res.body}`);
    }
    return res.json();
  }
  throw new Error(`POST ${path}: still rate limited after 5 attempts`);
}

export function setup() {
  const runId = Date.now().toString(36);
  const expires = new Date(Date.now() + 30 * 24 * 3600 * 1000).toISOString().replace(/\.\d+Z$/, 'Z');
  const result = { base_url: BASE_URL, users: [], urls: [] };

  for (let i = 0; i < USERS; i++) {
    const email = `lt-${runId}-${i}@fupi.test`;
    post('/auth/register', { username: `lt${runId}${i}`, first_name: 'Load', email, password: PASSWORD }, { headers: JSON_HEADERS }, 201);
    const login = post('/auth/login', { email, password: PASSWORD }, { headers: JSON_HEADERS }, 200);
    result.users.push({ email, password: PASSWORD, access_token: login.access_token, refresh_token: login.refresh_token });

    for (let j = 0; j < URLS_PER_USER; j++) {
      const target = `https://example.com/loadtest/${runId}/${i}/${j}`;
      const created = post('/urls', { original_url: target, expires_on: expires }, bearer(login.access_token), 201);
      result.urls.push({ short_code: created.short_code, original_url: target, owner: email });
    }
  }
  return result;
}

export default function () {}

// The seed result leaves setup() through the end-of-test summary.
export function handleSummary(data) {
  return { [SEED_FILE]: JSON.stringify(data.setup_data, null, 2) };
}
