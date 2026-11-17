# Spec Delta

## ADDED Requirements

### Requirement: Studio screen for the owner
The owner's studio SHALL show, in the arrangement of the approved design, a header, the stage, a layout picker,
the backstage, a side panel and the owner's controls. Every control SHALL be reachable and operable by keyboard
and carry an accessible name.

#### Scenario: Regions
- **WHEN** the owner opens the studio of a live broadcast with guests
- **THEN** the header shows the title, the on-air state with a timer and each chosen destination with its status;
  the layout picker shows the six layouts; the backstage lists every participant and shared screen with whether
  each is on stage; the side panel has the Banners, Brand and Destinations tabs; and the controls cover the
  owner's microphone, camera, screen share, invite link, settings and leaving

#### Scenario: Connecting
- **WHEN** the studio is connecting to the media server
- **THEN** the backstage says it is connecting, and after three failed attempts the page says the studio is not
  connected and offers to try again

#### Scenario: Preview before the stage
- **GIVEN** nothing is on stage and the owner's camera is on
- **WHEN** the owner looks at the studio
- **THEN** the stage says it is empty and how to fill it, and each backstage card shows the live picture of its
  camera or shared screen (the initials when the camera is off)

#### Scenario: Speaking
- **GIVEN** Bruno's microphone is on, whether he is on stage or backstage
- **WHEN** the media server hears him
- **THEN** the microphone mark of his card changes shape and colour and is named "Speaking", and goes back when
  he is silent

#### Scenario: Add to stage
- **GIVEN** Bruno is backstage
- **WHEN** the owner activates "Add to stage" on his card
- **THEN** his camera appears in the stage and his card shows he is on stage

#### Scenario: Change layout
- **WHEN** the owner picks the `sidebar` layout
- **THEN** the stage rearranges and the picker marks `sidebar` as selected

#### Scenario: Stage full
- **GIVEN** `studio.max_on_stage` sources are on stage
- **THEN** "Add to stage" on the other cards is disabled with a reason

#### Scenario: Keyboard only
- **WHEN** the owner uses only the keyboard
- **THEN** they can add a participant to the stage, change the layout, show a banner and go live, with a visible
  focus indicator throughout

### Requirement: Screen sizes
The studio SHALL fit the window without scrolling the page at Full HD (1920x1080), QHD (2560x1440) and 4K
(3840x2160), and at any window from 1280x720 up. The stage region SHALL take the space the other regions leave
and show the program as the largest 16:9 picture that fits in it, centred. Above Full HD the whole interface SHALL
scale up in proportion to the window, so QHD and 4K show the Full HD arrangement larger, not smaller text in a
bigger empty area.

#### Scenario: Full HD, QHD and 4K
- **WHEN** the studio is opened in windows of 1920x1080, 2560x1440 and 3840x2160 CSS pixels
- **THEN** in each the page has no scrollbar, every region is fully visible, the program is 16:9, and the three
  screenshots scaled to the same size differ by no more than rendering sharpness

#### Scenario: Text stays readable at 4K
- **WHEN** the studio is opened in a 3840x2160 window
- **THEN** body text is rendered at twice its Full HD size and no text is smaller than 22 px

#### Scenario: Smaller and odd windows
- **WHEN** the window is 1280x720, 1440x900 or an ultrawide 3440x1440
- **THEN** the page does not scroll, the program stays 16:9 and centred, and no control is cut off; the backstage
  row scrolls sideways when its cards do not fit

#### Scenario: Wide window
- **WHEN** the window is 19:10 or wider, such as 2340x1170, 2560x1314 or 3440x1440
- **THEN** the backstage stands above the side panel in the right-hand column and scrolls inside itself, the stage
  takes the whole height above the controls, and the program fills the stage with no empty band beside it

#### Scenario: Window too small
- **WHEN** the window is narrower than 1280 px or shorter than 720 px
- **THEN** the studio says a larger window is needed to host, and the guest view still works

### Requirement: The stage is the program
The stage the owner sees SHALL be the canvas that is published, drawn by the compositor from the stage state: the
sources in the selected layout with each participant's name, the logo, the banner being shown and the broadcast's
accent colour. A source whose participant is absent SHALL not be drawn; a present participant whose camera is off
SHALL be drawn as a placeholder with their initials. The program SHALL not change with the UI theme.

#### Scenario: Layout rectangles
- **WHEN** the layout function is given each layout with 0 to 6 present sources at 1280x720 and at 1920x1080
- **THEN** the rectangles are those of the layout table in the design (`solo` draws the first source; `grid` all;
  `spotlight`, `sidebar` and `screen` the featured source and the others beside it; `pip` the first two; none
  with no source), every rectangle lies inside the frame, none overlap except in `pip`, and the result is the
  same for the same input

#### Scenario: Absent participant
- **GIVEN** three sources on stage, one of a guest who left
- **THEN** the layout is drawn for the two present sources, and the third returns when the guest rejoins

#### Scenario: Camera off
- **WHEN** a participant on stage turns their camera off
- **THEN** their tile shows their initials and name, and their audio is still in the program

#### Scenario: Theme independence
- **WHEN** the owner switches the UI theme while on stage content is shown
- **THEN** the pixels of the program canvas do not change

### Requirement: Program audio
The program SHALL always carry one audio track: the mix of the microphones of the cameras on stage and the audio
of the shared screens on stage, each counted once, limited so that several voices do not clip, and silent when
nothing is on stage. In the studio, participants SHALL hear the same sources except their own, each once.
Backstage participants are left out of the program and of normal playback; this is not a privacy boundary
between participants of the same studio.

#### Scenario: Backstage is silent
- **GIVEN** Bruno is backstage and speaking
- **THEN** his voice is not in the program and nobody in the studio hears him

#### Scenario: Each source once
- **GIVEN** a tone from the owner's microphone, another from a guest's microphone and a third from a shared
  screen, all on stage
- **THEN** the destination's recording contains each tone once at its level, and the owner hears the guest's and
  the screen's tones once and not their own

#### Scenario: Guest reconnects
- **GIVEN** a guest on stage whose connection drops and returns
- **THEN** after the return their tone is in the program once, and no silent or duplicate copy of their earlier
  track remains in the mix

#### Scenario: Empty stage
- **WHEN** every source is taken off stage during a live broadcast
- **THEN** the program still carries an audio track, silent, and the broadcast stays `live`

#### Scenario: Level
- **WHEN** one tone at a known level is on stage
- **THEN** its level in the program is within 1 dB of its level at the source

#### Scenario: Six voices
- **WHEN** six microphones at full level are on stage
- **THEN** the program's audio does not clip

#### Scenario: Owner's own voice
- **GIVEN** the owner is on stage
- **THEN** their microphone is in the program and they do not hear themselves

### Requirement: Publishing while hidden
The compositor SHALL keep producing fresh pictures of every source on stage when the studio tab is hidden or its
window minimized, and the studio SHALL warn the owner while a live broadcast's tab is hidden.

#### Scenario: Hidden tab stays fresh
- **GIVEN** a live broadcast with the owner and a guest on stage, each sending a video with a frame counter
- **WHEN** the owner's studio tab stays hidden, and then its window minimized, for five minutes each
- **THEN** in the destination's recording both counters advance at least 25 times in every second and never stop
  for longer than 1 second

### Requirement: Live controls
The owner SHALL go live and end the broadcast from the studio, with a confirmation before ending, see the state
(`starting`, `live`, `interrupted`) and each destination's status and last error, and switch destinations on and
off in the Destinations tab while live. Going live SHALL be refused in the studio, with an explanation, in a
browser that cannot host.

#### Scenario: Destination error is visible
- **GIVEN** a destination is `retrying`
- **THEN** its chip in the header shows an error state and the Destinations tab shows the last error

#### Scenario: Unsupported browser
- **WHEN** the owner opens the studio in a browser that is not Chromium-based
- **THEN** the studio says hosting needs a Chromium-based desktop browser and "Go live" is disabled

#### Scenario: End needs confirmation
- **WHEN** the owner activates "End broadcast"
- **THEN** a themed dialog asks for confirmation before anything stops

### Requirement: Recovery after a reload
After a reload during a live or interrupted broadcast the studio SHALL reconnect to the room by itself and SHALL
publish the program again only when its audio is running: at once when the browser allows audio, otherwise on one
"Resume broadcast" action. It SHALL say that screen shares must be shared again.

#### Scenario: Reload while live
- **GIVEN** a live broadcast
- **WHEN** the owner reloads the studio in a browser profile with no autoplay exemption
- **THEN** the studio reconnects to the room without any action and shows "Resume broadcast"; on that one click
  it publishes the program with audio and the broadcast becomes `live` again; a screen that was shared is listed
  as needing to be shared again

### Requirement: Guest join page and guest view
The invite link SHALL open a page showing the broadcast's title, a field for the guest's name and a preview of the
chosen camera and microphone with device pickers, before joining. After joining, the guest SHALL see the stage,
whether the broadcast is on air, whether they are on stage or backstage, the other participants' names, and
controls for their own microphone, camera, screen share and leaving, and no control that changes the stage.

#### Scenario: Preview before joining
- **WHEN** a guest opens a valid invite link and allows camera and microphone
- **THEN** they see their camera preview and can pick devices before pressing "Enter studio"

#### Scenario: Permission denied
- **WHEN** the guest denies camera and microphone access
- **THEN** the page explains how to allow it and still lets them join without them

#### Scenario: Guest knows where they are
- **WHEN** the owner puts a guest on stage and later takes them off
- **THEN** the guest's view changes from "Backstage" to "On stage" and back

#### Scenario: Removed
- **WHEN** the owner removes a guest
- **THEN** the guest sees a page saying they were removed from the studio, not an error
