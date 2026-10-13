// Per-VU access tokens. Access tokens last 15 minutes and every refresh returns a new access AND refresh token, so each
// VU keeps its own session, starting from the seeded user's tokens, and refreshes shortly before the access token
// expires. Module state is per VU in k6, so sessions are never shared.
import http from 'k6/http';
import encoding from 'k6/encoding';
import { API, JSON_HEADERS, SEED_FILE } from './config.js';
import { tokenRefreshes } from './metrics.js';

const REFRESH_MARGIN_S = parseInt(__ENV.REFRESH_MARGIN_S || '120', 10);

export const seed = JSON.parse(open(SEED_FILE));

let session = null;

// expiry returns the exp claim (unix seconds) of a JWT.
function expiry(token) {
  const claims = JSON.parse(encoding.b64decode(token.split('.')[1], 'rawurl', 's'));
  return claims.exp;
}

function refresh() {
  const res = http.post(
    `${API}/auth/oauth/token?grant_type=refresh_token&refresh_token=${session.refresh}`,
    null,
    { headers: JSON_HEADERS, tags: { name: 'POST /auth/oauth/token' } },
  );
  tokenRefreshes.add(1);
  if (res.status !== 200) {
    return false;
  }
  session.access = res.json('access_token');
  session.refresh = res.json('refresh_token');
  session.exp = expiry(session.access);
  return true;
}

// accessToken returns a token that is valid for at least the refresh margin, or null when the refresh failed.
export function accessToken() {
  if (session === null) {
    const user = seed.users[(__VU - 1) % seed.users.length];
    session = { access: user.access_token, refresh: user.refresh_token, exp: expiry(user.access_token) };
  }
  if (session.exp - Date.now() / 1000 < REFRESH_MARGIN_S && !refresh()) {
    return null;
  }
  return session.access;
}
