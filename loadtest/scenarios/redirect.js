// The public redirect at a constant arrival rate (open model: a slow server does not reduce the offered load).
// Codes are Zipf-weighted; MISS_RATIO of requests use a code that does not exist, to exercise the 404 path. The first
// request for each code is a cache miss; later ones are hits.
//
//   make loadtest.run SCENARIO=redirect K6_ARGS="-e RATE=50 -e DURATION=5m"
import { maxDropped } from '../lib/config.js';
import { redirectOnce } from '../lib/actions.js';

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

export default function () {
  redirectOnce(MISS_RATIO);
}
