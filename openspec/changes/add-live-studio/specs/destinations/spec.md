# Spec Delta

## ADDED Requirements

### Requirement: Destination management
A signed-in user SHALL keep up to 20 destinations, each with a name of 1 to 60 characters, an address whose
scheme is `rtmp` or `rtmps` with a host, of at most 1024 characters and without whitespace or control characters,
and an optional stream key of at most 512 characters. With a key, the stream is sent to the address followed by
a slash and the key. Users SHALL see and change only their own destinations.

#### Scenario: Create
- **WHEN** Ana creates "YouTube" with `rtmp://a.rtmp.youtube.com/live2` and a key
- **THEN** it appears in her list and not in Bruno's

#### Scenario: Platform presets
- **WHEN** the user picks YouTube, Twitch, Facebook or Instagram in the new-destination form, or its shortcut on
  the empty destinations page
- **THEN** the form fills that platform's name and RTMPS address, both still editable, and only the stream key is
  left to paste; picking "Other" clears what the preset filled

#### Scenario: Invalid address
- **WHEN** a destination is saved with an `http` address, an address without a host, one containing a space or a
  line break, or a blank name
- **THEN** the response is 422 naming the field

#### Scenario: Address and key are joined
- **WHEN** a destination has the address `rtmp://a.example.com/live2/` and the key `abcd?x=1`
- **THEN** the relay publishes to `rtmp://a.example.com/live2/abcd?x=1`

#### Scenario: Query in the address with a key
- **WHEN** a destination is saved with an address that has a query string and a separate key
- **THEN** the response is 422 asking for the query to be given with the key

#### Scenario: Limit
- **WHEN** a user with 20 destinations creates another
- **THEN** the response is 422 saying the limit was reached

### Requirement: Destination secrecy
Everything in a destination after its host SHALL be treated as secret, because complete addresses often embed the
key: the address and the key SHALL be stored encrypted, the API SHALL return only the name, scheme, host and
whether a key is set, and neither SHALL appear in logs, error messages or status text. Updating a destination
without an `address` or `key` field SHALL keep the stored one; an empty `key` SHALL clear it.

#### Scenario: Nothing secret returned
- **WHEN** the owner lists or fetches destinations
- **THEN** each carries its name, scheme, host and `has_key`, and no field holds the path, the query or the key

#### Scenario: Key pasted in the address
- **WHEN** a destination is saved as `rtmp://live.example.com/app/SECRET123` without a key
- **THEN** `SECRET123` appears in no API response, and not in clear text in the database

#### Scenario: Rename keeps the address and the key
- **WHEN** the owner patches only the name of a destination
- **THEN** going live still reaches the same address with the same key

#### Scenario: Redacted from errors
- **GIVEN** a destination whose server rejects the stream
- **WHEN** its status reports the last error
- **THEN** the text contains neither the key nor anything of the address after the host

### Requirement: Private addresses refused by default
Unless `output.allow_private_destinations` is on, a destination whose host is, or resolves to, a loopback,
private, link-local or unspecified address, in IPv4 or IPv6, SHALL be refused when it is saved and SHALL not be
relayed to when a relay starts.

#### Scenario: Loopback and private hosts
- **WHEN** a destination is saved with the host `127.0.0.1`, `localhost`, `10.0.0.5`, `[::1]` or `169.254.169.254`
- **THEN** the response is 422 saying private addresses are not allowed

#### Scenario: Name that later resolves to a private address
- **GIVEN** a saved destination whose host now resolves to `10.0.0.5`
- **WHEN** a live broadcast starts its relay
- **THEN** no connection is opened and the destination is `retrying` with an error saying why

#### Scenario: Operator opt-in
- **GIVEN** `output.allow_private_destinations = true`
- **WHEN** a destination is saved with the host `10.0.0.5`
- **THEN** it is accepted

### Requirement: Destinations of a broadcast
The owner SHALL choose which of their destinations a broadcast uses with `PUT /api/v1/broadcasts/:id/destinations`,
at most `output.max_destinations`, and only destinations they own.

#### Scenario: Choose destinations
- **WHEN** the owner sets two of their destinations on a broadcast
- **THEN** the broadcast lists those two

#### Scenario: Someone else's destination
- **WHEN** the owner includes a destination id that belongs to another user
- **THEN** the response is 422 and the selection is unchanged

#### Scenario: Too many
- **WHEN** more than `output.max_destinations` are chosen
- **THEN** the response is 422

### Requirement: Destinations in use
While a broadcast is `starting`, `live` or `interrupted`, changing the address or key of one of its chosen
destinations, or deleting it, SHALL be refused with 409; renaming it SHALL be allowed. Deleting a destination
that no active broadcast uses SHALL remove it from every broadcast.

#### Scenario: Delete while in use
- **GIVEN** a live broadcast relaying to a destination
- **WHEN** the owner deletes that destination, or changes its key
- **THEN** the response is 409 and the relay keeps running

#### Scenario: In use while interrupted
- **GIVEN** an `interrupted` broadcast that chose a destination
- **WHEN** the owner changes that destination's address
- **THEN** the response is 409

#### Scenario: Delete when free
- **WHEN** the owner deletes a destination chosen only by offline broadcasts
- **THEN** it is gone and those broadcasts no longer list it
