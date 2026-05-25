import * as Localization from "expo-localization"
import { I18n } from "i18n-js"
import en from "./en.json"
import ja from "./ja.json"

const i18n = new I18n()
i18n.enableFallback = true
i18n.translations = { en, ja }

i18n.locale = Localization.getLocales()[0]?.languageTag || "en"

export { i18n }

/**
 * Builds up valid keypaths for translations.
 * Update to your default locale of choice if not English.
 */
type DefaultLocale = typeof en
export type TxKeyPath = RecursiveKeyOf<DefaultLocale>

type RecursiveKeyOf<TObj extends Record<string, any>> = {
  [TKey in keyof TObj & string]: TObj[TKey] extends Record<string, any>
    ? `${TKey}` | `${TKey}.${RecursiveKeyOf<TObj[TKey]>}`
    : `${TKey}`
}[keyof TObj & string]
