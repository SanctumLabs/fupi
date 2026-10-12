# Fupi

[![License](https://img.shields.io/github/license/sanctumlabs/fupi)](https://github.com/sanctumlabs/fupi/blob/main/LICENSE)
[![Version](https://img.shields.io/github/v/release/sanctumlabs/fupi?color=%235351FB&label=version)](https://github.com/sanctumlabs/fupi/releases)
[![CI](https://github.com/sanctumlabs/fupi/actions/workflows/ci.yml/badge.svg)](https://github.com/sanctumlabs/fupi/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/sanctumlabs/fupi/branch/develop/graph/badge.svg?token=RNg0UoESug)](https://codecov.io/gh/sanctumlabs/fupi)
[![Go](https://img.shields.io/badge/Go-1.18-blue.svg)](https://go.dev/)
[![Codacy Badge](https://app.codacy.com/project/badge/Grade/be035defd2d44675bddf744a88d1a2d5)](https://www.codacy.com/gh/SanctumLabs/fupi/dashboard?utm_source=github.com&amp;utm_medium=referral&amp;utm_content=SanctumLabs/fupi&amp;utm_campaign=Badge_Grade)

Simple URL Shortner Service

## Getting started

Ensure you have the following setup on you local development environment:

### [Go 1.25](https://go.dev/)

This is the programming language used to build the application. You will require this installed in order to install dependencies and run the application.

### [Docker](https://www.docker.com/)

The application is packaged & run in a Docker container. The services it depends on (Postgres, Redis, Kafka, ELK, Prometheus and Grafana) run locally in Docker too, either as high-availability clusters or as single nodes, as described in [Local infrastructure](./docs/LocalInfrastructure.md). If you prefer, you can install these services directly on your development machine instead. The API itself is built into a hardened image (`make build.docker`) and can run beside those services with `make infra.app.up`; see [Deployment](./docs/Deployment.md).

## Running the application

First install the required dependencies, this can be done using [make](https://www.gnu.org/software/make/) with helpful commands already available [here](./Makefile) or can be done using go cli tool:

```bash
make install
# or
go mod download
```

> Either option will work to setup the dependencies

Second, setup the environmant variables that the application will use. There are some defaults set up in [.env.sample](./.env.sample) and they can be used to setup the environment variables specific to how you want the application to run.

```bash
cp .env.sample .env
```

> This will copy over those environment variables. Afterwards, you can set them up accordingly.

Next step is to run the services the application needs to communicate with: Postgres, Redis and Kafka. If you have installed these locally, you can run them in separate terminal sessions. If not, use Docker (preferred); the lightest option is single-node mode:

```bash
make infra.core.up MODE=single
# or the high-availability topology
make infra.core.up
```

> Nothing starts without a profile, so a bare `docker compose up` does nothing. See [Local infrastructure](./docs/LocalInfrastructure.md) for every stack, mode and debugging tip. Stop the services with `make infra.core.down`.

Depending on which terminal session you are using to run the above steps(if all are in the same terminal session), you can continue to run the application as below:

```bash
go run app/cmd/main.go
# or using make
make run
```

> This will boot up the application with the provided environment variables. Check that it is ready with `curl -s localhost:8085/health/ready`; Postgres is required, Redis is optional. Apply the database migrations from the app's own migrator with `go run ./app/cmd/migrator` (or `make run.with.migrations`). See [Running the app against the stack](./docs/LocalInfrastructure.md#running-the-app-against-the-stack).

### Live reloading

You can optionally run the application with live reloading set using [air](https://github.com/cosmtrek/air). First install Air following the instructions provided in the attached link.
Then run with the below command in the root of the project:

```bash
air
```

That should be it.

## Testing the application

Running tests can be done with:

```bash
make test
# or
go test ./...
```

> This will run the unit tests in the application

If you want to see coverage you can do that with:

```bash
make test-coverage

# or
go test -tags testing -v -cover -covermode=atomic -coverprofile=coverage.out ./...
```

A coverage file will be generated `coverage.out`

## Linting

There are futher several useful commands that can be used for the application to perform linting, these can be conviniently setup with make:

```bash
make setup
```

> This will run the `setup-linting` & `setup-trivy` make commands which will setuop golangci-lint and trivy binaries in the [bin](./bin) directory.

Other useful commands can be found in the [Makefile](./Makefile).

## Deployment

Deployment instructions can be found [here](./docs/Deployment.md)

## Architecture

Architecture can be found [here](./docs/Architecture.md)

## Versioning

[SemVer](https://semver.org/) is used for versioning. For the versions available, see the [tags](https://github.com/SanctumLabs/fupi/tags) in this repository.

## License

View the project license [here](./LICENSE)
