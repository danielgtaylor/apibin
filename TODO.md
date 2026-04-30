# TODO

## OpenAPI operation auth demo

Add a dedicated auth demo fixture for Restish v2 operation-level authentication testing and documentation. The existing `/auth/*` endpoints are useful low-level fixtures for basic, bearer, header API key, and query API key auth, but they do not cover the richer OpenAPI security combinations Restish now needs to teach and validate.

### Goals

- Keep the API fake, deterministic, and easy to use without external accounts or real OAuth apps.
- Expose one compact set of routes that demonstrates how OpenAPI operation `security` affects generated Restish commands.
- Support Restish docs, manual demos, and regression tests with the same public API.
- Keep the existing `/auth/*` endpoints unchanged for compatibility.

### Proposed routes

- `GET /auth-demo/public`
  - OpenAPI: `security: []`
  - Behavior: succeeds without auth and confirms Restish suppresses globally configured auth.
- `GET /auth-demo/user`
  - OpenAPI: `security: [{ userBearer: [] }]`
  - Behavior: requires `Authorization: Bearer user-token`.
- `GET /auth-demo/admin`
  - OpenAPI: `security: [{ adminBearer: [] }]`
  - Behavior: requires `Authorization: Bearer admin-token`.
- `GET /auth-demo/partner`
  - OpenAPI: `security: [{ userBearer: [] }, { partnerKey: [] }]`
  - Behavior: accepts either `Authorization: Bearer user-token` or `X-Partner-Key: partner-key`.
- `GET /auth-demo/signed`
  - OpenAPI: `security: [{ userBearer: [], partnerKey: [] }]`
  - Behavior: requires both `Authorization: Bearer user-token` and `X-Partner-Key: partner-key`.
- `GET /auth-demo/optional`
  - OpenAPI: `security: [{}, { partnerKey: [] }]`
  - Behavior: succeeds anonymously, and reports whether `X-Partner-Key: partner-key` was supplied.

### OpenAPI security schemes

- `userBearer`: HTTP bearer auth, accepted token `user-token`.
- `adminBearer`: HTTP bearer auth, accepted token `admin-token`.
- `partnerKey`: API key auth in header `X-Partner-Key`, accepted key `partner-key`.

### Implementation notes

- Register the schemes in the generated OpenAPI document.
- Add handler-level validation so the fixture verifies Restish sent the expected headers.
- Return a small JSON body with `authenticated`, `scheme`, `subject`, and enough detail to make demos readable.
- Add tests for each route covering success and failure cases.
- Update `README.md` to mention the auth demo routes once implemented.
