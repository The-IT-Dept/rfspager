import React from "react"
import { KeyboardAwareScrollView } from "react-native-keyboard-controller"
import {
  ActivityIndicator,
  StyleSheet,
  Text,
  TextInput,
  TouchableOpacity,
  View,
  type TextInputProps,
} from "react-native"
import { useAppTheme, type AppTheme } from "../../theme"

/**
 * Grouped form building blocks for the account editing screens, matching the
 * card-and-section look of My Account.
 *
 * FormScreen is react-native-keyboard-controller's KeyboardAwareScrollView with
 * the same props as the Alert Zone create screen, which sits correctly under
 * the transparent iOS 26 header and follows the keyboard. Two things broke the
 * account screens: autoFocus (focusing as the screen pushes in scrolls the
 * field into view before the header inset exists, leaving it under the bar),
 * and a plain ScrollView's automaticallyAdjustKeyboardInsets fighting that
 * inset when the keyboard opened. Fields here must not autoFocus.
 */
export function FormScreen({ children }: { children: React.ReactNode }) {
  const theme = useAppTheme()
  return (
    <KeyboardAwareScrollView
      style={{ flex: 1, backgroundColor: theme.colors.backgroundTertiary }}
      showsVerticalScrollIndicator={false}
      contentInsetAdjustmentBehavior="automatic"
      automaticallyAdjustKeyboardInsets
      keyboardShouldPersistTaps="handled"
      contentContainerStyle={{ paddingBottom: 48 }}
    >
      {children}
    </KeyboardAwareScrollView>
  )
}

export function FormSection({
  title,
  footer,
  footerTone = "neutral",
  children,
}: {
  title?: string
  footer?: string | null
  footerTone?: "neutral" | "error"
  children: React.ReactNode
}) {
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  return (
    <View style={styles.section}>
      {title ? <Text style={styles.sectionTitle}>{title}</Text> : null}
      <View style={styles.card}>{children}</View>
      {footer ? (
        <Text style={[styles.footer, footerTone === "error" && { color: theme.colors.error }]}>{footer}</Text>
      ) : null}
    </View>
  )
}

export function FormField({
  label,
  isLast,
  ...input
}: TextInputProps & { label: string; isLast?: boolean }) {
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  return (
    <View style={[styles.field, !isLast && styles.fieldDivider]}>
      <Text style={styles.fieldLabel}>{label}</Text>
      <TextInput
        style={styles.fieldInput}
        placeholderTextColor={theme.colors.textSecondary}
        clearButtonMode="while-editing"
        {...input}
      />
    </View>
  )
}

export function FormButton({
  title,
  onPress,
  disabled,
  loading,
  tone = "primary",
}: {
  title: string
  onPress: () => void
  disabled?: boolean
  loading?: boolean
  tone?: "primary" | "destructive"
}) {
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  const inactive = disabled || loading
  return (
    <TouchableOpacity
      style={[
        styles.button,
        tone === "destructive" && { backgroundColor: theme.colors.error },
        inactive && styles.buttonDisabled,
      ]}
      onPress={onPress}
      disabled={inactive}
      accessibilityRole="button"
      accessibilityState={{ disabled: !!inactive, busy: !!loading }}
    >
      {loading ? <ActivityIndicator color="#fff" /> : <Text style={styles.buttonText}>{title}</Text>}
    </TouchableOpacity>
  )
}

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    section: { marginTop: 24 },
    sectionTitle: {
      marginBottom: 6,
      marginHorizontal: 32,
      fontSize: 13,
      color: theme.colors.textSecondary,
      textTransform: "uppercase",
      letterSpacing: 0.5,
    },
    card: {
      marginHorizontal: 16,
      backgroundColor: theme.colors.cardBackground,
      borderRadius: 10,
      overflow: "hidden",
    },
    footer: {
      marginTop: 8,
      marginHorizontal: 32,
      fontSize: 13,
      lineHeight: 18,
      color: theme.colors.textSecondary,
    },
    field: {
      flexDirection: "row",
      alignItems: "center",
      minHeight: 48,
      paddingLeft: 16,
      paddingRight: 8,
    },
    fieldDivider: {
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: theme.colors.border,
    },
    fieldLabel: {
      width: 104,
      fontSize: 17,
      color: theme.colors.textPrimary,
    },
    fieldInput: {
      flex: 1,
      fontSize: 17,
      paddingVertical: 12,
      color: theme.colors.textPrimary,
    },
    button: {
      marginTop: 32,
      marginHorizontal: 16,
      minHeight: 50,
      borderRadius: 12,
      backgroundColor: theme.colors.primary,
      alignItems: "center",
      justifyContent: "center",
    },
    buttonDisabled: { opacity: 0.4 },
    buttonText: { color: "#fff", fontSize: 17, fontWeight: "600" },
  })
