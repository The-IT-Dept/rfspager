import { Platform, type ImageSourcePropType } from "react-native"

/**
 * Icon for a Stack.Toolbar item.
 *
 * iOS draws SF Symbols natively in the header. Android's toolbar is a Compose
 * host that can only draw image sources: given a symbol name it logs a warning
 * and renders nothing, so the button silently disappears. Hand it an image.
 */
export function toolbarIcon<Symbol extends string>(
  sf: Symbol,
  android: ImageSourcePropType,
): Symbol | ImageSourcePropType {
  return Platform.OS === "ios" ? sf : android
}
