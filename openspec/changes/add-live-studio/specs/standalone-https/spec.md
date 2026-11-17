# Spec Delta

## ADDED Requirements

### Requirement: HTTPS from the binary
The server SHALL speak HTTPS by itself, with no reverse proxy, when `server.cert_file` and `server.key_file` name
a certificate chain and its key in PEM, or when `server.self_signed` is true. A certificate given in files SHALL
win over `self_signed`. With HTTPS on, `server.public_url` SHALL have to be `https://`, and the log SHALL name
the certificate in use and its SHA-256 fingerprint at every start, never the key.

#### Scenario: Certificate from files
- **GIVEN** `cert_file` and `key_file` point at a valid pair
- **WHEN** the server starts
- **THEN** it answers HTTPS with that certificate and logs its fingerprint

#### Scenario: Plain HTTP by default
- **WHEN** neither a certificate nor `self_signed` is set
- **THEN** the server speaks HTTP, as before

#### Scenario: Public URL not HTTPS
- **WHEN** HTTPS is on and `public_url` starts with `http://`
- **THEN** startup fails saying the public URL must be `https://`

#### Scenario: Unusable certificate
- **WHEN** `cert_file` does not match `key_file`, or one of them cannot be read
- **THEN** startup fails naming the file, without printing the key

### Requirement: Self-signed certificate
With `server.self_signed = true` and no certificate files, the server SHALL generate a certificate and key under
`<data_dir>/tls`, readable only by its user, for the host of `public_url`, `localhost` and the loopback
addresses, valid for one year, and SHALL reuse them at the next starts. It SHALL generate new ones when they are
missing, expired or within 30 days of expiring, or no longer cover the host of `public_url`.

#### Scenario: First start
- **WHEN** the server starts with `self_signed = true` and an empty data directory
- **THEN** `<data_dir>/tls` holds a certificate for the public host and its key with mode 0600, and HTTPS answers
  with it

#### Scenario: Reused
- **WHEN** the server restarts
- **THEN** the same certificate is served, with the same fingerprint

#### Scenario: Host changed
- **WHEN** `public_url` is changed to another host and the server restarts
- **THEN** a new certificate covering the new host is generated

#### Scenario: Close to expiry
- **WHEN** the stored certificate expires in less than 30 days
- **THEN** the next start generates a new one

### Requirement: LiveKit signalling through the server
The server SHALL proxy LiveKit's signalling at `/livekit/` to `livekit.api_url`, WebSocket upgrades included, so
that a browser on an HTTPS page reaches the studio over the page's own origin and certificate. When `livekit.url`
is empty, the URL given to browsers SHALL be the public URL's own origin with `/livekit`, as `wss://` under HTTPS
and `ws://` under HTTP. The proxy SHALL forward only to the configured LiveKit, never to a host the client names.

#### Scenario: Studio over HTTPS with no reverse proxy
- **GIVEN** the server with `self_signed = true` and LiveKit listening only on the loopback address
- **WHEN** an owner and a guest open the studio over `https://`
- **THEN** both connect to the room through `wss://<public host>/livekit` and see each other

#### Scenario: Derived URL
- **WHEN** `livekit.url` is empty and `public_url` is `https://studio.example.com:8443`
- **THEN** the studio token's `url` is `wss://studio.example.com:8443/livekit`

#### Scenario: Explicit URL kept
- **WHEN** `livekit.url` is set
- **THEN** browsers are given that URL unchanged

#### Scenario: Only the configured LiveKit
- **WHEN** a request to `/livekit/` carries a path, host or header naming another upstream
- **THEN** it is forwarded only to `livekit.api_url`

#### Scenario: LiveKit's API is not exposed
- **WHEN** an unauthenticated client calls LiveKit's server API paths through `/livekit/`
- **THEN** LiveKit answers as it would directly: its API still requires its own credentials
