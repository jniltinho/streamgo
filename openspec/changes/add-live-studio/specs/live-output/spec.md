# Spec Delta

## ADDED Requirements

### Requirement: Going live
`POST /api/v1/broadcasts/:id/live` SHALL move an `offline` broadcast to `starting` when it has at least one chosen
destination, none of them is in use by another broadcast, and fewer than `output.max_concurrent` broadcasts are
starting, live or interrupted; otherwise it SHALL refuse with 409 saying why. Requests for one broadcast SHALL be
handled one at a time, and the concurrency limit SHALL hold under simultaneous requests.

#### Scenario: Go live
- **GIVEN** an offline broadcast with two chosen destinations
- **WHEN** the owner goes live and their browser publishes the program
- **THEN** the state passes through `starting` to `live` and both destinations become `live`

#### Scenario: No destination
- **WHEN** the owner goes live on a broadcast with no chosen destination
- **THEN** the response is 409 and the state stays `offline`

#### Scenario: Concurrency limit under simultaneous requests
- **GIVEN** `output.max_concurrent` is 1
- **WHEN** two owners go live on two broadcasts at the same instant
- **THEN** exactly one becomes `starting` and the other is answered 409 saying the server is at its limit

#### Scenario: Double go live
- **WHEN** two go-live requests for the same broadcast arrive at the same instant
- **THEN** one succeeds, the other is answered 409, and one transcoder is started

#### Scenario: Destination already in use
- **GIVEN** a destination chosen by a broadcast that is live
- **WHEN** another broadcast that also chose it goes live
- **THEN** the response is 409 naming the destination

### Requirement: Becoming live
A broadcast SHALL become `live` only when its transcoder reports media flowing, and SHALL return to `offline`
with the reason `no program received` when that does not happen within `output.start_timeout`.

#### Scenario: Process alone is not live
- **GIVEN** a broadcast in `starting` whose transcoder process is running but has produced no output
- **THEN** the state is still `starting`

#### Scenario: No program
- **WHEN** the owner goes live and no program is published within `output.start_timeout`
- **THEN** the state returns to `offline` with the reason `no program received` and nothing is left running

### Requirement: Program ingest
The owner's browser SHALL publish the program by WHIP through `/api/v1/broadcasts/:id/program/whip`, which the
server SHALL forward to MediaMTX only for the owner and only while the broadcast is `starting`, `live` or
`interrupted`. A broadcast SHALL have at most one ingest session: a new offer replaces the previous one. The
media server's own listeners SHALL require the server's internal credentials.

#### Scenario: Owner publishes
- **GIVEN** a broadcast in `starting`
- **WHEN** the owner posts an SDP offer to the WHIP route
- **THEN** the response is 201 with an SDP answer and `Location` pointing at a new session resource of that
  broadcast

#### Scenario: Second offer replaces the first
- **WHEN** the owner posts a second offer while a session exists
- **THEN** the first upstream session is closed and only the second is publishing

#### Scenario: Late request for a replaced session
- **GIVEN** offer B replaced offer A
- **WHEN** a `DELETE` for A's session resource arrives
- **THEN** the response is 404 and B keeps publishing

#### Scenario: No trickle ICE
- **WHEN** a `PATCH` is sent to a session resource
- **THEN** the response is 405, and the studio's offers already carry their ICE candidates

#### Scenario: Media server error
- **WHEN** the media server refuses the offer or does not answer within 10 seconds
- **THEN** the response is 502, no session is stored and the broadcast can still be ended at once

#### Scenario: Orphan session
- **GIVEN** an ingest session still publishing for a broadcast that is `offline`
- **THEN** within 15 seconds the server has closed it

#### Scenario: Not the owner
- **WHEN** another signed-in user, or a guest, posts an offer, or sends `DELETE` to a session resource
- **THEN** the response is 404 and nothing reaches the media server

#### Scenario: Offline broadcast
- **WHEN** the owner posts an offer while the broadcast is `offline`
- **THEN** the response is 409

#### Scenario: Client cannot choose the upstream
- **WHEN** a request to the WHIP routes carries a path, host or header naming another upstream resource
- **THEN** the server forwards only to the broadcast's own path and stored session

#### Scenario: Direct access refused
- **WHEN** a client without the internal credentials tries to publish or read a path on the media server
- **THEN** the media server refuses it

### Requirement: Transcoding
While a broadcast is `live` the server SHALL run one transcoder that turns the program (VP8 or H.264 video with
Opus audio) into H.264 video at the configured resolution and 30 frames per second with a keyframe every
2 seconds, and AAC stereo audio at 48 kHz, whatever size or frame rate the browser sends.

#### Scenario: Output format
- **WHEN** a test destination records 20 seconds of a live broadcast, once with a VP8 program and once with H.264
- **THEN** each recording is H.264 at the configured size and 30 fps with AAC 48 kHz stereo, and its keyframes
  are 2 seconds apart

#### Scenario: Browser lowers its resolution
- **WHEN** the published program drops to a smaller size during the broadcast
- **THEN** the destination keeps receiving the configured size without the stream ending

#### Scenario: Silent program
- **WHEN** the program carries no sound for a minute
- **THEN** the destination keeps receiving an audio stream and the broadcast stays `live`

### Requirement: Relaying to destinations
For each chosen destination the server SHALL run a relay that copies the transcoded program to it without
re-encoding. Each destination SHALL have its own status (`off`, `connecting`, `live` or `retrying` with the last
error), visible only to the owner, and SHALL be `live` only while media is flowing to it. A relay that ends or
stalls while the broadcast is live SHALL be retried after 1, 2 and 5 seconds and then every 10 seconds, without
affecting the other destinations.

#### Scenario: One destination fails
- **GIVEN** a live broadcast with a working destination and one that refuses the connection
- **THEN** the first is `live`, the second is `retrying` with an error, and the first is never interrupted

#### Scenario: Destination stalls
- **GIVEN** a destination that accepts the connection and then stops reading
- **THEN** within 15 seconds its status is `retrying`, not `live`

#### Scenario: Destination recovers
- **GIVEN** a destination in `retrying`
- **WHEN** its server starts accepting the stream
- **THEN** it becomes `live` at the next retry

#### Scenario: Statuses are private
- **WHEN** a guest inspects everything they receive in the studio
- **THEN** it holds no destination name, address or status

### Requirement: Changing destinations while live
Changing a live broadcast's chosen destinations SHALL start a relay for each added destination and stop the relay
of each removed one, leaving the others running. Removing the last destination of a live broadcast SHALL be
refused with 409.

#### Scenario: Add while live
- **GIVEN** a broadcast live on YouTube
- **WHEN** the owner adds Twitch
- **THEN** Twitch becomes `live` and the YouTube relay is the same process as before

#### Scenario: Add a destination another broadcast is using
- **GIVEN** broadcast B is live on a destination
- **WHEN** the owner adds that destination to live broadcast A, also at the same instant as B's go live
- **THEN** exactly one of the two holds the destination and the other request is answered 409

#### Scenario: Remove while live
- **WHEN** the owner removes one of two live destinations
- **THEN** its relay ends within 5 seconds and the other stays `live`

### Requirement: Interruption and resumption
When the program stops arriving during a live broadcast the state SHALL become `interrupted`, the relays SHALL
stop and a deadline of `output.reconnect_grace` SHALL be stored. If the program flows again before the deadline
the broadcast SHALL become `live` with the same destinations; otherwise it SHALL return to `offline` with the
reason `program lost`.

#### Scenario: Program returns
- **GIVEN** a live broadcast
- **WHEN** the program stops and is published again within the grace period
- **THEN** the state goes `interrupted` then `live`, and the destinations are `live` again

#### Scenario: Program without audio
- **WHEN** an offer arrives without an audio track
- **THEN** the broadcast does not become `live` on it, and the studio publishes again once its audio is running

#### Scenario: Owner never returns
- **WHEN** the program is absent past the deadline
- **THEN** the state is `offline` with the reason `program lost` and no ffmpeg process of that broadcast remains

### Requirement: Server restart
Stopping the server SHALL not end a broadcast. After a restart, a broadcast stored as `starting` or `live` SHALL
be `interrupted` with a new deadline, one stored as `interrupted` SHALL keep the deadline it had, and those whose
program is still published SHALL become `live` again.

#### Scenario: Restart while live
- **GIVEN** a live broadcast whose owner is still publishing
- **WHEN** the server is stopped with SIGTERM and started again
- **THEN** the broadcast resumes as `live`, and ending it afterwards closes the ingest session opened before the
  restart

#### Scenario: Server killed
- **WHEN** the server is killed with SIGKILL during a live broadcast
- **THEN** no ffmpeg process started by it remains, and after a restart the broadcast resumes as above

#### Scenario: Restarts do not extend an outage
- **GIVEN** a broadcast `interrupted` with 30 seconds left before its deadline
- **WHEN** the server restarts and no program arrives
- **THEN** the broadcast returns to `offline` at the original deadline

### Requirement: Ending
`DELETE /api/v1/broadcasts/:id/live` SHALL stop the relays and the transcoder, close the ingest session, mark the
stage state as off air and return the broadcast to `offline`, from any state other than `offline`. Each process
SHALL get 5 seconds to finish before it is killed. Nothing belonging to an ended run SHALL act afterwards.

#### Scenario: End
- **WHEN** the owner ends a live broadcast
- **THEN** within 10 seconds the state is `offline`, the stage state has `on_air` false, and no ffmpeg process or
  ingest session of that broadcast remains

#### Scenario: End while starting
- **WHEN** the owner ends a broadcast that is still `starting`
- **THEN** it returns to `offline` and nothing is left running

#### Scenario: No retry after end
- **WHEN** a live broadcast ends while one relay is waiting to retry
- **THEN** no relay is started afterwards

#### Scenario: End then go live again
- **WHEN** the owner ends a broadcast and goes live again at once
- **THEN** a late event from the first run changes nothing in the second

### Requirement: On-air state for everyone
The stage state's `on_air` SHALL be true exactly while the broadcast is `live`, so every participant's studio
shows it.

#### Scenario: Guests see on air
- **WHEN** a broadcast becomes `live`, and later `interrupted`
- **THEN** a guest's studio shows on air, and then no longer does

### Requirement: Process hygiene
ffmpeg SHALL be started without a shell and without StreamGo's secrets in its environment, and its command line
SHALL never be written to logs.

#### Scenario: Command line not logged
- **WHEN** the server log is searched after a live broadcast with a destination key
- **THEN** neither the key nor the destination's address appears in it

#### Scenario: Clean environment
- **WHEN** the environment of a running relay is inspected
- **THEN** it holds no `STREAMGO_` variable
