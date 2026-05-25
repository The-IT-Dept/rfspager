import * as React from "react"
import {
  ActivityIndicator,
  StyleSheet,
  Text,
  TouchableOpacity,
  TouchableOpacityProps,
  ViewStyle,
  TextStyle,
} from "react-native"
import { AppTheme, useAppTheme } from "../../theme"

export type ButtonMode = "contained" | "outlined" | "text"

export type ButtonProps = Omit<TouchableOpacityProps, "style"> & {
  mode?: ButtonMode
  loading?: boolean
  disabled?: boolean
  children: React.ReactNode
  style?: ViewStyle | ViewStyle[]
  labelStyle?: TextStyle | TextStyle[]
  containerStyle?: ViewStyle
  onPress?: () => void
}

export const Button = function Button(props: ButtonProps) {
  const {
    mode = "contained",
    loading,
    disabled,
    children,
    style,
    labelStyle,
    containerStyle,
    onPress,
    ...rest
  } = props
  const theme = useAppTheme()
  const styles = makeStyles(theme, mode)
  const isDisabled = disabled || loading

  return (
    <TouchableOpacity
      activeOpacity={0.7}
      onPress={isDisabled ? undefined : onPress}
      disabled={isDisabled}
      style={[
        styles.button,
        isDisabled && styles.disabled,
        containerStyle,
        ...(Array.isArray(style) ? style : [style]),
      ]}
      {...rest}
    >
      {loading ? (
        <ActivityIndicator size="small" color={mode === "contained" ? theme.colors.white : theme.colors.primary} />
      ) : (
        <Text style={[styles.label, labelStyle]}>{children}</Text>
      )}
    </TouchableOpacity>
  )
}

const makeStyles = (theme: AppTheme, mode: ButtonMode) =>
  StyleSheet.create({
    button: {
      marginVertical: 10,
      paddingVertical: 12,
      paddingHorizontal: 16,
      borderRadius: 8,
      width: "100%",
      alignItems: "center",
      justifyContent: "center",
      backgroundColor:
        mode === "contained" ? theme.colors.primary : "transparent",
      borderWidth: mode === "outlined" ? 1 : 0,
      borderColor: mode === "outlined" ? theme.colors.primary : "transparent",
    },
    disabled: {
      opacity: 0.5,
    },
    label: {
      fontSize: 15,
      fontWeight: "bold",
      lineHeight: 26,
      color:
        mode === "contained" ? theme.colors.white : theme.colors.primary,
    },
  })
