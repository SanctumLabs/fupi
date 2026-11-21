# Curtz

[![License](https://img.shields.io/github/license/sanctumlabs/curtz)](https://github.com/sanctumlabs/curtz/blob/main/LICENSE)
[![Version](https://img.shields.io/github/v/release/sanctumlabs/curtz?color=%235351FB&label=version)](https://github.com/sanctumlabs/curtz/releases)
[![Tests](https://github.com/sanctumlabs/curtz/actions/workflows/tests.yml/badge.svg)](https://github.com/sanctumlabs/curtz/actions/workflows/tests.yml)
[![Lint](https://github.com/sanctumlabs/curtz/actions/workflows/lint.yml/badge.svg)](https://github.com/sanctumlabs/curtz/actions/workflows/lint.yml)
[![Build](https://github.com/sanctumlabs/curtz/actions/workflows/build_app.yml/badge.svg)](https://github.com/sanctumlabs/curtz/actions/workflows/build_app.yml)
[![codecov](https://codecov.io/gh/sanctumlabs/curtz/branch/develop/graph/badge.svg?token=RNg0UoESug)](https://codecov.io/gh/sanctumlabs/curtz)
[![Go](https://img.shields.io/badge/Go-1.18-blue.svg)](https://go.dev/)
[![Codacy Badge](https://app.codacy.com/project/badge/Grade/be035defd2d44675bddf744a88d1a2d5)](https://www.codacy.com/gh/SanctumLabs/curtz/dashboard?utm_source=github.com&amp;utm_medium=referral&amp;utm_content=SanctumLabs/curtz&amp;utm_campaign=Badge_Grade)

Simple URL Shortner Service

## Getting started

Ensure you have the following setup on you local development environment:

### [Go 1.25](https://go.dev/)

This is the programming language used to build the application. You will require this installed in order to install dependencies and run the application.

### [Docker](https://www.docker.com/)

The application is packaged & run in a Docker container, although it can be run without Docker, it uses services that can be run in Docker as specified in the [docker-compose.yml file](./docker-compose.yml). If you want to run supporting services such as [MongoDB](https://www.mongodb.com/) & [Redis](https://redis.io/) in docker containers, then you will require docker setup. If not, you can install these services locally on your development machine.

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

Next step is to run the services the application needs to communicate with; The database & the cache.

If you have installed these locally, you can run them in separate terminal sessions. If not, you can use Docker to do so(preferred option).

```bash
docker compose up
# You can optionally attach -d flag to the command like below
docker compose up -d
```

> This will run the services in docker containers, pulling the images and building the containers for use. Using the `-d` flag runs the services in the background.

Depending on which terminal session you are using to run the above steps(if all are in the same terminal session), you can continue to run the application as below:

```bash
go run app/cmd/main.go
# or using make
make run
```

> This will boot up the application with the provided environment variables.

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

[SemVer](https://semver.org/) is used for versioning. For the versions available, see the [tags](https://github.com/SanctumLabs/curtz/tags) in this repository.

## License

View the project license [here](./LICENSE)
