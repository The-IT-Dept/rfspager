import { Platform, type StyleProp, type TextStyle, type ViewStyle } from "react-native"
import { SymbolView, type SymbolViewProps } from "expo-symbols"
import { Ionicons } from "@expo/vector-icons"

export interface AppIconProps {
  /** SF Symbol name, used on iOS. */
  sf: SymbolViewProps["name"]
  /** Ionicons fallback, used on Android where SF Symbols do not exist. */
  ion: keyof typeof Ionicons.glyphMap
  size?: number
  color?: string
  /** Ionicons renders a font glyph (TextStyle) while SymbolView is a view
   *  (ViewStyle); accept only what both accept. */
  style?: StyleProp<ViewStyle & TextStyle>
}

/**
 * One icon, drawn natively per platform.
 *
 * SF Symbols are the system icon set on iOS: they carry the correct optical
 * weight for their size, align to the text baseline, and scale with Dynamic
 * Type. Ionicons is a web icon set, which is why the chrome read as slightly
 * off. SF Symbols have no Android equivalent, so Ionicons remains the fallback
 * there rather than shipping two different-looking icon sets on iOS.
 */
export function AppIcon({ sf, ion, size = 22, color, style }: AppIconProps) {
  if (Platform.OS === "ios") {
    return (
      <SymbolView
        name={sf}
        size={size}
        tintColor={color}
        resizeMode="scaleAspectFit"
        style={style}
      />
    )
  }
  return <Ionicons name={ion} size={size} color={color} style={style} />
}
