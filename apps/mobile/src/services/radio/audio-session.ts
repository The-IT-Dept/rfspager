import { audioAPI } from "./audio-api"

/**
 * The one audio session Radio's two players share (live listening and clip
 * playback).
 *
 * iOS: the "playback" category, so radio is heard with the silent switch on
 * and, with UIBackgroundModes "audio" (set by the react-native-audio-api config
 * plugin), keeps playing with the screen locked. Other apps' audio is
 * interrupted while Radio plays, like any player, and told it may resume once
 * Radio lets the session go (notifyOthersOnDeactivation).
 *
 * The session is only activated when something actually plays and is released
 * when both players are idle: opening the tab must not stop someone's music.
 */

type Holder = "live" | "clip"

const holders = new Set<Holder>()
let configured = false

export function acquireAudioSession(who: Holder) {
  try {
    const AudioManager = audioAPI()?.AudioManager
    if (!configured && AudioManager) {
      AudioManager.setAudioSessionOptions({
        iosCategory: "playback",
        iosMode: "spokenAudio",
        iosOptions: [],
        iosNotifyOthersOnDeactivation: true,
      })
      configured = true
    }
  } catch (e) {
    console.warn("[radio] audio session options failed", e)
  }
  holders.add(who)
}

/** Call after the holder's AudioContext is suspended or closed. */
export function releaseAudioSession(who: Holder) {
  if (!holders.delete(who) || holders.size > 0) return
  const AudioManager = audioAPI()?.AudioManager
  if (!AudioManager) return
  AudioManager.setAudioSessionActivity(false).catch((e: unknown) => {
    // Harmless: iOS refuses while another player still has I/O running.
    console.warn("[radio] audio session deactivate failed", e)
  })
}
