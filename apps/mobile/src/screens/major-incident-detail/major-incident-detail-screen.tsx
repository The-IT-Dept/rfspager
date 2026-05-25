import React, { FC } from "react"
import { ScrollView, StyleSheet, View, Platform, TouchableOpacity } from "react-native"
import { ActivityIndicator, Text } from "react-native"
import { FontAwesome5 } from "@expo/vector-icons"
import moment from "moment"
import { useLocalSearchParams } from "expo-router"
import MapView, { Marker } from "react-native-maps"

import { useMajorIncident } from "../../hooks"
import { BannerAd } from "../../utils/admob"
import { AppTheme, useAppTheme } from "../../theme"
import { Share } from "react-native"
import { showLocation } from "react-native-map-link"
import {
  ShareButton,
  ActionButtonsRow,
} from "../../components/action-buttons/ActionButtons"

const getStatusColor = (category: string | null | undefined): string => {
  if (!category) return "#8E8E93"

  const categoryLower = category.toLowerCase()
  if (categoryLower.includes("emergency") || categoryLower.includes("immediate")) return "#FF3B30"
  if (categoryLower.includes("watch") || categoryLower.includes("act")) return "#FF9500"
  if (categoryLower.includes("advice")) return "#FFCC00"
  if (categoryLower.includes("control") || categoryLower.includes("contained")) return "#34C759"

  return "#FF3B30" // Default to emergency for unknown types
}

const getStatusIcon = (category: string | null | undefined): string => {
  if (!category) return "exclamation-triangle"

  const categoryLower = category.toLowerCase()
  if (categoryLower.includes("fire") || categoryLower.includes("bush")) return "fire"
  if (categoryLower.includes("flood")) return "water"
  if (categoryLower.includes("storm")) return "cloud-rain"
  if (categoryLower.includes("accident")) return "car-crash"

  return "exclamation-triangle"
}

const RenderData = ({ heading, description }) => {
  const theme = useAppTheme()
  const styles = makeStyles(theme)

  if (!description || description === "Unknown" || description === "") return null

  return (
    <View style={styles.dataRow}>
      <Text style={styles.dataLabel}>{heading}</Text>
      <Text style={styles.dataValue}>{description}</Text>
    </View>
  )
}

export const MajorIncidentDetailScreen: FC = function MajorIncidentDetailScreen() {
  const theme = useAppTheme()
  const { id } = useLocalSearchParams<{ id: string }>()

  const { data: incident, isLoading: loading } = useMajorIncident(id)

  const styles = makeStyles(theme)

  if (loading || !incident) {
    return (
      <View style={styles.loadingContainer}>
        <ActivityIndicator size="large" color={theme.colors.interactive} />
        <Text style={styles.loadingText}>Loading incident details...</Text>
      </View>
    )
  }

  const incidentData = incident.data ? (typeof incident.data === "string" ? JSON.parse(incident.data) : incident.data) : {}

  const statusColor = getStatusColor(incident.category)
  const statusIcon = getStatusIcon(incident.category)
  const timeFormatted = moment(incident.created_at).format("DD/MM/YYYY HH:mm")
  const updatedTimeFormatted = moment(incident.updated_at).format("DD/MM/YYYY HH:mm")

  // Extract main properties
  const councilArea = incidentData.councilArea || "Unknown"
  const responsibleAgency = incidentData.responsibleAgency || "Unknown"
  // const status = incidentData.status || incident.category
  const incidentType = incidentData.type || incident.category || "Unknown"
  const size = incidentData.size || "Unknown"

  // Extract location from geometry if available
  console.log("Incident Data:", incident)
  let geometryData = null
  try {
    geometryData = incident.geometry ? (typeof incident.geometry === "string" ? JSON.parse(incident.geometry) : incident.geometry) : null
  } catch (e) {
    console.log("Failed to parse geometry:", e)
  }

  const hasLocation =
    geometryData && geometryData.coordinates && geometryData.coordinates.length === 2
  const location = hasLocation
    ? {
        latitude: geometryData.coordinates[1],
        longitude: geometryData.coordinates[0],
      }
    : null

  console.log("Geometry Data:", geometryData)
  console.log("Location:", location)
  console.log("Has Location:", hasLocation)

  const handleShare = async () => {
    try {
      await Share.share({
        message: `Major Incident: ${incident.title} in ${councilArea} - ${incidentType}`,
      })
    } catch (error) {
      console.error("Error sharing:", error)
    }
  }

  const handleDirections = () => {
    if (location) {
      showLocation({
        latitude: location.latitude,
        longitude: location.longitude,
        title: `${incident.title} - ${councilArea}`,
        directionsMode: "car",
        googleForceLatLon: true,
      })
    }
  }

  return (
    <View style={styles.container}>
      <ScrollView style={styles.scrollView} showsVerticalScrollIndicator={false} contentInsetAdjustmentBehavior="automatic">
        {/* Main Container */}
        <View style={styles.mainContainer}>
          <View style={styles.headerRow}>
            <View style={[styles.iconContainer, { backgroundColor: statusColor }]}>
              <FontAwesome5 name={statusIcon} size={10} color={theme.colors.white} />
            </View>
            <View style={styles.headerContent}>
              <Text style={styles.incidentTitle}>{incident.title || "Untitled Incident"}</Text>
              <Text style={styles.location}>{councilArea}</Text>
            </View>
            <Text style={styles.timestamp}>{timeFormatted}</Text>
          </View>
        </View>

        {/* Basic Information Section */}
        <View style={styles.sectionContainer}>
          <View style={styles.sectionHeader}>
            <Text style={styles.sectionTitle}>Basic Information</Text>
          </View>
          <RenderData heading="Type" description={incidentType} />
          <RenderData heading="Size" description={size} />
          <RenderData heading="Agency" description={responsibleAgency} />
          <RenderData heading="Location" description={councilArea} />
          <RenderData heading="Last Updated" description={updatedTimeFormatted} />

          <ActionButtonsRow>
            <View />
            <ShareButton onPress={handleShare} />
          </ActionButtonsRow>
        </View>

        {/* Additional Details Section */}
        {Object.keys(incidentData).filter((key) => {
          const value = incidentData[key]
          return (
            value &&
            value !== "Unknown" &&
            value !== "" &&
            !["councilArea", "responsibleAgency", "status", "size"].includes(key)
          )
        }).length > 0 && (
          <View style={styles.sectionContainer}>
            <View style={styles.sectionHeader}>
              <Text style={styles.sectionTitle}>Additional Details</Text>
            </View>
            {Object.keys(incidentData)
              .filter((key) => {
                const value = incidentData[key]
                return (
                  value &&
                  value !== "Unknown" &&
                  value !== "" &&
                  !["councilArea", "responsibleAgency", "status", "size"].includes(key)
                )
              })
              .map((key) => {
                const value = incidentData[key]
                const displayKey = key
                  .replace(/([A-Z])/g, " $1")
                  .replace(/^./, (str) => str.toUpperCase())
                  .replace(/\b\w/g, (str) => str.toUpperCase())

                return <RenderData key={key} heading={displayKey} description={String(value)} />
              })}
          </View>
        )}

        {/* Map Section */}
        {hasLocation && location && (
          <View style={styles.mapSection}>
            <View style={styles.mapHeader}>
              <Text style={styles.mapLabel}>Location</Text>
            </View>
            <MapView
              showsUserLocation={true}
              showsMyLocationButton={false}
              zoomControlEnabled={false}
              showsCompass={false}
              toolbarEnabled={false}
              style={styles.mapView}
              initialRegion={{
                latitude: location.latitude,
                latitudeDelta: 0.02,
                longitude: location.longitude,
                longitudeDelta: 0.02,
              }}
            >
              {Platform.OS !== "web" && (
                <Marker coordinate={location}>
                  <View style={[styles.mapMarker, { backgroundColor: statusColor }]}>
                    <FontAwesome5 name={statusIcon} size={10} color={theme.colors.white} />
                  </View>
                </Marker>
              )}
            </MapView>
            <View style={styles.mapActions}>
              <TouchableOpacity onPress={handleDirections} style={styles.smallDirectionsButton}>
                <FontAwesome5 name="directions" size={12} color={theme.colors.interactive} />
                <Text style={styles.smallActionText}>Get Directions</Text>
              </TouchableOpacity>
            </View>
          </View>
        )}

        <BannerAd />
      </ScrollView>
    </View>
  )
}

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    container: {
      flex: 1,
      backgroundColor: theme.colors.backgroundTertiary,
    },
    loadingContainer: {
      flex: 1,
      justifyContent: "center",
      alignItems: "center",
      backgroundColor: theme.colors.backgroundTertiary,
    },
    loadingText: {
      marginTop: 16,
      fontSize: 16,
      color: theme.colors.textTertiary,
      fontWeight: "400",
    },
    scrollView: {
      flex: 1,
    },
    mainContainer: {
      backgroundColor: theme.colors.cardBackground,
      marginHorizontal: 16,
      marginTop: 16,
      marginBottom: 8,
    },
    headerRow: {
      flexDirection: "row",
      alignItems: "center",
      paddingHorizontal: 16,
      paddingVertical: 16,
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
    incidentTitle: {
      fontSize: 16,
      fontWeight: "600",
      color: theme.colors.textPrimary,
      marginBottom: 2,
    },
    location: {
      fontSize: 14,
      color: theme.colors.textTertiary,
    },
    timestamp: {
      fontSize: 13,
      color: theme.colors.textTertiary,
      fontWeight: "400",
    },
    sectionContainer: {
      backgroundColor: theme.colors.cardBackground,
      marginHorizontal: 16,
      marginBottom: 8,
    },
    sectionHeader: {
      paddingHorizontal: 16,
      paddingVertical: 12,
      borderBottomWidth: 0.5,
      borderBottomColor: theme.colors.border,
    },
    sectionTitle: {
      fontSize: 16,
      fontWeight: "600",
      color: theme.colors.textPrimary,
    },
    dataRow: {
      flexDirection: "row",
      paddingHorizontal: 16,
      paddingVertical: 12,
      borderBottomWidth: 0.5,
      borderBottomColor: theme.colors.border,
    },
    dataLabel: {
      fontSize: 15,
      fontWeight: "400",
      color: theme.colors.textTertiary,
      minWidth: 120,
      marginRight: 12,
    },
    dataValue: {
      fontSize: 15,
      fontWeight: "400",
      color: theme.colors.textPrimary,
      flex: 1,
    },
    mapSection: {
      backgroundColor: theme.colors.cardBackground,
      marginHorizontal: 16,
      marginBottom: 8,
    },
    mapHeader: {
      paddingHorizontal: 16,
      paddingVertical: 12,
      borderBottomWidth: 0.5,
      borderBottomColor: theme.colors.border,
    },
    mapLabel: {
      fontSize: 16,
      fontWeight: "600",
      color: theme.colors.textPrimary,
    },
    mapView: {
      height: 400,
      width: "100%",
    },
    mapActions: {
      padding: 16,
      borderTopWidth: 0.5,
      borderTopColor: theme.colors.border,
    },
    smallActionsRow: {
      flexDirection: "row",
      alignItems: "center",
      paddingHorizontal: 16,
      paddingVertical: 12,
      borderTopWidth: 0.5,
      borderTopColor: theme.colors.border,
    },
    smallActionButton: {
      flexDirection: "row",
      alignItems: "center",
      paddingVertical: 4,
      paddingHorizontal: 8,
    },
    smallDirectionsButton: {
      flexDirection: "row",
      alignItems: "center",
      paddingVertical: 4,
      paddingHorizontal: 8,
    },
    smallActionText: {
      fontSize: 14,
      color: theme.colors.interactive,
      fontWeight: "400",
      marginLeft: 6,
    },
    mapMarker: {
      width: 20,
      height: 20,
      borderRadius: 3,
      justifyContent: "center",
      alignItems: "center",
    },
  })
