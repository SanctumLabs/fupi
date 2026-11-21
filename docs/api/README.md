# API Collections

This directory holds a [Yaak](https://yaak.app) workspace for calling the Fupi API by hand while developing locally: registering a user, logging in, creating and listing short URLs, following a redirect, and checking the API and worker probes. It is a convenience for local testing. The source of truth for the API itself is the OpenAPI spec served at <http://localhost:8085/docs/> (see [API.md](../API.md)).

## What's in the collection

The workspace is called **Fupi API** and has three folders:

| Folder | Request | Calls | Auth |
|---|---|---|---|
| Identity | Register | `POST {API_BASE_URL_V1}/auth/register` | none |
| Identity | Login | `POST {API_BASE_URL_V1}/auth/login` | none |
| Identity | Verify | `GET {API_BASE_URL_V1}/auth/verify?v=<token>` | none |
| Url | Create Short URL | `POST {API_BASE_URL_V1}/urls` | bearer |
| Url | List Short URLs | `GET {API_BASE_URL_V1}/urls` | bearer |
| Url | Redirect | `GET {API_BASE_URL}/<code>` | none |
| Probes | App Health / App Ready | `GET {API_BASE_URL}/health`, `/health/ready` | none |
| Probes | Worker Health / Worker Ready | `GET {WORKER_HOST}/health`, `/health/ready` | none |

Not covered yet: token refresh (`POST /auth/oauth/token`), get, update and delete by id (`/urls/:id`), and the admin-only reinstate (`POST /urls/:id/reinstate`). Add them in Yaak and commit the new files (see [Changing the collection](#changing-the-collection)).

## How the files are laid out

[api_collections](./api_collections/) is a Yaak [directory sync](https://yaak.app/docs/using-yaak/local-directory-git-sync) folder. Yaak writes one YAML file per resource, named `yaak.<id>.yaml`, and the prefix of the id says what the file is:

| Prefix | Resource |
|---|---|
| `wk_` | the workspace (one file) |
| `ev_` | an environment |
| `fl_` | a folder |
| `rq_` | an HTTP request |

Edit these files through Yaak, not by hand. Yaak writes changes made in the app to the folder straight away, and asks for confirmation before applying changes that arrive in the folder from somewhere else (for example from `git pull`).

## Setting up

### 1. Run the API

Start the services and the API as described in the [README](../../README.md#running-the-application) and [LocalInfrastructure.md](../LocalInfrastructure.md). The shortest path is:

```bash
make infra.core.up MODE=single
```

```bash
make run.with.migrations
```

The API then listens on `http://localhost:8085`. The Worker probe requests need the worker as well (`make run.worker`), which serves its probes on port `8086` (`WORKER_HTTP_PORT`).

### 2. Open the workspace in Yaak

1. Install Yaak from <https://yaak.app>.
2. In Yaak, open the workspace menu and choose **New Workspace → Open Folder**.
3. Select `docs/api/api_collections` in your clone of this repository.

Yaak imports the files and keeps the workspace synced with that folder. Don't create a new workspace and point its sync at the folder instead: Yaak only allows that for an empty directory.

### 3. Set the environment variables

The requests use three variables. The committed **Development** environment doesn't define them yet, so add them yourself in Yaak's environment editor:

| Variable | Local value |
|---|---|
| `API_BASE_URL` | `http://localhost:8085` |
| `API_BASE_URL_V1` | `http://localhost:8085/api/v1/fupi` |
| `WORKER_HOST` | `http://localhost:8086` |

Development is a *sharable* environment, so anything you add to it is written to `yaak.ev_EgiReDeBZT.yaml` and ends up in Git. That's what you want for these URLs, because the next person gets them for free. Never put a token, password or other secret in a sharable environment; keep those in an environment that isn't sharable, which Yaak doesn't sync.

### 4. Use your own access token

The Url requests authenticate with a bearer token. The tokens saved in the committed request files are encrypted with Yaak's `secure(…)` function using the author's workspace key, so they can't be read on another machine. They have also long expired: access tokens last 15 minutes. Replace them with your own:

1. Send **Register** (change the username and email if that user already exists).
2. Send **Login** and copy `access_token` from the response.
3. On **Create Short URL** and **List Short URLs**, open the **Auth** tab and paste the token as the Bearer token.

When the token expires (the API answers `401`), log in again and paste the new one. See Yaak's [secrets encryption](https://yaak.app/docs/using-yaak/secrets-encryption) docs for how the workspace key works.

## A typical session

1. **App Ready** should answer `200`. If it answers `503`, Postgres isn't reachable. A `degraded` body only means Redis is down, and the API still works.
2. **Register** creates the user. It answers `201` even though no email is sent (no email transport is configured; see [ADR-0009](../adr/0009-identity-notification-and-login-policy.md)).
3. **Verify** is optional, because an unverified user can still log in. With `ENVIRONMENT` set to `development` or `test`, the API logs the email it would have sent, `email not delivered: no email transport is configured`, and its `body` field holds the verification link. Open the link, or put its `v` value in the request's `v` query parameter. In any other environment the body isn't logged, because it carries a live token, so read the token from the database instead:

   ```bash
   make infra.psql
   ```

   ```sql
   SELECT verification_token FROM users WHERE email = 'janedoe@example.com';
   ```

   A token is valid for 15 minutes and can be used once.
4. **Login**, then paste the `access_token` into the Url requests (step 4 of [Setting up](#4-use-your-own-access-token)).
5. **Create Short URL**. The response holds `short_code` and `short_url`. Custom aliases are unique, so change or remove `custom_alias` in the body before creating a second URL, and keep `expires_on` in the future.
6. **List Short URLs** shows the URLs you own.
7. **Redirect**: replace the code at the end of the URL with your `short_code` or `custom_alias`. The API answers `302` with the target in `Location`. Yaak follows redirects by default, so to see the `302` itself, turn off **Follow redirects** in the request's settings.

## Changing the collection

- Make the change in Yaak. It writes the YAML files for you, so the diff to commit is whatever appears under `api_collections/`.
- Before committing, check the diff for values that only make sense on your machine, such as a short code or a verification token. Leave secrets in a non-sharable environment so they never reach the files.
- When a handler changes, update its request here so the collection keeps matching the API.
