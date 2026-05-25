import React, { FC, useRef, useEffect } from "react"
import {
  FlatList,
  RefreshControl,
  StyleSheet,
  View,
  Pressable,
  Animated,
  Share,
} from "react-native"
import { ActivityIndicator, Text } from "react-native"
import { FontAwesome5 } from "@expo/vector-icons"
import moment from "moment"
import { useRouter } from "expo-router"

import { useMajorIncidentsPaginated } from "../../hooks"
import type { MajorIncident } from "../../services/api-client"
import { BannerAd } from "../../utils/admob"
import {
  DetailsButton,
  DirectionsButton,
  ShareButton,
} from "../../components/action-buttons/ActionButtons"
import { AppTheme, useAppTheme } from "../../theme"
import { adManager } from "../../utils/ad-manager"
import { useInterstitialAd } from "../../utils/interstitial-ad-manager"
import { NativeAdCell } from "../../components/native-ad-cell"
import { useSubscriptionModal } from "../../utils/use-subscription-modal"
import { AdDebugPanel } from "../../components/ad-debug-panel"

interface MajorIncidentCardProps {
  incident: MajorIncident
  onViewDetails: (incident: MajorIncident) => void
}

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

const MajorIncidentCard: FC<MajorIncidentCardProps> = function MajorIncidentCard({ incident, onViewDetails }) {
  const theme = useAppTheme()
  const animatedHeight = React.useState(new Animated.Value(0))[0]
  const [isAnimated, setIsAnimated] = React.useState(false)

  const statusColor = getStatusColor(incident.category)
  const statusIcon = getStatusIcon(incident.category)
  const timeAgo = moment(incident.created_at).fromNow()

  // Parse the data field which contains additional incident details
  const incidentData = incident.data ? (typeof incident.data === "string" ? JSON.parse(incident.data) : incident.data) : {}
  const councilArea = incidentData.councilArea || "Unknown"
  const responsibleAgency = incidentData.responsibleAgency || "Unknown"
  const incidentType = incidentData.type || incident.category || "Unknown"
  const size = incidentData.size || "Unknown"

  const handleAnimation = () => {
    isAnimated
      ? Animated.timing(animatedHeight, {
          toValue: 0,
          duration: 300,
          useNativeDriver: false,
        }).start()
      : Animated.spring(animatedHeight, {
          toValue: 32,
          useNativeDriver: false,
        }).start()
  }

  const styles = makeCardStyles(theme, statusColor)

  return (
    <Pressable
      onPress={() => {
        setIsAnimated(!isAnimated)
        handleAnimation()
      }}
      style={styles.cardContainer}
    >
      <View style={[styles.iconContainer, { backgroundColor: statusColor }]}>
        <FontAwesome5 name={statusIcon} size={10} color={theme.colors.white} />
      </View>
      <View style={styles.contentContainer}>
        <View style={styles.row}>
          <View style={styles.headerContent}>
            <Text style={styles.title} numberOfLines={2}>
              {incident.title || "Untitled Incident"}
            </Text>
            <Text style={styles.location}>{councilArea}</Text>
          </View>
          <Text style={styles.timeAgo}>{timeAgo}</Text>
        </View>

        <View style={styles.row}>
          <Text style={styles.incidentDetails}>
            {incidentType} • {size} • {responsibleAgency}
          </Text>
        </View>

        <Animated.View
          style={[styles.expandedContainer, { height: animatedHeight, overflow: "hidden" }]}
        >
          <View style={styles.separator} />
          <View style={styles.row}>
            <Animated.View
              style={[
                styles.leftButtons,
                {
                  opacity: animatedHeight.interpolate({
                    inputRange: [0, 32],
                    outputRange: [0, 1],
                  }),
                },
              ]}
            >
              <DetailsButton onPress={() => onViewDetails(incident)} />
            </Animated.View>
            <View style={styles.actionsContainer}>
              <DirectionsButton onPress={() => {}} />
              <ShareButton
                onPress={() => {
                  const incidentData = incident.data ? (typeof incident.data === "string" ? JSON.parse(incident.data) : incident.data) : {}
                  const councilArea = incidentData.councilArea || "Unknown"
                  const incidentType = incidentData.type || incident.category || "Unknown"
                  Share.share({
                    message: `Major Incident: ${incident.title} in ${councilArea} - ${incidentType}`,
                  })
                }}
              />
            </View>
          </View>
        </Animated.View>
      </View>
    </Pressable>
  )
}

export const MajorIncidentsScreen: FC = function MajorIncidentsScreen() {
  const theme = useAppTheme()
  const router = useRouter()
  const flatListRef = useRef<FlatList>(null)
  const { showAdWithFallback } = useInterstitialAd()
  const { showModal, ModalComponent } = useSubscriptionModal()

  const {
    data,
    isLoading,
    isFetchingNextPage,
    fetchNextPage,
    hasNextPage,
    refetch,
  } = useMajorIncidentsPaginated()

  const incidents: MajorIncident[] = data?.pages.flatMap((p) => p.data) || []
  const initialLoaded = !isLoading
  const loadingMore = isFetchingNextPage

  const styles = makeStyles(theme)

  useEffect(() => {
    // Reset scroll tracking when entering screen
    adManager.resetScrollTracking()
  }, [])

  // Interstitial ads are triggered by scroll tracking in renderIncident, not by incident count

  const handleViewDetails = (incident: MajorIncident) => {
    router.push(`/incident/${incident.id}`)
  }

  const renderIncident = (item) => {
    const incident: MajorIncident = item.item
    const incidentIndex = item.index

    // Track scroll progress and check if we should show native ad
    const shouldShowNewAd = adManager.onScroll(incidentIndex + 1)
    const shouldShowStableAd = adManager.shouldShowStableAd(incidentIndex + 1)
    const shouldShowNativeAd = shouldShowNewAd || shouldShowStableAd

    // Check if we should show interstitial ad at scroll threshold
    const shouldShowInterstitial = (incidentIndex + 1) === 200 && adManager.shouldShowInterstitial()

    // Trigger interstitial ad if needed (only once at position 200)
    if (shouldShowInterstitial) {
      console.log(`[MajorIncidentsScreen] Triggering interstitial ad at incident ${incidentIndex + 1}`)
      setTimeout(() => {
        showAdWithFallback(() => showModal("interstitial_ad"))
      }, 1000) // Small delay to let the incident render first
    }

    return (
      <View key={`incident-${incident.id}`}>
        <MajorIncidentCard incident={incident} onViewDetails={handleViewDetails} />
        {shouldShowNativeAd && (
          <NativeAdCell
            key={`native-ad-${incidentIndex + 1}`}
            onAdImpression={() => {
              console.log(`[MajorIncidentsScreen] Native ad impression at major incident index ${incidentIndex}`)
            }}
            onAdClick={() => {
              console.log(`[MajorIncidentsScreen] Native ad clicked at major incident index ${incidentIndex}`)
            }}
          />
        )}
      </View>
    )
  }

  const renderEmpty = () => (
    <View style={styles.emptyContainer}>
      <FontAwesome5
        name="fire"
        size={48}
        color={theme.colors.textDisabled}
        style={styles.emptyIcon}
      />
      <Text style={styles.emptyTitle}>No Major Incidents</Text>
      <Text style={styles.emptySubtitle}>No major incidents have been reported recently.</Text>
    </View>
  )

  const renderFooter = () =>
    loadingMore ? (
      <View style={styles.footerLoading}>
        <ActivityIndicator size="small" color={theme.colors.interactive} />
        <Text style={styles.footerText}>Loading more incidents...</Text>
      </View>
    ) : null

  if (!initialLoaded) {
    return (
      <View style={styles.loadingContainer}>
        <ActivityIndicator size="large" color={theme.colors.interactive} />
        <Text style={styles.loadingText}>Loading major incidents...</Text>
      </View>
    )
  }

  return (
    <View style={styles.container}>
      <FlatList
        ref={flatListRef}
        data={incidents}
        keyExtractor={(incident) => String(incident.id)}
        renderItem={renderIncident}
        showsVerticalScrollIndicator={false}
        contentContainerStyle={styles.listContainer}
        contentInsetAdjustmentBehavior="automatic"
        ListEmptyComponent={renderEmpty}
        ListFooterComponent={renderFooter}
        onEndReached={() => {
          if (hasNextPage) fetchNextPage()
        }}
        onEndReachedThreshold={0.1}
        refreshControl={
          <RefreshControl
            tintColor={theme.colors.interactive}
            refreshing={false}
            onRefresh={() => refetch()}
          />
        }
      />
      <BannerAd />
      <ModalComponent />
      <AdDebugPanel />
    </View>
  )
}

const makeStyles = (theme: AppTheme) =>
  StyleSheet.create({
    container: {
      backgroundColor: theme.colors.backgroundTertiary,
      flex: 1,
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
    listContainer: {
      paddingBottom: 16,
    },
    emptyContainer: {
      flex: 1,
      justifyContent: "center",
      alignItems: "center",
      paddingHorizontal: 32,
      paddingTop: 100,
    },
    emptyIcon: {
      marginBottom: 16,
    },
    emptyTitle: {
      fontSize: 20,
      fontWeight: "600",
      color: theme.colors.textPrimary,
      textAlign: "center",
      marginBottom: 8,
    },
    emptySubtitle: {
      fontSize: 16,
      color: theme.colors.textTertiary,
      textAlign: "center",
      lineHeight: 22,
      marginBottom: 24,
    },
    upgradeButton: {
      backgroundColor: theme.colors.interactive,
      paddingHorizontal: 24,
      paddingVertical: 12,
      borderRadius: 8,
      shadowColor: theme.colors.shadowColor,
      shadowOffset: { width: 0, height: 1 },
      shadowOpacity: 0.2,
      shadowRadius: 2,
      elevation: 3,
    },
    upgradeButtonText: {
      color: theme.colors.white,
      fontSize: 16,
      fontWeight: "600",
    },
    footerLoading: {
      flexDirection: "row",
      justifyContent: "center",
      alignItems: "center",
      paddingVertical: 16,
    },
    footerText: {
      marginLeft: 8,
      fontSize: 14,
      color: theme.colors.textTertiary,
    },
  })

const makeCardStyles = (theme: AppTheme, statusColor: string) =>
  StyleSheet.create({
    cardContainer: {
      backgroundColor: theme.colors.cardBackground,
      marginBottom: 8,
      paddingHorizontal: 16,
      paddingVertical: 12,
      flexDirection: "row",
    },
    iconContainer: {
      width: 20,
      height: 20,
      borderRadius: 3,
      justifyContent: "center",
      alignItems: "center",
      marginRight: 12,
    },
    contentContainer: {
      flex: 1,
    },
    row: {
      alignItems: "center",
      flexDirection: "row",
      justifyContent: "space-between",
    },
    headerContent: {
      flex: 1,
    },
    title: {
      fontSize: 16,
      fontWeight: "600",
      color: theme.colors.textPrimary,
      marginBottom: 2,
    },
    location: {
      fontSize: 14,
      color: theme.colors.textTertiary,
    },
    timeAgo: {
      fontSize: 13,
      color: theme.colors.textTertiary,
      fontWeight: "400",
    },
    incidentDetails: {
      fontSize: 14,
      lineHeight: 19,
      color: theme.colors.textSecondary,
      marginVertical: 3,
    },
    expandedContainer: {
      paddingTop: 0,
    },
    separator: {
      height: 0.5,
      backgroundColor: theme.colors.border,
      marginBottom: 12,
    },
    leftButtons: {
      flexDirection: "row",
      alignItems: "center",
    },
    detailsButton: {
      backgroundColor: theme.colors.interactive,
      paddingHorizontal: 10,
      paddingVertical: 4,
      borderRadius: 4,
    },
    detailsText: {
      fontSize: 11,
      fontWeight: "600",
      color: theme.colors.white,
    },
    actionsContainer: {
      flexDirection: "row",
      alignItems: "center",
    },
    actionIcon: {
      padding: 4,
      marginLeft: 4,
    },
  })
