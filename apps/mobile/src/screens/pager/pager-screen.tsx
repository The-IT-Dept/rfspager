import React, { FC, useEffect, useRef, useState } from "react"
import {
  FlatList,
  RefreshControl,
  StyleSheet,
  View,
  TouchableOpacity,
} from "react-native"
import { ActivityIndicator, Text } from "react-native"
import { ScrollMarker } from "../../components/scroll-marker/scroll-marker"
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
import { toolbarIcon } from "../../components/toolbar-icon/toolbar-icon"
import { Images } from "../../constants/images"
import { hideFeedForEmptyFilter, pagerEmptyState } from "./pager-empty-state"

export const PagerScreen: FC =
  function PagerScreen() {
    const router = useRouter()
    const theme = useAppTheme()

    const pagerFilter = usePreferencesStore((s) => s.pagerFilter)
    const pagerShowUnknown = usePreferencesStore((s) => s.pagerShowUnknown)
    const setPagerFilter = usePreferencesStore((s) => s.setPagerFilter)
    const setPagerShowUnknown = usePreferencesStore((s) => s.setPagerShowUnknown)
    const validSubscription = useValidSubscription()
    const filters = useSubscriptionsStore((s) => s.filters)
    const filtersLoaded = useSubscriptionsStore((s) => s.loaded)
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


    // The list is never unmounted. Loading and empty states render through
    // ListEmptyComponent so the same UIScrollView survives every state change;
    // it is the scroll view UIKit watches for the tab bar minimise and header
    // glass, and replacing it made the chrome snap back to expanded.
    const renderEmpty = () => {
      if (!initialLoaded)
        return (
          <View style={styles.loadingContainer}>
            <ActivityIndicator size="large" color={theme.colors.interactive} />
            <Text style={styles.loadingText}>Loading messages...</Text>
          </View>
        )
      const empty = pagerEmptyState({ filterOn: pagerFilter, filterCount: filters.length, filtersLoaded })
      return (
        <View style={styles.emptyContainer}>
          <FontAwesome5
            name={empty.action === "select" ? "filter" : "inbox"}
            size={48}
            color={theme.colors.textDisabled}
            style={styles.emptyIcon}
          />
          <Text style={styles.emptyTitle}>{empty.title}</Text>
          <Text style={styles.emptySubtitle}>{empty.body}</Text>
          {empty.action && (
            <TouchableOpacity style={styles.actionButton} onPress={() => router.push("/capcodes")}>
              <Text style={styles.actionButtonText}>
                {empty.action === "select" ? "Select Capcodes" : "Adjust Capcodes"}
              </Text>
            </TouchableOpacity>
          )}
        </View>
      )
    }

    const listData = hideFeedForEmptyFilter({ filterOn: pagerFilter, filterCount: filters.length, filtersLoaded })
      ? []
      : messages

    return (
      <View style={styles.container}>
        {/* Native action menu: a single glass "filter" capsule in the header
            that opens a UIMenu. The toggles mirror Settings → Pager so they are
            one tap away from the feed; both are Pro features, gated the same
            way Settings gates them. */}
        <Stack.Toolbar placement="right">
          <Stack.Toolbar.Menu
            icon={toolbarIcon("line.3.horizontal.decrease.circle", Images.filter)}
            accessibilityLabel="Feed options"
          >
            <Stack.Toolbar.MenuAction
              icon="checkmark.circle"
              isOn={pagerFilter}
              unstable_keepPresented
              onPress={() => {
                console.log("[pager-menu] filter tapped", { validSubscription, pagerFilter })
                validSubscription ? setPagerFilter(!pagerFilter) : showModal()
              }}
            >
              Only selected capcodes
            </Stack.Toolbar.MenuAction>
            <Stack.Toolbar.MenuAction
              icon="questionmark.circle"
              isOn={pagerShowUnknown}
              unstable_keepPresented
              onPress={() => {
                console.log("[pager-menu] unknown tapped", { validSubscription, pagerShowUnknown })
                validSubscription ? setPagerShowUnknown(!pagerShowUnknown) : showModal()
              }}
            >
              Show unknown capcodes
            </Stack.Toolbar.MenuAction>
          </Stack.Toolbar.Menu>
        </Stack.Toolbar>

        {/* Native search, iOS 26 style: a glass magnifier button in the header
            that expands into the field in place. Always present — the button
            itself is the show/hide, so a preference for it was redundant. */}
        <Stack.SearchBar
          placement="integratedButton"
          placeholder="Search messages..."
          autoCapitalize="none"
          hideWhenScrolling={false}
          onChangeText={(e) => {
            const text = e.nativeEvent.text ?? ""
            setSearch(text === "" ? null : text)
          }}
          onCancelButtonPress={() => setSearch(null)}
        />

          <ScrollMarker>
            <FlatList
              ref={flatListRef}
              data={listData}
              keyExtractor={(message: any) => String(message.id)}
              ListEmptyComponent={renderEmpty}
              // Items only ever prepend (newer pages) or append; pin the viewport
              // so an arriving message above the fold does not scroll the list.
              maintainVisibleContentPosition={{ minIndexForVisible: 0 }}
              showsVerticalScrollIndicator={false}
              renderItem={renderItem}
              ListFooterComponent={renderFooter}
              contentContainerStyle={[
                styles.listContainer,
                listData.length === 0 && styles.listContainerEmpty,
              ]}
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
          </ScrollMarker>

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
    // Lets the loading / empty views centre in the viewport when there are
    // no rows, since they now live inside the list.
    listContainerEmpty: {
      flexGrow: 1,
    },
  })
