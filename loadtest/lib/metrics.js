import { Counter } from 'k6/metrics';

// 429s are counted apart from other failures so a rate limiter on the API is not mistaken for a broken endpoint.
export const rateLimited = new Counter('rate_limited');
export const tokenRefreshes = new Counter('token_refreshes');
// Requests the API rejected as unauthenticated: must stay 0 if the refresh logic is right.
export const unauthorized = new Counter('unauthorized');
