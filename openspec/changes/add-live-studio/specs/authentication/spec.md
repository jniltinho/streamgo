# Spec Delta

## ADDED Requirements

### Requirement: Accounts
Accounts SHALL be created only from the CLI, identified by a case-insensitive unique e-mail, with a name and a
password of at least 10 characters stored as an argon2id hash. A disabled account SHALL not be able to sign in and
its sessions SHALL stop working at once.

#### Scenario: No public sign-up
- **WHEN** any unauthenticated request tries to create an account through the API
- **THEN** no route exists for it and the response is 404

#### Scenario: Disabled while signed in
- **GIVEN** Ana is signed in
- **WHEN** an operator runs `streamgo user disable ana@example.com`
- **THEN** her next API request is answered 401

### Requirement: Password sign-in and sessions
`POST /api/v1/auth/login` SHALL create a server-side session for a correct e-mail and password and set a cookie
that is `HttpOnly`, `SameSite=Lax` and, when the public URL is HTTPS, `Secure`. Only a hash of the cookie value
SHALL be stored. Sessions SHALL expire after 14 days without use, and `POST /api/v1/auth/logout` SHALL delete the
session. A wrong e-mail and a wrong password SHALL produce the same response.

#### Scenario: Sign in
- **WHEN** Ana posts her e-mail and password
- **THEN** the response sets the session cookie and `GET /api/v1/me` returns her id, name, e-mail, theme and
  language

#### Scenario: Wrong credentials are indistinguishable
- **WHEN** a login is tried with an unknown e-mail, and another with a known e-mail and a wrong password
- **THEN** both answer 401 with the same body

#### Scenario: Expiry after inactivity
- **GIVEN** a session last used 13 days ago
- **WHEN** it is used, and then not used for another 14 days
- **THEN** the first request succeeds and renews it, and a request after the 14 idle days is answered 401

#### Scenario: Logout
- **WHEN** Ana logs out and then reuses the old cookie
- **THEN** the request is answered 401

### Requirement: Request protections
Every state-changing API request SHALL be refused with 403 unless its `Origin` header equals the origin of
`server.public_url`. Sign-in attempts SHALL be limited to 10 per minute per client address, answered 429 beyond
that, with the limiter's memory bounded. The client address SHALL be taken from forwarding headers only when the
peer is in `server.trusted_proxies`. Request bodies SHALL be limited before they are read.

#### Scenario: Cross-origin post
- **WHEN** a signed-in browser sends `POST /api/v1/broadcasts` with `Origin: https://evil.example`
- **THEN** the response is 403 and nothing is created

#### Scenario: Missing or null origin
- **WHEN** a state-changing request arrives without an `Origin` header, or with `Origin: null`
- **THEN** the response is 403

#### Scenario: Oversized body
- **WHEN** a JSON request body larger than 64 kB is sent to any API route
- **THEN** the response is 413 and the body is not processed

#### Scenario: Sign-in rate limit
- **WHEN** one address sends 11 login attempts within a minute
- **THEN** the eleventh is answered 429

#### Scenario: Spoofed forwarding header
- **GIVEN** no trusted proxy is configured
- **WHEN** a client sends `X-Forwarded-For` with another address
- **THEN** the rate limit still counts the connection's own address

### Requirement: Nothing sensitive in logs or caches
API responses SHALL carry `Cache-Control: no-store`, and the request log SHALL record the route pattern, never
the raw path or the query string, so invite tokens, session values and LiveKit tokens do not reach logs.

#### Scenario: Invite token not logged
- **WHEN** a guest opens `/api/v1/join/<token>` and the server log is searched
- **THEN** the token does not appear, and the entry names the route as `/api/v1/join/:token`

#### Scenario: Responses not cached
- **WHEN** any API route answers
- **THEN** the response has `Cache-Control: no-store`

### Requirement: Authorization in the server
Every route that reads or changes a broadcast, a destination, a stage, a banner, a logo or a live output SHALL
verify in the server that the signed-in user owns it, answering 404 when they do not.

#### Scenario: Another user's broadcast
- **GIVEN** Ana owns a broadcast
- **WHEN** Bruno, signed in, requests `GET`, `PATCH` or `DELETE` on it, or `POST` on its `live` route
- **THEN** each answer is 404 and nothing changes
