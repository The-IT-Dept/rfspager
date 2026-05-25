/**
 * styles for alertZone component
 * @format
 */

import { StyleSheet } from "react-native"
import { AppTheme } from "../../theme/rfspager"

export const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    container: {
      backgroundColor: theme.colors.cardBackground,
      overflow: "hidden",
    },
    headerRow: {
      flexDirection: "row",
      alignItems: "center",
      paddingHorizontal: 16,
      paddingVertical: 12,
      borderBottomWidth: 0.5,
      borderBottomColor: theme.colors.border,
    },
    iconContainer: {
      width: 20,
      height: 20,
      borderRadius: 3,
      justifyContent: "center",
      alignItems: "center",
      marginRight: 12,
    },
    headerContent: {
      flex: 1,
    },
    zoneName: {
      fontSize: 16,
      fontWeight: "600",
      color: theme.colors.textPrimary,
      marginBottom: 2,
    },
    zoneSubtitle: {
      fontSize: 14,
      color: theme.colors.textTertiary,
    },
    deleteButton: {
      padding: 8,
    },
    contentRow: {
      flexDirection: "row",
      alignItems: "stretch",
    },
    infoColumn: {
      flex: 1,
      justifyContent: "center",
      paddingLeft: 16,
      paddingVertical: 10,
    },
    infoItem: {
      flexDirection: "row",
      alignItems: "center",
      paddingVertical: 3,
    },
    infoLabel: {
      fontSize: 13,
      color: theme.colors.textTertiary,
      width: 48,
    },
    infoValue: {
      fontSize: 13,
      color: theme.colors.textPrimary,
      fontVariant: ["tabular-nums"],
    },
    mapContainer: {
      width: 120,
      height: 100,
      overflow: "hidden",
    },
    minimap: {
      width: 120,
      height: 100,
    },
    loader: {
      justifyContent: "center",
      alignItems: "center",
      flex: 1,
      backgroundColor: theme.colors.backgroundTertiary,
      paddingVertical: 20,
    },
  })
