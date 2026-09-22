import React, { useEffect, useState, useCallback } from "react"
import { ActivityIndicator, Text } from "react-native"
import { Platform, StyleSheet, View, Image, TouchableOpacity, ScrollView } from "react-native"
import moment from "moment"
import MapView, { Marker } from "react-native-maps"
import { FontAwesome5 } from "@expo/vector-icons"
import {
  RespondButton,
  ShareButton,
  ActionButtonsRow,
} from "../../components/action-buttons/ActionButtons"

import { useLocalSearchParams, useRouter } from "expo-router"

import {
  hasLocation,
  extractLocation,
  openMapWithNavigation,
  shareMessage,
} from "../../utils/extract-coords"
import { useMessage } from "../../hooks"
import { useAuthStore, useValidSubscription } from "../../stores"
import { api } from "../../services/api-client"
import { useQueryClient } from "@tanstack/react-query"
import { BannerAd } from "../../utils/admob"

import { AppTheme, useAppTheme } from "../../theme"

export function PagerMessageScreen() {
  const { id } = useLocalSearchParams<{ id: string }>()
  const router = useRouter()
  const validSubscription = useValidSubscription()
  const upsertInstallation = useAuthStore((s) => s.upsertInstallation)
  const queryClient = useQueryClient()
  const currentUser = useAuthStore((s) => s.user)
  const [respondLoading, setRespondLoading] = useState(false)
  const [localResponding, setLocalResponding] = useState<boolean | null>(null)
  //const upload = useImageUpload()
  // const [fabState, setFabState] = useState<any>({ open: false })

  useEffect(() => {
    upsertInstallation()
  }, [])

  const theme = useAppTheme()
  const styles = makeStyles(theme)

  const { data: message, isLoading: loading } = useMessage(id)

  const serverResponding =
    message?.am_i_responding ??
    message?.responders?.some(
      (r: any) =>
        r.user_id === currentUser?.id || String(r.user_id) === currentUser?.user_id,
    ) ??
    false
  const userIsResponding = localResponding ?? serverResponding

  const handleRespond = useCallback(async () => {
    if (!message || respondLoading) return
    const previous = userIsResponding
    setRespondLoading(true)
    setLocalResponding(!previous) // optimistic flip
    try {
      const result = await api.messages.respond(message.id)
      setLocalResponding(result.responding)
      queryClient.invalidateQueries({ queryKey: ["message", String(message.id)] })
      queryClient.invalidateQueries({ queryKey: ["messages"] })
    } catch (e) {
      console.error("Failed to respond:", e)
      setLocalResponding(previous) // revert
    } finally {
      setRespondLoading(false)
    }
  }, [message, respondLoading, userIsResponding, queryClient])

  if (loading || !message)
    return (
      <View style={styles.loader}>
        <ActivityIndicator size="large" color={theme.colors.interactive} />
        <Text style={styles.loadingText}>Loading message...</Text>
      </View>
    )

  // Adapt flat REST fields to the shape used in JSX
  const messageAdapted = {
    ...message,
    capcode: {
      id: message.capcode_id,
      colour: message.capcode_colour,
      icon: message.capcode_icon || "fas fa-question",
      agency: message.agency_name,
      alias: message.capcode_alias,
    },
  }
  const responders = messageAdapted.responders ?? []

  const location = extractLocation(messageAdapted)

  const RenderData = ({ heading, desciption }) => (
    <View style={styles.dataRow}>
      <Text style={styles.dataLabel}>{heading}</Text>
      <Text style={styles.dataValue}>{desciption}</Text>
    </View>
  )

  return (
    <View style={styles.container}>
      <ScrollView showsVerticalScrollIndicator={false} contentInsetAdjustmentBehavior="automatic">
        <View style={styles.mainContainer}>
          <View style={styles.headerRow}>
            <View
              style={[
                styles.iconContainer,
                { backgroundColor: messageAdapted.capcode_colour || messageAdapted.capcode?.colour || "#007AFF" },
              ]}
            >
              <FontAwesome5
                name={(messageAdapted.capcode_icon || messageAdapted.capcode?.icon || "fas fa-question").replace("fas fa-", "")}
                size={10}
                color={"#FFFFFF"}
              />
            </View>
            <View style={styles.headerContent}>
              <Text style={styles.agencyName}>{messageAdapted.agency_name || messageAdapted.capcode?.agency || "Unknown"}</Text>
              <Text style={styles.aliasName}>{messageAdapted.capcode_alias || messageAdapted.capcode?.alias || "Unknown"}</Text>
            </View>
            <Text style={styles.timestamp}>
              {moment.utc(messageAdapted.created_at).local().format("HH:mm")}
            </Text>
          </View>
        </View>

        {/* Basic Information Section */}
        <View style={styles.sectionContainer}>
          <View style={styles.sectionHeader}>
            <Text style={styles.sectionTitle}>Basic Information</Text>
          </View>
          <RenderData heading={"Message ID"} desciption={messageAdapted.id} />
          <RenderData heading={"Capcode"} desciption={messageAdapted.address} />
          <RenderData
            heading={"Date & Time"}
            desciption={moment.utc(messageAdapted.created_at).local().format("YYYY-MM-DD HH:mm:ss")}
          />

          <ActionButtonsRow>
            <RespondButton onPress={handleRespond} isResponding={userIsResponding} />
            <ShareButton onPress={() => shareMessage(messageAdapted)} />
          </ActionButtonsRow>
        </View>

        {/* Receiver — which SDR station picked this page off the air.
            Tappable to jump to the system detail.
            Falls back gracefully if upstream didn't link a system. */}
        {messageAdapted.system_name && (
          <View style={styles.sectionContainer}>
            <View style={styles.sectionHeader}>
              <Text style={styles.sectionTitle}>Decoded By</Text>
            </View>
            <TouchableOpacity
              activeOpacity={messageAdapted.system_uuid ? 0.6 : 1}
              disabled={!messageAdapted.system_uuid}
              onPress={() =>
                messageAdapted.system_uuid &&
                router.push(`/system/${messageAdapted.system_uuid}`)
              }
              style={styles.receiverRow}
            >
              <View style={styles.receiverIcon}>
                <FontAwesome5 name="broadcast-tower" size={12} color={theme.colors.textSecondary} />
              </View>
              <View style={styles.receiverContent}>
                <Text style={styles.receiverName}>{messageAdapted.system_name}</Text>
                {messageAdapted.system_suburb && (
                  <Text style={styles.receiverMeta}>{messageAdapted.system_suburb}</Text>
                )}
              </View>
              {messageAdapted.system_uuid && (
                <FontAwesome5
                  name="chevron-right"
                  size={12}
                  color={theme.colors.textTertiary}
                />
              )}
            </TouchableOpacity>
          </View>
        )}

        <View style={styles.messageSection}>
          <Text style={styles.messageText}>{messageAdapted.message}</Text>
        </View>

        {responders.length > 0 && (
          <View style={styles.respondersSection}>
            <View style={styles.respondersHeader}>
              <Text style={styles.respondersLabel}>Responders ({responders.length})</Text>
            </View>
            <View style={styles.avatarRow}>
              {responders.map((responder) => (
                <View key={responder.id} style={styles.avatarContainer}>
                  <Image
                    style={styles.avatar}
                    source={{
                      uri: `https://www.gravatar.com/avatar/${responder.hash}.png?d=robohash&s=64`,
                    }}
                  />
                </View>
              ))}
            </View>
          </View>
        )}

        {/* {message.responders?.length !== 0 && <View style={styles.avatarRow}>
                {message.responders?.map((responder) => {
                    return <Avatar.Image style={{backgroundColor: theme.colors.background}} size={24} key={responder.id}
                                         source={{uri: 'https://www.gravatar.com/avatar/' + responder.hash + '.png?d=robohash'}}/>
                })}
            </View>} */}

        {hasLocation(messageAdapted) && (
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
                  <View
                    style={[
                      styles.mapMarker,
                      { backgroundColor: messageAdapted.capcode?.colour || "#007AFF" },
                    ]}
                  >
                    <FontAwesome5
                      name={messageAdapted.capcode?.icon.replace("fas fa-", "") || "question"}
                      size={10}
                      color={"#FFFFFF"}
                    />
                  </View>
                </Marker>
              )}
            </MapView>
            <View style={styles.mapActions}>
              <TouchableOpacity
                onPress={() => openMapWithNavigation(messageAdapted)}
                style={styles.smallDirectionsButton}
              >
                <FontAwesome5 name="directions" size={12} color={theme.colors.interactive} />
                <Text style={styles.smallActionText}>Get Directions</Text>
              </TouchableOpacity>
            </View>
          </View>
        )}

        {!validSubscription && !hasLocation(messageAdapted) && <View style={{ flex: 1 }} />}

        {/*{false && validSubscription && (*/}
        {/*  <FAB.Group*/}
        {/*    open={fabState?.open}*/}
        {/*    icon={"camera"}*/}
        {/*    onStateChange={setFabState}*/}
        {/*    actions={[*/}
        {/*      {*/}
        {/*        icon: "images",*/}
        {/*        label: "Select from Gallery",*/}
        {/*        onPress: () =>*/}
        {/*          upload.selectImage({ capcodeId: message.capcode.id, messageId: message.id }),*/}
        {/*      },*/}
        {/*      {*/}
        {/*        icon: "camera",*/}
        {/*        label: "Take Photo",*/}
        {/*        onPress: () =>*/}
        {/*          upload.takePhoto({ capcodeId: message.capcode.id, messageId: message.id }),*/}
        {/*      },*/}
        {/*    ]}*/}
        {/*  />*/}
        {/*)}*/}

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
    loader: {
      justifyContent: "center",
      alignItems: "center",
      flex: 1,
      backgroundColor: theme.colors.backgroundTertiary,
    },
    loadingText: {
      marginTop: 16,
      fontSize: 16,
      color: theme.colors.textTertiary,
      fontWeight: "400",
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
    agencyName: {
      fontSize: 16,
      fontWeight: "600",
      color: theme.colors.textPrimary,
      marginBottom: 2,
    },
    aliasName: {
      fontSize: 14,
      color: theme.colors.textTertiary,
    },
    timestamp: {
      fontSize: 13,
      color: theme.colors.textTertiary,
      fontWeight: "400",
    },
    dataRow: {
      flexDirection: "row",
      alignItems: "center",
      paddingHorizontal: 16,
      paddingVertical: 12,
      borderBottomWidth: 0.5,
      borderBottomColor: theme.colors.border,
    },
    dataLabel: {
      fontSize: 15,
      fontWeight: "400",
      color: theme.colors.textPrimary,
      width: 100,
    },
    dataValue: {
      fontSize: 15,
      color: theme.colors.textTertiary,
      flex: 1,
    },
    messageSection: {
      backgroundColor: theme.colors.cardBackground,
      marginHorizontal: 16,
      marginTop: 8,
      paddingHorizontal: 16,
      paddingVertical: 16,
      borderBottomWidth: 0.5,
      borderBottomColor: theme.colors.border,
    },
    messageText: {
      fontSize: 16,
      lineHeight: 22,
      color: theme.colors.textPrimary,
    },
    respondersSection: {
      backgroundColor: theme.colors.cardBackground,
      marginHorizontal: 16,
      marginTop: 8,
    },
    respondersHeader: {
      paddingHorizontal: 16,
      paddingVertical: 12,
      borderBottomWidth: 0.5,
      borderBottomColor: theme.colors.border,
    },
    respondersLabel: {
      fontSize: 15,
      fontWeight: "600",
      color: theme.colors.textPrimary,
    },
    avatarRow: {
      flexDirection: "row",
      flexWrap: "wrap",
      paddingHorizontal: 16,
      paddingBottom: 16,
      paddingTop: 12,
    },
    avatarContainer: {
      marginRight: 8,
      marginBottom: 8,
    },
    avatar: {
      width: 28,
      height: 28,
      borderRadius: 14,
      backgroundColor: theme.colors.backgroundTertiary,
    },
    actionsSection: {
      backgroundColor: theme.colors.cardBackground,
      marginHorizontal: 16,
      marginTop: 8,
      paddingHorizontal: 16,
      paddingVertical: 16,
    },
    smallActionsRow: {
      flexDirection: "row",
      alignItems: "center",
    },
    actionSeparator: {
      fontSize: 14,
      color: theme.colors.textTertiary,
      marginHorizontal: 12,
    },
    smallActionButton: {
      paddingVertical: 4,
      paddingHorizontal: 8,
    },
    smallActionText: {
      fontSize: 14,
      color: theme.colors.interactive,
      fontWeight: "400",
      marginLeft: 6,
    },
    mapSection: {
      backgroundColor: theme.colors.cardBackground,
      marginHorizontal: 16,
      marginTop: 8,
      overflow: "hidden",
    },
    mapHeader: {
      paddingHorizontal: 16,
      paddingVertical: 12,
      borderBottomWidth: 0.5,
      borderBottomColor: theme.colors.border,
    },
    mapLabel: {
      fontSize: 15,
      fontWeight: "600",
      color: theme.colors.textPrimary,
    },
    mapView: {
      height: 400,
    },
    mapActions: {
      padding: 16,
      borderTopWidth: 0.5,
      borderTopColor: theme.colors.border,
    },
    smallDirectionsButton: {
      flexDirection: "row",
      alignItems: "center",
      paddingVertical: 4,
      paddingHorizontal: 8,
    },
    mapMarker: {
      width: 20,
      height: 20,
      borderRadius: 3,
      justifyContent: "center",
      alignItems: "center",
      shadowColor: theme.colors.shadowColor,
      shadowOffset: { width: 0, height: 1 },
      shadowOpacity: 0.2,
      shadowRadius: 2,
      elevation: 3,
    },
    progressContainer: {
      flex: 1,
      alignItems: "center",
      justifyContent: "center",
      paddingVertical: 20,
      backgroundColor: theme.colors.backgroundTertiary,
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
    // Receiver row
    receiverRow: {
      flexDirection: "row",
      alignItems: "center",
      paddingHorizontal: 16,
      paddingVertical: 12,
    },
    receiverIcon: {
      width: 28,
      height: 28,
      borderRadius: 14,
      backgroundColor: theme.colors.backgroundTertiary,
      alignItems: "center",
      justifyContent: "center",
      marginRight: 12,
    },
    receiverContent: {
      flex: 1,
    },
    receiverName: {
      fontSize: 15,
      fontWeight: "600",
      color: theme.colors.textPrimary,
    },
    receiverMeta: {
      fontSize: 13,
      color: theme.colors.textTertiary,
      marginTop: 1,
    },
  })
