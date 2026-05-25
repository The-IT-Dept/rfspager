import React from "react"
import { TouchableOpacity, Text, View, StyleSheet } from "react-native"
import { FontAwesome5 } from "@expo/vector-icons"
import { useAppTheme } from "../../theme"

interface ShareButtonProps {
  onPress: () => void
}

interface RespondButtonProps {
  onPress: () => void
  isResponding: boolean
}

interface DirectionsButtonProps {
  onPress: () => void
}

interface DetailsButtonProps {
  onPress: () => void
}

interface ActionButtonsRowProps {
  children: React.ReactNode
}

export const ShareButton: React.FC<ShareButtonProps> = ({ onPress }) => {
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  return (
    <TouchableOpacity onPress={onPress} style={styles.actionButton}>
      <FontAwesome5 name="share" size={12} color={theme.colors.interactive} />
      <Text style={styles.actionText}>Share</Text>
    </TouchableOpacity>
  )
}

export const RespondButton: React.FC<RespondButtonProps> = ({ onPress, isResponding }) => {
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  return (
    <TouchableOpacity onPress={onPress} style={styles.actionButton}>
      <FontAwesome5
        name={!isResponding ? "user-plus" : "user-times"}
        size={12}
        color={theme.colors.interactive}
      />
      <Text style={styles.actionText}>{!isResponding ? "Respond" : "Not Responding"}</Text>
    </TouchableOpacity>
  )
}

export const DirectionsButton: React.FC<DirectionsButtonProps> = ({ onPress }) => {
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  return (
    <TouchableOpacity onPress={onPress} style={styles.actionButton}>
      <FontAwesome5 name="directions" size={12} color={theme.colors.interactive} />
      <Text style={styles.actionText}>Directions</Text>
    </TouchableOpacity>
  )
}

export const DetailsButton: React.FC<DetailsButtonProps> = ({ onPress }) => {
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  return (
    <TouchableOpacity onPress={onPress} style={styles.actionButton}>
      <FontAwesome5 name="info-circle" size={12} color={theme.colors.interactive} />
      <Text style={styles.actionText}>Details</Text>
    </TouchableOpacity>
  )
}

export const ActionButtonsRow: React.FC<ActionButtonsRowProps> = ({ children }) => {
  const theme = useAppTheme()
  const styles = makeStyles(theme)
  return <View style={styles.actionButtonsRow}>{children}</View>
}

const makeStyles = (theme) =>
  StyleSheet.create({
    actionButtonsRow: {
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      paddingHorizontal: 16,
      paddingVertical: 12,
      borderTopWidth: 0.5,
      borderTopColor: theme.colors.border,
    },
    actionButton: {
      flexDirection: "row",
      alignItems: "center",
      paddingVertical: 4,
      paddingHorizontal: 8,
    },
    actionText: {
      fontSize: 14,
      color: theme.colors.interactive,
      fontWeight: "400",
      marginLeft: 6,
    },
  })
