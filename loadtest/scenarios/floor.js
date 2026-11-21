// The cheapest request the API serves: a management route without a token, rejected by the auth middleware before it
// touches Postgres or Redis. Its latency is the floor for the load generator, the Docker network, the HTTP server and
// the access log, so a tail on the real scenarios that is also here is not the application's code.
import http from 'k6/http';
import { check } from 'k6';
import { API } from '../lib/config.js';

export const options = {
  scenarios: {
    floor: {
      executor: 'constant-arrival-rate',
      rate: parseInt(__ENV.RATE || '600', 10),
      timeUnit: '1s',
      duration: __ENV.DURATION || '30s',
      preAllocatedVUs: parseInt(__ENV.PRE_VUS || '100', 10),
      maxVUs: parseInt(__ENV.MAX_VUS || '1000', 10),
    },
  },
};

const rejected = http.expectedStatuses(401);

export default function () {
  const res = http.get(`${API}/urls`, { responseCallback: rejected, tags: { name: 'GET /urls (no token)' } });
  check(res, { 'unauthenticated is 401': (r) => r.status === 401 });
}
