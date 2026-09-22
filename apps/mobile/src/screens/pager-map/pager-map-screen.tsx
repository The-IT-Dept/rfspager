import React, { FC, useCallback, useMemo, useRef } from "react"
import { StyleSheet, View } from "react-native"
import { Text } from "react-native"
import MapView, { Callout, Circle, Marker, Polygon } from "react-native-maps"
import { FontAwesome5 } from "@expo/vector-icons"
import moment from "moment"

import { usePreferencesStore } from "../../stores"
import { useAuthStore } from "../../stores"
import { useAlertzones, useMapMessages, useMajorIncidents, useHazardReductions } from "../../hooks"
import { extractLocation, hasLocation } from "../../utils/extract-coords"
import { makeGeoJsonOverlays } from "../../utils/geojson-overlays"
import type { Message, HazardReduction, MajorIncident } from "../../services/api-client"
import { BannerAd } from "../../utils/admob"
import { useFocusEffect } from "expo-router"
import { AppTheme, useAppTheme } from "../../theme"

interface CalloutProps {
  message: Message
}

export const PagerMapScreen: FC =
  function PagerMapScreen() {
    const theme = useAppTheme()

    const styles = makeStyles(theme)
    const mapPageSize = usePreferencesStore((s) => s.mapPageSize)
    const mapShowHR = usePreferencesStore((s) => s.mapShowHR)
    const mapShowUpdates = usePreferencesStore((s) => s.mapShowUpdates)
    const darkMode = usePreferencesStore((s) => s.darkMode)
    const upsertInstallation = useAuthStore((s) => s.upsertInstallation)

    const mapRef = useRef(null)

    const createdAfter = useMemo(() => moment().subtract(24, "hours").toISOString(), [])

    const { data: alertzones, refetch: refetchAlertzones } = useAlertzones()
    const { data: mapMessagesData, refetch: refetchMessages } = useMapMessages({ limit: mapPageSize, createdAfter })
    const { data: majorIncidentsResp, refetch: refetchMajorIncidents } = useMajorIncidents({ perPage: mapPageSize })
    const { data: hazardReductionsResp, refetch: refetchHazardReductions } = useHazardReductions({ perPage: mapPageSize })
    const majorIncidentsData = majorIncidentsResp?.data || []
    const hazardReductionsData = hazardReductionsResp?.data || []

    useFocusEffect(
      useCallback(() => {
        refetchMessages()
        refetchAlertzones()
        refetchMajorIncidents()
        refetchHazardReductions()
        upsertInstallation()
      }, []),
    )

    const CustomCallOut = (props: CalloutProps) => {
      const { message } = props

      const alias = message.capcode_alias || "Unknown"
      const agency = message.agency_name || "Unknown"

      return (
        <View style={styles.calloutContainer}>
          <Text style={styles.bold}>{alias + " | " + agency}</Text>
          <Text style={styles.calloutDate}>{moment.utc(message.created_at).local().fromNow()}</Text>
          <View style={styles.calloutRow}>
            <Text style={styles.calloutText}>{message.message}</Text>
          </View>
        </View>
      )
    }
    const renderHRCallout = (hr: HazardReduction) => {
      return (
        <View style={styles.calloutContainer}>
          <Text style={styles.bold}>Hazard Reduction Burn - {hr.title}</Text>

          <View style={styles.calloutHRContainer}>
            <View style={styles.calloutRow}>
              <View style={styles.calloutHRCol}>
                <Text style={styles.bold}>Start Date</Text>
              </View>
              <View style={styles.calloutHRColRow}>
                <Text style={styles.calloutText}>{hr.start_at}</Text>
              </View>
            </View>

            <View style={styles.calloutRow}>
              <View style={styles.calloutHRCol}>
                <Text style={styles.bold}>End Date</Text>
              </View>
              <View style={styles.calloutHRColRow}>
                <Text style={styles.calloutText}>{hr.end_at}</Text>
              </View>
            </View>

            <View style={styles.calloutRow}>
              <View style={styles.calloutHRCol}>
                <Text style={styles.bold}>Size</Text>
              </View>
              <View style={styles.calloutHRColRow}>
                <Text style={styles.calloutText}>{hr.size} ha</Text>
              </View>
            </View>

            <View style={styles.calloutRow}>
              <View style={styles.calloutHRCol}>
                <Text style={styles.bold}>Lead Agency</Text>
              </View>
              <View style={styles.calloutHRColRow}>
                <Text style={styles.calloutText}>{hr.leadAgency}</Text>
              </View>
            </View>

            <View style={styles.calloutRow}>
              <View style={styles.calloutHRCol}>
                <Text style={styles.bold}>Supporting Agencies</Text>
              </View>
              <View style={styles.calloutHRColRow}>
                <Text style={styles.calloutText}>{hr.supportingAgencies}</Text>
              </View>
            </View>

            <View style={styles.calloutRow}>
              <View style={styles.calloutHRCol}>
                <Text style={styles.bold}>Tenure</Text>
              </View>
              <View style={styles.calloutHRColRow}>
                <Text style={styles.calloutText}>{hr.tenure}</Text>
              </View>
            </View>

            <View style={styles.calloutRow}>
              <View style={styles.calloutHRCol}>
                <Text style={styles.bold}>LGA</Text>
              </View>
              <View style={styles.calloutHRColRow}>
                <Text style={styles.calloutText}>{hr.lga}</Text>
              </View>
            </View>
          </View>
        </View>
      )
    }
    const renderMICallout = (incident: MajorIncident) => {
      const data = typeof incident.data === "string" ? JSON.parse(incident.data) : incident.data || {}

      return (
        <View style={styles.calloutContainer}>
          <Text style={styles.bold}>Major Incident - {incident.title}</Text>

          <View style={styles.calloutHRContainer}>
            <View style={styles.calloutRow}>
              <View style={styles.calloutHRCol}>
                <Text style={styles.bold}>Date</Text>
              </View>
              <View style={styles.calloutHRColRow}>
                <Text style={styles.calloutText}>{incident.created_at}</Text>
              </View>
            </View>

            <View style={styles.calloutRow}>
              <View style={styles.calloutHRCol}>
                <Text style={styles.bold}>Category</Text>
              </View>
              <View style={styles.calloutHRColRow}>
                <Text style={styles.calloutText}>{incident.category}</Text>
              </View>
            </View>

            <View style={styles.calloutRow}>
              <View style={styles.calloutHRCol}>
                <Text style={styles.bold}>Council Area</Text>
              </View>
              <View style={styles.calloutHRColRow}>
                <Text style={styles.calloutText}>{data.councilArea || "Unknown"}</Text>
              </View>
            </View>
            <View style={styles.calloutRow}>
              <View style={styles.calloutHRCol}>
                <Text style={styles.bold}>Responsible Agency</Text>
              </View>
              <View style={styles.calloutHRColRow}>
                <Text style={styles.calloutText}>{data.responsibleAgency || "Unknown"}</Text>
              </View>
            </View>

            <View style={styles.calloutRow}>
              <View style={styles.calloutHRCol}>
                <Text style={styles.bold}>Status</Text>
              </View>
              <View style={styles.calloutHRColRow}>
                <Text style={styles.calloutText}>{data.status || "Unknown"}</Text>
              </View>
            </View>

            <View style={styles.calloutRow}>
              <View style={styles.calloutHRCol}>
                <Text style={styles.bold}>Size</Text>
              </View>
              <View style={styles.calloutHRColRow}>
                <Text style={styles.calloutText}>{data.size || "Unknown"}</Text>
              </View>
            </View>
          </View>
        </View>
      )
    }

    // Adapt flat REST message fields to shape used by map overlays
    const adaptMessage = (msg: Message) => ({
      ...msg,
      capcode: {
        icon: msg.capcode_icon || "fas fa-question",
        colour: msg.capcode_colour,
        agency: msg.agency_name,
        alias: msg.capcode_alias,
      },
    })

    const mmOverlays =
      (mapMessagesData || []).filter(hasLocation).map((msg) => {
        const message = adaptMessage(msg)
        const location = extractLocation(message)
        let icon = message.capcode?.icon.replace("fas fa-", "") || "question"

        if (message.message.includes("MVA")) icon = "car-crash"

        if (message.message.includes("Vehicle")) icon = "car"

        return {
          type: "Feature",
          id: message.id,
          properties: {
            id: message.id,
            icon: icon,
            colour: message.capcode?.colour || theme.colors.accent,
            message: message.message,
            agency: message.capcode?.agency || "Unknown",
            alias: message.capcode?.alias || "Unknown",
            raw: msg,
          },
          geometry: {
            type: "Point",
            coordinates: [location.longitude, location.latitude],
          },
        }
      }) || []

    const hrOverlays = makeGeoJsonOverlays(
      (hazardReductionsData || []).map((hr) => {
        const geometry = typeof hr.geometry === "string" ? JSON.parse(hr.geometry) : hr.geometry
        return {
          id: hr.id,
          type: "Feature",
          geometry: geometry,
          properties: hr,
        }
      }) || [],
    )
    const miOverlays = makeGeoJsonOverlays(
      (majorIncidentsData || []).map((mi) => {
        const geometry = typeof mi.geometry === "string" ? JSON.parse(mi.geometry) : mi.geometry
        return {
          id: mi.id,
          type: "Feature",
          geometry: geometry,
          properties: mi,
        }
      }) || [],
    )

    return (
      <View style={styles.container}>
        <MapView
          ref={mapRef}
          showsUserLocation={true}
          showsMyLocationButton={false}
          zoomControlEnabled={false}
          showsCompass={false}
          toolbarEnabled={false}
          showsScale={false}
          showsBuildings={false}
          showsIndoors={false}
          showsTraffic={false}
          userInterfaceStyle={darkMode ? "dark" : "light"}
          initialRegion={{
            latitude: -33.850473544275076,
            latitudeDelta: 2.5024496947281634,
            longitude: 151.34355350194977,
            longitudeDelta: 5.132560925070095,
          }}
          style={styles.map}
        >
          {mapShowHR &&
            hrOverlays.map((overlay, index) => {
              if (overlay.type === "polygon") {
                return (
                  <Marker key={1000 + index} coordinate={overlay.coordinates[0]}>
                    <View style={styles.markerContainer}>
                      <FontAwesome5 size={10} color={"#FFFFFF"} name={"burn"} />
                    </View>
                    <Callout>{renderHRCallout(overlay.feature.properties)}</Callout>
                  </Marker>
                )
              }
              return null
            })}
          {mapShowHR &&
            hrOverlays.map((overlay, index) => {
              if (overlay.type === "polygon") {
                return (
                  <Polygon
                    key={2000 + index}
                    coordinates={overlay.coordinates}
                    holes={overlay.holes}
                    strokeColor={"#FF6B47"}
                    fillColor={"#FF6B4715"}
                    strokeWidth={2}
                  />
                )
              }
              return null
            })}
          {mapShowUpdates &&
            miOverlays.map((overlay, index) => {
              if (overlay.type === "point") {
                return (
                  <Marker key={3000 + index} coordinate={overlay.coordinates}>
                    <View style={styles.emergencyMarker}>
                      <FontAwesome5 size={10} color={"#FFFFFF"} name={"fire"} />
                    </View>
                    <Callout>{renderMICallout(overlay.feature.properties)}</Callout>
                  </Marker>
                )
              }
              return null
            })}

          {mmOverlays.map((feature, index) => {
            if (feature.geometry.type === "Point") {
              return (
                <Marker
                  key={4000 + index}
                  coordinate={{
                    longitude: feature.geometry.coordinates[0],
                    latitude: feature.geometry.coordinates[1],
                  }}
                >
                  <View
                    style={[
                      styles.messageMarker,
                      { backgroundColor: feature.properties.colour || "#007AFF" },
                    ]}
                  >
                    <FontAwesome5 size={10} color={"#FFFFFF"} name={feature.properties.icon} />
                  </View>
                  <Callout>
                    <CustomCallOut message={feature.properties.raw} />
                  </Callout>
                </Marker>
              )
            }

            return null
          })}

          {alertzones &&
            alertzones.map((alertzone) => (
              <Circle
                key={5000 + alertzone.id}
                center={{ latitude: alertzone.lat, longitude: alertzone.lng }}
                radius={alertzone.radius * 1000}
                strokeWidth={2}
                strokeColor={"#007AFF"}
                fillColor={"#007AFF15"}
              />
            ))}

          {alertzones &&
            alertzones.map((alertzone) => (
              <Marker
                key={6000 + alertzone.id}
                title={"Alert Zone - " + alertzone.name}
                description={"Radius: " + alertzone.radius + "km"}
                coordinate={{ latitude: alertzone.lat, longitude: alertzone.lng }}
              >
                <View style={styles.alertZoneMarker}>
                  <FontAwesome5 size={10} color={"#FFFFFF"} name={"eye"} />
                </View>
              </Marker>
            ))}
        </MapView>

        <BannerAd />
      </View>
    )
  }

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    bold: {
      color: theme.colors.textPrimary,
      fontWeight: "600",
      fontSize: 16,
    },
    calloutContainer: {
      flex: 0,
      flexDirection: "column",
      minWidth: 280,
      maxWidth: 320,
      backgroundColor: theme.colors.cardBackground,
      borderRadius: 12,
      padding: 12,
      shadowColor: "#000000",
      shadowOffset: { width: 0, height: 2 },
      shadowOpacity: 0.15,
      shadowRadius: 8,
      elevation: 5,
    },
    calloutDate: {
      color: theme.colors.textTertiary,
      fontSize: 13,
      marginTop: 4,
      marginBottom: 8,
    },
    calloutHRCol: {
      flex: 1,
      flexShrink: 1,
      maxWidth: 100,
    },
    calloutHRColRow: {
      flex: 1,
      flexShrink: 1,
    },
    calloutHRContainer: {
      flex: 1,
      width: "100%",
      marginTop: 8,
    },
    calloutRow: {
      flex: 1,
      flexDirection: "row",
      width: "100%",
      paddingVertical: 4,
      borderBottomWidth: 0.5,
      borderBottomColor: theme.colors.border,
    },
    calloutText: {
      flex: 1,
      flexShrink: 1,
      flexWrap: "wrap",
      color: theme.colors.textPrimary,
      fontSize: 14,
      lineHeight: 18,
    },
    container: {
      backgroundColor: theme.colors.backgroundTertiary,
      flex: 1,
      height: "100%",
      width: "100%",
    },
    map: {
      flex: 1,
    },
    markerContainer: {
      width: 20,
      height: 20,
      borderRadius: 10,
      backgroundColor: "#FF6B47", // Orange for hazard reduction
      justifyContent: "center",
      alignItems: "center",
      shadowColor: "#000000",
      shadowOffset: { width: 0, height: 2 },
      shadowOpacity: 0.3,
      shadowRadius: 3,
      elevation: 5,
    },
    emergencyMarker: {
      width: 20,
      height: 20,
      borderRadius: 10,
      backgroundColor: "#FF3B30", // Red for emergencies
      justifyContent: "center",
      alignItems: "center",
      shadowColor: "#000000",
      shadowOffset: { width: 0, height: 2 },
      shadowOpacity: 0.3,
      shadowRadius: 3,
      elevation: 5,
    },
    messageMarker: {
      width: 18,
      height: 18,
      borderRadius: 9,
      justifyContent: "center",
      alignItems: "center",
      shadowColor: "#000000",
      shadowOffset: { width: 0, height: 1 },
      shadowOpacity: 0.2,
      shadowRadius: 2,
      elevation: 3,
    },
    alertZoneMarker: {
      width: 20,
      height: 20,
      borderRadius: 10,
      backgroundColor: "#007AFF", // iOS blue for alert zones
      justifyContent: "center",
      alignItems: "center",
      shadowColor: "#000000",
      shadowOffset: { width: 0, height: 2 },
      shadowOpacity: 0.3,
      shadowRadius: 3,
      elevation: 5,
    },
  })
