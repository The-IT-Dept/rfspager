import type * as AudioAPI from "react-native-audio-api"

/**
 * react-native-audio-api, loaded on first use rather than at import.
 *
 * Importing it installs its native module and throws when that module isn't
 * in the binary (a JS update reaching a build made before Radio). Required
 * lazily, that costs the listener Radio's audio, not the app.
 */
let mod: typeof AudioAPI | null | undefined

export function audioAPI(): typeof AudioAPI | null {
  if (mod === undefined) {
    try {
      mod = require("react-native-audio-api") as typeof AudioAPI
    } catch (e) {
      console.warn("[radio] react-native-audio-api unavailable", e)
      mod = null
    }
  }
  return mod
}

export class AudioUnavailableError extends Error {
  constructor() {
    super("Audio isn't available in this build of the app.")
    this.name = "AudioUnavailableError"
  }
}

/** The module, or throws AudioUnavailableError. */
export function requireAudioAPI(): typeof AudioAPI {
  const m = audioAPI()
  if (!m) throw new AudioUnavailableError()
  return m
}
