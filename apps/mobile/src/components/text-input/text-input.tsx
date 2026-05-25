import * as React from "react"
import {
  StyleSheet,
  Text,
  TextInput as RNTextInput,
  TextInputProps as RNTextInputProps,
  View,
  ViewStyle,
} from "react-native"
import { AppTheme, useAppTheme } from "../../theme/rfspager"

export type TextInputProps = RNTextInputProps & {
  label?: string
  description?: string
  errorText?: string
  containerStyle?: ViewStyle
}

export const TextInput = function TextInput(props: TextInputProps) {
  const { label, description, errorText, containerStyle, style, ...rest } = props
  const theme = useAppTheme()
  const styles = makeStyles(theme)

  return (
    <View style={[styles.container, containerStyle]}>
      {label ? <Text style={styles.label}>{label}</Text> : null}
      <RNTextInput
        style={[styles.input, errorText ? styles.inputError : null, style]}
        selectionColor={theme.colors.primary}
        placeholderTextColor={theme.colors.textTertiary}
        {...rest}
      />
      {description && !errorText ? <Text style={styles.description}>{description}</Text> : null}
      {errorText ? <Text style={styles.error}>{errorText}</Text> : null}
    </View>
  )
}

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    container: {
      marginVertical: 12,
      width: "100%",
    },
    label: {
      fontSize: 13,
      color: theme.colors.textSecondary,
      marginBottom: 6,
    },
    input: {
      backgroundColor: theme.colors.surface,
      borderWidth: 1,
      borderColor: theme.colors.border,
      borderRadius: 8,
      paddingHorizontal: 12,
      paddingVertical: 10,
      fontSize: 16,
      color: theme.colors.textPrimary,
    },
    inputError: {
      borderColor: theme.colors.error,
    },
    description: {
      color: theme.colors.accent,
      fontSize: 13,
      paddingTop: 8,
    },
    error: {
      color: theme.colors.error,
      fontSize: 13,
      paddingTop: 8,
    },
  })
