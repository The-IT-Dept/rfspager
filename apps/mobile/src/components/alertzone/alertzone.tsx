/**
 * alertzone component
 * @format
 */
import * as React from "react"
import { View, Text, TouchableOpacity, Alert } from "react-native"
import { ActivityIndicator } from "react-native"
import MapView, { Circle, Marker } from "react-native-maps"
import { MaterialCommunityIcons } from "@expo/vector-icons"
import { makeStyles } from "./styles"
import { useAppTheme } from "../../theme"

export interface AlertzoneProps {
  alertzone: any
  onDelete: () => void
}

export function Alertzone(props: AlertzoneProps) {
  const { alertzone, onDelete } = props
  const theme = useAppTheme()
  const styles = makeStyles(theme)

  if (typeof alertzone === "undefined") {
    return (
      <View style={styles.loader}>
        <ActivityIndicator />
      </View>
    )
  }

  // Calculate delta to fit the circle in the minimap
  const radiusKm = alertzone.radius || 5
  const latDelta = (radiusKm / 111) * 2.5
  const lngDelta = latDelta

  return (
    <>
      {alertzone.name && (
        <View style={styles.container}>
          <View style={styles.headerRow}>
            <View style={[styles.iconContainer, { backgroundColor: "#007AFF" }]}>
              <MaterialCommunityIcons name="map-marker" size={10} color="#FFFFFF" />
            </View>
            <View style={styles.headerContent}>
              <Text style={styles.zoneName}>{alertzone.name}</Text>
              <Text style={styles.zoneSubtitle}>{radiusKm} km radius</Text>
            </View>
            <TouchableOpacity
              onPress={() =>
                Alert.alert("Delete Alert Zone", "This action cannot be undone.", [
                  { text: "Cancel", style: "cancel" },
                  {
                    text: "Delete",
                    style: "destructive",
                    onPress: () => alertzone.delete(onDelete),
                  },
                ])
              }
              style={styles.deleteButton}
            >
              <MaterialCommunityIcons name="trash-can-outline" size={16} color="#FF3B30" />
            </TouchableOpacity>
          </View>

          {alertzone.lat && alertzone.lng && (
            <View style={styles.contentRow}>
              <View style={styles.infoColumn}>
                <View style={styles.infoItem}>
                  <Text style={styles.infoLabel}>Lat</Text>
                  <Text style={styles.infoValue}>{alertzone.lat.toFixed(4)}</Text>
                </View>
                <View style={styles.infoItem}>
                  <Text style={styles.infoLabel}>Lng</Text>
                  <Text style={styles.infoValue}>{alertzone.lng.toFixed(4)}</Text>
                </View>
                <View style={styles.infoItem}>
                  <Text style={styles.infoLabel}>Radius</Text>
                  <Text style={styles.infoValue}>{radiusKm} km</Text>
                </View>
              </View>
              <View style={styles.mapContainer}>
                <MapView
                  style={styles.minimap}
                  scrollEnabled={false}
                  zoomEnabled={false}
                  rotateEnabled={false}
                  pitchEnabled={false}
                  initialRegion={{
                    latitude: alertzone.lat,
                    longitude: alertzone.lng,
                    latitudeDelta: latDelta,
                    longitudeDelta: lngDelta,
                  }}
                >
                  <Marker
                    coordinate={{ latitude: alertzone.lat, longitude: alertzone.lng }}
                    pinColor="#007AFF"
                  />
                  <Circle
                    center={{ latitude: alertzone.lat, longitude: alertzone.lng }}
                    radius={radiusKm * 1000}
                    strokeColor="rgba(0, 122, 255, 0.6)"
                    fillColor="rgba(0, 122, 255, 0.1)"
                    strokeWidth={1}
                  />
                </MapView>
              </View>
            </View>
          )}
        </View>
      )}
    </>
  )
}
