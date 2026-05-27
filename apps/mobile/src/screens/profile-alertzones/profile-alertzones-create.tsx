/**
 * ProfileAlertzonesCreateScreen
 * @format
 */

import React, { FC, useState } from "react"
import { Alert, View, TouchableOpacity, ScrollView, TextInput as RNTextInput } from "react-native"
import { Text } from "react-native"
import Slider from "@react-native-community/slider"
import MapView, { Circle, LatLng, Marker } from "react-native-maps"
import { useRouter } from "expo-router"

import { nameValidator } from "../../utils/validators"
import { useCreateAlertzone } from "../../hooks"
import { usePreferencesStore } from "../../stores"
import { makestyles } from "./styles"
import { useAppTheme } from "../../theme"

export const ProfileAlertzonesCreateScreen: FC =
  function ProfileAlertzonesCreatecreen() {
    const router = useRouter()
    const [pin, setPin] = useState<LatLng>()
    const [radius, setRadius] = useState<number>(5)
    const [name, setName] = useState({ value: "", error: "" })
    const [locationError, setLocationError] = useState("")

    // TODO: Add address search functionality for map
    // - Add search bar above map
    // - Implement geocoding to convert addresses to coordinates
    // - Auto-place pin when address is selected
    const createAlertzone = useCreateAlertzone()
    const darkMode = usePreferencesStore((s) => s.darkMode)
    const theme = useAppTheme()
    const styles = makestyles(theme)
    const saveAlertzone = async () => {
      const nameError = nameValidator(name.value)
      const hasLocation = pin !== undefined

      // Clear previous errors
      setLocationError("")

      // Check for validation errors
      if (nameError) {
        setName({ ...name, error: nameError })
      }

      if (!hasLocation) {
        setLocationError("Please select a location on the map")
      }

      // Don't save if there are any errors
      if (nameError || !hasLocation) {
        return
      }

      createAlertzone.mutateAsync({
        name: name.value, radius: radius, lat: pin.latitude, lng: pin.longitude,
      }).then(
        () => router.back(),
        () => Alert.alert("Error", "Error saving Alert Zone. Please try again."),
      )
    }


    return (
      <ScrollView
        style={styles.createContainer}
        showsVerticalScrollIndicator={false}
        contentInsetAdjustmentBehavior="automatic"
        automaticallyAdjustKeyboardInsets
        keyboardShouldPersistTaps="handled"
        contentContainerStyle={{ paddingBottom: 48 }}
      >
        {/* Zone Details Form */}
        <View style={styles.formContainer}>
          <View style={styles.formHeader}>
            <Text style={styles.formTitle}>Zone Details</Text>
          </View>

          <View style={name.error ? styles.inputRow : styles.inputRowLast}>
            <Text style={styles.inputLabel}>Name</Text>
            <RNTextInput
              style={name.error ? styles.textInputError : styles.textInput}
              value={name.value}
              placeholder="Enter zone name"
              onChangeText={(text) => setName({ value: text, error: "" })}
              returnKeyType="done"
              placeholderTextColor={theme.colors.textTertiary}
            />
          </View>

          {name.error ? (
            <View style={styles.errorRow}>
              <Text style={styles.errorText}>{name.error}</Text>
            </View>
          ) : null}
        </View>

        {/* Radius Slider */}
        <View style={styles.sliderContainer}>
          <View style={styles.sliderHeader}>
            <Text style={styles.sliderTitle}>Coverage Radius</Text>
            <Text style={styles.radiusValue}>{radius} km</Text>
          </View>

          <View style={styles.sliderContent}>
            <Slider
              style={styles.slider}
              minimumValue={5}
              maximumValue={100}
              step={1}
              value={radius}
              minimumTrackTintColor={theme.colors.primary}
              maximumTrackTintColor={theme.colors.borderSecondary}
              onValueChange={setRadius}
              thumbTintColor={theme.colors.primary}
            />
            <View style={styles.sliderLabels}>
              <Text style={styles.sliderLabel}>5 km</Text>
              <Text style={styles.sliderLabel}>100 km</Text>
            </View>
          </View>
        </View>

        {/* Map Section */}
        <View style={locationError ? styles.mapContainerError : styles.mapContainer}>
          <View style={styles.mapHeader}>
            <Text style={styles.mapTitle}>Select Location</Text>
            <Text style={locationError ? styles.mapDescriptionError : styles.mapDescription}>
              {locationError || "Long press on the map to place your alert zone"}
            </Text>
          </View>

          <MapView
            style={styles.map}
            showsUserLocation={true}
            showsMyLocationButton={true}
            zoomControlEnabled={true}
            showsCompass={true}
            toolbarEnabled={false}
            userInterfaceStyle={darkMode ? "dark" : "light"}
            initialRegion={{
              latitude: -33.850473544275076,
              latitudeDelta: 2.5024496947281634,
              longitude: 151.34355350194977,
              longitudeDelta: 5.132560925070095,
            }}
            onLongPress={(event) => {
              setPin(event.nativeEvent.coordinate)
              setLocationError("") // Clear error when location is selected
            }}
          >
            {pin && <Marker coordinate={pin} />}
            {pin && (
              <Circle
                center={pin}
                radius={radius * 1000}
                strokeWidth={2}
                strokeColor={theme.colors.primary}
                fillColor="rgba(0, 122, 255, 0.2)"
              />
            )}
          </MapView>
        </View>

        {/* Action Buttons */}
        <View style={styles.actionsContainer}>
          <TouchableOpacity
            style={[styles.actionButton, styles.saveButton]}
            onPress={saveAlertzone}
          >
            <Text style={styles.saveButtonText}>Save Alert Zone</Text>
          </TouchableOpacity>

          <TouchableOpacity
            style={[styles.actionButtonLast, styles.discardButton]}
            onPress={() => router.back()}
          >
            <Text style={styles.discardButtonText}>Cancel</Text>
          </TouchableOpacity>
        </View>
      </ScrollView>
    )
  }
