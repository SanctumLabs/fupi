// A viral link: Zipf-weighted redirect traffic runs at BASE_RATE for the whole test, and after LEAD an extra stream of
// up to SPIKE_RATE requests per second hits ONE code (HOT_CODE, default the first seeded url) for SPIKE_DURATION, then
// ends. Checks the cache under a stampede and what happens to the analytics buffer (watch the app's "access analytics
// buffer is full" log lines).
//
//   make loadtest.run SCENARIO=spike K6_ARGS="-e BASE_RATE=100 -e SPIKE_RATE=1000"
import { maxDropped, seconds } from '../lib/config.js';
import { seed } from '../lib/auth.js';
import { redirectCode, redirectOnce } from '../lib/actions.js';

const BASE_RATE = parseInt(__ENV.BASE_RATE || '100', 10);
const SPIKE_RATE = parseInt(__ENV.SPIKE_RATE || '1000', 10);
const SPIKE_DURATION = __ENV.SPIKE_DURATION || '1m';
const LEAD = __ENV.LEAD || '1m';
const RAMP = 5; // seconds up and down
const hot = seed.urls.find((u) => u.short_code === __ENV.HOT_CODE) || seed.urls[0];
const total = 2 * seconds(LEAD) + seconds(SPIKE_DURATION) + 2 * RAMP;

export const options = {
  scenarios: {
    baseline: {
      executor: 'constant-arrival-rate',
      exec: 'baseline',
      rate: BASE_RATE,
      timeUnit: '1s',
      duration: `${total}s`,
      preAllocatedVUs: Math.ceil(BASE_RATE / 10) + 10,
      maxVUs: Math.ceil(BASE_RATE / 2) + 50,
    },
    spike: {
      executor: 'ramping-arrival-rate',
      exec: 'spike',
      startTime: LEAD,
      startRate: 1,
      timeUnit: '1s',
      preAllocatedVUs: Math.ceil(SPIKE_RATE / 10) + 10,
      maxVUs: Math.ceil(SPIKE_RATE / 2) + 50,
      stages: [
        { target: SPIKE_RATE, duration: `${RAMP}s` },
        { target: SPIKE_RATE, duration: SPIKE_DURATION },
        { target: 1, duration: `${RAMP}s` },
      ],
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.001'],
    // the baseline traffic must not suffer while the hot code is hammered
    'http_req_duration{scenario:baseline}': [`p(95)<${__ENV.P95_MS || 100}`],
    'http_req_duration{scenario:spike}': [`p(95)<${__ENV.P95_MS || 100}`],
    dropped_iterations: [`count<=${maxDropped(SPIKE_RATE, SPIKE_DURATION) * 5}`],
    checks: ['rate>0.999'],
  },
};

export function baseline() {
  redirectOnce(0.05);
}

export function spike() {
  redirectCode(hot);
}
