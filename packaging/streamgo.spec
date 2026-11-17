# Built by packaging/build.sh from an already staged tree (_stagedir); nothing is compiled here.
# Portions adapted from MeetHub (MIT): packaging/meethub.spec
Name:           streamgo
Version:        @VERSION@
Release:        1
Summary:        Self-hosted live-streaming studio
License:        MIT and Apache-2.0
URL:            https://github.com/jniltinho/streamgo
BuildArch:      x86_64
AutoReqProv:    no
Requires(post): shadow-utils
# the program is encoded with an ffmpeg that has libx264 and aac. The base repositories of the RHEL family have no
# ffmpeg, EPEL's ffmpeg-free has no libx264 (the service then refuses to start, naming it), RPM Fusion's ffmpeg has
# both (docs/install-linux.md): so a recommendation, not a requirement
Recommends:     (ffmpeg or ffmpeg-free)

# static Go binaries: no debuginfo package, no stripping, no build-id links
%global debug_package %{nil}
%global __os_install_post %{nil}
%global _build_id_links none

%description
StreamGo is a browser studio: invite guests, put cameras and screens on stage, and go live to RTMP and RTMPS
destinations, served by one static binary with the web app embedded.

Everything is installed in /opt/streamgo, with the LiveKit and MediaMTX servers the studio needs, and runs as the
streamgo user under systemd (streamgo.service, streamgo-livekit.service, streamgo-mediamtx.service).

%install
cp -a %{_stagedir}/. %{buildroot}/

%files
%dir /opt/streamgo
%dir /opt/streamgo/bin
/opt/streamgo/bin/streamgo
/opt/streamgo/bin/livekit-server
/opt/streamgo/bin/mediamtx
/opt/streamgo/bin/init-env
%config(noreplace) /opt/streamgo/config.toml
%config(noreplace) /opt/streamgo/livekit.yaml
%config(noreplace) /opt/streamgo/mediamtx.yml
/opt/streamgo/streamgo.env.example
/opt/streamgo/LICENSE
/opt/streamgo/NOTICE
/opt/streamgo/THIRD_PARTY
/usr/lib/systemd/system/streamgo.service
/usr/lib/systemd/system/streamgo-livekit.service
/usr/lib/systemd/system/streamgo-mediamtx.service

%post -f %{_scripts}/postinstall.sh
%preun -f %{_scripts}/preremove.sh
%postun -f %{_scripts}/postremove.sh
