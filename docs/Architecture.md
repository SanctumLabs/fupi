# Architecture

There are a couple of components of the overal architecture that are just as important as the application itself. They are needed for the application to run & function as intended. This document will explain the components and further explain the internals of the architecture of the application.

Below is a diagram with the components at a high level.

![architecture](./images/Fupi-Architecture.png)

## Cache

The application caches redirect requests on shortened urls (Redis, via [app/pkg/infra/cache/redis](../app/pkg/infra/cache/redis/)), preventing a round trip to the database. If there is a cache miss, the application will fetch the url from the database given the short code and redirect the user to the original url, then save this in the cache. Subsequent requests for a given url given a short code will be served from a cache, therefore speeding up these requests.

## Database

PostgreSQL is the system of record for users, urls, keywords and the transactional outbox. Schema changes are SQL migrations in [app/internal/adapters/postgres/migrations](../app/internal/adapters/postgres/migrations/) that run from the `migrator` command, and queries are generated with [sqlc](https://sqlc.dev) from [app/internal/adapters/postgres/sql](../app/internal/adapters/postgres/sql/). See [ADR-0015](./adr/0015-postgres-is-the-only-required-dependency.md).

## Application

The application is split into two bounded contexts, Identity and URL, each built in layers:

- **domain**: aggregates, value objects and domain events. It has no infrastructure dependencies.
- **application**: use cases that orchestrate the domain through ports.
- **ports**: interfaces the application needs from the outside world (datastores, cache, event bus, notifier, token issuer).
- **adapters**: implementations of the ports (PostgreSQL, Kafka, JWT, notifications).
- **api**: the HTTP layer that translates requests to use cases and errors to responses.

Domain events are written to an outbox table in the same transaction as the state change and published to the broker by the outbox relay (see [ADR-0011](./adr/0011-single-broker-agnostic-outbox.md) and [ADR-0018](./adr/0018-the-outbox-relay-is-one-leader-elected-polling-worker-over-franz-go.md)).

This follows the [Hexagonal Architecture](https://en.wikipedia.org/wiki/Hexagonal_architecture_(software)) which strongly emphasizes the decoupling of several components to allow for the intent of the application to be clear and precise. Swapping storage, the broker or the token issuer means writing a new adapter, with no change to the domain or the use cases.

Here is a directory structure of the application:

```plain
app
├── api
│   ├── middleware
│   ├── probes
│   └── v1
│       ├── identity
│       └── url
├── cmd
│   ├── main.go
│   ├── migrator
│   └── worker
├── config
├── internal
│   ├── adapters
│   │   ├── jwtauth
│   │   ├── kafka
│   │   ├── notifications
│   │   └── postgres
│   │       ├── identity
│   │       ├── migrations
│   │       ├── outbox
│   │       ├── sql
│   │       └── url
│   ├── application
│   │   ├── identity
│   │   ├── outbox
│   │   └── url
│   ├── core
│   │   ├── entity
│   │   └── ports
│   ├── domain
│   │   ├── identity
│   │   └── url
│   ├── pkg
│   └── ports
├── pkg
│   ├── errdefs
│   ├── infra
│   │   ├── cache
│   │   ├── database
│   │   ├── logger
│   │   ├── monitoring
│   │   ├── queue
│   │   ├── server
│   │   ├── telemetry
│   │   └── tracing
│   ├── jwt
│   ├── utils
│   └── validators
└── test
    └── e2e
```

> This directory structure can be found in the [app](../app/) directory and follows closely [Go's](https://github.com/golang-standards/project-layout) recommended project layout

The [internal](../app/internal/) is where the _heart_ or the intent of the application lies.

The [domain](../app/internal/domain/) and [application](../app/internal/application/) packages contain the entities and use cases, which communicate that the application handles urls and users. This makes it clear what its actual purpose or intent is.

The [adapters](../app/internal/adapters/) package contains the implementations of the ports: the [PostgreSQL](../app/internal/adapters/postgres/) persistence layer, [Kafka](../app/internal/adapters/kafka/) publishing, [JWT](../app/internal/adapters/jwtauth/) tokens and [notifications](../app/internal/adapters/notifications/). If there is a change of how storage is handled, this is where the change is done.

[pkg](../app/pkg) contains utilities that can be used across any layer, including the shared infrastructure code in [pkg/infra](../app/pkg/infra/) (HTTP server, logging, telemetry, cache, queue and database clients). [config](../app/config/) contains configuration code for setting up the application. The [api](../app/api) package contains the API layer that handles HTTP communication with external clients.

Finally, the [cmd](../app/cmd/) package contains the entry points (the API, the outbox `worker` and the `migrator`) where everything is wired up together to form the **Fupi** application.
