// The popular platforms whose ingest address is the same for every account: only the stream key differs, so the
// form can fill the address in. A platform that hands each account or each stream its own address (LinkedIn,
// TikTok, Kick, X) is not here: its address is typed like any other.
export interface DestinationPreset {
  id: string
  name: string
  address: string
}

export const destinationPresets: readonly DestinationPreset[] = [
  { id: 'youtube', name: 'YouTube', address: 'rtmps://a.rtmps.youtube.com/live2' },
  { id: 'twitch', name: 'Twitch', address: 'rtmps://live.twitch.tv/app' },
  { id: 'facebook', name: 'Facebook', address: 'rtmps://live-api-s.facebook.com:443/rtmp/' },
  { id: 'instagram', name: 'Instagram', address: 'rtmps://live-upload.instagram.com:443/rtmp/' },
]
