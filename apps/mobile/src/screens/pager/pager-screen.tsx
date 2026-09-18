import React, { FC, useEffect, useRef, useState } from "react"
import {
  FlatList,
  RefreshControl,
  StyleSheet,
  View,
  TouchableOpacity,
} from "react-native"
import { ActivityIndicator, Text } from "react-native"
import { FontAwesome5 } from "@expo/vector-icons"
import { Stack, useRouter } from "expo-router"

import { Message } from "../../components"
import { useValidSubscription, usePreferencesStore, useAuthStore, useSubscriptionsStore } from "../../stores"
import { useMessages } from "../../hooks"
import { BannerAd } from "../../utils/admob"
import { AppTheme, useAppTheme } from "../../theme"
import { adManager } from "../../utils/ad-manager"
import { useInterstitialAd } from "../../utils/interstitial-ad-manager"
import { NativeAdCell } from "../../components/native-ad-cell"
import { useSubscriptionModal } from "../../utils/use-subscription-modal"
import { AdDebugPanel } from "../../components/ad-debug-panel"

export const PagerScreen: FC =
  function PagerScreen() {
    const router = useRouter()
    const theme = useAppTheme()

    const showSearchFields = usePreferencesStore((s) => s.showSearchFields)
    const pagerFilter = usePreferencesStore((s) => s.pagerFilter)
    const pagerShowUnknown = usePreferencesStore((s) => s.pagerShowUnknown)
    const validSubscription = useValidSubscription()
    const filters = useSubscriptionsStore((s) => s.filters)
    const upsertInstallation = useAuthStore((s) => s.upsertInstallation)
    const styles = makeStyles(theme)

    const [search, setSearch] = useState<string | null>(null)
    const [reloading, setReloading] = useState<boolean>(false)
    const flatListRef = useRef<FlatList>(null)
    const { showAdWithFallback } = useInterstitialAd()
    const { showModal, ModalComponent } = useSubscriptionModal()

    useEffect(() => {
      upsertInstallation()
      adManager.resetScrollTracking()
    }, [])

    const {
      data,
      fetchNextPage,
      hasNextPage,
      isFetchingNextPage,
      isLoading,
      refetch,
    } = useMessages({
      showOnlyFiltered: validSubscription ? pagerFilter : false,
      showUnknown: validSubscription ? pagerShowUnknown : false,
      search: search || undefined,
    })

    const messages = data?.pages.flatMap((p) => p.data) || []
    const initialLoaded = !isLoading
    const loadingMore = isFetchingNextPage

    const renderItem = (item: any) => {
      const message = item.item
      const messageIndex = item.index
      
      // Track scroll progress and check if we should show native ad
      const shouldShowNewAd = adManager.onScroll(messageIndex + 1)
      const shouldShowStableAd = adManager.shouldShowStableAd(messageIndex + 1)
      const shouldShowNativeAd = shouldShowNewAd || shouldShowStableAd
      
      // Check if we should show interstitial ad at scroll threshold
      const shouldShowInterstitial = (messageIndex + 1) === 200 && adManager.shouldShowInterstitial()
      
      // Trigger interstitial ad if needed (only once at position 200)
      if (shouldShowInterstitial) {
        console.log(`🎯 [PagerScreen] Triggering interstitial ad at message ${messageIndex + 1}`)
        setTimeout(() => {
          showAdWithFallback(() => showModal("interstitial_ad"))
        }, 1000) // Small delay to let the message render first
      }
      
      return (
        <View key={`message-${message.id}`}>
          <Message message={message} />
          {shouldShowNativeAd && (
            <NativeAdCell 
              key={`native-ad-${messageIndex + 1}`}
              onAdImpression={() => {
                console.log(`🎯 [PagerScreen] Native ad impression at index ${messageIndex}`)
              }}
              onAdClick={() => {
                console.log(`🎯 [PagerScreen] Native ad clicked at index ${messageIndex}`)
              }}
            />
          )}
        </View>
      )
    }

    const renderFooter = () => {
      return loadingMore ? (
        <View style={styles.footerLoading}>
          <ActivityIndicator size="small" color={theme.colors.interactive} />
          <Text style={styles.footerText}>Loading more messages...</Text>
        </View>
      ) : null
    }

    if (!initialLoaded)
      return (
        <View style={styles.loadingContainer}>
          <ActivityIndicator size="large" color={theme.colors.interactive} />
          <Text style={styles.loadingText}>Loading messages...</Text>
        </View>
      )

    return (
      <View style={styles.container}>
        <Stack.Screen
          options={{
            headerSearchBarOptions: showSearchFields
              ? {
                  placeholder: "Search messages...",
                  onChangeText: (e: any) => {
                    const text = e?.nativeEvent?.text ?? ""
                    setSearch(text === "" ? null : text)
                  },
                  onClose: () => setSearch(null),
                  autoCapitalize: "none",
                }
              : undefined,
          }}
        />

        {pagerFilter && filters.length === 0 ? (
          <View style={styles.emptyContainer}>
            <FontAwesome5
              name={"filter"}
              size={48}
              color={theme.colors.textDisabled}
              style={styles.emptyIcon}
            />
            <Text style={styles.emptyTitle}>No Capcodes Selected</Text>
            <Text style={styles.emptySubtitle}>
              You have filtering enabled but no capcodes selected.
            </Text>
            <TouchableOpacity
              style={styles.actionButton}
              onPress={() => router.push("/capcodes")}
            >
              <Text style={styles.actionButtonText}>Select Capcodes</Text>
            </TouchableOpacity>
          </View>
        ) : messages.length === 0 ? (
          <View style={styles.emptyContainer}>
            <FontAwesome5
              name={"inbox"}
              size={48}
              color={theme.colors.textDisabled}
              style={styles.emptyIcon}
            />
            <Text style={styles.emptyTitle}>No Messages</Text>
            <Text style={styles.emptySubtitle}>
              There are no messages that match your current filters.
            </Text>
            {pagerFilter && (
              <TouchableOpacity
                style={styles.actionButton}
                onPress={() => router.push("/capcodes")}
              >
                <Text style={styles.actionButtonText}>Adjust Capcodes</Text>
              </TouchableOpacity>
            )}
          </View>
        ) : (
          <FlatList
            ref={flatListRef}
            data={messages}
            keyExtractor={(message: any) => String(message.id)}
            showsVerticalScrollIndicator={false}
            renderItem={renderItem}
            ListFooterComponent={renderFooter}
            contentContainerStyle={styles.listContainer}
            contentInsetAdjustmentBehavior="automatic"
            scrollEnabled={true}
            onEndReached={() => hasNextPage && fetchNextPage()}
            refreshControl={
              <RefreshControl
                tintColor={theme.colors.interactive}
                refreshing={reloading}
                onRefresh={() => {
                  setReloading(true)
                  refetch().finally(() => setReloading(false))
                }}
              />
            }
          />
        )}

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
    searchContainer: {
      margin: 16,
      marginTop: 6,
      marginBottom: 8,
      borderRadius: 8,
      backgroundColor: theme.colors.cardBackground,
      alignItems: "center",
      flexDirection: "row",
      paddingHorizontal: 12,
      paddingVertical: 10,
      shadowColor: theme.colors.shadowColor,
      shadowOffset: { width: 0, height: 0.5 },
      shadowOpacity: 0.08,
      shadowRadius: 2,
      elevation: 1,
    },
    searchIcon: {
      marginRight: 8,
    },
    searchInput: {
      flex: 1,
      fontSize: 16,
      color: theme.colors.textPrimary,
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
    emptyContainer: {
      flex: 1,
      justifyContent: "center",
      alignItems: "center",
      paddingHorizontal: 32,
      backgroundColor: theme.colors.backgroundTertiary,
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
    actionButton: {
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
    actionButtonText: {
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
    listContainer: {
      paddingBottom: 16,
    },
  })
