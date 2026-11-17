# Spec Delta

## ADDED Requirements

### Requirement: Documentation set
The repository SHALL document StreamGo in English under `docs/`, indexed by `docs/README.md` with a "where to
start" table and linked from the root README: the architecture, installing from the Linux packages, installing
the binary by hand, running with Docker, serving HTTPS from the binary itself, running behind a reverse proxy,
the deployment guide and the security model.

#### Scenario: Index is complete
- **WHEN** the documentation test reads `docs/README.md`
- **THEN** it links `architecture.md`, `install-linux.md`, `install-binary.md`, `install-docker.md`,
  `standalone-https.md`, `reverse-proxy.md`, `deployment.md` and `security.md`, every link resolves to a file,
  and the root README links the index

#### Scenario: No dead links
- **WHEN** the documentation test follows every relative link and image in `docs/` and the root README
- **THEN** each one resolves to an existing file

#### Scenario: Commands in the guides run
- **WHEN** the commands of `install-docker.md` are followed on a clean checkout, and those of `install-linux.md`
  on a clean Ubuntu 26.04 container
- **THEN** StreamGo is running and the first user signs in

### Requirement: Diagrams
`docs/images/` SHALL hold the diagrams the documents use, each as an SVG source and a PNG of it: the overview of
the pieces, the media path from the browsers to the destinations, the go-live sequence, the deployment
topologies (packages, Docker, behind a reverse proxy, standalone HTTPS) and the ports. Diagrams SHALL be drawn
with the project's dark palette, with text that stays readable at the width of a GitHub page.

#### Scenario: Every diagram has both formats
- **WHEN** the documentation test lists `docs/images/diagram-*.svg`
- **THEN** each has a PNG of the same name, and each is used by at least one document

#### Scenario: Diagrams match the design
- **WHEN** the media-path diagram is read against the design
- **THEN** it shows LiveKit for the studio, the owner's browser composing the program, WHIP through StreamGo to
  MediaMTX, the ffmpeg transcoder and one relay per destination, and no Redis and no Egress
