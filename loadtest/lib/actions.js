// The single requests the scenarios are built from, so redirect, create, mixed and spike measure the same thing.
import http from 'k6/http';
import { check } from 'k6';
import { API, BASE_URL, bearer } from './config.js';
import { accessToken } from './auth.js';
import { expiresInDays, pickUrl } from './data.js';
import { rateLimited, unauthorized } from './metrics.js';

const known = http.expectedStatuses(302);
const unknown = http.expectedStatuses(404);

function note(res) {
  if (res.status === 401) {
    unauthorized.add(1);
  }
  if (res.status === 429) {
    rateLimited.add(1);
  }
}

// redirectCode requests one short code and checks the redirect goes where the seed says it should.
export function redirectCode(target) {
  const res = http.get(`${BASE_URL}/${target.short_code}`, { redirects: 0, responseCallback: known, tags: { name: 'GET /{code}' } });
  note(res);
  check(res, {
    'redirect is 302': (r) => r.status === 302,
    'redirect goes to the stored url': (r) => r.headers['Location'] === target.original_url,
  });
}

// redirectOnce is a Zipf-weighted redirect, or with probability missRatio a code that does not exist (the 404 path).
// The unknown code has 8 characters: the redirect only serves 3-12 character single segments, anything else needs a token.
export function redirectOnce(missRatio) {
  if (Math.random() < missRatio) {
    const code = `zz${String(Math.floor(Math.random() * 1e6)).padStart(6, '0')}`;
    const res = http.get(`${BASE_URL}/${code}`, { redirects: 0, responseCallback: unknown, tags: { name: 'GET /{code} (unknown)' } });
    check(res, { 'unknown code is 404': (r) => r.status === 404 });
    return;
  }
  redirectCode(pickUrl());
}

// withToken runs fn with a valid access token, or records a failed check when the refresh failed.
function withToken(fn) {
  const token = accessToken();
  if (token === null) {
    check(null, { 'token refresh succeeded': () => false });
    return;
  }
  fn(token);
}

export function createOnce() {
  withToken((token) => {
    const body = JSON.stringify({
      original_url: `https://example.com/load/${__VU}/${__ITER}/${Date.now()}`,
      expires_on: expiresInDays(30),
    });
    const res = http.post(`${API}/urls`, body, { ...bearer(token), tags: { name: 'POST /urls' } });
    note(res);
    check(res, {
      'create is 201': (r) => r.status === 201,
      'create returns a short code': (r) => r.status !== 201 || !!r.json('short_code'),
    });
  });
}

// ownUrls is the VU's last page of its own urls, from which update picks one.
let ownUrls = [];

// listOnce reads the caller's urls (and keeps them for updateOnce).
export function listOnce() {
  withToken((token) => {
    const res = http.get(`${API}/urls?size=20`, { ...bearer(token), tags: { name: 'GET /urls' } });
    note(res);
    check(res, { 'list is 200': (r) => r.status === 200 });
    if (res.status === 200) {
      ownUrls = res.json('records') || [];
    }
  });
}

// updateOnce changes the keywords of one of the caller's urls; it lists first when the VU has none yet.
export function updateOnce() {
  if (ownUrls.length === 0) {
    listOnce();
  }
  if (ownUrls.length === 0) {
    return;
  }
  withToken((token) => {
    const target = ownUrls[Math.floor(Math.random() * ownUrls.length)];
    const res = http.patch(`${API}/urls/${target.id}`, JSON.stringify({ keywords: [`k${Math.floor(Math.random() * 100)}`] }), { ...bearer(token), tags: { name: 'PATCH /urls/{id}' } });
    note(res);
    check(res, { 'update is 200': (r) => r.status === 200 });
  });
}
