# API

This is a RESTful API & therefore is exposes to clients a REST interface that they can use to handle url shortening.

The API documentation can be found on url <http://localhost:8085/docs/> after running the application with the instructions set in the [Readme](../README.md). The spec is generated from the handler annotations: run `make generate.open.api.docs` after changing a handler and commit `app/api/openapi/swagger.json` (see [ADR-0023](./adr/0023-openapi-spec-is-generated-from-the-handlers.md)).
