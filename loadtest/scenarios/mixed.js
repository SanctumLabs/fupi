// A realistic traffic mix running in parallel at constant arrival rates: RATE is the total per second, split
// 95% redirects, 4% creates and 1% reads/updates of the caller's own urls (MIX_* override the shares). This is also the
// soak scenario: run it for hours at about 60-70% of what stress found.
//
//   make loadtest.run SCENARIO=mixed K6_ARGS="-e RATE=500 -e DURATION=30m"
//   make loadtest.soak RATE=500
import { maxDropped } from '../lib/config.js';
import { createOnce, listOnce, redirectOnce, updateOnce } from '../lib/actions.js';

const TOTAL = parseFloat(__ENV.RATE || '20');
const DURATION = __ENV.DURATION || '1m';
const MISS_RATIO = parseFloat(__ENV.MISS_RATIO || '0.05');
const MIX = {
  redirect: parseFloat(__ENV.MIX_REDIRECT || '0.95'),
  create: parseFloat(__ENV.MIX_CREATE || '0.04'),
  manage: parseFloat(__ENV.MIX_MANAGE || '0.01'),
};
const rate = (share) => Math.max(1, Math.round(TOTAL * share));

function scenario(exec, share, preVUs, maxVUs) {
  return {
    executor: 'constant-arrival-rate',
    exec,
    rate: rate(share),
    timeUnit: '1s',
    duration: DURATION,
    preAllocatedVUs: preVUs,
    maxVUs,
  };
}

export const options = {
  scenarios: {
    redirect: scenario('redirect', MIX.redirect, Math.ceil(TOTAL / 10) + 10, Math.ceil(TOTAL / 2) + 50),
    create: scenario('create', MIX.create, 10, 100),
    manage: scenario('manage', MIX.manage, 5, 50),
  },
  thresholds: {
    'http_req_failed{scenario:redirect}': ['rate<0.001'],
    'http_req_duration{scenario:redirect}': [`p(95)<${__ENV.P95_MS || 50}`],
    'http_req_duration{scenario:create}': ['p(95)<300'],
    'http_req_duration{scenario:manage}': ['p(95)<300'],
    'http_req_failed{scenario:create}': ['rate<0.001'],
    'http_req_failed{scenario:manage}': ['rate<0.001'],
    dropped_iterations: [`count<=${maxDropped(TOTAL, DURATION)}`],
    unauthorized: ['count==0'],
    checks: ['rate>0.999'],
  },
};

export function redirect() {
  redirectOnce(MISS_RATIO);
}

export function create() {
  createOnce();
}

// manage alternates reads and updates, so a VU's update has urls to pick from.
export function manage() {
  if (Math.random() < 0.5) {
    listOnce();
  } else {
    updateOnce();
  }
}
