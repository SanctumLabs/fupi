// Creating short urls as authenticated users at a constant arrival rate. Each VU is pinned to a seeded user and
// refreshes its token before it expires (see lib/auth.js).
//
//   make loadtest.run SCENARIO=create K6_ARGS="-e RATE=2 -e DURATION=20m"
import { maxDropped } from '../lib/config.js';
import { createOnce } from '../lib/actions.js';

const RATE = parseInt(__ENV.RATE || '2', 10);
const DURATION = __ENV.DURATION || '1m';

export const options = {
  scenarios: {
    create: {
      executor: 'constant-arrival-rate',
      rate: RATE,
      timeUnit: '1s',
      duration: DURATION,
      preAllocatedVUs: parseInt(__ENV.PRE_VUS || '10', 10),
      maxVUs: parseInt(__ENV.MAX_VUS || '100', 10),
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.001'],
    http_req_duration: [`p(95)<${__ENV.P95_MS || 300}`],
    dropped_iterations: [`count<=${maxDropped(RATE, DURATION)}`],
    unauthorized: ['count==0'],
    checks: ['rate>0.999'],
  },
};

export default function () {
  createOnce();
}
